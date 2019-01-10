// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrsqlite

import (
	"context"
	"fmt"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/v4/blockdata"
	"github.com/decred/dcrdata/v4/txhelpers"
)

// ChainMonitor handles change notifications from the node client
type ChainMonitor struct {
	ctx            context.Context
	db             *WiredDB
	collector      *blockdata.Collector
	wg             *sync.WaitGroup
	blockChan      chan *chainhash.Hash
	reorgChan      chan *txhelpers.ReorgData
	ConnectingLock chan struct{}
	DoneConnecting chan struct{}
}

// NewChainMonitor creates a new ChainMonitor
func (db *WiredDB) NewChainMonitor(ctx context.Context, collector *blockdata.Collector, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash, reorgChan chan *txhelpers.ReorgData) *ChainMonitor {
	return &ChainMonitor{
		ctx:            ctx,
		db:             db,
		collector:      collector,
		wg:             wg,
		blockChan:      blockChan,
		reorgChan:      reorgChan,
		ConnectingLock: make(chan struct{}, 1),
		DoneConnecting: make(chan struct{}),
	}
}

// switchToSideChain attempts to switch to a side chain by collecting data for
// each block in the side chain, and saving it as the new mainchain in sqlite.
func (p *ChainMonitor) switchToSideChain(reorgData *txhelpers.ReorgData) (int32, *chainhash.Hash, error) {
	if reorgData == nil || len(reorgData.NewChain) == 0 {
		return 0, nil, fmt.Errorf("no side chain")
	}

	newChain := reorgData.NewChain
	// newChain does not include the common ancestor.
	commonAncestorHeight := int64(reorgData.NewChainHeight) - int64(len(newChain))

	mainTip := p.db.GetBestBlockHeight()
	if mainTip != int64(reorgData.OldChainHeight) {
		log.Warnf("StakeDatabase height is %d, expected %d. Rewinding as "+
			"needed to complete reorg from ancestor at %d", mainTip,
			reorgData.OldChainHeight, commonAncestorHeight)
	}

	// Update DB tables, just overwriting any existing data. TODO(chappjc): Set
	// is_mainchain=false for the previous main chain (disconnected) blocks. For
	// now, StoreBlockSummary first does this for any main chain block being
	// added. This is unnecessary except during a reorg, but it does the job.

	// Save blocks from previous side chain that is now the main chain
	log.Infof("Saving %d new blocks from previous side chain to sqlite.", len(newChain))
	for i := range newChain {
		// Get data by block hash, which requires the stakedb's PoolInfoCache to
		// contain data for the side chain blocks already (guaranteed if stakedb
		// block-connected ntfns are always handled before these).
		blockDataSummary, stakeInfoSummaryExtended := p.collector.CollectAPITypes(&newChain[i])
		if blockDataSummary == nil || stakeInfoSummaryExtended == nil {
			log.Error("Failed to collect data for reorg.")
			continue
		}
		if err := p.db.StoreBlockSummary(blockDataSummary); err != nil {
			log.Errorf("Failed to store block summary data: %v", err)
		}
		if err := p.db.StoreStakeInfoExtended(stakeInfoSummaryExtended); err != nil {
			log.Errorf("Failed to store stake info data: %v", err)
		}
		log.Infof("Stored block %v (height %d) from side chain.",
			blockDataSummary.Hash, blockDataSummary.Height)
	}

	// Retrieve height and hash of the best block in the DB.
	hash, height, err := p.db.GetBestBlockHeightHash()
	if err != nil {
		return 0, nil, fmt.Errorf("unable to retrieve best block: %v", err)
	}

	return int32(height), &hash, err
}

// ReorgHandler receives notification of a chain reorganization and initiates a
// corresponding update of the SQL db keeping the main chain data.
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

			reorgData.WG.Done()

		case <-p.ctx.Done():
			log.Debugf("Got quit signal. Exiting reorg notification handler.")
			break out
		}
	}
}
