// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/dcrdata/dcrdata/db/dcrpg"
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
func mainCore() error {
	// Parse the configuration file, and setup logger.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load dcrdata config: %s\n", err.Error())
		return err
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
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// Start with version info
	log.Infof(appName+" version %s", ver.String())

	//log.Debugf("Output folder: %v", cfg.OutFolder)
	log.Debugf("Log folder: %v", cfg.LogDir)

	// PostgreSQL
	usePG := cfg.FullMode
	if usePG {
		log.Info(`Running in full-functionality mode with PostgreSQL backend enabled.`)
	} else {
		log.Info(`Running in "Lite" mode with only SQLite backend and limited functionality.`)
	}

	// Connect to dcrd RPC server using websockets

	// Set up the notification handler to deliver blocks through a channel.
	makeNtfnChans(cfg)

	// Daemon client connection
	ntfnHandlers, collectionQueue := makeNodeNtfnHandlers(cfg)
	dcrdClient, nodeVer, err := connectNodeRPC(cfg, ntfnHandlers)
	if err != nil || dcrdClient == nil {
		return fmt.Errorf("Connection to dcrd failed: %v", err)
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
		return fmt.Errorf("Unable to get current network from dcrd: %v", err)
	}
	log.Infof("Connected to dcrd (JSON-RPC API v%s) on %v",
		nodeVer.String(), curnet.String())

	// Sqlite output
	dbInfo := dcrsqlite.DBInfo{FileName: cfg.DBFileName}
	baseDB, cleanupDB, err := dcrsqlite.InitWiredDB(&dbInfo,
		ntfnChans.updateStatusDBHeight, dcrdClient, activeChain)
	defer cleanupDB()
	if err != nil {
		return fmt.Errorf("Unable to initialize SQLite database: %v", err)
	}
	log.Infof("SQLite DB successfully opened: %s", cfg.DBFileName)
	defer baseDB.Close()

	// PostgreSQL
	var auxDB *dcrpg.ChainDB
	var newPGIndexes, updateAllAddresses, updateAllVotes bool
	if usePG {
		pgHost, pgPort := cfg.PGHost, ""
		if !strings.HasPrefix(pgHost, "/") {
			pgHost, pgPort, err = net.SplitHostPort(cfg.PGHost)
			if err != nil {
				return fmt.Errorf("SplitHostPort failed: %v", err)
			}
		}
		dbi := dcrpg.DBInfo{
			Host:   pgHost,
			Port:   pgPort,
			User:   cfg.PGUser,
			Pass:   cfg.PGPass,
			DBName: cfg.PGDBName,
		}
		auxDB, err = dcrpg.NewChainDB(&dbi, activeChain, baseDB.GetStakeDB())
		if auxDB != nil {
			defer auxDB.Close()
		}
		if err != nil {
			return err
		}

		if err = auxDB.VersionCheck(); err != nil {
			return err
		}

		var idxExists bool
		idxExists, err = auxDB.ExistsIndexVinOnVins()
		if !idxExists || err != nil {
			newPGIndexes = true
			log.Infof("Indexes not found. Forcing new index creation.")
		}

		idxExists, err = auxDB.ExistsIndexAddressesVoutIDAddress()
		if !idxExists || err != nil {
			updateAllAddresses = true
		}
	}

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

	_, height, err := dcrdClient.GetBestBlock()
	if err != nil {
		return fmt.Errorf("Unable to get block from node: %v", err)
	}

	var fetchToHeight = int64(math.MaxInt32)
	if usePG {
		var heightDB uint64
		heightDB, err = auxDB.HeightDB()
		lastBlockPG := int64(heightDB)
		if err != nil {
			if err != sql.ErrNoRows {
				return fmt.Errorf("Unable to get height from PostgreSQL DB: %v", err)
			}
			lastBlockPG = -1
		}
		// Allow stakedb to catch up to the auxDB, but after fetchToHeight,
		// stakedb must receive block signals from auxDB.
		fetchToHeight = lastBlockPG + 1

		// PG height and StakeDatabase height must be equal. StakeDatabase will
		// catch up automatically if it is behind, but we must manually rewind
		// it here if it is ahead of PG.
		stakedbHeight := int64(baseDB.GetStakeDB().Height())
		if uint64(stakedbHeight) > heightDB {
			// rewind stakedb
			log.Infof("Rewinding StakeDatabase from %d to %d.", stakedbHeight, lastBlockPG)
			stakedbHeight, err = baseDB.RewindStakeDB(lastBlockPG, quit)
			if err != nil {
				return fmt.Errorf("RewindStakeDB failed: %v", err)
			}
			if stakedbHeight != lastBlockPG {
				return fmt.Errorf("failed to rewind stakedb: got %d, expecting %d", stakedbHeight, lastBlockPG)
			}
		}

		// How far auxDB is behind the node
		blocksBehind := height - lastBlockPG
		if blocksBehind < 0 {
			return fmt.Errorf("Node is still syncing. Node height = %d, "+
				"DB height = %d", height, heightDB)
		}
		if blocksBehind > 7500 {
			log.Warnf("Setting PSQL sync to rebuild address table after large "+
				"import (%d blocks).", blocksBehind)
			updateAllAddresses = true
			if blocksBehind > 40000 {
				log.Warnf("Setting PSQL sync to drop indexes prior to bulk data "+
					"import (%d blocks).", blocksBehind)
				newPGIndexes = true
			}
		}

		// PG gets winning tickets out of baseDB's pool info cache, so it must
		// be big enough to hold the needed blocks' info, and charged with the
		// data from disk. The cache is updated on each block connect.
		tpcSize := int(blocksBehind) + 20
		log.Debugf("Setting ticket pool cache capacity to %d blocks", tpcSize)
		err = baseDB.GetStakeDB().SetPoolCacheCapacity(tpcSize)
		if err != nil {
			return err
		}

		/*stakeInfoHeight, err := baseDB.GetStakeInfoHeight()
		if err != nil {
			return err
		}
		if stakeInfoHeight >= int64(heightDB) {
			// Charge stakedb pool info cache, including previous PG block, up
			// to best in sqlite.
			if err = baseDB.ChargePoolInfoCache(int64(heightDB) - 10); err != nil {
				return fmt.Errorf("Failed to charge pool info cache: %v", err)
			}
		}*/
		if err = baseDB.ChargePoolInfoCache(int64(heightDB) - 2); err != nil {
			return fmt.Errorf("Failed to charge pool info cache: %v", err)
		}
	}

	// Simultaneously synchronize the ChainDB (PostgreSQL) and the block/stake
	// info DB (sqlite). Results are returned over channels:
	var sqliteHeight, pgHeight int64
	sqliteSyncRes := make(chan dbtypes.SyncResult)
	pgSyncRes := make(chan dbtypes.SyncResult)
	// Synchronization between DBs via rpcutils.BlockGate
	smartClient := rpcutils.NewBlockGate(dcrdClient, 10)
	//smartClient.SetFetchToHeight(fetchToHeight)
	for {
		// Launch the sync functions for both DBs

		// stakedb (in baseDB) connects blocks *after* ChainDB retrieves them,
		// but it has to get a notification channel first to receive them.
		baseDB.SyncDBAsync(sqliteSyncRes, quit, smartClient, fetchToHeight)

		// Now that stakedb is catching up or waiting
		go auxDB.SyncChainDBAsync(pgSyncRes, smartClient, quit,
			updateAllAddresses, updateAllVotes, newPGIndexes)

		// Wait for the results
		sqliteRes := <-sqliteSyncRes
		sqliteHeight = sqliteRes.Height
		log.Infof("SQLite sync ended at height %d", sqliteHeight)
		if sqliteRes.Error != nil {
			log.Errorf("dcrsqlite.SyncDBAsync failed at height %d.", sqliteHeight)
			close(quit)
			return sqliteRes.Error
		}

		pgRes := <-pgSyncRes
		pgHeight = pgRes.Height
		if usePG {
			log.Infof("PostgreSQL sync ended at height %d", pgHeight)
		}

		// See if there was a SIGINT (CTRL+C)
		select {
		case <-quit:
			log.Info("Quit signal received during DB sync.")
			return nil
		default:
		}

		// Check for errors and combine if necessary
		if usePG && pgRes.Error != nil {
			if sqliteRes.Error != nil {
				log.Error("dcrsqlite.SyncDBAsync AND dcrpg.SyncChainDBAsync "+
					"failed at heights %d and %d, respectively.",
					sqliteHeight, pgHeight)
				errCombined := fmt.Sprintln(sqliteRes.Error, ", ", pgRes.Error)
				return errors.New(errCombined)
			}
			log.Errorf("dcrpg.SyncChainDBAsync failed at height %d.", pgHeight)
			return pgRes.Error
		}

		// Break loop to continue starting dcrdata.
		if !usePG || pgHeight == sqliteHeight {
			break
		}
		log.Infof("Restarting sync with PostgreSQL at %d, SQLite at %d.",
			pgHeight, sqliteHeight)
		updateAllAddresses, newPGIndexes = false, false
	}

	// Block data collector
	collector := blockdata.NewCollector(dcrdClient, activeChain, baseDB.GetStakeDB())
	if collector == nil {
		return fmt.Errorf("Failed to create block data collector")
	}

	// Build a slice of each required saver type for each data source
	var blockDataSavers []blockdata.BlockDataSaver
	var mempoolSavers []mempool.MempoolDataSaver

	blockDataSavers = append(blockDataSavers, auxDB)

	// For example, dumping all mempool fees with a custom saver
	if cfg.DumpAllMPTix {
		log.Debugf("Dumping all mempool tickets to file in %s.\n", cfg.OutFolder)
		mempoolFeeDumper := mempool.NewMempoolFeeDumper(cfg.OutFolder, "mempool-fees")
		mempoolSavers = append(mempoolSavers, mempoolFeeDumper)
	}

	blockDataSavers = append(blockDataSavers, &baseDB)
	mempoolSavers = append(mempoolSavers, baseDB.MPC)

	// Start the explorer system
	explore := explorer.New(&baseDB, auxDB, cfg.UseRealIP, ver.String())
	explore.UseSIGToReloadTemplates()
	defer explore.StopWebsocketHub()
	blockDataSavers = append(blockDataSavers, explore)
	mempoolSavers = append(mempoolSavers, explore)

	// Initial data summary for web ui
	blockData, _, err := collector.Collect()
	if err != nil {
		return fmt.Errorf("Block data collection for initial summary failed: %v",
			err.Error())
	}

	if err = explore.Store(blockData, nil); err != nil {
		return fmt.Errorf("Failed to store initial block data for explorer pages: %v", err.Error())
	}
	// WaitGroup for the monitor goroutines
	var wg sync.WaitGroup

	// Blockchain monitor for the collector
	addrMap := make(map[string]txhelpers.TxAction) // for support of watched addresses
	// On reorg, only update web UI since dcrsqlite's own reorg handler will
	// deal with patching up the block info database.
	reorgBlockDataSavers := []blockdata.BlockDataSaver{explore}
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
	sdbChainMonitor := baseDB.NewStakeDBChainMonitor(quit, &wg,
		ntfnChans.connectChanStakeDB, ntfnChans.reorgChanStakeDB)
	wg.Add(2)
	go sdbChainMonitor.BlockConnectedHandler()
	go sdbChainMonitor.ReorgHandler()

	// Blockchain monitor for the wired sqlite DB
	wiredDBChainMonitor := baseDB.NewChainMonitor(collector, quit, &wg,
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
			return fmt.Errorf("Failed to create mempool data collector")
		}

		mpData, err := mpoolCollector.Collect()
		if err != nil {
			return fmt.Errorf("Mempool info collection failed while gathering"+
				" initial data: %v", err.Error())
		}

		// Store initial MP data
		if err = baseDB.MPC.StoreMPData(mpData, time.Now()); err != nil {
			return fmt.Errorf("Failed to store initial mempool data (wiredDB): %v",
				err.Error())
		}

		// Store initial MP data to explore
		if err = explore.StoreMPData(mpData, time.Now()); err != nil {
			return fmt.Errorf("Failed to store initial mempool data (explore): %v",
				err.Error())
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
		return nil
	default:
	}

	// Register for notifications now that the monitors are listening
	cerr := registerNodeNtfnHandlers(dcrdClient)
	if cerr != nil {
		return fmt.Errorf("RPC client error: %v (%v)", cerr.Error(), cerr.Cause())
	}

	// Start web API
	app := newContext(dcrdClient, &baseDB, cfg.IndentJSON)
	// Start notification hander to keep /status up-to-date
	wg.Add(1)
	go app.StatusNtfnHandler(&wg, quit)
	// Initial setting of db_height. Subsequently, Store() will send this.
	ntfnChans.updateStatusDBHeight <- uint32(baseDB.GetHeight())

	apiMux := newAPIRouter(app, cfg.UseRealIP)

	webMux := chi.NewRouter()
	webMux.Get("/", explore.Home)
	webMux.Get("/ws", explore.RootWebsocket)
	webMux.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./public/images/favicon.ico")
	})
	cacheControlMaxAge := int64(cfg.CacheControlMaxAge)
	FileServer(webMux, "/js", http.Dir("./public/js"), cacheControlMaxAge)
	FileServer(webMux, "/css", http.Dir("./public/css"), cacheControlMaxAge)
	FileServer(webMux, "/fonts", http.Dir("./public/fonts"), cacheControlMaxAge)
	FileServer(webMux, "/images", http.Dir("./public/images"), cacheControlMaxAge)
	webMux.NotFound(explore.NotFound)
	webMux.Mount("/api", apiMux.Mux)

	webMux.Mount("/explorer", explore.Mux)
	webMux.Get("/blocks", explore.Blocks)
	webMux.With(explore.BlockHashPathOrIndexCtx).Get("/block/{blockhash}", explore.Block)
	webMux.With(explorer.TransactionHashCtx).Get("/tx/{txid}", explore.TxPage)
	webMux.With(explorer.AddressPathCtx).Get("/address/{address}", explore.AddressPage)
	webMux.Get("/decodetx", explore.DecodeTxPage)
	webMux.Get("/search", explore.Search)

	// HTTP profiler
	if cfg.HTTPProfile {
		http.Handle("/", http.RedirectHandler("/debug/pprof", http.StatusSeeOther))
		webMux.Mount("/p", http.DefaultServeMux)
	}

	if err = listenAndServeProto(cfg.APIListen, cfg.APIProto, webMux); err != nil {
		log.Criticalf("listenAndServeProto: %v", err)
		close(quit)
	}

	// Wait for notification handlers to quit
	wg.Wait()

	return nil
}

func main() {
	if err := mainCore(); err != nil {
		if logRotator != nil {
			log.Error(err)
		}
		os.Exit(1)
	}
	os.Exit(0)
}

func connectNodeRPC(cfg *config, ntfnHandlers *rpcclient.NotificationHandlers) (*rpcclient.Client, semver.Semver, error) {
	return rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdPass,
		cfg.DcrdCert, cfg.DisableDaemonTLS, ntfnHandlers)
}

func listenAndServeProto(listen, proto string, mux http.Handler) error {
	// Try to bind web server
	errChan := make(chan error)
	if proto == "https" {
		go func() {
			errChan <- http.ListenAndServeTLS(listen, "dcrdata.cert", "dcrdata.key", mux)
		}()
	} else {
		go func() {
			errChan <- http.ListenAndServe(listen, mux)
		}()
	}

	// Briefly wait for an error and then return
	t := time.NewTimer(3 * time.Second)
	select {
	case err := <-errChan:
		return fmt.Errorf("Failed to bind web server: %v", err)
	case <-t.C:
		expLog.Infof("Now serving on %s://%v/", proto, listen)
		apiLog.Infof("Now serving on %s://%v/", proto, listen)
		return nil
	}
}

// FileServer conveniently sets up a http.FileServer handler to serve
// static files from a http.FileSystem.
func FileServer(r chi.Router, path string, root http.FileSystem, CacheControlMaxAge int64) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.With(CacheControl(CacheControlMaxAge)).Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	}))
}
