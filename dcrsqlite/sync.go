package dcrsqlite

import (
	"fmt"
	"math"
	"os"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	"github.com/decred/dcrutil"
	"github.com/oleiade/lane"
	"strconv"
)

const (
	rescanLogBlockChunk = 1000
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
	i := int64(bestStakeHeight)
	if bestBlockHeight < bestStakeHeight {
		i = int64(bestBlockHeight)
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

		blockhash, err := db.client.GetBlockHash(i)
		if err != nil {
			return fmt.Errorf("GetBlockHash(%d) failed: %v", i, err)
		}

		block, err := db.client.GetBlock(blockhash)
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
			// rawTx, err := db.client.GetRawTransactionVerbose(&newSStx[it])
			// if err != nil {
			// 	log.Errorf("Unable to get sstx details: %v", err)
			// }
			// rawTx.Vin[iv].AmountIn
			rawTx, err := db.client.GetRawTransaction(&newSStx[it])
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

	// Get DB's best block (for block summary and stake info tables)
	bestBlockHeight := db.GetBlockSummaryHeight()
	bestStakeHeight := db.GetStakeInfoHeight()

	log.Info("Current best block (chain server): ", height)
	log.Info("Current best block (summary DB):   ", bestBlockHeight)
	log.Info("Current best block (stakeinfo DB): ", bestStakeHeight)

	// Start with the older of summary or stake table heights
	i := int64(bestStakeHeight)
	if bestBlockHeight < bestStakeHeight {
		i = int64(bestBlockHeight)
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

	// Create a new database to store the accepted stake node data into.
	dbName := DefaultStakeDbName
	_ = os.RemoveAll(dbName)
	stakeDB, err := database.Create(dbType, dbName, db.params.Net)
	if err != nil {
		return fmt.Errorf("error creating db: %v\n", err)
	}
	defer os.RemoveAll(dbName)
	defer stakeDB.Close()

	// Load the genesis block
	var bestNode *stake.Node
	err = stakeDB.Update(func(dbTx database.Tx) error {
		var errLocal error
		bestNode, errLocal = stake.InitDatabaseState(dbTx, db.params)
		return errLocal
	})
	if err != nil {
		return err
	}

	// a ticket treap would be nice, but a map will do for a cache
	liveTicketMap := make(map[chainhash.Hash]int64)

	blockQueue := lane.NewQueue()

	firstMatureHeight := startHeight - int64(db.params.TicketMaturity)
	startHeight0 := firstMatureHeight
	if startHeight < db.params.StakeEnabledHeight {
		firstMatureHeight = db.params.StakeEnabledHeight - int64(db.params.TicketMaturity)
		startHeight0 = 0
	}
	log.Infof("Start height (maturing height): %d (%d); First maturing ticket height: %d",
		startHeight, startHeight0, firstMatureHeight)

	for i = startHeight0; i <= height; i++ {
		// check for quit signal
		select {
		case <-quit:
			return nil
		default:
		}

		blockhash, err := db.client.GetBlockHash(i)
		if err != nil {
			return fmt.Errorf("GetBlockHash(%d) failed: %v", i, err)
		}

		block, err := db.client.GetBlock(blockhash)
		if err != nil {
			return fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
		}

		if i >= firstMatureHeight {
			blockQueue.Enqueue(block)
		}

		if i%rescanLogBlockChunk == 0 /* || i == startHeight */ {
			log.Infof("Scanning blocks %d to %d...", i, i+rescanLogBlockChunk)
		}

		if i < startHeight {
			continue
		}

		//numLive := bestNode.PoolSize()
		liveTickets := bestNode.LiveTickets()

		var poolValue int64
		for _, hash := range liveTickets {
			val, ok := liveTicketMap[hash]
			if !ok {
				txid, err := db.client.GetRawTransaction(&hash)
				if err != nil {
					fmt.Printf("Unable to get transaction %v: %v\n", hash, err)
					continue
				}
				// This isn't quite right for pool tickets where the small
				// pool fees are included in vout[0], but it's close.
				liveTicketMap[hash] = txid.MsgTx().TxOut[0].Value
			}
			poolValue += val
		}

		var ticketsToAdd []chainhash.Hash
		if i >= db.params.StakeEnabledHeight {
			//matureHeight := (i - int64(db.params.TicketMaturity))
			if blockQueue.Size() != int(db.params.TicketMaturity)+1 {
				panic("crap: " + strconv.Itoa(blockQueue.Size()) + " " + strconv.Itoa(int(i)))
			}
			maturingBlock := blockQueue.Dequeue().(*dcrutil.Block)
			if maturingBlock.Height() != i - int64(db.params.TicketMaturity) {
				panic("crap: " + strconv.Itoa(int(maturingBlock.Height())) + " " +
					strconv.Itoa(int(i) - int(db.params.TicketMaturity)))
			}
			ticketsToAdd = txhelpers.TicketsInBlock(maturingBlock)
		}

		spentTickets := txhelpers.TicketsSpentInBlock(block)
		for it := range spentTickets {
			delete(liveTicketMap, spentTickets[it])
		}
		revokedTickets := txhelpers.RevokedTicketsInBlock(block)
		for it := range revokedTickets {
			delete(liveTicketMap, revokedTickets[it])
		}

		bestNode, err = bestNode.ConnectNode(block.MsgBlock().Header,
			spentTickets, revokedTickets, ticketsToAdd)
		if err != nil {
			return fmt.Errorf("couldn't connect node: %v\n", err.Error())
		}

		err = stakeDB.Update(func(dbTx database.Tx) error {
			// Write the new node to db.
			err = stake.WriteConnectedBestNode(dbTx, bestNode, *block.Hash())
			if err != nil {
				return fmt.Errorf("failure writing the best node: %v\n",
					err.Error())
			}

			return nil
		})
		if err != nil {
			return err
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
			// rawTx, err := db.client.GetRawTransactionVerbose(&newSStx[it])
			// if err != nil {
			// 	log.Errorf("Unable to get sstx details: %v", err)
			// }
			// rawTx.Vin[iv].AmountIn
			rawTx, err := db.client.GetRawTransaction(&newSStx[it])
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

		meanFee /= float64(si.Feeinfo.Number)
		si.Feeinfo.Mean = meanFee
		si.Feeinfo.Median = txhelpers.MedianCoin(fees)
		si.Feeinfo.Min = minFee
		si.Feeinfo.Max = maxFee

		// Price window number and block index
		si.PriceWindowNum = int(i) / int(winSize)
		si.IdxBlockInWindow = int(i)%int(winSize) + 1

		// Ticket pool info
		si.PoolInfo = blockSummary.PoolInfo

		if err = db.StoreStakeInfoExtended(&si); err != nil {
			return fmt.Errorf("Unable to store stake info in database: %v", err)
		}

		// update height, the end condition for the loop
		// _, height, err = db.client.GetBestBlock()
		// if err != nil {
		// 	return fmt.Errorf("GetBestBlock failed: %v", err)
		//}
	}

	return nil
}
