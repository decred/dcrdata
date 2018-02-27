// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package blockdata

import (
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrdata/txhelpers"
)

// ReorgData contains the information from a reoranization notification
type ReorgData struct {
	OldChainHead   chainhash.Hash
	OldChainHeight int32
	NewChainHead   chainhash.Hash
	NewChainHeight int32
}

// for getblock, ticketfeeinfo, estimatestakediff, etc.
type chainMonitor struct {
	collector       *Collector
	dataSavers      []BlockDataSaver
	reorgDataSavers []BlockDataSaver
	quit            chan struct{}
	wg              *sync.WaitGroup
	watchaddrs      map[string]txhelpers.TxAction
	blockChan       chan *chainhash.Hash
	recvTxBlockChan chan *txhelpers.BlockWatchedTx
	reorgChan       chan *ReorgData
	ConnectingLock  chan struct{}
	DoneConnecting  chan struct{}
	syncConnect     sync.Mutex

	// reorg handling
	reorgLock    sync.Mutex
	reorgData    *ReorgData
	sideChain    []chainhash.Hash
	reorganizing bool
}

// NewChainMonitor creates a new chainMonitor
func NewChainMonitor(collector *Collector,
	savers []BlockDataSaver, reorgSavers []BlockDataSaver,
	quit chan struct{}, wg *sync.WaitGroup,
	addrs map[string]txhelpers.TxAction, blockChan chan *chainhash.Hash,
	recvTxBlockChan chan *txhelpers.BlockWatchedTx,
	reorgChan chan *ReorgData) *chainMonitor {
	return &chainMonitor{
		collector:       collector,
		dataSavers:      savers,
		reorgDataSavers: reorgSavers,
		quit:            quit,
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
func (p *chainMonitor) BlockConnectedSync(hash *chainhash.Hash) {
	// Connections go one at a time so signals cannot be mixed
	p.syncConnect.Lock()
	defer p.syncConnect.Unlock()
	// lock with buffered channel
	p.ConnectingLock <- struct{}{}
	p.blockChan <- hash
	// wait
	<-p.DoneConnecting
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
			release := func() {}
			select {
			case <-p.ConnectingLock:
				// send on unbuffered channel
				release = func() { p.DoneConnecting <- struct{}{} }
			default:
			}

			if !ok {
				log.Warnf("Block connected channel closed.")
				release()
				break out
			}

			// If reorganizing, the block will first go to a side chain
			p.reorgLock.Lock()
			reorg, reorgData := p.reorganizing, p.reorgData
			p.reorgLock.Unlock()

			if reorg {
				p.sideChain = append(p.sideChain, *hash)
				log.Infof("Adding block %v to blockdata sidechain", *hash)

				// Just append to side chain until the new main chain tip block is reached
				if !reorgData.NewChainHead.IsEqual(hash) {
					release()
					break keepon
				}

				// Reorg is complete, do nothing lol
				p.sideChain = nil
				p.reorgLock.Lock()
				p.reorganizing = false
				p.reorgLock.Unlock()
				log.Infof("Reorganization to block %v (height %d) complete in blockdata",
					p.reorgData.NewChainHead, p.reorgData.NewChainHeight)
				// dcrsqlite's chainmonitor handles the reorg, but we keep going
				// to update the web UI with the new best block.
			}

			msgBlock, _ := p.collector.dcrdChainSvr.GetBlock(hash)
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

			var blockData *BlockData
			chainHeight, err := p.collector.dcrdChainSvr.GetBlockCount()
			if err != nil {
				log.Errorf("Unable to get chain height: %v", err)
				release()
				break keepon
			}

			// If new block height not equal to chain height, then we are behind
			// on data collection, so specify the hash of the notified, skipping
			// stake diff estimates and other stuff for web ui that is only
			// relevant for the best block.
			if chainHeight != height {
				log.Infof("Behind on our collection...")
				blockData, _, err = p.collector.CollectHash(hash)
				if err != nil {
					log.Errorf("blockdata.CollectHash(hash) failed: %v", err.Error())
					release()
					break keepon
				}
			} else {
				blockData, _, err = p.collector.Collect()
				if err != nil {
					log.Errorf("blockdata.Collect() failed: %v", err.Error())
					release()
					break keepon
				}
			}

			// Store block data with each saver
			savers := p.dataSavers
			if reorg {
				savers = p.reorgDataSavers
			}
			for _, s := range savers {
				if s != nil {
					// save data to wherever the saver wants to put it
					s.Store(blockData, msgBlock)
				}
			}

			release()

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting block connected handler.")
				break out
			}
		}
	}

}

// ReorgHandler receives notification of a chain reorganization
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

			newHeight, oldHeight := reorgData.NewChainHeight, reorgData.OldChainHeight
			newHash, oldHash := reorgData.NewChainHead, reorgData.OldChainHead

			p.reorgLock.Lock()
			if p.reorganizing {
				p.reorgLock.Unlock()
				log.Errorf("Reorg notified for chain tip %v (height %v), but already "+
					"processing a reorg to block %v", newHash, newHeight,
					p.reorgData.NewChainHead)
				break keepon
			}

			p.reorganizing = true
			p.reorgData = reorgData
			p.reorgLock.Unlock()

			log.Infof("Reorganize started in blockdata. NEW head block %v at height %d.",
				newHash, newHeight)
			log.Infof("Reorganize started in blockdata. OLD head block %v at height %d.",
				oldHash, oldHeight)

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting reorg notification handler.")
				break out
			}
		}
	}
}
