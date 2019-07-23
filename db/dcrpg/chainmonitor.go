// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrpg

import (
	"context"
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/txhelpers/v3"
)

// ChainMonitor responds to block connection and chain reorganization.
type ChainMonitor struct {
	ctx            context.Context
	db             *ChainDBRPC
	ConnectingLock chan struct{}
	DoneConnecting chan struct{}
}

// NewChainMonitor creates a new ChainMonitor.
func (pgb *ChainDBRPC) NewChainMonitor(ctx context.Context) *ChainMonitor {
	if pgb == nil {
		return nil
	}
	return &ChainMonitor{
		ctx:            ctx,
		db:             pgb,
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
	newMainRoot, numBlocksmoved, err := p.db.TipToSideChain(mainRoot)
	if err != nil || mainRoot != newMainRoot {
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
			return int32(p.db.bestBlock.Height()), p.db.bestBlock.Hash(),
				fmt.Errorf("error connecting block %v", newChain[i])
		}

		endHeight = int32(msgBlock.Header.Height)
		endHash = msgBlock.BlockHash()

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
func (p *ChainMonitor) ReorgHandler(reorg *txhelpers.ReorgData) (err error) {
	p.db.InReorg = true // to avoid project fund balance computation
	newHeight, oldHeight := reorg.NewChainHeight, reorg.OldChainHeight
	newHash, oldHash := reorg.NewChainHead, reorg.OldChainHead

	log.Infof("Reorganize started. NEW head block %v at height %d.",
		newHash, newHeight)
	log.Infof("Reorganize started. OLD head block %v at height %d.",
		oldHash, oldHeight)

	appendError := func(e error) error {
		if err == nil {
			return e
		}
		return fmt.Errorf("%s. %s", err.Error(), e.Error())
	}

	// Switch to the side chain.
	stakeDBTipHeight, stakeDBTipHash, err := p.switchToSideChain(reorg)
	if err != nil {
		appendError(fmt.Errorf("switchToSideChain failed: %v", err))
	}
	if stakeDBTipHeight != newHeight {
		appendError(fmt.Errorf("stakeDBTipHeight is %d, expected %d",
			stakeDBTipHeight, newHeight))
	}
	if *stakeDBTipHash != newHash {
		appendError(fmt.Errorf("stakeDBTipHash is %d, expected %d", stakeDBTipHash,
			newHash))
	}

	p.db.InReorg = false
	// Freshen project fund balance and clear ALL address cache data.
	_ = p.db.FreshenAddressCaches(true, nil) // async update

	return err
}
