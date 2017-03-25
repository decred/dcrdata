package dcrsqlite

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	"github.com/decred/dcrutil"
	"github.com/oleiade/lane"
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
		diffRatio := blockdata.GetDifficultyRatio(header.Bits, db.params)

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
		newSStx := txhelpers.TicketsInBlock(block)
		si.Feeinfo.Height = uint32(i)
		si.Feeinfo.Number = uint32(len(newSStx))

		var minFee, maxFee, meanFee float64
		maxFee = math.MaxFloat64
		fees := make([]float64, si.Feeinfo.Number)
		for it := range newSStx {
			var rawTx *dcrutil.Tx
			// rawTx, err = db.client.GetRawTransactionVerbose(&newSStx[it])
			// if err != nil {
			// 	log.Errorf("Unable to get sstx details: %v", err)
			// }
			// rawTx.Vin[iv].AmountIn
			rawTx, err = db.client.GetRawTransaction(&newSStx[it])
			if err != nil {
				return fmt.Errorf("Unable to get sstx details (are you running"+
					"dcrd with --txindex?): %v", err)
			}
			msgTx := rawTx.MsgTx()
			var amtIn int64
			for iv := range msgTx.TxIn {
				amtIn += msgTx.TxIn[iv].ValueIn
			}
			var amtOut int64
			for iv := range msgTx.TxOut {
				amtOut += msgTx.TxOut[iv].Value
			}
			fee := dcrutil.Amount(amtIn - amtOut).ToCoin()
			if fee < minFee {
				minFee = fee
			}
			if fee > maxFee {
				maxFee = fee
			}
			meanFee += fee
			fees[it] = fee
		}

		meanFee /= float64(si.Feeinfo.Number)
		si.Feeinfo.Mean = meanFee
		si.Feeinfo.Median = txhelpers.MedianCoin(fees)
		si.Feeinfo.Min = minFee
		si.Feeinfo.Max = maxFee

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

func (db *wiredDB) resyncDBWithPoolValue(quit chan struct{}) error {
	// Get chain servers's best block
	_, height, err := db.client.GetBestBlock()
	if err != nil {
		return fmt.Errorf("GetBestBlock failed: %v", err)
	}

	// Time this function
	defer func(start time.Time, perr *error) {
		if *perr != nil {
			log.Infof("blockDataCollector.Collect() completed in %v", time.Since(start))
		}
	}(time.Now(), &err)

	// Get DB's best block (for block summary and stake info tables)
	bestBlockHeight := db.GetBlockSummaryHeight()
	bestStakeHeight := db.GetStakeInfoHeight()

	// Create a new database to store the accepted stake node data into.
	dbName := DefaultStakeDbName
	stakeDB, err := database.Open(dbType, dbName, db.params.Net)
	if err != nil {
		_ = os.RemoveAll(dbName)
		stakeDB, err = database.Create(dbType, dbName, db.params.Net)
		if err != nil {
			return fmt.Errorf("error creating db: %v", err)
		}
	}
	//defer os.RemoveAll(dbName)
	defer stakeDB.Close()

	// Load the best block from stake db
	var bestNode *stake.Node
	var bestNodeHeight int64
	err = stakeDB.View(func(dbTx database.Tx) error {
		v := dbTx.Metadata().Get([]byte("stakechainstate"))
		if v == nil {
			return fmt.Errorf("missing key for chain state data")
		}

		var stakeDBHash chainhash.Hash
		copy(stakeDBHash[:], v[:chainhash.HashSize])
		offset := chainhash.HashSize
		stakeDBHeight := binary.LittleEndian.Uint32(v[offset : offset+4])
		bestNodeHeight = int64(stakeDBHeight)

		var errLocal error
		block, errLocal := db.client.GetBlock(&stakeDBHash)
		if errLocal != nil {
			return fmt.Errorf("GetBlock failed (%s): %v", stakeDBHash, errLocal)
		}
		header := block.MsgBlock().Header

		bestNode, errLocal = stake.LoadBestNode(dbTx, stakeDBHeight,
			stakeDBHash, header, db.params)
		return errLocal
	})
	if err != nil {
		err = stakeDB.Update(func(dbTx database.Tx) error {
			var errLocal error
			bestNode, errLocal = stake.InitDatabaseState(dbTx, db.params)
			return errLocal
		})
		if err != nil {
			return err
		}
		log.Debug("Created new stake db.")
	} else {
		log.Debug("Opened existing stake db.")
	}

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
			return fmt.Errorf("delete stake db (ffldb_stake) and try again")
		}
		log.Infof("Rewinding stake node from %d to %d", bestNodeHeight, startHeight)
		// rewind best node in ticket db
		for bestNodeHeight > startHeight {
			// check for quit signal
			select {
			case <-quit:
				log.Infof("Rewind cancelled at height %d.", bestNodeHeight)
				return nil
			default:
			}
			// previous best node
			err = stakeDB.Update(func(dbTx database.Tx) error {
				var errLocal error
				block, _, errLocal := db.getBlock(bestNodeHeight - 1)
				if errLocal != nil {
					return errLocal
				}
				bestNode, errLocal = bestNode.DisconnectNode(block.MsgBlock().Header, nil, nil, dbTx)
				return errLocal
			})
			if err != nil {
				return err
			}
			bestNodeHeight = int64(bestNode.Height())
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
		return nil
	}

	// Start at next block we don't have in every DB
	startHeight++
	log.Infof("Resyncing from %v", startHeight)

	winSize := uint32(db.params.StakeDiffWindowSize)

	// a ticket treap would be nice, but a map will do for a cache
	liveTicketCache := make(map[chainhash.Hash]int64)

	blockQueue := lane.NewQueue()

	var startHeight0, firstMatureHeight int64
	if startHeight < db.params.StakeEnabledHeight {
		startHeight0 = 0
		firstMatureHeight = db.params.StakeEnabledHeight - int64(db.params.TicketMaturity)
		log.Infof("Start height (rewound): %d (%d); First maturing ticket height: %d",
			startHeight, startHeight0, firstMatureHeight)
	} else {
		startHeight0 = startHeight - int64(db.params.TicketMaturity)
		firstMatureHeight = startHeight0
		log.Infof("Start height (rewound/maturing height): %d (%d)",
			startHeight, startHeight0)
	}

	for i := startHeight0; i <= height; i++ {
		// check for quit signal
		select {
		case <-quit:
			log.Infof("Rescan cancelled at height %d.", i)
			return nil
		default:
		}

		block, blockhash, err := db.getBlock(i)
		if err != nil {
			return fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
		}

		if i >= firstMatureHeight {
			blockQueue.Enqueue(block)
		}

		numLive := bestNode.PoolSize()
		liveTickets := bestNode.LiveTickets()
		// TODO: winning tickets
		//winningTickets := bestNode.Winners()

		if i%rescanLogBlockChunk == 0 || i == startHeight0 {
			endRangeBlock := rescanLogBlockChunk * (1 + i/rescanLogBlockChunk)
			if endRangeBlock > height {
				endRangeBlock = height
			}
			log.Infof("Scanning blocks %d to %d (%d live)...",
				i, endRangeBlock, numLive)
		}

		var poolValue int64
		for _, hash := range liveTickets {
			val, ok := liveTicketCache[hash]
			if !ok {
				var txid *dcrutil.Tx
				txid, err = db.client.GetRawTransaction(&hash)
				if err != nil {
					fmt.Printf("Unable to get transaction %v: %v\n", hash, err)
					continue
				}
				// This isn't quite right for pool tickets where the small
				// pool fees are included in vout[0], but it's close.
				liveTicketCache[hash] = txid.MsgTx().TxOut[0].Value
			}
			poolValue += val
		}

		if i > bestNodeHeight {
			var ticketsToAdd []chainhash.Hash
			if i >= startHeight && i >= db.params.StakeEnabledHeight {
				//matureHeight := (i - int64(db.params.TicketMaturity))
				if blockQueue.Size() != int(db.params.TicketMaturity)+1 {
					panic("crap: " + strconv.Itoa(blockQueue.Size()) + " " + strconv.Itoa(int(i)))
				}
				maturingBlock := blockQueue.Dequeue().(*dcrutil.Block)
				if maturingBlock.Height() != i-int64(db.params.TicketMaturity) {
					panic("crap: " + strconv.Itoa(int(maturingBlock.Height())) + " " +
						strconv.Itoa(int(i)-int(db.params.TicketMaturity)))
				}
				ticketsToAdd = txhelpers.TicketsInBlock(maturingBlock)
				log.Tracef("Adding %02d tickets from block %d", len(ticketsToAdd), i)
			}

			spentTickets := txhelpers.TicketsSpentInBlock(block)
			for it := range spentTickets {
				delete(liveTicketCache, spentTickets[it])
			}
			revokedTickets := txhelpers.RevokedTicketsInBlock(block)
			for it := range revokedTickets {
				delete(liveTicketCache, revokedTickets[it])
			}

			if bestNode.Height()+1 != uint32(i) {
				panic(fmt.Sprintf("Best node height %d, trying to add %d", bestNode.Height(), i))
			}

			bestNode, err = bestNode.ConnectNode(block.MsgBlock().Header,
				spentTickets, revokedTickets, ticketsToAdd)
			if err != nil {
				return fmt.Errorf("couldn't connect node %d: %v", i, err.Error())
			}

			err = stakeDB.Update(func(dbTx database.Tx) error {
				// Write the new node to db.
				err = stake.WriteConnectedBestNode(dbTx, bestNode, *block.Hash())
				if err != nil {
					return fmt.Errorf("failure writing the best node: %v",
						err.Error())
				}

				return nil
			})
			if err != nil {
				return err
			}
		}

		if blockQueue.Size() == int(db.params.TicketMaturity)+1 {
			blockQueue.Dequeue()
		}

		if i < startHeight {
			continue
		}

		header := block.MsgBlock().Header
		diffRatio := blockdata.GetDifficultyRatio(header.Bits, db.params)

		poolCoin := dcrutil.Amount(poolValue).ToCoin()
		valAvg, poolSize := 0.0, float64(header.PoolSize)
		if header.PoolSize > 0 {
			valAvg = poolCoin / poolSize
		}

		blockSummary := apitypes.BlockDataBasic{
			Height:     header.Height,
			Size:       header.Size,
			Hash:       blockhash.String(),
			Difficulty: diffRatio,
			StakeDiff:  dcrutil.Amount(header.SBits).ToCoin(),
			Time:       header.Timestamp.Unix(),
			PoolInfo: apitypes.TicketPoolInfo{
				Size:   header.PoolSize,
				Value:  poolCoin,
				ValAvg: valAvg,
			},
		}

		if i > bestBlockHeight {
			if err = db.StoreBlockSummary(&blockSummary); err != nil {
				return fmt.Errorf("Unable to store block summary in database: %v", err)
			}
		}
		//log.Debugf("Stored block summary: %d", i)

		if i <= bestStakeHeight {
			// update height, the end condition for the loop
			if _, height, err = db.client.GetBestBlock(); err != nil {
				return fmt.Errorf("GetBestBlock failed: %v", err)
			}
			continue
		}

		// Stake info
		si := apitypes.StakeInfoExtended{}

		// Ticket fee info
		newSStx := txhelpers.TicketsInBlock(block)
		si.Feeinfo.Height = uint32(i)
		si.Feeinfo.Number = uint32(len(newSStx))

		var minFee, maxFee, meanFee float64
		maxFee = math.MaxFloat64
		fees := make([]float64, si.Feeinfo.Number)
		for it := range newSStx {
			var rawTx *dcrutil.Tx
			// rawTx, err := db.client.GetRawTransactionVerbose(&newSStx[it])
			// if err != nil {
			// 	log.Errorf("Unable to get sstx details: %v", err)
			// }
			// rawTx.Vin[iv].AmountIn
			rawTx, err = db.client.GetRawTransaction(&newSStx[it])
			if err != nil {
				log.Errorf("Unable to get sstx details: %v", err)
			}
			msgTx := rawTx.MsgTx()
			var amtIn int64
			for iv := range msgTx.TxIn {
				amtIn += msgTx.TxIn[iv].ValueIn
			}
			var amtOut int64
			for iv := range msgTx.TxOut {
				amtOut += msgTx.TxOut[iv].Value
			}
			fee := dcrutil.Amount(amtIn - amtOut).ToCoin()
			if fee < minFee {
				minFee = fee
			}
			if fee > maxFee {
				maxFee = fee
			}
			meanFee += fee
			fees[it] = fee
		}

		if si.Feeinfo.Number > 0 {
			meanFee /= float64(si.Feeinfo.Number)
			si.Feeinfo.Mean = meanFee
			si.Feeinfo.Median = txhelpers.MedianCoin(fees)
			si.Feeinfo.Min = minFee
			si.Feeinfo.Max = maxFee
		}

		// Price window number and block index
		si.PriceWindowNum = int(i) / int(winSize)
		si.IdxBlockInWindow = int(i)%int(winSize) + 1

		// Ticket pool info
		si.PoolInfo = blockSummary.PoolInfo

		if err = db.StoreStakeInfoExtended(&si); err != nil {
			return fmt.Errorf("Unable to store stake info in database: %v", err)
		}
		//log.Debugf("Stored stake info: %d", i)

		// update height, the end condition for the loop
		if _, height, err = db.client.GetBestBlock(); err != nil {
			return fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	log.Infof("Rescan finished successfully at height %d.", height)

	return nil
}

func (db *wiredDB) getBlock(ind int64) (*dcrutil.Block, *chainhash.Hash, error) {
	blockhash, err := db.client.GetBlockHash(ind)
	if err != nil {
		return nil, nil, fmt.Errorf("GetBlockHash(%d) failed: %v", ind, err)
	}

	block, err := db.client.GetBlock(blockhash)
	if err != nil {
		return nil, blockhash,
			fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
	}

	return block, blockhash, nil
}
