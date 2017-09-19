// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/db/dcrsqlite"
	"github.com/dcrdata/dcrdata/explorer"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/semver"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient"
	"github.com/go-chi/chi"
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
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	if cfg.CPUProfile != "" {
		var f *os.File
		f, err = os.Create(cfg.CPUProfile)
		if err != nil {
			log.Critical(err)
			return -1
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// Start with version info
	log.Infof(appName+" version %s", ver.String())

	//log.Debugf("Output folder: %v", cfg.OutFolder)
	log.Debugf("Log folder: %v", cfg.LogDir)

	// // Create data output folder if it does not already exist
	// if err = os.MkdirAll(cfg.OutFolder, 0750); err != nil {
	// 	log.Errorf("Failed to create data output folder %s. Error: %s\n",
	// 		cfg.OutFolder, err.Error())
	// 	return 2
	// }

	// Connect to dcrd RPC server using websockets

	// Set up the notification handler to deliver blocks through a channel.
	makeNtfnChans(cfg)

	// Daemon client connection
	ntfnHandlers, collectionQueue := makeNodeNtfnHandlers(cfg)
	dcrdClient, nodeVer, err := connectNodeRPC(cfg, ntfnHandlers)
	if err != nil || dcrdClient == nil {
		log.Errorf("Connection to dcrd failed: %v", err)
		return 4
	}

	defer func() {
		// Closing these channels should be unnecessary if quit was handled right
		closeNtfnChans()

		if dcrdClient != nil {
			log.Infof("Closing connection to dcrd.")
			dcrdClient.Shutdown()
		}

		log.Infof("Bye!")
		time.Sleep(250 * time.Millisecond)
	}()

	// Display connected network
	curnet, err := dcrdClient.GetCurrentNet()
	if err != nil {
		log.Errorf("Unable to get current network from dcrd: %v", err)
		return 5
	}
	log.Infof("Connected to dcrd (JSON-RPC API v%s) on %v",
		nodeVer.String(), curnet.String())

	// Another (horrible) example of saving to a map in memory
	// blockDataMapSaver := NewBlockDataToMemdb()
	// blockDataSavers = append(blockDataSavers, blockDataMapSaver)

	// Sqlite output
	dbInfo := dcrsqlite.DBInfo{FileName: cfg.DBFileName}
	//sqliteDB, err := dcrsqlite.InitDB(&dbInfo)
	sqliteDB, cleanupDB, err := dcrsqlite.InitWiredDB(&dbInfo,
		ntfnChans.updateStatusDBHeight, dcrdClient, activeChain)
	defer cleanupDB()
	if err != nil {
		log.Errorf("Unable to initialize SQLite database: %v", err)
		return 16
	}
	log.Infof("SQLite DB successfully opened: %s", cfg.DBFileName)
	defer sqliteDB.Close()

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
	}()

	// Resync db
	var waitSync sync.WaitGroup
	waitSync.Add(1)
	// start as goroutine to let chain monitor start, but the sync will keep up
	// with current height, it is not likely to matter.
	if err = sqliteDB.SyncDBWithPoolValue(&waitSync, quit); err != nil {
		log.Error("Resync failed: ", err)
		return 15
	}

	// wait for resync before serving or collecting
	waitSync.Wait()

	select {
	case <-quit:
		return 20
	default:
	}

	// Block data collector
	collector := blockdata.NewCollector(dcrdClient, activeChain, sqliteDB.GetStakeDB())
	if collector == nil {
		log.Errorf("Failed to create block data collector")
		return 9
	}

	// Build a slice of each required saver type for each data source
	var blockDataSavers []blockdata.BlockDataSaver
	var mempoolSavers []mempool.MempoolDataSaver

	// For example, dumping all mempool fees with a custom saver
	if cfg.DumpAllMPTix {
		log.Debugf("Dumping all mempool tickets to file in %s.\n", cfg.OutFolder)
		mempoolFeeDumper := mempool.NewMempoolFeeDumper(cfg.OutFolder, "mempool-fees")
		mempoolSavers = append(mempoolSavers, mempoolFeeDumper)
	}

	blockDataSavers = append(blockDataSavers, &sqliteDB)
	mempoolSavers = append(mempoolSavers, sqliteDB.MPC)

	// Web template data. WebUI implements BlockDataSaver interface
	webUI := NewWebUI(&sqliteDB)
	if webUI == nil {
		log.Info("Failed to start WebUI. Missing HTML resources?")
		return 17
	}
	defer webUI.StopWebsocketHub()
	webUI.UseSIGToReloadTemplates()
	blockDataSavers = append(blockDataSavers, webUI)
	mempoolSavers = append(mempoolSavers, webUI)

	// Initial data summary for web ui
	blockData, err := collector.Collect()
	if err != nil {
		log.Errorf("Block data collection for initial summary failed: %v",
			err.Error())
		return 10
	}

	if err = webUI.Store(blockData); err != nil {
		log.Errorf("Failed to store initial block data: %v", err.Error())
		return 11
	}

	// WaitGroup for the monitor goroutines
	var wg sync.WaitGroup

	// Blockchain monitor for the collector
	addrMap := make(map[string]txhelpers.TxAction) // for support of watched addresses
	// On reorg, only update web UI since dcrsqlite's own reorg handler will
	// deal with patching up the block info database.
	reorgBlockDataSavers := []blockdata.BlockDataSaver{webUI}
	wsChainMonitor := blockdata.NewChainMonitor(collector, blockDataSavers,
		reorgBlockDataSavers, quit, &wg, addrMap,
		ntfnChans.connectChan, ntfnChans.recvTxBlockChan,
		ntfnChans.reorgChanBlockData)
	wg.Add(2)
	go wsChainMonitor.BlockConnectedHandler()
	// The blockdata reorg handler disables collection during reorg, leaving
	// dcrsqlite to do the switch, except for the last block which gets
	// collected and stored via reorgBlockDataSavers.
	go wsChainMonitor.ReorgHandler()

	// Blockchain monitor for the stake DB
	sdbChainMonitor := sqliteDB.NewStakeDBChainMonitor(quit, &wg,
		ntfnChans.connectChanStakeDB, ntfnChans.reorgChanStakeDB)
	wg.Add(2)
	go sdbChainMonitor.BlockConnectedHandler()
	go sdbChainMonitor.ReorgHandler()

	// Blockchain monitor for the wired sqlite DB
	wiredDBChainMonitor := sqliteDB.NewChainMonitor(collector, quit, &wg,
		ntfnChans.connectChanWiredDB, ntfnChans.reorgChanWiredDB)
	wg.Add(2)
	// dcrsqlite does not handle new blocks except during reorg
	go wiredDBChainMonitor.BlockConnectedHandler()
	go wiredDBChainMonitor.ReorgHandler()

	// Setup the synchronous handler functions called by the collectionQueue via
	// OnBlockConnected.
	collectionQueue.SetSynchronousHandlers([]func(*chainhash.Hash){
		sdbChainMonitor.BlockConnectedSync,     // 1. Stake DB for pool info
		wsChainMonitor.BlockConnectedSync,      // 2. blockdata for regular block data collection and storage
		wiredDBChainMonitor.BlockConnectedSync, // 3. dcrsqlite for sqlite DB reorg handling
	})

	if cfg.MonitorMempool {
		mpoolCollector := mempool.NewMempoolDataCollector(dcrdClient, activeChain)
		if mpoolCollector == nil {
			log.Error("Failed to create mempool data collector")
			return 13
		}

		mpData, err := mpoolCollector.Collect()
		if err != nil {
			log.Errorf("Mempool info collection failed while gathering initial"+
				"data: %v", err.Error())
			return 14
		}

		// Store initial MP data
		sqliteDB.MPC.StoreMPData(mpData, time.Now())

		// Store initial MP data to webUI
		if err = webUI.StoreMPData(mpData, time.Now()); err != nil {
			log.Errorf("Failed to store initial mempool data: %v",
				err.Error())
			return 19
		}

		// Setup monitor
		mpi := &mempool.MempoolInfo{
			CurrentHeight:               mpData.GetHeight(),
			NumTicketPurchasesInMempool: mpData.GetNumTickets(),
			NumTicketsSinceStatsReport:  0,
			LastCollectTime:             time.Now(),
		}

		newTicketLimit := int32(cfg.MPTriggerTickets)
		mini := time.Duration(cfg.MempoolMinInterval) * time.Second
		maxi := time.Duration(cfg.MempoolMaxInterval) * time.Second

		mpm := mempool.NewMempoolMonitor(mpoolCollector, mempoolSavers,
			ntfnChans.newTxChan, quit, &wg, newTicketLimit, mini, maxi, mpi)
		wg.Add(1)
		go mpm.TxHandler(dcrdClient)
	}

	select {
	case <-quit:
		return 20
	default:
	}

	// Register for notifications now that the monitors are listening
	cerr := registerNodeNtfnHandlers(dcrdClient)
	if cerr != nil {
		log.Errorf("RPC client error: %v (%v)", cerr.Error(), cerr.Cause())
		return 6
	}

	// Start web API
	app := newContext(dcrdClient, &sqliteDB, cfg.IndentJSON)
	// Start notification hander to keep /status up-to-date
	wg.Add(1)
	go app.StatusNtfnHandler(&wg, quit)
	// Initial setting of db_height. Subsequently, Store() will send this.
	ntfnChans.updateStatusDBHeight <- uint32(sqliteDB.GetHeight())

	apiMux := newAPIRouter(app, cfg.UseRealIP)

	// Start the explorer system
	explore := explorer.New(&sqliteDB, cfg.UseRealIP)
	explore.UseSIGToReloadTemplates()

	webMux := chi.NewRouter()
	webMux.Get("/", webUI.RootPage)
	webMux.Get("/ws", webUI.WSBlockUpdater)
	webMux.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./public/images/favicon.ico")
	})
	cacheControlMaxAge := int64(cfg.CacheControlMaxAge)
	FileServer(webMux, "/js", http.Dir("./public/js"), cacheControlMaxAge)
	FileServer(webMux, "/css", http.Dir("./public/css"), cacheControlMaxAge)
	FileServer(webMux, "/fonts", http.Dir("./public/fonts"), cacheControlMaxAge)
	FileServer(webMux, "/images", http.Dir("./public/images"), cacheControlMaxAge)
	webMux.With(SearchPathCtx).Get("/error/{search}", webUI.ErrorPage)
	webMux.NotFound(webUI.ErrorPage)
	webMux.Mount("/api", apiMux.Mux)
	webMux.Mount("/explorer", explore.Mux)
	listenAndServeProto(cfg.APIListen, cfg.APIProto, webMux)

	// Wait for notification handlers to quit
	wg.Wait()

	return 0
}

func main() {
	os.Exit(mainCore())
}

func connectNodeRPC(cfg *config, ntfnHandlers *rpcclient.NotificationHandlers) (*rpcclient.Client, semver.Semver, error) {
	return rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdPass,
		cfg.DcrdCert, cfg.DisableDaemonTLS, ntfnHandlers)
}

func listenAndServeProto(listen, proto string, mux http.Handler) {
	apiLog.Infof("Now serving on %s://%v/", proto, listen)
	if proto == "https" {
		go http.ListenAndServeTLS(listen, "dcrdata.cert", "dcrdata.key", mux)
	}
	go http.ListenAndServe(listen, mux)
}
