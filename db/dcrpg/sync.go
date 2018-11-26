// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/explorer"
	"github.com/decred/dcrdata/v3/rpcutils"
)

const (
	rescanLogBlockChunk      = 500
	InitialLoadSyncStatusMsg = "(Full Mode) Syncing stake, base and auxiliary DBs..."
	AddressesSyncStatusMsg   = "Syncing addresses table with spending info..."
)

/////////// Coordinated synchronization of base DB and auxiliary DB. ///////////
//
// In full mode, a challenge is to keep base and aux DBs syncing at the same
// height. One of the main reasons is that there is one StakeDatabase, which is
// shared between the two, and both DBs need access to it for each height.
//
// rpcutils.BlockGetter is an interface with basic accessor methods like Block
// and WaitForHash that can request current data and channels for future data,
// but do not update the state of the object. The rpcutils.MasterBlockGetter is
// an interface that embeds the regular BlockGetter, adding with functions that
// can change state and signal to the BlockGetters, such as UpdateToHeight,
// which will get the block via RPC and signal to all channels configured for
// that block.
//
// In full mode, ChainDB has the MasterBlockGetter and wiredDB has the
// BlockGetter. The way ChainDB is in charge of requesting blocks on demand from
// RPC without getting ahead of wiredDB during sync is that StakeDatabase has a
// very similar coordination mechanism (WaitForHeight).
//
// 1. In main, we make a new `rpcutils.BlockGate`, a concrete type that
//    implements `MasterBlockGetter` and thus `BlockGetter` too.  This
//    "smart client" is provided to `baseDB` (a `wiredDB`) as a `BlockGetter`,
//    and to `auxDB` (a `ChainDB`) as a `MasterBlockGetter`.
//
// 2. `baseDB` makes a channel using `BlockGetter.WaitForHeight` and starts
//    waiting for the current block to come across the channel.
//
// 3. `auxDB` makes a channel using `StakeDatabase.WaitForHeight`, which
//    instructs the shared stake DB to send notification when it connects the
//    specified block. `auxDB` does not start waiting for the signal yet.
//
// 4. `auxDB` requests that the same current block be retrieved via RPC using
//    `MasterBlockGetter.UpdateToBlock`.
//
// 5. `auxDB` immediately begins waiting for the signal that `StakeDatabase` has
//    connected the block.
//
// 6. The call to `UpdateToBlock` causes the underlying (shared) smartClient to
//    send a signal on all channels registered for that block.
//
// 7. `baseDB`gets notification on the channel and retrieves the block (which
//    the channel signaled is now available) via `BlockGetter.Block`.
//
// 8. Before connecting the block in the `StakeDatabase`, `baseDB` gets a new
//    channel for the following (i+1) block, so that when `auxDB` requests it
//    later the channel will be registered already.
//
// 9. `baseDB` connects the block in `StakeDatabase`.
//
// 10. `StakeDatabase` signals to all waiters (`auxDB`, see 5) that the stake db
//    is ready at the needed height.
//
// 11. `baseDB` finishes block data processing/storage and goes back to 2. for
//    the next block.
//
// 12. Concurrent with `baseDB` processing in 11., `auxDB` receives the
//    notification from `StakeDatabase` sent in 10. and continues block data
//    processing/storage.  When done processing, `auxDB` goes back to step 3.
//    for the next block.  As with the previous iteration, it sets the pace with
//    `UpdateToBlock`.
//
// With the above approach, (a) the DBs share a single StakeDatabase, (b) the
// DBs are in sync (tightly coupled), (c) there is ample opportunity for
// concurrent computations, and (d) the shared blockGetter (as a
// MasterBlockGetter in auxDB, and a BlockGetter in baseDB) makes it so a given
// block will only be fetched via RPC ONCE and stored for the BlockGetters that
// are waiting for the block.
////////////////////////////////////////////////////////////////////////////////

// SyncChainDBAsync is like SyncChainDB except it also takes a result channel on
// which the caller should wait to receive the result. As such, this method
// should be called as a goroutine or it will hang on send if the channel is
// unbuffered.
func (db *ChainDB) SyncChainDBAsync(ctx context.Context, res chan dbtypes.SyncResult,
	client rpcutils.MasterBlockGetter, updateAllAddresses, updateAllVotes, newIndexes bool,
	updateExplorer chan *chainhash.Hash, barLoad chan *dbtypes.ProgressBarLoad) {
	if db == nil {
		res <- dbtypes.SyncResult{
			Height: -1,
			Error:  fmt.Errorf("ChainDB (psql) disabled"),
		}
		return
	}

	height, err := db.SyncChainDB(ctx, client, updateAllAddresses,
		updateAllVotes, newIndexes, updateExplorer, barLoad)
	if err != nil {
		log.Debugf("SyncChainDB quit at height %d, err: %v", height, err)
	} else {
		log.Debugf("SyncChainDB completed at height %d.", height)
	}

	res <- dbtypes.SyncResult{
		Height: height,
		Error:  err,
	}
}

// SyncChainDB stores in the DB all blocks on the main chain available from the
// RPC client. The table indexes may be force-dropped and recreated by setting
// newIndexes to true. The quit channel is used to break the sync loop. For
// example, closing the channel on SIGINT.
func (db *ChainDB) SyncChainDB(ctx context.Context, client rpcutils.MasterBlockGetter,
	updateAllAddresses, updateAllVotes, newIndexes bool,
	updateExplorer chan *chainhash.Hash, barLoad chan *dbtypes.ProgressBarLoad) (int64, error) {
	// Note that we are doing a batch blockchain sync
	db.InBatchSync = true
	defer func() { db.InBatchSync = false }()

	// Get chain servers's best block
	nodeHeight, err := client.NodeHeight()
	if err != nil {
		return -1, fmt.Errorf("GetBestBlock failed: %v", err)
	}

	// Total and rate statistics
	var totalTxs, totalVins, totalVouts, totalAddresses int64
	var lastTxs, lastVins, lastVouts int64
	tickTime := 20 * time.Second
	ticker := time.NewTicker(tickTime)
	startTime := time.Now()
	o := sync.Once{}
	speedReporter := func() {
		ticker.Stop()
		totalElapsed := time.Since(startTime).Seconds()
		if int64(totalElapsed) == 0 {
			return
		}
		totalVoutPerSec := totalVouts / int64(totalElapsed)
		totalTxPerSec := totalTxs / int64(totalElapsed)
		if totalTxs == 0 {
			return
		}
		log.Infof("Avg. speed: %d tx/s, %d vout/s", totalTxPerSec, totalVoutPerSec)
	}
	speedReport := func() { o.Do(speedReporter) }
	defer speedReport()

	startingHeight, err := db.HeightDB()
	lastBlock := int64(startingHeight)
	if err != nil {
		if err == sql.ErrNoRows {
			lastBlock = -1
			log.Info("blocks table is empty, starting fresh.")
		} else {
			return -1, fmt.Errorf("RetrieveBestBlockHeight: %v", err)
		}
	}

	// Remove indexes/constraints before bulk import
	blocksToSync := nodeHeight - lastBlock
	reindexing := newIndexes || blocksToSync > nodeHeight/2
	if reindexing {
		log.Info("Large bulk load: Removing indexes and disabling duplicate checks.")
		err = db.DeindexAll()
		if err != nil && !strings.Contains(err.Error(), "does not exist") {
			return lastBlock, err
		}
		db.EnableDuplicateCheckOnInsert(false)
	} else {
		db.EnableDuplicateCheckOnInsert(true)
	}

	if barLoad != nil {
		// Add the various updates that should run on successful sync.
		barLoad <- &dbtypes.ProgressBarLoad{
			Msg:   InitialLoadSyncStatusMsg,
			BarID: dbtypes.InitialDBLoad,
		}
		// Addresses table sync should only run if bulk update is enabled.
		if updateAllAddresses {
			barLoad <- &dbtypes.ProgressBarLoad{
				Msg:   AddressesSyncStatusMsg,
				BarID: dbtypes.AddressesTableSync,
			}
		}
	}

	timeStart := time.Now()

	// Start rebuilding
	startHeight := lastBlock + 1
	for ib := startHeight; ib <= nodeHeight; ib++ {
		// check for quit signal
		select {
		case <-ctx.Done():
			log.Infof("Rescan cancelled at height %d.", ib)
			return ib - 1, nil
		default:
		}

		if (ib-1)%rescanLogBlockChunk == 0 || ib == startHeight {
			if ib == 0 {
				log.Infof("Scanning genesis block into auxiliary chain db.")
			} else {
				endRangeBlock := rescanLogBlockChunk * (1 + (ib-1)/rescanLogBlockChunk)
				if endRangeBlock > nodeHeight {
					endRangeBlock = nodeHeight
				}
				log.Infof("Processing blocks %d to %d...", ib, endRangeBlock)

				if barLoad != nil {
					// Full mode is definitely running so no need to check.
					timeTakenPerBlock := (time.Since(timeStart).Seconds() / float64(endRangeBlock-ib))
					barLoad <- &dbtypes.ProgressBarLoad{
						From:      ib,
						To:        nodeHeight,
						Timestamp: int64(timeTakenPerBlock * float64(nodeHeight-endRangeBlock)),
						Msg:       InitialLoadSyncStatusMsg,
						BarID:     dbtypes.InitialDBLoad,
					}
					timeStart = time.Now()
				}
			}
		}
		select {
		case <-ticker.C:
			blocksPerSec := float64(ib-lastBlock) / tickTime.Seconds()
			txPerSec := float64(totalTxs-lastTxs) / tickTime.Seconds()
			vinsPerSec := float64(totalVins-lastVins) / tickTime.Seconds()
			voutPerSec := float64(totalVouts-lastVouts) / tickTime.Seconds()
			log.Infof("(%3d blk/s,%5d tx/s,%5d vin/sec,%5d vout/s)", int64(blocksPerSec),
				int64(txPerSec), int64(vinsPerSec), int64(voutPerSec))
			lastBlock, lastTxs = ib, totalTxs
			lastVins, lastVouts = totalVins, totalVouts
		default:
		}

		// Register for notification from stakedb when it connects this block.
		waitChan := db.stakeDB.WaitForHeight(ib)

		// Get the block, making it available to stakedb, which will signal on
		// the above channel when it is done connecting it.
		block, err := client.UpdateToBlock(ib)
		if err != nil {
			log.Errorf("UpdateToBlock (%d) failed: %v", ib, err)
			return ib - 1, fmt.Errorf("UpdateToBlock (%d) failed: %v", ib, err)
		}

		// Wait for our StakeDatabase to connect the block
		var blockHash *chainhash.Hash
		select {
		case blockHash = <-waitChan:
		case <-ctx.Done():
			log.Infof("Rescan cancelled at height %d.", ib)
			return ib - 1, nil
		}
		if blockHash == nil {
			log.Errorf("stakedb says that block %d has come and gone", ib)
			return ib - 1, fmt.Errorf("stakedb says that block %d has come and gone", ib)
		}
		// If not master:
		//blockHash := <-client.WaitForHeight(ib)
		//block, err := client.Block(blockHash)
		// direct:
		//block, blockHash, err := rpcutils.GetBlock(ib, client)

		// Winning tickets from StakeDatabase, which just connected the block,
		// as signaled via the waitChan.
		tpi, ok := db.stakeDB.PoolInfo(*blockHash)
		if !ok {
			return ib - 1, fmt.Errorf("stakeDB.PoolInfo could not locate block %s", blockHash.String())
		}
		winners := tpi.Winners

		// Get the chainwork
		chainWork, err := client.GetChainWork(blockHash)
		if err != nil {
			return ib - 1, fmt.Errorf("GetChainWork failed (%s): %v", blockHash, err)
		}

		// Store data from this block in the database
		isValid, isMainchain := true, true
		// updateExisting is ignored if dupCheck=false, but true since this is
		// processing main chain blocks.
		updateExisting := true
		numVins, numVouts, numAddresses, err := db.StoreBlock(block.MsgBlock(), winners, isValid,
			isMainchain, updateExisting, !updateAllAddresses, !updateAllVotes, chainWork)
		if err != nil {
			return ib - 1, fmt.Errorf("StoreBlock failed: %v", err)
		}
		totalVins += numVins
		totalVouts += numVouts
		totalAddresses += numAddresses

		// Total transactions is the sum of regular and stake transactions
		totalTxs += int64(len(block.STransactions()) + len(block.Transactions()))

		// If updating explorer is activated, update it at intervals of 20
		if updateExplorer != nil && ib%20 == 0 &&
			explorer.SyncExplorerUpdateStatus() && !updateAllAddresses {
			log.Infof("Updating the explorer with information for block %v", ib)
			updateExplorer <- blockHash
		}

		// Update height, the end condition for the loop
		if nodeHeight, err = client.NodeHeight(); err != nil {
			return ib, fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	// After the last call to StoreBlock, synchronously update the project fund
	// and clear the general address balance cache.
	if err = db.FreshenAddressCaches(false); err != nil {
		log.Warnf("FreshenAddressCaches: %v", err)
		err = nil // not an error with sync
	}

	// Signal the end of the initial load sync.
	if barLoad != nil {
		barLoad <- &dbtypes.ProgressBarLoad{
			From:  nodeHeight,
			To:    nodeHeight,
			Msg:   InitialLoadSyncStatusMsg,
			BarID: dbtypes.InitialDBLoad,
		}
	}

	speedReport()

	if reindexing || newIndexes {
		// Duplicate transactions, vins, and vouts can end up in the tables when
		// identical transactions are included in multiple blocks. This happens
		// when a block is invalidated and the transactions are subsequently
		// re-mined in another block. Remove these before indexing.
		if err = db.DeleteDuplicates(barLoad); err != nil {
			return 0, err
		}

		// Create indexes
		if err = db.IndexAll(barLoad); err != nil {
			return nodeHeight, fmt.Errorf("IndexAll failed: %v", err)
		}
		// Only reindex addresses and tickets tables here if not doing it below
		if !updateAllAddresses {
			err = db.IndexAddressTable(barLoad)
		}
		if !updateAllVotes {
			err = db.IndexTicketsTable(barLoad)
		}
	}

	// Batch update addresses table with spending info
	if updateAllAddresses {
		// Remove existing indexes not on funding txns
		_ = db.DeindexAddressTable() // ignore errors for non-existent indexes
		log.Infof("Populating spending tx info in address table...")
		numAddresses, err := db.UpdateSpendingInfoInAllAddresses(barLoad)
		if err != nil {
			log.Errorf("UpdateSpendingInfoInAllAddresses FAILED: %v", err)
		}
		// Index addresses table
		log.Infof("Updated %d rows of address table", numAddresses)
		if err = db.IndexAddressTable(barLoad); err != nil {
			log.Errorf("IndexAddressTable FAILED: %v", err)
		}
	}

	// Batch update tickets table with spending info
	if updateAllVotes {
		// Remove indexes not on funding txns (remove on tickets table indexes)
		_ = db.DeindexTicketsTable() // ignore errors for non-existent indexes
		db.EnableDuplicateCheckOnInsert(false)
		log.Infof("Populating spending tx info in tickets table...")
		numTicketsUpdated, err := db.UpdateSpendingInfoInAllTickets()
		if err != nil {
			log.Errorf("UpdateSpendingInfoInAllTickets FAILED: %v", err)
		}
		// Index tickets table
		log.Infof("Updated %d rows of address table", numTicketsUpdated)
		if err = db.IndexTicketsTable(barLoad); err != nil {
			log.Errorf("IndexTicketsTable FAILED: %v", err)
		}
	}

	// After sync and indexing, must use upsert statement, which checks for
	// duplicate entries and updates instead of throwing and error and panicing.
	db.EnableDuplicateCheckOnInsert(true)

	if barLoad != nil {
		barID := dbtypes.InitialDBLoad
		if updateAllAddresses {
			barID = dbtypes.AddressesTableSync
		}
		barLoad <- &dbtypes.ProgressBarLoad{BarID: barID, Subtitle: "sync complete"}
	}

	log.Infof("Sync finished at height %d. Delta: %d blocks, %d transactions, %d ins, %d outs, %d addresses",
		nodeHeight, nodeHeight-startHeight+1, totalTxs, totalVins, totalVouts, totalAddresses)

	return nodeHeight, err
}
