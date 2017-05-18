// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrsqlite

import (
	"fmt"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

// ReorgData contains the information from a reoranization notification
type ReorgData struct {
	OldChainHead   chainhash.Hash
	OldChainHeight int32
	NewChainHead   chainhash.Hash
	NewChainHeight int32
}

// ChainMonitor handles change notifications from the node client
type ChainMonitor struct {
	db        *wiredDB
	quit      chan struct{}
	wg        *sync.WaitGroup
	blockChan chan *chainhash.Hash
	reorgChan chan *ReorgData

	// reorg handling
	reorgLock    sync.Mutex
	reorgData    *ReorgData
	sideChain    []chainhash.Hash
	reorganizing bool
}

// NewChainMonitor creates a new ChainMonitor
func (db *wiredDB) NewChainMonitor(quit chan struct{}, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash, reorgChan chan *ReorgData) *ChainMonitor {
	return &ChainMonitor{
		db:        db,
		quit:      quit,
		wg:        wg,
		blockChan: blockChan,
		reorgChan: reorgChan,
	}
}

// BlockConnectedHandler handles block connected notifications, which helps deal
// with a chain reorganization.
func (p *ChainMonitor) BlockConnectedHandler() {
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

				// Once all blocks in side chain are lined up, switch over
				// newHeight, newHash, err := p.switchToSideChain()
				// if err != nil {
				// 	log.Error(err)
				// }

				// if !p.reorgData.NewChainHead.IsEqual(newHash) ||
				// 	p.reorgData.NewChainHeight != newHeight {
				// 	panic(fmt.Sprintf("Failed to reorg to %v. Got to %v (height %d) instead.",
				// 		p.reorgData.NewChainHead, newHash, newHeight))
				// }

				// Reorg is complete
				p.sideChain = nil
				p.reorgLock.Lock()
				p.reorganizing = false
				p.reorgLock.Unlock()
				log.Infof("Reorganization to block %v (height %d) complete",
					p.reorgData.NewChainHead, p.reorgData.NewChainHeight)
			}

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting block connected handler.")
				break out
			}
		}
	}

}

// switchToSideChain attempts to switch to a new side chain by: determining a
// common ancestor block, disconnecting blocks from the main chain back to this
// block, and connecting the side chain blocks onto the mainchain.
func (p *ChainMonitor) switchToSideChain() (int32, *chainhash.Hash, error) {
	if len(p.sideChain) == 0 {
		return 0, nil, fmt.Errorf("no side chain")
	}

	// Update DBs, just overwrite

	return 0, nil, nil
}

// ReorgHandler receives notification of a chain reorganization and initiates a
// corresponding update of the SQL db keeping the main chain data.
func (p *ChainMonitor) ReorgHandler() {
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
			newHash, oldHash := reorgData.NewChainHeight, reorgData.OldChainHeight

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
