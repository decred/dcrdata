package dcrsqlite

import (
	"fmt"
	"math"
	"os"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrutil"
)

const (
	rescanLogBlockChunk = 1000
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

func (db *wiredDB) resyncDBWithPoolValue() error {
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

	// TODO: REDO THIS! We need to use a persistent stake db as it takes too
	// long to get back to the live ticket pool at any point.  It really has to
	// be saved on disk, loaded, and updated one block at a time.

	// Only save the block summaries we don't have in the DB yet
	blockSummaries := make([]apitypes.BlockDataBasic, 0, minBlocksToCheck+1)
	// But get ALL blocks to build stake tree for pool value computation
	blocks := make(map[int64]*dcrutil.Block)

	for i = 0; i <= height; i++ {
		blockhash, err := db.client.GetBlockHash(i)
		if err != nil {
			return fmt.Errorf("GetBlockHash(%d) failed: %v", i, err)
		}

		block, err := db.client.GetBlock(blockhash)
		if err != nil {
			return fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
		}
		blocks[i] = block

		if i%500 == 0 {
			log.Infof("%d", i)
		}

		// only get block summaries we need
		if i >= startHeight {
			header := block.MsgBlock().Header
			diffRatio := blockdata.GetDifficultyRatio(header.Bits, db.params)

			blockSummaries = append(blockSummaries, apitypes.BlockDataBasic{
				Height:     header.Height,
				Size:       header.Size,
				Hash:       blockhash.String(),
				Difficulty: diffRatio,
				StakeDiff:  dcrutil.Amount(header.SBits).ToCoin(),
				Time:       header.Timestamp.Unix(),
				PoolInfo: apitypes.TicketPoolInfo{
					Size: header.PoolSize,
				},
			})
		}

		// update height
		_, height, err = db.client.GetBestBlock()
		if err != nil {
			return fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	log.Info("Building stake tree to compute pool values...")
	dbName := "ffldb_stake"
	stakeDB, poolValues, err := txhelpers.BuildStakeTree(blocks,
		db.params, db.client, startHeight, dbName)
	if err != nil {
		return fmt.Errorf("Failed to create stake db: %v", err)
	}
	defer os.RemoveAll(dbName)
	defer stakeDB.Close()

	log.Info("Saving block summaries to database...")
	for i := range blockSummaries {
		blockInd := i + int(startHeight)
		blockSummaries[i].PoolInfo.Value = dcrutil.Amount(poolValues[blockInd]).ToCoin()
		if blockSummaries[i].PoolInfo.Size > 0 {
			blockSummaries[i].PoolInfo.ValAvg = blockSummaries[i].PoolInfo.Value / float64(blockSummaries[i].PoolInfo.Size)
		} else {
			blockSummaries[i].PoolInfo.ValAvg = 0
		}

		if err = db.StoreBlockSummary(&blockSummaries[i]); err != nil {
			return fmt.Errorf("Unable to store block summary in database: %v", err)
		}

		if blockInd%1000 == 0 {
			log.Infof("%d", blockInd)
		}
	}

	// Stake info
	log.Info("Collecting and storing stake info to database...")
	winSize := uint32(db.params.StakeDiffWindowSize)

	for i := startHeight; i <= height; i++ {
		if i%1000 == 0 {
			log.Infof("%d", i)
		}

		si := apitypes.StakeInfoExtended{}

		// Ticket fee info
		block := blocks[i]
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
		si.PoolInfo = blockSummaries[i-startHeight].PoolInfo

		if err = db.StoreStakeInfoExtended(&si); err != nil {
			return fmt.Errorf("Unable to store stake info in database: %v", err)
		}
	}

	return nil
}
