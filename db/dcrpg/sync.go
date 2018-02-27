// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/rpcutils"
)

const (
	rescanLogBlockChunk = 250
)

// SyncChainDBAsync is like SyncChainDB except it also takes a result channel on
// which the caller should wait to receive the result. As such, this method
// should be called as a goroutine or it will hang on send if the channel is
// unbuffered.
func (db *ChainDB) SyncChainDBAsync(res chan dbtypes.SyncResult,
	client rpcutils.MasterBlockGetter, quit chan struct{}, updateAllAddresses,
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
func (db *ChainDB) SyncChainDB(client rpcutils.MasterBlockGetter, quit chan struct{},
	updateAllAddresses, updateAllVotes, newIndexes bool) (int64, error) {
	// Get chain servers's best block
	nodeHeight, err := client.NodeHeight()
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

		// Register for notification from stakedb when it connects this block.
		waitChan := db.stakeDB.WaitForHeight(ib)

		// Get the block, making it available to stakedb, which will signal on
		// the above channel when it is done connecting it.
		block, err := client.UpdateToBlock(ib)
		if err != nil {
			return ib - 1, fmt.Errorf("UpdateToBlock (%d) failed: %v", ib, err)
		}

		// Wait for our StakeDatabase to connect the block
		var blockHash *chainhash.Hash
		select {
		case blockHash = <-waitChan:
		case <-quit:
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

		// Store data from this block in the database
		numVins, numVouts, err := db.StoreBlock(block.MsgBlock(), winners, true,
			!updateAllAddresses, !updateAllVotes)
		if err != nil {
			return ib - 1, fmt.Errorf("StoreBlock failed: %v", err)
		}
		totalVins += numVins
		totalVouts += numVouts

		// Total transactions is the sum of regular and stake transactions
		totalTxs += int64(len(block.STransactions()) + len(block.Transactions()))

		// Update height, the end condition for the loop
		if nodeHeight, err = client.NodeHeight(); err != nil {
			return ib, fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	speedReport()

	if reindexing || newIndexes {
		// Duplicate transactions, vins, and vouts can end up in the tables when
		// identical transactions are included in multiple blocks. This happens
		// when a block is invalidated and the transactions are subsequently
		// re-mined in another block. Remove these before indexing.
		if err = db.DeleteDuplicates(); err != nil {
			return 0, err
		}

		// Create indexes
		if err = db.IndexAll(); err != nil {
			return nodeHeight, fmt.Errorf("IndexAll failed: %v", err)
		}
		// Only reindex addresses and tickets tables here if not doing it below
		if !updateAllAddresses {
			err = db.IndexAddressTable()
		}
		if !updateAllVotes {
			err = db.IndexTicketsTable()
		}
	}

	// Batch update addresses table with spending info
	if updateAllAddresses {
		// Remove existing indexes not on funding txns
		_ = db.DeindexAddressTable() // ignore errors for non-existent indexes
		log.Infof("Populating spending tx info in address table...")
		numAddresses, err := db.UpdateSpendingInfoInAllAddresses()
		if err != nil {
			log.Errorf("UpdateSpendingInfoInAllAddresses FAILED: %v", err)
		}
		// Index addresses table
		log.Infof("Updated %d rows of address table", numAddresses)
		if err = db.IndexAddressTable(); err != nil {
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
		if err = db.IndexTicketsTable(); err != nil {
			log.Errorf("IndexTicketsTable FAILED: %v", err)
		}
	}

	log.Infof("Sync finished at height %d. Delta: %d blocks, %d transactions, %d ins, %d outs",
		nodeHeight, nodeHeight-startHeight+1, totalTxs, totalVins, totalVouts)

	return nodeHeight, err
}
