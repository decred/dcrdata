// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chappjc/trylock"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient/v2"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/blockdata"
	"github.com/decred/dcrdata/db/cache"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/db/dcrpg/internal"
	"github.com/decred/dcrdata/rpcutils"
	"github.com/decred/dcrdata/stakedb"
	"github.com/decred/dcrdata/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

var (
	zeroHash            = chainhash.Hash{}
	zeroHashStringBytes = []byte(chainhash.Hash{}.String())
)

// storedAgendas holds the current state of agenda data already in the db.
// This helps track changes in the lockedIn and activated heights when they
// happen without making too many db accesses everytime we are updating the
// agenda_votes table.
var storedAgendas map[string]dbtypes.MileStone

// ticketPoolDataCache stores the most recent ticketpool graphs information
// fetched to minimize the possibility of making multiple queries to the db
// fetching the same information.
type ticketPoolDataCache struct {
	sync.RWMutex
	Height          map[dbtypes.TimeBasedGrouping]int64
	TimeGraphCache  map[dbtypes.TimeBasedGrouping]*dbtypes.PoolTicketsData
	PriceGraphCache map[dbtypes.TimeBasedGrouping]*dbtypes.PoolTicketsData
	// DonutGraphCache persist data for the Number of tickets outputs pie chart.
	DonutGraphCache map[dbtypes.TimeBasedGrouping]*dbtypes.PoolTicketsData
}

// ticketPoolGraphsCache persists the latest ticketpool data queried from the db.
var ticketPoolGraphsCache = &ticketPoolDataCache{
	Height:          make(map[dbtypes.TimeBasedGrouping]int64),
	TimeGraphCache:  make(map[dbtypes.TimeBasedGrouping]*dbtypes.PoolTicketsData),
	PriceGraphCache: make(map[dbtypes.TimeBasedGrouping]*dbtypes.PoolTicketsData),
	DonutGraphCache: make(map[dbtypes.TimeBasedGrouping]*dbtypes.PoolTicketsData),
}

// TicketPoolData is a thread-safe way to access the ticketpool graphs data
// stored in the cache.
func TicketPoolData(interval dbtypes.TimeBasedGrouping, height int64) (timeGraph *dbtypes.PoolTicketsData,
	priceGraph *dbtypes.PoolTicketsData, donutChart *dbtypes.PoolTicketsData, actualHeight int64, intervalFound, isStale bool) {
	ticketPoolGraphsCache.RLock()
	defer ticketPoolGraphsCache.RUnlock()

	var tFound, pFound, dFound bool
	timeGraph, tFound = ticketPoolGraphsCache.TimeGraphCache[interval]
	priceGraph, pFound = ticketPoolGraphsCache.PriceGraphCache[interval]
	donutChart, dFound = ticketPoolGraphsCache.DonutGraphCache[interval]
	intervalFound = tFound && pFound && dFound

	actualHeight = ticketPoolGraphsCache.Height[interval]
	isStale = ticketPoolGraphsCache.Height[interval] != height

	return
}

// UpdateTicketPoolData updates the ticket pool cache with the latest data fetched.
// This is a thread-safe way to update ticket pool cache data. TryLock helps avoid
// stacking calls to update the cache.
func UpdateTicketPoolData(interval dbtypes.TimeBasedGrouping, timeGraph *dbtypes.PoolTicketsData,
	priceGraph *dbtypes.PoolTicketsData, donutcharts *dbtypes.PoolTicketsData, height int64) {
	ticketPoolGraphsCache.Lock()
	defer ticketPoolGraphsCache.Unlock()

	ticketPoolGraphsCache.Height[interval] = height
	ticketPoolGraphsCache.TimeGraphCache[interval] = timeGraph
	ticketPoolGraphsCache.PriceGraphCache[interval] = priceGraph
	ticketPoolGraphsCache.DonutGraphCache[interval] = donutcharts
}

// utxoStore provides a UTXOData cache with thread-safe get/set methods.
type utxoStore struct {
	sync.Mutex
	c map[string]map[uint32]*dbtypes.UTXOData
}

// newUtxoStore constructs a new utxoStore.
func newUtxoStore(prealloc int) utxoStore {
	return utxoStore{
		c: make(map[string]map[uint32]*dbtypes.UTXOData, prealloc),
	}
}

// Get attempts to locate UTXOData for the specified outpoint. If the data is
// not in the cache, a nil pointer and false are returned. If the data is
// located, the data and true are returned, and the data is evicted from cache.
func (u *utxoStore) Get(txHash string, txIndex uint32) (*dbtypes.UTXOData, bool) {
	u.Lock()
	defer u.Unlock()
	utxoData, ok := u.c[txHash][txIndex]
	if ok {
		u.c[txHash][txIndex] = nil
		delete(u.c[txHash], txIndex)
		if len(u.c[txHash]) == 0 {
			delete(u.c, txHash)
		}
	}
	return utxoData, ok
}

func (u *utxoStore) set(txHash string, txIndex uint32, addrs []string, val int64) {
	txUTXOVals, ok := u.c[txHash]
	if !ok {
		u.c[txHash] = map[uint32]*dbtypes.UTXOData{
			txIndex: {
				Addresses: addrs,
				Value:     val,
			},
		}
	} else {
		txUTXOVals[txIndex] = &dbtypes.UTXOData{
			Addresses: addrs,
			Value:     val,
		}
	}
}

// Set stores the addresses and amount in a UTXOData entry in the cache for the
// given outpoint.
func (u *utxoStore) Set(txHash string, txIndex uint32, addrs []string, val int64) {
	u.Lock()
	defer u.Unlock()
	u.set(txHash, txIndex, addrs, val)
}

// Reinit re-initializes the utxoStore with the given UTXOs.
func (u *utxoStore) Reinit(utxos []dbtypes.UTXO) {
	u.Lock()
	defer u.Unlock()
	// Pre-allocate the transaction hash map assuming the number of unique
	// transaction hashes in input is roughly 2/3 of the number of UTXOs.
	prealloc := 2 * len(utxos) / 3
	u.c = make(map[string]map[uint32]*dbtypes.UTXOData, prealloc)
	for i := range utxos {
		u.set(utxos[i].TxHash, utxos[i].TxIndex, utxos[i].Addresses, utxos[i].Value)
	}
}

// Size returns the size of the utxo cache in number of UTXOs.
func (u *utxoStore) Size() (sz int) {
	u.Lock()
	defer u.Unlock()
	for _, m := range u.c {
		sz += len(m)
	}
	return
}

type cacheLocks struct {
	bal        *cache.CacheLock
	rows       *cache.CacheLock
	rowsMerged *cache.CacheLock
	utxo       *cache.CacheLock
}

// ChainDB provides an interface for storing and manipulating extracted
// blockchain data in a PostgreSQL database.
type ChainDB struct {
	ctx                context.Context
	queryTimeout       time.Duration
	db                 *sql.DB
	mp                 rpcutils.MempoolAddressChecker
	chainParams        *chaincfg.Params
	devAddress         string
	dupChecks          bool
	bestBlock          *BestBlock
	lastBlock          map[chainhash.Hash]uint64
	stakeDB            *stakedb.StakeDatabase
	unspentTicketCache *TicketTxnIDGetter
	AddressCache       *cache.AddressCache
	CacheLocks         cacheLocks
	devPrefetch        bool
	InBatchSync        bool
	InReorg            bool
	tpUpdatePermission map[dbtypes.TimeBasedGrouping]*trylock.Mutex
	utxoCache          utxoStore
	deployments        *ChainDeployments
}

// ChainDeployments is mutex-protected blockchain deployment data.
type ChainDeployments struct {
	mtx       sync.RWMutex
	chainInfo *dbtypes.BlockChainData
}

// BestBlock is mutex-protected block hash and height.
type BestBlock struct {
	mtx    sync.RWMutex
	height int64
	hash   string
}

func (pgb *ChainDB) timeoutError() string {
	return fmt.Sprintf("%s after %v", dbtypes.TimeoutPrefix, pgb.queryTimeout)
}

// replaceCancelError will replace the generic error strings that can occur when
// a PG query is canceled (dbtypes.PGCancelError) or a context deadline is
// exceeded (dbtypes.CtxDeadlineExceeded from context.DeadlineExceeded).
func (pgb *ChainDB) replaceCancelError(err error) error {
	if err == nil {
		return err
	}

	patched := err.Error()
	if strings.Contains(patched, dbtypes.PGCancelError) {
		patched = strings.Replace(patched, dbtypes.PGCancelError,
			pgb.timeoutError(), -1)
	} else if strings.Contains(patched, dbtypes.CtxDeadlineExceeded) {
		patched = strings.Replace(patched, dbtypes.CtxDeadlineExceeded,
			pgb.timeoutError(), -1)
	} else {
		return err
	}
	return errors.New(patched)
}

// ChainDBRPC provides an interface for storing and manipulating extracted and
// includes the RPC Client blockchain data in a PostgreSQL database.
type ChainDBRPC struct {
	*ChainDB
	Client *rpcclient.Client
}

// NewChainDBRPC contains ChainDB and RPC client parameters. By default,
// duplicate row checks on insertion are enabled. also enables rpc client
func NewChainDBRPC(chaindb *ChainDB, cl *rpcclient.Client) (*ChainDBRPC, error) {
	return &ChainDBRPC{chaindb, cl}, nil
}

// SyncChainDBAsync calls (*ChainDB).SyncChainDBAsync after a nil pointer check
// on the ChainDBRPC receiver.
func (pgb *ChainDBRPC) SyncChainDBAsync(ctx context.Context, res chan dbtypes.SyncResult,
	client rpcutils.MasterBlockGetter, updateAllAddresses, updateAllVotes, newIndexes bool,
	updateExplorer chan *chainhash.Hash, barLoad chan *dbtypes.ProgressBarLoad) {
	// Allowing db to be nil simplifies logic in caller.
	if pgb == nil {
		res <- dbtypes.SyncResult{
			Height: -1,
			Error:  fmt.Errorf("ChainDB (psql) disabled"),
		}
		return
	}
	pgb.ChainDB.SyncChainDBAsync(ctx, res, client, updateAllAddresses,
		updateAllVotes, newIndexes, updateExplorer, barLoad)
}

// Store satisfies BlockDataSaver. Blocks stored this way are considered valid
// and part of mainchain. This calls (*ChainDB).Store after a nil pointer check
// on the ChainDBRPC receiver
func (pgb *ChainDBRPC) Store(blockData *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	// Allowing db to be nil simplifies logic in caller.
	if pgb == nil {
		return nil
	}

	// update blockchain state
	pgb.UpdateChainState(blockData.BlockchainInfo)

	return pgb.ChainDB.Store(blockData, msgBlock)
}

// UpdateChainState calls (*ChainDB).UpdateChainState after a nil pointer check
// on the ChainDBRPC receiver.
func (pgb *ChainDBRPC) UpdateChainState(blockChainInfo *dcrjson.GetBlockChainInfoResult) {
	if pgb == nil || pgb.ChainDB == nil {
		return
	}
	pgb.ChainDB.UpdateChainState(blockChainInfo)
}

// MissingSideChainBlocks identifies side chain blocks that are missing from the
// DB. Side chains known to dcrd are listed via the getchaintips RPC. Each block
// presence in the postgres DB is checked, and any missing block is returned in
// a SideChain along with a count of the total number of missing blocks.
func (pgb *ChainDBRPC) MissingSideChainBlocks() ([]dbtypes.SideChain, int, error) {
	// First get the side chain tips (head blocks).
	tips, err := rpcutils.SideChains(pgb.Client)
	if err != nil {
		return nil, 0, fmt.Errorf("unable to get chain tips from node: %v", err)
	}
	nSideChains := len(tips)

	// Build a list of all the blocks in each side chain that are not
	// already in the database.
	blocksToStore := make([]dbtypes.SideChain, nSideChains)
	var nSideChainBlocks int
	for it := range tips {
		sideHeight := tips[it].Height
		log.Tracef("Getting full side chain with tip %s at %d.", tips[it].Hash, sideHeight)

		sideChain, err := rpcutils.SideChainFull(pgb.Client, tips[it].Hash)
		if err != nil {
			return nil, 0, fmt.Errorf("unable to get side chain blocks for chain tip %s: %v",
				tips[it].Hash, err)
		}
		// Starting height is the lowest block in the side chain.
		sideHeight -= int64(len(sideChain)) - 1

		// For each block in the side chain, check if it already stored.
		for is := range sideChain {
			// Check for the block hash in the DB.
			sideHeightDB, err := pgb.BlockHeight(sideChain[is])
			if err == sql.ErrNoRows {
				// This block is NOT already in the DB.
				blocksToStore[it].Hashes = append(blocksToStore[it].Hashes, sideChain[is])
				blocksToStore[it].Heights = append(blocksToStore[it].Heights, sideHeight)
				nSideChainBlocks++
			} else if err == nil {
				// This block is already in the DB.
				log.Tracef("Found block %s in postgres at height %d.",
					sideChain[is], sideHeightDB)
				if sideHeight != sideHeightDB {
					log.Errorf("Side chain block height %d, expected %d.",
						sideHeightDB, sideHeight)
				}
			} else /* err != nil && err != sql.ErrNoRows */ {
				// Unexpected error
				log.Errorf("Failed to retrieve block %s: %v", sideChain[is], err)
			}

			// Next block
			sideHeight++
		}
	}

	return blocksToStore, nSideChainBlocks, nil
}

// TicketTxnIDGetter provides a cache for DB row IDs of tickets.
type TicketTxnIDGetter struct {
	mtx     sync.RWMutex
	idCache map[string]uint64
	db      *sql.DB
}

// TxnDbID fetches DB row ID for the ticket specified by the input transaction
// hash. A cache is checked first. In the event of a cache hit, the DB ID is
// returned and deleted from the internal cache. In the event of a cache miss,
// the database is queried. If the database query fails, the error is non-nil.
func (t *TicketTxnIDGetter) TxnDbID(txid string, expire bool) (uint64, error) {
	if t == nil {
		panic("You're using an uninitialized TicketTxnIDGetter")
	}
	t.mtx.RLock()
	dbID, ok := t.idCache[txid]
	t.mtx.RUnlock()
	if ok {
		if expire {
			t.mtx.Lock()
			delete(t.idCache, txid)
			t.mtx.Unlock()
		}
		return dbID, nil
	}
	// Cache miss. Get the row id by hash from the tickets table.
	log.Tracef("Cache miss for %s.", txid)
	return RetrieveTicketIDByHashNoCancel(t.db, txid)
}

// Set stores the (transaction hash, DB row ID) pair a map for future access.
func (t *TicketTxnIDGetter) Set(txid string, txDbID uint64) {
	if t == nil {
		return
	}
	t.mtx.Lock()
	defer t.mtx.Unlock()
	t.idCache[txid] = txDbID
}

// SetN stores several (transaction hash, DB row ID) pairs in the map.
func (t *TicketTxnIDGetter) SetN(txid []string, txDbID []uint64) {
	if t == nil {
		return
	}
	t.mtx.Lock()
	defer t.mtx.Unlock()
	for i := range txid {
		t.idCache[txid[i]] = txDbID[i]
	}
}

// NewTicketTxnIDGetter constructs a new TicketTxnIDGetter with an empty cache.
func NewTicketTxnIDGetter(db *sql.DB) *TicketTxnIDGetter {
	return &TicketTxnIDGetter{
		db:      db,
		idCache: make(map[string]uint64),
	}
}

// DBInfo holds the PostgreSQL database connection information.
type DBInfo struct {
	Host, Port, User, Pass, DBName string
	QueryTimeout                   time.Duration
}

// NewChainDB constructs a ChainDB for the given connection and Decred network
// parameters. By default, duplicate row checks on insertion are enabled. See
// NewChainDBWithCancel to enable context cancellation of running queries.
func NewChainDB(dbi *DBInfo, params *chaincfg.Params, stakeDB *stakedb.StakeDatabase,
	devPrefetch, hidePGConfig bool, addrCacheCap int, mp rpcutils.MempoolAddressChecker) (*ChainDB, error) {
	ctx := context.Background()
	chainDB, err := NewChainDBWithCancel(ctx, dbi, params, stakeDB,
		devPrefetch, hidePGConfig, addrCacheCap, mp)
	if err != nil {
		return nil, err
	}

	return chainDB, nil
}

// NewChainDBWithCancel constructs a cancellation-capable ChainDB for the given
// connection and Decred network parameters. By default, duplicate row checks on
// insertion are enabled. See EnableDuplicateCheckOnInsert to change this
// behavior. NewChainDB creates context that cannot be cancelled
// (context.Background()) except by the pg timeouts. If it is necessary to
// cancel queries with CTRL+C, for example, use NewChainDBWithCancel.
func NewChainDBWithCancel(ctx context.Context, dbi *DBInfo, params *chaincfg.Params,
	stakeDB *stakedb.StakeDatabase, devPrefetch, hidePGConfig bool, addrCacheCap int, mp rpcutils.MempoolAddressChecker) (*ChainDB, error) {
	// Connect to the PostgreSQL daemon and return the *sql.DB.
	db, err := Connect(dbi.Host, dbi.Port, dbi.User, dbi.Pass, dbi.DBName)
	if err != nil {
		return nil, err
	}

	// Put the PostgreSQL time zone in UTC.
	var initTZ string
	initTZ, err = CheckCurrentTimeZone(db)
	if err != nil {
		return nil, err
	}
	if initTZ != "UTC" {
		log.Infof("Switching PostgreSQL time zone to UTC for this session.")
		if _, err = db.Exec(`SET TIME ZONE UTC`); err != nil {
			return nil, fmt.Errorf("Failed to set time zone to UTC: %v", err)
		}
	}

	pgVersion, err := RetrievePGVersion(db)
	if err != nil {
		return nil, err
	}
	log.Info(pgVersion)

	// Optionally logs the PostgreSQL configuration.
	if !hidePGConfig {
		perfSettings, err := RetrieveSysSettingsPerformance(db)
		if err != nil {
			return nil, err
		}
		log.Infof("postgres configuration settings:\n%v", perfSettings)

		servSettings, err := RetrieveSysSettingsServer(db)
		if err != nil {
			return nil, err
		}
		log.Infof("postgres server settings:\n%v", servSettings)
	}

	// Attempt to get DB best block height from tables, but if the tables are
	// empty or not yet created, it is not an error.
	bestHeight, bestHash, _, err := RetrieveBestBlockHeight(ctx, db)
	if err != nil && !(err == sql.ErrNoRows ||
		strings.HasSuffix(err.Error(), "does not exist")) {
		return nil, err
	}

	// Development subsidy address of the current network
	var devSubsidyAddress string
	if devSubsidyAddress, err = dbtypes.DevSubsidyAddress(params); err != nil {
		log.Warnf("ChainDB.NewChainDB: %v", err)
	}

	if err = setupTables(db); err != nil {
		log.Warnf("ATTENTION! %v", err)
		// TODO: Actually handle the upgrades/reindexing somewhere.
		return nil, err
	}

	log.Infof("Pre-loading unspent ticket info for InsertVote optimization.")
	unspentTicketCache := NewTicketTxnIDGetter(db)
	unspentTicketDbIDs, unspentTicketHashes, err := RetrieveUnspentTickets(ctx, db)
	if err != nil && err != sql.ErrNoRows && !strings.HasSuffix(err.Error(), "does not exist") {
		return nil, err
	}
	if len(unspentTicketDbIDs) != 0 {
		log.Infof("Storing data for %d unspent tickets in cache.", len(unspentTicketDbIDs))
		unspentTicketCache.SetN(unspentTicketHashes, unspentTicketDbIDs)
	}

	// For each chart grouping type create a non-blocking updater mutex.
	tpUpdatePermissions := make(map[dbtypes.TimeBasedGrouping]*trylock.Mutex)
	for g := range dbtypes.TimeBasedGroupings {
		tpUpdatePermissions[g] = new(trylock.Mutex)
	}

	// If a query timeout is not set (i.e. zero), default to 24 hrs for
	// essentially no timeout.
	queryTimeout := dbi.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = time.Hour
	}

	log.Infof("Setting PostgreSQL DB statement timeout to %v.", queryTimeout)

	bestBlock := &BestBlock{
		height: int64(bestHeight),
		hash:   bestHash,
	}

	// Create the address cache with an arbitrary capacity. The project fund
	// address is set to prevent purging its data when cache reaches capacity.
	addrCache := cache.NewAddressCache(addrCacheCap)
	addrCache.ProjectAddress = devSubsidyAddress

	return &ChainDB{
		ctx:                ctx,
		queryTimeout:       queryTimeout,
		db:                 db,
		mp:                 mp,
		chainParams:        params,
		devAddress:         devSubsidyAddress,
		dupChecks:          true,
		bestBlock:          bestBlock,
		lastBlock:          make(map[chainhash.Hash]uint64),
		stakeDB:            stakeDB,
		unspentTicketCache: unspentTicketCache,
		AddressCache:       addrCache,
		CacheLocks:         cacheLocks{cache.NewCacheLock(), cache.NewCacheLock(), cache.NewCacheLock(), cache.NewCacheLock()},
		devPrefetch:        devPrefetch,
		tpUpdatePermission: tpUpdatePermissions,
		utxoCache:          newUtxoStore(5e4),
		deployments:        new(ChainDeployments),
	}, nil
}

// Close closes the underlying sql.DB connection to the database.
func (pgb *ChainDB) Close() error {
	return pgb.db.Close()
}

// InitUtxoCache resets the UTXO cache with the given slice of UTXO data.
func (pgb *ChainDB) InitUtxoCache(utxos []dbtypes.UTXO) {
	pgb.utxoCache.Reinit(utxos)
}

// UseStakeDB is used to assign a stakedb.StakeDatabase for ticket tracking.
// This may be useful when it is necessary to construct a ChainDB prior to
// creating or loading a StakeDatabase, such as when dropping tables.
func (pgb *ChainDB) UseStakeDB(stakeDB *stakedb.StakeDatabase) {
	pgb.stakeDB = stakeDB
}

// UseMempoolChecker assigns a MempoolAddressChecker for searching mempool for
// transactions involving a certain address.
func (pgb *ChainDB) UseMempoolChecker(mp rpcutils.MempoolAddressChecker) {
	pgb.mp = mp
}

// EnableDuplicateCheckOnInsert specifies whether SQL insertions should check
// for row conflicts (duplicates), and avoid adding or updating.
func (pgb *ChainDB) EnableDuplicateCheckOnInsert(dupCheck bool) {
	if pgb == nil {
		return
	}
	pgb.dupChecks = dupCheck
}

// SetupTables creates the required tables and type, and prints table versions
// stored in the table comments when debug level logging is enabled.
func (pgb *ChainDB) SetupTables() error {
	return setupTables(pgb.db)
}

func setupTables(db *sql.DB) error {
	if err := CreateTypes(db); err != nil {
		return err
	}

	return CreateTables(db)
}

// VersionCheck checks the current version of all known tables and notifies when
// an upgrade is required. If there is no automatic upgrade supported, an error
// is returned when any table is not of the correct version.
// A smart client is passed to implement the supported upgrades if need be.
func (pgb *ChainDB) VersionCheck(client *rpcclient.Client) error {
	vers := TableVersions(pgb.db)
	for tab, ver := range vers {
		log.Debugf("Table %s: v%s", tab, ver)
	}

	var needsUpgrade []TableUpgrade
	tableUpgrades := TableUpgradesRequired(vers)

	for _, val := range tableUpgrades {
		switch val.UpgradeType {
		case Upgrade, Reindex:
			// Select the all tables that need an upgrade or reindex.
			needsUpgrade = append(needsUpgrade, val)

		case Unknown, Rebuild:
			// All the tables require rebuilding.
			return fmt.Errorf("rebuild of PostgreSQL tables required (drop with rebuilddb2 -D)")
		}
	}

	if len(needsUpgrade) == 0 {
		// All tables have the correct version.
		log.Debugf("All tables at correct version (%v)", tableUpgrades[0].RequiredVer)
		return nil
	}

	// UpgradeTables adds the pending db upgrades and reindexes.
	_, err := pgb.UpgradeTables(client, needsUpgrade[0].CurrentVer,
		needsUpgrade[0].RequiredVer)
	if err != nil {
		return err
	}

	// Upgrade was successful.
	return nil
}

// DropTables drops (deletes) all of the known dcrdata tables.
func (pgb *ChainDB) DropTables() {
	DropTables(pgb.db)
}

// SideChainBlocks retrieves all known side chain blocks.
func (pgb *ChainDB) SideChainBlocks() ([]*dbtypes.BlockStatus, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	scb, err := RetrieveSideChainBlocks(ctx, pgb.db)
	return scb, pgb.replaceCancelError(err)
}

// SideChainTips retrieves the tip/head block for all known side chains.
func (pgb *ChainDB) SideChainTips() ([]*dbtypes.BlockStatus, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	sct, err := RetrieveSideChainTips(ctx, pgb.db)
	return sct, pgb.replaceCancelError(err)
}

// DisapprovedBlocks retrieves all blocks disapproved by stakeholder votes.
func (pgb *ChainDB) DisapprovedBlocks() ([]*dbtypes.BlockStatus, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	disb, err := RetrieveDisapprovedBlocks(ctx, pgb.db)
	return disb, pgb.replaceCancelError(err)
}

// BlockStatus retrieves the block chain status of the specified block.
func (pgb *ChainDB) BlockStatus(hash string) (dbtypes.BlockStatus, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	bs, err := RetrieveBlockStatus(ctx, pgb.db, hash)
	return bs, pgb.replaceCancelError(err)
}

// blockFlags retrieves the block's isValid and isMainchain flags.
func (pgb *ChainDB) blockFlags(ctx context.Context, hash string) (bool, bool, error) {
	iv, im, err := RetrieveBlockFlags(ctx, pgb.db, hash)
	return iv, im, pgb.replaceCancelError(err)
}

// BlockFlags retrieves the block's isValid and isMainchain flags.
func (pgb *ChainDB) BlockFlags(hash string) (bool, bool, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	return pgb.blockFlags(ctx, hash)
}

// BlockFlagsNoCancel retrieves the block's isValid and isMainchain flags.
func (pgb *ChainDB) BlockFlagsNoCancel(hash string) (bool, bool, error) {
	return pgb.blockFlags(context.Background(), hash)
}

// blockChainDbID gets the row ID of the given block hash in the block_chain
// table. The cancellation context is used without timeout.
func (pgb *ChainDB) blockChainDbID(ctx context.Context, hash string) (dbID uint64, err error) {
	err = pgb.db.QueryRowContext(ctx, internal.SelectBlockChainRowIDByHash, hash).Scan(&dbID)
	err = pgb.replaceCancelError(err)
	return
}

// BlockChainDbID gets the row ID of the given block hash in the block_chain
// table. The cancellation context is used without timeout.
func (pgb *ChainDB) BlockChainDbID(hash string) (dbID uint64, err error) {
	return pgb.blockChainDbID(pgb.ctx, hash)
}

// BlockChainDbIDNoCancel gets the row ID of the given block hash in the
// block_chain table. The cancellation context is used without timeout.
func (pgb *ChainDB) BlockChainDbIDNoCancel(hash string) (dbID uint64, err error) {
	return pgb.blockChainDbID(context.Background(), hash)
}

// TransactionBlocks retrieves the blocks in which the specified transaction
// appears, along with the index of the transaction in each of the blocks. The
// next and previous block hashes are NOT SET in each BlockStatus.
func (pgb *ChainDB) TransactionBlocks(txHash string) ([]*dbtypes.BlockStatus, []uint32, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	hashes, heights, inds, valids, mainchains, err := RetrieveTxnsBlocks(ctx, pgb.db, txHash)
	if err != nil {
		return nil, nil, pgb.replaceCancelError(err)
	}

	blocks := make([]*dbtypes.BlockStatus, len(hashes))

	for i := range hashes {
		blocks[i] = &dbtypes.BlockStatus{
			IsValid:     valids[i],
			IsMainchain: mainchains[i],
			Height:      heights[i],
			Hash:        hashes[i],
			// Next and previous hash not set
		}
	}

	return blocks, inds, nil
}

// HeightDB queries the DB for the best block height. When the tables are empty,
// the returned height will be -1.
func (pgb *ChainDB) HeightDB() (int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	bestHeight, _, _, err := RetrieveBestBlockHeight(ctx, pgb.db)
	height := int64(bestHeight)
	if err == sql.ErrNoRows {
		height = -1
	}
	// DO NOT change this to return -1 if err == sql.ErrNoRows.
	return height, pgb.replaceCancelError(err)
}

// HashDB queries the DB for the best block's hash.
func (pgb *ChainDB) HashDB() (string, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, bestHash, _, err := RetrieveBestBlockHeight(ctx, pgb.db)
	return bestHash, pgb.replaceCancelError(err)
}

// HeightHashDB queries the DB for the best block's height and hash.
func (pgb *ChainDB) HeightHashDB() (uint64, string, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	height, hash, _, err := RetrieveBestBlockHeight(ctx, pgb.db)
	return height, hash, pgb.replaceCancelError(err)
}

// Height is a getter for ChainDB.bestBlock.height.
func (pgb *ChainDB) Height() int64 {
	return pgb.bestBlock.Height()
}

// Height uses the last stored height.
func (block *BestBlock) Height() int64 {
	block.mtx.RLock()
	defer block.mtx.RUnlock()
	return block.height
}

// GetHeight is for middleware DataSource compatibility. No DB query is
// performed; the last stored height is used.
func (pgb *ChainDB) GetHeight() (int64, error) {
	return pgb.Height(), nil
}

// HashStr uses the last stored block hash.
func (block *BestBlock) HashStr() string {
	block.mtx.RLock()
	defer block.mtx.RUnlock()
	return block.hash
}

// Hash uses the last stored block hash.
func (block *BestBlock) Hash() *chainhash.Hash {
	// Caller should check hash instead of error
	hash, _ := chainhash.NewHashFromStr(block.HashStr())
	return hash
}

func (pgb *ChainDB) BestBlock() (*chainhash.Hash, int64) {
	pgb.bestBlock.mtx.RLock()
	defer pgb.bestBlock.mtx.RUnlock()
	hash, _ := chainhash.NewHashFromStr(pgb.bestBlock.hash)
	return hash, pgb.bestBlock.height
}

func (pgb *ChainDB) BestBlockStr() (string, int64) {
	pgb.bestBlock.mtx.RLock()
	defer pgb.bestBlock.mtx.RUnlock()
	return pgb.bestBlock.hash, pgb.bestBlock.height
}

// BestBlockHash is a getter for ChainDB.bestBlock.hash.
func (pgb *ChainDB) BestBlockHash() *chainhash.Hash {
	return pgb.bestBlock.Hash()
}

// BestBlockHashStr is a getter for ChainDB.bestBlock.hash.
func (pgb *ChainDB) BestBlockHashStr() string {
	return pgb.bestBlock.HashStr()
}

// BlockHeight queries the DB for the height of the specified hash.
func (pgb *ChainDB) BlockHeight(hash string) (int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	height, err := RetrieveBlockHeight(ctx, pgb.db, hash)
	return height, pgb.replaceCancelError(err)
}

// BlockHash queries the DB for the hash of the mainchain block at the given
// height.
func (pgb *ChainDB) BlockHash(height int64) (string, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	hash, err := RetrieveBlockHash(ctx, pgb.db, height)
	return hash, pgb.replaceCancelError(err)
}

// BlockTimeByHeight queries the DB for the time of the mainchain block at the
// given height.
func (pgb *ChainDB) BlockTimeByHeight(height int64) (int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	time, err := RetrieveBlockTimeByHeight(ctx, pgb.db, height)
	return time.UNIX(), pgb.replaceCancelError(err)
}

// VotesInBlock returns the number of votes mined in the block with the
// specified hash.
func (pgb *ChainDB) VotesInBlock(hash string) (int16, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	voters, err := RetrieveBlockVoteCount(ctx, pgb.db, hash)
	if err != nil {
		err = pgb.replaceCancelError(err)
		log.Errorf("Unable to get block voter count for hash %s: %v", hash, err)
		return -1, err
	}
	return voters, nil
}

// SpendingTransactions retrieves all transactions spending outpoints from the
// specified funding transaction. The spending transaction hashes, the spending
// tx input indexes, and the corresponding funding tx output indexes, and an
// error value are returned.
func (pgb *ChainDB) SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, spendingTxns, vinInds, voutInds, err := RetrieveSpendingTxsByFundingTx(ctx, pgb.db, fundingTxID)
	return spendingTxns, vinInds, voutInds, pgb.replaceCancelError(err)
}

// SpendingTransaction returns the transaction that spends the specified
// transaction outpoint, if it is spent. The spending transaction hash, input
// index, tx tree, and an error value are returned.
func (pgb *ChainDB) SpendingTransaction(fundingTxID string,
	fundingTxVout uint32) (string, uint32, int8, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, spendingTx, vinInd, tree, err := RetrieveSpendingTxByTxOut(ctx, pgb.db, fundingTxID, fundingTxVout)
	return spendingTx, vinInd, tree, pgb.replaceCancelError(err)
}

// BlockTransactions retrieves all transactions in the specified block, their
// indexes in the block, their tree, and an error value.
func (pgb *ChainDB) BlockTransactions(blockHash string) ([]string, []uint32, []int8, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, blockTransactions, blockInds, trees, _, err := RetrieveTxsByBlockHash(ctx, pgb.db, blockHash)
	return blockTransactions, blockInds, trees, pgb.replaceCancelError(err)
}

// Transaction retrieves all rows from the transactions table for the given
// transaction hash.
func (pgb *ChainDB) Transaction(txHash string) ([]*dbtypes.Tx, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, dbTxs, err := RetrieveDbTxsByHash(ctx, pgb.db, txHash)
	return dbTxs, pgb.replaceCancelError(err)
}

// BlockMissedVotes retrieves the ticket IDs for all missed votes in the
// specified block, and an error value.
func (pgb *ChainDB) BlockMissedVotes(blockHash string) ([]string, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	mv, err := RetrieveMissedVotesInBlock(ctx, pgb.db, blockHash)
	return mv, pgb.replaceCancelError(err)
}

// TicketMisses retrieves all blocks in which the specified ticket was called to
// vote but failed to do so (miss). There may be multiple since this consideres
// side chain blocks. See TicketMiss for a mainchain-only version. If the ticket
// never missed a vote, the returned error will be sql.ErrNoRows.
func (pgb *ChainDB) TicketMisses(ticketHash string) ([]string, []int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	blockHashes, blockHeights, err := RetrieveMissesForTicket(ctx, pgb.db, ticketHash)
	return blockHashes, blockHeights, pgb.replaceCancelError(err)
}

// TicketMiss retrieves the mainchain block in which the specified ticket was
// called to vote but failed to do so (miss). If the ticket never missed a vote,
// the returned error will be sql.ErrNoRows.
func (pgb *ChainDB) TicketMiss(ticketHash string) (string, int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	blockHash, blockHeight, err := RetrieveMissForTicket(ctx, pgb.db, ticketHash)
	return blockHash, blockHeight, pgb.replaceCancelError(err)
}

// PoolStatusForTicket retrieves the specified ticket's spend status and ticket
// pool status, and an error value.
func (pgb *ChainDB) PoolStatusForTicket(txid string) (dbtypes.TicketSpendType, dbtypes.TicketPoolStatus, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, spendType, poolStatus, err := RetrieveTicketStatusByHash(ctx, pgb.db, txid)
	return spendType, poolStatus, pgb.replaceCancelError(err)
}

// VoutValue retrieves the value of the specified transaction outpoint in atoms.
func (pgb *ChainDB) VoutValue(txID string, vout uint32) (uint64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	voutValue, err := RetrieveVoutValue(ctx, pgb.db, txID, vout)
	if err != nil {
		return 0, pgb.replaceCancelError(err)
	}
	return voutValue, nil
}

// VoutValues retrieves the values of each outpoint of the specified
// transaction. The corresponding indexes in the block and tx trees of the
// outpoints, and an error value are also returned.
func (pgb *ChainDB) VoutValues(txID string) ([]uint64, []uint32, []int8, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	voutValues, txInds, txTrees, err := RetrieveVoutValues(ctx, pgb.db, txID)
	if err != nil {
		return nil, nil, nil, pgb.replaceCancelError(err)
	}
	return voutValues, txInds, txTrees, nil
}

// TransactionBlock retrieves the hash of the block containing the specified
// transaction. The index of the transaction within the block, the transaction
// index, and an error value are also returned.
func (pgb *ChainDB) TransactionBlock(txID string) (string, uint32, int8, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, blockHash, blockInd, tree, err := RetrieveTxByHash(ctx, pgb.db, txID)
	return blockHash, blockInd, tree, pgb.replaceCancelError(err)
}

// AgendaVotes fetches the data used to plot a graph of votes cast per day per
// choice for the provided agenda.
func (pgb *ChainDB) AgendaVotes(agendaID string, chartType int) (*dbtypes.AgendaVoteChoices, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	chainInfo := pgb.ChainInfo()
	agendaInfo := chainInfo.AgendaMileStones[agendaID]

	// check if starttime is in the future exit.
	if time.Now().Before(agendaInfo.StartTime) {
		return nil, nil
	}

	avc, err := retrieveAgendaVoteChoices(ctx, pgb.db, agendaID, chartType,
		agendaInfo.VotingStarted, agendaInfo.VotingDone)
	return avc, pgb.replaceCancelError(err)
}

// AgendasVotesSummary fetches the total vote choices count for the provided agenda.
func (pgb *ChainDB) AgendasVotesSummary(agendaID string) (summary *dbtypes.AgendaSummary, err error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	chainInfo := pgb.ChainInfo()
	agendaInfo := chainInfo.AgendaMileStones[agendaID]

	// Check if starttime is in the future and exit if true.
	if time.Now().Before(agendaInfo.StartTime) {
		return
	}

	summary = &dbtypes.AgendaSummary{
		VotingStarted: agendaInfo.VotingStarted,
		LockedIn:      agendaInfo.VotingDone,
	}

	summary.Yes, summary.Abstain, summary.No, err = retrieveTotalAgendaVotesCount(ctx,
		pgb.db, agendaID, agendaInfo.VotingStarted, agendaInfo.VotingDone)
	return
}

// AgendaVoteCounts returns the vote counts for the agenda as builtin types.
func (pgb *ChainDB) AgendaVoteCounts(agendaID string) (yes, abstain, no uint32, err error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	chainInfo := pgb.ChainInfo()
	agendaInfo := chainInfo.AgendaMileStones[agendaID]

	// Check if starttime is in the future and exit if true.
	if time.Now().Before(agendaInfo.StartTime) {
		return
	}

	return retrieveTotalAgendaVotesCount(ctx, pgb.db, agendaID,
		agendaInfo.VotingStarted, agendaInfo.VotingDone)
}

// AllAgendas returns all the agendas stored currently.
func (pgb *ChainDB) AllAgendas() (map[string]dbtypes.MileStone, error) {
	return retrieveAllAgendas(pgb.db)
}

// NumAddressIntervals gets the number of unique time intervals for the
// specified grouping where there are entries in the addresses table for the
// given address.
func (pgb *ChainDB) NumAddressIntervals(addr string, grouping dbtypes.TimeBasedGrouping) (int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	return retrieveAddressTxsCount(ctx, pgb.db, addr, grouping.String())
}

// AddressMetrics returns the block time of the oldest transaction and the
// total count for all the transactions linked to the provided address grouped
// by years, months, weeks and days time grouping in seconds.
// This helps plot more meaningful address history graphs to the user.
func (pgb *ChainDB) AddressMetrics(addr string) (*dbtypes.AddressMetrics, error) {
	// For each time grouping/interval size, get the number if intervals with
	// data for the address.
	var metrics dbtypes.AddressMetrics
	for _, s := range dbtypes.TimeIntervals {
		numIntervals, err := pgb.NumAddressIntervals(addr, s)
		if err != nil {
			return nil, fmt.Errorf("retrieveAddressAllTxsCount failed: error: %v", err)
		}

		switch s {
		case dbtypes.YearGrouping:
			metrics.YearTxsCount = numIntervals
		case dbtypes.MonthGrouping:
			metrics.MonthTxsCount = numIntervals
		case dbtypes.WeekGrouping:
			metrics.WeekTxsCount = numIntervals
		case dbtypes.DayGrouping:
			metrics.DayTxsCount = numIntervals
		}
	}

	// Get the time of the block with the first transaction involving the
	// address (oldest transaction block time).
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	blockTime, err := retrieveOldestTxBlockTime(ctx, pgb.db, addr)
	if err != nil {
		return nil, fmt.Errorf("retrieveOldestTxBlockTime failed: error: %v", err)
	}
	metrics.OldestBlockTime = blockTime

	return &metrics, pgb.replaceCancelError(err)
}

// AddressTransactions retrieves a slice of *dbtypes.AddressRow for a given
// address and transaction type (i.e. all, credit, or debit) from the DB. Only
// the first N transactions starting from the offset element in the set of all
// txnType transactions.
func (pgb *ChainDB) AddressTransactions(address string, N, offset int64,
	txnType dbtypes.AddrTxnViewType) (addressRows []*dbtypes.AddressRow, err error) {
	var addrFunc func(context.Context, *sql.DB, string, int64, int64) ([]*dbtypes.AddressRow, error)
	switch txnType {
	case dbtypes.AddrTxnCredit:
		addrFunc = RetrieveAddressCreditTxns
	case dbtypes.AddrTxnAll:
		addrFunc = RetrieveAddressTxns
	case dbtypes.AddrTxnDebit:
		addrFunc = RetrieveAddressDebitTxns
	case dbtypes.AddrMergedTxnDebit:
		addrFunc = RetrieveAddressMergedDebitTxns
	case dbtypes.AddrMergedTxnCredit:
		addrFunc = RetrieveAddressMergedCreditTxns
	case dbtypes.AddrMergedTxn:
		addrFunc = RetrieveAddressMergedTxns
	default:
		return nil, fmt.Errorf("unknown AddrTxnViewType %v", txnType)
	}

	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	addressRows, err = addrFunc(ctx, pgb.db, address, N, offset)
	err = pgb.replaceCancelError(err)
	return
}

// AddressTransactionsAll retrieves all non-merged main chain addresses table
// rows for the given address.
func (pgb *ChainDB) AddressTransactionsAll(address string) (addressRows []*dbtypes.AddressRow, err error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	addressRows, err = RetrieveAllMainchainAddressTxns(ctx, pgb.db, address)
	err = pgb.replaceCancelError(err)
	return
}

// AddressTransactionsAllMerged retrieves all merged (stakeholder-approved and
// mainchain only) addresses table rows for the given address.
func (pgb *ChainDB) AddressTransactionsAllMerged(address string) (addressRows []*dbtypes.AddressRow, err error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	onlyValidMainchain := true
	_, addressRows, err = RetrieveAllAddressMergedTxns(ctx, pgb.db, address,
		onlyValidMainchain)
	err = pgb.replaceCancelError(err)
	return
}

// AddressHistoryAll retrieves N address rows of type AddrTxnAll, skipping over
// offset rows first, in order of block time.
func (pgb *ChainDB) AddressHistoryAll(address string, N, offset int64) ([]*dbtypes.AddressRow, *dbtypes.AddressBalance, error) {
	return pgb.AddressHistory(address, N, offset, dbtypes.AddrTxnAll)
}

// TicketPoolBlockMaturity returns the block at which all tickets with height
// greater than it are immature.
func (pgb *ChainDB) TicketPoolBlockMaturity() int64 {
	bestBlock := int64(pgb.stakeDB.Height())
	return bestBlock - int64(pgb.chainParams.TicketMaturity)
}

// TicketPoolByDateAndInterval fetches the tickets ordered by the purchase date
// interval provided and an error value.
func (pgb *ChainDB) TicketPoolByDateAndInterval(maturityBlock int64,
	interval dbtypes.TimeBasedGrouping) (*dbtypes.PoolTicketsData, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	tpd, err := retrieveTicketsByDate(ctx, pgb.db, maturityBlock, interval.String())
	return tpd, pgb.replaceCancelError(err)
}

// PosIntervals retrieves the blocks at the respective stakebase windows
// interval. The term "window" is used here to describe the group of blocks
// whose count is defined by chainParams.StakeDiffWindowSize. During this
// chainParams.StakeDiffWindowSize block interval the ticket price and the
// difficulty value is constant.
func (pgb *ChainDB) PosIntervals(limit, offset uint64) ([]*dbtypes.BlocksGroupedInfo, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	bgi, err := retrieveWindowBlocks(ctx, pgb.db, pgb.chainParams.StakeDiffWindowSize, limit, offset)
	return bgi, pgb.replaceCancelError(err)
}

// TimeBasedIntervals retrieves blocks groups by the selected time-based
// interval. For the consecutive groups the number of blocks grouped together is
// not uniform.
func (pgb *ChainDB) TimeBasedIntervals(timeGrouping dbtypes.TimeBasedGrouping,
	limit, offset uint64) ([]*dbtypes.BlocksGroupedInfo, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	bgi, err := retrieveTimeBasedBlockListing(ctx, pgb.db, timeGrouping.String(),
		limit, offset)
	return bgi, pgb.replaceCancelError(err)
}

// TicketPoolVisualization helps block consecutive and duplicate DB queries for
// the requested ticket pool chart data. If the data for the given interval is
// cached and fresh, it is returned. If the cached data is stale and there are
// no queries running to update the cache for the given interval, this launches
// a query and updates the cache. If there is no cached data for the interval,
// this will launch a new query for the data if one is not already running, and
// if one is running, it will wait for the query to complete.
func (pgb *ChainDB) TicketPoolVisualization(interval dbtypes.TimeBasedGrouping) (*dbtypes.PoolTicketsData,
	*dbtypes.PoolTicketsData, *dbtypes.PoolTicketsData, int64, error) {
	// Attempt to retrieve data for the current block from cache.
	heightSeen := pgb.Height() // current block seen *by the ChainDB*
	if heightSeen < 0 {
		return nil, nil, nil, -1, fmt.Errorf("no charts data available")
	}
	timeChart, priceChart, donutCharts, height, intervalFound, stale :=
		TicketPoolData(interval, heightSeen)
	if intervalFound && !stale {
		// The cache was fresh.
		return timeChart, priceChart, donutCharts, height, nil
	}

	// Cache is stale or empty. Attempt to gain updater status.
	if !pgb.tpUpdatePermission[interval].TryLock() {
		// Another goroutine is running db query to get the updated data.
		if !intervalFound {
			// Do not even have stale data. Must wait for the DB update to
			// complete to get any data at all. Use a blocking call on the
			// updater lock even though we are not going to actually do an
			// update ourselves so we do not block the cache while waiting.
			pgb.tpUpdatePermission[interval].Lock()
			defer pgb.tpUpdatePermission[interval].Unlock()
			// Try again to pull it from cache now that the update is completed.
			heightSeen = pgb.Height()
			timeChart, priceChart, donutCharts, height, intervalFound, stale =
				TicketPoolData(interval, heightSeen)
			// We waited for the updater of this interval, so it should be found
			// at this point. If not, this is an error.
			if !intervalFound {
				log.Errorf("Charts data for interval %v failed to update.", interval)
				return nil, nil, nil, 0, fmt.Errorf("no charts data available")
			}
			if stale {
				log.Warnf("Charts data for interval %v updated, but still stale.", interval)
			}
		}
		// else return the stale data instead of waiting.

		return timeChart, priceChart, donutCharts, height, nil
	}
	// This goroutine is now the cache updater.
	defer pgb.tpUpdatePermission[interval].Unlock()

	// Retrieve chart data for best block in DB.
	var err error
	timeChart, priceChart, donutCharts, height, err = pgb.ticketPoolVisualization(interval)
	if err != nil {
		log.Errorf("Failed to fetch ticket pool data: %v", err)
		return nil, nil, nil, 0, err
	}

	// Update the cache with the new ticket pool data.
	UpdateTicketPoolData(interval, timeChart, priceChart, donutCharts, height)

	return timeChart, priceChart, donutCharts, height, nil
}

// ticketPoolVisualization fetches the following ticketpool data: tickets
// grouped on the specified interval, tickets grouped by price, and ticket
// counts by ticket type (solo, pool, other split). The interval may be one of:
// "mo", "wk", "day", or "all". The data is needed to populate the ticketpool
// graphs. The data grouped by time and price are returned in a slice.
func (pgb *ChainDB) ticketPoolVisualization(interval dbtypes.TimeBasedGrouping) (timeChart *dbtypes.PoolTicketsData,
	priceChart *dbtypes.PoolTicketsData, byInputs *dbtypes.PoolTicketsData, height int64, err error) {
	// Ensure DB height is the same before and after queries since they are not
	// atomic. Initial height:
	height = pgb.Height()
	for {
		// Latest block where mature tickets may have been mined.
		maturityBlock := pgb.TicketPoolBlockMaturity()

		// Tickets grouped by time interval
		timeChart, err = pgb.TicketPoolByDateAndInterval(maturityBlock, interval)
		if err != nil {
			return nil, nil, nil, 0, err
		}

		// Tickets grouped by price
		priceChart, err = pgb.TicketsByPrice(maturityBlock)
		if err != nil {
			return nil, nil, nil, 0, err
		}

		// Tickets grouped by number of inputs.
		byInputs, err = pgb.TicketsByInputCount()
		if err != nil {
			return nil, nil, nil, 0, err
		}

		heightEnd := pgb.Height()
		if heightEnd == height {
			break
		}
		// otherwise try again to ensure charts are consistent.
		height = heightEnd
	}

	return
}

// GetTicketInfo retrieves information about the pool and spend statuses, the
// purchase block, the lottery block, and the spending transaction.
func (pgb *ChainDB) GetTicketInfo(txid string) (*apitypes.TicketInfo, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	spendStatus, poolStatus, purchaseBlock, lotteryBlock, spendTxid, err := RetrieveTicketInfoByHash(ctx, pgb.db, txid)

	if err != nil {
		return nil, pgb.replaceCancelError(err)
	}

	var vote, revocation *string
	status := strings.ToLower(poolStatus.String())
	maturity := purchaseBlock.Height + uint32(pgb.chainParams.TicketMaturity)
	expiration := maturity + pgb.chainParams.TicketExpiry
	if pgb.Height() < int64(maturity) {
		status = "immature"
	}
	if spendStatus == dbtypes.TicketRevoked {
		status = spendStatus.String()
		revocation = &spendTxid
	} else if spendStatus == dbtypes.TicketVoted {
		vote = &spendTxid
	}

	if poolStatus == dbtypes.PoolStatusMissed {
		hash, height, err := RetrieveMissForTicket(ctx, pgb.db, txid)
		if err != nil {
			return nil, pgb.replaceCancelError(err)
		}
		lotteryBlock = &apitypes.TinyBlock{
			Hash:   hash,
			Height: uint32(height),
		}
	}

	return &apitypes.TicketInfo{
		Status:           status,
		PurchaseBlock:    purchaseBlock,
		MaturityHeight:   maturity,
		ExpirationHeight: expiration,
		LotteryBlock:     lotteryBlock,
		Vote:             vote,
		Revocation:       revocation,
	}, nil
}

func (pgb *ChainDB) updateProjectFundCache() error {
	_, _, err := pgb.AddressHistoryAll(pgb.devAddress, 1, 0)
	return err
	// Update balance.
	// _, _, err := pgb.AddressBalance(pgb.devAddress)
	// if err != nil {
	// 	return err
	// }

	// _, err = pgb.AddressRowsCompact(pgb.devAddress)
	// return err
}

// FreshenAddressCaches resets the address balance cache by purging data for the
// addresses listed in expireAddresses, and prefetches the project fund balance
// if devPrefetch is enabled and not mid-reorg. The project fund update is run
// asynchronously if lazyProjectFund is true.
func (pgb *ChainDB) FreshenAddressCaches(lazyProjectFund bool, expireAddresses []string) error {
	// Clear existing cache entries.
	//numCleared := pgb.AddressCache.ClearAll()
	numCleared := pgb.AddressCache.Clear(expireAddresses)
	log.Debugf("Cleared cache for %d addresses.", numCleared)

	// Do not initiate project fund queries if a reorg is in progress, or
	// pre-fetch is disabled.
	if !pgb.devPrefetch || pgb.InReorg {
		return nil
	}

	// Update project fund data.
	updateFundData := func() error {
		log.Infof("Pre-fetching project fund data at height %d...", pgb.Height())
		if err := pgb.updateProjectFundCache(); err != nil && err.Error() != "retry" {
			err = pgb.replaceCancelError(err)
			return fmt.Errorf("Failed to update project fund data: %v", err)
		}
		return nil
	}

	if lazyProjectFund {
		go func() {
			runtime.Gosched()
			if err := updateFundData(); err != nil {
				log.Error(err)
			}
		}()
		return nil
	}
	return updateFundData()
}

// DevBalance returns the current development/project fund balance, updating the
// cached balance if it is stale. DevBalance differs slightly from
// addressBalance(devAddress) in that it will not initiate a DB query if a chain
// reorganization is in progress.
func (pgb *ChainDB) DevBalance() (*dbtypes.AddressBalance, error) {
	// Check cache first.
	cachedBalance, validBlock := pgb.AddressCache.Balance(pgb.devAddress)
	bestBlockHash := pgb.BestBlockHash()
	if cachedBalance != nil && validBlock.Hash == *bestBlockHash {
		return cachedBalance, nil
	}

	if !pgb.InReorg {
		bal, _, err := pgb.AddressBalance(pgb.devAddress)
		if err != nil {
			return nil, err
		}
		return bal, nil
	}

	// In reorg and cache is stale.
	if cachedBalance != nil {
		return cachedBalance, nil
	}
	return nil, fmt.Errorf("unable to query for balance during reorg")
}

// AddressBalance attempts to retrieve balance information for a specific
// address from cache, and if cache is stale or missing data for the address, a
// DB query is used. A successful DB query will freshen the cache.
func (pgb *ChainDB) AddressBalance(address string) (bal *dbtypes.AddressBalance, cacheUpdated bool, err error) {
	// Check the cache first.
	bestHash, height := pgb.BestBlock()
	var validHeight *cache.BlockID
	bal, validHeight = pgb.AddressCache.Balance(address)
	if bal != nil && *bestHash == validHeight.Hash {
		return
	}

	busy, wait, done := pgb.CacheLocks.bal.TryLock(address)
	if busy {
		// Let others get the wait channel while we wait.
		// To return stale cache data if it is available:
		// utxos, _ := pgb.AddressCache.UTXOs(address)
		// if utxos != nil {
		// 	return utxos, nil
		// }
		<-wait

		// Try again, starting with the cache.
		return pgb.AddressBalance(address)
	}

	// We will run the DB query, so block others from doing the same. When query
	// and/or cache update is completed, broadcast to any waiters that the coast
	// is clear.
	defer done()

	// Cache is empty or stale, so query the DB.
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	bal, err = RetrieveAddressBalance(ctx, pgb.db, address)
	if err != nil {
		err = pgb.replaceCancelError(err)
		return
	}

	// Update the address cache.
	cacheUpdated = pgb.AddressCache.StoreBalance(address, bal,
		cache.NewBlockID(bestHash, height))
	return
}

// updateAddressRows updates address rows, or waits for them to update by an
// ongoing query. On completion, the cache should be ready, although it must be
// checked again.
func (pgb *ChainDB) updateAddressRows(address string) (rows []*dbtypes.AddressRow, cacheUpdated bool, err error) {
	busy, wait, done := pgb.CacheLocks.rows.TryLock(address)
	if busy {
		// Just wait until the updater is finished.
		<-wait
		err = fmt.Errorf("retry")
		return
	}

	// We will run the DB query, so block others from doing the same. When query
	// and/or cache update is completed, broadcast to any waiters that the coast
	// is clear.
	defer done()

	hash, height := pgb.BestBlock()
	blockID := cache.NewBlockID(hash, height)

	// Retrieve all non-merged address transaction rows.
	rows, err = pgb.AddressTransactionsAll(address)
	if err != nil && err != sql.ErrNoRows {
		return
	}

	// Update address rows cache.
	cacheUpdated = pgb.AddressCache.StoreRows(address, rows, blockID)
	return
}

// AddressRowsMerged gets the merged address rows either from cache or via DB
// query.
func (pgb *ChainDB) AddressRowsMerged(address string) ([]dbtypes.AddressRowMerged, error) {
	// Try the address cache.
	hash := pgb.BestBlockHash()
	rowsCompact, validBlock := pgb.AddressCache.Rows(address)
	cacheCurrent := validBlock != nil && validBlock.Hash == *hash && rowsCompact != nil
	if cacheCurrent {
		log.Tracef("AddressRows: rows cache HIT for %s.", address)
		return dbtypes.MergeRowsCompact(rowsCompact), nil
	}

	log.Tracef("AddressRows: rows cache MISS for %s.", address)

	// Update or wait for an update to the cached AddressRows.
	rows, _, err := pgb.updateAddressRows(address)
	if err != nil {
		if err.Error() == "retry" {
			// Try again, starting with cache.
			return pgb.AddressRowsMerged(address)
		}
		return nil, err
	}

	// We have a result.
	return dbtypes.MergeRows(rows)
}

// AddressRowsCompact gets non-merged address rows either from cache or via DB
// query.
func (pgb *ChainDB) AddressRowsCompact(address string) ([]dbtypes.AddressRowCompact, error) {
	// Try the address cache.
	hash := pgb.BestBlockHash()
	rowsCompact, validBlock := pgb.AddressCache.Rows(address)
	cacheCurrent := validBlock != nil && validBlock.Hash == *hash && rowsCompact != nil
	if cacheCurrent {
		log.Tracef("AddressRows: rows cache HIT for %s.", address)
		return rowsCompact, nil
	}

	log.Tracef("AddressRows: rows cache MISS for %s.", address)

	// Update or wait for an update to the cached AddressRows.
	rows, _, err := pgb.updateAddressRows(address)
	if err != nil {
		if err.Error() == "retry" {
			// Try again, starting with cache.
			return pgb.AddressRowsCompact(address)
		}
		return nil, err
	}

	// We have a result.
	return dbtypes.CompactRows(rows), err
}

// retrieveMergedTxnCount queries the DB for the merged address transaction view
// row count.
func (pgb *ChainDB) retrieveMergedTxnCount(addr string, txnView dbtypes.AddrTxnViewType) (int, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var count int64
	var err error
	switch txnView {
	case dbtypes.AddrMergedTxnDebit:
		count, err = CountMergedSpendingTxns(ctx, pgb.db, addr)
	case dbtypes.AddrMergedTxnCredit:
		count, err = CountMergedFundingTxns(ctx, pgb.db, addr)
	case dbtypes.AddrMergedTxn:
		count, err = CountMergedTxns(ctx, pgb.db, addr)
	default:
		return 0, fmt.Errorf("retrieveMergedTxnCount: requested count for non-merged view")
	}
	return int(count), err
}

// mergedTxnCount checks cache and falls back to retrieveMergedTxnCount.
func (pgb *ChainDB) mergedTxnCount(addr string, txnView dbtypes.AddrTxnViewType) (int, error) {
	// Try the cache first.
	rows, blockID := pgb.AddressCache.Rows(addr)
	if blockID == nil {
		// Query the DB.
		return pgb.retrieveMergedTxnCount(addr, txnView)
	}

	return dbtypes.CountMergedRowsCompact(rows, txnView)
}

// nonMergedTxnCount gets the non-merged address transaction view row count via
// AddressBalance, which checks the cache and falls back to a DB query.
func (pgb *ChainDB) nonMergedTxnCount(addr string, txnView dbtypes.AddrTxnViewType) (int, error) {
	bal, _, err := pgb.AddressBalance(addr)
	if err != nil {
		return 0, err
	}
	var count int64
	switch txnView {
	case dbtypes.AddrTxnAll:
		count = (bal.NumSpent * 2) + bal.NumUnspent
	case dbtypes.AddrTxnCredit:
		count = bal.NumSpent + bal.NumUnspent
	case dbtypes.AddrTxnDebit:
		count = bal.NumSpent
	default:
		return 0, fmt.Errorf("NonMergedTxnCount: requested count for merged view")
	}
	return int(count), nil
}

// CountTransactions gets the total row count for the given address and address
// transaction view.
func (pgb *ChainDB) CountTransactions(addr string, txnView dbtypes.AddrTxnViewType) (int, error) {
	merged, err := txnView.IsMerged()
	if err != nil {
		return 0, err
	}

	countFn := pgb.nonMergedTxnCount
	if merged {
		countFn = pgb.mergedTxnCount
	}

	count, err := countFn(addr, txnView)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// AddressHistory queries the database for rows of the addresses table
// containing values for a certain type of transaction (all, credits, or debits)
// for the given address.
func (pgb *ChainDB) AddressHistory(address string, N, offset int64,
	txnView dbtypes.AddrTxnViewType) ([]*dbtypes.AddressRow, *dbtypes.AddressBalance, error) {
	// Try the address rows cache.
	hash, height := pgb.BestBlock()
	addressRows, validBlock, err := pgb.AddressCache.Transactions(address, N, offset, txnView)
	if err != nil {
		return nil, nil, err
	}

	cacheCurrent := validBlock != nil && validBlock.Hash == *hash
	if !cacheCurrent {
		log.Debugf("Address rows (view=%s) cache MISS for %s.",
			txnView.String(), address)

		// Update or wait for an update to the cached AddressRows.
		addressRows, _, err = pgb.updateAddressRows(address)
		// See if another caller ran the update, in which case we were just
		// waiting to avoid a simultaneous query. With luck the cache will be
		// updated with this data, although it may not be. Try again.
		if err != nil && err != sql.ErrNoRows {
			if err.Error() == "retry" {
				// Try again, starting with cache.
				return pgb.AddressHistory(address, N, offset, txnView)
			}
			return nil, nil, err
		}
	}
	log.Debugf("Address rows (view=%s) cache HIT for %s.",
		txnView.String(), address)

	// addressRows is now present and current. Proceed to get the balance.

	// Try the address balance cache.
	balance, validBlock := pgb.AddressCache.Balance(address)
	cacheCurrent = validBlock != nil && validBlock.Hash == *hash
	if cacheCurrent {
		log.Debugf("Address balance cache HIT for %s.", address)
		return addressRows, balance, nil
	}
	log.Debugf("Address balance cache MISS for %s.", address)

	// Short cut: we have all txs when the total number of fetched txs is less
	// than the limit, txtype is AddrTxnAll, and Offset is zero.
	if len(addressRows) < int(N) && offset == 0 && txnView == dbtypes.AddrTxnAll {
		log.Debugf("Taking balance shortcut since address rows includes all.")
		// Zero balances and txn counts when rows is zero length.
		if len(addressRows) == 0 {
			balance = &dbtypes.AddressBalance{
				Address: address,
			}
		} else {
			addrInfo, fromStake, toStake := dbtypes.ReduceAddressHistory(addressRows)
			if addrInfo == nil {
				return addressRows, nil,
					fmt.Errorf("ReduceAddressHistory failed. len(addressRows) = %d",
						len(addressRows))
			}

			balance = &dbtypes.AddressBalance{
				Address:      address,
				NumSpent:     addrInfo.NumSpendingTxns,
				NumUnspent:   addrInfo.NumFundingTxns - addrInfo.NumSpendingTxns,
				TotalSpent:   int64(addrInfo.AmountSent),
				TotalUnspent: int64(addrInfo.AmountUnspent),
				FromStake:    fromStake,
				ToStake:      toStake,
			}
		}
		// Update balance cache.
		blockID := cache.NewBlockID(hash, height)
		pgb.AddressCache.StoreBalance(address, balance, blockID)
	} else {
		// Count spent/unspent amounts and transactions.
		log.Debugf("Obtaining balance via DB query.")
		balance, _, err = pgb.AddressBalance(address)
		if err != nil && err != sql.ErrNoRows {
			return nil, nil, err
		}
	}

	log.Infof("%s: %d spent totalling %f DCR, %d unspent totalling %f DCR",
		address, balance.NumSpent, dcrutil.Amount(balance.TotalSpent).ToCoin(),
		balance.NumUnspent, dcrutil.Amount(balance.TotalUnspent).ToCoin())
	log.Infof("Receive count for address %s: count = %d at block %d.",
		address, balance.NumSpent+balance.NumUnspent, height)

	return addressRows, balance, nil
}

// AddressData returns comprehensive, paginated information for an address.
func (pgb *ChainDBRPC) AddressData(address string, limitN, offsetAddrOuts int64,
	txnType dbtypes.AddrTxnViewType) (addrData *dbtypes.AddressInfo, err error) {
	merged, err := txnType.IsMerged()
	if err != nil {
		return nil, err
	}

	addrHist, balance, err := pgb.AddressHistory(address, limitN, offsetAddrOuts, txnType)
	if dbtypes.IsTimeoutErr(err) {
		return nil, err
	}

	populateTemplate := func() {
		addrData.Offset = offsetAddrOuts
		addrData.Limit = limitN
		addrData.TxnType = txnType.String()
		addrData.Address = address
		addrData.Fullmode = true
	}

	if err == sql.ErrNoRows || (err == nil && len(addrHist) == 0) {
		// We do not have any confirmed transactions. Prep to display ONLY
		// unconfirmed transactions (or none at all).
		addrData = new(dbtypes.AddressInfo)
		populateTemplate()
		addrData.Balance = &dbtypes.AddressBalance{}
		log.Tracef("AddressHistory: No confirmed transactions for address %s.", address)
	} else if err != nil {
		// Unexpected error
		log.Errorf("AddressHistory: %v", err)
		return nil, fmt.Errorf("AddressHistory: %v", err)
	} else /*err == nil*/ {
		// Generate AddressInfo skeleton from the address table rows.
		addrData, _, _ = dbtypes.ReduceAddressHistory(addrHist)
		if addrData == nil {
			// Empty history is not expected for credit or all txnType with any
			// txns. i.e. Empty history is OK for debit views (merged or not).
			if (txnType != dbtypes.AddrTxnDebit && txnType != dbtypes.AddrMergedTxnDebit) &&
				(balance.NumSpent+balance.NumUnspent) > 0 {
				log.Debugf("empty address history (%s) for view %s: n=%d&start=%d",
					address, txnType.String(), limitN, offsetAddrOuts)
				return nil, fmt.Errorf("that address has no history")
			}
			addrData = new(dbtypes.AddressInfo)
		}
		// Balances and txn counts (partial unless in full mode)
		populateTemplate()
		addrData.Balance = balance
		addrData.KnownTransactions = (balance.NumSpent * 2) + balance.NumUnspent
		addrData.KnownFundingTxns = balance.NumSpent + balance.NumUnspent
		addrData.KnownSpendingTxns = balance.NumSpent

		// Obtain the TxnCount, which pertains to the number of table rows.
		addrData.IsMerged, err = txnType.IsMerged()
		if err != nil {
			return nil, err
		}
		if addrData.IsMerged {
			// For merged views, check the cache and fall back on a DB query.
			count, err := pgb.mergedTxnCount(address, txnType)
			if err != nil {
				return nil, err
			}
			addrData.TxnCount = int64(count)
		} else {
			// For non-merged views, use the balance data.
			switch txnType {
			case dbtypes.AddrTxnAll:
				addrData.TxnCount = addrData.KnownFundingTxns + addrData.KnownSpendingTxns
			case dbtypes.AddrTxnCredit:
				addrData.TxnCount = addrData.KnownFundingTxns
				addrData.Transactions = addrData.TxnsFunding
			case dbtypes.AddrTxnDebit:
				addrData.TxnCount = addrData.KnownSpendingTxns
				addrData.Transactions = addrData.TxnsSpending
			}
		}

		// Transactions on current page
		addrData.NumTransactions = int64(len(addrData.Transactions))
		if addrData.NumTransactions > limitN {
			addrData.NumTransactions = limitN
		}

		// Query database for transaction details.
		err = pgb.FillAddressTransactions(addrData)
		if dbtypes.IsTimeoutErr(err) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("Unable to fill address %s transactions: %v", address, err)
		}
	}

	// Check for unconfirmed transactions.
	addressUTXOs, numUnconfirmed, err := pgb.mp.UnconfirmedTxnsForAddress(address)
	if err != nil || addressUTXOs == nil {
		return nil, fmt.Errorf("UnconfirmedTxnsForAddress failed for address %s: %v", address, err)
	}
	addrData.NumUnconfirmed = numUnconfirmed
	addrData.NumTransactions += numUnconfirmed
	if addrData.UnconfirmedTxns == nil {
		addrData.UnconfirmedTxns = new(dbtypes.AddressTransactions)
	}

	// Funding transactions (unconfirmed)
	var received, sent, numReceived, numSent int64
FUNDING_TX_DUPLICATE_CHECK:
	for _, f := range addressUTXOs.Outpoints {
		// TODO: handle merged transactions
		if merged {
			break FUNDING_TX_DUPLICATE_CHECK
		}

		// Mempool transactions stick around for 2 blocks. The first block
		// incorporates the transaction and mines it. The second block
		// validates it by the stake. However, transactions move into our
		// database as soon as they are mined and thus we need to be careful
		// to not include those transactions in our list.
		for _, b := range addrData.Transactions {
			if f.Hash.String() == b.TxID && f.Index == b.InOutID {
				continue FUNDING_TX_DUPLICATE_CHECK
			}
		}
		fundingTx, ok := addressUTXOs.TxnsStore[f.Hash]
		if !ok {
			log.Errorf("An outpoint's transaction is not available in TxnStore.")
			continue
		}
		if fundingTx.Confirmed() {
			log.Errorf("An outpoint's transaction is unexpectedly confirmed.")
			continue
		}
		if txnType == dbtypes.AddrTxnAll || txnType == dbtypes.AddrTxnCredit {
			addrTx := &dbtypes.AddressTx{
				TxID:          fundingTx.Hash().String(),
				TxType:        txhelpers.DetermineTxTypeString(fundingTx.Tx),
				InOutID:       f.Index,
				Time:          dbtypes.NewTimeDefFromUNIX(fundingTx.MemPoolTime),
				FormattedSize: humanize.Bytes(uint64(fundingTx.Tx.SerializeSize())),
				Total:         txhelpers.TotalOutFromMsgTx(fundingTx.Tx).ToCoin(),
				ReceivedTotal: dcrutil.Amount(fundingTx.Tx.TxOut[f.Index].Value).ToCoin(),
			}
			addrData.Transactions = append(addrData.Transactions, addrTx)
		}
		received += fundingTx.Tx.TxOut[f.Index].Value
		numReceived++

	}

	// Spending transactions (unconfirmed)
SPENDING_TX_DUPLICATE_CHECK:
	for _, f := range addressUTXOs.PrevOuts {
		// TODO: handle merged transactions
		if merged {
			break SPENDING_TX_DUPLICATE_CHECK
		}

		// Mempool transactions stick around for 2 blocks. The first block
		// incorporates the transaction and mines it. The second block
		// validates it by the stake. However, transactions move into our
		// database as soon as they are mined and thus we need to be careful
		// to not include those transactions in our list.
		for _, b := range addrData.Transactions {
			if f.TxSpending.String() == b.TxID && f.InputIndex == int(b.InOutID) {
				continue SPENDING_TX_DUPLICATE_CHECK
			}
		}
		spendingTx, ok := addressUTXOs.TxnsStore[f.TxSpending]
		if !ok {
			log.Errorf("A previous outpoint's spending transaction is not available in TxnStore.")
			continue
		}
		if spendingTx.Confirmed() {
			log.Errorf("An outpoint's transaction is unexpectedly confirmed.")
			continue
		}

		// The total send amount must be looked up from the previous
		// outpoint because vin:i valuein is not reliable from dcrd.
		prevhash := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Hash
		strprevhash := prevhash.String()
		previndex := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Index
		valuein := addressUTXOs.TxnsStore[prevhash].Tx.TxOut[previndex].Value

		// Look through old transactions and set the spending transactions'
		// matching transaction fields.
		for _, dbTxn := range addrData.Transactions {
			if dbTxn.TxID == strprevhash && dbTxn.InOutID == previndex && dbTxn.IsFunding {
				dbTxn.MatchedTx = spendingTx.Hash().String()
				dbTxn.MatchedTxIndex = uint32(f.InputIndex)
			}
		}

		if txnType == dbtypes.AddrTxnAll || txnType == dbtypes.AddrTxnDebit {
			addrTx := &dbtypes.AddressTx{
				TxID:           spendingTx.Hash().String(),
				TxType:         txhelpers.DetermineTxTypeString(spendingTx.Tx),
				InOutID:        uint32(f.InputIndex),
				Time:           dbtypes.NewTimeDefFromUNIX(spendingTx.MemPoolTime),
				FormattedSize:  humanize.Bytes(uint64(spendingTx.Tx.SerializeSize())),
				Total:          txhelpers.TotalOutFromMsgTx(spendingTx.Tx).ToCoin(),
				SentTotal:      dcrutil.Amount(valuein).ToCoin(),
				MatchedTx:      strprevhash,
				MatchedTxIndex: previndex,
			}
			addrData.Transactions = append(addrData.Transactions, addrTx)
		}

		sent += valuein
		numSent++
	} // range addressUTXOs.PrevOuts

	// Totals from funding and spending transactions.
	addrData.Balance.NumSpent += numSent
	addrData.Balance.NumUnspent += (numReceived - numSent)
	addrData.Balance.TotalSpent += sent
	addrData.Balance.TotalUnspent += (received - sent)

	// Sort by date and calculate block height.
	addrData.PostProcess(uint32(pgb.Height()))

	return
}

// DbTxByHash retrieves a row of the transactions table corresponding to the
// given transaction hash. Transactions in valid and mainchain blocks are chosen
// first.
func (pgb *ChainDB) DbTxByHash(txid string) (*dbtypes.Tx, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, dbTx, err := RetrieveDbTxByHash(ctx, pgb.db, txid)
	return dbTx, pgb.replaceCancelError(err)
}

// FundingOutpointIndxByVinID retrieves the the transaction output index of the
// previous outpoint for a transaction input specified by row ID in the vins
// table, which stores previous outpoints for each vin.
func (pgb *ChainDB) FundingOutpointIndxByVinID(id uint64) (uint32, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	ind, err := RetrieveFundingOutpointIndxByVinID(ctx, pgb.db, id)
	return ind, pgb.replaceCancelError(err)
}

// FillAddressTransactions is used to fill out the transaction details in an
// explorer.AddressInfo generated by dbtypes.ReduceAddressHistory, usually from
// the output of AddressHistory. This function also sets the number of
// unconfirmed transactions for the current best block in the database.
func (pgb *ChainDB) FillAddressTransactions(addrInfo *dbtypes.AddressInfo) error {
	if addrInfo == nil {
		return nil
	}

	var numUnconfirmed int64

	for i, txn := range addrInfo.Transactions {
		// Retrieve the most valid, most mainchain, and most recent tx with this
		// hash. This means it prefers mainchain and valid blocks first.
		dbTx, err := pgb.DbTxByHash(txn.TxID)
		if err != nil {
			return err
		}
		txn.Size = dbTx.Size
		txn.FormattedSize = humanize.Bytes(uint64(dbTx.Size))
		txn.Total = dcrutil.Amount(dbTx.Sent).ToCoin()
		txn.Time = dbTx.BlockTime
		if txn.Time.UNIX() > 0 {
			txn.Confirmations = uint64(pgb.Height() - dbTx.BlockHeight + 1)
		} else {
			numUnconfirmed++
			txn.Confirmations = 0
		}

		// Get the funding or spending transaction matching index if there is a
		// matching tx hash already present.  During the next database
		// restructuring we may want to consider including matching tx index
		// along with matching tx hash in the addresses table.
		if txn.MatchedTx != `` {
			if !txn.IsFunding {
				// Spending transaction: lookup the previous outpoint's txout
				// index by the vins table row ID.
				idx, err := pgb.FundingOutpointIndxByVinID(dbTx.VinDbIds[txn.InOutID])
				if err != nil {
					log.Warnf("Matched Transaction Lookup failed for %s:%d: id: %d:  %v",
						txn.TxID, txn.InOutID, txn.InOutID, err)
				} else {
					addrInfo.Transactions[i].MatchedTxIndex = idx
				}

			} else {
				// Funding transaction: lookup by the matching (spending) tx
				// hash and tx index.
				_, idx, _, err := pgb.SpendingTransaction(txn.TxID, txn.InOutID)
				if err != nil {
					log.Warnf("Matched Transaction Lookup failed for %s:%d: %v",
						txn.TxID, txn.InOutID, err)
				} else {
					addrInfo.Transactions[i].MatchedTxIndex = idx
				}
			}
		}
	}

	addrInfo.NumUnconfirmed = numUnconfirmed

	return nil
}

// AddressTotals queries for the following totals: amount spent, amount unspent,
// number of unspent transaction outputs and number spent.
func (pgb *ChainDB) AddressTotals(address string) (*apitypes.AddressTotals, error) {
	// Fetch address totals
	var err error
	var ab *dbtypes.AddressBalance
	if address == pgb.devAddress {
		ab, err = pgb.DevBalance()
	} else {
		ab, _, err = pgb.AddressBalance(address)
	}

	if err != nil || ab == nil {
		return nil, err
	}

	bestHash, bestHeight := pgb.BestBlockStr()

	return &apitypes.AddressTotals{
		Address:      address,
		BlockHeight:  uint64(bestHeight),
		BlockHash:    bestHash,
		NumSpent:     ab.NumSpent,
		NumUnspent:   ab.NumUnspent,
		CoinsSpent:   dcrutil.Amount(ab.TotalSpent).ToCoin(),
		CoinsUnspent: dcrutil.Amount(ab.TotalUnspent).ToCoin(),
	}, nil
}

// MakeCsvAddressRows converts an AddressRow slice into a [][]string, including
// column headers, suitable for saving to CSV.
func MakeCsvAddressRows(rows []*dbtypes.AddressRow) [][]string {
	csvRows := make([][]string, 0, len(rows)+1)
	csvRows = append(csvRows, []string{"tx_hash", "direction", "io_index",
		"valid_mainchain", "value", "time_stamp", "tx_type", "matching_tx_hash"})

	for _, r := range rows {
		var strValidMainchain string
		if r.ValidMainChain {
			strValidMainchain = "1"
		} else {
			strValidMainchain = "0"
		}

		var strDirection string
		if r.IsFunding {
			strDirection = "1"
		} else {
			strDirection = "-1"
		}

		csvRows = append(csvRows, []string{
			r.TxHash,
			strDirection,
			strconv.Itoa(int(r.TxVinVoutIndex)),
			strValidMainchain,
			strconv.FormatFloat(dcrutil.Amount(r.Value).ToCoin(), 'f', -1, 64),
			strconv.FormatInt(r.TxBlockTime.UNIX(), 10),
			txhelpers.TxTypeToString(int(r.TxType)),
			r.MatchingTxHash,
		})
	}
	return csvRows
}

func MakeCsvAddressRowsCompact(rows []dbtypes.AddressRowCompact) [][]string {
	csvRows := make([][]string, 0, len(rows)+1)
	csvRows = append(csvRows, []string{"tx_hash", "direction", "io_index",
		"valid_mainchain", "value", "time_stamp", "tx_type", "matching_tx_hash"})

	for _, r := range rows {
		var strValidMainchain string
		if r.ValidMainChain {
			strValidMainchain = "1"
		} else {
			strValidMainchain = "0"
		}

		var strDirection string
		if r.IsFunding {
			strDirection = "1"
		} else {
			strDirection = "-1"
		}

		csvRows = append(csvRows, []string{
			r.TxHash.String(),
			strDirection,
			strconv.Itoa(int(r.TxVinVoutIndex)),
			strValidMainchain,
			strconv.FormatFloat(dcrutil.Amount(r.Value).ToCoin(), 'f', -1, 64),
			strconv.FormatInt(r.TxBlockTime, 10),
			txhelpers.TxTypeToString(int(r.TxType)),
			r.MatchingTxHash.String(),
		})
	}
	return csvRows
}

// AddressTxIoCsv grabs rows of an address' transaction input/output data as a
// 2-D array of strings to be CSV-formatted.
func (pgb *ChainDB) AddressTxIoCsv(address string) ([][]string, error) {
	rows, err := pgb.AddressRowsCompact(address)
	if err != nil {
		return nil, err
	}

	return MakeCsvAddressRowsCompact(rows), nil

	// ALT implementation, without cache update:

	// Try the address rows cache.
	// hash := pgb.BestBlockHash()
	// rows, validBlock := pgb.AddressCache.Rows(address)
	// cacheCurrent := validBlock != nil && validBlock.Hash == *hash
	// if cacheCurrent && rows != nil {
	// 	log.Debugf("AddressTxIoCsv: Merged address rows cache HIT for %s.", address)

	// 	csvRows := make([][]string, 0, len(rows)+1)
	// 	csvRows = append(csvRows, []string{"tx_hash", "direction", "io_index",
	// 		"valid_mainchain", "value", "time_stamp", "tx_type", "matching_tx_hash"})

	// 	for _, r := range rows {
	// 		var strValidMainchain string
	// 		if r.ValidMainChain {
	// 			strValidMainchain = "1"
	// 		} else {
	// 			strValidMainchain = "0"
	// 		}

	// 		var strDirection string
	// 		if r.IsFunding {
	// 			strDirection = "1"
	// 		} else {
	// 			strDirection = "-1"
	// 		}

	// 		csvRows = append(csvRows, []string{
	// 			r.TxHash,
	// 			strDirection,
	// 			strconv.Itoa(int(r.TxVinVoutIndex)),
	// 			strValidMainchain,
	// 			strconv.FormatFloat(dcrutil.Amount(r.Value).ToCoin(), 'f', -1, 64),
	// 			strconv.FormatInt(r.TxBlockTime.UNIX(), 10),
	// 			txhelpers.TxTypeToString(int(r.TxType)),
	// 			r.MatchingTxHash,
	// 		})
	// 	}

	// 	return csvRows, nil
	// }

	// log.Debugf("AddressTxIoCsv: Merged address rows cache MISS for %s.", address)

	// ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	// defer cancel()

	// csvRows, err := retrieveAddressIoCsv(ctx, pgb.db, address)
	// if err != nil {
	// 	return nil, fmt.Errorf("retrieveAddressIoCsv: %v", err)
	// }
	// return csvRows, nil
}

func (pgb *ChainDB) addressInfo(addr string, count, skip int64, txnType dbtypes.AddrTxnViewType) (*dbtypes.AddressInfo, *dbtypes.AddressBalance, error) {
	address, err := dcrutil.DecodeAddress(addr)
	if err != nil {
		log.Infof("Invalid address %s: %v", addr, err)
		return nil, nil, err
	}

	// Get rows from the addresses table for the address
	addrHist, balance, err := pgb.AddressHistory(addr, count, skip, txnType)
	if err != nil {
		log.Errorf("Unable to get address %s history: %v", address, err)
		return nil, nil, err
	}

	// Generate AddressInfo skeleton from the address table rows
	addrData, _, _ := dbtypes.ReduceAddressHistory(addrHist)
	if addrData == nil {
		// Empty history is not expected for credit txnType with any txns.
		if txnType != dbtypes.AddrTxnDebit && (balance.NumSpent+balance.NumUnspent) > 0 {
			return nil, nil, fmt.Errorf("empty address history (%s): n=%d&start=%d", address, count, skip)
		}
		// No mined transactions. Return Address with nil Transactions slice.
		return nil, balance, nil
	}

	// Transactions to fetch with FillAddressTransactions. This should be a
	// noop if AddressHistory/ReduceAddressHistory are working right.
	switch txnType {
	case dbtypes.AddrTxnAll, dbtypes.AddrMergedTxnDebit:
	case dbtypes.AddrTxnCredit:
		addrData.Transactions = addrData.TxnsFunding
	case dbtypes.AddrTxnDebit:
		addrData.Transactions = addrData.TxnsSpending
	default:
		// shouldn't happen because AddressHistory does this check
		return nil, nil, fmt.Errorf("unknown address transaction type: %v", txnType)
	}

	// Query database for transaction details
	err = pgb.FillAddressTransactions(addrData)
	if err != nil {
		return nil, balance, fmt.Errorf("Unable to fill address %s transactions: %v", address, err)
	}

	return addrData, balance, nil
}

// AddressTransactionDetails returns an apitypes.Address with at most the last
// count transactions of type txnType in which the address was involved,
// starting after skip transactions. This does NOT include unconfirmed
// transactions.
func (pgb *ChainDB) AddressTransactionDetails(addr string, count, skip int64,
	txnType dbtypes.AddrTxnViewType) (*apitypes.Address, error) {
	// Fetch address history for given transaction range and type
	addrData, _, err := pgb.addressInfo(addr, count, skip, txnType)
	if err != nil {
		return nil, err
	}
	// No transactions found. Not an error.
	if addrData == nil {
		return &apitypes.Address{
			Address:      addr,
			Transactions: make([]*apitypes.AddressTxShort, 0), // not nil for JSON formatting
		}, nil
	}

	// Convert each dbtypes.AddressTx to apitypes.AddressTxShort
	txs := addrData.Transactions
	txsShort := make([]*apitypes.AddressTxShort, 0, len(txs))
	for i := range txs {
		txsShort = append(txsShort, &apitypes.AddressTxShort{
			TxID:          txs[i].TxID,
			Time:          apitypes.TimeAPI{S: txs[i].Time},
			Value:         txs[i].Total,
			Confirmations: int64(txs[i].Confirmations),
			Size:          int32(txs[i].Size),
		})
	}

	// put a bow on it
	return &apitypes.Address{
		Address:      addr,
		Transactions: txsShort,
	}, nil
}

// TODO: finish
func (pgb *ChainDB) AddressTransactionRawDetails(addr string, count, skip int64,
	txnType dbtypes.AddrTxnViewType) ([]*apitypes.AddressTxRaw, error) {
	addrData, _, err := pgb.addressInfo(addr, count, skip, txnType)
	if err != nil {
		return nil, err
	}

	// Convert each dbtypes.AddressTx to apitypes.AddressTxRaw
	txs := addrData.Transactions
	txsRaw := make([]*apitypes.AddressTxRaw, 0, len(txs))
	for i := range txs {
		txsRaw = append(txsRaw, &apitypes.AddressTxRaw{
			Size: int32(txs[i].Size),
			TxID: txs[i].TxID,
			// Version
			// LockTime
			// Vin
			// Vout
			//
			Confirmations: int64(txs[i].Confirmations),
			//BlockHash: txs[i].
			Time: apitypes.TimeAPI{S: txs[i].Time},
			//Blocktime:
		})
	}

	return txsRaw, nil
}

// UpdateChainState updates the blockchain's state, which includes each of the
// agenda's VotingDone and Activated heights. If the agenda passed (i.e. status
// is "lockedIn" or "activated"), Activated is set to the height at which the rule
// change will take(or took) place.
func (pgb *ChainDB) UpdateChainState(blockChainInfo *dcrjson.GetBlockChainInfoResult) {
	if pgb == nil {
		return
	}
	if blockChainInfo == nil {
		log.Errorf("dcrjson.GetBlockChainInfoResult data passed is empty")
		return
	}

	ruleChangeInterval := int64(pgb.chainParams.RuleChangeActivationInterval)

	chainInfo := dbtypes.BlockChainData{
		Chain:                  blockChainInfo.Chain,
		SyncHeight:             blockChainInfo.SyncHeight,
		BestHeight:             blockChainInfo.Blocks,
		BestBlockHash:          blockChainInfo.BestBlockHash,
		Difficulty:             blockChainInfo.Difficulty,
		VerificationProgress:   blockChainInfo.VerificationProgress,
		ChainWork:              blockChainInfo.ChainWork,
		IsInitialBlockDownload: blockChainInfo.InitialBlockDownload,
		MaxBlockSize:           blockChainInfo.MaxBlockSize,
	}

	var voteMilestones = make(map[string]dbtypes.MileStone)

	for agendaID, entry := range blockChainInfo.Deployments {
		var agendaInfo = dbtypes.MileStone{
			Status:     dbtypes.AgendaStatusFromStr(entry.Status),
			StartTime:  time.Unix(int64(entry.StartTime), 0).UTC(),
			ExpireTime: time.Unix(int64(entry.ExpireTime), 0).UTC(),
		}

		// The period between Voting start height to voting end height takes
		// chainParams.RuleChangeActivationInterval blocks to change. The Period
		// between Voting Done and activation also takes
		// chainParams.RuleChangeActivationInterval blocks
		switch agendaInfo.Status {
		case dbtypes.StartedAgendaStatus:
			agendaInfo.VotingDone = entry.Since + ruleChangeInterval
			agendaInfo.Activated = agendaInfo.VotingDone + ruleChangeInterval

		case dbtypes.FailedAgendaStatus:
			agendaInfo.VotingDone = entry.Since

		case dbtypes.LockedInAgendaStatus:
			agendaInfo.VotingDone = entry.Since
			agendaInfo.Activated = entry.Since + ruleChangeInterval

		case dbtypes.ActivatedAgendaStatus:
			agendaInfo.VotingDone = entry.Since - ruleChangeInterval
			agendaInfo.Activated = entry.Since
		}

		agendaInfo.VotingStarted = agendaInfo.VotingDone - ruleChangeInterval

		voteMilestones[agendaID] = agendaInfo
	}

	chainInfo.AgendaMileStones = voteMilestones

	pgb.deployments.mtx.Lock()
	defer pgb.deployments.mtx.Unlock()

	pgb.deployments.chainInfo = &chainInfo
}

// ChainInfo guarantees thread-safe access of the deployment data.
func (pgb *ChainDB) ChainInfo() *dbtypes.BlockChainData {
	pgb.deployments.mtx.RLock()
	defer pgb.deployments.mtx.RUnlock()
	return pgb.deployments.chainInfo
}

// Store satisfies BlockDataSaver. Blocks stored this way are considered valid
// and part of mainchain. Store should not be used for batch block processing;
// instead, use StoreBlock and specify appropriate flags.
func (pgb *ChainDB) Store(blockData *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	// This function must handle being run when pgb is nil (not constructed).
	if pgb == nil {
		return nil
	}

	// New blocks stored this way are considered valid and part of mainchain,
	// warranting updates to existing records. When adding side chain blocks
	// manually, call StoreBlock directly with appropriate flags for isValid,
	// isMainchain, and updateExistingRecords, and nil winningTickets.
	isValid, isMainChain, updateExistingRecords := true, true, true

	// Since Store should not be used in batch block processing, addresses and
	// tickets spending information is updated.
	updateAddressesSpendingInfo, updateTicketsSpendingInfo := true, true

	_, _, _, err := pgb.StoreBlock(msgBlock, blockData.WinningTickets,
		isValid, isMainChain, updateExistingRecords, updateAddressesSpendingInfo,
		updateTicketsSpendingInfo, blockData.Header.ChainWork)
	return err
}

// PurgeBestBlocks deletes all data for the N best blocks in the DB.
func (pgb *ChainDB) PurgeBestBlocks(N int64) (*dbtypes.DeletionSummary, int64, error) {
	res, height, _, err := DeleteBlocks(pgb.ctx, N, pgb.db)
	if err != nil {
		return nil, int64(height), pgb.replaceCancelError(err)
	}

	summary := dbtypes.DeletionSummarySlice(res).Reduce()

	return &summary, int64(height), err
}

// TxHistoryData fetches the address history chart data for specified chart
// type and time grouping.
func (pgb *ChainDB) TxHistoryData(address string, addrChart dbtypes.HistoryChart,
	chartGroupings dbtypes.TimeBasedGrouping) (cd *dbtypes.ChartsData, err error) {
	// First check cache for this address' chart data of the given type and
	// interval.
	bestHash, height := pgb.BestBlock()
	var validHeight *cache.BlockID
	cd, validHeight = pgb.AddressCache.HistoryChart(address, addrChart, chartGroupings)
	if cd != nil && *bestHash == validHeight.Hash {
		return
	}

	busy, wait, done := pgb.CacheLocks.bal.TryLock(address)
	if busy {
		// Let others get the wait channel while we wait.
		// To return stale cache data if it is available:
		// utxos, _ := pgb.AddressCache.UTXOs(address)
		// if utxos != nil {
		// 	return utxos, nil
		// }
		<-wait

		// Try again, starting with the cache.
		return pgb.TxHistoryData(address, addrChart, chartGroupings)
	}

	// We will run the DB query, so block others from doing the same. When query
	// and/or cache update is completed, broadcast to any waiters that the coast
	// is clear.
	defer done()

	timeInterval := chartGroupings.String()

	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	switch addrChart {
	case dbtypes.TxsType:
		cd, err = retrieveTxHistoryByType(ctx, pgb.db, address, timeInterval)

	case dbtypes.AmountFlow:
		cd, err = retrieveTxHistoryByAmountFlow(ctx, pgb.db, address, timeInterval)

	default:
		cd, err = nil, fmt.Errorf("unknown error occurred")
	}
	err = pgb.replaceCancelError(err)
	if err != nil {
		return
	}

	// Update cache.
	_ = pgb.AddressCache.StoreHistoryChart(address, addrChart, chartGroupings,
		cd, cache.NewBlockID(bestHash, height))
	return
}

// TicketsPriceByHeight returns the ticket price by height chart data. This is
// the default chart that appears at charts page.
func (pgb *ChainDB) TicketsPriceByHeight() (*dbtypes.ChartsData, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	data := new(dbtypes.ChartsData)

	data.Time, data.ValueF, _, err = retrieveTicketsPriceByHeight(ctx, pgb.db,
		pgb.chainParams.StakeDiffWindowSize, data.Time, data.ValueF, []float64{})
	if err != nil {
		return nil, pgb.replaceCancelError(err)
	}

	return data, nil
}

// TicketsByPrice returns chart data for tickets grouped by price. maturityBlock
// is used to define when tickets are considered live.
func (pgb *ChainDB) TicketsByPrice(maturityBlock int64) (*dbtypes.PoolTicketsData, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	ptd, err := retrieveTicketByPrice(ctx, pgb.db, maturityBlock)
	return ptd, pgb.replaceCancelError(err)
}

// TicketsByInputCount returns chart data for tickets grouped by number of
// inputs.
func (pgb *ChainDB) TicketsByInputCount() (*dbtypes.PoolTicketsData, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	ptd, err := retrieveTicketsGroupedByType(ctx, pgb.db)
	return ptd, pgb.replaceCancelError(err)
}

// ticketsPriceByHeight fetches the charts data from retrieveTicketsPriceByHeight.
func (pgb *ChainDB) ticketsPriceByHeight(timeArr []dbtypes.TimeDef,
	priceArr, powArr []float64) ([]dbtypes.TimeDef, []float64, []float64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	timeArr, priceArr, powArr, err = retrieveTicketsPriceByHeight(ctx, pgb.db,
		pgb.chainParams.StakeDiffWindowSize, timeArr, priceArr, powArr)
	if err != nil {
		err = fmt.Errorf("ticketsPriceByHeight: %v", pgb.replaceCancelError(err))
	}

	return timeArr, priceArr, powArr, err
}

// coinSupply fetches the coin supply chart data from retrieveCoinSupply.
func (pgb *ChainDB) coinSupply(timeArr []dbtypes.TimeDef, sumArr []float64) (
	[]dbtypes.TimeDef, []float64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	timeArr, sumArr, err = retrieveCoinSupply(ctx, pgb.db, timeArr, sumArr)
	if err != nil {
		err = fmt.Errorf("coinSupply: %v", pgb.replaceCancelError(err))
	}

	return timeArr, sumArr, err
}

// blocksByTime fetches the charts data from retrieveBlockByTime.
func (pgb *ChainDB) blocksByTime(timeArr []dbtypes.TimeDef, chainSizeArr,
	avgSizeArr []uint64) ([]dbtypes.TimeDef, []uint64, []uint64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	timeArr, chainSizeArr, avgSizeArr, err = retrieveBlockByTime(ctx, pgb.db,
		timeArr, chainSizeArr, avgSizeArr)
	if err != nil {
		err = fmt.Errorf("blocksByTime: %v", pgb.replaceCancelError(err))
	}

	return timeArr, chainSizeArr, avgSizeArr, err
}

// blocksByHeight appends context and fetches the charts data from
// retrieveBlocksByHeight.
func (pgb *ChainDB) blocksByHeight(heightArr, blocksCountArr []uint64,
	durationArr []float64) ([]uint64, []uint64, []float64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	heightArr, blocksCountArr, durationArr, err = retrieveBlocksByHeight(ctx,
		pgb.db, heightArr, blocksCountArr, durationArr)
	if err != nil {
		err = fmt.Errorf("blocksByHeight: %v", pgb.replaceCancelError(err))
	}

	return heightArr, blocksCountArr, durationArr, err
}

// txPerDay fetches the tx-per-day chart data from retrieveTxPerDay.
func (pgb *ChainDB) txPerDay(timeArr []dbtypes.TimeDef, txCountArr []uint64) (
	[]dbtypes.TimeDef, []uint64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	timeArr, txCountArr, err = retrieveTxPerDay(ctx, pgb.db, timeArr, txCountArr)
	if err != nil {
		err = fmt.Errorf("txPerDay: %v", pgb.replaceCancelError(err))
	}

	return timeArr, txCountArr, err
}

// ticketSpendTypePerBlock fetches the tickets spend type chart data from
// retrieveTicketSpendTypePerBlock.
func (pgb *ChainDB) ticketSpendTypePerBlock(heightArr, unSpentArr, revokedArr []uint64) (
	[]uint64, []uint64, []uint64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	heightArr, unSpentArr, revokedArr, err = retrieveTicketSpendTypePerBlock(ctx,
		pgb.db, heightArr, unSpentArr, revokedArr)
	if err != nil {
		err = fmt.Errorf("ticketSpendTypePerBlock: %v", pgb.replaceCancelError(err))
	}

	return heightArr, unSpentArr, revokedArr, err
}

// ticketsByBlocks fetches the tickets by blocks output count chart data from
// retrieveTicketByOutputCount
func (pgb *ChainDB) ticketsByBlocks(heightArr, soloArr, pooledArr []uint64) ([]uint64,
	[]uint64, []uint64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	heightArr, soloArr, pooledArr, err = retrieveTicketByOutputCount(ctx,
		pgb.db, 1, outputCountByAllBlocks, heightArr, soloArr, pooledArr)
	if err != nil {
		err = fmt.Errorf("ticketsByBlocks: %v", pgb.replaceCancelError(err))
	}

	return heightArr, soloArr, pooledArr, err
}

// ticketsByTPWindows fetches the tickets by ticket pool windows count chart data
// from retrieveTicketByOutputCount.
func (pgb *ChainDB) ticketsByTPWindows(heightArr, soloArr, pooledArr []uint64) ([]uint64,
	[]uint64, []uint64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	var err error
	heightArr, soloArr, pooledArr, err = retrieveTicketByOutputCount(ctx, pgb.db,
		pgb.chainParams.StakeDiffWindowSize, outputCountByTicketPoolWindow,
		heightArr, soloArr, pooledArr)
	if err != nil {
		err = fmt.Errorf("ticketsByTPWindows: %v", pgb.replaceCancelError(err))
	}

	return heightArr, soloArr, pooledArr, err
}

// chainWork fetches the chainwork charts data from
func (pgb *ChainDB) chainWork(data [2]*dbtypes.ChartsData) ([2]*dbtypes.ChartsData, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()

	chainData, err := retrieveChainWork(ctx, pgb.db, data)
	if err != nil {
		err = fmt.Errorf("chainWork: %v", pgb.replaceCancelError(err))
	}

	return chainData, err
}

// getChartData returns the chart data if it exists and initializes a new chart
// data instance if otherwise.
func (pgb *ChainDB) getChartData(data map[string]*dbtypes.ChartsData,
	chartT string) *dbtypes.ChartsData {
	cData := data[chartT]
	if cData == nil {
		cData = new(dbtypes.ChartsData)
	}
	return cData
}

// PgChartsData accepts an array of the old charts data that needs update.
// If any of the charts has no entries, its records from the oldest to the
// most recent are queries from the db pushed into the chart's data. If a given
// chart data is not empty, only the change since the last update is queried
// and pushed.
func (pgb *ChainDB) PgChartsData(data map[string]*dbtypes.ChartsData) (err error) {
	txRate := pgb.getChartData(data, dbtypes.TxPerDay)
	txRate.Time, txRate.Count, err = pgb.txPerDay(txRate.Time, txRate.Count)
	if err != nil {
		return
	}

	supply := pgb.getChartData(data, dbtypes.CoinSupply)
	supply.Time, supply.ValueF, err = pgb.coinSupply(supply.Time, supply.ValueF)
	if err != nil {
		return
	}

	chainData := [2]*dbtypes.ChartsData{
		data[dbtypes.ChainWork],
		data[dbtypes.HashRate],
	}

	chainData, err = pgb.chainWork(chainData)
	if err != nil {
		return
	}

	avgSize := pgb.getChartData(data, dbtypes.AvgBlockSize)
	blockSize := pgb.getChartData(data, dbtypes.BlockChainSize)
	blockSize.Time, blockSize.ChainSize, avgSize.Size, err = pgb.blocksByTime(blockSize.Time,
		blockSize.ChainSize, avgSize.Size)
	if err != nil {
		return
	}

	avgSize.Time = blockSize.Time

	durPerB := pgb.getChartData(data, dbtypes.DurationBTW)
	txPerB := pgb.getChartData(data, dbtypes.TxPerBlock)
	txPerB.Height, txPerB.Count, durPerB.ValueF, err = pgb.blocksByHeight(txPerB.Height,
		txPerB.Count, durPerB.ValueF)
	if err != nil {
		return
	}

	durPerB.Height = txPerB.Height

	tickets := pgb.getChartData(data, dbtypes.TicketPrice)
	diff := pgb.getChartData(data, dbtypes.POWDifficulty)
	tickets.Time, tickets.ValueF, diff.Difficulty, err = pgb.ticketsPriceByHeight(tickets.Time,
		tickets.ValueF, diff.Difficulty)
	if err != nil {
		return
	}

	diff.Time = tickets.Time

	tByAllBlocks := pgb.getChartData(data, dbtypes.TicketsByBlocks)
	tByAllBlocks.Height, tByAllBlocks.Solo, tByAllBlocks.Pooled, err = pgb.ticketsByBlocks(tByAllBlocks.Height,
		tByAllBlocks.Solo, tByAllBlocks.Pooled)
	if err != nil {
		return
	}

	ticketsByWin := pgb.getChartData(data, dbtypes.TicketByWindows)
	ticketsByWin.Height, ticketsByWin.Solo, ticketsByWin.Pooled, err = pgb.ticketsByTPWindows(ticketsByWin.Height,
		ticketsByWin.Solo, ticketsByWin.Pooled)
	if err != nil {
		return
	}

	ticketSpendT := pgb.getChartData(data, dbtypes.TicketSpendT)
	ticketSpendT.Height, ticketSpendT.Unspent, ticketSpendT.Revoked, err = pgb.ticketSpendTypePerBlock(ticketSpendT.Height,
		ticketSpendT.Unspent, ticketSpendT.Revoked)
	if err != nil {
		return
	}

	data[dbtypes.AvgBlockSize] = avgSize
	data[dbtypes.BlockChainSize] = blockSize
	data[dbtypes.ChainWork] = chainData[0]
	data[dbtypes.CoinSupply] = supply
	data[dbtypes.DurationBTW] = durPerB
	data[dbtypes.HashRate] = chainData[1]
	data[dbtypes.POWDifficulty] = diff
	data[dbtypes.TicketByWindows] = ticketsByWin
	data[dbtypes.TicketPrice] = tickets
	data[dbtypes.TicketsByBlocks] = tByAllBlocks
	data[dbtypes.TicketSpendT] = ticketSpendT
	data[dbtypes.TxPerBlock] = txPerB
	data[dbtypes.TxPerDay] = txRate

	return
}

// SetVinsMainchainByBlock first retrieves for all transactions in the specified
// block the vin_db_ids and vout_db_ids arrays, along with mainchain status,
// from the transactions table, and then sets the is_mainchain flag in the vins
// table for each row of vins in the vin_db_ids array. The returns are the
// number of vins updated, the vin row IDs array, the vouts row IDs array, and
// an error value.
func (pgb *ChainDB) SetVinsMainchainByBlock(blockHash string) (int64, []dbtypes.UInt64Array, []dbtypes.UInt64Array, error) {
	// The queries in this function should not timeout or (probably) canceled,
	// so use a background context.
	ctx := context.Background()

	// Get vins DB IDs from the transactions table, for each tx in the block.
	onlyRegularTxns := false
	vinDbIDsBlk, voutDbIDsBlk, areMainchain, err :=
		RetrieveTxnsVinsVoutsByBlock(ctx, pgb.db, blockHash, onlyRegularTxns)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("unable to retrieve vin data for block %s: %v", blockHash, err)
	}

	// Set the is_mainchain flag for each vin.
	vinsUpdated, err := pgb.setVinsMainchainForMany(vinDbIDsBlk, areMainchain)
	return vinsUpdated, vinDbIDsBlk, voutDbIDsBlk, err
}

func (pgb *ChainDB) setVinsMainchainForMany(vinDbIDsBlk []dbtypes.UInt64Array, areMainchain []bool) (int64, error) {
	var rowsUpdated int64
	// each transaction
	for it, vs := range vinDbIDsBlk {
		// each vin
		numUpd, err := pgb.setVinsMainchainOneTxn(vs, areMainchain[it])
		if err != nil {
			return rowsUpdated, err
		}
		rowsUpdated += numUpd
	}
	return rowsUpdated, nil
}

func (pgb *ChainDB) setVinsMainchainOneTxn(vinDbIDs dbtypes.UInt64Array,
	isMainchain bool) (int64, error) {
	var rowsUpdated int64

	// each vin
	for _, vinDbID := range vinDbIDs {
		result, err := pgb.db.Exec(internal.SetIsMainchainByVinID,
			vinDbID, isMainchain)
		if err != nil {
			return rowsUpdated, fmt.Errorf("db ID %d not found: %v", vinDbID, err)
		}

		c, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}

		rowsUpdated += c
	}

	return rowsUpdated, nil
}

// PkScriptByVinID retrieves the pkScript and script version for the row of the
// vouts table corresponding to the previous output of the vin specified by row
// ID of the vins table.
func (pgb *ChainDB) PkScriptByVinID(id uint64) (pkScript []byte, ver uint16, err error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	pks, ver, err := RetrievePkScriptByVinID(ctx, pgb.db, id)
	return pks, ver, pgb.replaceCancelError(err)
}

// PkScriptByVoutID retrieves the pkScript and script version for the row of the
// vouts table specified by the row ID id.
func (pgb *ChainDB) PkScriptByVoutID(id uint64) (pkScript []byte, ver uint16, err error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	pks, ver, err := RetrievePkScriptByVoutID(ctx, pgb.db, id)
	return pks, ver, pgb.replaceCancelError(err)
}

// VinsForTx returns a slice of dbtypes.VinTxProperty values for each vin
// referenced by the transaction dbTx, along with the pkScript and script
// version for the corresponding prevous outpoints.
func (pgb *ChainDB) VinsForTx(dbTx *dbtypes.Tx) ([]dbtypes.VinTxProperty, []string, []uint16, error) {
	// Retrieve the pkScript and script version for the previous outpoint of
	// each vin.
	prevPkScripts := make([]string, 0, len(dbTx.VinDbIds))
	versions := make([]uint16, 0, len(dbTx.VinDbIds))
	for _, id := range dbTx.VinDbIds {
		pkScript, ver, err := pgb.PkScriptByVinID(id)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("PkScriptByVinID: %v", err)
		}
		prevPkScripts = append(prevPkScripts, hex.EncodeToString(pkScript))
		versions = append(versions, ver)
	}

	// Retrieve the vins row data.
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	vins, err := RetrieveVinsByIDs(ctx, pgb.db, dbTx.VinDbIds)
	if err != nil {
		err = fmt.Errorf("RetrieveVinsByIDs: %v", err)
	}
	return vins, prevPkScripts, versions, pgb.replaceCancelError(err)
}

// VoutsForTx returns a slice of dbtypes.Vout values for each vout referenced by
// the transaction dbTx.
func (pgb *ChainDB) VoutsForTx(dbTx *dbtypes.Tx) ([]dbtypes.Vout, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	vouts, err := RetrieveVoutsByIDs(ctx, pgb.db, dbTx.VoutDbIds)
	return vouts, pgb.replaceCancelError(err)
}

func (pgb *ChainDB) TipToSideChain(mainRoot string) (string, int64, error) {
	tipHash := pgb.BestBlockHashStr()
	var blocksMoved, txnsUpdated, vinsUpdated, votesUpdated, ticketsUpdated, addrsUpdated int64
	for tipHash != mainRoot {
		// 1. Block. Set is_mainchain=false on the tip block, return hash of
		// previous block.
		now := time.Now()
		previousHash, err := SetMainchainByBlockHash(pgb.db, tipHash, false)
		if err != nil {
			log.Errorf("Failed to set block %s as a sidechain block: %v",
				tipHash, err)
		}
		blocksMoved++
		log.Debugf("SetMainchainByBlockHash: %v", time.Since(now))

		// 2. Transactions. Set is_mainchain=false on all transactions in the
		// tip block, returning only the number of transactions updated.
		now = time.Now()
		rowsUpdated, _, err := UpdateTransactionsMainchain(pgb.db, tipHash, false)
		if err != nil {
			log.Errorf("Failed to set transactions in block %s as sidechain: %v",
				tipHash, err)
		}
		txnsUpdated += rowsUpdated
		log.Debugf("UpdateTransactionsMainchain: %v", time.Since(now))

		// 3. Vins. Set is_mainchain=false on all vins, returning the number of
		// vins updated, the vins table row IDs, and the vouts table row IDs.
		now = time.Now()
		rowsUpdated, vinDbIDsBlk, voutDbIDsBlk, err := pgb.SetVinsMainchainByBlock(tipHash) // isMainchain from transactions table
		if err != nil {
			log.Errorf("Failed to set vins in block %s as sidechain: %v",
				tipHash, err)
		}
		vinsUpdated += rowsUpdated
		log.Debugf("SetVinsMainchainByBlock: %v", time.Since(now))

		// 4. Addresses. Set valid_mainchain=false on all addresses rows
		// corresponding to the spending transactions specified by the vins DB
		// row IDs, and the funding transactions specified by the vouts DB row
		// IDs. The IDs come for free via RetrieveTxnsVinsVoutsByBlock.
		now = time.Now()
		numAddrSpending, numAddrFunding, err := UpdateAddressesMainchainByIDs(pgb.db,
			vinDbIDsBlk, voutDbIDsBlk, false)
		if err != nil {
			log.Errorf("Failed to set addresses rows in block %s as sidechain: %v",
				tipHash, err)
		}
		addrsUpdated += numAddrSpending + numAddrFunding
		log.Debugf("UpdateAddressesMainchainByIDs: %v", time.Since(now))

		// 5. Votes. Sets is_mainchain=false on all votes in the tip block.
		now = time.Now()
		rowsUpdated, err = UpdateVotesMainchain(pgb.db, tipHash, false)
		if err != nil {
			log.Errorf("Failed to set votes in block %s as sidechain: %v",
				tipHash, err)
		}
		votesUpdated += rowsUpdated
		log.Debugf("UpdateVotesMainchain: %v", time.Since(now))

		// 6. Tickets. Sets is_mainchain=false on all tickets in the tip block.
		now = time.Now()
		rowsUpdated, err = UpdateTicketsMainchain(pgb.db, tipHash, false)
		if err != nil {
			log.Errorf("Failed to set tickets in block %s as sidechain: %v",
				tipHash, err)
		}
		ticketsUpdated += rowsUpdated
		log.Debugf("UpdateTicketsMainchain: %v", time.Since(now))

		// move on to next block
		tipHash = previousHash

		pgb.bestBlock.mtx.Lock()
		pgb.bestBlock.height, err = pgb.BlockHeight(tipHash)
		if err != nil {
			log.Errorf("Failed to retrieve block height for %s", tipHash)
		}
		pgb.bestBlock.hash = tipHash
		pgb.bestBlock.mtx.Unlock()
	}

	log.Debugf("Reorg orphaned: %d blocks, %d txns, %d vins, %d addresses, %d votes, %d tickets",
		blocksMoved, txnsUpdated, vinsUpdated, addrsUpdated, votesUpdated, ticketsUpdated)

	return tipHash, blocksMoved, nil
}

// StoreBlock processes the input wire.MsgBlock, and saves to the data tables.
// The number of vins and vouts stored are returned.
func (pgb *ChainDB) StoreBlock(msgBlock *wire.MsgBlock, winningTickets []string,
	isValid, isMainchain, updateExistingRecords, updateAddressesSpendingInfo,
	updateTicketsSpendingInfo bool, chainWork string) (
	numVins int64, numVouts int64, numAddresses int64, err error) {
	// Convert the wire.MsgBlock to a dbtypes.Block
	dbBlock := dbtypes.MsgBlockToDBBlock(msgBlock, pgb.chainParams, chainWork)

	// Get the previous winners (stake DB pool info cache has this info). If the
	// previous block is side chain, stakedb will not have the
	// winners/validators. Since Validators are only used to identify misses in
	// InsertVotes, we will leave the Validators empty and assume there are no
	// misses. If this block becomes main chain at some point via a
	// reorganization, its table entries will be updated appropriately, which
	// will include inserting any misses since the stakeDB will then include the
	// block, thus allowing the winning tickets to be known at that time.
	// TODO: Somehow verify reorg operates as described when switching manually
	// imported side chain blocks over to main chain.
	prevBlockHash := msgBlock.Header.PrevBlock
	var winners []string
	if isMainchain && !bytes.Equal(zeroHash[:], prevBlockHash[:]) {
		tpi, found := pgb.stakeDB.PoolInfo(prevBlockHash)
		if !found {
			err = fmt.Errorf("stakedb.PoolInfo failed for block %s", msgBlock.BlockHash())
			return
		}
		winners = tpi.Winners
	}

	// Wrap the message block with newly winning tickets and the tickets
	// expected to vote in this block (on the previous block).
	MsgBlockPG := &MsgBlockPG{
		MsgBlock:       msgBlock,
		WinningTickets: winningTickets,
		Validators:     winners,
	}

	// Extract transactions and their vouts, and insert vouts into their pg table,
	// returning their DB PKs, which are stored in the corresponding transaction
	// data struct. Insert each transaction once they are updated with their
	// vouts' IDs, returning the transaction PK ID, which are stored in the
	// containing block data struct.

	// regular transactions
	resChanReg := make(chan storeTxnsResult)
	go func() {
		resChanReg <- pgb.storeBlockTxnTree(MsgBlockPG, wire.TxTreeRegular,
			pgb.chainParams, isValid, isMainchain, updateExistingRecords,
			updateAddressesSpendingInfo, updateTicketsSpendingInfo)
	}()

	// stake transactions
	resChanStake := make(chan storeTxnsResult)
	go func() {
		resChanStake <- pgb.storeBlockTxnTree(MsgBlockPG, wire.TxTreeStake,
			pgb.chainParams, isValid, isMainchain, updateExistingRecords,
			updateAddressesSpendingInfo, updateTicketsSpendingInfo)
	}()

	if dbBlock.Height%5000 == 0 {
		log.Debugf("UTXO cache size: %d", pgb.utxoCache.Size())
	}

	errReg := <-resChanReg
	errStk := <-resChanStake
	if errStk.err != nil {
		if errReg.err == nil {
			err = errStk.err
			numVins = errReg.numVins
			numVouts = errReg.numVouts
			numAddresses = errReg.numAddresses
			return
		}
		err = errors.New(errReg.Error() + ", " + errStk.Error())
		return
	} else if errReg.err != nil {
		err = errReg.err
		numVins = errStk.numVins
		numVouts = errStk.numVouts
		numAddresses = errStk.numAddresses
		return
	}

	numVins = errStk.numVins + errReg.numVins
	numVouts = errStk.numVouts + errReg.numVouts
	numAddresses = errStk.numAddresses + errReg.numAddresses
	dbBlock.TxDbIDs = errReg.txDbIDs
	dbBlock.STxDbIDs = errStk.txDbIDs

	// Merge the affected addresses, which are to be purged from the cache.
	affectedAddresses := errReg.addresses
	for ad := range errStk.addresses {
		affectedAddresses[ad] = struct{}{}
	}
	// Put them in a slice.
	addresses := make([]string, 0, len(affectedAddresses))
	for ad := range affectedAddresses {
		addresses = append(addresses, ad)
	}

	// Store the block now that it has all if its transaction row IDs.
	var blockDbID uint64
	blockDbID, err = InsertBlock(pgb.db, dbBlock, isValid, isMainchain, pgb.dupChecks)
	if err != nil {
		log.Error("InsertBlock:", err)
		return
	}
	pgb.lastBlock[msgBlock.BlockHash()] = blockDbID

	if isMainchain {
		// Update best block height and hash.
		pgb.bestBlock.mtx.Lock()
		pgb.bestBlock.height = int64(dbBlock.Height)
		pgb.bestBlock.hash = dbBlock.Hash
		pgb.bestBlock.mtx.Unlock()
	}

	// Insert the block in the block_chain table with the previous block hash
	// and an empty string for the next block hash, which may be updated when a
	// new block extends this chain.
	err = InsertBlockPrevNext(pgb.db, blockDbID, dbBlock.Hash,
		dbBlock.PreviousHash, "")
	if err != nil && err != sql.ErrNoRows {
		log.Error("InsertBlockPrevNext:", err)
		return
	}

	// Update the previous block's next block hash in the block_chain table with
	// this block's hash as it is next. If the current block's votes
	// invalidated/disapproved the previous block, also update the is_valid
	// columns for the previous block's entries in the following tables: blocks,
	// vins, addresses, and transactions.
	err = pgb.UpdateLastBlock(msgBlock, isMainchain)
	if err != nil && err != sql.ErrNoRows {
		err = fmt.Errorf("InsertBlockPrevNext: %v", err)
		return
	}

	// If not in batch sync, lazy update the dev fund balance, and expire cache
	// data for the affected addresses.
	if !pgb.InBatchSync {
		if err = pgb.FreshenAddressCaches(true, addresses); err != nil {
			log.Warnf("FreshenAddressCaches: %v", err)
		}
	}

	return
}

// UpdateLastBlock set the previous block's next block hash in the block_chain
// table with this block's hash as it is next. If the current block's votes
// invalidated/disapproved the previous block, it also updates the is_valid
// columns for the previous block's entries in the following tables: blocks,
// vins, addresses, and transactions. If the previous block is not on the same
// chain as this block (as indicated by isMainchain), no updates are performed.
func (pgb *ChainDB) UpdateLastBlock(msgBlock *wire.MsgBlock, isMainchain bool) error {
	// Only update if last was not genesis, which is not in the table (implied).
	lastBlockHash := msgBlock.Header.PrevBlock
	if lastBlockHash == zeroHash {
		return nil
	}

	// Ensure previous block has the same main/sidechain status. If the
	// current block being added is side chain, do not invalidate the
	// mainchain block or any of its components, or update the block_chain
	// table to point to this block.
	if !isMainchain { // only check when current block is side chain
		_, lastIsMainchain, err := pgb.BlockFlagsNoCancel(lastBlockHash.String())
		if err != nil {
			log.Errorf("Unable to determine status of previous block %v: %v",
				lastBlockHash, err)
			return nil // do not return an error, but this should not happen
		}
		// Do not update previous block data if it is not the same blockchain
		// branch. i.e. A side chain block does not invalidate a main chain block.
		if lastIsMainchain != isMainchain {
			log.Debugf("Previous block %v is on the main chain, while current "+
				"block %v is on a side chain. Not updating main chain parent.",
				lastBlockHash, msgBlock.BlockHash())
			return nil
		}
	}

	// Attempt to find the row id of the block hash in cache.
	lastBlockDbID, ok := pgb.lastBlock[lastBlockHash]
	if !ok {
		log.Debugf("The previous block %s for block %s not found in cache, "+
			"looking it up.", lastBlockHash, msgBlock.BlockHash())
		var err error
		lastBlockDbID, err = pgb.BlockChainDbIDNoCancel(lastBlockHash.String())
		if err != nil {
			return fmt.Errorf("unable to locate block %s in block_chain table: %v",
				lastBlockHash, err)
		}
	}

	// Update the previous block's next block hash in the block_chain table.
	err := UpdateBlockNext(pgb.db, lastBlockDbID, msgBlock.BlockHash().String())
	if err != nil {
		return fmt.Errorf("UpdateBlockNext: %v", err)
	}

	// If the previous block is invalidated by this one: (1) update it's
	// is_valid flag in the blocks table if needed, and (2) flag all the vins,
	// transactions, and addresses table rows from the previous block's
	// transactions as invalid. Do nothing otherwise since blocks' transactions
	// are initially added as valid.
	lastIsValid := msgBlock.Header.VoteBits&1 != 0
	if !lastIsValid {
		// Update the is_valid flag in the blocks table.
		log.Infof("Setting last block %s as INVALID", lastBlockHash)
		err := UpdateLastBlockValid(pgb.db, lastBlockDbID, lastIsValid)
		if err != nil {
			return fmt.Errorf("UpdateLastBlockValid: %v", err)
		}

		// Update the is_valid flag for the last block's vins.
		err = UpdateLastVins(pgb.db, lastBlockHash.String(), lastIsValid, isMainchain)
		if err != nil {
			return fmt.Errorf("UpdateLastVins: %v", err)
		}

		// Update the is_valid flag for the last block's regular transactions.
		_, _, err = UpdateTransactionsValid(pgb.db, lastBlockHash.String(), lastIsValid)
		if err != nil {
			return fmt.Errorf("UpdateTransactionsValid: %v", err)
		}

		// Update addresses table for last block's regular transactions.
		err = UpdateLastAddressesValid(pgb.db, lastBlockHash.String(), lastIsValid)
		if err != nil {
			return fmt.Errorf("UpdateLastAddressesValid: %v", err)
		}

		// NOTE: Updating the tickets, votes, and misses tables is not
		// necessary since the stake tree is not subject to stakeholder
		// approval.
	}

	return nil
}

// storeTxnsResult is the type of object sent back from the goroutines wrapping
// storeBlockTxnTree in StoreBlock.
type storeTxnsResult struct {
	numVins, numVouts, numAddresses int64
	txDbIDs                         []uint64
	err                             error
	addresses                       map[string]struct{}
}

func (r *storeTxnsResult) Error() string {
	return r.err.Error()
}

// MsgBlockPG extends wire.MsgBlock with the winning tickets from the block,
// WinningTickets, and the tickets from the previous block that may vote on this
// block's validity, Validators.
type MsgBlockPG struct {
	*wire.MsgBlock
	WinningTickets []string
	Validators     []string
}

// storeTxns inserts all vins, vouts, and transactions.  The VoutDbIds and
// VinDbIds fields of each Tx in the input txns slice are set upon insertion of
// vouts and vins, respectively. The Vouts fields are also set to the
// corresponding Vout slice from the vouts input argument. For each transaction,
// a []AddressRow is created while inserting the vouts. The [][]AddressRow is
// returned. The row IDs of the inserted transactions in the transactions table
// is returned in txDbIDs []uint64.
func (pgb *ChainDB) storeTxns(txns []*dbtypes.Tx, vouts [][]*dbtypes.Vout, vins []dbtypes.VinTxPropertyARRAY,
	updateExistingRecords bool) (dbAddressRows [][]dbtypes.AddressRow, txDbIDs []uint64, totalAddressRows, numOuts, numIns int, err error) {
	// vins, vouts, and transactions inserts in atomic DB transaction
	var dbTx *sql.Tx
	dbTx, err = pgb.db.Begin()
	if err != nil {
		_ = dbTx.Rollback()
		err = fmt.Errorf("failed to begin database transaction: %v", err)
		return
	}

	checked, doUpsert := pgb.dupChecks, updateExistingRecords

	var voutStmt *sql.Stmt
	voutStmt, err = dbTx.Prepare(internal.MakeVoutInsertStatement(checked, doUpsert))
	if err != nil {
		_ = dbTx.Rollback()
		err = fmt.Errorf("failed to prepare vout insert statment: %v", err)
		return
	}
	defer voutStmt.Close()

	var vinStmt *sql.Stmt
	vinStmt, err = dbTx.Prepare(internal.MakeVinInsertStatement(checked, doUpsert))
	if err != nil {
		_ = dbTx.Rollback()
		err = fmt.Errorf("failed to prepare vin insert statment: %v", err)
		return
	}
	defer vinStmt.Close()

	// dbAddressRows contains the data added to the address table, arranged as
	// [tx_i][addr_j], transactions paying to different numbers of addresses.
	dbAddressRows = make([][]dbtypes.AddressRow, len(txns))

	for it, Tx := range txns {
		// Insert vouts, and collect AddressRows to add to address table for
		// each output.
		Tx.VoutDbIds, dbAddressRows[it], err = InsertVoutsStmt(voutStmt,
			vouts[it], pgb.dupChecks, updateExistingRecords)
		if err != nil && err != sql.ErrNoRows {
			err = fmt.Errorf("failure in InsertVoutsStmt: %v", err)
			_ = dbTx.Rollback()
			return
		}
		totalAddressRows += len(dbAddressRows[it])
		numOuts += len(Tx.VoutDbIds)
		if err == sql.ErrNoRows || len(vouts[it]) != len(Tx.VoutDbIds) {
			log.Warnf("Incomplete Vout insert.")
		}

		// Insert vins
		Tx.VinDbIds, err = InsertVinsStmt(vinStmt, vins[it], pgb.dupChecks,
			updateExistingRecords)
		if err != nil && err != sql.ErrNoRows {
			err = fmt.Errorf("failure in InsertVinsStmt: %v", err)
			_ = dbTx.Rollback()
			return
		}
		numIns += len(Tx.VinDbIds)

		// return the transactions vout slice if processing stake tree
		//if txTree == wire.TxTreeStake {
		Tx.Vouts = vouts[it]
		//}
	}

	// Get the tx PK IDs for storage in the blocks, tickets, and votes table.
	txDbIDs, err = InsertTxnsDbTxn(dbTx, txns, pgb.dupChecks, updateExistingRecords)
	if err != nil && err != sql.ErrNoRows {
		err = fmt.Errorf("failure in InsertTxnsDbTxn: %v", err)
		return
	}

	if err = dbTx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %v", err)
	}
	return
}

// storeBlockTxnTree stores the transactions of a given block.
func (pgb *ChainDB) storeBlockTxnTree(msgBlock *MsgBlockPG, txTree int8,
	chainParams *chaincfg.Params, isValid, isMainchain bool,
	updateExistingRecords, updateAddressesSpendingInfo,
	updateTicketsSpendingInfo bool) storeTxnsResult {
	// For the given block, transaction tree, and network, extract the
	// transactions, vins, and vouts.
	dbTransactions, dbTxVouts, dbTxVins := dbtypes.ExtractBlockTransactions(
		msgBlock.MsgBlock, txTree, chainParams, isValid, isMainchain)

	// Store the transactions, vins, and vouts. This sets the VoutDbIds,
	// VinDbIds, and Vouts fields of each Tx in the dbTransactions slice.
	dbAddressRows, txDbIDs, totalAddressRows, numOuts, numIns, err :=
		pgb.storeTxns(dbTransactions, dbTxVouts, dbTxVins, updateExistingRecords)
	if err != nil {
		return storeTxnsResult{err: err}
	}

	// The return value, containing counts of inserted vins/vouts/txns, and an
	// error value.
	txRes := storeTxnsResult{
		numVins:  int64(numIns),
		numVouts: int64(numOuts),
		txDbIDs:  txDbIDs,
	}

	// For a side chain block, set Validators to an empty slice so that there
	// will be no misses even if there are less than 5 votes. Any Validators
	// that do not match a spent ticket hash in InsertVotes are considered
	// misses. By listing no required validators, there are no misses. For side
	// chain blocks, this is acceptable and necessary because the misses table
	// does not record the block hash or main/side chain status.
	if !isMainchain {
		msgBlock.Validators = []string{}
	}

	// If processing stake tree transactions, insert tickets, votes, and misses.
	// Also update pool status and spending information in tickets table
	// pertaining to the new votes, revokes, misses, and expires.
	if txTree == wire.TxTreeStake {
		// Tickets: Insert new (unspent) tickets
		newTicketDbIDs, newTicketTx, err := InsertTickets(pgb.db, dbTransactions, txDbIDs,
			pgb.dupChecks, updateExistingRecords)
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertTickets:", err)
			txRes.err = err
			return txRes
		}

		// Cache the unspent ticket DB row IDs and and their hashes. Needed do
		// efficiently update their spend status later.
		var unspentTicketCache *TicketTxnIDGetter
		if updateTicketsSpendingInfo {
			for it, tdbid := range newTicketDbIDs {
				pgb.unspentTicketCache.Set(newTicketTx[it].TxID, tdbid)
			}
			unspentTicketCache = pgb.unspentTicketCache
		}

		// Votes: insert votes and misses (tickets that did not vote when
		// called). Return the ticket hash of all misses, which may include
		// revokes at this point. Unrevoked misses are identified when updating
		// ticket spend info below.

		// voteDbIDs, voteTxns, spentTicketHashes, ticketDbIDs, missDbIDs, err := ...
		var missesHashIDs map[string]uint64
		_, _, _, _, missesHashIDs, err = InsertVotes(pgb.db, dbTransactions, txDbIDs,
			unspentTicketCache, msgBlock, pgb.dupChecks, updateExistingRecords,
			pgb.chainParams, pgb.ChainInfo())
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertVotes:", err)
			txRes.err = err
			return txRes
		}

		if updateTicketsSpendingInfo {
			// Get information for transactions spending tickets (votes and
			// revokes), and the ticket DB row IDs themselves. Also return
			// tickets table row IDs for newly spent tickets, if we are updating
			// them as we go (SetSpendingForTickets). CollectTicketSpendDBInfo
			// uses ChainDB's ticket DB row ID cache (unspentTicketCache), and
			// immediately expires any found entries for a main chain block.
			spendingTxDbIDs, spendTypes, spentTicketHashes, ticketDbIDs, err :=
				pgb.CollectTicketSpendDBInfo(dbTransactions, txDbIDs,
					msgBlock.MsgBlock, isMainchain)
			if err != nil {
				log.Error("CollectTicketSpendDBInfo:", err)
				txRes.err = err
				return txRes
			}

			// Get a consistent view of the stake node at its present height.
			pgb.stakeDB.LockStakeNode()

			// Classify and record the height of each ticket spend (vote or
			// revoke). For revokes, further distinguish miss or expire.
			revokes := make(map[string]uint64)
			blockHeights := make([]int64, len(spentTicketHashes))
			poolStatuses := make([]dbtypes.TicketPoolStatus, len(spentTicketHashes))
			for iv := range spentTicketHashes {
				blockHeights[iv] = int64(msgBlock.Header.Height) /* voteDbTxns[iv].BlockHeight */

				// Vote or revoke
				switch spendTypes[iv] {
				case dbtypes.TicketVoted:
					poolStatuses[iv] = dbtypes.PoolStatusVoted
				case dbtypes.TicketRevoked:
					revokes[spentTicketHashes[iv]] = ticketDbIDs[iv]
					// Revoke reason
					h, err0 := chainhash.NewHashFromStr(spentTicketHashes[iv])
					if err0 != nil {
						log.Errorf("Invalid hash %v", spentTicketHashes[iv])
						continue // no info about spent ticket!
					}
					expired := pgb.stakeDB.BestNode.ExistsExpiredTicket(*h)
					if !expired {
						poolStatuses[iv] = dbtypes.PoolStatusMissed
					} else {
						poolStatuses[iv] = dbtypes.PoolStatusExpired
					}
				}
			}

			// Update tickets table with spending info.
			_, err = SetSpendingForTickets(pgb.db, ticketDbIDs, spendingTxDbIDs,
				blockHeights, spendTypes, poolStatuses)
			if err != nil {
				log.Error("SetSpendingForTickets:", err)
			}

			// Unspent not-live tickets are also either expired or missed.

			// Missed but not revoked
			var unspentMissedTicketHashes []string
			var missStatuses []dbtypes.TicketPoolStatus
			unspentMisses := make(map[string]struct{})
			// missesHashIDs refers to lottery winners that did not vote.
			for miss := range missesHashIDs {
				if _, ok := revokes[miss]; !ok {
					// unrevoked miss
					unspentMissedTicketHashes = append(unspentMissedTicketHashes, miss)
					unspentMisses[miss] = struct{}{}
					missStatuses = append(missStatuses, dbtypes.PoolStatusMissed)
				}
			}

			// Expired but not revoked
			unspentEnM := make([]string, len(unspentMissedTicketHashes))
			// Start with the unspent misses and append unspent expires to get
			// "unspent expired and missed".
			copy(unspentEnM, unspentMissedTicketHashes)
			unspentExpiresAndMisses := pgb.stakeDB.BestNode.MissedByBlock()
			for _, missHash := range unspentExpiresAndMisses {
				// MissedByBlock includes tickets that missed votes or expired
				// (and which may be revoked in this block); we just want the
				// expires, and not the revoked ones. Screen each ticket from
				// MissedByBlock for the actual unspent expires.
				if pgb.stakeDB.BestNode.ExistsExpiredTicket(missHash) {
					emHash := missHash.String()
					// Next check should not be unnecessary. Make sure not in
					// unspent misses from above, and not just revoked.
					_, justMissed := unspentMisses[emHash] // should be redundant
					_, justRevoked := revokes[emHash]      // exclude if revoked
					if !justMissed && !justRevoked {
						unspentEnM = append(unspentEnM, emHash)
						missStatuses = append(missStatuses, dbtypes.PoolStatusExpired)
					}
				}
			}

			// Release the stake node.
			pgb.stakeDB.UnlockStakeNode()

			// Locate the row IDs of the unspent expired and missed tickets. Do
			// not expire the cache entry.
			unspentEnMRowIDs := make([]uint64, len(unspentEnM))
			for iu := range unspentEnM {
				t, err0 := unspentTicketCache.TxnDbID(unspentEnM[iu], false)
				if err0 != nil {
					txRes.err = fmt.Errorf("failed to retrieve ticket %s DB ID: %v",
						unspentEnM[iu], err0)
					return txRes
				}
				unspentEnMRowIDs[iu] = t
			}

			// Update status of the unspent expired and missed tickets.
			numUnrevokedMisses, err := SetPoolStatusForTickets(pgb.db,
				unspentEnMRowIDs, missStatuses)
			if err != nil {
				log.Errorf("SetPoolStatusForTicketsByHash: %v", err)
			} else if numUnrevokedMisses > 0 {
				log.Tracef("Noted %d unrevoked newly-missed tickets.", numUnrevokedMisses)
			}
		} // updateTicketsSpendingInfo
	} // txTree == wire.TxTreeStake

	// Store txn block time and mainchain validity status in AddressRows, and
	// set IsFunding to true since InsertVouts is supplying the AddressRows.
	dbAddressRowsFlat := make([]*dbtypes.AddressRow, 0, totalAddressRows)
	for it, tx := range dbTransactions {
		// A UTXO may have multiple addresses associated with it, so check each
		// addresses table row for multiple entries for the same output of this
		// txn. This can only happen if the output's pkScript is a P2PK or P2PKH
		// multisignature script. This does not refer to P2SH, where a single
		// address corresponds to the redeem script hash even though the script
		// may be a multi-signature script.
		utxos := make([]*dbtypes.UTXO, tx.NumVout)
		for iv := range dbAddressRows[it] {
			// Transaction that pays to the address
			dba := &dbAddressRows[it][iv]

			// Do not store zero-value output data.
			if dba.Value == 0 {
				continue
			}

			// Set addresses row fields not set by InsertVouts: TxBlockTime,
			// IsFunding, ValidMainChain, and MatchingTxHash. Only
			// MatchingTxHash goes unset initially, later set by
			// insertAddrSpendingTxUpdateMatchedFunding (called by
			// SetSpendingForFundingOP below, and other places).
			dba.TxBlockTime = tx.BlockTime
			dba.IsFunding = true // from vouts
			dba.ValidMainChain = isMainchain && isValid

			// Funding tx hash, vout id, value, and address are already assigned
			// by InsertVouts. Only the block time and is_funding was needed.
			dbAddressRowsFlat = append(dbAddressRowsFlat, dba)

			// See if this output has already been assigned to a UTXO.
			utxo := utxos[dba.TxVinVoutIndex]
			if utxo == nil {
				// New UTXO. Initialize with this address.
				utxos[dba.TxVinVoutIndex] = &dbtypes.UTXO{
					TxHash:  tx.TxID,
					TxIndex: dba.TxVinVoutIndex,
					UTXOData: dbtypes.UTXOData{
						Addresses: []string{dba.Address},
						Value:     int64(dba.Value),
					},
				}
				continue
			}

			// Existing UTXO. Append address for this likely-bare multisig output.
			utxo.UTXOData.Addresses = append(utxo.UTXOData.Addresses, dba.Address)
			log.Infof(" ============== BARE MULTISIG: %s:%d ============== ",
				dba.TxHash, dba.TxVinVoutIndex)
		}

		// Store each output of this transaction in the UTXO cache.
		for _, utxo := range utxos {
			// Skip any that were not set (e.g. zero-value, no address, etc.).
			if utxo == nil {
				continue
			}
			pgb.utxoCache.Set(utxo.TxHash, utxo.TxIndex, utxo.Addresses, utxo.Value)
		}
	}

	// Insert each new AddressRow, absent MatchingTxHash (spending txn since
	// these new address rows are *funding*).
	_, err = InsertAddressRows(pgb.db, dbAddressRowsFlat, pgb.dupChecks, updateExistingRecords)
	if err != nil {
		log.Error("InsertAddressRows:", err)
		txRes.err = err
		return txRes
	}
	txRes.numAddresses = int64(totalAddressRows)
	txRes.addresses = make(map[string]struct{})
	for _, ad := range dbAddressRowsFlat {
		txRes.addresses[ad.Address] = struct{}{}
	}

	// Check the new vins, inserting spending address rows, and (if
	// updateAddressesSpendingInfo) update matching_tx_hash in corresponding
	// funding rows.
	dbTx, err := pgb.db.Begin()
	if err != nil {
		txRes.err = fmt.Errorf(`unable to begin database transaction: %v`, err)
		return txRes
	}

	for it, tx := range dbTransactions {
		// vins array for this transaction
		txVins := dbTxVins[it]
		for iv := range txVins {
			// Transaction that spends an outpoint paying to >=0 addresses
			vin := &txVins[iv]

			// Skip coinbase inputs (they are generated and thus have no
			// previous outpoint funding them).
			if bytes.Equal(zeroHashStringBytes, []byte(vin.PrevTxHash)) {
				continue
			}

			// Insert spending txn data in addresses table, and updated spend
			// status for the previous outpoints' rows in the same table.
			vinDbID := tx.VinDbIds[iv]
			spendingTxHash := vin.TxID
			spendingTxIndex := vin.TxIndex
			validMainchain := tx.IsValidBlock && tx.IsMainchainBlock
			// Attempt to retrieve cached data for this now-spent TXO. A
			// successful get will delete the entry from the cache.
			utxoData, ok := pgb.utxoCache.Get(vin.PrevTxHash, vin.PrevTxIndex)
			if !ok {
				log.Tracef("Data for that utxo (%s:%d) wasn't cached!", vin.PrevTxHash, vin.PrevTxIndex)
			}
			numAddressRowsSet, err := insertSpendingAddressRow(dbTx,
				vin.PrevTxHash, vin.PrevTxIndex, int8(vin.PrevTxTree),
				spendingTxHash, spendingTxIndex, vinDbID, utxoData, pgb.dupChecks,
				updateExistingRecords, validMainchain, vin.TxType, updateAddressesSpendingInfo,
				tx.BlockTime)
			if err != nil {
				txRes.err = fmt.Errorf(`insertSpendingAddressRow: %v + %v (rollback)`,
					err, dbTx.Rollback())
				return txRes
			}
			txRes.numAddresses += numAddressRowsSet
		}
	}

	txRes.err = dbTx.Commit()

	return txRes
}

// CollectTicketSpendDBInfo processes the stake transactions in msgBlock, which
// correspond to the transaction data in dbTxns, and extracts data for votes and
// revokes, including the spent ticket hash and DB row ID.
func (pgb *ChainDB) CollectTicketSpendDBInfo(dbTxns []*dbtypes.Tx, txDbIDs []uint64,
	msgBlock *wire.MsgBlock, isMainchain bool) (spendingTxDbIDs []uint64, spendTypes []dbtypes.TicketSpendType,
	ticketHashes []string, ticketDbIDs []uint64, err error) {
	// This only makes sense for stake transactions. Check that the number of
	// dbTxns equals the number of STransactions in msgBlock.
	msgTxns := msgBlock.STransactions
	if len(msgTxns) != len(dbTxns) {
		err = fmt.Errorf("number of stake transactions (%d) not as expected (%d)",
			len(msgTxns), len(dbTxns))
		return
	}

	for i, tx := range dbTxns {
		// Ensure the transaction slices correspond.
		msgTx := msgTxns[i]
		if tx.TxID != msgTx.TxHash().String() {
			err = fmt.Errorf("txid of dbtypes.Tx does not match that of msgTx")
			return
		}

		// Filter for votes and revokes only.
		var stakeSubmissionVinInd int
		var spendType dbtypes.TicketSpendType
		switch tx.TxType {
		case int16(stake.TxTypeSSGen):
			spendType = dbtypes.TicketVoted
			stakeSubmissionVinInd = 1
		case int16(stake.TxTypeSSRtx):
			spendType = dbtypes.TicketRevoked
		default:
			continue
		}

		if stakeSubmissionVinInd >= len(msgTx.TxIn) {
			log.Warnf("Invalid vote or ticket with %d inputs", len(msgTx.TxIn))
			continue
		}

		spendTypes = append(spendTypes, spendType)

		// vote/revoke row ID in *transactions* table
		spendingTxDbIDs = append(spendingTxDbIDs, txDbIDs[i])

		// ticket hash
		ticketHash := msgTx.TxIn[stakeSubmissionVinInd].PreviousOutPoint.Hash.String()
		ticketHashes = append(ticketHashes, ticketHash)

		// ticket's row ID in *tickets* table
		expireEntries := isMainchain // expire all cache entries for main chain blocks
		t, err0 := pgb.unspentTicketCache.TxnDbID(ticketHash, expireEntries)
		if err0 != nil {
			err = fmt.Errorf("failed to retrieve ticket %s DB ID: %v", ticketHash, err0)
			return
		}
		ticketDbIDs = append(ticketDbIDs, t)
	}
	return
}

// UpdateSpendingInfoInAllAddresses completely rebuilds the matching transaction
// columns for funding rows of the addresses table. This is intended to be use
// after syncing all other tables and creating their indexes, particularly the
// indexes on the vins table, and the addresses table index on the funding tx
// columns. This can be used instead of using updateAddressesSpendingInfo=true
// with storeBlockTxnTree, which will update these addresses table columns too, but much
// more slowly for a number of reasons (that are well worth investigating BTW!).
func (pgb *ChainDB) UpdateSpendingInfoInAllAddresses(barLoad chan *dbtypes.ProgressBarLoad) (int64, error) {
	// Get the full list of vinDbIDs
	allVinDbIDs, err := RetrieveAllVinDbIDs(pgb.db)
	if err != nil {
		log.Errorf("RetrieveAllVinDbIDs: %v", err)
		return 0, err
	}

	updatesPerDBTx := 1000
	totalVinIbIDs := len(allVinDbIDs)

	timeStart := time.Now()

	log.Infof("Updating spending tx info for %d addresses...", totalVinIbIDs)
	var numAddresses int64
	for i := 0; i < totalVinIbIDs; i += updatesPerDBTx {
		if i%100000 == 0 {
			endRange := i + 100000 - 1
			if endRange > totalVinIbIDs {
				endRange = totalVinIbIDs
			}
			log.Infof("Updating from vins %d to %d...", i, endRange)
		}

		var numAddressRowsSet int64
		endChunk := i + updatesPerDBTx
		if endChunk > totalVinIbIDs {
			endChunk = totalVinIbIDs
		}

		if barLoad != nil {
			// Full mode is definitely running so no need to check.
			timeTakenPerBlock := (time.Since(timeStart).Seconds() / float64(endChunk-i))
			barLoad <- &dbtypes.ProgressBarLoad{
				From:      int64(i),
				To:        int64(totalVinIbIDs),
				Msg:       addressesSyncStatusMsg,
				BarID:     dbtypes.AddressesTableSync,
				Timestamp: int64(timeTakenPerBlock * float64(totalVinIbIDs-endChunk)),
			}

			timeStart = time.Now()
		}

		_, numAddressRowsSet, err = SetSpendingForVinDbIDs(pgb.db, allVinDbIDs[i:endChunk])
		if err != nil {
			log.Errorf("SetSpendingForVinDbIDs: %v", err)
			continue
		}
		numAddresses += numAddressRowsSet
	}

	// Signal the completion of the sync to the status page.
	if barLoad != nil {
		barLoad <- &dbtypes.ProgressBarLoad{
			From:  int64(totalVinIbIDs),
			To:    int64(totalVinIbIDs),
			Msg:   addressesSyncStatusMsg,
			BarID: dbtypes.AddressesTableSync,
		}
	}

	return numAddresses, err
}

// UpdateSpendingInfoInAllTickets reviews all votes and revokes and sets this
// spending info in the tickets table.
func (pgb *ChainDB) UpdateSpendingInfoInAllTickets() (int64, error) {
	// The queries in this function should not timeout or (probably) canceled,
	// so use a background context.
	ctx := context.Background()

	// Get the full list of votes (DB IDs and heights), and spent ticket hashes
	allVotesDbIDs, allVotesHeights, ticketDbIDs, err :=
		RetrieveAllVotesDbIDsHeightsTicketDbIDs(ctx, pgb.db)
	if err != nil {
		log.Errorf("RetrieveAllVotesDbIDsHeightsTicketDbIDs: %v", err)
		return 0, err
	}

	// To update spending info in tickets table, get the spent tickets' DB
	// row IDs and block heights.
	spendTypes := make([]dbtypes.TicketSpendType, len(ticketDbIDs))
	for iv := range ticketDbIDs {
		spendTypes[iv] = dbtypes.TicketVoted
	}
	poolStatuses := ticketpoolStatusSlice(dbtypes.PoolStatusVoted, len(ticketDbIDs))

	// Update tickets table with spending info from new votes
	var totalTicketsUpdated int64
	totalTicketsUpdated, err = SetSpendingForTickets(pgb.db, ticketDbIDs,
		allVotesDbIDs, allVotesHeights, spendTypes, poolStatuses)
	if err != nil {
		log.Warn("SetSpendingForTickets:", err)
	}

	// Revokes

	revokeIDs, _, revokeHeights, vinDbIDs, err := RetrieveAllRevokes(ctx, pgb.db)
	if err != nil {
		log.Errorf("RetrieveAllRevokes: %v", err)
		return 0, err
	}

	revokedTicketHashes := make([]string, len(vinDbIDs))
	for i, vinDbID := range vinDbIDs {
		revokedTicketHashes[i], err = RetrieveFundingTxByVinDbID(ctx, pgb.db, vinDbID)
		if err != nil {
			log.Errorf("RetrieveFundingTxByVinDbID: %v", err)
			return 0, err
		}
	}

	revokedTicketDbIDs, err := RetrieveTicketIDsByHashes(ctx, pgb.db, revokedTicketHashes)
	if err != nil {
		log.Errorf("RetrieveTicketIDsByHashes: %v", err)
		return 0, err
	}

	poolStatuses = ticketpoolStatusSlice(dbtypes.PoolStatusMissed, len(revokedTicketHashes))
	pgb.stakeDB.LockStakeNode()
	for ih := range revokedTicketHashes {
		rh, _ := chainhash.NewHashFromStr(revokedTicketHashes[ih])
		if pgb.stakeDB.BestNode.ExistsExpiredTicket(*rh) {
			poolStatuses[ih] = dbtypes.PoolStatusExpired
		}
	}
	pgb.stakeDB.UnlockStakeNode()

	// To update spending info in tickets table, get the spent tickets' DB
	// row IDs and block heights.
	spendTypes = make([]dbtypes.TicketSpendType, len(revokedTicketDbIDs))
	for iv := range revokedTicketDbIDs {
		spendTypes[iv] = dbtypes.TicketRevoked
	}

	// Update tickets table with spending info from new votes
	var revokedTicketsUpdated int64
	revokedTicketsUpdated, err = SetSpendingForTickets(pgb.db, revokedTicketDbIDs,
		revokeIDs, revokeHeights, spendTypes, poolStatuses)
	if err != nil {
		log.Warn("SetSpendingForTickets:", err)
	}

	return totalTicketsUpdated + revokedTicketsUpdated, err
}

func ticketpoolStatusSlice(ss dbtypes.TicketPoolStatus, N int) []dbtypes.TicketPoolStatus {
	S := make([]dbtypes.TicketPoolStatus, N)
	for ip := range S {
		S[ip] = ss
	}
	return S
}

// GetChainWork fetches the dcrjson.BlockHeaderVerbose and returns only the
// ChainWork attribute as a hex-encoded string, without 0x prefix.
func (pgb *ChainDBRPC) GetChainWork(hash *chainhash.Hash) (string, error) {
	return rpcutils.GetChainWork(pgb.Client, hash)
}
