// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrpg

import (
	"context"
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v8/txhelpers"
)

// ChainMonitor responds to block connection and chain reorganization.
type ChainMonitor struct {
	ctx context.Context
	db  *ChainDB
}

// NewChainMonitor creates a new ChainMonitor.
func (pgb *ChainDB) NewChainMonitor(ctx context.Context) *ChainMonitor {
	if pgb == nil {
		return nil
	}
	return &ChainMonitor{
		ctx: ctx, // usage is TODO
		db:  pgb,
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
		log.Infof("switchToSideChain completed in %v", time.Since(start))
	}(time.Now())

	newChain := reorgData.NewChain
	// newChain does not include the common ancestor.
	commonAncestorHeight := int64(reorgData.NewChainHeight) - int64(len(newChain))

	mainTip := p.db.bestBlock.Height()
	if mainTip != int64(reorgData.OldChainHeight) {
		log.Warnf("StakeDatabase height is %d, expected %d. Rewinding as "+
			"needed to complete reorg from ancestor at %d", mainTip,
			reorgData.OldChainHeight, commonAncestorHeight)
	}

	// Flag blocks from mainRoot+1 to mainTip as is_mainchain=false
	startTime := time.Now()
	mainRoot := reorgData.CommonAncestor.String()
	log.Infof("Moving %d blocks to side chain...", mainTip-commonAncestorHeight)
	newMainRoot, numBlocksmoved := p.db.TipToSideChain(mainRoot)
	if mainRoot != newMainRoot {
		return 0, nil, fmt.Errorf("failed to flag blocks as side chain")
	}
	log.Infof("Moved %d blocks from the main chain to a side chain in %v.",
		numBlocksmoved, time.Since(startTime))

	// Verify the tip is now the previous common ancestor
	mainTip = p.db.bestBlock.Height()
	if mainTip != commonAncestorHeight {
		panic(fmt.Sprintf("disconnect blocks failed: tip height %d, expected %d",
			mainTip, commonAncestorHeight))
	}

	// Connect blocks in side chain onto main chain
	log.Debugf("Connecting %d new main chain blocks", len(newChain))
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
			var err error
			msgBlock, err = p.db.Client.GetBlock(context.TODO(), &newChain[i])
			if err != nil {
				return 0, nil,
					fmt.Errorf("unable to get side chain block %v", newChain[i])
			}
		}

		// Get the chainWork
		blockHash := msgBlock.BlockHash()
		chainWork, err := p.db.GetChainWork(&blockHash)
		if err != nil {
			return 0, nil, fmt.Errorf("GetChainWork failed (%s): %v", blockHash.String(), err)
		}

		// New blocks stored this way are considered part of mainchain. They are
		// also considered valid unless invalidated by the next block
		// (invalidation of previous handled inside StoreBlock).
		isValid, isMainChain := true, true
		updateExistingRecords, updateAddressesSpendingInfo := true, true
		_, _, _, err = p.db.StoreBlock(msgBlock, isValid, isMainChain,
			updateExistingRecords, updateAddressesSpendingInfo, chainWork)
		if err != nil {
			return int32(p.db.bestBlock.Height()), p.db.bestBlock.Hash(),
				fmt.Errorf("error connecting block %v", newChain[i])
		}

		endHeight = int32(msgBlock.Header.Height)
		endHash = blockHash

		log.Infof("Connected block %v (height %d) from side chain.", endHash, endHeight)

		currentHeight++
	}

	log.Infof("Moved %d blocks from a side chain to the main chain in %v.",
		numBlocksmoved, time.Since(startTime))

	mainTip = p.db.bestBlock.Height()
	if mainTip != int64(endHeight) {
		panic(fmt.Sprintf("connected block height %d not db tip height %d",
			endHeight, mainTip))
	}

	return endHeight, &endHash, nil
}

// ReorgHandler processes a blockchain reorganization and initiates a
// corresponding reorganization of the ChainDB. ReorgHandler satisfies
// notification.ReorgHandler, and is registered as a handler in main.go.
func (p *ChainMonitor) ReorgHandler(reorg *txhelpers.ReorgData) error {
	p.db.InReorg = true // to avoid project fund balance computation
	newHeight, oldHeight := reorg.NewChainHeight, reorg.OldChainHeight
	newHash, oldHash := reorg.NewChainHead, reorg.OldChainHead

	log.Infof("Reorganize started. NEW head block %v at height %d.",
		newHash, newHeight)
	log.Infof("Reorganize started. OLD head block %v at height %d.",
		oldHash, oldHeight)

	// Switch to the side chain.
	stakeDBTipHeight, stakeDBTipHash, err := p.switchToSideChain(reorg)
	if err != nil {
		return fmt.Errorf("switchToSideChain failed: %w", err)
	}
	if stakeDBTipHeight != newHeight {
		return fmt.Errorf("stakeDBTipHeight is %d, expected %d", stakeDBTipHeight, newHeight)
	}
	if *stakeDBTipHash != newHash {
		return fmt.Errorf("stakeDBTipHash is %d, expected %d", stakeDBTipHash, newHash)
	}

	p.db.InReorg = false

	// Update project fund in cache, but clear NO address cache data since
	// switchToSideChain maintains the cache via TipToSideChain and StoreBlock.
	go func() {
		if err := p.db.updateProjectFundCache(); err != nil {
			log.Errorf("Failed to update project fund data in cache: %v", err)
		}
	}()

	return err
}
