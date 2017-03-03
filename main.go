// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/dcrsqlite"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/semver"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrrpcclient"
)

// mainCore does all the work. Deferred functions do not run after os.Exit(),
// so main wraps this function, which returns a code.
func mainCore() int {
	// Parse the configuration file, and setup logger.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load dcrdata config: %s\n", err.Error())
		return 1
	}
	defer backendLog.Flush()

	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			log.Critical(err)
			return -1
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// Start with version info
	log.Infof(appName+" version %s", ver.String())

	dcrrpcclient.UseLogger(clientLog)

	log.Debugf("Output folder: %v", cfg.OutFolder)
	log.Debugf("Log folder: %v", cfg.LogDir)

	// Create data output folder if it does not already exist
	if err = os.MkdirAll(cfg.OutFolder, 0750); err != nil {
		fmt.Printf("Failed to create data output folder %s. Error: %s\n",
			cfg.OutFolder, err.Error())
		return 2
	}

	// Connect to dcrd RPC server using websockets

	// Set up the notification handler to deliver blocks through a channel.
	makeNtfnChans(cfg)

	// Daemon client connection
	rpcutils.UseLogger(daemonLog)
	dcrdClient, nodeVer, err := connectNodeRPC(cfg)
	if err != nil || dcrdClient == nil {
		log.Infof("Connection to dcrd failed: %v", err)
		return 4
	}

	// Display connected network
	curnet, err := dcrdClient.GetCurrentNet()
	if err != nil {
		fmt.Println("Unable to get current network from dcrd:", err.Error())
		return 5
	}
	log.Infof("Connected to dcrd (JSON-RPC API v%s) on %v",
		nodeVer.String(), curnet.String())

	cerr := registerNodeNtfnHandlers(dcrdClient)
	if cerr != nil {
		log.Errorf("RPC client error: %v (%v)", cerr.Error(), cerr.Cause())
		return 6
	}

	// Block data collector
	collector := blockdata.NewBlockDataCollector(dcrdClient, activeChain)
	if collector == nil {
		log.Errorf("Failed to create block data collector")
		return 9
	}

	backendLog.Flush()

	// Build a slice of each required saver type for each data source
	var blockDataSavers []blockdata.BlockDataSaver
	var mempoolSavers []MempoolDataSaver

	// For example, dumping all mempool fees with a custom saver
	if cfg.DumpAllMPTix {
		log.Debugf("Dumping all mempool tickets to file in %s.\n", cfg.OutFolder)
		mempoolFeeDumper := NewMempoolFeeDumper(cfg.OutFolder, "mempool-fees")
		mempoolSavers = append(mempoolSavers, mempoolFeeDumper)
	}

	// Another (horrible) example of saving to a map in memory
	// blockDataMapSaver := NewBlockDataToMemdb()
	// blockDataSavers = append(blockDataSavers, blockDataMapSaver)

	// Sqlite output
	dcrsqlite.UseLogger(log)
	dbInfo := dcrsqlite.DBInfo{FileName: cfg.DBFileName}
	//sqliteDB, err := dcrsqlite.InitDB(&dbInfo)
	sqliteDB, err := dcrsqlite.InitWiredDB(&dbInfo, dcrdClient, activeChain)
	if err != nil {
		log.Errorf("Unable to initialize SQLite database: %v", err)
	}
	log.Infof("SQLite DB successfully opened: %s", cfg.DBFileName)
	defer sqliteDB.Close()

	blockDataSavers = append(blockDataSavers, &sqliteDB)

	// Ctrl-C to shut down.
	// Nothing should be sent the quit channel.  It should only be closed.
	quit := make(chan struct{})
	// Only accept a single CTRL+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// Start waiting for the interrupt signal
	go func() {
		<-c
		signal.Stop(c)
		// Close the channel so multiple goroutines can get the message
		log.Infof("CTRL+C hit.  Closing goroutines.")
		close(quit)
		return
	}()

	// Resync db
	var waitSync sync.WaitGroup
	waitSync.Add(1)
	go sqliteDB.SyncDB(&waitSync, quit)

	// WaitGroup for the monitor goroutines
	var wg sync.WaitGroup

	// Blockchain monitor for the collector
	wg.Add(1)
	// If collector is nil, so is connectChan
	addrMap := make(map[string]txhelpers.TxAction) // for support of watched addresses
	wsChainMonitor := blockdata.NewChainMonitor(collector, blockDataSavers,
		quit, &wg, !cfg.PoolValue, addrMap,
		ntfnChans.connectChan, ntfnChans.recvTxBlockChan)
	go wsChainMonitor.BlockConnectedHandler()

	if cfg.MonitorMempool {
		mpoolCollector := newMempoolDataCollector(dcrdClient)
		if mpoolCollector == nil {
			log.Error("Failed to create mempool data collector")
			return 13
		}

		mpData, err := mpoolCollector.Collect()
		if err != nil {
			log.Error("Mempool info collection failed while gathering initial"+
				"data: %v", err.Error())
			return 14
		}

		mpi := &mempoolInfo{
			currentHeight:               mpData.height,
			numTicketPurchasesInMempool: mpData.numTickets,
			numTicketsSinceStatsReport:  0,
			lastCollectTime:             time.Now(),
		}

		newTicketLimit := int32(cfg.MPTriggerTickets)
		mini := time.Duration(cfg.MempoolMinInterval) * time.Second
		maxi := time.Duration(cfg.MempoolMaxInterval) * time.Second

		mpm := newMempoolMonitor(mpoolCollector, mempoolSavers,
			quit, &wg, newTicketLimit, mini, maxi, mpi)
		wg.Add(1)
		go mpm.txHandler(dcrdClient)
	}

	// wait for resync before serving
	waitSync.Wait()

	// Start web API, unless resync was canceled
	select {
	case <-quit:
	default:
		app := newContext(dcrdClient, &sqliteDB)
		mux := newAPIRouter(app)
		mux.ListenAndServeProto(cfg.APIListen, cfg.APIProto)
	}

	// Wait for handlers to quit
	wg.Wait()

	// Closing these channels should be unnecessary if quit was handled right
	closeNtfnChans()

	if dcrdClient != nil {
		log.Infof("Closing connection to dcrd.")
		dcrdClient.Shutdown()
	}

	log.Infof("Bye!")
	time.Sleep(250 * time.Millisecond)

	return 0
}

func main() {
	os.Exit(mainCore())
}

func connectNodeRPC(cfg *config) (*dcrrpcclient.Client, semver.Semver, error) {
	notificationHandlers := getNodeNtfnHandlers(cfg)
	return rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdPass,
		cfg.DcrdCert, cfg.DisableDaemonTLS, notificationHandlers)
}
