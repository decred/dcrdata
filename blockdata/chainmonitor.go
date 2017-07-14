// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package blockdata

import (
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg/chainhash"
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
	quit            chan struct{}
	wg              *sync.WaitGroup
	watchaddrs      map[string]txhelpers.TxAction
	blockChan       chan *chainhash.Hash
	recvTxBlockChan chan *txhelpers.BlockWatchedTx
	reorgChan       chan *ReorgData

	// reorg handling
	reorgLock    sync.Mutex
	reorgData    *ReorgData
	sideChain    []chainhash.Hash
	reorganizing bool
}

// NewChainMonitor creates a new chainMonitor
func NewChainMonitor(collector *Collector,
	savers []BlockDataSaver,
	quit chan struct{}, wg *sync.WaitGroup,
	addrs map[string]txhelpers.TxAction, blockChan chan *chainhash.Hash,
	recvTxBlockChan chan *txhelpers.BlockWatchedTx,
	reorgChan chan *ReorgData) *chainMonitor {
	return &chainMonitor{
		collector:       collector,
		dataSavers:      savers,
		quit:            quit,
		wg:              wg,
		watchaddrs:      addrs,
		blockChan:       blockChan,
		recvTxBlockChan: recvTxBlockChan,
		reorgChan:       reorgChan,
	}
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
			if !ok {
				log.Warnf("Block connected channel closed.")
				break out
			}

			// If reorganizing, the block will first go to a side chain
			p.reorgLock.Lock()
			reorg, reorgData := p.reorganizing, p.reorgData
			p.reorgLock.Unlock()

			if reorg {
				p.sideChain = append(p.sideChain, *hash)
				log.Infof("Adding block %v to sidechain", *hash)

				// Just append to side chain until the new main chain tip block is reached
				if !reorgData.NewChainHead.IsEqual(hash) {
					break keepon
				}

				// Reorg is complete
				p.sideChain = nil
				p.reorgLock.Lock()
				p.reorganizing = false
				p.reorgLock.Unlock()
				log.Infof("Reorganization to block %v (height %d) complete",
					p.reorgData.NewChainHead, p.reorgData.NewChainHeight)
			}

			block, _ := p.collector.dcrdChainSvr.GetBlock(hash)
			height := block.Height()
			log.Infof("Block height %v connected", height)

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

			// data collection with timeout
			bdataChan := make(chan *BlockData)
			// fire it off and get the BlockData pointer back through the channel
			go func() {
				BlockData, err := p.collector.Collect()
				if err != nil {
					log.Errorf("Block data collection failed: %v", err.Error())
					// BlockData is nil when err != nil
				}
				bdataChan <- BlockData
			}()

			// Wait for X seconds before giving up on Collect()
			var BlockData *BlockData
			select {
			case BlockData = <-bdataChan:
				if BlockData == nil {
					break keepon
				}
			case <-time.After(time.Second * 20):
				log.Errorf("Block data collection TIMEOUT after 20 seconds.")
				break keepon
			}

			// Store block data with each saver
			for _, s := range p.dataSavers {
				if s != nil {
					// save data to wherever the saver wants to put it
					go s.Store(BlockData)
				}
			}

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting block connected handler.")
				break out
			}
		}
	}

}

// ReorgHandler receives notification of a chain reorganization and initiates a
// corresponding update of the SQL db keeping the main chain data.
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

			log.Infof("Reorganize started. NEW head block %v at height %d.",
				newHash, newHeight)
			log.Infof("Reorganize started. OLD head block %v at height %d.",
				oldHash, oldHeight)

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting reorg notification handler.")
				break out
			}
		}
	}
}
