// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/decred/dcrd/rpcclient"
)

const (
	rescanLogBlockChunk = 250
)

// SyncChainDBAsync is like SyncChainDB except it also takes a result channel on
// which the caller should wait to receive the result. As such, this method
// should be called as a goroutine or it will hang on send if the channel is
// unbuffered.
func (db *ChainDB) SyncChainDBAsync(res chan dbtypes.SyncResult,
	client *rpcclient.Client, quit chan struct{}, updateAllAddresses,
	updateAllVotes, newIndexes bool) {
	if db == nil {
		res <- dbtypes.SyncResult{
			Height: -1,
			Error:  fmt.Errorf("ChainDB (psql) disabled"),
		}
		return
	}
	height, err := db.SyncChainDB(client, quit, updateAllAddresses,
		updateAllVotes, newIndexes)
	res <- dbtypes.SyncResult{
		Height: height,
		Error:  err,
	}
}

// SyncChainDB stores in the DB all blocks on the main chain available from the
// RPC client. The table indexes may be force-dropped and recreated by setting
// newIndexes to true. The quit channel is used to break the sync loop. For
// example, closing the channel on SIGINT.
func (db *ChainDB) SyncChainDB(client *rpcclient.Client, quit chan struct{},
	updateAllAddresses, updateAllVotes, newIndexes bool) (int64, error) {
	// Get chain servers's best block
	_, nodeHeight, err := client.GetBestBlock()
	if err != nil {
		return -1, fmt.Errorf("GetBestBlock failed: %v", err)
	}

	// Total and rate statistics
	var totalTxs, totalVins, totalVouts int64
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

	// Start rebuilding
	startHeight := lastBlock + 1
	for ib := startHeight; ib <= nodeHeight; ib++ {
		// check for quit signal
		select {
		case <-quit:
			log.Infof("Rescan cancelled at height %d.", ib)
			return ib - 1, nil
		default:
		}

		if (ib-1)%rescanLogBlockChunk == 0 || ib == startHeight {
			if ib == 0 {
				log.Infof("Scanning genesis block.")
			} else {
				endRangeBlock := rescanLogBlockChunk * (1 + (ib-1)/rescanLogBlockChunk)
				if endRangeBlock > nodeHeight {
					endRangeBlock = nodeHeight
				}
				log.Infof("Processing blocks %d to %d...", ib, endRangeBlock)
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

		block, blockHash, err := rpcutils.GetBlock(ib, client)
		if err != nil {
			return ib - 1, fmt.Errorf("GetBlock failed (%s): %v", blockHash, err)
		}

		var winners []string
		//prevBlockHash := block.MsgBlock().Header.PrevBlock
		for {
			// check for quit signal
			select {
			case <-quit:
				log.Infof("Rescan cancelled at height %d.", ib)
				return ib - 1, nil
			default:
			}

			if tpi, ok := db.stakeDB.PoolInfo(*blockHash); ok {
				winners = tpi.Winners
				break
			}
			blocksBehind := ib - int64(db.stakeDB.Height())
			if blocksBehind <= 0 {
				return ib - 1, fmt.Errorf("stakeDB.PoolInfo failed.")
			}
			log.Infof("Waiting for stake DB to catch up. Query height %d, "+
				"stake DB height %d.", ib, db.stakeDB.Height())
			waitSec := math.Max(5, math.Min(30.0, float64(blocksBehind)/500))
			time.Sleep(time.Duration(waitSec) * time.Second)

			if blocksBehind <= int64(db.stakeDB.Height()) {
				log.Infof("Rescan halted waiting for stakedb to advance.")
				return ib - 1, nil
			}
		}

		var numVins, numVouts int64
		if numVins, numVouts, err = db.StoreBlock(block.MsgBlock(),
			winners, true, !updateAllAddresses, !updateAllVotes); err != nil {
			return ib - 1, fmt.Errorf("StoreBlock failed: %v", err)
		}
		totalVins += numVins
		totalVouts += numVouts

		numSTx := int64(len(block.STransactions()))
		numRTx := int64(len(block.Transactions()))
		totalTxs += numRTx + numSTx
		// totalRTxs += numRTx
		// totalSTxs += numSTx

		// update height, the end condition for the loop
		if _, nodeHeight, err = client.GetBestBlock(); err != nil {
			return ib, fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	speedReport()

	if reindexing || newIndexes {
		if err = db.DeleteDuplicates(); err != nil {
			return 0, err
		}

		// Create indexes
		if err = db.IndexAll(); err != nil {
			return nodeHeight, fmt.Errorf("IndexAll failed: %v", err)
		}
		// Only reindex address table here if we do not do it below
		if !updateAllAddresses {
			err = db.IndexAddressTable()
		}
		if !updateAllVotes {
			err = db.IndexTicketsTable()
		}
	}

	if updateAllAddresses {
		// Remove existing indexes not on funding txns
		_ = db.DeindexAddressTable() // ignore errors for non-existent indexes
		log.Infof("Populating spending tx info in address table...")
		numAddresses, err := db.UpdateSpendingInfoInAllAddresses()
		if err != nil {
			log.Errorf("UpdateSpendingInfoInAllAddresses FAILED: %v", err)
		}
		log.Infof("Updated %d rows of address table", numAddresses)
		if err = db.IndexAddressTable(); err != nil {
			log.Errorf("IndexAddressTable FAILED: %v", err)
		}
	}

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
		if err = db.IndexTicketsTable(); err != nil {
			log.Errorf("IndexTicketsTable FAILED: %v", err)
		}
	}

	log.Infof("Sync finished at height %d. Delta: %d blocks, %d transactions, %d ins, %d outs",
		nodeHeight, nodeHeight-startHeight+1, totalTxs, totalVins, totalVouts)

	return nodeHeight, err
}
