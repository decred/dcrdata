// Copyright (c) 2018-2021, The Decred developers
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
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/rpcutils"
)

const (
	quickStatsTarget         = 250
	deepStatsTarget          = 600
	rescanLogBlockChunk      = 500
	initialLoadSyncStatusMsg = "Syncing stake and chain DBs..."
	addressesSyncStatusMsg   = "Syncing addresses table with spending info..."
)

// SyncChainDB stores in the DB all blocks on the main chain available from the
// RPC client. The table indexes may be force-dropped and recreated by setting
// newIndexes to true. The quit channel is used to break the sync loop. For
// example, closing the channel on SIGINT.
func (pgb *ChainDB) SyncChainDB(ctx context.Context, client rpcutils.BlockFetcher,
	updateAllAddresses, newIndexes bool, updateExplorer chan *chainhash.Hash,
	barLoad chan *dbtypes.ProgressBarLoad) (int64, error) {
	// Note that we are doing a batch blockchain sync.
	pgb.InBatchSync = true
	defer func() { pgb.InBatchSync = false }()

	// Get the chain servers's best block.
	_, nodeHeight, err := client.GetBestBlock(ctx)
	if err != nil {
		return -1, fmt.Errorf("GetBestBlock failed: %w", err)
	}

	// Retrieve the best block in the database from the meta table.
	lastBlock, err := pgb.HeightDB()
	if err != nil {
		return -1, fmt.Errorf("RetrieveBestBlockHeight: %w", err)
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
		return lastBlock, fmt.Errorf("IBDComplete failed: %w", err)
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
			return lastBlock, fmt.Errorf("RewindStakeDB failed: %w", err)
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

	if pgb.utxoCache.Size() == 0 { // entries at any height implies it's warmed by previous sync
		log.Infof("Collecting all UTXO data prior to height %d...", lastBlock+1)
		utxos, err := RetrieveUTXOs(ctx, pgb.db)
		if err != nil {
			return -1, fmt.Errorf("RetrieveUTXOs: %w", err)
		}

		log.Infof("Pre-warming UTXO cache with %d UTXOs...", len(utxos))
		pgb.InitUtxoCache(utxos)
		log.Infof("UTXO cache is ready.")
	}

	if reindexing {
		// Remove any existing indexes.
		log.Info("Large bulk load: Removing indexes and disabling duplicate checks.")
		err = pgb.DeindexAll()
		if err != nil && !strings.Contains(err.Error(), "does not exist") {
			return lastBlock, err
		}

		// Create the temporary index on addresses(tx_vin_vout_row_id) that
		// prevents the stake disapproval updates to a block to cause massive
		// slowdown during initial sync without an index on tx_vin_vout_row_id.
		log.Infof("Creating temporary index on addresses(tx_vin_vout_row_id).")
		_, err = pgb.db.Exec(`CREATE INDEX IF NOT EXISTS idx_addresses_vinvout_id_tmp ` +
			`ON addresses(tx_vin_vout_row_id)`)
		if err != nil {
			return lastBlock, err
		}

		// Disable duplicate checks on insert queries since the unique indexes
		// that enforce the constraints will not exist.
		pgb.EnableDuplicateCheckOnInsert(false)
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
			return nodeHeight, fmt.Errorf("failed to set meta.ibd_complete: %w", err)
		}
	}

	stages := 1 // without indexing and spend updates, just block import
	if reindexing {
		stages += 3 // duplicate data removal, indexing, deep analyze
	} else if requireAnalyze {
		stages++ // not reindexing, just quick analyzing because far behind
	}
	if updateAllAddresses {
		stages++ // addresses table spending info update
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
			log.Debugf("(*ChainDB).SyncChainDB: barLoad chan full. Halting sync progress updates.")
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
			log.Debugf("(*ChainDB).SyncChainDB: updateExplorer chan full. Halting explorer updates.")
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
		syncedBlocks := nodeHeight - startHeight + 1
		log.Infof("Block import elapsed: %.2f minutes, %d blocks (%.2f blocks/s)",
			timeElapsed.Minutes(), syncedBlocks, float64(syncedBlocks)/timeElapsed.Seconds())
	}
	speedReport := func() { o.Do(speedReporter) }
	defer speedReport()

	lastProgressUpdateTime := startTime

	stage := 1
	log.Infof("Beginning SYNC STAGE %d of %d (block data import).", stage, stages)

	importBlocks := func(start int64) (int64, error) {
		for ib := start; ib <= nodeHeight; ib++ {
			// Check for quit signal.
			select {
			case <-ctx.Done():
				return ib - 1, fmt.Errorf("sync cancelled at height %d", ib)
			default:
			}

			// Progress logging
			if (ib-1)%rescanLogBlockChunk == 0 || ib == startHeight {
				if ib == 0 {
					log.Infof("Scanning genesis block into chain db.")
				} else {
					_, nodeHeight, err = client.GetBestBlock(ctx)
					if err != nil {
						return ib, fmt.Errorf("GetBestBlock failed: %w", err)
					}
					endRangeBlock := rescanLogBlockChunk * (1 + (ib-1)/rescanLogBlockChunk)
					if endRangeBlock > nodeHeight {
						endRangeBlock = nodeHeight
					}
					log.Infof("Processing blocks %d to %d...", ib, endRangeBlock)

					if barLoad != nil {
						timeTakenPerBlock := time.Since(lastProgressUpdateTime).Seconds() /
							float64(endRangeBlock-ib)
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
			block, blockHash, err := rpcutils.GetBlock(ib, client)
			if err != nil {
				log.Errorf("UpdateToBlock (%d) failed: %v", ib, err)
				return ib - 1, fmt.Errorf("UpdateToBlock (%d) failed: %w", ib, err)
			}

			// Advance stakedb height, which should always be less than or equal to
			// PSQL height. stakedb always has genesis, as enforced by the rewinding
			// code in this function.
			if ib > stakeDBHeight {
				behind := ib - stakeDBHeight
				if behind != 1 {
					panic(fmt.Sprintf("About to connect the wrong block: %d, %d\n"+
						"The stake database is corrupted. "+
						"Restart with --purge-n-blocks=%d to recover.",
						ib, stakeDBHeight, 2*behind))
				}
				if err = pgb.stakeDB.ConnectBlock(block); err != nil {
					return ib - 1, pgb.supplementUnknownTicketError(err)
				}
			}
			stakeDBHeight = int64(pgb.stakeDB.Height()) // i

			// Get the chainwork
			chainWork, err := rpcutils.GetChainWork(client, blockHash)
			if err != nil {
				return ib - 1, fmt.Errorf("GetChainWork failed (%s): %w", blockHash, err)
			}

			// Store data from this block in the database.
			isValid, isMainchain := true, true
			// updateExisting is ignored if dupCheck=false, but set it to true since
			// SyncChainDB is processing main chain blocks.
			updateExisting := true
			numVins, numVouts, numAddresses, err := pgb.StoreBlock(block.MsgBlock(),
				isValid, isMainchain, updateExisting, !updateAllAddresses, chainWork)
			if err != nil {
				return ib - 1, fmt.Errorf("StoreBlock failed: %w", err)
			}
			totalVins += numVins
			totalVouts += numVouts
			totalAddresses += numAddresses

			// Total transactions is the sum of regular and stake transactions.
			totalTxs += int64(len(block.STransactions()) + len(block.Transactions()))

			// Update explorer pages at intervals of 20 blocks if the update channel
			// is active (non-nil and not closed).
			if !updateAllAddresses && ib%20 == 0 {
				log.Tracef("Updating the explorer with information for block %v", ib)
				sendPageData(blockHash)
			}

			// Update node height, the end condition for the loop.
			if ib == nodeHeight {
				_, nodeHeight, err = client.GetBestBlock(ctx)
				if err != nil {
					return ib, fmt.Errorf("GetBestBlock failed: %w", err)
				}
			}
		}
		return nodeHeight, nil
	}

	// Start syncing blocks.
	endHeight, err := importBlocks(startHeight)
	if err != nil {
		return endHeight, err
	} // else endHeight == nodeHeight

	// Final speed report
	speedReport()

	// Signal the final height to any heightClients.
	pgb.SignalHeight(uint32(nodeHeight))

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
		stage++
		log.Infof("Beginning SYNC STAGE %d of %d (duplicate row removal).", stage, stages)

		// Drop the temporary index on addresses(tx_vin_vout_row_id).
		log.Infof("Dropping temporary index on addresses(tx_vin_vout_row_id).")
		_, err = pgb.db.Exec(`DROP INDEX IF EXISTS idx_addresses_vinvout_id_tmp;`)
		if err != nil {
			return nodeHeight, err
		}

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

		stage++
		log.Infof("Beginning SYNC STAGE %d of %d (table indexing and analyzing).", stage, stages)

		// Create all indexes except those on addresses.matching_tx_hash and all
		// tickets indexes.
		if err = pgb.IndexAll(barLoad); err != nil {
			return nodeHeight, fmt.Errorf("IndexAll failed: %w", err)
		}

		if !updateAllAddresses {
			// The addresses.matching_tx_hash index is not included in IndexAll.
			if err = IndexAddressTableOnMatchingTxHash(pgb.db); err != nil {
				return nodeHeight, fmt.Errorf("IndexAddressTableOnMatchingTxHash failed: %w", err)
			}
		}

		// Tickets table indexes are not included in IndexAll. (move it into
		// IndexAll since tickets are always updated on the fly and not part of
		// updateAllAddresses?)
		if err = pgb.IndexTicketsTable(barLoad); err != nil {
			return nodeHeight, fmt.Errorf("IndexTicketsTable failed: %w", err)
		}

		// Deep ANALYZE all tables.
		stage++
		log.Infof("Beginning SYNC STAGE %d of %d (deep database ANALYZE).", stage, stages)
		if err = AnalyzeAllTables(pgb.db, deepStatsTarget); err != nil {
			return nodeHeight, fmt.Errorf("failed to ANALYZE tables: %w", err)
		}
		analyzed = true
	}

	select {
	case <-ctx.Done():
		return nodeHeight, fmt.Errorf("sync cancelled")
	default:
	}

	// Batch update addresses table with spending info.
	if updateAllAddresses {
		stage++
		log.Infof("Beginning SYNC STAGE %d of %d (setting spending info in addresses table). "+
			"This will take a while.", stage, stages)

		// Analyze vouts and transactions tables first.
		if !analyzed {
			log.Infof("Performing an ANALYZE(%d) on vouts table...", deepStatsTarget)
			if err = AnalyzeTable(pgb.db, "vouts", deepStatsTarget); err != nil {
				return nodeHeight, fmt.Errorf("failed to ANALYZE vouts table: %w", err)
			}
			log.Infof("Performing an ANALYZE(%d) on transactions table...", deepStatsTarget)
			if err = AnalyzeTable(pgb.db, "transactions", deepStatsTarget); err != nil {
				return nodeHeight, fmt.Errorf("failed to ANALYZE transactions table: %w", err)
			}
		}

		// Drop the index on addresses.matching_tx_hash if it exists.
		_ = DeindexAddressTableOnMatchingTxHash(pgb.db) // ignore error if the index is absent

		numAddresses, err := pgb.UpdateSpendingInfoInAllAddresses(barLoad)
		if err != nil {
			return nodeHeight, fmt.Errorf("UpdateSpendingInfoInAllAddresses FAILED: %w", err)
		}
		log.Infof("Updated %d rows of addresses table.", numAddresses)

		log.Info("Indexing addresses table on matching_tx_hash...")
		if err = IndexAddressTableOnMatchingTxHash(pgb.db); err != nil {
			return nodeHeight, fmt.Errorf("IndexAddressTableOnMatchingTxHash failed: %w", err)
		}

		// Deep ANALYZE the newly indexed addresses table.
		log.Infof("Performing an ANALYZE(%d) on addresses table...", deepStatsTarget)
		if err = AnalyzeTable(pgb.db, "addresses", deepStatsTarget); err != nil {
			return nodeHeight, fmt.Errorf("failed to ANALYZE addresses table: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		return nodeHeight, fmt.Errorf("sync cancelled")
	default:
	}

	// Quickly ANALYZE all tables if not already done after indexing.
	if !analyzed && requireAnalyze {
		stage++
		log.Infof("Beginning SYNC STAGE %d of %d (quick database ANALYZE).", stage, stages)
		if err = AnalyzeAllTables(pgb.db, quickStatsTarget); err != nil {
			return nodeHeight, fmt.Errorf("failed to ANALYZE tables: %w", err)
		}
	}

	// Set meta.ibd_complete = TRUE.
	if err = SetIBDComplete(pgb.db, true); err != nil {
		return nodeHeight, fmt.Errorf("failed to set meta.ibd_complete: %w", err)
	}

	select {
	case <-ctx.Done():
		return nodeHeight, fmt.Errorf("sync cancelled")
	default:
	}

	// After sync and indexing, must use upsert statement, which checks for
	// duplicate entries and updates instead of throwing and error and panicing.
	pgb.EnableDuplicateCheckOnInsert(true)

	// Catch up after indexing and other updates.
	_, nodeHeight, err = client.GetBestBlock(ctx)
	if err != nil {
		return nodeHeight, fmt.Errorf("GetBestBlock failed: %w", err)
	}
	if nodeHeight > endHeight {
		log.Infof("Catching up with network at block height %d from %d...", nodeHeight, endHeight)
		if endHeight, err = importBlocks(endHeight); err != nil {
			return endHeight, err
		}
	}

	// Caller should pre-fetch treasury balance when ready.

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

	log.Infof("SYNC COMPLETED at height %d. Delta:\n\t\t\t% 10d blocks\n\t\t\t% 10d transactions\n\t\t\t% 10d ins\n\t\t\t% 10d outs\n\t\t\t% 10d addresses",
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
	txraw, err1 := pgb.Client.GetRawTransactionVerbose(context.TODO(), ticketHash)
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
