// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrsqlite

import (
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/rpcutils"
	"github.com/decred/dcrdata/txhelpers"
)

const (
	rescanLogBlockChunk = 250
)

// DBHeights returns the best block heights of: SQLite database tables (block
// summary and stake info tables), the stake database (ffldb_stake), and the
// lowest of these. An error value is returned if any database is inaccessible.
func (db *wiredDB) DBHeights() (lowest int64, summaryHeight int64, stakeInfoHeight int64,
	stakeDatabaseHeight int64, err error) {
	// Get DB's best block (for block summary and stake info tables)
	if summaryHeight, err = db.GetBlockSummaryHeight(); err != nil {
		return 0, 0, 0, -1, fmt.Errorf("GetBlockSummaryHeight failed: %v", err)
	}
	if stakeInfoHeight, err = db.GetStakeInfoHeight(); err != nil {
		return 0, 0, 0, -1, fmt.Errorf("GetStakeInfoHeight failed: %v", err)
	}

	// Create a new database to store the accepted stake node data into.
	if db.sDB == nil || db.sDB.BestNode == nil {
		return 0, 0, 0, -1, fmt.Errorf("stake DB is missing")
	}
	stakeDatabaseHeight = int64(db.sDB.Height())

	lowest = stakeInfoHeight
	if summaryHeight < stakeInfoHeight {
		lowest = summaryHeight
	}
	if stakeDatabaseHeight < lowest {
		lowest = stakeDatabaseHeight
	}

	return
}

func (db *wiredDB) initWaitChan(waitChan chan chainhash.Hash) {
	db.waitChan = waitChan
}

// RewindStakeDB attempts to disconnect blocks from the stake database to reach
// the specified height. A channel must be provided for signaling if the rewind
// should abort. If the specified height is greater than the current stake DB
// height, RewindStakeDB will exit without error, returning the current stake DB
// height and a nil error.
func (db *wiredDB) RewindStakeDB(toHeight int64, quit chan struct{}) (stakeDBHeight int64, err error) {
	// rewind best node in ticket db
	stakeDBHeight = int64(db.sDB.Height())
	if toHeight < 0 {
		toHeight = 0
	}
	log.Infof("Rewinding from %d to %d", stakeDBHeight, toHeight)
	for stakeDBHeight > toHeight {
		log.Infof("Rewinding from %d to %d", stakeDBHeight, toHeight)
		// check for quit signal
		select {
		case <-quit:
			log.Infof("Rewind cancelled at height %d.", stakeDBHeight)
			return
		default:
		}
		if err = db.sDB.DisconnectBlock(); err != nil {
			return
		}
		stakeDBHeight = int64(db.sDB.Height())
		log.Tracef("Stake db now at height %d.", stakeDBHeight)
	}
	return
}

func (db *wiredDB) resyncDB(quit chan struct{}, blockGetter rpcutils.BlockGetter,
	fetchToHeight int64) (int64, error) {
	// Determine if we're in lite mode, when we are the "master" who sets the
	// pace rather than waiting on other consumers to get done with the stakedb.
	master := blockGetter == nil || blockGetter.(*rpcutils.BlockGate) == nil

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

	// Check and report heights of the DBs
	startHeight, summaryHeight, stakeInfoHeight, stakeDBHeight, err := db.DBHeights()
	if err != nil {
		return -1, fmt.Errorf("DBHeights failed: %v", err)
	}
	if startHeight < -1 {
		panic("invalid starting height")
	}

	log.Info("Current best block (chain server):    ", height)
	log.Info("Current best block (sqlite block DB): ", summaryHeight)
	if stakeInfoHeight != summaryHeight {
		log.Error("Current best block (sqlite stake DB): ", stakeInfoHeight)
		return -1, fmt.Errorf("SQLite database (dcrdata.sqlt.db) is corrupted")
	}
	log.Info("Current best block (stakedb):         ", stakeDBHeight)

	// Attempt to rewind stake database, if needed
	if stakeDBHeight > startHeight && stakeDBHeight > 0 {
		if startHeight < 0 || stakeDBHeight > 2*startHeight {
			return -1, fmt.Errorf("delete stake db (ffldb_stake) and try again")
		}
		log.Infof("Rewinding stake node from %d to %d", stakeDBHeight, startHeight)
		// rewind best node in ticket db
		stakeDBHeight, err = db.RewindStakeDB(startHeight, quit)
		if err != nil {
			return startHeight, fmt.Errorf("RewindStakeDB failed: %v", err)
		}
	}

	if fetchToHeight < stakeDBHeight && !master {
		return startHeight, fmt.Errorf("fetchToHeight may not be less than stakedb height")
	}

	// At least this many blocks to check (another may come in before finishing)
	minBlocksToCheck := height - startHeight
	if minBlocksToCheck < 1 {
		if minBlocksToCheck < 0 {
			return startHeight, fmt.Errorf("chain server behind DBs")
		}
		return startHeight, nil
	}

	// Start at next block we don't have in every DB
	startHeight++

	for i := startHeight; i <= height; i++ {
		// check for quit signal
		select {
		case <-quit:
			log.Infof("Rescan cancelled at height %d.", i)
			return i - 1, nil
		default:
		}

		// Either fetch the block or wait for a signal that it is ready
		var block *dcrutil.Block
		var blockhash chainhash.Hash
		if master || i < fetchToHeight {
			// Not coordinating with blockGetter for this block
			var h *chainhash.Hash
			block, h, err = db.getBlock(i)
			if err != nil {
				return i - 1, fmt.Errorf("getBlock failed (%d): %v", i, err)
			}
			blockhash = *h
		} else {
			// Wait for this block to become available in the MasterBlockGetter
			select {
			case blockhash = <-db.waitChan:
			case <-quit:
				log.Infof("Rescan cancelled at height %d.", i)
				return i - 1, nil
			}
			block, err = blockGetter.Block(blockhash)
			if err != nil {
				return i - 1, fmt.Errorf("blockGetter.Block failed (%s): %v", blockhash, err)
			}
			// Before connecting the block in the StakeDatabase, request
			// notification for the next block.
			db.waitChan = blockGetter.WaitForHeight(i + 1)
		}

		if i > stakeDBHeight {
			if i != int64(db.sDB.Height()+1) {
				panic(fmt.Sprintf("about to connect the wrong block: %d, %d", i, db.sDB.Height()))
			}
			if err = db.sDB.ConnectBlock(block); err != nil {
				return i - 1, err
			}
		}

		numLive := db.sDB.PoolSize()
		//liveTickets := db.sDB.BestNode.LiveTickets()
		// TODO: winning tickets
		//winningTickets := db.sDB.BestNode.Winners()

		if (i-1)%rescanLogBlockChunk == 0 && i-1 != startHeight || i == startHeight {
			if i == 0 {
				log.Infof("Scanning genesis block.")
			} else {
				endRangeBlock := rescanLogBlockChunk * (1 + (i-1)/rescanLogBlockChunk)
				if endRangeBlock > height {
					endRangeBlock = height
				}
				log.Infof("Scanning blocks %d to %d (%d live)...",
					i, endRangeBlock, numLive)
			}
		}

		var tpi *apitypes.TicketPoolInfo
		var found bool
		if tpi, found = db.sDB.PoolInfo(blockhash); !found {
			if i != 0 {
				log.Warnf("Unable to find block (%s) in pool info cache. Resync is malfunctioning!", blockhash.String())
			}
			tpi = db.sDB.PoolInfoBest()
			if int64(tpi.Height) != i {
				log.Errorf("Collected block height %d != stake db height %d. Pool info "+
					"will not match the rest of this block's data.", tpi.Height, i)
			}
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

		if i > summaryHeight {
			if err = db.StoreBlockSummary(&blockSummary); err != nil {
				return i - 1, fmt.Errorf("Unable to store block summary in database: %v", err)
			}
		}

		if i <= stakeInfoHeight {
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
