// Copyright (c) 2018-2022, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient/v8"

	"github.com/decred/dcrdata/db/dcrpg/v8"
	"github.com/decred/dcrdata/exchanges/v3"
	"github.com/decred/dcrdata/gov/v6/agendas"
	politeia "github.com/decred/dcrdata/gov/v6/politeia"

	"github.com/decred/dcrdata/v8/blockdata"
	"github.com/decred/dcrdata/v8/db/cache"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/mempool"
	"github.com/decred/dcrdata/v8/pubsub"
	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
	"github.com/decred/dcrdata/v8/rpcutils"
	"github.com/decred/dcrdata/v8/semver"
	"github.com/decred/dcrdata/v8/stakedb"

	"github.com/decred/dcrdata/cmd/dcrdata/internal/api"
	"github.com/decred/dcrdata/cmd/dcrdata/internal/api/insight"
	"github.com/decred/dcrdata/cmd/dcrdata/internal/explorer"
	mw "github.com/decred/dcrdata/cmd/dcrdata/internal/middleware"
	notify "github.com/decred/dcrdata/cmd/dcrdata/internal/notification"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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

// Instead of an rpcutils.AsyncTxClient for NewMempoolDataCollector, we could
// make a simple wrapper to provide txhelpers.VerboseTransactionPromiseGetter:
//
// type mempoolClient struct {
// 	*rpcclient.Client
// }
// func (cl *mempoolClient) GetRawTransactionVerbosePromise(ctx context.Context, txHash *chainhash.Hash) txhelpers.VerboseTxReceiver {
// 	return cl.Client.GetRawTransactionVerboseAsync(ctx, txHash)
// }
// var _ txhelpers.VerboseTransactionPromiseGetter = (*mempoolClient)(nil)

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
	log.Infof("%s version %v (Go version %s)", AppName, Version(), runtime.Version())

	// Grab a Notifier. After all databases are synced, register handlers with
	// the Register*Group methods, set the best block height with
	// SetPreviousBlock and start receiving notifications with Listen. Create
	// the notifier now so the *rpcclient.NotificationHandlers can be obtained,
	// using (*Notifier).DcrdHandlers, for the rpcclient.Client constructor.
	notifier := notify.NewNotifier()

	// Connect to dcrd RPC server using a websocket.
	dcrdClient, nodeVer, err := connectNodeRPC(cfg, notifier.DcrdHandlers())
	if err != nil || dcrdClient == nil {
		return fmt.Errorf("Connection to dcrd failed: %v", err)
	}

	defer func() {
		if dcrdClient != nil {
			log.Infof("Closing connection to dcrd.")
			dcrdClient.Shutdown()
			dcrdClient.WaitForShutdown()
		}
		log.Infof("Bye!")
		time.Sleep(250 * time.Millisecond)
	}()

	// Display connected network (e.g. mainnet, testnet, simnet).
	curnet, err := dcrdClient.GetCurrentNet(ctx)
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

	// Wrap the rpcclient to satisfy the TransactionPromiseGetter and
	// VerboseTransactionPromiseGetter interfaces in txhelpers. Both stakedb and
	// mempool packages use this rather than require an actual rpcclient.Client.
	promiseClient := rpcutils.NewAsyncTxClient(dcrdClient)

	// StakeDatabase
	stakeDB, stakeDBHeight, err := stakedb.NewStakeDatabase(promiseClient, activeChain, cfg.DataDir)
	if err != nil {
		log.Errorf("Unable to create stake DB: %v", err)
		if stakeDBHeight >= 0 {
			log.Infof("Attempting to recover stake DB...")
			stakeDB, err = stakedb.LoadAndRecover(promiseClient, activeChain, cfg.DataDir, stakeDBHeight-288)
			stakeDBHeight = int64(stakeDB.Height())
		}
		if err != nil {
			if stakeDB != nil {
				_ = stakeDB.Close()
			}
			return fmt.Errorf("StakeDatabase recovery failed: %v", err)
		}
	}
	defer stakeDB.Close()

	log.Infof("Loaded StakeDatabase at height %d", stakeDBHeight)

	// Main chain DB
	var newPGIndexes, updateAllAddresses bool
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

	// If using {netname} then replace it with activeNet.Name.
	dbi.DBName = strings.Replace(dbi.DBName, "{netname}", activeNet.Name, -1)

	// Rough estimate of capacity in rows, using size of struct plus some
	// for the string buffer of the Address field.
	rowCap := cfg.AddrCacheCap / int(32+reflect.TypeOf(dbtypes.AddressRowCompact{}).Size())
	log.Infof("Address cache capacity: %d addresses: ~%.0f MiB tx data (%d items) + %.0f MiB UTXOs",
		cfg.AddrCacheLimit, float64(cfg.AddrCacheCap)/1024/1024, rowCap, float64(cfg.AddrCacheUXTOCap)/1024/1024)

	// Open and upgrade the database.
	dbCfg := dcrpg.ChainDBCfg{
		DBi:                  &dbi,
		Params:               activeChain,
		DevPrefetch:          !cfg.NoDevPrefetch,
		HidePGConfig:         cfg.HidePGConfig,
		AddrCacheAddrCap:     cfg.AddrCacheLimit,
		AddrCacheRowCap:      rowCap,
		AddrCacheUTXOByteCap: cfg.AddrCacheUXTOCap,
	}

	mpChecker := rpcutils.NewMempoolAddressChecker(dcrdClient, activeChain)
	chainDB, err := dcrpg.NewChainDB(ctx, &dbCfg,
		stakeDB, mpChecker, dcrdClient, requestShutdown)
	if chainDB != nil {
		defer chainDB.Close()
	}
	if err != nil {
		return fmt.Errorf("Failed to connect to PostgreSQL: %w", err)
	}

	if cfg.DropIndexes {
		log.Info("Dropping all table indexing and quitting...")
		err = chainDB.DeindexAll()
		requestShutdown()
		return err
	}

	// Check for missing indexes.
	missingIndexes, descs, err := chainDB.MissingIndexes()
	if err != nil {
		return err
	}

	// If any indexes are missing, forcibly drop any existing indexes, and
	// create them all after block sync.
	if len(missingIndexes) > 0 {
		newPGIndexes = true
		updateAllAddresses = true
		// Warn if this is not a fresh sync.
		if chainDB.Height() > 0 {
			log.Warnf("Some table indexes not found!")
			for im, mi := range missingIndexes {
				log.Warnf(` - Missing Index "%s": "%s"`, mi, descs[im])
			}
			log.Warnf("Forcing new index creation and addresses table spending info update.")
		}
	}

	// Heights gets the current height of the DB and the chain server.
	Heights := func() (nodeHeight, chainDBHeight int64, err error) {
		_, nodeHeight, err = dcrdClient.GetBestBlock(ctx)
		if err != nil {
			err = fmt.Errorf("unable to get block from node: %w", err)
			return
		}

		chainDBHeight, err = chainDB.HeightDB()
		if err != nil {
			err = fmt.Errorf("chainDB.HeightDB failed: %w", err)
			return
		}
		if chainDBHeight == -1 {
			log.Infof("chainDB block summary table is empty.")
		}
		log.Debugf("chainDB height: %d", chainDBHeight)

		return
	}

	// Check for database tip blocks that have been orphaned. If any are found,
	// purge blocks to get to a common ancestor. Only message when purging more
	// than requested in the configuration settings.
	blocksToPurge := int64(cfg.PurgeNBestBlocks)
	_, chainDBHeight, err := Heights()
	if err != nil {
		return fmt.Errorf("Failed to get Heights for tip check: %w", err)
	}

	if chainDBHeight > -1 {
		orphaned, err := rpcutils.OrphanedTipLength(ctx, dcrdClient, chainDBHeight, chainDB.BlockHash)
		if err != nil {
			return fmt.Errorf("Failed to compare tip blocks for the DB: %w", err)
		}
		if orphaned > blocksToPurge {
			blocksToPurge = orphaned
			log.Infof("Orphaned tip detected in DB. Purging %d blocks", blocksToPurge)
		}
	}

	// Give a chance to abort a purge.
	if shutdownRequested(ctx) {
		return nil
	}

	if blocksToPurge > 0 {
		purgeToBlock := chainDBHeight - blocksToPurge
		log.Infof("Purging PostgreSQL data for the %d best blocks back to %d...", blocksToPurge, purgeToBlock)
		s, heightDB, err := chainDB.PurgeBestBlocks(blocksToPurge)
		if err != nil {
			return fmt.Errorf("failed to purge %d blocks from PostgreSQL: %w", blocksToPurge, err)
		}
		if s != nil {
			log.Infof("Successfully purged data for %d blocks from PostgreSQL "+
				"(new height = %d):\n%v", s.Blocks, heightDB, s)
		} // otherwise likely dbtypes.ErrNoResult (heightDB was already -1)
	}

	// Get the last block added to the DB.
	lastBlockPG, err := chainDB.HeightDB()
	if err != nil {
		return fmt.Errorf("Unable to get height from PostgreSQL DB: %v", err)
	}

	// For consistency with StakeDatabase, a non-negative height is needed.
	heightDB := lastBlockPG
	if heightDB < 0 {
		heightDB = 0
	}

	charts := cache.NewChartData(ctx, uint32(heightDB), activeChain)
	chainDB.RegisterCharts(charts)

	// DB height and stakedb height must be equal. StakeDatabase will catch up
	// automatically if it is behind, but we must rewind it here if it is ahead
	// of chainDB. For chainDB to receive notification from StakeDatabase when
	// the required blocks are connected, the StakeDatabase must be at the same
	// height or lower than chainDB.
	stakeDBHeight = int64(stakeDB.Height())
	if stakeDBHeight > heightDB {
		// Have chainDB rewind it's the StakeDatabase. stakeDBHeight is
		// always rewound to a height of zero even when lastBlockPG is -1,
		// hence we rewind to heightDB.
		log.Infof("Rewinding StakeDatabase from block %d to %d.",
			stakeDBHeight, heightDB)
		stakeDBHeight, err = chainDB.RewindStakeDB(ctx, heightDB)
		if err != nil {
			return fmt.Errorf("RewindStakeDB failed: %v", err)
		}

		// Verify that the StakeDatabase is at the intended height.
		if stakeDBHeight != heightDB {
			return fmt.Errorf("failed to rewind stakedb: got %d, expecting %d",
				stakeDBHeight, heightDB)
		}
	}

	// TODO: just use getblockchaininfo to see if it still syncing and what
	// height the network's best block is at.
	blockHash, nodeHeight, err := dcrdClient.GetBestBlock(ctx)
	if err != nil {
		return fmt.Errorf("Unable to get block from node: %v", err)
	}

	block, err := dcrdClient.GetBlockHeader(ctx, blockHash)
	if err != nil {
		return fmt.Errorf("unable to fetch the block from the node: %v", err)
	}

	// bestBlockAge is the time since the dcrd best block was mined.
	bestBlockAge := time.Since(block.Timestamp).Minutes()

	// Since mining a block take approximately ChainParams.TargetTimePerBlock then the
	// expected height of the best block from dcrd now should be this.
	expectedHeight := int64(bestBlockAge/float64(activeChain.TargetTimePerBlock)) + nodeHeight

	// Estimate how far chainDB is behind the node.
	blocksBehind := expectedHeight - lastBlockPG
	if blocksBehind < 0 {
		return fmt.Errorf("Node is still syncing. Node height = %d, "+
			"DB height = %d", expectedHeight, heightDB)
	}

	// PG gets winning tickets out of baseDB's pool info cache, so it must
	// be big enough to hold the needed blocks' info, and charged with the
	// data from disk. The cache is updated on each block connect.
	tpcSize := int(blocksBehind) + 200
	log.Debugf("Setting ticket pool cache capacity to %d blocks", tpcSize)
	err = stakeDB.SetPoolCacheCapacity(tpcSize)
	if err != nil {
		return err
	}

	// Charge stakedb pool info cache, including previous PG blocks.
	if err = chainDB.ChargePoolInfoCache(heightDB - 2); err != nil {
		return fmt.Errorf("Failed to charge pool info cache: %v", err)
	}

	// Block data collector. Needs a StakeDatabase too.
	collector := blockdata.NewCollector(dcrdClient, activeChain, stakeDB)
	if collector == nil {
		return fmt.Errorf("Failed to create block data collector")
	}

	// Build a slice of each required saver type for each data source.
	blockDataSavers := []blockdata.BlockDataSaver{chainDB}

	mempoolSavers := []mempool.MempoolDataSaver{chainDB.MPC} // mempool.DataCache

	// Allow Ctrl-C to halt startup here.
	if shutdownRequested(ctx) {
		return nil
	}

	// WaitGroup for monitoring goroutines
	var wg sync.WaitGroup

	// ExchangeBot
	var xcBot *exchanges.ExchangeBot
	if cfg.EnableExchangeBot && activeChain.Name != "mainnet" {
		log.Warnf("disabling exchange monitoring. only available on mainnet")
		cfg.EnableExchangeBot = false
	}
	if cfg.EnableExchangeBot {
		botCfg := exchanges.ExchangeBotConfig{
			BtcIndex:       cfg.ExchangeCurrency,
			MasterBot:      cfg.RateMaster,
			MasterCertFile: cfg.RateCertificate,
		}
		if cfg.DisabledExchanges != "" {
			botCfg.Disabled = strings.Split(cfg.DisabledExchanges, ",")
		}
		xcBot, err = exchanges.NewExchangeBot(&botCfg)
		if err != nil {
			log.Errorf("Could not create exchange monitor. Exchange info will be disabled: %v", err)
		} else {
			var xcList, prepend string
			for k := range xcBot.Exchanges {
				xcList += prepend + k
				prepend = ", "
			}
			log.Infof("ExchangeBot monitoring %s", xcList)
			wg.Add(1)
			go xcBot.Start(ctx, &wg)
		}
	}

	// Creates a new or loads an existing agendas db instance that helps to
	// store and retrieves agendas data. Agendas votes are On-Chain
	// transactions that appear in the decred blockchain. If corrupted data is
	// is found, its deleted pending the data update that restores valid data.
	var agendaDB *agendas.AgendaDB
	agendaDB, err = agendas.NewAgendasDB(
		dcrdClient, filepath.Join(cfg.DataDir, cfg.AgendasDBFileName))
	if err != nil {
		return fmt.Errorf("failed to create new agendas db instance: %v", err)
	}

	// Creates a new or loads an existing proposals db instance that stores and
	// retrieves data from politeia and is used by dcrdata.
	proposalsDB, err := politeia.NewProposalsDB(cfg.PoliteiaURL,
		filepath.Join(cfg.DataDir, cfg.ProposalsFileName))
	if err != nil {
		return fmt.Errorf("failed to create new proposals db instance: %v", err)
	}

	// A vote tracker tracks current block and stake versions and votes. Only
	// initialize the vote tracker if not on simnet. nil tracker is a sentinel
	// value throughout.
	var tracker *agendas.VoteTracker
	if !cfg.SimNet {
		tracker, err = agendas.NewVoteTracker(activeChain, dcrdClient,
			chainDB.AgendaVoteCounts)
		if err != nil {
			return fmt.Errorf("Unable to initialize vote tracker: %v", err)
		}
	}

	// Create the explorer system.
	explore := explorer.New(&explorer.ExplorerConfig{
		DataSource:    chainDB,
		ChartSource:   charts,
		UseRealIP:     cfg.UseRealIP,
		AppVersion:    Version(),
		DevPrefetch:   !cfg.NoDevPrefetch,
		Viewsfolder:   "views",
		XcBot:         xcBot,
		AgendasSource: agendaDB,
		Tracker:       tracker,
		Proposals:     proposalsDB,
		PoliteiaURL:   cfg.PoliteiaURL,
		MainnetLink:   cfg.MainnetLink,
		TestnetLink:   cfg.TestnetLink,
		ReloadHTML:    cfg.ReloadHTML,
		OnionAddress:  cfg.OnionAddress,
	})
	// TODO: allow views config
	if explore == nil {
		return fmt.Errorf("failed to create new explorer (templates missing?)")
	}
	explore.UseSIGToReloadTemplates()
	defer explore.StopWebsocketHub()

	// Create the pub sub hub.
	psHub, err := pubsub.NewPubSubHub(chainDB)
	if err != nil {
		return fmt.Errorf("failed to create new pubsubhub: %v", err)
	}
	defer psHub.StopWebsocketHub()

	blockDataSavers = append(blockDataSavers, psHub)
	mempoolSavers = append(mempoolSavers, psHub) // individual transactions are from mempool monitor

	// Store explorerUI data after pubsubhub.
	blockDataSavers = append(blockDataSavers, explore)
	mempoolSavers = append(mempoolSavers, explore)

	// Block certain updates in explorer and pubsubhub during sync.
	explore.SetDBsSyncing(true)
	psHub.SetReady(false)

	// Create the mempool data collector.
	mpoolCollector := mempool.NewDataCollector(promiseClient, activeChain)
	if mpoolCollector == nil {
		// Shutdown goroutines.
		requestShutdown()
		return fmt.Errorf("Failed to create mempool data collector")
	}

	// The MempoolMonitor receives notifications of new transactions on
	// notify.NtfnChans.NewTxChan, and of new blocks on the same channel with a
	// nil transaction message. The mempool monitor will process the
	// transactions, and forward new ones on via the mpDataToPSHub with an
	// appropriate signal to the underlying WebSocketHub on signalToPSHub.
	signalToPSHub := psHub.HubRelay()
	signalToExplorer := explore.MempoolSignal()
	mempoolSigOuts := []chan<- pstypes.HubMessage{signalToPSHub, signalToExplorer}
	mpm, err := mempool.NewMempoolMonitor(ctx, mpoolCollector, mempoolSavers,
		activeChain, mempoolSigOuts, true)

	// Ensure the initial collect/store succeeded.
	if err != nil {
		// Shutdown goroutines.
		requestShutdown()
		return fmt.Errorf("NewMempoolMonitor: %v", err)
	}

	// Use the MempoolMonitor in DB to get unconfirmed transaction data.
	chainDB.UseMempoolChecker(mpm)

	// Prepare for sync by setting up the channels for status/progress updates
	// (barLoad) or full explorer page updates (latestBlockHash).

	// barLoad is used to send sync status updates to websocket clients (e.g.
	// browsers with the status page opened) via the goroutines launched by
	// BeginSyncStatusUpdates.
	var barLoad chan *dbtypes.ProgressBarLoad

	// latestBlockHash communicates the hash of block most recently processed
	// during synchronization. This is done if all of the explorer pages (not
	// just the status page) are to be served during sync.
	var latestBlockHash chan *chainhash.Hash

	// Display the blockchain syncing status page if the number of blocks behind
	// the node's best block height are more than the set limit. The sync status
	// page should also be displayed when updateAllAddresses and newPGIndexes
	// are true, indicating maintenance or an initial sync.
	nodeHeight, chainDBHeight, err = Heights()
	if err != nil {
		return fmt.Errorf("Heights failed: %v", err)
	}
	blocksBehind = nodeHeight - chainDBHeight
	log.Debugf("dbHeight: %d / blocksBehind: %d", chainDBHeight, blocksBehind)
	displaySyncStatusPage := blocksBehind >= int64(cfg.SyncStatusLimit) || // over limit
		updateAllAddresses || newPGIndexes // maintenance or initial sync

	// Initiate the sync status monitor and the coordinating goroutines if the
	// sync status is activated, otherwise coordinate updating the full set of
	// explorer pages.
	if displaySyncStatusPage {
		// Start goroutines that keep the update the shared progress bar data,
		// and signal the websocket hub to send progress updates to clients.
		barLoad = make(chan *dbtypes.ProgressBarLoad, 2)
		explore.BeginSyncStatusUpdates(barLoad)
	} else {
		// Start a goroutine to update the explorer pages when the DB sync
		// functions send a new block hash on the following channel.
		latestBlockHash = make(chan *chainhash.Hash, 2)

		// The BlockConnected handler should not be started until after sync.
		go func() {
			// Keep receiving updates until the channel is closed, or a nil Hash
			// pointer received.
			for hash := range latestBlockHash {
				if hash == nil {
					return
				}
				// Fetch the blockdata by block hash.
				d, msgBlock, err := collector.CollectHash(hash)
				if err != nil {
					log.Warnf("failed to fetch blockdata for (%s) hash. error: %v",
						hash.String(), err)
					continue
				}

				// Store the blockdata for the explorer pages.
				if err = explore.Store(d, msgBlock); err != nil {
					log.Warnf("failed to store (%s) hash's blockdata for the explorer pages error: %v",
						hash.String(), err)
				}
			}
		}()

		// Before starting the DB sync, trigger the explorer to display data for
		// the current best block.

		// Retrieve the hash of the best block across every DB.
		latestDBBlockHash, err := dcrdClient.GetBlockHash(ctx, chainDBHeight)
		if err != nil {
			return fmt.Errorf("failed to fetch the block at height (%d): %v",
				chainDBHeight, err)
		}

		// Signal to load this block's data into the explorer. Future signals
		// will come from the sync methods of ChainDB.
		latestBlockHash <- latestDBBlockHash
	}

	// Create the Insight socket.io server, and add it to block savers if in
	// full/pg mode. Since insightSocketServer is added into the url before even
	// the sync starts, this implementation cannot be moved to
	// initiateHandlersAndCollectBlocks function.
	insightSocketServer, err := insight.NewSocketServer(activeChain, dcrdClient)
	if err != nil {
		return fmt.Errorf("Could not create Insight socket.io server: %v", err)
	}
	defer insightSocketServer.Close()
	blockDataSavers = append(blockDataSavers, insightSocketServer)

	// Start dcrdata's JSON web API.
	app := api.NewContext(&api.AppContextConfig{
		Client:            dcrdClient,
		Params:            activeChain,
		DataSource:        chainDB,
		XcBot:             xcBot,
		AgendasDBInstance: agendaDB,
		ProposalsDB:       proposalsDB,
		MaxAddrs:          cfg.MaxCSVAddrs,
		Charts:            charts,
	})
	// Start the notification hander for keeping /status up-to-date.
	wg.Add(1)
	go app.StatusNtfnHandler(ctx, &wg, chainDB.UpdateChan())
	// Initial setting of DBHeight. Subsequently, Store() will send this.
	if chainDBHeight >= 0 {
		// Do not sent 4294967295 = uint32(-1) if there are no blocks.
		chainDB.SignalHeight(uint32(chainDBHeight))
	}

	// Configure the URL path to http handler router for the API.
	apiMux := api.NewAPIRouter(app, cfg.IndentJSON, cfg.UseRealIP, cfg.CompressAPI)

	// File downloads piggy-back on the API.
	fileMux := api.NewFileRouter(app, cfg.UseRealIP)

	// Configure the explorer web pages router.
	webMux := chi.NewRouter()
	if cfg.ServerHeader != "" {
		log.Debugf("Using Server HTTP response header %q", cfg.ServerHeader)
		webMux.Use(mw.Server(cfg.ServerHeader))
	}

	// Request per sec limit for "POST /verify-message" endpoint.
	reqPerSecLimit := 5.0
	// Create a rate limiter struct.
	limiter := mw.NewLimiter(reqPerSecLimit)
	limiter.SetMessage(fmt.Sprintf(
		"You have reached the maximum request limit (%g req/s)", reqPerSecLimit))

	if cfg.UseRealIP {
		webMux.Use(middleware.RealIP)
		// RealIP sets RemoteAddr
		limiter.SetIPLookups([]string{"RemoteAddr"})
	} else {
		limiter.SetIPLookups([]string{"X-Forwarded-For", "X-Real-IP", "RemoteAddr"})
	}

	webMux.Use(middleware.Recoverer)
	webMux.Use(mw.RequestBodyLimiter(1 << 21)) // 2 MiB, down from 10 MiB default
	if cfg.TrustProxy {                        // try to determine actual request scheme and host from x-forwarded-{proto,host} headers
		webMux.Use(explorer.ProxyHeaders)
	}
	if len(cfg.AllowedHosts) > 0 {
		webMux.Use(explorer.AllowedHosts(cfg.AllowedHosts))
	}

	webMux.With(explore.SyncStatusPageIntercept).Group(func(r chi.Router) {
		r.Get("/", explore.Home)
		r.Get("/visualblocks", explore.VisualBlocks)
	})
	webMux.Get("/ws", explore.RootWebsocket)
	webMux.Get("/ps", psHub.WebSocketHandler)

	// Make the static assets available under a path with the given prefix.
	mountAssetPaths := func(pathPrefix string) {
		if !strings.HasSuffix(pathPrefix, "/") {
			pathPrefix += "/"
		}

		webMux.Get(pathPrefix+"favicon.ico", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./public/images/favicon/favicon.ico")
		})

		cacheControlMaxAge := int64(cfg.CacheControlMaxAge)
		FileServer(webMux, pathPrefix+"js", "./public/js", cacheControlMaxAge)
		FileServer(webMux, pathPrefix+"css", "./public/css", cacheControlMaxAge)
		FileServer(webMux, pathPrefix+"fonts", "./public/fonts", cacheControlMaxAge)
		FileServer(webMux, pathPrefix+"images", "./public/images", cacheControlMaxAge)
		FileServer(webMux, pathPrefix+"dist", "./public/dist", cacheControlMaxAge)
	}
	// Mount under root (e.g. /js, /css, etc.).
	mountAssetPaths("/")

	// HTTP profiler
	if cfg.HTTPProfile {
		profPath := cfg.HTTPProfPath
		log.Warnf("Starting the HTTP profiler on path %s.", profPath)
		// http pprof uses http.DefaultServeMux
		http.Handle("/", http.RedirectHandler(profPath+"/debug/pprof/", http.StatusSeeOther))
		webMux.Mount(profPath, http.StripPrefix(profPath, http.DefaultServeMux))
	}

	// SyncStatusAPIIntercept returns a json response if the sync status page is
	// enabled (no the full explorer while syncing).
	webMux.With(explore.SyncStatusAPIIntercept).Group(func(r chi.Router) {
		// Mount the dcrdata's REST API.
		r.Mount("/api", apiMux.Mux)
		// Setup and mount the Insight API.
		insightApp := insight.NewInsightAPI(dcrdClient, chainDB,
			activeChain, mpm, cfg.IndentJSON, app.Status)
		insightApp.SetReqRateLimit(cfg.InsightReqRateLimit)
		insightMux := insight.NewInsightAPIRouter(insightApp, cfg.UseRealIP,
			cfg.CompressAPI, cfg.MaxCSVAddrs)
		r.Mount("/insight/api", insightMux.Mux)

		if insightSocketServer != nil {
			r.With(mw.NoOrigin).Get("/insight/socket.io/", insightSocketServer.ServeHTTP)
		}
	})

	// HTTP Error 503 StatusServiceUnavailable for file requests before sync.
	webMux.With(explore.SyncStatusFileIntercept).Group(func(r chi.Router) {
		r.Mount("/download", fileMux.Mux)
	})

	webMux.With(explore.SyncStatusPageIntercept).Group(func(r chi.Router) {
		r.NotFound(explore.NotFound)

		r.Mount("/explorer", explore.Mux) // legacy
		r.Get("/days", explore.DayBlocksListing)
		r.Get("/weeks", explore.WeekBlocksListing)
		r.Get("/months", explore.MonthBlocksListing)
		r.Get("/years", explore.YearBlocksListing)
		r.Get("/blocks", explore.Blocks)
		r.Get("/ticketpricewindows", explore.StakeDiffWindows)
		r.Get("/side", explore.SideChains)
		r.Get("/rejects", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/disapproved", http.StatusPermanentRedirect)
		})
		r.Get("/disapproved", explore.DisapprovedBlocks)
		r.Get("/mempool", explore.Mempool)
		r.Get("/parameters", explore.ParametersPage)
		r.With(explore.BlockHashPathOrIndexCtx).Get("/block/{blockhash}", explore.Block)
		r.With(explorer.TransactionHashCtx).Get("/tx/{txid}", explore.TxPage)
		r.With(explorer.TransactionHashCtx, explorer.TransactionIoIndexCtx).Get("/tx/{txid}/{inout}/{inoutid}", explore.TxPage)
		r.With(explorer.AddressPathCtx).Get("/address/{address}", explore.AddressPage)
		r.With(explorer.AddressPathCtx).Get("/addresstable/{address}", explore.AddressTable)
		r.Get("/treasury", explore.TreasuryPage)
		r.Get("/treasurytable", explore.TreasuryTable)
		r.Get("/agendas", explore.AgendasPage)
		r.With(explorer.AgendaPathCtx).Get("/agenda/{agendaid}", explore.AgendaPage)
		r.Get("/proposals", explore.ProposalsPage)
		r.With(explorer.ProposalPathCtx).Get("/proposal/{proposaltoken}", explore.ProposalPage)
		r.Get("/decodetx", explore.DecodeTxPage)
		r.Get("/search", explore.Search)
		r.Get("/charts", explore.Charts)
		r.Get("/ticketpool", explore.Ticketpool)
		r.Get("/stats", explore.StatsPage)
		r.Get("/market", explore.MarketPage)
		r.Get("/statistics", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/stats", http.StatusPermanentRedirect)
		})
		// MenuFormParser will typically redirect, but going to the homepage as a
		// fallback.
		r.With(explorer.MenuFormParser).Post("/set", explore.Home)
		r.Get("/attack-cost", explore.AttackCost)
		r.Get("/verify-message", explore.VerifyMessagePage)
		r.Get("/stakingcalc", explore.StakeRewardCalcPage)
		r.With(mw.Tollbooth(limiter)).Post("/verify-message", explore.VerifyMessageHandler)
	})

	// Configure a page for the bare "/insight" path. This mounts the static
	// assets under /insight (e.g. /insight/js) to support the page's complete
	// loading when the root mounter is not accessible, such as the case in
	// certain reverse proxy configurations that map /insight as the root path.
	webMux.With(mw.OriginalRequestURI).Get("/insight", explore.InsightRootPage)
	// Serve static assets under /insight for when the a reverse proxy prefixes
	// all requests with "/insight". (e.g. /insight/js, /insight/css, etc.).
	mountAssetPaths("/insight")

	// Start the web server.
	listenAndServeProto(ctx, &wg, cfg.APIListen, cfg.APIProto, webMux)

	// Last chance to quit before syncing if the web server could not start.
	if shutdownRequested(ctx) {
		return nil
	}

	log.Infof("Starting blockchain sync...")

	syncChainDB := func() (int64, error) {
		// Use the plain rpcclient.Client or a rpcutils.BlockPrefetchClient.
		var bf rpcutils.BlockFetcher
		if cfg.NoBlockPrefetch {
			bf = dcrdClient
		} else {
			pfc := rpcutils.NewBlockPrefetchClient(dcrdClient)
			defer func() {
				pfc.Stop()
				log.Debugf("Block prefetcher hits = %d, misses = %d.",
					pfc.Hits(), pfc.Misses())
			}()
			bf = pfc
		}

		// Now that stakedb is either catching up or waiting for a block, start
		// the chainDB sync, which is the master block getter, retrieving and
		// making available blocks to the baseDB. In return, baseDB maintains a
		// StakeDatabase at the best block's height. For a detailed description
		// on how the DBs' synchronization is coordinated, see the documents in
		// db/dcrpg/sync.go.
		height, err := chainDB.SyncChainDB(ctx, bf, updateAllAddresses,
			newPGIndexes, latestBlockHash, barLoad)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				requestShutdown()
			}
			log.Errorf("dcrpg.SyncChainDB failed at height %d.", height)
			return height, err
		}
		app.Status.SetHeight(uint32(height))
		return height, nil
	}

	chainDBHeight, err = syncChainDB()
	if err != nil {
		return err
	}

	// After sync and indexing, must use upsert statement, which checks for
	// duplicate entries and updates instead of erroring. SyncChainDB should
	// set this on successful sync, but do it again anyway.
	chainDB.EnableDuplicateCheckOnInsert(true)

	// Ensure all side chains known by dcrd are also present in the DB and
	// import them if they are not already there.
	if cfg.ImportSideChains {
		// First identify the side chain blocks that are missing from the DB.
		log.Info("Retrieving side chain blocks from dcrd...")
		sideChainBlocksToStore, nSideChainBlocks, err := chainDB.MissingSideChainBlocks()
		if err != nil {
			return fmt.Errorf("Unable to determine missing side chain blocks: %v", err)
		}
		nSideChains := len(sideChainBlocksToStore)

		// Importing side chain blocks involves only the aux (postgres) DBs
		// since stakedb only supports mainchain. TODO: Get stakedb to work with
		// side chain blocks to get ticket pool info.

		// Collect and store data for each side chain.
		log.Infof("Importing %d new block(s) from %d known side chains...",
			nSideChainBlocks, nSideChains)
		// Disable recomputing project fund balance, and clearing address
		// balance and counts cache.
		chainDB.InBatchSync = true
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
				_, msgBlock, err := collector.CollectHash(blockHash)
				if err != nil {
					// Do not quit if unable to collect side chain block data.
					log.Errorf("Unable to collect data for side chain block %s: %v.",
						hash, err)
					continue
				}

				// Get the chainwork
				chainWork, err := rpcutils.GetChainWork(chainDB.Client, blockHash)
				if err != nil {
					log.Errorf("GetChainWork failed (%s): %v", blockHash, err)
					continue
				}

				// Main DB
				log.Debugf("Importing block %s (height %d) into DB.",
					blockHash, msgBlock.Header.Height)

				// Stake invalidation is always handled by subsequent block, so
				// add the block as valid. These are all side chain blocks.
				isValid, isMainchain := true, false

				// Existing DB records might be for mainchain and/or valid
				// blocks, so these imported blocks should not data in rows that
				// are conflicting as per the different table constraints and
				// unique indexes.
				updateExistingRecords := false

				// Store data in the DB.
				_, _, _, err = chainDB.StoreBlock(msgBlock, isValid, isMainchain,
					updateExistingRecords, true, chainWork)
				if err != nil {
					// If data collection succeeded, but storage fails, bail out
					// to diagnose the DB trouble.
					return fmt.Errorf("ChainDB.StoreBlock failed: %w", err)
				}

				sideChainBlocksStored++
			}
		}
		chainDB.InBatchSync = false
		log.Infof("Successfully added %d blocks from %d side chains into dcrpg DB.",
			sideChainBlocksStored, sideChainsStored)
	}

	// Exits immediately after the sync completes if SyncAndQuit is to true
	// because all we needed then was the blockchain sync be completed successfully.
	if cfg.SyncAndQuit {
		log.Infof("All ready, at height %d. Quitting.", chainDBHeight)
		return nil
	}

	// Pre-populate charts data using the dumped cache data in the .gob file
	// path provided instead of querying the data from the dbs. Should be
	// invoked before explore.Store to avoid double charts data cache
	// population. This charts pre-population is faster than db querying and can
	// be done before the monitors are fully set up.
	dumpPath := filepath.Join(cfg.DataDir, cfg.ChartsCacheDump)
	if err = charts.Load(dumpPath); err != nil {
		log.Warnf("Failed to load charts data cache: %v", err)
	} else {
		explore.ChartsUpdated()
	}
	// Dump the cache charts data into a file for future use on system exit.
	defer charts.Dump(dumpPath)

	// Add charts saver method after explorer and database stores. This may run
	// asynchronously.
	blockDataSavers = append(blockDataSavers, blockdata.BlockTrigger{
		Async: true,
		Saver: func(hash string, height uint32) error {
			if err := charts.TriggerUpdate(hash, height); err != nil {
				return err
			}
			explore.ChartsUpdated()
			return nil
		},
	})

	// Block further usage of the barLoad by sending a nil value
	if barLoad != nil {
		select {
		case barLoad <- nil:
		default:
		}
	}

	// Set that newly sync'd blocks should no longer be stored in the explorer.
	// Monitors that fetch the latest updates from dcrd will be launched next.
	if latestBlockHash != nil {
		close(latestBlockHash)
	}

	// The proposals and agenda db updates are run after the db indexing.
	// Retrieve blockchain deployment updates and add them to the agendas db.
	if err = agendaDB.UpdateAgendas(); err != nil {
		return fmt.Errorf("updating agendas db failed: %v", err)
	}

	// Retrieve updates and newly added proposals from Politeia and store them
	// on our stormdb. This call is made asynchronously to not block execution
	// while the proposals db is syncing.
	log.Info("Syncing proposals data with Politeia...")
	go func() {
		if err := proposalsDB.ProposalsSync(); err != nil {
			log.Errorf("updating proposals db failed: %v", err)
		}
	}()

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
	// On reorg, only update web UI since the DB's own reorg handler will
	// deal with patching up the block info database.
	reorgBlockDataSavers := []blockdata.BlockDataSaver{explore}
	bdChainMonitor := blockdata.NewChainMonitor(ctx, collector, blockDataSavers,
		reorgBlockDataSavers)

	// Blockchain monitor for the stake DB
	sdbChainMonitor := stakeDB.NewChainMonitor(ctx)

	// Blockchain monitor for the main DB
	chainDBChainMonitor := chainDB.NewChainMonitor(ctx)
	if chainDBChainMonitor == nil {
		return fmt.Errorf("failed to enable dcrpg ChainMonitor")
	}

	// Notifications are sequenced by adding groups of notification handlers.
	// The groups are run sequentially, but the handlers within a group are run
	// concurrently. For example, register(A); register(B, C) will result in A
	// running alone and completing, then B and C running concurrently.
	notifier.RegisterBlockHandlerGroup(sdbChainMonitor.ConnectBlock)
	notifier.RegisterBlockHandlerGroup(bdChainMonitor.ConnectBlock)
	notifier.RegisterBlockHandlerLiteGroup(app.UpdateNodeHeight, mpm.BlockHandler)
	notifier.RegisterReorgHandlerGroup(sdbChainMonitor.ReorgHandler)
	notifier.RegisterReorgHandlerGroup(bdChainMonitor.ReorgHandler, chainDBChainMonitor.ReorgHandler)
	notifier.RegisterReorgHandlerGroup(charts.ReorgHandler) // snip charts data
	notifier.RegisterTxHandlerGroup(mpm.TxHandler, insightSocketServer.SendNewTx)

	// After this final node sync check, the monitors will handle new blocks.
	// TODO: make this not racy at all by having notifiers register first, but
	// enable operation on signal of sync complete.
	nodeHeight, chainDBHeight, err = Heights()
	if err != nil {
		return fmt.Errorf("Heights failed: %w", err)
	}
	if nodeHeight != chainDBHeight {
		log.Infof("Initial chain DB sync complete. Now catching up with network...")
		newPGIndexes, updateAllAddresses = false, false
		chainDBHeight, err = syncChainDB()
		if err != nil {
			return err
		}
	}

	// Set the current best block in the collection queue so that it can verify
	// that subsequent blocks are in the correct sequence.
	bestHash, bestHeight := chainDB.BestBlock()
	notifier.SetPreviousBlock(*bestHash, uint32(bestHeight))

	// Register for notifications from dcrd. This also sets the daemon RPC
	// client used by other functions in the notify/notification package (i.e.
	// common ancestor identification in processReorg).
	cerr := notifier.Listen(ctx, dcrdClient)
	if cerr != nil {
		return fmt.Errorf("RPC client error: %v (%v)", cerr.Error(), cerr.Cause())
	}

	// Update the treasury balance, and clear any cached address data in case
	// the sync status page not intercepting requests (see SyncStatusLimit).
	_ = chainDB.FreshenAddressCaches(true, nil) // async treasury queries, no error

	log.Infof("All ready, at height %d.", chainDBHeight)
	explore.SetDBsSyncing(false) // let explorer.Store do final updates
	psHub.SetReady(true)         // make the psHub's WebsocketHub ready to send

	// Initial data summary for web ui and pubsubhub. Normally the notification
	// handlers will do Collect followed by Store.
	{
		blockData, msgBlock, err := collector.Collect()
		if err != nil {
			return fmt.Errorf("Block data collection for initial summary failed: %w", err)
		}

		// Update the current chain state in the ChainDB.
		chainDB.UpdateChainState(blockData.BlockchainInfo)
		log.Infof("Current DCP0010 activation height is %d.", chainDB.DCP0010ActivationHeight())
		log.Infof("Current DCP0011 activation height is %d.", chainDB.DCP0011ActivationHeight())
		log.Infof("Current DCP0012 activation height is %d.", chainDB.DCP0012ActivationHeight())

		if err = explore.Store(blockData, msgBlock); err != nil {
			return fmt.Errorf("Failed to store initial block data for explorer pages: %w", err)
		}

		if err = psHub.Store(blockData, msgBlock); err != nil {
			return fmt.Errorf("Failed to store initial block data with the PubSubHub: %w", err)
		}
	}

	wg.Wait()

	return nil
}

func connectNodeRPC(cfg *config, ntfnHandlers *rpcclient.NotificationHandlers) (*rpcclient.Client, semver.Semver, error) {
	return rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdPass,
		cfg.DcrdCert, cfg.DisableDaemonTLS, true, ntfnHandlers)
}

func listenAndServeProto(ctx context.Context, wg *sync.WaitGroup, listen, proto string, mux http.Handler) {
	// Try to bind web server
	server := http.Server{
		Addr:         listen,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,  // slow requests should not hold connections opened
		WriteTimeout: 60 * time.Second, // hung responses must die
	}

	// Add the graceful shutdown to the waitgroup.
	wg.Add(1)
	go func() {
		// Start graceful shutdown of web server on shutdown signal.
		<-ctx.Done()

		// We received an interrupt signal, shut down.
		log.Infof("Gracefully shutting down web server...")
		ctxShut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctxShut); err != nil {
			// Error from closing listeners.
			log.Infof("HTTP server Shutdown: %v", err)
		}

		// wg.Wait can proceed.
		wg.Done()
	}()

	log.Infof("Now serving the explorer and APIs on %s://%v/", proto, listen)
	// Start the server.
	go func() {
		var err error
		if proto == "https" {
			err = server.ListenAndServeTLS("dcrdata.cert", "dcrdata.key")
		} else {
			err = server.ListenAndServe()
		}
		// If the server dies for any reason other than ErrServerClosed (from
		// graceful server.Shutdown), log the error and request dcrdata be
		// shutdown.
		if err != nil && err != http.ErrServerClosed {
			log.Errorf("Failed to start server: %v", err)
			requestShutdown()
		}
	}()

	// If the server successfully binds to a listening port, ListenAndServe*
	// will block until the server is shutdown. Wait here briefly so the startup
	// operations in main can have a chance to bail out.
	time.Sleep(250 * time.Millisecond)
}

// FileServer conveniently sets up a http.FileServer handler to serve static
// files from path on the file system. Directory listings are denied, as are URL
// paths containing "..".
func FileServer(r chi.Router, pathRoot, fsRoot string, cacheControlMaxAge int64) {
	if strings.ContainsAny(pathRoot, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	// Define a http.HandlerFunc to serve files but not directory indexes.
	hf := func(w http.ResponseWriter, r *http.Request) {
		// Ensure the path begins with "/".
		upath := r.URL.Path
		if strings.Contains(upath, "..") {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
			r.URL.Path = upath
		}
		// Strip the path prefix and clean the path.
		upath = path.Clean(strings.TrimPrefix(upath, pathRoot))

		// Deny directory listings (http.ServeFile recognizes index.html and
		// attempts to serve the directory contents instead).
		if strings.HasSuffix(upath, "/index.html") {
			http.NotFound(w, r)
			return
		}

		// Generate the full file system path and test for existence.
		fullFilePath := filepath.Join(fsRoot, upath)
		fi, err := os.Stat(fullFilePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Deny directory listings
		if fi.IsDir() {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		http.ServeFile(w, r, fullFilePath)
	}

	// For the chi.Mux, make sure a path that ends in "/" and append a "*".
	muxRoot := pathRoot
	if pathRoot != "/" && pathRoot[len(pathRoot)-1] != '/' {
		r.Get(pathRoot, http.RedirectHandler(pathRoot+"/", 301).ServeHTTP)
		muxRoot += "/"
	}
	muxRoot += "*"

	// Mount the http.HandlerFunc on the pathRoot.
	r.With(mw.CacheControl(cacheControlMaxAge)).Get(muxRoot, hf)
}
