package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btclog"
	"github.com/dcrdata/dcrdata/db/dcrpg"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/stakedb"
	"github.com/decred/dcrd/rpcclient"
)

var (
	backendLog      *btclog.Backend
	rpcclientLogger btclog.Logger
	sqliteLogger    btclog.Logger
)

const (
	rescanLogBlockChunk = 250
)

func init() {
	err := InitLogger()
	if err != nil {
		fmt.Printf("Unable to start logger: %v", err)
		os.Exit(1)
	}
	backendLog = btclog.NewBackend(log.Writer())
	rpcclientLogger = backendLog.Logger("RPC")
	rpcclient.UseLogger(rpcclientLogger)
	sqliteLogger = backendLog.Logger("DSQL")
	dcrpg.UseLogger(rpcclientLogger)
}

func mainCore() error {
	// Parse the configuration file, and setup logger.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load dcrdata config: %s\n", err.Error())
		return err
	}

	if cfg.CPUProfile != "" {
		var f *os.File
		f, err = os.Create(cfg.CPUProfile)
		if err != nil {
			log.Fatal(err)
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// Connect to node RPC server
	client, _, err := rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser,
		cfg.DcrdPass, cfg.DcrdCert, cfg.DisableDaemonTLS)
	if err != nil {
		log.Fatalf("Unable to connect to RPC server: %v", err)
		return err
	}

	infoResult, err := client.GetInfo()
	if err != nil {
		log.Errorf("GetInfo failed: %v", err)
		return err
	}
	log.Info("Node connection count: ", infoResult.Connections)

	host, port := cfg.DBHostPort, ""
	if !strings.HasPrefix(host, "/") {
		host, port, err = net.SplitHostPort(cfg.DBHostPort)
		if err != nil {
			log.Errorf("SplitHostPort failed: %v", err)
			return err
		}
	}

	stakeDB, err := stakedb.NewStakeDatabase(client, activeChain, "pg_rebuild_stakedb")
	if err != nil {
		return fmt.Errorf("Unable to create stake DB: %v", err)
	}
	defer stakeDB.Close()
	stakeDBHeight := int64(stakeDB.Height())

	dbi := dcrpg.DBInfo{
		Host:   host,
		Port:   port,
		User:   cfg.DBUser,
		Pass:   cfg.DBPass,
		DBName: cfg.DBName,
	}
	db, err := dcrpg.NewChainDB(&dbi, activeChain, stakeDB)
	if db != nil {
		defer db.Close()
	}
	if err != nil {
		return err
	}

	if cfg.DropDBTables {
		db.DropTables()
		return nil
	}

	if err = db.SetupTables(); err != nil {
		return err
	}

	// Ctrl-C to shut down.
	// Nothing should be sent the quit channel.  It should only be closed.
	quit := make(chan struct{})
	// Only accept a single CTRL+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// Check current height of DB
	bestHeight, err := db.HeightDB()
	lastBlock := int64(bestHeight)
	if err != nil {
		if err == sql.ErrNoRows {
			lastBlock = -1
			log.Info("blocks table is empty, starting fresh.")
		} else {
			log.Errorln("RetrieveBestBlockHeight:", err)
			return err
		}
	}

	// Start waiting for the interrupt signal
	go func() {
		<-c
		signal.Stop(c)
		// Close the channel so multiple goroutines can get the message
		log.Infof("CTRL+C hit.  Closing goroutines. Please wait.")
		close(quit)
	}()

	// Get stakedb at PG DB height
	if stakeDBHeight > lastBlock+1 {
		log.Infof("Rewinding stake db from %d to %d...", stakeDBHeight, lastBlock+1)
	}
	for stakeDBHeight > lastBlock+1 {
		// check for quit signal
		select {
		case <-quit:
			log.Infof("Rewind cancelled at height %d.", stakeDBHeight)
			return nil
		default:
		}
		if err = stakeDB.DisconnectBlock(); err != nil {
			return err
		}
		stakeDBHeight = int64(stakeDB.Height())
	}

	if stakeDBHeight < lastBlock {
		log.Infof("Advancing stake db from %d to %d...", stakeDBHeight, lastBlock)
	}
	for stakeDBHeight < lastBlock {
		block, blockHash, err := rpcutils.GetBlock(stakeDBHeight+1, client)
		if err != nil {
			return fmt.Errorf("GetBlock failed (%s): %v", blockHash, err)
		}

		if err = stakeDB.ConnectBlock(block); err != nil {
			return err
		}
		stakeDBHeight = int64(stakeDB.Height())
		if stakeDBHeight%1000 == 0 {
			log.Infof("Stake DB at height %d.", stakeDBHeight)
		}
	}

	var totalTxs, totalVins, totalVouts int64
	var lastTxs, lastVins, lastVouts int64
	tickTime := 10 * time.Second
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

	// Get chain servers's best block
	_, height, err := client.GetBestBlock()
	if err != nil {
		return fmt.Errorf("GetBestBlock failed: %v", err)
	}

	// Remove indexes/constraints before bulk import
	blocksToSync := height - lastBlock
	reindexing := blocksToSync > height/2
	if reindexing || cfg.ForceReindex {
		log.Info("Large bulk load: Removing indexes and disabling duplicate checks.")
		err = db.DeindexAll()
		if err != nil && !strings.Contains(err.Error(), "does not exist") {
			return err
		}
		db.EnableDuplicateCheckOnInsert(false)
	} else {
		db.EnableDuplicateCheckOnInsert(true)
	}

	startHeight := lastBlock + 1
	for ib := startHeight; ib <= height; ib++ {
		// check for quit signal
		select {
		case <-quit:
			log.Infof("Rescan cancelled at height %d.", ib)
			return nil
		default:
		}

		if (ib-1)%rescanLogBlockChunk == 0 || ib == startHeight {
			if ib == 0 {
				log.Infof("Scanning genesis block.")
			} else {
				endRangeBlock := rescanLogBlockChunk * (1 + (ib-1)/rescanLogBlockChunk)
				if endRangeBlock > height {
					endRangeBlock = height
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
			return fmt.Errorf("GetBlock failed (%s): %v", blockHash, err)
		}

		// stake db always has genesis, so do not connect it
		var winners []string
		if ib > 0 {
			if err = stakeDB.ConnectBlock(block); err != nil {
				return fmt.Errorf("stakedb.ConnectBlock failed: %v", err)
			}

			tpi, found := stakeDB.PoolInfo(*blockHash)
			if !found {
				return fmt.Errorf("stakedb.PoolInfo failed to return info for: %v", blockHash)
			}

			winners = tpi.Winners
		}

		var numVins, numVouts int64
		numVins, numVouts, err = db.StoreBlock(block.MsgBlock(), winners,
			true, cfg.AddrSpendInfoOnline)
		if err != nil {
			return fmt.Errorf("StoreBlock failed: %v", err)
		}
		totalVins += numVins
		totalVouts += numVouts

		numSTx := int64(len(block.STransactions()))
		numRTx := int64(len(block.Transactions()))
		totalTxs += numRTx + numSTx
		// totalRTxs += numRTx
		// totalSTxs += numSTx

		// update height, the end condition for the loop
		if _, height, err = client.GetBestBlock(); err != nil {
			return fmt.Errorf("GetBestBlock failed: %v", err)
		}
	}

	speedReport()

	if reindexing || cfg.ForceReindex {
		if err = db.IndexAll(); err != nil {
			return fmt.Errorf("IndexAll failed: %v", err)
		}
		// Only reindex address table here if we do not do it below
		if cfg.AddrSpendInfoOnline {
			err = db.IndexAddressTable()
		}
	}

	if !cfg.AddrSpendInfoOnline {
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

	log.Infof("Rebuild finished at height %d. Delta: %d blocks, %d transactions, %d ins, %d outs",
		height, height-startHeight+1, totalTxs, totalVins, totalVouts)

	return err
}

func main() {
	if err := mainCore(); err != nil {
		log.Error(err)
		os.Exit(1)
	}
	os.Exit(0)
}
