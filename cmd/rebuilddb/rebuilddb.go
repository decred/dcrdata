package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"

	"github.com/decred/dcrd/rpcclient/v2"
	"github.com/decred/dcrdata/db/dcrsqlite"
	"github.com/decred/dcrdata/rpcutils"
	"github.com/decred/dcrdata/stakedb"
	"github.com/decred/slog"
)

var (
	backendLog      *slog.Backend
	rpcclientLogger slog.Logger
	sqliteLogger    slog.Logger
	stakedbLogger   slog.Logger
)

func init() {
	err := InitLogger()
	if err != nil {
		fmt.Printf("Unable to start logger: %v", err)
		os.Exit(1)
	}
	backendLog = slog.NewBackend(log.Writer())
	rpcclientLogger = backendLog.Logger("RPC")
	rpcclient.UseLogger(rpcclientLogger)
	sqliteLogger = backendLog.Logger("DSQL")
	dcrsqlite.UseLogger(rpcclientLogger)
	stakedbLogger = backendLog.Logger("SKDB")
	stakedb.UseLogger(stakedbLogger)
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

	// Connect to node RPC server
	client, _, err := rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser,
		cfg.DcrdPass, cfg.DcrdCert, cfg.DisableDaemonTLS, false)
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

	_, _, err = client.GetBestBlock()
	if err != nil {
		log.Error("GetBestBlock failed: ", err)
		return 2
	}

	// StakeDatabase
	sdbDir := "rebuild_data"
	stakeDB, stakeDBHeight, err := stakedb.NewStakeDatabase(client, activeChain, sdbDir)
	if err != nil {
		log.Errorf("Unable to create stake DB: %v", err)
		if stakeDBHeight >= 0 {
			log.Infof("Attempting to recover stake DB...")
			stakeDB, err = stakedb.LoadAndRecover(client, activeChain, sdbDir, stakeDBHeight-288)
		}
		if err != nil {
			if stakeDB != nil {
				_ = stakeDB.Close()
			}
			log.Errorf("StakeDatabase recovery failed: %v", err)
			return 1
		}
	}
	defer stakeDB.Close()

	// Sqlite output
	dbInfo := dcrsqlite.DBInfo{FileName: cfg.DBFileName}
	sqliteDB, err := dcrsqlite.InitWiredDB(&dbInfo, stakeDB, client,
		activeChain, func() {})
	if err != nil {
		log.Errorf("Unable to initialize SQLite database: %v", err)
	}
	log.Infof("SQLite DB successfully opened: %s", cfg.DBFileName)
	defer sqliteDB.Close()

	// Ctrl-C to shut down.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	ctx, cancel := context.WithCancel(context.Background())

	// Start waiting for the interrupt signal.
	go func() {
		<-c
		cancel()
		for range c {
			log.Info("Shutdown signaled. Already shutting down...")
		}
	}()

	// Resync db
	var height int64
	height, err = sqliteDB.SyncDB(ctx, nil, 0)
	if err != nil {
		log.Error(err)
	}

	log.Printf("Done at height %d!", height)

	return 0
}

func main() {
	os.Exit(mainCore())
}
