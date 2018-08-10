// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrpg

import (
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
)

// ReorgData contains the information from a reorganization notification.
type ReorgData struct {
	OldChainHead   chainhash.Hash
	OldChainHeight int32
	NewChainHead   chainhash.Hash
	NewChainHeight int32
	WG             *sync.WaitGroup
}

// ChainMonitor responds to block connection and chain reorganization.
type ChainMonitor struct {
	db             *ChainDBRPC
	quit           chan struct{}
	wg             *sync.WaitGroup
	blockChan      chan *chainhash.Hash
	reorgChan      chan *ReorgData
	syncConnect    sync.Mutex
	ConnectingLock chan struct{}
	DoneConnecting chan struct{}

	// reorg handling
	sync.Mutex
	reorgData    *ReorgData
	sideChain    []chainhash.Hash
	reorganizing bool
}

// NewChainMonitor creates a new ChainMonitor.
func (db *ChainDBRPC) NewChainMonitor(quit chan struct{}, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash, reorgChan chan *ReorgData) *ChainMonitor {
	return &ChainMonitor{
		db:             db,
		quit:           quit,
		wg:             wg,
		blockChan:      blockChan,
		reorgChan:      reorgChan,
		ConnectingLock: make(chan struct{}, 1),
		DoneConnecting: make(chan struct{}),
	}
}

// BlockConnectedSync is the synchronous (blocking call) handler for the newly
// connected block given by the hash.
func (p *ChainMonitor) BlockConnectedSync(hash *chainhash.Hash) {
	// Connections go one at a time so signals cannot be mixed
	p.syncConnect.Lock()
	defer p.syncConnect.Unlock()
	// lock with buffered channel, accepting handoff in BlockConnectedHandler
	p.ConnectingLock <- struct{}{}
	p.blockChan <- hash
	// wait
	<-p.DoneConnecting
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
			p.Lock()
			release := func() { p.Unlock() }
			select {
			case <-p.ConnectingLock:
				// send on unbuffered channel
				release = func() { p.Unlock(); p.DoneConnecting <- struct{}{} }
			default:
			}

			if !ok {
				log.Warnf("Block connected channel closed.")
				release()
				break out
			}

			// If reorganizing, the block will first go to a side chain
			reorg, reorgData := p.reorganizing, p.reorgData

			// Otherwise this handler does nothing.
			if !reorg {
				release()
				break keepon
			}

			p.sideChain = append(p.sideChain, *hash)
			log.Infof("Adding block %v to sidechain", *hash)

			// Just append to side chain until the new main chain tip block is reached
			if reorgData.NewChainHead != *hash {
				release()
				break keepon
			}

			// Once all blocks in side chain are lined up, switch over
			log.Info("Switching to side chain...")
			newHeight, newHash, err := p.switchToSideChain()
			if err != nil {
				panic(err)
			}

			// Verify the hash of the new main chain tip is as expected.
			if p.reorgData.NewChainHead != *newHash ||
				p.reorgData.NewChainHeight != newHeight {
				panic(fmt.Sprintf("Failed to reorg to %v. Got to %v (height %d) instead.",
					p.reorgData.NewChainHead, newHash, newHeight))
			}

			// Reorg is complete
			p.sideChain = nil
			p.reorganizing = false
			p.db.InReorg = false
			log.Infof("Reorganization to block %v (height %d) complete",
				p.reorgData.NewChainHead, p.reorgData.NewChainHeight)

			release()

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting block connected handler.")
				break out
			}
		}
	}

}

// switchToSideChain attempts to switch to a new side chain by: determining a
// common ancestor block, flagging elements of the old main chain as
// is_mainchain, and connecting the side chain blocks onto the mainchain.
func (p *ChainMonitor) switchToSideChain() (int32, *chainhash.Hash, error) {
	if len(p.sideChain) == 0 {
		return 0, nil, fmt.Errorf("no side chain")
	}

	// Time this process.
	defer func(start time.Time) {
		log.Infof("dcrpg: switchToSideChain completed in %v", time.Since(start))
	}(time.Now())

	// Determine highest common ancestor of side chain and main chain
	msgBlock, err := p.db.Client.GetBlock(&p.sideChain[0])
	if err != nil {
		return 0, nil, fmt.Errorf("unable to get block at root of side chain")
	}
	block := dcrutil.NewBlock(msgBlock)

	mainRootHash := msgBlock.Header.PrevBlock
	prevMsgBlock, err := p.db.Client.GetBlock(&mainRootHash)
	if err != nil {
		return 0, nil, fmt.Errorf("unable to get common ancestor on side chain")
	}
	prevBlock := dcrutil.NewBlock(prevMsgBlock)

	commonAncestorHeight := block.Height() - 1
	if prevBlock.Height() != commonAncestorHeight {
		panic("Failed to determine common ancestor.")
	}

	// Flag blocks from mainRoot+1 to mainTip as is_mainchain=false
	startTime := time.Now()
	mainTip := int64(p.db.Height())
	mainRoot := mainRootHash.String()
	log.Infof("Moving %d blocks to side chain...", mainTip-commonAncestorHeight)
	newMainRoot, numBlocksmoved, err := p.db.TipToSideChain(mainRoot)
	if err != nil || mainRoot != newMainRoot {
		return 0, nil, fmt.Errorf("failed to flag blocks as side chain")
	}
	log.Infof("Moved %d blocks from the main chain to a side chain in %v.",
		numBlocksmoved, time.Since(startTime))

	// Verify the tip is now the previous common ancestor
	mainTip = int64(p.db.Height())
	if mainTip != commonAncestorHeight {
		panic(fmt.Sprintf("disconnect blocks failed: tip height %d, expected %d",
			mainTip, commonAncestorHeight))
	}

	// Connect blocks in side chain onto main chain
	log.Debugf("Connecting %d blocks", len(p.sideChain))
	currentHeight := commonAncestorHeight + 1
	for i := range p.sideChain {
		// Try to find block in stakedb cache
		block, found := p.db.stakeDB.BlockCached(currentHeight)
		if found {
			msgBlock = block.MsgBlock()
		}
		// Fetch the block by RPC if not found or wrong hash
		if !found || msgBlock.BlockHash() != p.sideChain[i] {
			log.Debugf("block %v not found in stakedb cache, fetching from dcrd", p.sideChain[i])
			// Request MsgBlock from dcrd
			msgBlock, err = p.db.Client.GetBlock(&p.sideChain[i])
			if err != nil {
				return 0, nil, fmt.Errorf("unable to get side chain block %v", p.sideChain[i])
			}
		}

		// Get current winning votes from stakedb
		tpi, found := p.db.stakeDB.PoolInfo(p.sideChain[i])
		if !found {
			return 0, nil, fmt.Errorf("stakedb.PoolInfo failed to return info for: %v", p.sideChain[i])
		}
		winners := tpi.Winners

		// New blocks stored this way are considered part of mainchain. They are
		// also considered valid unless invalidated by the next block
		// (invalidation of previous handled inside StoreBlock).
		isValid, isMainChain := true, true
		_, _, err := p.db.StoreBlock(msgBlock, winners, isValid, isMainChain, true, true)
		if err != nil {
			return int32(p.db.Height()), p.db.Hash(),
				fmt.Errorf("error connecting block %v", p.sideChain[i])
		}

		log.Infof("Connected block %v (height %d) from side chain.",
			msgBlock.BlockHash(), msgBlock.Header.Height)

		currentHeight++
	}

	log.Infof("Moved %d blocks from a side chain to the main chain in %v.",
		numBlocksmoved, time.Since(startTime))

	mainTip = int64(p.db.Height())
	if mainTip != int64(msgBlock.Header.Height) {
		panic("connected block height not db tip height")
	}

	blockHash := msgBlock.BlockHash()
	return int32(mainTip), &blockHash, nil
}

// ReorgHandler receives notification of a chain reorganization and initiates a
// corresponding reorganization of the ChainDB.
func (p *ChainMonitor) ReorgHandler() {
	defer p.wg.Done()
out:
	for {
	keepon:
		select {
		case reorgData, ok := <-p.reorgChan:
			p.Lock()
			if !ok {
				p.Unlock()
				log.Warnf("Reorg channel closed.")
				break out
			}

			newHeight, oldHeight := reorgData.NewChainHeight, reorgData.OldChainHeight
			newHash, oldHash := reorgData.NewChainHead, reorgData.OldChainHead

			if p.reorganizing {
				log.Errorf("Reorg notified for chain tip %v (height %v), but already "+
					"processing a reorg to block %v", newHash, newHeight,
					p.reorgData.NewChainHead)
				p.Unlock()
				break keepon
			}

			// This is the primary action of this handler.
			p.db.InReorg = true
			p.reorganizing = true
			p.reorgData = reorgData
			p.Unlock()

			log.Infof("Reorganize started. NEW head block %v at height %d.",
				newHash, newHeight)
			log.Infof("Reorganize started. OLD head block %v at height %d.",
				oldHash, oldHeight)

			reorgData.WG.Done()

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting reorg notification handler.")
				break out
			}
		}
	}
}
