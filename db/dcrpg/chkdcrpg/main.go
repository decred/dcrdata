// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/decred/dcrdata/db/dcrpg/v6"
	"github.com/decred/dcrdata/v6/rpcutils"
	"github.com/decred/dcrdata/v6/txhelpers"
)

func mainCore(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	err = os.MkdirAll(cfg.AppDirectory, 0700)
	if err != nil {
		return fmt.Errorf("Unable to create application directory: %v", err)
	}

	initializeLogging(filepath.Join(cfg.LogPath, "chkdcrpg.log"), cfg.DebugLevel)

	if cfg.HTTPProfile {
		go func() {
			log.Info(http.ListenAndServe("localhost:6060", nil))
		}()
	}

	if cfg.CPUProfile != "" {
		var f *os.File
		f, err = os.Create(cfg.CPUProfile)
		if err != nil {
			return err
		}
		if err = pprof.StartCPUProfile(f); err != nil {
			log.Errorf("StartCPUProfile: %v", err)
		}
		defer pprof.StopCPUProfile()
	}

	if cfg.MemProfile != "" {
		var f *os.File
		f, err = os.Create(cfg.MemProfile)
		if err != nil {
			return err
		}
		timer := time.NewTimer(time.Second * 15)
		go func() {
			<-timer.C
			if err = pprof.WriteHeapProfile(f); err != nil {
				log.Errorf("WriteHeapProfile: %v", err)
			}
			f.Close()
		}()
	}

	// Connect to node RPC server
	client, _, err := rpcutils.ConnectNodeRPC(cfg.DcrdServ, cfg.DcrdUser,
		cfg.DcrdPass, cfg.DcrdCert, cfg.DisableDaemonTLS, false)
	if err != nil {
		return fmt.Errorf("Unable to connect to RPC server: %v", err)
	}

	infoResult, err := client.GetInfo(ctx)
	if err != nil {
		return fmt.Errorf("GetInfo failed: %v", err)
	}
	log.Info("Node connection count: ", infoResult.Connections)

	host, port := cfg.DBHostPort, ""
	if !strings.HasPrefix(host, "/") {
		host, port, err = net.SplitHostPort(cfg.DBHostPort)
		if err != nil {
			return fmt.Errorf("SplitHostPort failed: %v", err)
		}
	}

	// Create/load stake database (which includes the separate ticket pool DB).
	// log.Infof("Loading StakeDatabase from %s", cfg.DcrdataDataDirectory)
	// stakeDB, stakeDBHeight, err := stakedb.NewStakeDatabase(client, activeChain,
	// 	cfg.DcrdataDataDirectory)
	// if err != nil {
	// 	log.Errorf("Unable to create stake DB: %v", err)
	// 	if stakeDBHeight >= 0 {
	// 		log.Infof("Attempting to recover stake DB...")
	// 		stakeDB, err = stakedb.LoadAndRecover(client, activeChain,
	// 			cfg.DcrdataDataDirectory, stakeDBHeight-288)
	// 		stakeDBHeight = int64(stakeDB.Height())
	// 	}
	// 	if err != nil {
	// 		if stakeDB != nil {
	// 			_ = stakeDB.Close()
	// 		}
	// 		return fmt.Errorf("StakeDatabase recovery failed: %v", err)
	// 	}
	// }
	// defer stakeDB.Close()

	// log.Infof("Loaded StakeDatabase at height %d", stakeDBHeight)

	if shutdownRequested(ctx) {
		return fmt.Errorf("shutdown requested")
	}

	// Configure PostgreSQL ChainDB
	dbi := dcrpg.DBInfo{
		Host:   host,
		Port:   port,
		User:   cfg.DBUser,
		Pass:   cfg.DBPass,
		DBName: cfg.DBName,
	}
	dbCfg := dcrpg.ChainDBCfg{
		DBi:                  &dbi,
		Params:               activeChain,
		DevPrefetch:          false,
		HidePGConfig:         cfg.HidePGConfig,
		AddrCacheAddrCap:     1 << 10,
		AddrCacheRowCap:      1 << 8,
		AddrCacheUTXOByteCap: 1 << 8,
	}

	// Construct a ChainDB without a stakeDB to allow quick dropping of tables.
	mpChecker := rpcutils.NewMempoolAddressChecker(client, activeChain)
	db, err := dcrpg.NewChainDB(ctx, &dbCfg, nil, mpChecker, client, func() {})
	if db != nil {
		defer db.Close()
	}
	if err != nil || db == nil {
		return fmt.Errorf("NewChainDB failed: %v", err)
	}

	// Check for missing indexes.
	missingIndexes, descs, err := db.MissingIndexes()
	if err != nil {
		return err
	}
	if len(missingIndexes) > 0 {
		// Warn if this is not a fresh sync.
		if db.Height() > 0 {
			for im, mi := range missingIndexes {
				log.Warnf(` - Missing Index "%s": "%s"`, mi, descs[im])
			}
			return fmt.Errorf("some table indexes not found")
		}
	}

	// Check current height of DB.
	lastBlock, err := db.HeightDB()
	if err != nil {
		return fmt.Errorf("HeightDB: %v", err)
	}
	if lastBlock == -1 {
		log.Info("tables are empty, starting fresh.")
	}
	log.Infof("Loaded ChainDB at height %d", lastBlock)

	// // Ensure that stakedb is at PG DB height.
	// var rewindTo int64
	// if lastBlock > 0 {
	// 	// Rewind one extra block to ensure previous winning tickets (validators
	// 	// for current block) get stored in the cache by advancing one block.
	// 	// Normally WiredDB will do this via ChargePoolInfoCache, but we are not
	// 	// using WiredDB.
	// 	rewindTo = lastBlock - 1
	// }
	// stakeDBHeight = int64(stakeDB.Height())
	// if stakeDBHeight > rewindTo+1 {
	// 	log.Infof("Rewinding stake db from %d to %d...", stakeDBHeight, rewindTo)
	// }
	// for stakeDBHeight > rewindTo {
	// 	// Check for quit signal.
	// 	if shutdownRequested(ctx) {
	// 		return fmt.Errorf("StakeDatabase rewind cancelled at height %d.",
	// 			stakeDBHeight)
	// 	}
	// 	if err = stakeDB.DisconnectBlock(false); err != nil {
	// 		return err
	// 	}
	// 	stakeDBHeight = int64(stakeDB.Height())
	// }

	// // Advance to last block.
	// if stakeDBHeight+1 < lastBlock {
	// 	log.Infof("Fast-forwarding StakeDatabase from %d to %d...",
	// 		stakeDBHeight, lastBlock)
	// }
	// for stakeDBHeight < lastBlock {
	// 	// check for quit signal
	// 	if shutdownRequested(ctx) {
	// 		return fmt.Errorf("StakeDatabase fast-forward cancelled at height %d.",
	// 			stakeDBHeight)
	// 	}

	// 	block, blockHash, err := rpcutils.GetBlock(stakeDBHeight+1, client)
	// 	if err != nil {
	// 		return fmt.Errorf("GetBlock failed (%s): %v", blockHash, err)
	// 	}

	// 	if err = stakeDB.ConnectBlock(block); err != nil {
	// 		return err
	// 	}
	// 	stakeDBHeight = int64(stakeDB.Height())
	// 	if stakeDBHeight%1000 == 0 {
	// 		log.Infof("Stake DB at height %d.", stakeDBHeight)
	// 	}
	// }

	// log.Infof("StakeDatabase ready at height %d", stakeDBHeight)

	// Run DB checks.

	log.Infof("Checking the addresses table for spending rows where the " +
		"matching (funding) txhash has not been set...")
	if ids, addrs, err := dcrpg.CheckUnmatchedSpending(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckUnmatchedSpending: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found spending rows with unset matching tx!")
		for i := range ids {
			log.Warnf("\taddresses rowid %d, address %s", ids[i], addrs[i])
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the blocks table for multiple main chain blocks at a given height....")
	if ids, heights, hashes, err := dcrpg.CheckExtraMainchainBlocks(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckExtraMainchainBlocks: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found multiple main chain blocks at the same height!")
		for i := range ids {
			log.Warnf("\tblocks rowid %d, height %d, hash %s", ids[i], heights[i], hashes[i])
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the blocks table for blocks labeled as approved, but " +
		"for which the next block has specified vote bits that invalidate it...")
	if ids, hashes, err := dcrpg.CheckMislabeledInvalidBlocks(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckMislabeledInvalidBlocks: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found mislabeled invalid blocks!")
		for i := range ids {
			log.Warnf("\tblocks rowid %d, hash %s", ids[i], hashes[i])
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the tickets table for tickets that are flagged as " +
		"unspent, but which have set either a spend height or spending " +
		"transaction row id...")
	if ids, spendHeights, spendTxDbIDs, err :=
		dcrpg.CheckUnspentTicketsWithSpendInfo(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckUnspentTicketsWithSpendInfo: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found tickets labeled unspent with spending info set!")
		for i := range ids {
			log.Warnf("\ttickets rowid %d, spend height %d, spent tx DB ID %d",
				ids[i], spendHeights[i], spendTxDbIDs[i])
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the transactions table for ticket transactions that " +
		"also appear in the tickets table, but which do not have the proper " +
		"tx_type set...")
	if ids, types, hashes, err :=
		dcrpg.CheckMislabeledTicketTransactions(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckMislabeledTicketTransactions: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found ticket txns not labeled as tickets!")
		for i := range ids {
			log.Warnf("\ttransactions rowid %d, type %d, hash %s",
				ids[i], txhelpers.TxTypeToString(int(types[i])), hashes[i])
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the transactions table for ticket transactions that do " +
		"NOT appear in the tickets table...")
	if ids, types, hashes, err :=
		dcrpg.CheckMissingTickets(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckMissingTickets: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found ticket txns missing from tickets table!")
		for i := range ids {
			log.Warnf("\ttransactions rowid %d, type %d, hash %s",
				ids[i], txhelpers.TxTypeToString(int(types[i])), hashes[i])
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the tickets table for tickets that do NOT appear in the " +
		"transactions table at all...")
	if ids, hashes, err :=
		dcrpg.CheckMissingTicketTransactions(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckMissingTicketTransactions: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found ticket missing from transactions table!")
		for i := range ids {
			log.Warnf("\ttickets rowid %d, hash %s", ids[i], hashes[i])
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the tickets table for tickets with live pool status " +
		"but not unspent status...")
	if ids, hashes, types, err :=
		dcrpg.CheckBadSpentLiveTickets(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckBadSpentLiveTickets: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found ticket with live pool status but not unspent status!")
		for i := range ids {
			log.Warnf("\ttickets rowid %d, hash %s, type %d",
				ids[i], hashes[i], txhelpers.TxTypeToString(int(types[i])))
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the tickets table for tickets with voted pool status " +
		"but not voted spend status...")
	if ids, hashes, types, err :=
		dcrpg.CheckBadVotedTickets(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckBadVotedTickets: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found ticket with voted pool status but not voted spend status!")
		for i := range ids {
			log.Warnf("\ttickets rowid %d, hash %s, type %d",
				ids[i], hashes[i], txhelpers.TxTypeToString(int(types[i])))
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the tickets table for tickets with expired pool status " +
		"but with voted status...")
	if ids, hashes, types, err :=
		dcrpg.CheckBadExpiredVotedTickets(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckBadExpiredVotedTickets: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found ticket with expired pool status but with voted spend status!")
		for i := range ids {
			log.Warnf("\ttickets rowid %d, hash %s, type %d",
				ids[i], hashes[i], txhelpers.TxTypeToString(int(types[i])))
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the tickets table for tickets with missed pool status " +
		"but with voted status...")
	if ids, hashes, types, err :=
		dcrpg.CheckBadMissedVotedTickets(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckBadMissedVotedTickets: %v", err)
	} else if len(ids) > 0 {
		log.Warn("Found ticket with missed pool status but with voted spend status!")
		for i := range ids {
			log.Warnf("\ttickets rowid %d, hash %s, type %d",
				ids[i], hashes[i], txhelpers.TxTypeToString(int(types[i])))
		}
	}

	if shutdownRequested(ctx) {
		return fmt.Errorf("Shutdown requested.")
	}

	log.Infof("Checking the blocks table for blocks with an incorrect " +
		"approval flag as determined by approvals/total_votes > 0.5...")
	if hashes, approvals, disapprovals, totals, approvedActual, approvedSet, err :=
		dcrpg.CheckBadBlockApproval(ctx, db.SqlDB()); err != nil {
		log.Errorf("CheckBadBlockApproval: %v", err)
	} else if len(hashes) > 0 {
		log.Warn("Found block with incorrect approval flag!")
		for i := range hashes {
			log.Warnf("\tblock hash %s, appr %d, dis %d, tot %d, actual, set",
				hashes[i], approvals[i], disapprovals[i], totals[i],
				approvedActual[i], approvedSet[i])
		}
	}

	log.Info("Done!")

	return nil
}

func main() {
	// Create a context that is cancelled when a shutdown request is received
	// via requestShutdown.
	ctx := withShutdownCancel(context.Background())
	// Listen for both interrupt signals and shutdown requests.
	go shutdownListener()

	if err := mainCore(ctx); err != nil {
		log.Error(err)
		os.Exit(1)
	}
	os.Exit(0)
}

// shutdownRequested checks if the Done channel of the given context has been
// closed. This could indicate cancellation, expiration, or deadline expiry. But
// when called for the context provided by withShutdownCancel, it indicates if
// shutdown has been requested (i.e. via requestShutdown).
func shutdownRequested(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// shutdownRequest is used to initiate shutdown from one of the
// subsystems using the same code paths as when an interrupt signal is received.
var shutdownRequest = make(chan struct{})

// shutdownSignal is closed whenever shutdown is invoked through an interrupt
// signal or from an JSON-RPC stop request.  Any contexts created using
// withShutdownChannel are cancelled when this is closed.
var shutdownSignal = make(chan struct{})

// withShutdownCancel creates a copy of a context that is cancelled whenever
// shutdown is invoked through an interrupt signal or from an JSON-RPC stop
// request.
func withShutdownCancel(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-shutdownSignal
		cancel()
	}()
	return ctx
}

// requestShutdown signals for starting the clean shutdown of the process
// through an internal component (such as through the JSON-RPC stop request).
// func requestShutdown() {
// 	shutdownRequest <- struct{}{}
// }

// shutdownListener listens for shutdown requests and cancels all contexts
// created from withShutdownCancel.  This function never returns and is intended
// to be spawned in a new goroutine.
func shutdownListener() {
	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, os.Interrupt)

	// Listen for the initial shutdown signal
	select {
	case sig := <-interruptChannel:
		log.Infof("Received signal (%s). Shutting down...", sig)
	case <-shutdownRequest:
		log.Info("Shutdown requested. Shutting down...")
	}

	// Cancel all contexts created from withShutdownCancel.
	close(shutdownSignal)

	// Listen for any more shutdown signals and log that shutdown has already
	// been signaled.
	for {
		select {
		case <-interruptChannel:
		case <-shutdownRequest:
		}
		log.Info("Shutdown signaled. Already shutting down...")
	}
}
