// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrdata/v3/api"
	"github.com/decred/dcrdata/v3/api/insight"
	"github.com/decred/dcrdata/v3/blockdata"
	"github.com/decred/dcrdata/v3/db/agendadb"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/db/dcrpg"
	"github.com/decred/dcrdata/v3/db/dcrsqlite"
	"github.com/decred/dcrdata/v3/explorer"
	"github.com/decred/dcrdata/v3/mempool"
	m "github.com/decred/dcrdata/v3/middleware"
	notify "github.com/decred/dcrdata/v3/notification"
	"github.com/decred/dcrdata/v3/rpcutils"
	"github.com/decred/dcrdata/v3/semver"
	"github.com/decred/dcrdata/v3/txhelpers"
	"github.com/decred/dcrdata/v3/version"
	"github.com/go-chi/chi"
	"github.com/google/gops/agent"
)

func main() {
	// Create a context that is cancelled when a shutdown request is received
	// via requestShutdown.
	ctx := withShutdownCancel(context.Background())
	// Listen for both interrupt signals and shutdown requests.
	go shutdownListener()

	if err := _main(ctx); err != nil {
		if logRotator != nil {
			log.Error(err)
		}
		os.Exit(1)
	}
	os.Exit(0)
}

// _main does all the work. Deferred functions do not run after os.Exit(), so
// main wraps this function, which returns a code.
func _main(ctx context.Context) error {
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

	if cfg.UseGops {
		// Start gops diagnostic agent, with shutdown cleanup.
		if err = agent.Listen(agent.Options{}); err != nil {
			return err
		}
		defer agent.Close()
	}

	// Display app version.
	log.Infof("%s version %v (Go version %s)", version.AppName,
		version.Version(), runtime.Version())

	// PostgreSQL backend is enabled via FullMode config option (--pg switch).
	usePG := cfg.FullMode
	if usePG {
		log.Info(`Running in full-functionality mode with PostgreSQL backend enabled.`)
	} else {
		log.Info(`Running in "Lite" mode with only SQLite backend and limited functionality.`)
	}

	// Setup the notification handlers.
	notify.MakeNtfnChans(cfg.MonitorMempool, usePG)

	// Connect to dcrd RPC server using a websocket.
	ntfnHandlers, collectionQueue := notify.MakeNodeNtfnHandlers()
	dcrdClient, nodeVer, err := connectNodeRPC(cfg, ntfnHandlers)
	if err != nil || dcrdClient == nil {
		return fmt.Errorf("Connection to dcrd failed: %v", err)
	}

	defer func() {
		// The individial hander's loops should close the notifications channels
		// on quit, but do it here too to be sure.
		notify.CloseNtfnChans()

		if dcrdClient != nil {
			log.Infof("Closing connection to dcrd.")
			dcrdClient.Shutdown()
		}

		log.Infof("Bye!")
		time.Sleep(250 * time.Millisecond)
	}()

	// Display connected network (e.g. mainnet, testnet, simnet).
	curnet, err := dcrdClient.GetCurrentNet()
	if err != nil {
		return fmt.Errorf("Unable to get current network from dcrd: %v", err)
	}
	log.Infof("Connected to dcrd (JSON-RPC API v%s) on %v",
		nodeVer.String(), curnet.String())

	if curnet != activeNet.Net {
		log.Criticalf("Network of connected node, %s, does not match expected "+
			"network, %s.", activeNet.Net, curnet)
		return fmt.Errorf("expected network %s, got %s", activeNet.Net, curnet)
	}

	// Sqlite output
	dbPath := filepath.Join(cfg.DataDir, cfg.DBFileName)
	dbInfo := dcrsqlite.DBInfo{FileName: dbPath}
	baseDB, cleanupDB, err := dcrsqlite.InitWiredDB(&dbInfo,
		notify.NtfnChans.UpdateStatusDBHeight, dcrdClient, activeChain, cfg.DataDir, !usePG)
	defer cleanupDB()
	if err != nil {
		return fmt.Errorf("Unable to initialize SQLite database: %v", err)
	}
	log.Infof("SQLite DB successfully opened: %s", cfg.DBFileName)
	defer baseDB.Close()

	// Auxiliary DB (currently PostgreSQL)
	var auxDB *dcrpg.ChainDBRPC
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
			Host:         pgHost,
			Port:         pgPort,
			User:         cfg.PGUser,
			Pass:         cfg.PGPass,
			DBName:       cfg.PGDBName,
			QueryTimeout: cfg.PGQueryTimeout,
		}
		chainDB, err := dcrpg.NewChainDBWithCancel(ctx, &dbi, activeChain, baseDB.GetStakeDB(), !cfg.NoDevPrefetch)
		if chainDB != nil {
			defer chainDB.Close()
		}
		if err != nil {
			return err
		}

		auxDB, err = dcrpg.NewChainDBRPC(chainDB, dcrdClient)
		if err != nil {
			return err
		}

		if err = auxDB.VersionCheck(dcrdClient); err != nil {
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

	blockHash, nodeHeight, err := dcrdClient.GetBestBlock()
	if err != nil {
		return fmt.Errorf("Unable to get block from node: %v", err)
	}

	var blocksBehind int64

	// When in lite mode, baseDB should get blocks without having to coordinate
	// with auxDB. Setting fetchToHeight to a large number allows this.
	var fetchToHeight = int64(math.MaxInt32)
	if usePG {
		// Get the last block added to the aux DB.
		var heightDB uint64
		heightDB, err = auxDB.HeightDB()
		lastBlockPG := int64(heightDB)
		if err != nil {
			if err != sql.ErrNoRows {
				return fmt.Errorf("Unable to get height from PostgreSQL DB: %v", err)
			}
			// lastBlockPG of 0 implies genesis is already processed.
			lastBlockPG = -1
		}

		// Allow wiredDB/stakedb to catch up to the auxDB, but after
		// fetchToHeight, wiredDB must receive block signals from auxDB, and
		// stakedb must send connect signals to auxDB.
		fetchToHeight = lastBlockPG + 1

		// Aux DB height and stakedb height must be equal. StakeDatabase will
		// catch up automatically if it is behind, but we must rewind it here if
		// it is ahead of auxDB. For auxDB to receive notification from
		// StakeDatabase when the required blocks are connected, the
		// StakeDatabase must be at the same height or lower than auxDB.
		stakedbHeight := int64(baseDB.GetStakeDB().Height())
		fromHeight := stakedbHeight
		if uint64(stakedbHeight) > heightDB {
			// Rewind stakedb and log at intervals of 200 blocks.
			if stakedbHeight == fromHeight || stakedbHeight%200 == 0 {
				log.Infof("Rewinding StakeDatabase from %d to %d.", stakedbHeight, heightDB)
			}
			stakedbHeight, err = baseDB.RewindStakeDB(ctx, int64(heightDB))
			if err != nil {
				return fmt.Errorf("RewindStakeDB failed: %v", err)
			}
			// stakedbHeight is always rewound to a height of zero even when lastBlockPG is -1.
			if stakedbHeight != int64(heightDB) {
				return fmt.Errorf("failed to rewind stakedb: got %d, expecting %d",
					stakedbHeight, heightDB)
			}
		}

		block, err := dcrdClient.GetBlockHeader(blockHash)
		if err != nil {
			return fmt.Errorf("unable to fetch the block from the node: %v", err)
		}

		// elapsedTime is the time since the dcrd best block was mined.
		elapsedTime := time.Since(block.Timestamp).Minutes()

		// Since mining a block take approximately ChainParams.TargetTimePerBlock then the
		// expected height of the best block from dcrd now should be this.
		expectedHeight := int64(elapsedTime/float64(activeChain.TargetTimePerBlock)) + nodeHeight

		// How far auxDB is behind the node
		blocksBehind = expectedHeight - lastBlockPG
		if blocksBehind < 0 {
			return fmt.Errorf("Node is still syncing. Node height = %d, "+
				"DB height = %d", expectedHeight, heightDB)
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
		tpcSize := int(blocksBehind) + 200
		log.Debugf("Setting ticket pool cache capacity to %d blocks", tpcSize)
		err = baseDB.GetStakeDB().SetPoolCacheCapacity(tpcSize)
		if err != nil {
			return err
		}

		// Charge stakedb pool info cache, including previous PG blocks, up to
		// best in sqlite.
		if err = baseDB.ChargePoolInfoCache(int64(heightDB) - 2); err != nil {
			return fmt.Errorf("Failed to charge pool info cache: %v", err)
		}
	}

	// Set the path to the AgendaDB file.
	agendadb.SetDbPath(filepath.Join(cfg.DataDir, cfg.AgendaDBFileName))

	// AgendaDB upgrade check
	if err = agendadb.CheckForUpdates(dcrdClient); err != nil {
		return fmt.Errorf("agendadb upgrade failed: %v", err)
	}

	// Block data collector. Needs a StakeDatabase too.
	collector := blockdata.NewCollector(dcrdClient, activeChain, baseDB.GetStakeDB())
	if collector == nil {
		return fmt.Errorf("Failed to create block data collector")
	}

	// Build a slice of each required saver type for each data source.
	var blockDataSavers []blockdata.BlockDataSaver
	var mempoolSavers []mempool.MempoolDataSaver
	if usePG {
		blockDataSavers = append(blockDataSavers, auxDB)
	}

	// For example, dumping all mempool fees with a custom saver
	if cfg.DumpAllMPTix {
		log.Debugf("Dumping all mempool tickets to file in %s.\n", cfg.OutFolder)
		mempoolFeeDumper := mempool.NewMempoolFeeDumper(cfg.OutFolder, "mempool-fees")
		mempoolSavers = append(mempoolSavers, mempoolFeeDumper)
	}

	blockDataSavers = append(blockDataSavers, &baseDB)
	mempoolSavers = append(mempoolSavers, baseDB.MPC)

	// Allow Ctrl-C to halt startup here.
	if shutdownRequested(ctx) {
		return nil
	}

	// Create the explorer system.
	explore := explorer.New(&baseDB, auxDB, cfg.UseRealIP, version.Version(), !cfg.NoDevPrefetch)
	if explore == nil {
		return fmt.Errorf("failed to create new explorer (templates missing?)")
	}
	explore.UseSIGToReloadTemplates()
	defer explore.StopWebsocketHub()
	defer explore.StopMempoolMonitor(notify.NtfnChans.ExpNewTxChan)

	wireDBheight := baseDB.GetHeight() // sqlite base db

	var auxDBheight int
	if usePG {
		auxDBheight = auxDB.GetHeight() // pg db
	}

	// barLoad is used to send sync status updates from a given function or
	// method to SyncStatusUpdate function via websocket. Sends do not need to
	// block, so this is buffered.
	barLoad := make(chan *dbtypes.ProgressBarLoad, 2)

	// latestBlockHash receives the block hash of the latest block to be sync'd
	// in dcrdata. This may not necessarily be the latest block in the
	// blockchain but it is the latest block to be sync'd according to dcrdata.
	// This block hash is sent if the webserver is providing the full explorer
	// functionality during blockchain syncing. Sends do not need to block, so
	// this is buffered.
	latestBlockHash := make(chan *chainhash.Hash, 2)

	// The blockchain syncing status page should be displayed; if the blocks
	// behind the current height are more than the set status limit. On initial
	// dcrdata startup syncing should be displayed by default. (If height is
	// less than 40,000, initial startup from scratch must have been run)
	// The sync status page should also be displayed when updateAllAddresses and
	// newPGIndexes are set to true.
	displaySyncStatusPage := blocksBehind > cfg.SyncStatusLimit ||
		(usePG && auxDBheight < 40000) || wireDBheight < 40000 ||
		updateAllAddresses || newPGIndexes

	// Set the displaySyncStatusPage value.
	explore.SetDisplaySyncStatusPage(displaySyncStatusPage)

	// Initiate the sync status monitor and SyncStatusUpdate goroutine if the
	// sync status is activated or else initiate the goroutine that handles
	// storing blocks needed for the explorer pages.
	if displaySyncStatusPage {
		// Starts a goroutine that signals the websocket to check and send to
		// the frontend the latest sync status progress updates.
		explore.StartSyncingStatusMonitor()
	} else {
		// Set that blocks freshly sync'd to to be stored for the explorer pages
		// till the sync is done.
		explorer.SetSyncExplorerUpdateStatus(true)

		// This goroutines updates the blocks needed on the explorer pages. It
		// only runs when the status sync page is not the default page that a user
		// can view on the running webserver but the syncing of blocks behind the
		// best block is happening in the background. No new blocks monitor from
		// dcrd will be initiated until the best block from dcrd is in sync with
		// the best block from dcrdata.
		go func() {
			for hash := range latestBlockHash {
				// Checks if updates should be sent to the explorer. If its been
				// deactivated it breaks the loop.
				if !explorer.SyncExplorerUpdateStatus() {
					break
				}

				// Setch the blockdata using its block hash.
				d, msgBlock, err := collector.CollectHash(hash)
				if err != nil {
					log.Warnf("failed to fetch blockdata for (%s) hash. error: %v",
						hash.String(), err)
					continue
				}

				// Store the blockdata fetch for the explorer pages
				if err = explore.Store(d, msgBlock); err != nil {
					log.Warnf("failed to store (%s) hash's blockdata for the explorer pages error: %v",
						hash.String(), err)
				}
			}
		}()

		// Stores the first block in the explorer. It should correspond with the
		// block that is currently in sync in all the dcrdata dbs
		loadHeight := wireDBheight
		if usePG && (auxDBheight < wireDBheight) {
			loadHeight = auxDBheight
		}

		// Fetches the first block hash whose blockdata will be loaded in the
		// explorer for it pages.
		loadBlockHash, err := dcrdClient.GetBlockHash(int64(loadHeight))
		if err != nil {
			return fmt.Errorf("failed to fetch the block at height (%d), dcrdata dbs must be corrupted",
				loadHeight)
		}

		// Signal to load this block's data into the explorer.
		latestBlockHash <- loadBlockHash
	}

	// Starts a goroutine that fetches the raw updates, process them and
	// set them as the latest sync status progress updates. The goroutine exits
	// when no sync is running.
	explore.SyncStatusUpdate(barLoad)

	blockDataSavers = append(blockDataSavers, explore)

	// Create the Insight socket.io server, and add it to block savers if in
	// full/pg mode. Since insightSocketServer is added into the url before even
	// the sync starts, this implementation cannot be moved to
	// initiateHandlersAndCollectBlocks function.
	var insightSocketServer *insight.SocketServer
	if usePG {
		insightSocketServer, err = insight.NewSocketServer(notify.NtfnChans.InsightNewTxChan, activeChain)
		if err == nil {
			blockDataSavers = append(blockDataSavers, insightSocketServer)
		} else {
			return fmt.Errorf("Could not create Insight socket.io server: %v", err)
		}
	}

	// WaitGroup for the monitor goroutines
	var wg sync.WaitGroup

	// Start dcrdata's JSON web API.
	app := api.NewContext(dcrdClient, activeChain, &baseDB, auxDB, cfg.IndentJSON)
	// Start the notification hander for keeping /status up-to-date.
	wg.Add(1)
	go app.StatusNtfnHandler(ctx, &wg)
	// Initial setting of DBHeight. Subsequently, Store() will send this.
	if wireDBheight >= 0 {
		// Do not sent 4294967295 = uint32(-1) if there are no blocks.
		notify.NtfnChans.UpdateStatusDBHeight <- uint32(wireDBheight)
	}

	// Configure the URL path to http handler router for the API.
	apiMux := api.NewAPIRouter(app, cfg.UseRealIP)
	// Configure the explorer web pages router.
	webMux := chi.NewRouter()
	webMux.With(explore.SyncStatusPageActivation).Group(func(r chi.Router) {
		r.Get("/", explore.Home)
		r.Get("/nexthome", explore.NextHome)
	})
	webMux.Get("/ws", explore.RootWebsocket)
	webMux.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./public/images/favicon.ico")
	})
	cacheControlMaxAge := int64(cfg.CacheControlMaxAge)
	FileServer(webMux, "/js", http.Dir("./public/js"), cacheControlMaxAge)
	FileServer(webMux, "/css", http.Dir("./public/css"), cacheControlMaxAge)
	FileServer(webMux, "/fonts", http.Dir("./public/fonts"), cacheControlMaxAge)
	FileServer(webMux, "/images", http.Dir("./public/images"), cacheControlMaxAge)

	// SyncStatusApiResponse returns a json response when the sync status page is running.
	webMux.With(explore.SyncStatusApiResponse).Group(func(r chi.Router) {
		// Mount the dcrdata's REST API.
		r.Mount("/api", apiMux.Mux)
		// Setup and mount the Insight API.
		if usePG {
			insightApp := insight.NewInsightContext(dcrdClient, auxDB, activeChain, &baseDB, cfg.IndentJSON)
			insightMux := insight.NewInsightApiRouter(insightApp, cfg.UseRealIP)
			r.Mount("/insight/api", insightMux.Mux)

			if insightSocketServer != nil {
				r.Get("/insight/socket.io/", insightSocketServer.ServeHTTP)
			}
		}
	})

	webMux.With(explore.SyncStatusPageActivation).Group(func(r chi.Router) {
		r.NotFound(explore.NotFound)

		r.Mount("/explorer", explore.Mux)
		r.Get("/days", explore.DayBlocksListing)
		r.Get("/weeks", explore.WeekBlocksListing)
		r.Get("/months", explore.MonthBlocksListing)
		r.Get("/years", explore.YearBlocksListing)
		r.Get("/blocks", explore.Blocks)
		r.Get("/ticketpricewindows", explore.StakeDiffWindows)
		r.Get("/side", explore.SideChains)
		r.Get("/rejects", explore.DisapprovedBlocks)
		r.Get("/mempool", explore.Mempool)
		r.Get("/parameters", explore.ParametersPage)
		r.With(explore.BlockHashPathOrIndexCtx).Get("/block/{blockhash}", explore.Block)
		r.With(explorer.TransactionHashCtx).Get("/tx/{txid}", explore.TxPage)
		r.With(explorer.TransactionHashCtx, explorer.TransactionIoIndexCtx).Get("/tx/{txid}/{inout}/{inoutid}", explore.TxPage)
		r.With(explorer.AddressPathCtx).Get("/address/{address}", explore.AddressPage)
		r.Get("/agendas", explore.AgendasPage)
		r.With(explorer.AgendaPathCtx).Get("/agenda/{agendaid}", explore.AgendaPage)
		r.Get("/decodetx", explore.DecodeTxPage)
		r.Get("/search", explore.Search)
		r.Get("/charts", explore.Charts)
		r.Get("/ticketpool", explore.Ticketpool)
		r.Get("/stats", explore.StatsPage)
		r.Get("/statistics", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/stats", http.StatusPermanentRedirect)
		})

		// HTTP profiler
		if cfg.HTTPProfile {
			profPath := cfg.HTTPProfPath
			log.Warnf("Starting the HTTP profiler on path %s.", profPath)
			// http pprof uses http.DefaultServeMux
			http.Handle("/", http.RedirectHandler(profPath+"/debug/pprof/", http.StatusSeeOther))
			r.Mount(profPath, http.StripPrefix(profPath, http.DefaultServeMux))
		}
	})

	// Start the web server.
	if err = listenAndServeProto(cfg.APIListen, cfg.APIProto, webMux); err != nil {
		log.Criticalf("listenAndServeProto: %v", err)
		requestShutdown()
	}

	log.Infof("Starting blockchain sync...")

	// Coordinate the sync of both sqlite and auxiliary DBs with the network.
	// This closure captures the RPC client and the quit channel.
	getSyncd := func(updateAddys, updateVotes, newPGInds bool,
		fetchHeightInBaseDB int64) (int64, int64, error) {
		// Simultaneously synchronize the ChainDB (PostgreSQL) and the
		// block/stake info DB (sqlite). Results are returned over channels:
		sqliteSyncRes := make(chan dbtypes.SyncResult)
		pgSyncRes := make(chan dbtypes.SyncResult)

		// Synchronization between DBs via rpcutils.BlockGate
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		// stakedb (in baseDB) connects blocks *after* ChainDB retrieves them,
		// but it has to get a notification channel first to receive them. The
		// BlockGate will provide this for blocks after fetchHeightInBaseDB. In
		// full mode, baseDB will be configured not to send progress updates or
		// chain data to the explorer pages since auxDB will do it.
		baseDB.SyncDBAsync(ctx, sqliteSyncRes, smartClient, fetchHeightInBaseDB,
			latestBlockHash, barLoad)

		// Now that stakedb is either catching up or waiting for a block, start
		// the auxDB sync, which is the master block getter, retrieving and
		// making available blocks to the baseDB. In return, baseDB maintains a
		// StakeDatabase at the best block's height. For a detailed description
		// on how the DBs' synchronization is coordinated, see the documents in
		// db/dcrpg/sync.go.
		go auxDB.SyncChainDBAsync(ctx, pgSyncRes, smartClient,
			updateAddys, updateVotes, newPGInds, latestBlockHash, barLoad)

		// Wait for the results from both of these DBs.
		return waitForSync(ctx, sqliteSyncRes, pgSyncRes, usePG)
	}

	baseDBHeight, auxDBHeight, err := getSyncd(updateAllAddresses,
		updateAllVotes, newPGIndexes, fetchToHeight)
	if err != nil {
		return err
	}

	if usePG {
		// After sync and indexing, must use upsert statement, which checks for
		// duplicate entries and updates instead of erroring. SyncChainDB should
		// set this on successful sync, but do it again anyway.
		auxDB.EnableDuplicateCheckOnInsert(true)
	}

	// The sync routines may have lengthy tasks, such as table indexing, that
	// follow main sync loop. Before enabling the chain monitors, again ensure
	// the DBs are at the node's best block.
	ensureSync := func() error {
		updateAllAddresses, updateAllVotes, newPGIndexes = false, false, false
		_, height, err := dcrdClient.GetBestBlock()
		if err != nil {
			return fmt.Errorf("unable to get block from node: %v", err)
		}

		for baseDBHeight < height {
			fetchToHeight = auxDBHeight + 1
			baseDBHeight, auxDBHeight, err = getSyncd(updateAllAddresses, updateAllVotes,
				newPGIndexes, fetchToHeight)
			if err != nil {
				return err
			}
			_, height, err = dcrdClient.GetBestBlock()
			if err != nil {
				return fmt.Errorf("unable to get block from node: %v", err)
			}
		}
		return nil
	}
	if err = ensureSync(); err != nil {
		return err
	}

	// Exits immediately after the sync completes if SyncAndQuit is to true
	// because all we needed then was the blockchain sync be completed successfully.
	if cfg.SyncAndQuit {
		log.Infof("All ready, at height %d.", baseDBHeight)
		return nil
	}

	// Ensure all side chains known by dcrd are also present in the auxiliary DB
	// and import them if they are not already there.
	if usePG && cfg.ImportSideChains {
		// First identify the side chain blocks that are missing from the DB.
		log.Infof("Initial sync complete, at height %d. "+
			"Now retrieving side chain blocks from dcrd...", baseDBHeight)
		sideChainBlocksToStore, nSideChainBlocks, err := auxDB.MissingSideChainBlocks()
		if err != nil {
			return fmt.Errorf("unable to determine missing side chain blocks: %v", err)
		}
		nSideChains := len(sideChainBlocksToStore)

		// Importing side chain blocks involves only the aux (postgres) DBs
		// since dcrsqlite does not track side chain blocks, and stakedb only
		// supports mainchain. TODO: Get stakedb to work with side chain blocks
		// to get ticket pool info.

		// Collect and store data for each side chain.
		log.Infof("Importing %d new block(s) from %d known side chains...",
			nSideChainBlocks, nSideChains)
		// Disable recomputing project fund balance, and clearing address
		// balance and counts cache.
		auxDB.InBatchSync = true
		var sideChainsStored, sideChainBlocksStored int
		for _, sideChain := range sideChainBlocksToStore {
			// Process this side chain only if there are blocks in it that need
			// to be stored.
			if len(sideChain.Hashes) == 0 {
				continue
			}
			sideChainsStored++

			// Collect and store data for each block in this side chain.
			for _, hash := range sideChain.Hashes {
				// Validate the block hash.
				blockHash, err := chainhash.NewHashFromStr(hash)
				if err != nil {
					log.Errorf("Invalid block hash %s: %v.", hash, err)
					continue
				}

				// Collect block data.
				blockData, msgBlock, err := collector.CollectHash(blockHash)
				if err != nil {
					// Do not quit if unable to collect side chain block data.
					log.Errorf("Unable to collect data for side chain block %s: %v.",
						hash, err)
					continue
				}

				// Get the chainwork
				chainWork, err := rpcutils.GetChainWork(auxDB.Client, blockHash)
				if err != nil {
					log.Errorf("GetChainWork failed (%s): %v", blockHash, err)
					continue
				}

				// SQLite / base DB
				// TODO: Make hash the primary key instead of height, otherwise
				// the main chain block will be overwritten.
				// log.Debugf("Importing block %s (height %d) into base DB.",
				// 	blockHash, msgBlock.Header.Height)

				// blockDataSummary, stakeInfoSummaryExtended := collector.CollectAPITypes(blockHash)
				// if blockDataSummary == nil || stakeInfoSummaryExtended == nil {
				// 	log.Error("Failed to collect data for reorg.")
				// 	continue
				// }
				// if err = baseDB.StoreBlockSummary(blockDataSummary); err != nil {
				// 	log.Errorf("Failed to store block summary data: %v", err)
				// }
				// if err = baseDB.StoreStakeInfoExtended(stakeInfoSummaryExtended); err != nil {
				// 	log.Errorf("Failed to store stake info data: %v", err)
				// }

				// PostgreSQL / aux DB
				log.Debugf("Importing block %s (height %d) into aux DB.",
					blockHash, msgBlock.Header.Height)

				// Stake invalidation is always handled by subsequent block, so
				// add the block as valid. These are all side chain blocks.
				isValid, isMainchain := true, false

				// Existing DB records might be for mainchain and/or valid
				// blocks, so these imported blocks should not data in rows that
				// are conflicting as per the different table constraints and
				// unique indexes.
				updateExistingRecords := false

				// Store data in the aux (dcrpg) DB.
				_, _, _, err = auxDB.StoreBlock(msgBlock, blockData.WinningTickets,
					isValid, isMainchain, updateExistingRecords, true, true, chainWork)
				if err != nil {
					// If data collection succeeded, but storage fails, bail out
					// to diagnose the DB trouble.
					return fmt.Errorf("ChainDBRPC.StoreBlock failed: %v", err)
				}

				sideChainBlocksStored++
			}
		}
		auxDB.InBatchSync = false
		log.Infof("Successfully added %d blocks from %d side chains into dcrpg DB.",
			sideChainBlocksStored, sideChainsStored)

		// That may have taken a while, check again for new blocks from network.
		if err = ensureSync(); err != nil {
			return err
		}
	}

	log.Infof("All ready, at height %d.", baseDBHeight)

	// Deactivate displaying the sync status page after the db sync was completed.
	explore.SetDisplaySyncStatusPage(false)

	// Set that newly sync'd blocks should no longer be stored in the explorer.
	// Monitors that fetch the latest updates from dcrd will be launched next.
	explorer.SetSyncExplorerUpdateStatus(false)

	// Monitors for new blocks, transactions, and reorgs should not run before
	// blockchain syncing and DB indexing completes. If started before then, the
	// DBs will not be prepared to process the notified events. For example, if
	// dcrd notifies of block 200000 while dcrdata has only reached 1000 in
	// batch synchronization, trying to process that block will be impossible as
	// the entire chain before it is not yet processed. Similarly, if we have
	// already registered for notifications with dcrd but the monitors below are
	// not started, notifications will fill up the channels, only to be
	// processed after sync. This is also incorrect since dcrd might notify of a
	// bew block 200000, but the batch sync will process that block on its own,
	// causing this to be a duplicate block by the time the monitors begin
	// pulling data out of the full channels.

	// The following configures and starts handlers that monitor for new blocks,
	// changes in the mempool, and handle chain reorg. It also initiates data
	// collection for the explorer.

	// Blockchain monitor for the collector
	addrMap := make(map[string]txhelpers.TxAction) // for support of watched addresses
	// On reorg, only update web UI since dcrsqlite's own reorg handler will
	// deal with patching up the block info database.
	reorgBlockDataSavers := []blockdata.BlockDataSaver{explore}
	wsChainMonitor := blockdata.NewChainMonitor(ctx, collector, blockDataSavers,
		reorgBlockDataSavers, &wg, addrMap, notify.NtfnChans.ConnectChan,
		notify.NtfnChans.RecvTxBlockChan, notify.NtfnChans.ReorgChanBlockData)

	// Blockchain monitor for the stake DB
	sdbChainMonitor := baseDB.NewStakeDBChainMonitor(ctx, &wg,
		notify.NtfnChans.ConnectChanStakeDB, notify.NtfnChans.ReorgChanStakeDB)

	// Blockchain monitor for the wired sqlite DB
	wiredDBChainMonitor := baseDB.NewChainMonitor(ctx, collector, &wg,
		notify.NtfnChans.ConnectChanWiredDB, notify.NtfnChans.ReorgChanWiredDB)

	var auxDBChainMonitor *dcrpg.ChainMonitor
	if usePG {
		// Blockchain monitor for the aux (PG) DB
		auxDBChainMonitor = auxDB.NewChainMonitor(ctx, &wg,
			notify.NtfnChans.ConnectChanDcrpgDB, notify.NtfnChans.ReorgChanDcrpgDB)
		if auxDBChainMonitor == nil {
			return fmt.Errorf("Failed to enable dcrpg ChainMonitor. *ChainDB is nil.")
		}
	}

	// Setup the synchronous handler functions called by the collectionQueue via
	// OnBlockConnected.
	collectionQueue.SetSynchronousHandlers([]func(*chainhash.Hash){
		sdbChainMonitor.BlockConnectedSync, // 1. Stake DB for pool info
		wsChainMonitor.BlockConnectedSync,  // 2. blockdata for regular block data collection and storage
	})

	// Initial data summary for web ui. stakedb must be at the same height, so
	// we do this before starting the monitors.
	blockData, msgBlock, err := collector.Collect()
	if err != nil {
		return fmt.Errorf("Block data collection for initial summary failed: %v",
			err.Error())
	}

	if err = explore.Store(blockData, msgBlock); err != nil {
		return fmt.Errorf("Failed to store initial block data for explorer pages: %v", err.Error())
	}

	// Register for notifications from dcrd. This also sets the daemon RPC
	// client used by other functions in the notify/notification package (i.e.
	// common ancestor identification in signalReorg).
	cerr := notify.RegisterNodeNtfnHandlers(dcrdClient)
	if cerr != nil {
		return fmt.Errorf("RPC client error: %v (%v)", cerr.Error(), cerr.Cause())
	}

	// After this final node sync check, the monitors will handle new blocks.
	// TODO: make this not racy at all by having sync stop at specified block.
	if err = ensureSync(); err != nil {
		return err
	}

	// Set the current best block in the collection queue so that it can verify
	// that subsequent blocks are in the correct sequence.
	bestHash, bestHeight, err := baseDB.GetBestBlockHeightHash()
	if err != nil {
		return fmt.Errorf("Failed to determine base DB's best block: %v", err)
	}
	collectionQueue.SetPreviousBlock(bestHash, bestHeight)

	// Start the monitors' event handlers.

	// blockdata collector handlers
	wg.Add(2)
	go wsChainMonitor.BlockConnectedHandler()
	// The blockdata reorg handler disables collection during reorg, leaving
	// dcrsqlite to do the switch, except for the last block which gets
	// collected and stored via reorgBlockDataSavers (for the explorer UI).
	go wsChainMonitor.ReorgHandler()

	// StakeDatabase
	wg.Add(2)
	go sdbChainMonitor.BlockConnectedHandler()
	go sdbChainMonitor.ReorgHandler()

	// dcrsqlite does not handle new blocks except during reorg.
	wg.Add(1)
	go wiredDBChainMonitor.ReorgHandler()

	if usePG {
		// dcrpg also does not handle new blocks except during reorg.
		wg.Add(1)
		go auxDBChainMonitor.ReorgHandler()
	}

	explore.StartMempoolMonitor(notify.NtfnChans.ExpNewTxChan)

	if cfg.MonitorMempool {
		// Create the mempool data collector.
		mpoolCollector := mempool.NewMempoolDataCollector(dcrdClient, activeChain)
		if mpoolCollector == nil {
			return fmt.Errorf("Failed to create mempool data collector")
		}

		// Collect and store initial mempool data.
		mpData, err := mpoolCollector.Collect()
		if err != nil {
			return fmt.Errorf("Mempool info collection failed while gathering"+
				" initial data: %v", err.Error())
		}

		// Store initial MP data.
		if err = baseDB.MPC.StoreMPData(mpData, time.Now()); err != nil {
			return fmt.Errorf("Failed to store initial mempool data (wiredDB): %v",
				err.Error())
		}

		// Setup the mempool monitor, which handles notifications of new
		// transactions.
		mpi := &mempool.MempoolInfo{
			CurrentHeight:               mpData.GetHeight(),
			NumTicketPurchasesInMempool: mpData.GetNumTickets(),
			NumTicketsSinceStatsReport:  0,
			LastCollectTime:             time.Now(),
		}

		// Parameters for triggering data collection. See config.go.
		newTicketLimit := int32(cfg.MPTriggerTickets)
		mini := time.Duration(cfg.MempoolMinInterval) * time.Second
		maxi := time.Duration(cfg.MempoolMaxInterval) * time.Second

		mpm := mempool.NewMempoolMonitor(ctx, mpoolCollector, mempoolSavers,
			notify.NtfnChans.NewTxChan, &wg, newTicketLimit, mini, maxi, mpi)
		wg.Add(1)
		go mpm.TxHandler(dcrdClient)
	}

	// Pre-populate charts data now that blocks are sync'd and new-block
	// monitors are running.
	explore.PrepareCharts()

	// Wait for notification handlers to quit.
	wg.Wait()

	return nil
}

func waitForSync(ctx context.Context, base chan dbtypes.SyncResult, aux chan dbtypes.SyncResult, useAux bool) (int64, int64, error) {
	baseRes := <-base
	baseDBHeight := baseRes.Height
	log.Infof("SQLite sync ended at height %d", baseDBHeight)
	if baseRes.Error != nil {
		log.Errorf("dcrsqlite.SyncDBAsync failed at height %d: %v.", baseDBHeight, baseRes.Error)
		requestShutdown()
		auxRes := <-aux
		return baseDBHeight, auxRes.Height, baseRes.Error
	}

	auxRes := <-aux
	auxDBHeight := auxRes.Height
	log.Infof("PostgreSQL sync ended at height %d", auxDBHeight)

	// See if there was a SIGINT (CTRL+C)
	select {
	case <-ctx.Done():
		return baseDBHeight, auxDBHeight, fmt.Errorf("quit signal received during DB sync")
	default:
	}

	if baseRes.Error != nil {
		log.Errorf("dcrsqlite.SyncDBAsync failed at height %d.", baseDBHeight)
		requestShutdown()
		return baseDBHeight, auxDBHeight, baseRes.Error
	}

	if useAux {
		// Check for errors and combine the messages if necessary
		if auxRes.Error != nil {
			requestShutdown()
			if baseRes.Error != nil {
				log.Error("dcrsqlite.SyncDBAsync AND dcrpg.SyncChainDBAsync "+
					"failed at heights %d and %d, respectively.",
					baseDBHeight, auxDBHeight)
				errCombined := fmt.Sprintln(baseRes.Error, ", ", auxRes.Error)
				return baseDBHeight, auxDBHeight, errors.New(errCombined)
			}
			log.Errorf("dcrpg.SyncChainDBAsync failed at height %d.", auxDBHeight)
			return baseDBHeight, auxDBHeight, auxRes.Error
		}

		// DBs must finish at the same height
		if auxDBHeight != baseDBHeight {
			return baseDBHeight, auxDBHeight, fmt.Errorf("failed to hit same"+
				"sync height for PostgreSQL (%d) and SQLite (%d)",
				auxDBHeight, baseDBHeight)
		}
	}
	return baseDBHeight, auxDBHeight, nil
}

func connectNodeRPC(cfg *config, ntfnHandlers *rpcclient.NotificationHandlers) (*rpcclient.Client, semver.Semver, error) {
	return rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdPass,
		cfg.DcrdCert, cfg.DisableDaemonTLS, ntfnHandlers)
}

func listenAndServeProto(listen, proto string, mux http.Handler) error {
	// Try to bind web server
	server := http.Server{
		Addr:         listen,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,  // slow requests should not hold connections opened
		WriteTimeout: 60 * time.Second, // hung responses must die
	}
	errChan := make(chan error)
	if proto == "https" {
		go func() {
			errChan <- server.ListenAndServeTLS("dcrdata.cert", "dcrdata.key")
		}()
	} else {
		go func() {
			errChan <- server.ListenAndServe()
		}()
	}

	// Briefly wait for an error and then return
	t := time.NewTimer(2 * time.Second)
	select {
	case err := <-errChan:
		return fmt.Errorf("Failed to bind web server promptly: %v", err)
	case <-t.C:
		expLog.Infof("Now serving explorer on %s://%v/", proto, listen)
		apiLog.Infof("Now serving API on %s://%v/", proto, listen)
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

	r.With(m.CacheControl(CacheControlMaxAge)).Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	}))
}
