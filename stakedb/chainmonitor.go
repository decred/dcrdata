// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package stakedb

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

// ChainMonitor connects blocks to the stake DB as they come in.
type ChainMonitor struct {
	db        *StakeDatabase
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
func (db *StakeDatabase) NewChainMonitor(quit chan struct{}, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash, reorgChan chan *ReorgData) *ChainMonitor {
	return &ChainMonitor{
		db:        db,
		quit:      quit,
		wg:        wg,
		blockChan: blockChan,
		reorgChan: reorgChan,
	}
}

// BlockConnectedHandler handles block connected notifications, which trigger
// data collection and storage.
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
				if reorgData.NewChainHead != *hash {
					break keepon
				}

				// Once all blocks in side chain are lined up, switch over
				log.Info("Switching to side chain...")
				newHeight, newHash, err := p.switchToSideChain()
				if err != nil {
					log.Error(err)
				}

				if p.reorgData.NewChainHead != *newHash ||
					p.reorgData.NewChainHeight != newHeight {
					panic(fmt.Sprintf("Failed to reorg to %v. Got to %v (height %d) instead.",
						p.reorgData.NewChainHead, newHash, newHeight))
				}

				// Reorg is complete
				p.sideChain = nil
				p.reorgLock.Lock()
				p.reorganizing = false
				p.reorgLock.Unlock()
				log.Infof("Reorganization to block %v (height %d) complete",
					p.reorgData.NewChainHead, p.reorgData.NewChainHeight)
			} else {
				// Extend main chain
				block, err := p.db.ConnectBlockHash(hash)
				if err != nil {
					log.Error(err)
					break keepon
				}

				log.Infof("Connected block %d to stake DB.", block.Height())
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

	// Determine highest common ancestor of side chain and main chain
	block, err := p.db.NodeClient.GetBlock(&p.sideChain[0])
	if err != nil {
		return 0, nil, fmt.Errorf("unable to get block on side chain")
	}

	commonAncestorHeight := int64(block.Height()) - 1
	mainTip := int64(p.db.Height())

	// Disconnect blocks back to common ancestor
	if err = p.db.DisconnectBlocks(mainTip - commonAncestorHeight); err != nil {
		return 0, nil, err
	}

	mainTip = int64(p.db.Height())
	if mainTip != commonAncestorHeight {
		panic(fmt.Sprintf("disconnect blocks failed: tip height %d, expected %d",
			mainTip, commonAncestorHeight))
	}

	// Connect blocks in side chain onto main chain
	for i := range p.sideChain {
		if block, err = p.db.ConnectBlockHash(&p.sideChain[i]); err != nil {
			mainTip = int64(p.db.Height())
			block, _ = p.db.Block(mainTip)
			return int32(mainTip), block.Hash(), fmt.Errorf("error connecting block %v", p.sideChain[i])
		}
		log.Infof("Connected block %v (height %d) from side chain.",
			block.Hash(), block.Height())
	}

	mainTip = int64(p.db.Height())
	if mainTip != block.Height() {
		panic("connected block height not db tip height")
	}

	return int32(mainTip), block.Hash(), nil
}

// ReorgHandler receives notification of a chain reorganization and initiates a
// corresponding reorganization of the stakedb.StakeDatabase.
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
