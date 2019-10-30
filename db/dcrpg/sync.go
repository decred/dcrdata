// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/db/dbtypes/v2"
	"github.com/decred/dcrdata/rpcutils/v3"
)

const (
	quickStatsTarget         = 250
	deepStatsTarget          = 1000
	rescanLogBlockChunk      = 500
	initialLoadSyncStatusMsg = "Syncing stake, base and auxiliary DBs..."
	addressesSyncStatusMsg   = "Syncing addresses table with spending info..."
)

/////////// Coordinated synchronization of base DB and auxiliary DB. ///////////
//
// A challenge is to keep base and aux DBs syncing at the same height. One of
// the main reasons is that there is one StakeDatabase, which is shared between
// the two, and both DBs need access to it for each height.
//
// rpcutils.BlockGetter is an interface with basic accessor methods like Block
// and WaitForHash that can request current data and channels for future data,
// but do not update the state of the object. The rpcutils.MasterBlockGetter is
// an interface that embeds the regular BlockGetter, adding with functions that
// can change state and signal to the BlockGetters, such as UpdateToHeight,
// which will get the block via RPC and signal to all channels configured for
// that block.
//
// ChainDB has the MasterBlockGetter and WiredDB has the BlockGetter. The way
// ChainDB is in charge of requesting blocks on demand from RPC without getting
// ahead of WiredDB during sync is that StakeDatabase has a very similar
// coordination mechanism (WaitForHeight).
//
// 1. In main, we make a new `rpcutils.BlockGate`, a concrete type that
//    implements `MasterBlockGetter` and thus `BlockGetter` too.  This
//    "smart client" is provided to `baseDB` (a `WiredDB`) as a `BlockGetter`,
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
func (pgb *ChainDB) SyncChainDBAsync(ctx context.Context, res chan dbtypes.SyncResult,
	client rpcutils.MasterBlockGetter, updateAllAddresses, newIndexes bool,
	updateExplorer chan *chainhash.Hash, barLoad chan *dbtypes.ProgressBarLoad) {
	if pgb == nil {
		res <- dbtypes.SyncResult{
			Height: -1,
			Error:  fmt.Errorf("ChainDB (psql) disabled"),
		}
		return
	}

	height, err := pgb.SyncChainDB(ctx, client, updateAllAddresses, newIndexes,
		updateExplorer, barLoad)
	if err != nil {
		log.Errorf("SyncChainDB quit at height %d, err: %v", height, err)
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
func (pgb *ChainDB) SyncChainDB(ctx context.Context, client rpcutils.MasterBlockGetter,
	updateAllAddresses, newIndexes bool, updateExplorer chan *chainhash.Hash,
	barLoad chan *dbtypes.ProgressBarLoad) (int64, error) {
	// Note that we are doing a batch blockchain sync.
	pgb.InBatchSync = true
	defer func() { pgb.InBatchSync = false }()

	// Get the chain servers's best block.
	nodeHeight, err := client.NodeHeight()
	if err != nil {
		return -1, fmt.Errorf("GetBestBlock failed: %v", err)
	}

	// Retrieve the best block in the database from the meta table.
	lastBlock, err := pgb.HeightDB()
	if err != nil {
		return -1, fmt.Errorf("RetrieveBestBlockHeight: %v", err)
	}
	if lastBlock == -1 {
		log.Info("Tables are empty, starting fresh.")
	}

	// Remove indexes/constraints before an initial sync or when explicitly
	// requested to reindex and update spending information in the addresses
	// table.
	reindexing := newIndexes || lastBlock == -1

	// See if initial sync (initial block download) was previously completed.
	ibdComplete, err := IBDComplete(pgb.db)
	if err != nil {
		return lastBlock, fmt.Errorf("IBDComplete failed: %v", err)
	}

	// Check and report heights of the DBs. dbHeight is the lowest of the
	// heights, and may be -1 with an empty DB.
	stakeDBHeight := int64(pgb.stakeDB.Height())
	if lastBlock < -1 {
		panic("invalid starting height")
	}

	log.Info("Current best block (dcrd):       ", nodeHeight)
	log.Info("Current best block (primary db): ", lastBlock)
	log.Info("Current best block (stakedb):    ", stakeDBHeight)

	// Attempt to rewind stake database, if needed, forcing it to the lowest DB
	// height (or 0 if the lowest DB height is -1).
	if stakeDBHeight > lastBlock && stakeDBHeight > 0 {
		if lastBlock < 0 || stakeDBHeight > 2*lastBlock {
			return -1, fmt.Errorf("delete stake db (ffldb_stake) and try again")
		}
		log.Infof("Rewinding stake node from %d to %d", stakeDBHeight, lastBlock)
		// Rewind best node in ticket DB to larger of lowest DB height or zero.
		stakeDBHeight, err = pgb.RewindStakeDB(ctx, lastBlock)
		if err != nil {
			return lastBlock, fmt.Errorf("RewindStakeDB failed: %v", err)
		}
	}

	// When IBD is not yet completed, force reindexing and update of full
	// spending info in addresses table after block sync.
	if !ibdComplete {
		if lastBlock > -1 {
			log.Warnf("Detected that initial sync was previously started but not completed!")
		}
		if !reindexing {
			reindexing = true
			log.Warnf("Forcing table reindexing.")
		}
		if !updateAllAddresses {
			updateAllAddresses = true
			log.Warnf("Forcing full update of spending information in addresses table.")
		}
	}

	if reindexing {
		// Remove any existing indexes.
		log.Info("Large bulk load: Removing indexes and disabling duplicate checks.")
		err = pgb.DeindexAll()
		if err != nil && !strings.Contains(err.Error(), "does not exist") {
			return lastBlock, err
		}

		// Disable duplicate checks on insert queries since the unique indexes
		// that enforce the constraints will not exist.
		pgb.EnableDuplicateCheckOnInsert(false)

		// Syncing blocks without indexes requires a UTXO cache to avoid
		// extremely expensive queries. Warm the UTXO cache if resuming an
		// interrupted initial sync.
		blocksToSync := nodeHeight - lastBlock
		if lastBlock > 0 && (blocksToSync > 50 || pgb.cockroach) {
			if pgb.cockroach {
				log.Infof("Removing duplicate vins prior to indexing.")
				N, err := pgb.DeleteDuplicateVinsCockroach()
				if err != nil {
					return -1, fmt.Errorf("failed to remove duplicate vins: %v", err)
				}
				log.Infof("Removed %d duplicate vins rows.", N)
				log.Infof("Indexing vins table on vins for CockroachDB to load UTXO set.")
				if err = IndexVinTableOnVins(pgb.db); err != nil {
					return -1, fmt.Errorf("failed to index vins on vins: %v", err)
				}
				log.Infof("Indexing vins table on prevouts for CockroachDB to load UTXO set.")
				if err = IndexVinTableOnPrevOuts(pgb.db); err != nil {
					return -1, fmt.Errorf("failed to index vins on prevouts: %v", err)
				}

				log.Infof("Removing duplicate vouts prior to indexing.")
				N, err = pgb.DeleteDuplicateVoutsCockroach()
				if err != nil {
					return -1, fmt.Errorf("failed to remove duplicate vouts: %v", err)
				}
				log.Infof("Removed %d duplicate vouts rows.", N)
				log.Infof("Indexing vouts table on tx hash and idx for CockroachDB to load UTXO set.")
				if err = IndexVoutTableOnTxHashIdx(pgb.db); err != nil {
					return -1, fmt.Errorf("failed to index vouts on tx hash and idx: %v", err)
				}
			}
			log.Infof("Collecting all UTXO data prior to height %d...", lastBlock+1)
			utxos, err := RetrieveUTXOs(ctx, pgb.db)
			if err != nil {
				return -1, fmt.Errorf("RetrieveUTXOs: %v", err)
			}
			if pgb.cockroach {
				err = pgb.DeindexAll()
				if err != nil && !strings.Contains(err.Error(), "does not exist") {
					return lastBlock, err
				}
			}
			log.Infof("Pre-warming UTXO cache with %d UTXOs...", len(utxos))
			pgb.InitUtxoCache(utxos)
			log.Infof("UTXO cache is ready.")
		}
	} else {
		// When the unique indexes exist, inserts should check for conflicts
		// with the tables' constraints.
		pgb.EnableDuplicateCheckOnInsert(true)
	}

	// When reindexing or adding a large amount of data, ANALYZE tables.
	requireAnalyze := reindexing || nodeHeight-lastBlock > 10000

	// If reindexing or batch table data updates are required, set the
	// ibd_complete flag to false if it is not already false.
	if ibdComplete && (reindexing || updateAllAddresses) {
		// Set meta.ibd_complete = FALSE.
		if err = SetIBDComplete(pgb.db, false); err != nil {
			return nodeHeight, fmt.Errorf("failed to set meta.ibd_complete: %v", err)
		}
	}

	// Safely send sync status updates on barLoad channel, and set the channel
	// to nil if the buffer is full.
	sendProgressUpdate := func(p *dbtypes.ProgressBarLoad) {
		if barLoad == nil {
			return
		}
		select {
		case barLoad <- p:
		default:
			log.Debugf("(*ChainDB).SyncChainDB: barLoad chan closed or full. Halting sync progress updates.")
			barLoad = nil
		}
	}

	// Safely send new block hash on updateExplorer channel, and set the channel
	// to nil if the buffer is full.
	sendPageData := func(hash *chainhash.Hash) {
		if updateExplorer == nil {
			return
		}
		select {
		case updateExplorer <- hash:
		default:
			log.Debugf("(*ChainDB).SyncChainDB: updateExplorer chan closed or full. Halting explorer updates.")
			updateExplorer = nil
		}
	}

	// Add the various updates that should run on successful sync.
	sendProgressUpdate(&dbtypes.ProgressBarLoad{
		Msg:   initialLoadSyncStatusMsg,
		BarID: dbtypes.InitialDBLoad,
	})
	// Addresses table sync should only run if bulk update is enabled.
	if updateAllAddresses {
		sendProgressUpdate(&dbtypes.ProgressBarLoad{
			Msg:   addressesSyncStatusMsg,
			BarID: dbtypes.AddressesTableSync,
		})
	}

	// Total and rate statistics
	var totalTxs, totalVins, totalVouts, totalAddresses int64
	var lastTxs, lastVins, lastVouts int64
	tickTime := 20 * time.Second
	ticker := time.NewTicker(tickTime)
	startTime := time.Now()
	startHeight := lastBlock + 1
	o := sync.Once{}
	speedReporter := func() {
		ticker.Stop()
		timeElapsed := time.Since(startTime)
		secsElapsed := timeElapsed.Seconds()
		if int64(secsElapsed) == 0 {
			return
		}
		totalVoutPerSec := totalVouts / int64(secsElapsed)
		totalTxPerSec := totalTxs / int64(secsElapsed)
		if totalTxs == 0 {
			return
		}
		log.Infof("Avg. speed: %d tx/s, %d vout/s", totalTxPerSec, totalVoutPerSec)
		syncedBlocks := float64(nodeHeight - startHeight + 1)
		log.Infof("Total sync: %.2f minutes (%.2f blocks/s)", timeElapsed.Minutes(), syncedBlocks/timeElapsed.Seconds())
	}
	speedReport := func() { o.Do(speedReporter) }
	defer speedReport()

	lastProgressUpdateTime := startTime

	// Start syncing blocks.
	for ib := startHeight; ib <= nodeHeight; ib++ {
		// Check for quit signal.
		select {
		case <-ctx.Done():
			log.Infof("Rescan cancelled at height %d.", ib)
			return ib - 1, nil
		default:
		}

		// Progress logging
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
					timeTakenPerBlock := (time.Since(lastProgressUpdateTime).Seconds() /
						float64(endRangeBlock-ib))
					sendProgressUpdate(&dbtypes.ProgressBarLoad{
						From:      ib,
						To:        nodeHeight,
						Timestamp: int64(timeTakenPerBlock * float64(nodeHeight-endRangeBlock)),
						Msg:       initialLoadSyncStatusMsg,
						BarID:     dbtypes.InitialDBLoad,
					})
					lastProgressUpdateTime = time.Now()
				}
			}
		}

		// Speed report
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

		// Get the block, making it available to stakedb, which will signal on
		// the above channel when it is done connecting it.
		block, err := client.UpdateToBlock(ib)
		if err != nil {
			log.Errorf("UpdateToBlock (%d) failed: %v", ib, err)
			return ib - 1, fmt.Errorf("UpdateToBlock (%d) failed: %v", ib, err)
		}

		// Advance stakedb height, which should always be less than or equal to
		// PSQL height. stakedb always has genesis, as enforced by the rewinding
		// code in this function.
		if ib > stakeDBHeight {
			if ib != int64(pgb.stakeDB.Height()+1) {
				panic(fmt.Sprintf("about to connect the wrong block: %d, %d", ib, pgb.stakeDB.Height()))
			}
			if err = pgb.stakeDB.ConnectBlock(block); err != nil {
				return ib - 1, pgb.supplementUnknownTicketError(err)
			}
		}
		stakeDBHeight = int64(pgb.stakeDB.Height()) // i
		blockHash := block.Hash()

		// Get the chainwork
		chainWork, err := client.GetChainWork(blockHash)
		if err != nil {
			return ib - 1, fmt.Errorf("GetChainWork failed (%s): %v", blockHash, err)
		}

		// Store data from this block in the database.
		isValid, isMainchain := true, true
		// updateExisting is ignored if dupCheck=false, but set it to true since
		// SyncChainDB is processing main chain blocks.
		updateExisting := true
		numVins, numVouts, numAddresses, err := pgb.StoreBlock(block.MsgBlock(),
			isValid, isMainchain, updateExisting, !updateAllAddresses, true, chainWork)
		if err != nil {
			return ib - 1, fmt.Errorf("StoreBlock failed: %v", err)
		}
		totalVins += numVins
		totalVouts += numVouts
		totalAddresses += numAddresses

		// Total transactions is the sum of regular and stake transactions.
		totalTxs += int64(len(block.STransactions()) + len(block.Transactions()))

		// Update explorer pages at intervals of 20 blocks if the update channel
		// is active (non-nil and not closed).
		if ib%20 == 0 && !updateAllAddresses {
			if updateExplorer != nil {
				log.Infof("Updating the explorer with information for block %v", ib)
				sendPageData(blockHash)
			}
		}

		// Update node height, the end condition for the loop.
		if nodeHeight, err = client.NodeHeight(); err != nil {
			return ib, fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	// Final speed report
	speedReport()

	// Signal the final height to any heightClients.
	pgb.SignalHeight(uint32(nodeHeight))

	// After the last call to StoreBlock, synchronously update the project fund
	// and clear the general address balance cache.
	if err = pgb.FreshenAddressCaches(false, nil); err != nil {
		log.Warnf("FreshenAddressCaches: %v", err)
		err = nil // not an error with sync
	}

	// Signal the end of the initial load sync.
	sendProgressUpdate(&dbtypes.ProgressBarLoad{
		From:  nodeHeight,
		To:    nodeHeight,
		Msg:   initialLoadSyncStatusMsg,
		BarID: dbtypes.InitialDBLoad,
	})

	// Index and analyze tables.
	var analyzed bool
	if reindexing {
		// To build indexes, there must NOT be duplicate rows in terms of the
		// constraints defined by the unique indexes. Duplicate transactions,
		// vins, and vouts can end up in the tables when identical transactions
		// are included in multiple blocks. This happens when a block is
		// invalidated and the transactions are subsequently re-mined in another
		// block. Remove these before indexing.
		log.Infof("Finding and removing duplicate table rows before indexing...")
		if err = pgb.DeleteDuplicates(barLoad); err != nil {
			return 0, err
		}

		// Create all indexes.
		if err = pgb.IndexAll(barLoad); err != nil {
			return nodeHeight, fmt.Errorf("IndexAll failed: %v", err)
		}

		// Only reindex addresses table here if not doing it below.
		if !updateAllAddresses {
			if err = pgb.IndexAddressTable(barLoad); err != nil {
				return nodeHeight, fmt.Errorf("IndexAddressTable failed: %v", err)
			}
		}

		// Tickets table index is not included in IndexAll.
		if err = pgb.IndexTicketsTable(barLoad); err != nil {
			return nodeHeight, fmt.Errorf("IndexTicketsTable failed: %v", err)
		}

		// Deep ANALYZE all tables.
		log.Infof("Performing an ANALYZE(%d) on all tables...", deepStatsTarget)
		if err = AnalyzeAllTables(pgb.db, deepStatsTarget); err != nil {
			return nodeHeight, fmt.Errorf("failed to ANALYZE tables: %v", err)
		}
		analyzed = true
	}

	// Batch update addresses table with spending info.
	if updateAllAddresses {
		// Analyze vins and table first.
		if !analyzed {
			log.Infof("Performing an ANALYZE(%d) on vins table...", deepStatsTarget)
			if err = AnalyzeTable(pgb.db, "vins", deepStatsTarget); err != nil {
				return nodeHeight, fmt.Errorf("failed to ANALYZE vins table: %v", err)
			}
		}

		// Remove existing indexes not on funding txns
		_ = pgb.DeindexAddressTable() // ignore errors for non-existent indexes
		log.Infof("Populating spending tx info in address table...")
		numAddresses, err := pgb.UpdateSpendingInfoInAllAddresses(barLoad)
		if err != nil {
			log.Errorf("UpdateSpendingInfoInAllAddresses FAILED: %v", err)
		}
		// Index addresses table
		log.Infof("Updated %d rows of address table", numAddresses)
		if err = pgb.IndexAddressTable(barLoad); err != nil {
			log.Errorf("IndexAddressTable FAILED: %v", err)
		}

		// Deep ANALYZE the newly indexed addresses table.
		log.Infof("Performing an ANALYZE(%d) on addresses table...", deepStatsTarget)
		if err = AnalyzeTable(pgb.db, "addresses", deepStatsTarget); err != nil {
			return nodeHeight, fmt.Errorf("failed to ANALYZE addresses table: %v", err)
		}
	}

	// Quickly ANALYZE all tables if not already done after indexing.
	if !analyzed && requireAnalyze {
		// Analyze all tables.
		log.Infof("Performing an ANALYZE(%d) on all tables...", quickStatsTarget)
		if err = AnalyzeAllTables(pgb.db, quickStatsTarget); err != nil {
			return nodeHeight, fmt.Errorf("failed to ANALYZE tables: %v", err)
		}
	}

	// After sync and indexing, must use upsert statement, which checks for
	// duplicate entries and updates instead of throwing and error and panicing.
	pgb.EnableDuplicateCheckOnInsert(true)

	// Set meta.ibd_complete = TRUE.
	if err = SetIBDComplete(pgb.db, true); err != nil {
		return nodeHeight, fmt.Errorf("failed to set meta.ibd_complete: %v", err)
	}

	if barLoad != nil {
		barID := dbtypes.InitialDBLoad
		if updateAllAddresses {
			barID = dbtypes.AddressesTableSync
		}
		sendProgressUpdate(&dbtypes.ProgressBarLoad{
			BarID:    barID,
			Subtitle: "sync complete",
		})
	}

	log.Infof("Sync finished at height %d. Delta: %d blocks, %d transactions, %d ins, %d outs, %d addresses",
		nodeHeight, nodeHeight-startHeight+1, totalTxs, totalVins, totalVouts, totalAddresses)

	return nodeHeight, err
}

func parseUnknownTicketError(err error) (hash *chainhash.Hash) {
	// Look for the dreaded ticket database error.
	re := regexp.MustCompile(`unknown ticket (\w*) spent in block`)
	matches := re.FindStringSubmatch(err.Error())
	var unknownTicket string
	if len(matches) <= 1 {
		// Unable to parse the error as unknown ticket message.
		return
	}
	unknownTicket = matches[1]
	ticketHash, err1 := chainhash.NewHashFromStr(unknownTicket)
	if err1 != nil {
		return
	}
	return ticketHash
}

// supplementUnknownTicketError checks the passed error for the "unknown ticket
// [hash] spent in block" message, and supplements matching errors with the
// block height of the ticket and switches to help recovery.
func (pgb *ChainDB) supplementUnknownTicketError(err error) error {
	ticketHash := parseUnknownTicketError(err)
	if ticketHash == nil {
		return err
	}
	txraw, err1 := pgb.Client.GetRawTransactionVerbose(ticketHash)
	if err1 != nil {
		return err
	}
	badTxBlock := txraw.BlockHeight
	sDBHeight := int64(pgb.stakeDB.Height())
	numToPurge := sDBHeight - badTxBlock + 1
	return fmt.Errorf("%v\n\t**** Unknown ticket was mined in block %d. "+
		"Try \"--purge-n-blocks=%d to recover. ****",
		err, badTxBlock, numToPurge)
}
