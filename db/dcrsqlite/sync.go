// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrsqlite

import (
	"fmt"
	"time"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
)

const (
	rescanLogBlockChunk = 250
	// dbType is the database backend type to use
	dbType = "ffldb"
	// DefaultStakeDbName is the default database name
	DefaultStakeDbName = "ffldb_stake"
)

func (db *wiredDB) resyncDB(quit chan struct{}) error {
	// Get chain servers's best block
	_, height, err := db.client.GetBestBlock()
	if err != nil {
		return fmt.Errorf("GetBestBlock failed: %v", err)
	}

	// Get DB's best block (for block summary and stake info tables)
	bestBlockHeight := db.GetBlockSummaryHeight()
	bestStakeHeight := db.GetStakeInfoHeight()

	log.Info("Current best block (chain server): ", height)
	log.Info("Current best block (summary DB):   ", bestBlockHeight)
	log.Info("Current best block (stakeinfo DB): ", bestStakeHeight)

	// Start with the older of summary or stake table heights
	i := bestStakeHeight
	if bestBlockHeight < bestStakeHeight {
		i = bestBlockHeight
	}
	if i < -1 {
		i = -1
	}

	// At least this many blocks to check (at least because another might come
	// in during this process).
	minBlocksToCheck := height - i
	if minBlocksToCheck < 1 {
		return nil
	}

	startHeight := i + 1
	log.Infof("Resyncing from %v", startHeight)

	winSize := uint32(db.params.StakeDiffWindowSize)

	// Only save the block summaries we don't have in the DB yet
	//blockSummaries := make([]apitypes.BlockDataBasic, 0, minBlocksToCheck+1)

	for i = startHeight; i <= height; i++ {
		// check for quit signal
		select {
		case <-quit:
			return nil
		default:
		}

		block, blockhash, err := db.getBlock(i)
		if err != nil {
			return fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
		}

		if i%rescanLogBlockChunk == 0 {
			log.Infof("Scanning blocks %d to %d...", i, i+rescanLogBlockChunk)
		}

		header := block.MsgBlock().Header
		diffRatio := txhelpers.GetDifficultyRatio(header.Bits, db.params)

		blockSummary := apitypes.BlockDataBasic{
			Height:     header.Height,
			Size:       header.Size,
			Hash:       blockhash.String(),
			Difficulty: diffRatio,
			StakeDiff:  dcrutil.Amount(header.SBits).ToCoin(),
			Time:       header.Timestamp.Unix(),
			PoolInfo: apitypes.TicketPoolInfo{
				Size: header.PoolSize,
			},
		}

		if err = db.StoreBlockSummary(&blockSummary); err != nil {
			return fmt.Errorf("Unable to store block summary in database: %v", err)
		}

		// Stake info
		si := apitypes.StakeInfoExtended{}

		// Ticket fee info
		fib := txhelpers.FeeRateInfoBlock(block)
		if fib == nil {
			return fmt.Errorf("FeeRateInfoBlock failed")
		}
		si.Feeinfo = *fib

		// Price window number and block index
		si.PriceWindowNum = int(i) / int(winSize)
		si.IdxBlockInWindow = int(i)%int(winSize) + 1

		// Ticket pool info (just size in this function)
		si.PoolInfo = blockSummary.PoolInfo

		if err = db.StoreStakeInfoExtended(&si); err != nil {
			return fmt.Errorf("Unable to store stake info in database: %v", err)
		}

		// update height
		_, height, err = db.client.GetBestBlock()
		if err != nil {
			return fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	log.Info("Resync complete.")

	return nil
}

func (db *wiredDB) resyncDBWithPoolValue(quit chan struct{}) (int64, error) {
	// Get chain servers's best block
	_, height, err := db.client.GetBestBlock()
	if err != nil {
		return -1, fmt.Errorf("GetBestBlock failed: %v", err)
	}

	// Time this function
	defer func(start time.Time, perr *error) {
		if *perr != nil {
			log.Infof("resyncDBWithPoolValue() completed in %v", time.Since(start))
		}
	}(time.Now(), &err)

	// Get DB's best block (for block summary and stake info tables)
	bestBlockHeight := db.GetBlockSummaryHeight()
	bestStakeHeight := db.GetStakeInfoHeight()

	// Create a new database to store the accepted stake node data into.
	if db.sDB == nil || db.sDB.BestNode == nil {
		return -1, fmt.Errorf("Cannot resync without the stake DB")
	}
	bestNodeHeight := int64(db.sDB.Height())

	log.Info("Current best block (chain server): ", height)
	log.Info("Current best block (summary DB):   ", bestBlockHeight)
	log.Info("Current best block (stakeinfo DB): ", bestStakeHeight)
	log.Info("Current best block (ticketdb):     ", bestNodeHeight)

	// Start with the older of summary or stake table heights
	startHeight := bestStakeHeight
	if bestBlockHeight < bestStakeHeight {
		startHeight = bestBlockHeight
	}
	if bestNodeHeight < startHeight {
		startHeight = bestNodeHeight
	} else if bestNodeHeight > startHeight && bestNodeHeight > 0 {
		if startHeight < 0 || bestNodeHeight > 2*startHeight {
			// log.Debug("Creating new stake db.")
			// if err = stakeDB.Update(func(dbTx database.Tx) error {
			// 	var errLocal error
			// 	bestNode, errLocal = stake.InitDatabaseState(dbTx, db.params)
			// 	return errLocal
			// }); err != nil {
			// 	return err
			// }
			return -1, fmt.Errorf("delete stake db (ffldb_stake) and try again")
		}
		log.Infof("Rewinding stake node from %d to %d", bestNodeHeight, startHeight)
		// rewind best node in ticket db
		for bestNodeHeight > startHeight {
			// check for quit signal
			select {
			case <-quit:
				log.Infof("Rewind cancelled at height %d.", bestNodeHeight)
				return startHeight, nil
			default:
			}
			if err = db.sDB.DisconnectBlock(); err != nil {
				return startHeight, err
			}
			bestNodeHeight = int64(db.sDB.Height())
			log.Infof("Stake db now at height %d.", bestNodeHeight)
		}
		if bestNodeHeight != startHeight {
			panic("rewind failed")
		}
	}
	if startHeight < -1 {
		startHeight = -1
	}

	// At least this many blocks to check (at least because another might come
	// in during this process).
	minBlocksToCheck := height - startHeight
	if minBlocksToCheck < 1 {
		if minBlocksToCheck < 0 {
			log.Warn("Chain server behind DBs!")
		}
		return startHeight, nil
	}

	// Start at next block we don't have in every DB
	startHeight++

	// a ticket treap would be nice, but a map will do for a cache
	//liveTicketCache := make(map[chainhash.Hash]int64)

	for i := startHeight; i <= height; i++ {
		// check for quit signal
		select {
		case <-quit:
			log.Infof("Rescan cancelled at height %d.", i)
			return i - 1, nil
		default:
		}

		block, blockhash, err := db.getBlock(i)
		if err != nil {
			return i - 1, fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
		}

		if i > bestNodeHeight {
			if i != int64(db.sDB.Height()+1) {
				panic(fmt.Sprintf("about to connect the wrong block: %d, %d", i, db.sDB.Height()))
			}
			if err = db.sDB.ConnectBlock(block); err != nil {
				return i - 1, err
			}
		}

		numLive := db.sDB.BestNode.PoolSize()
		//liveTickets := db.sDB.BestNode.LiveTickets()
		// TODO: winning tickets
		//winningTickets := db.sDB.BestNode.Winners()

		if (i-1)%rescanLogBlockChunk == 0 || i == startHeight {
			endRangeBlock := rescanLogBlockChunk * (1 + (i-1)/rescanLogBlockChunk)
			if endRangeBlock > height {
				endRangeBlock = height
			}
			log.Infof("Scanning blocks %d to %d (%d live)...",
				i, endRangeBlock, numLive)
		}

		var tpi *apitypes.TicketPoolInfo
		var found bool
		if tpi, found = db.sDB.PoolInfo(*blockhash); !found {
			log.Warnf("Unable to find block (%s) in pool info cache. Resync is malfunctioning!", blockhash.String())
			ticketPoolInfo, sdbHeight := db.sDB.PoolInfoBest()
			if int64(sdbHeight) != i {
				log.Errorf("Collected block height %d != stake db height %d. Pool info "+
					"will not match the rest of this block's data.", height, i)
			}
			tpi = &ticketPoolInfo
		}

		header := block.MsgBlock().Header
		diffRatio := txhelpers.GetDifficultyRatio(header.Bits, db.params)

		blockSummary := apitypes.BlockDataBasic{
			Height:     header.Height,
			Size:       header.Size,
			Hash:       blockhash.String(),
			Difficulty: diffRatio,
			StakeDiff:  dcrutil.Amount(header.SBits).ToCoin(),
			Time:       header.Timestamp.Unix(),
			PoolInfo:   *tpi,
		}

		if i > bestBlockHeight {
			if err = db.StoreBlockSummary(&blockSummary); err != nil {
				return i - 1, fmt.Errorf("Unable to store block summary in database: %v", err)
			}
		}

		if i <= bestStakeHeight {
			// update height, the end condition for the loop
			if _, height, err = db.client.GetBestBlock(); err != nil {
				return i - 1, fmt.Errorf("GetBestBlock failed: %v", err)
			}
			continue
		}

		// Stake info
		si := apitypes.StakeInfoExtended{}

		// Ticket fee info
		fib := txhelpers.FeeRateInfoBlock(block)
		if fib == nil {
			return i - 1, fmt.Errorf("FeeRateInfoBlock failed")
		}
		si.Feeinfo = *fib

		// Price window number and block index
		winSize := uint32(db.params.StakeDiffWindowSize)
		si.PriceWindowNum = int(i) / int(winSize)
		si.IdxBlockInWindow = int(i)%int(winSize) + 1

		// Ticket pool info
		si.PoolInfo = blockSummary.PoolInfo

		if err = db.StoreStakeInfoExtended(&si); err != nil {
			return i - 1, fmt.Errorf("Unable to store stake info in database: %v", err)
		}

		// update height, the end condition for the loop
		if _, height, err = db.client.GetBestBlock(); err != nil {
			return i, fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	log.Infof("Rescan finished successfully at height %d.", height)

	return height, nil
}

func (db *wiredDB) getBlock(ind int64) (*dcrutil.Block, *chainhash.Hash, error) {
	blockhash, err := db.client.GetBlockHash(ind)
	if err != nil {
		return nil, nil, fmt.Errorf("GetBlockHash(%d) failed: %v", ind, err)
	}

	msgBlock, err := db.client.GetBlock(blockhash)
	if err != nil {
		return nil, blockhash,
			fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
	}
	block := dcrutil.NewBlock(msgBlock)

	return block, blockhash, nil
}
