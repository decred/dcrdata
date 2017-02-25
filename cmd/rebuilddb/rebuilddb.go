package main

import (
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/btcsuite/btclog"
	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/dcrsqlite"
	"github.com/dcrdata/dcrdata/rpcutils"
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

// var routes = flag.Bool("dbuser", "dcrdata", "DB user")
// var proto = flag.String("dbpass", "bananas", "DB pass")

func mainCore() int {
	// defer logFile.Close()
	// flag.Parse()

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

	blockSummaries := make([]apitypes.BlockDataBasic, height+1)

	for i := int64(0); i < height+1; i++ {
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

		// info, err := client.GetInfo()
		// if err != nil {
		// 	log.Errorf("GetInfo failed: %v", err)
		// 	return 5
		// }

		if i%500 == 0 {
			log.Infof("%d", block.Height())
		}

		header := block.MsgBlock().Header
		diffRatio := blockdata.GetDifficultyRatio(header.Bits, activeChain)

		blockSummaries[i] = apitypes.BlockDataBasic{
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

		if err = db.StoreBlockSummary(&blockSummaries[i]); err != nil {
			log.Fatalf("Unable to store block summary in database: %v", err)
			return 5
		}
	}

	return 0
}

func main() {
	os.Exit(mainCore())
}
