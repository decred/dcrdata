package main

import (
	"fmt"
	"math"
	"os"
	"runtime/pprof"

	"github.com/btcsuite/btclog"
	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/dcrsqlite"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

func init() {
	err := InitLogger()
	if err != nil {
		fmt.Printf("Unable to start logger: %v", err)
		os.Exit(1)
	}
}

func mainCore() int {
	// Parse the configuration file, and setup logger.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load dcrdata config: %s\n", err.Error())
		return 1
	}

	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			log.Fatal(err)
			return -1
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	btclogger, err := btclog.NewLoggerFromWriter(log.Writer(), btclog.InfoLvl)
	if err != nil {
		log.Error("Unable to create logger for dcrrpcclient: ", err)
	}
	dcrrpcclient.UseLogger(btclogger)

	// Setup Sqlite db
	dcrsqlite.UseLogger(btclogger)
	db, err := dcrsqlite.InitDB(&dcrsqlite.DBInfo{cfg.DBFileName})
	if err != nil {
		log.Fatalf("InitDB failed: %v", err)
		return 1
	}

	log.Infof("sqlite db successfully opened: %s", cfg.DBFileName)
	defer db.Close()

	// Connect to node RPC server
	client, _, err := rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser,
		cfg.DcrdPass, cfg.DcrdCert, cfg.DisableDaemonTLS)
	if err != nil {
		log.Fatalf("Unable to connect to RPC server: %v", err)
		return 1
	}

	infoResult, err := client.GetInfo()
	if err != nil {
		log.Errorf("GetInfo failed: %v", err)
		return 1
	}
	log.Info("Node connection count: ", infoResult.Connections)

	_, height, err := client.GetBestBlock()
	if err != nil {
		log.Error("GetBestBlock failed: ", err)
		return 2
	}

	log.Info("Current block:")

	blockSummaries := make([]apitypes.BlockDataBasic, 0, height+1)
	blocks := make(map[int64]*dcrutil.Block)

	i := int64(0)
	for i <= height {
		blockhash, err := client.GetBlockHash(i)
		if err != nil {
			log.Errorf("GetBlockHash(%d) failed: %v", i, err)
			return 3
		}

		block, err := client.GetBlock(blockhash)
		if err != nil {
			log.Errorf("GetBlock failed (%s): %v", blockhash, err)
			return 4
		}
		blocks[i] = block

		if i%500 == 0 {
			log.Infof("%d", i)
		}

		header := block.MsgBlock().Header
		diffRatio := blockdata.GetDifficultyRatio(header.Bits, activeChain)

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

		// update height
		_, height, err = client.GetBestBlock()
		if err != nil {
			log.Error("GetBestBlock failed: ", err)
			return 2
		}
		i++
	}

	log.Info("Building stake tree to compute pool values...")
	dbName := "ffldb_stake"
	stakeDB, poolValues, err := txhelpers.BuildStakeTree(blocks,
		activeChain, client, dbName)
	if err != nil {
		log.Errorf("Failed to create stake db: %v", err)
		return 8
	}
	defer os.RemoveAll(dbName)
	defer stakeDB.Close()

	log.Info("Saving block summaries to database...")
	for i := range blockSummaries {
		blockSummaries[i].PoolInfo.Value = dcrutil.Amount(poolValues[i]).ToCoin()
		if blockSummaries[i].PoolInfo.Size > 0 {
			blockSummaries[i].PoolInfo.ValAvg = blockSummaries[i].PoolInfo.Value / float64(blockSummaries[i].PoolInfo.Size)
		} else {
			blockSummaries[i].PoolInfo.ValAvg = 0
		}

		if err = db.StoreBlockSummary(&blockSummaries[i]); err != nil {
			log.Fatalf("Unable to store block summary in database: %v", err)
			return 5
		}

		if i%1000 == 0 {
			log.Infof("%d", i)
		}
	}

	// Stake info
	log.Info("Collecting and storing stake info to datbase...")
	stakeInfos := make([]apitypes.StakeInfoExtended, height+1)
	winSize := uint32(activeChain.StakeDiffWindowSize)

	for i := range blockSummaries {
		if i%1000 == 0 {
			log.Infof("%d", i)
		}

		si := apitypes.StakeInfoExtended{}

		// Ticket fee info
		block := blocks[int64(i)]
		newSStx := txhelpers.TicketsInBlock(block)
		si.Feeinfo.Height = uint32(i)
		si.Feeinfo.Number = uint32(len(newSStx))

		var minFee, maxFee, meanFee float64
		maxFee = math.MaxFloat64
		fees := make([]float64, si.Feeinfo.Number)
		for it := range newSStx {
			// rawTx, err := client.GetRawTransactionVerbose(&newSStx[it])
			// if err != nil {
			// 	log.Errorf("Unable to get sstx details: %v", err)
			// }
			// rawTx.Vin[iv].AmountIn
			rawTx, err := client.GetRawTransaction(&newSStx[it])
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
		si.PriceWindowNum = i / int(winSize)
		si.IdxBlockInWindow = i%int(winSize) + 1

		// Ticket pool info
		si.PoolInfo = blockSummaries[i].PoolInfo

		stakeInfos[i] = si

		if err = db.StoreStakeInfoExtended(&stakeInfos[i]); err != nil {
			log.Fatalf("Unable to store stake info in database: %v", err)
			return 5
		}
	}

	log.Print("Done!")

	return 0
}

func main() {
	os.Exit(mainCore())
}
