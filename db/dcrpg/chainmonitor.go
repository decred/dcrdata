// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrpg

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v3/txhelpers"
)

// ChainMonitor responds to block connection and chain reorganization.
type ChainMonitor struct {
	ctx            context.Context
	db             *ChainDBRPC
	wg             *sync.WaitGroup
	blockChan      chan *chainhash.Hash
	reorgChan      chan *txhelpers.ReorgData
	ConnectingLock chan struct{}
	DoneConnecting chan struct{}
}

// NewChainMonitor creates a new ChainMonitor.
func (db *ChainDBRPC) NewChainMonitor(ctx context.Context, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash, reorgChan chan *txhelpers.ReorgData) *ChainMonitor {
	if db == nil {
		return nil
	}
	return &ChainMonitor{
		ctx:            ctx,
		db:             db,
		wg:             wg,
		blockChan:      blockChan,
		reorgChan:      reorgChan,
		ConnectingLock: make(chan struct{}, 1),
		DoneConnecting: make(chan struct{}),
	}
}

// switchToSideChain attempts to switch to a new side chain by: determining a
// common ancestor block, flagging elements of the old main chain as
// is_mainchain, and connecting the side chain blocks onto the mainchain.
func (p *ChainMonitor) switchToSideChain(reorgData *txhelpers.ReorgData) (int32, *chainhash.Hash, error) {
	if reorgData == nil || len(reorgData.NewChain) == 0 {
		return 0, nil, fmt.Errorf("no side chain")
	}

	// Time this process.
	defer func(start time.Time) {
		log.Infof("dcrpg: switchToSideChain completed in %v", time.Since(start))
	}(time.Now())

	newChain := reorgData.NewChain
	// newChain does not include the common ancestor.
	commonAncestorHeight := int64(reorgData.NewChainHeight) - int64(len(newChain))

	mainTip := int64(p.db.Height())
	if mainTip != int64(reorgData.OldChainHeight) {
		log.Warnf("StakeDatabase height is %d, expected %d. Rewinding as "+
			"needed to complete reorg from ancestor at %d", mainTip,
			reorgData.OldChainHeight, commonAncestorHeight)
	}

	// Flag blocks from mainRoot+1 to mainTip as is_mainchain=false
	startTime := time.Now()
	mainRoot := reorgData.CommonAncestor.String()
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
	log.Debugf("Connecting %d blocks", len(newChain))
	currentHeight := commonAncestorHeight + 1
	var endHash chainhash.Hash
	var endHeight int32
	for i := range newChain {
		var msgBlock *wire.MsgBlock
		// Try to find block in stakedb cache
		block, found := p.db.stakeDB.BlockCached(currentHeight)
		if found {
			msgBlock = block.MsgBlock()
		}
		// Fetch the block by RPC if not found or wrong hash
		if !found || msgBlock.BlockHash() != newChain[i] {
			log.Debugf("block %v not found in stakedb cache, fetching from dcrd", newChain[i])
			// Request MsgBlock from dcrd
			msgBlock, err = p.db.Client.GetBlock(&newChain[i])
			if err != nil {
				return 0, nil,
					fmt.Errorf("unable to get side chain block %v", newChain[i])
			}
		}

		// Get current winning votes from stakedb
		tpi, found := p.db.stakeDB.PoolInfo(newChain[i])
		if !found {
			return 0, nil,
				fmt.Errorf("stakedb.PoolInfo failed to return info for: %v",
					newChain[i])
		}
		winners := tpi.Winners

		// Get the chainWork
		blockHash := msgBlock.BlockHash()
		chainWork, err := p.db.GetChainWork(&blockHash)
		if err != nil {
			return 0, nil, fmt.Errorf("GetChainWork failed (%s): %v", blockHash.String(), err)
		}

		// New blocks stored this way are considered part of mainchain. They are
		// also considered valid unless invalidated by the next block
		// (invalidation of previous handled inside StoreBlock).
		isValid, isMainChain, updateExisting := true, true, true
		_, _, _, err = p.db.StoreBlock(msgBlock, winners, isValid, isMainChain,
			updateExisting, true, true, chainWork)
		if err != nil {
			return int32(p.db.Height()), p.db.Hash(),
				fmt.Errorf("error connecting block %v", newChain[i])
		}

		endHeight = int32(msgBlock.Header.Height)
		endHash = msgBlock.BlockHash()

		log.Infof("Connected block %v (height %d) from side chain.", endHash, endHeight)

		currentHeight++
	}

	log.Infof("Moved %d blocks from a side chain to the main chain in %v.",
		numBlocksmoved, time.Since(startTime))

	mainTip = int64(p.db.Height())
	if mainTip != int64(endHeight) {
		panic(fmt.Sprintf("connected block height %d not db tip height %d",
			endHeight, mainTip))
	}

	return endHeight, &endHash, nil
}

// ReorgHandler receives notification of a chain reorganization and initiates a
// corresponding reorganization of the ChainDB.
func (p *ChainMonitor) ReorgHandler() {
	defer p.wg.Done()
out:
	for {
		//keepon:
		select {
		case reorgData, ok := <-p.reorgChan:
			if !ok {
				log.Warnf("Reorg channel closed.")
				break out
			}

			p.db.InReorg = true // to avoid project fund balance computation
			newHeight, oldHeight := reorgData.NewChainHeight, reorgData.OldChainHeight
			newHash, oldHash := reorgData.NewChainHead, reorgData.OldChainHead

			log.Infof("Reorganize started. NEW head block %v at height %d.",
				newHash, newHeight)
			log.Infof("Reorganize started. OLD head block %v at height %d.",
				oldHash, oldHeight)

			// Switch to the side chain.
			stakeDBTipHeight, stakeDBTipHash, err := p.switchToSideChain(reorgData)
			if err != nil {
				log.Errorf("switchToSideChain failed: %v", err)
			}
			if stakeDBTipHeight != newHeight {
				log.Errorf("stakeDBTipHeight is %d, expected %d",
					stakeDBTipHeight, newHeight)
			}
			if *stakeDBTipHash != newHash {
				log.Errorf("stakeDBTipHash is %d, expected %d",
					stakeDBTipHash, newHash)
			}

			p.db.InReorg = false

			_ = p.db.FreshenAddressCaches(true) // async update

			reorgData.WG.Done()

		case <-p.ctx.Done():
			log.Debugf("Got quit signal. Exiting reorg notification handler.")
			break out
		}
	}
}
