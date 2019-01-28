// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package blockdata

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v4/txhelpers"
)

// for getblock, ticketfeeinfo, estimatestakediff, etc.
type chainMonitor struct {
	ctx             context.Context
	collector       *Collector
	dataSavers      []BlockDataSaver
	reorgDataSavers []BlockDataSaver
	wg              *sync.WaitGroup
	watchaddrs      map[string]txhelpers.TxAction
	blockChan       chan *chainhash.Hash
	recvTxBlockChan chan *txhelpers.BlockWatchedTx
	reorgChan       chan *txhelpers.ReorgData
	ConnectingLock  chan struct{}
	DoneConnecting  chan struct{}
	syncConnect     sync.Mutex
	reorgLock       sync.Mutex
}

// NewChainMonitor creates a new chainMonitor.
func NewChainMonitor(ctx context.Context, collector *Collector, savers []BlockDataSaver,
	reorgSavers []BlockDataSaver, wg *sync.WaitGroup, addrs map[string]txhelpers.TxAction,
	blockChan chan *chainhash.Hash, recvTxBlockChan chan *txhelpers.BlockWatchedTx,
	reorgChan chan *txhelpers.ReorgData) *chainMonitor {
	return &chainMonitor{
		ctx:             ctx,
		collector:       collector,
		dataSavers:      savers,
		reorgDataSavers: reorgSavers,
		wg:              wg,
		watchaddrs:      addrs,
		blockChan:       blockChan,
		recvTxBlockChan: recvTxBlockChan,
		reorgChan:       reorgChan,
		ConnectingLock:  make(chan struct{}, 1),
		DoneConnecting:  make(chan struct{}),
	}
}

// BlockConnectedSync is the synchronous (blocking call) handler for the newly
// connected block given by the hash.
func (p *chainMonitor) BlockConnectedSync(hash *chainhash.Hash) error {
	// Connections go one at a time so signals cannot be mixed
	p.syncConnect.Lock()
	defer p.syncConnect.Unlock()
	// lock with buffered channel
	p.ConnectingLock <- struct{}{}
	p.blockChan <- hash
	// wait
	<-p.DoneConnecting
	return nil
}

func (p *chainMonitor) collect(hash *chainhash.Hash) (*wire.MsgBlock, *BlockData, error) {
	// getblock RPC
	msgBlock, err := p.collector.dcrdChainSvr.GetBlock(hash)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get block %v: %v", hash, err)
	}
	block := dcrutil.NewBlock(msgBlock)
	height := block.Height()
	log.Infof("Block height %v connected. Collecting data...", height)

	if len(p.watchaddrs) > 0 {
		// txsForOutpoints := blockConsumesOutpointWithAddresses(block, p.watchaddrs,
		// 	p.collector.dcrdChainSvr)
		// if len(txsForOutpoints) > 0 {
		// 	p.spendTxBlockChan <- &BlockWatchedTx{height, txsForOutpoints}
		// }

		txsForAddrs := txhelpers.BlockReceivesToAddresses(block,
			p.watchaddrs, p.collector.netParams)
		if len(txsForAddrs) > 0 {
			p.recvTxBlockChan <- &txhelpers.BlockWatchedTx{
				BlockHeight:   height,
				TxsForAddress: txsForAddrs}
		}
	}

	// Get node's best block height to see if the block for which we are
	// collecting data is the best block.
	chainHeight, err := p.collector.dcrdChainSvr.GetBlockCount()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get chain height: %v", err)
	}

	// If new block height not equal to chain height, then we are behind
	// on data collection, so specify the hash of the notified, skipping
	// stake diff estimates and other stuff for web ui that is only
	// relevant for the best block.
	var blockData *BlockData
	if chainHeight != height {
		log.Debugf("Collecting data for block %v (%d), behind tip %d.",
			hash, height, chainHeight)
		blockData, _, err = p.collector.CollectHash(hash)
		if err != nil {
			return nil, nil, fmt.Errorf("blockdata.CollectHash(hash) failed: %v", err.Error())
		}
	} else {
		blockData, _, err = p.collector.Collect()
		if err != nil {
			return nil, nil, fmt.Errorf("blockdata.Collect() failed: %v", err.Error())
		}
	}

	return msgBlock, blockData, nil
}

// blockConnectedHandler handles block connected notifications, which trigger
// data collection and storage.
func (p *chainMonitor) BlockConnectedHandler() {
	defer p.wg.Done()
out:
	for {
	keepon:
		select {
		case hash, ok := <-p.blockChan:
			// Do not handle reorg and block connects simultaneously.
			p.reorgLock.Lock()
			release := func() { p.reorgLock.Unlock() }

			select {
			case <-p.ConnectingLock:
				// send on unbuffered channel
				release = func() { p.reorgLock.Unlock(); p.DoneConnecting <- struct{}{} }
			default:
			}

			if !ok {
				log.Warnf("Block connected channel closed.")
				release()
				break out
			}

			// Collect block data.
			msgBlock, blockData, err := p.collect(hash)
			if err != nil {
				log.Errorf("Failed to collect data for block %v: %v", hash, err)
				release()
				break keepon
			}

			// Store block data with each saver.
			for _, s := range p.dataSavers {
				if s != nil {
					// Save data to wherever the saver wants to put it.
					if err = s.Store(blockData, msgBlock); err != nil {
						log.Errorf("(%v).Store failed: %v", reflect.TypeOf(s), err)
					}
				}
			}

			release()

		case <-p.ctx.Done():
			log.Debugf("Got quit signal. Exiting block connected handler.")
			break out
		}
	}

}

// ReorgHandler receives notification of a chain reorganization. A reorg is
// handled in blockdata by simply collecting data for the new best block, and
// storing it in the *reorgDataSavers*.
func (p *chainMonitor) ReorgHandler() {
	defer p.wg.Done()
out:
	for {
	keepon:
		select {
		case reorgData, ok := <-p.reorgChan:
			if !ok {
				log.Warnf("Reorg channel closed.")
				break out
			}
			if reorgData == nil {
				log.Warnf("nil reorg data received!")
				break keepon
			}

			newHeight := reorgData.NewChainHeight
			newHash := reorgData.NewChainHead

			// Do not handle reorg and block connects simultaneously.
			p.reorgLock.Lock()

			log.Infof("Reorganize signaled to blockdata. "+
				"Collecting data for NEW head block %v at height %d.",
				newHash, newHeight)

			// Collect data for the new best block.
			msgBlock, blockData, err := p.collect(&newHash)
			if err != nil {
				log.Errorf("ReorgHandler: Failed to collect data for block %v: %v", newHash, err)
				p.reorgLock.Unlock()
				reorgData.WG.Done()
				break keepon
			}

			// Store block data with each REORG saver.
			for _, s := range p.reorgDataSavers {
				if s != nil {
					// Save data to wherever the saver wants to put it.
					if err := s.Store(blockData, msgBlock); err != nil {
						log.Errorf("(%v).Store failed: %v", reflect.TypeOf(s), err)
					}
				}
			}

			p.reorgLock.Unlock()

			reorgData.WG.Done()

		case <-p.ctx.Done():
			log.Debugf("Got quit signal. Exiting reorg notification handler.")
			break out
		}
	}
}
