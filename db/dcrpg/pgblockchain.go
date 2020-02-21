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
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chappjc/trylock"
	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/rpcclient/v5"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/api/types/v5"
	"github.com/decred/dcrdata/blockdata/v5"
	"github.com/decred/dcrdata/db/cache/v3"
	"github.com/decred/dcrdata/db/dbtypes/v2"
	"github.com/decred/dcrdata/db/dcrpg/v5/internal"
	exptypes "github.com/decred/dcrdata/explorer/types/v2"
	"github.com/decred/dcrdata/mempool/v5"
	"github.com/decred/dcrdata/rpcutils/v3"
	"github.com/decred/dcrdata/stakedb/v3"
	"github.com/decred/dcrdata/txhelpers/v4"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	pitypes "github.com/dmigwi/go-piparser/proposals/types"
	humanize "github.com/dustin/go-humanize"
	"github.com/lib/pq"
)

var (
	zeroHash            = chainhash.Hash{}
	zeroHashStringBytes = []byte(chainhash.Hash{}.String())
)

const dcrToAtoms = 1e8

type retryError struct{}

// Error implements Stringer for retryError.
func (s retryError) Error() string {
	return "retry"
}

// IsRetryError checks if an error is a retryError type.
func IsRetryError(err error) bool {
	_, isRetryErr := err.(retryError)
	return isRetryErr
}

// storedAgendas holds the current state of agenda data already in the db.
// This helps track changes in the lockedIn and activated heights when they
// happen without making too many db accesses everytime we are updating the
// agenda_votes table.
var storedAgendas map[string]dbtypes.MileStone

// isPiparserRunning is the flag set when a Piparser instance is running.
const isPiparserRunning = uint32(1)

// piParserCounter is a counter that helps guarantee that only one instance
// of proposalsUpdateHandler can ever be running at any one given moment.
var piParserCounter uint32

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

// ProposalsFetcher defines the interface of the proposals plug-n-play data source.
type ProposalsFetcher interface {
	UpdateSignal() <-chan struct{}
	ProposalsHistory() ([]*pitypes.History, error)
	ProposalsHistorySince(since time.Time) ([]*pitypes.History, error)
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

func (u *utxoStore) Peek(txHash string, txIndex uint32) *dbtypes.UTXOData {
	u.Lock()
	defer u.Unlock()
	txVals, ok := u.c[txHash]
	if !ok {
		return nil
	}
	return txVals[txIndex]
}

func (u *utxoStore) set(txHash string, txIndex uint32, voutDbID int64, addrs []string, val int64, mixed bool) {
	txUTXOVals, ok := u.c[txHash]
	if !ok {
		u.c[txHash] = map[uint32]*dbtypes.UTXOData{
			txIndex: {
				Addresses: addrs,
				Value:     val,
				Mixed:     mixed,
				VoutDbID:  voutDbID,
			},
		}
	} else {
		txUTXOVals[txIndex] = &dbtypes.UTXOData{
			Addresses: addrs,
			Value:     val,
			Mixed:     mixed,
			VoutDbID:  voutDbID,
		}
	}
}

// Set stores the addresses and amount in a UTXOData entry in the cache for the
// given outpoint.
func (u *utxoStore) Set(txHash string, txIndex uint32, voutDbID int64, addrs []string, val int64, mixed bool) {
	u.Lock()
	defer u.Unlock()
	u.set(txHash, txIndex, voutDbID, addrs, val, mixed)
}

// Reinit re-initializes the utxoStore with the given UTXOs.
func (u *utxoStore) Reinit(utxos []dbtypes.UTXO) {
	if len(utxos) == 0 {
		return
	}
	u.Lock()
	defer u.Unlock()
	// Pre-allocate the transaction hash map assuming the number of unique
	// transaction hashes in input is roughly 2/3 of the number of UTXOs.
	prealloc := 2 * len(utxos) / 3
	u.c = make(map[string]map[uint32]*dbtypes.UTXOData, prealloc)
	for i := range utxos {
		u.set(utxos[i].TxHash, utxos[i].TxIndex, utxos[i].VoutDbID, utxos[i].Addresses, utxos[i].Value, utxos[i].Mixed)
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

// BlockGetter implements a few basic blockchain data retrieval functions. It is
// like rpcutils.BlockFetcher except that it must also implement
// GetBlockChainInfo.
type BlockGetter interface {
	// rpcutils.BlockFetcher implements GetBestBlock, GetBlock, GetBlockHash,
	// and GetBlockHeaderVerbose.
	rpcutils.BlockFetcher

	// GetBlockChainInfo is required for a legacy upgrade involving agendas.
	GetBlockChainInfo() (*chainjson.GetBlockChainInfoResult, error)
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
	piparser           ProposalsFetcher
	proposalsSync      lastSync
	cockroach          bool
	MPC                *mempool.MempoolDataCache
	// BlockCache stores apitypes.BlockDataBasic and apitypes.StakeInfoExtended
	// in StoreBlock for quick retrieval without a DB query.
	BlockCache        *apitypes.APICache
	heightClients     []chan uint32
	shutdownDcrdata   func()
	Client            *rpcclient.Client
	tipMtx            sync.Mutex
	tipSummary        *apitypes.BlockDataBasic
	lastExplorerBlock struct {
		sync.Mutex
		hash      string
		blockInfo *exptypes.BlockInfo
		// Somewhat unrelated, difficulties is a map of timestamps to mining
		// difficulties. It is in this cache struct since these values are
		// commonly retrieved when the explorer block is updated.
		difficulties map[int64]float64
	}
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

// lastSync defines the latest sync time for the proposal votes sync.
type lastSync struct {
	mtx      sync.RWMutex
	syncTime time.Time
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

// MissingSideChainBlocks identifies side chain blocks that are missing from the
// DB. Side chains known to dcrd are listed via the getchaintips RPC. Each block
// presence in the postgres DB is checked, and any missing block is returned in
// a SideChain along with a count of the total number of missing blocks.
func (pgb *ChainDB) MissingSideChainBlocks() ([]dbtypes.SideChain, int, error) {
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

type ChainDBCfg struct {
	DBi                               *DBInfo
	Params                            *chaincfg.Params
	DevPrefetch, HidePGConfig         bool
	AddrCacheRowCap, AddrCacheAddrCap int
	AddrCacheUTXOByteCap              int
}

// NewChainDB constructs a ChainDB for the given connection and Decred network
// parameters. By default, duplicate row checks on insertion are enabled. See
// NewChainDBWithCancel to enable context cancellation of running queries.
// proposalsUpdateChan is used to manage politeia update notifications trigger
// between the notifier and the handler method. A non-nil BlockGetter is only
// needed if database upgrades are required.
func NewChainDB(cfg *ChainDBCfg, stakeDB *stakedb.StakeDatabase,
	mp rpcutils.MempoolAddressChecker, parser ProposalsFetcher, client *rpcclient.Client,
	shutdown func()) (*ChainDB, error) {
	ctx := context.Background()
	chainDB, err := NewChainDBWithCancel(ctx, cfg, stakeDB, mp, parser, client, shutdown)
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
// cancel queries with CTRL+C, for example, use NewChainDBWithCancel. A non-nil
// BlockGetter is only needed if database upgrades are required.
func NewChainDBWithCancel(ctx context.Context, cfg *ChainDBCfg, stakeDB *stakedb.StakeDatabase,
	mp rpcutils.MempoolAddressChecker, parser ProposalsFetcher, client *rpcclient.Client,
	shutdown func()) (*ChainDB, error) {
	// Connect to the PostgreSQL daemon and return the *sql.DB.
	dbi := cfg.DBi
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

	cockroach := strings.Contains(pgVersion, "CockroachDB")

	// Optionally logs the PostgreSQL configuration.
	if !cockroach && !cfg.HidePGConfig {
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

	// Check the synchronous_commit setting.
	if !cockroach {
		syncCommit, err := RetrieveSysSettingSyncCommit(db)
		if err != nil {
			return nil, err
		}
		if syncCommit != "off" {
			log.Warnf(`PERFORMANCE ISSUE! The synchronous_commit setting is "%s". `+
				`Changing it to "off".`, syncCommit)
			// Turn off synchronous_commit.
			if err = SetSynchronousCommit(db, "off"); err != nil {
				return nil, fmt.Errorf("failed to set synchronous_commit: %v", err)
			}
			// Verify that the setting was changed.
			if syncCommit, err = RetrieveSysSettingSyncCommit(db); err != nil {
				return nil, err
			}
			if syncCommit != "off" {
				log.Errorf(`Failed to set synchronous_commit="off". Check PostgreSQL user permissions.`)
			}
		}
	} else {
		// Force CockroachDB to use a real sequence when creating a table with a
		// SERIAL column.
		_, err = db.Exec("SET experimental_serial_normalization = sql_sequence;")
		if err != nil {
			return nil, fmt.Errorf("failed to set experimental_serial_normalization: %v", err)
		}

		// Prevent too many versions of nextval() during bulk inserts using
		// autoincrement of row primary key by lowering garbage the collection
		// interval (from 25 hours!).
		crdbGCInterval := 1200 // 20 minutes between garbage collections
		_, err = db.Exec(fmt.Sprintf(`ALTER DATABASE %s CONFIGURE ZONE USING gc.ttlseconds=$1;`,
			dbi.DBName), crdbGCInterval)
		if err != nil {
			// In secure mode, the user may need permissions to modify zones. e.g.
			// GRANT UPDATE ON TABLE dcrdata_mainnet.crdb_internal.zones TO dcrdata_user;
			return nil, fmt.Errorf(`failed to set gc.ttlseconds=%d for database "%s": %v`,
				crdbGCInterval, dbi.DBName, err)
		}
	}

	params := cfg.Params

	// Perform any necessary database schema upgrades.
	var doLegacyUpgrade bool
	dbVer, compatAction, err := versionCheck(db)
	switch err {
	case nil:
		if compatAction == OK {
			// meta table present and no upgrades required
			log.Infof("DB schema version %v", dbVer)
			break
		}

		// Upgrades required
		if client == nil {
			return nil, fmt.Errorf("a rpcclient.Client is required for database upgrades")
		}
		// Do upgrades required by meta table versioning.
		log.Infof("DB schema version %v upgrading to version %v", dbVer, targetDatabaseVersion)
		upgrader := NewUpgrader(ctx, db, client, stakeDB)
		success, err := upgrader.UpgradeDatabase()
		if err != nil {
			return nil, fmt.Errorf("failed to upgrade database: %v", err)
		}
		if !success {
			return nil, fmt.Errorf("failed to upgrade database (upgrade not supported?)")
		}
	case tablesNotFoundErr:
		// Empty database (no blocks table). Proceed to setupTables.
		log.Infof(`Empty database "%s". Creating tables...`, dbi.DBName)
		if err = CreateTables(db); err != nil {
			return nil, fmt.Errorf("failed to create tables: %v", err)
		}
		err = insertMetaData(db, &metaData{
			netName:         params.Name,
			currencyNet:     uint32(params.Net),
			bestBlockHeight: -1,
			dbVer:           *targetDatabaseVersion,
		})
		if err != nil {
			return nil, fmt.Errorf("insertMetaData failed: %v", err)
		}
	case metaNotFoundErr:
		log.Infof("Legacy DB versioning found.")
		doLegacyUpgrade = true
		if client == nil {
			return nil, fmt.Errorf("a rpcclient.Client is required for database upgrades")
		}
		// Create any missing tables with version comments for legacy upgrades.
		if err = CreateTablesLegacy(db); err != nil {
			return nil, fmt.Errorf("failed to create tables with legacy versioning: %v", err)
		}
	default:
		return nil, err
	}

	// Get the best block height from the blocks table.
	bestHeight, bestHash, err := RetrieveBestBlock(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("RetrieveBestBlock: %v", err)
	}
	// NOTE: Once legacy versioned tables are no longer in use, use the height
	// and hash from DBBestBlock instead.

	// Unless legacy table versioning is still in use, verify that the best
	// block in the meta table is the same as in the blocks table. If the blocks
	// table is ahead of the meta table, it is likely that the data for the best
	// block was not fully inserted into all tables. Purge data back to the meta
	// table's best block height. Also purge if the hashes do not match.
	if !doLegacyUpgrade {
		dbHash, dbHeightInit, err := DBBestBlock(ctx, db)
		if err != nil {
			return nil, fmt.Errorf("DBBestBlock: %v", err)
		}

		// Best block height in the transactions table (written to even before
		// the blocks table).
		bestTxsBlockHeight, bestTxsBlockHash, err :=
			RetrieveTxsBestBlockMainchain(ctx, db)
		if err != nil {
			return nil, err
		}

		if bestTxsBlockHeight > bestHeight {
			bestHeight = bestTxsBlockHeight
			bestHash = bestTxsBlockHash
		}

		// The meta table's best block height should never end up larger than
		// the blocks table's best block height, but purge a block anyway since
		// something went awry. This will update the best block in the meta
		// table to match the blocks table, allowing dcrdata to start.
		if dbHeightInit > bestHeight {
			log.Warnf("Best block height in meta table (%d) "+
				"greater than best height in blocks table (%d)!",
				dbHeightInit, bestHeight)
			_, bestHeight, bestHash, err = DeleteBestBlock(ctx, db)
			if err != nil {
				return nil, fmt.Errorf("DeleteBestBlock: %v", err)
			}
			dbHash, dbHeightInit, err = DBBestBlock(ctx, db)
			if err != nil {
				return nil, fmt.Errorf("DBBestBlock: %v", err)
			}
		}

		// Purge blocks if the best block hashes do not match, and until the
		// best block height in the data tables is less than or equal to the
		// starting height in the meta table.
		log.Debugf("meta height %d / blocks height %d", dbHeightInit, bestHeight)
		for dbHash != bestHash || dbHeightInit < bestHeight {
			log.Warnf("Purging best block %s (%d).", bestHash, bestHeight)

			// Delete the best block across all tables, updating the best block
			// in the meta table.
			_, bestHeight, bestHash, err = DeleteBestBlock(ctx, db)
			if err != nil {
				return nil, fmt.Errorf("DeleteBestBlock: %v", err)
			}
			if bestHeight == -1 {
				break
			}

			// Now dbHash must equal bestHash. If not, DeleteBestBlock failed to
			// update the meta table.
			dbHash, _, err = DBBestBlock(ctx, db)
			if err != nil {
				return nil, fmt.Errorf("DBBestBlock: %v", err)
			}
			if dbHash != bestHash {
				return nil, fmt.Errorf("best block hash in meta and blocks tables do not match: "+
					"%s != %s", dbHash, bestHash)
			}
		}
	}

	// Project fund address of the current network
	var projectFundAddress string
	if projectFundAddress, err = dbtypes.DevSubsidyAddress(params); err != nil {
		log.Warnf("ChainDB.NewChainDB: %v", err)
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
		height: bestHeight,
		hash:   bestHash,
	}

	// Create the address cache with the given capacity. The project fund
	// address is set to prevent purging its data when cache reaches capacity.
	addrCache := cache.NewAddressCache(cfg.AddrCacheRowCap, cfg.AddrCacheAddrCap,
		cfg.AddrCacheUTXOByteCap)
	addrCache.ProjectAddress = projectFundAddress

	chainDB := &ChainDB{
		ctx:                ctx,
		queryTimeout:       queryTimeout,
		db:                 db,
		mp:                 mp,
		chainParams:        params,
		devAddress:         projectFundAddress,
		dupChecks:          true,
		bestBlock:          bestBlock,
		lastBlock:          make(map[chainhash.Hash]uint64),
		stakeDB:            stakeDB,
		unspentTicketCache: unspentTicketCache,
		AddressCache:       addrCache,
		CacheLocks:         cacheLocks{cache.NewCacheLock(), cache.NewCacheLock(), cache.NewCacheLock(), cache.NewCacheLock()},
		devPrefetch:        cfg.DevPrefetch,
		tpUpdatePermission: tpUpdatePermissions,
		utxoCache:          newUtxoStore(5e4),
		deployments:        new(ChainDeployments),
		piparser:           parser,
		cockroach:          cockroach,
		MPC:                new(mempool.MempoolDataCache),
		BlockCache:         apitypes.NewAPICache(1e4),
		heightClients:      make([]chan uint32, 0),
		shutdownDcrdata:    shutdown,
		Client:             client,
	}
	chainDB.lastExplorerBlock.difficulties = make(map[int64]float64)

	// Update the current chain state in the ChainDB
	if client != nil {
		bci, err := chainDB.BlockchainInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch the latest blockchain info")
		}
		chainDB.UpdateChainState(bci)
	}

	// If loading a DB with the legacy versioning system, fully upgrade prior to
	// migrating to meta table versioning.
	if doLegacyUpgrade {
		log.Infof("Performing legacy DB version check and upgrade.")
		if err = chainDB.VersionCheck(client); err != nil {
			return chainDB, fmt.Errorf("legacy DB version check/upgrade failed: %v", err)
		}

		log.Infof("Migrating DB versioning scheme to meta table versioning...")
		if err = CreateTable(db, "meta"); err != nil {
			return chainDB, fmt.Errorf(`failed to create "meta" table: %v`, err)
		}
		err = insertMetaData(db, &metaData{
			netName:         params.Name,
			currencyNet:     uint32(params.Net),
			bestBlockHeight: bestBlock.height,
			bestBlockHash:   bestBlock.hash,
			dbVer:           *legacyDatabaseVersion,
			ibdComplete:     true, // We don't know this, but assume it is done.
		})
		if err != nil {
			return chainDB, fmt.Errorf("insertMetaData failed: %v", err)
		}

		// Now run any upgrades from legacyDatabaseVersion to
		// targetDatabaseVersion.
		upgrader := NewUpgrader(ctx, db, client, stakeDB)
		success, err := upgrader.UpgradeDatabase()
		if err != nil {
			return chainDB, fmt.Errorf("failed to upgrade legacy database: %v", err)
		} else if !success {
			return chainDB, fmt.Errorf("failed to upgrade legacy database (upgrade not supported?)")
		}
	}

	return chainDB, nil
}

// StartPiparserHandler controls how piparser update handler will be initiated.
// This handler should to be run once only when the first sync after startup completes.
func (pgb *ChainDB) StartPiparserHandler() {
	if atomic.CompareAndSwapUint32(&piParserCounter, 0, isPiparserRunning) {
		// Start the proposal updates handler async method.
		pgb.proposalsUpdateHandler()

		log.Info("Piparser instance to handle updates is now active")
	} else {
		log.Error("piparser instance is already running, another one cannot be activated")
	}
}

// Close closes the underlying sql.DB connection to the database.
func (pgb *ChainDB) Close() error {
	return pgb.db.Close()
}

// SqlDB returns the underlying sql.DB, which should not be used directly unless
// you know what you are doing (if you have to ask...).
func (pgb *ChainDB) SqlDB() *sql.DB {
	return pgb.db
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

var (
	// metaNotFoundErr is the error from versionCheck when the meta table does
	// not exist.
	metaNotFoundErr = errors.New("meta table not found")

	// tablesNotFoundErr is the error from versionCheck when any of the tables
	// do not exist.
	tablesNotFoundErr = errors.New("tables not found")
)

// versionCheck attempts to retrieve the database version from the meta table,
// along with a CompatAction upgrade plan. If any of the regular data tables do
// not exist, a tablesNotFoundErr error is returned to indicated that the tables
// do not exist (or are partially created.) If the data tables exist but the
// meta table does not exist, a metaNotFoundErr error is returned to indicate
// that the legacy table versioning system is in use.
func versionCheck(db *sql.DB) (*DatabaseVersion, CompatAction, error) {
	// Detect an empty database, only checking for the "blocks" table since some
	// of the tables are created by schema upgrades (e.g. proposal_votes).
	exists, err := TableExists(db, "blocks")
	if err != nil {
		return nil, Unknown, err
	}
	if !exists {
		return nil, Unknown, tablesNotFoundErr
	}

	// The meta table stores the database schema version.
	exists, err = TableExists(db, "meta")
	if err != nil {
		return nil, Unknown, err
	}
	// If there is no meta table, this could indicate the legacy table
	// versioning system is still in used. Return the MetaNotFoundErr error.
	if !exists {
		return nil, Unknown, metaNotFoundErr
	}

	// Retrieve the database version from the meta table.
	dbVer, err := DBVersion(db)
	if err != nil {
		return nil, Unknown, fmt.Errorf("DBVersion failure: %v", err)
	}

	// Return the version, and an upgrade plan to reach targetDatabaseVersion.
	return &dbVer, dbVer.NeededToReach(targetDatabaseVersion), nil
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

// RegisterCharts registers chart data fetchers and appenders with the provided
// ChartData.
func (pgb *ChainDB) RegisterCharts(charts *cache.ChartData) {
	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "basic blocks",
		Fetcher:  pgb.chartBlocks,
		Appender: appendChartBlocks,
	})

	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "coin supply",
		Fetcher:  pgb.coinSupply,
		Appender: appendCoinSupply,
	})

	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "window stats",
		Fetcher:  pgb.windowStats,
		Appender: appendWindowStats,
	})

	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "missed votes stats",
		Fetcher:  pgb.missedVotesStats,
		Appender: appendMissedVotesPerWindow,
	})

	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "fees",
		Fetcher:  pgb.blockFees,
		Appender: appendBlockFees,
	})

	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "privacyParticipation",
		Fetcher:  pgb.privacyParticipation,
		Appender: appendPrivacyParticipation,
	})

	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "anonymitySet",
		Fetcher:  pgb.anonymitySet,
		Appender: appendAnonymitySet,
	})

	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "pool stats",
		Fetcher:  pgb.poolStats,
		Appender: appendPoolStats,
	})
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

// HeightDB retrieves the best block height according to the meta table.
func (pgb *ChainDB) HeightDB() (int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, height, err := DBBestBlock(ctx, pgb.db)
	return height, pgb.replaceCancelError(err)
}

// HashDB retrieves the best block hash according to the meta table.
func (pgb *ChainDB) HashDB() (string, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	hash, _, err := DBBestBlock(ctx, pgb.db)
	return hash, pgb.replaceCancelError(err)
}

// HeightHashDB retrieves the best block height and hash according to the meta
// table.
func (pgb *ChainDB) HeightHashDB() (int64, string, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	hash, height, err := DBBestBlock(ctx, pgb.db)
	return height, hash, pgb.replaceCancelError(err)
}

// HeightDBLegacy queries the blocks table for the best block height. When the
// tables are empty, the returned height will be -1.
func (pgb *ChainDB) HeightDBLegacy() (int64, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	bestHeight, _, _, err := RetrieveBestBlockHeight(ctx, pgb.db)
	height := int64(bestHeight)
	if err == sql.ErrNoRows {
		height = -1
	}
	return height, pgb.replaceCancelError(err)
}

// HashDBLegacy queries the blocks table for the best block's hash.
func (pgb *ChainDB) HashDBLegacy() (string, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	_, bestHash, _, err := RetrieveBestBlockHeight(ctx, pgb.db)
	return bestHash, pgb.replaceCancelError(err)
}

// HeightHashDBLegacy queries the blocks table for the best block's height and
// hash.
func (pgb *ChainDB) HeightHashDBLegacy() (uint64, string, error) {
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

// GetBestBlockHash is for middleware DataSource compatibility. No DB query is
// performed; the last stored height is used.
func (pgb *ChainDB) GetBestBlockHash() (string, error) {
	return pgb.BestBlockHashStr(), nil
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

// proposalsUpdateHandler runs in the background asynchronous to retrieve the
// politeia proposal updates that the piparser tool signaled.
func (pgb *ChainDB) proposalsUpdateHandler() {
	// Do not initiate the async update if invalid or disabled piparser instance was found.
	if pgb.piparser == nil {
		log.Error("invalid or disabled piparser instance found: proposals async update stopped")
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("recovered from piparser panic in proposalsUpdateHandler: %v", r)
				select {
				case <-time.NewTimer(time.Minute).C:
					log.Infof("attempting to restart proposalsUpdateHandler")
					pgb.proposalsUpdateHandler()
				case <-pgb.ctx.Done():
				}
			}
		}()
		for range pgb.piparser.UpdateSignal() {
			count, err := pgb.PiProposalsHistory()
			if err != nil {
				log.Error("pgb.PiProposalsHistory failed : %v", err)
			} else {
				log.Infof("%d politeia's proposal commits were processed", count)
			}
		}
	}()
}

// LastPiParserSync returns last time value when the piparser run sync on proposals
// and proposal_votes table.
func (pgb *ChainDB) LastPiParserSync() time.Time {
	pgb.proposalsSync.mtx.RLock()
	defer pgb.proposalsSync.mtx.RUnlock()
	return pgb.proposalsSync.syncTime
}

// PiProposalsHistory queries the politeia's proposal updates via the parser tool
// and pushes them to the proposals and proposal_votes tables.
func (pgb *ChainDB) PiProposalsHistory() (int64, error) {
	if pgb.piparser == nil {
		return -1, fmt.Errorf("invalid piparser instance was found")
	}

	pgb.proposalsSync.mtx.Lock()

	// set the sync time
	pgb.proposalsSync.syncTime = time.Now().UTC()

	pgb.proposalsSync.mtx.Unlock()

	var isChecked bool
	var proposalsData []*pitypes.History

	lastUpdate, err := retrieveLastCommitTime(pgb.db)
	switch {
	case err == sql.ErrNoRows:
		// No records exists yet fetch all the history.
		proposalsData, err = pgb.piparser.ProposalsHistory()

	case err != nil:
		return -1, fmt.Errorf("retrieveLastCommitTime failed :%v", err)

	default:
		// Fetch the updates since the last insert only.
		proposalsData, err = pgb.piparser.ProposalsHistorySince(lastUpdate)
		isChecked = true
	}

	if err != nil {
		return -1, fmt.Errorf("politeia proposals fetch failed: %v", err)
	}

	var commitsCount int64

	for _, entry := range proposalsData {
		if entry.CommitSHA == "" {
			// If missing commit sha ignore the entry.
			continue
		}

		// Multiple tokens votes data can be packed in a single Politeia's commit.
		for _, val := range entry.Patch {
			if val.Token == "" {
				// If missing token ignore it.
				continue
			}

			id, err := InsertProposal(pgb.db, val.Token, entry.Author,
				entry.CommitSHA, entry.Date, isChecked)
			if err != nil {
				return -1, fmt.Errorf("InsertProposal failed: %v", err)
			}

			for _, vote := range val.VotesInfo {
				_, err = InsertProposalVote(pgb.db, id, vote.Ticket,
					string(vote.VoteBit), isChecked)
				if err != nil {
					return -1, fmt.Errorf("InsertProposalVote failed: %v", err)
				}
			}
		}
		commitsCount++
	}

	return commitsCount, err
}

// ProposalVotes retrieves all the votes data associated with the provided token.
func (pgb *ChainDB) ProposalVotes(proposalToken string) (*dbtypes.ProposalChartsData, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	chartsData, err := retrieveProposalVotesData(ctx, pgb.db, proposalToken)
	return chartsData, pgb.replaceCancelError(err)
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

// AgendasVotesSummary fetches the total vote choices count for the provided
// agenda.
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
	bgi, err := retrieveWindowBlocks(ctx, pgb.db,
		pgb.chainParams.StakeDiffWindowSize, pgb.Height(), limit, offset)
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
	timeChart, priceChart, outputsChart, height, intervalFound, stale :=
		TicketPoolData(interval, heightSeen)
	if intervalFound && !stale {
		// The cache was fresh.
		return timeChart, priceChart, outputsChart, height, nil
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
			timeChart, priceChart, outputsChart, height, intervalFound, stale =
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

		return timeChart, priceChart, outputsChart, height, nil
	}
	// This goroutine is now the cache updater.
	defer pgb.tpUpdatePermission[interval].Unlock()

	// Retrieve chart data for best block in DB.
	var err error
	timeChart, priceChart, outputsChart, height, err = pgb.ticketPoolVisualization(interval)
	if err != nil {
		log.Errorf("Failed to fetch ticket pool data: %v", err)
		return nil, nil, nil, 0, err
	}

	// Update the cache with the new ticket pool data.
	UpdateTicketPoolData(interval, timeChart, priceChart, outputsChart, height)

	return timeChart, priceChart, outputsChart, height, nil
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
		err := pgb.updateProjectFundCache()
		if err != nil && !IsRetryError(err) {
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
	bal, validHeight = pgb.AddressCache.Balance(address) // bal is a copy
	if bal != nil && *bestHash == validHeight.Hash {
		return
	}

	busy, wait, done := pgb.CacheLocks.bal.TryLock(address)
	if busy {
		// Let others get the wait channel while we wait.
		// To return stale cache data if it is available:
		// bal, _ := pgb.AddressCache.Balance(address)
		// if bal != nil {
		// 	return bal, nil
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
		cache.NewBlockID(bestHash, height)) // stores a copy of bal
	return
}

// updateAddressRows updates address rows, or waits for them to update by an
// ongoing query. On completion, the cache should be ready, although it must be
// checked again. The returned []*dbtypes.AddressRow contains ALL non-merged
// address transaction rows that were stored in the cache.
func (pgb *ChainDB) updateAddressRows(address string) (rows []*dbtypes.AddressRow, err error) {
	busy, wait, done := pgb.CacheLocks.rows.TryLock(address)
	if busy {
		// Just wait until the updater is finished.
		<-wait
		err = retryError{}
		return
	}

	// We will run the DB query, so block others from doing the same. When query
	// and/or cache update is completed, broadcast to any waiters that the coast
	// is clear.
	defer done()

	// Prior to performing the query, clear the old rows to save memory.
	pgb.AddressCache.ClearRows(address)

	hash, height := pgb.BestBlock()
	blockID := cache.NewBlockID(hash, height)

	// Retrieve all non-merged address transaction rows.
	rows, err = pgb.AddressTransactionsAll(address)
	if err != nil && err != sql.ErrNoRows {
		return
	}

	// Update address rows cache.
	pgb.AddressCache.StoreRows(address, rows, blockID)
	return
}

// AddressRowsMerged gets the merged address rows either from cache or via DB
// query.
func (pgb *ChainDB) AddressRowsMerged(address string) ([]*dbtypes.AddressRowMerged, error) {
	// Try the address cache.
	hash := pgb.BestBlockHash()
	rowsCompact, validBlock := pgb.AddressCache.Rows(address)
	cacheCurrent := validBlock != nil && validBlock.Hash == *hash && rowsCompact != nil
	if cacheCurrent {
		log.Tracef("AddressRowsMerged: rows cache HIT for %s.", address)
		return dbtypes.MergeRowsCompact(rowsCompact), nil
	}

	// Make the pointed to AddressRowMerged structs eligible for garbage
	// collection. pgb.updateAddressRows sets a new AddressRowMerged slice
	// retrieved from the database, so we do not want to hang on to a copy of
	// the old data.
	//nolint:ineffassign
	rowsCompact = nil

	log.Tracef("AddressRowsMerged: rows cache MISS for %s.", address)

	// Update or wait for an update to the cached AddressRows.
	rows, err := pgb.updateAddressRows(address)
	if err != nil {
		if IsRetryError(err) {
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
func (pgb *ChainDB) AddressRowsCompact(address string) ([]*dbtypes.AddressRowCompact, error) {
	// Try the address cache.
	hash := pgb.BestBlockHash()
	rowsCompact, validBlock := pgb.AddressCache.Rows(address)
	cacheCurrent := validBlock != nil && validBlock.Hash == *hash && rowsCompact != nil
	if cacheCurrent {
		log.Tracef("AddressRowsCompact: rows cache HIT for %s.", address)
		return rowsCompact, nil
	}

	// Make the pointed to AddressRowCompact structs eligible for garbage
	// collection. pgb.updateAddressRows sets a new AddressRowCompact slice
	// retrieved from the database, so we do not want to hang on to a copy of
	// the old data.
	//nolint:ineffassign
	rowsCompact = nil

	log.Tracef("AddressRowsCompact: rows cache MISS for %s.", address)

	// Update or wait for an update to the cached AddressRows.
	rows, err := pgb.updateAddressRows(address)
	if err != nil {
		if IsRetryError(err) {
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
		//nolint:ineffassign
		addressRows = nil // allow garbage collection of each AddressRow in cache.
		log.Debugf("Address rows (view=%s) cache MISS for %s.",
			txnView.String(), address)

		// Update or wait for an update to the cached AddressRows, returning ALL
		// NON-MERGED address transaction rows.
		addressRows, err = pgb.updateAddressRows(address)
		if err != nil && err != sql.ErrNoRows {
			// See if another caller ran the update, in which case we were just
			// waiting to avoid a simultaneous query. With luck the cache will
			// be updated with this data, although it may not be. Try again.
			if IsRetryError(err) {
				// Try again, starting with cache.
				return pgb.AddressHistory(address, N, offset, txnView)
			}
			return nil, nil, fmt.Errorf("failed to updateAddressRows: %v", err)
		}

		// Select the correct type and range of address rows, merging if needed.
		addressRows, err = dbtypes.SliceAddressRows(addressRows, int(N), int(offset), txnView)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to SliceAddressRows: %v", err)
		}
	}
	log.Debugf("Address rows (view=%s) cache HIT for %s.",
		txnView.String(), address)

	// addressRows is now present and current. Proceed to get the balance.

	// Try the address balance cache.
	balance, validBlock := pgb.AddressCache.Balance(address) // balance is a copy
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
		pgb.AddressCache.StoreBalance(address, balance, blockID) // a copy of balance is stored
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
func (pgb *ChainDB) AddressData(address string, limitN, offsetAddrOuts int64,
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

		// Balances and txn counts
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
				IsFunding:     true,
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

func MakeCsvAddressRowsCompact(rows []*dbtypes.AddressRowCompact) [][]string {
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
	address, err := dcrutil.DecodeAddress(addr, pgb.chainParams)
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

// UpdateChainState updates the blockchain's state, which includes each of the
// agenda's VotingDone and Activated heights. If the agenda passed (i.e. status
// is "lockedIn" or "activated"), Activated is set to the height at which the
// rule change will take(or took) place.
func (pgb *ChainDB) UpdateChainState(blockChainInfo *chainjson.GetBlockChainInfoResult) {
	if pgb == nil {
		return
	}
	if blockChainInfo == nil {
		log.Errorf("chainjson.GetBlockChainInfoResult data passed is empty")
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

	chainInfo.AgendaMileStones = make(map[string]dbtypes.MileStone, len(blockChainInfo.Deployments))

	for agendaID, entry := range blockChainInfo.Deployments {
		agendaInfo := dbtypes.MileStone{
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
			// The voting period start height is not necessarily the height when the
			// state changed to StartedAgendaStatus because there could have already
			// been one or more RCIs of voting that failed to reach quorum. Get the
			// right voting period start height here.
			h := blockChainInfo.Blocks
			agendaInfo.VotingStarted = h - (h-entry.Since)%ruleChangeInterval
			agendaInfo.Activated = agendaInfo.VotingStarted + 2*ruleChangeInterval

		case dbtypes.FailedAgendaStatus:
			agendaInfo.VotingStarted = entry.Since - ruleChangeInterval

		case dbtypes.LockedInAgendaStatus:
			agendaInfo.VotingStarted = entry.Since - ruleChangeInterval
			agendaInfo.Activated = entry.Since + ruleChangeInterval

		case dbtypes.ActivatedAgendaStatus:
			agendaInfo.VotingStarted = entry.Since - 2*ruleChangeInterval
			agendaInfo.Activated = entry.Since
		}

		if agendaInfo.VotingStarted != 0 {
			agendaInfo.VotingDone = agendaInfo.VotingStarted + ruleChangeInterval - 1
		}

		chainInfo.AgendaMileStones[agendaID] = agendaInfo
	}

	pgb.deployments.mtx.Lock()
	pgb.deployments.chainInfo = &chainInfo
	pgb.deployments.mtx.Unlock()
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

	// update blockchain state
	pgb.UpdateChainState(blockData.BlockchainInfo)

	// New blocks stored this way are considered valid and part of mainchain,
	// warranting updates to existing records. When adding side chain blocks
	// manually, call StoreBlock directly with appropriate flags for isValid,
	// isMainchain, and updateExistingRecords, and nil winningTickets.
	isValid, isMainChain, updateExistingRecords := true, true, true

	// Since Store should not be used in batch block processing, addresses and
	// tickets spending information is updated.
	updateAddressesSpendingInfo, updateTicketsSpendingInfo := true, true

	_, _, _, err := pgb.StoreBlock(msgBlock, isValid, isMainChain,
		updateExistingRecords, updateAddressesSpendingInfo,
		updateTicketsSpendingInfo, blockData.Header.ChainWork)

	// Signal updates to any subscribed heightClients.
	pgb.SignalHeight(msgBlock.Header.Height)

	return err
}

// PurgeBestBlocks deletes all data for the N best blocks in the DB.
func (pgb *ChainDB) PurgeBestBlocks(N int64) (*dbtypes.DeletionSummary, int64, error) {
	res, height, _, err := DeleteBlocks(pgb.ctx, N, pgb.db)
	if err != nil {
		return nil, height, pgb.replaceCancelError(err)
	}

	summary := dbtypes.DeletionSummarySlice(res).Reduce()

	// Rewind stake database to this height.
	var stakeDBHeight int64
	stakeDBHeight, err = pgb.RewindStakeDB(pgb.ctx, height, true)
	if err != nil {
		return nil, height, pgb.replaceCancelError(err)
	}
	if stakeDBHeight != height {
		err = fmt.Errorf("rewind of StakeDatabase to height %d failed, "+
			"reaching height %d instead", height, stakeDBHeight)
		return nil, height, pgb.replaceCancelError(err)
	}

	return &summary, height, err
}

// RewindStakeDB attempts to disconnect blocks from the stake database to reach
// the specified height. A Context may be provided to allow cancellation of the
// rewind process. If the specified height is greater than the current stake DB
// height, RewindStakeDB will exit without error, returning the current stake DB
// height and a nil error.
func (pgb *ChainDB) RewindStakeDB(ctx context.Context, toHeight int64, quiet ...bool) (stakeDBHeight int64, err error) {
	// Target height must be non-negative. It is not possible to disconnect the
	// genesis block.
	if toHeight < 0 {
		toHeight = 0
	}

	// Periodically log progress unless quiet[0]==true
	showProgress := true
	if len(quiet) > 0 {
		showProgress = !quiet[0]
	}

	// Disconnect blocks until the stake database reaches the target height.
	stakeDBHeight = int64(pgb.stakeDB.Height())
	startHeight := stakeDBHeight
	pStep := int64(1000)
	for stakeDBHeight > toHeight {
		// Log rewind progress at regular intervals.
		if stakeDBHeight == startHeight || stakeDBHeight%pStep == 0 {
			endSegment := pStep * ((stakeDBHeight - 1) / pStep)
			if endSegment < toHeight {
				endSegment = toHeight
			}
			if showProgress {
				log.Infof("Rewinding from %d to %d", stakeDBHeight, endSegment)
			}
		}

		// Check for quit signal.
		select {
		case <-ctx.Done():
			log.Infof("Rewind cancelled at height %d.", stakeDBHeight)
			return
		default:
		}

		// Disconnect the best block.
		if err = pgb.stakeDB.DisconnectBlock(false); err != nil {
			return
		}
		stakeDBHeight = int64(pgb.stakeDB.Height())
		log.Tracef("Stake db now at height %d.", stakeDBHeight)
	}
	return
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

	// Make the pointed to ChartsData eligible for garbage collection.
	// pgb.AddressCache.StoreHistoryChart sets a new ChartsData retrieved from
	// the database, so we do not want to hang on to a copy of the old data.
	//nolint:ineffassign
	cd = nil

	busy, wait, done := pgb.CacheLocks.bal.TryLock(address)
	if busy {
		// Let others get the wait channel while we wait.
		// To return stale cache data if it is available:
		// cd, _ := pgb.AddressCache.HistoryChart(...)
		// if cd != nil {
		// 	return cd, nil
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

// windowStats fetches the charts data from retrieveWindowStats.
// This is the Fetcher half of a pair that make up a cache.ChartUpdater. The
// Appender half is appendWindowStats.
func (pgb *ChainDB) windowStats(charts *cache.ChartData) (*sql.Rows, func(), error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)

	rows, err := retrieveWindowStats(ctx, pgb.db, charts)
	if err != nil {
		return nil, cancel, fmt.Errorf("windowStats: %v", pgb.replaceCancelError(err))
	}

	return rows, cancel, nil
}

// missedVotesStats fetches the charts data from retrieveMissedVotes.
// This is the Fetcher half of a pair that make up a cache.ChartUpdater. The
// Appender half is appendMissedVotes.
func (pgb *ChainDB) missedVotesStats(charts *cache.ChartData) (*sql.Rows, func(), error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)

	rows, err := retrieveMissedVotes(ctx, pgb.db, charts)
	if err != nil {
		return nil, cancel, fmt.Errorf("missedVotesStats: %v", pgb.replaceCancelError(err))
	}

	return rows, cancel, nil
}

// chartBlocks sets or updates a series of per-block datasets.
// This is the Fetcher half of a pair that make up a cache.ChartUpdater. The
// Appender half is appendChartBlocks.
func (pgb *ChainDB) chartBlocks(charts *cache.ChartData) (*sql.Rows, func(), error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)

	rows, err := retrieveChartBlocks(ctx, pgb.db, charts)
	if err != nil {
		return nil, cancel, fmt.Errorf("chartBlocks: %v", pgb.replaceCancelError(err))
	}
	return rows, cancel, nil
}

// coinSupply fetches the coin supply chart data from retrieveCoinSupply.
// This is the Fetcher half of a pair that make up a cache.ChartUpdater. The
// Appender half is appendCoinSupply.
func (pgb *ChainDB) coinSupply(charts *cache.ChartData) (*sql.Rows, func(), error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)

	rows, err := retrieveCoinSupply(ctx, pgb.db, charts)
	if err != nil {
		return nil, cancel, fmt.Errorf("coinSupply: %v", pgb.replaceCancelError(err))
	}

	return rows, cancel, nil
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

// blockFees sets or updates a series of per-block fees.
// This is the Fetcher half of a pair that make up a cache.ChartUpdater. The
// Appender half is appendBlockFees.
func (pgb *ChainDB) blockFees(charts *cache.ChartData) (*sql.Rows, func(), error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)

	rows, err := retrieveBlockFees(ctx, pgb.db, charts)
	if err != nil {
		return nil, cancel, fmt.Errorf("chartBlocks: %v", pgb.replaceCancelError(err))
	}
	return rows, cancel, nil
}

// appendAnonymitySet sets or updates a series of per-block privacy
// participation. This is the Fetcher half of a pair that make up a
// cache.ChartUpdater. The Appender half is appendPrivacyParticipation.
func (pgb *ChainDB) privacyParticipation(charts *cache.ChartData) (*sql.Rows, func(), error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)

	rows, err := retrievePrivacyParticipation(ctx, pgb.db, charts)
	if err != nil {
		return nil, cancel, fmt.Errorf("privacyParticipation: %v", pgb.replaceCancelError(err))
	}
	return rows, cancel, nil
}

// anonymitySet sets or updates a series of per-block anonymity set. This is the
// Fetcher half of a pair that make up a cache.ChartUpdater. The Appender half
// is appendAnonymitySet.
func (pgb *ChainDB) anonymitySet(charts *cache.ChartData) (*sql.Rows, func(), error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)

	rows, err := retrieveAnonymitySet(ctx, pgb.db, charts)
	if err != nil {
		return nil, cancel, fmt.Errorf("chartBlocks: %v", pgb.replaceCancelError(err))
	}
	return rows, cancel, nil
}

// poolStats sets or updates a series of per-height ticket pool statistics.
// This is the Fetcher half of a pair that make up a cache.ChartUpdater. The
// Appender half is appendPoolStats.
func (pgb *ChainDB) poolStats(charts *cache.ChartData) (*sql.Rows, func(), error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)

	rows, err := retrievePoolStats(ctx, pgb.db, charts)
	if err != nil {
		return nil, cancel, fmt.Errorf("chartBlocks: %v", pgb.replaceCancelError(err))
	}
	return rows, cancel, nil
}

// PowerlessTickets fetches all missed and expired tickets, sorted by revocation
// status.
func (pgb *ChainDB) PowerlessTickets() (*apitypes.PowerlessTickets, error) {
	ctx, cancel := context.WithTimeout(pgb.ctx, pgb.queryTimeout)
	defer cancel()
	return retrievePowerlessTickets(ctx, pgb.db)
}

// ticketsByBlocks fetches the tickets by blocks output count chart data from
// retrieveTicketByOutputCount
// This chart has been deprecated. Leaving ticketsByBlocks for possible future
// re-appropriation, says buck54321 on April 24, 2019.
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
// This chart has been deprecated. Leaving ticketsByTPWindows for possible
// future re-appropriation, says buck54321 on April 24, 2019.
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
// version for the corresponding previous outpoints.
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

		// 3. Vouts. For all transactions in this block, locate any vouts that
		// reference them in vouts.spend_tx_row_id, and unset spend_tx_row_id.
		err = clearVoutAllSpendTxRowIDs(pgb.db, tipHash)
		if err != nil {
			log.Errorf("clearVoutAllSpendTxRowIDs for block %s: %v", tipHash, err)
		}

		// 4. Vins. Set is_mainchain=false on all vins, returning the number of
		// vins updated, the vins table row IDs, and the vouts table row IDs.
		now = time.Now()
		rowsUpdated, vinDbIDsBlk, voutDbIDsBlk, err := pgb.SetVinsMainchainByBlock(tipHash) // isMainchain from transactions table
		if err != nil {
			log.Errorf("Failed to set vins in block %s as sidechain: %v",
				tipHash, err)
		}
		vinsUpdated += rowsUpdated
		log.Debugf("SetVinsMainchainByBlock: %v", time.Since(now))

		// 5. Addresses. Set valid_mainchain=false on all addresses rows
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

		// 6. Votes. Sets is_mainchain=false on all votes in the tip block.
		now = time.Now()
		rowsUpdated, err = UpdateVotesMainchain(pgb.db, tipHash, false)
		if err != nil {
			log.Errorf("Failed to set votes in block %s as sidechain: %v",
				tipHash, err)
		}
		votesUpdated += rowsUpdated
		log.Debugf("UpdateVotesMainchain: %v", time.Since(now))

		// 7. Tickets. Sets is_mainchain=false on all tickets in the tip block.
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
func (pgb *ChainDB) StoreBlock(msgBlock *wire.MsgBlock, isValid, isMainchain,
	updateExistingRecords, updateAddressesSpendingInfo,
	updateTicketsSpendingInfo bool, chainWork string) (
	numVins int64, numVouts int64, numAddresses int64, err error) {

	// winningTickets is only set during initial chain sync.
	// Retrieve it from the stakeDB.
	var tpi *apitypes.TicketPoolInfo
	var winningTickets []string
	if isMainchain {
		var found bool
		tpi, found = pgb.stakeDB.PoolInfo(msgBlock.BlockHash())
		if !found {
			err = fmt.Errorf("TicketPoolInfo not found for block %s", msgBlock.BlockHash().String())
			return
		}
		if tpi.Height != msgBlock.Header.Height {
			err = fmt.Errorf("TicketPoolInfo height mismatch. expected %d. found %d", msgBlock.Header.Height, tpi.Height)
			return
		}
		winningTickets = tpi.Winners
	}

	// Convert the wire.MsgBlock to a dbtypes.Block.
	dbBlock := dbtypes.MsgBlockToDBBlock(msgBlock, pgb.chainParams, chainWork, winningTickets)

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
		lastTpi, found := pgb.stakeDB.PoolInfo(prevBlockHash)
		if !found {
			err = fmt.Errorf("stakedb.PoolInfo failed for block %s", msgBlock.BlockHash())
			return
		}
		winners = lastTpi.Winners
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
		err = fmt.Errorf("UpdateLastBlock: %v", err)
		return
	}

	if isMainchain {
		// Update best block height and hash.
		pgb.bestBlock.mtx.Lock()
		pgb.bestBlock.height = int64(dbBlock.Height)
		pgb.bestBlock.hash = dbBlock.Hash
		pgb.bestBlock.mtx.Unlock()

		// Insert the block stats.
		if isMainchain && tpi != nil {
			err = InsertBlockStats(pgb.db, blockDbID, tpi)
			if err != nil {
				err = fmt.Errorf("InsertBlockStats: %v", err)
				return
			}
		}

		// Update the best block in the meta table.
		err = SetDBBestBlock(pgb.db, dbBlock.Hash, int64(dbBlock.Height))
		if err != nil {
			err = fmt.Errorf("SetDBBestBlock: %v", err)
			return
		}
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

// SetDBBestBlock stores ChainDB's BestBlock data in the meta table.
func (pgb *ChainDB) SetDBBestBlock() error {
	pgb.bestBlock.mtx.RLock()
	bbHash, bbHeight := pgb.bestBlock.hash, pgb.bestBlock.height
	pgb.bestBlock.mtx.RUnlock()
	return SetDBBestBlock(pgb.db, bbHash, bbHeight)
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

		// For the transactions invalidated by this block, locate any vouts that
		// reference them in vouts.spend_tx_row_id, and unset spend_tx_row_id.
		err = clearVoutRegularSpendTxRowIDs(pgb.db, lastBlockHash.String())
		if err != nil {
			return fmt.Errorf("clearVoutRegularSpendTxRowIDs: %v", err)
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
		err = fmt.Errorf("failed to begin database transaction: %v", err)
		return
	}

	checked, doUpsert := pgb.dupChecks, updateExistingRecords

	var voutStmt *sql.Stmt
	voutStmt, err = dbTx.Prepare(internal.MakeVoutInsertStatement(checked, doUpsert))
	if err != nil {
		_ = dbTx.Rollback()
		err = fmt.Errorf("failed to prepare vout insert statement: %v", err)
		return
	}
	defer voutStmt.Close()

	var vinStmt *sql.Stmt
	vinStmt, err = dbTx.Prepare(internal.MakeVinInsertStatement(checked, doUpsert))
	if err != nil {
		_ = dbTx.Rollback()
		err = fmt.Errorf("failed to prepare vin insert statement: %v", err)
		return
	}
	defer vinStmt.Close()

	// dbAddressRows contains the data added to the address table, arranged as
	// [tx_i][addr_j], transactions paying to different numbers of addresses.
	dbAddressRows = make([][]dbtypes.AddressRow, len(txns))

	for it, Tx := range txns {
		// Insert vouts, and collect AddressRows to add to address table for
		// each output.
		Tx.VoutDbIds, dbAddressRows[it], err = InsertVoutsStmt(voutStmt, Tx.BlockHeight,
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

		// Return the transactions vout slice.
		Tx.Vouts = vouts[it]
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
	// For the given block and transaction tree, extract the transactions, vins,
	// and vouts.
	dbTransactions, dbTxVouts, dbTxVins := dbtypes.ExtractBlockTransactions(
		msgBlock.MsgBlock, txTree, chainParams, isValid, isMainchain)

	// The transactions' VinDbIds are not yet set, but update the UTXO cache
	// without it so we can check the mixed status of stake transaction inputs
	// without missing prevouts generated by txns mined in the same block
	pgb.updateUtxoCache(dbTxVouts, dbTransactions)

	// Check the previous outputs funding the stake transactions and certain
	// regular transactions, tagging the new outputs as mixed if all of the
	// previous outputs were mixed.
txns:
	for it, tx := range dbTransactions {
		if tx.MixCount > 0 {
			continue
		}

		// This only applies to stake transactions that spend the mixed outputs.
		if txhelpers.TxIsRegular(int(tx.TxType)) {
			continue
		}

		for iv := range dbTxVins[it] {
			vin := &dbTxVins[it][iv]
			if txhelpers.IsZeroHashStr(vin.PrevTxHash) {
				continue
			}
			utxo := pgb.utxoCache.Peek(vin.PrevTxHash, vin.PrevTxIndex)
			if utxo == nil {
				log.Warnf("Unable to find cached UTXO data for %s:%d",
					vin.PrevTxHash, vin.PrevTxIndex)
				continue txns
			}
			if !utxo.Mixed {
				continue txns
			}
		}

		log.Tracef("Tagging outputs of txn %s (%s) as mixed.", tx.TxID,
			txhelpers.TxTypeToString(int(tx.TxType)))
		for _, vout := range dbTxVouts[it] {
			vout.Mixed = true
		}

		//pgb.updateUtxoCache([][]*dbtypes.Vout{dbTxVouts[it]}, []*dbtypes.Tx{tx})
	}

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

	// Flatten the address rows into a single slice, and update the utxoCache
	// (again, now that vin DB IDs are set in dbTransactions).
	var dbAddressRowsFlat []*dbtypes.AddressRow
	var wg sync.WaitGroup
	processAddressRows := func() {
		dbAddressRowsFlat = pgb.flattenAddressRows(dbAddressRows, dbTransactions)
		wg.Done()
	}
	updateUTXOCache := func() {
		pgb.updateUtxoCache(dbTxVouts, dbTransactions)
		wg.Done()
	}
	// Do this concurrently with stake transaction data insertion.
	wg.Add(2)
	go processAddressRows()
	go updateUTXOCache()

	// For a side chain block, set Validators to an empty slice so that there
	// will be no misses even if there are less than 5 votes. Any Validators
	// that do not match a spent ticket hash in InsertVotes are considered
	// misses. By listing no required validators, there are no misses. For side
	// chain blocks, this is acceptable and necessary because the misses table
	// does not record the block hash or main/side chain status.
	if !isMainchain {
		msgBlock.Validators = []string{}
	}

	// If processing stake transactions, insert tickets, votes, and misses. Also
	// update pool status and spending information in tickets table pertaining
	// to the new votes, revokes, misses, and expires.
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

	wg.Wait()

	// Begin a database transaction to insert spending address rows, and (if
	// updateAddressesSpendingInfo) update matching_tx_hash in corresponding
	// funding rows and spend_tx_row_id in vouts.
	dbTx, err := pgb.db.Begin()
	if err != nil {
		txRes.err = fmt.Errorf(`unable to begin database transaction: %v`, err)
		return txRes
	}

	// Insert each new funding AddressRow, absent MatchingTxHash (spending txn
	// since these new address rows are *funding*).
	_, err = InsertAddressRowsDbTx(dbTx, dbAddressRowsFlat, pgb.dupChecks, updateExistingRecords)
	if err != nil {
		_ = dbTx.Rollback()
		log.Error("InsertAddressRows:", err)
		txRes.err = err
		return txRes
	}
	txRes.numAddresses = int64(totalAddressRows)
	txRes.addresses = make(map[string]struct{})
	for _, ad := range dbAddressRowsFlat {
		txRes.addresses[ad.Address] = struct{}{}
	}

	for it, tx := range dbTransactions {
		// vins array for this transaction
		txVins := dbTxVins[it]
		txDbID := txDbIDs[it] // for the newly-spent TXOs in the vouts table
		var voutDbIDs []int64
		if updateAddressesSpendingInfo {
			voutDbIDs = make([]int64, 0, len(txVins))
		}

		validatedTx := tx.IsValid || tx.Tree == wire.TxTreeStake
		for iv := range txVins {
			// Transaction that spends an outpoint paying to >=0 addresses
			vin := &txVins[iv]

			// Skip coinbase inputs (they are new coins and thus have no
			// previous outpoint funding them).
			if bytes.Equal(zeroHashStringBytes, []byte(vin.PrevTxHash)) {
				continue
			}

			// Insert spending txn data in addresses table, and updated spend
			// status for the previous outpoints' rows in the same table and in
			// the vouts table.
			vinDbID := tx.VinDbIds[iv]
			spendingTxHash := vin.TxID
			spendingTxIndex := vin.TxIndex

			// Attempt to retrieve cached data for this now-spent TXO. A
			// successful get will delete the entry from the cache.
			utxoData, ok := pgb.utxoCache.Get(vin.PrevTxHash, vin.PrevTxIndex)
			if !ok {
				log.Tracef("Data for that utxo (%s:%d) wasn't cached!", vin.PrevTxHash, vin.PrevTxIndex)
			}
			numAddressRowsSet, voutDbID, err := insertSpendingAddressRow(dbTx,
				vin.PrevTxHash, vin.PrevTxIndex, int8(vin.PrevTxTree),
				spendingTxHash, spendingTxIndex, vinDbID, utxoData, pgb.dupChecks,
				updateExistingRecords, tx.IsMainchainBlock, validatedTx,
				vin.TxType, updateAddressesSpendingInfo, tx.BlockTime)
			if err != nil {
				txRes.err = fmt.Errorf(`insertSpendingAddressRow: %v + %v (rollback)`,
					err, dbTx.Rollback())
				return txRes
			}
			txRes.numAddresses += numAddressRowsSet
			if updateAddressesSpendingInfo {
				voutDbIDs = append(voutDbIDs, int64(voutDbID))
			}
		}

		// NOTE: vouts.spend_tx_row_id is not updated if this is a side chain
		// block or if the transaction is stake-invalidated. Spending
		// information for extended side chain transaction outputs must still be
		// done via addresses.matching_tx_hash.
		if updateAddressesSpendingInfo && validatedTx && tx.IsMainchainBlock {
			// Set spend_tx_row_id for each prevout consumed by this txn.
			err = setSpendingForVouts(dbTx, voutDbIDs, txDbID)
			if err != nil {
				txRes.err = fmt.Errorf(`setSpendingForVouts: %v + %v (rollback)`,
					err, dbTx.Rollback())
				return txRes
			}
			// notify chart cache to update vouts
			setVoutsSpendingHeightOnCharts(voutDbIDs, msgBlock.Header.Height)
		}
	}

	txRes.err = dbTx.Commit()

	return txRes
}

func (pgb *ChainDB) updateUtxoCache(dbVouts [][]*dbtypes.Vout, txns []*dbtypes.Tx) {
	for it, tx := range txns {
		utxos := make([]*dbtypes.UTXO, 0, tx.NumVout)
		for iv, vout := range dbVouts[it] {
			// Do not store zero-value output data.
			if vout.Value == 0 {
				continue
			}

			// Allow tx.VoutDbIds to be unset.
			voutDbID := int64(-1)
			if len(tx.VoutDbIds) == len(dbVouts[it]) {
				voutDbID = int64(tx.VoutDbIds[iv])
			}

			utxos = append(utxos, &dbtypes.UTXO{
				TxHash:  vout.TxHash,
				TxIndex: vout.TxIndex,
				UTXOData: dbtypes.UTXOData{
					Addresses: vout.ScriptPubKeyData.Addresses,
					Value:     int64(vout.Value),
					Mixed:     vout.Mixed,
					VoutDbID:  voutDbID,
				},
			})
		}

		// Store each output of this transaction in the UTXO cache.
		for _, utxo := range utxos {
			pgb.utxoCache.Set(utxo.TxHash, utxo.TxIndex, utxo.VoutDbID, utxo.Addresses, utxo.Value, utxo.Mixed)
		}
	}
}

func (pgb *ChainDB) flattenAddressRows(dbAddressRows [][]dbtypes.AddressRow, txns []*dbtypes.Tx) []*dbtypes.AddressRow {
	var totalAddressRows int
	for it := range dbAddressRows {
		for ia := range dbAddressRows[it] {
			if dbAddressRows[it][ia].Value > 0 {
				totalAddressRows++
			}
		}
	}

	dbAddressRowsFlat := make([]*dbtypes.AddressRow, 0, totalAddressRows)

	for it, tx := range txns {
		// Store txn block time and mainchain validity status in AddressRows, and
		// set IsFunding to true since InsertVouts is supplying the AddressRows.
		validMainChainTx := tx.IsMainchainBlock && (tx.IsValid || tx.Tree == wire.TxTreeStake)

		// A UTXO may have multiple addresses associated with it, so check each
		// addresses table row for multiple entries for the same output of this
		// txn. This can only happen if the output's pkScript is a P2PK or P2PKH
		// multisignature script. This does not refer to P2SH, where a single
		// address corresponds to the redeem script hash even though the script
		// may be a multi-signature script.
		for ia := range dbAddressRows[it] {
			// Transaction that pays to the address
			dba := &dbAddressRows[it][ia]

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
			dba.ValidMainChain = validMainChainTx

			// Funding tx hash, vout id, value, and address are already assigned
			// by InsertVouts. Only the block time and is_funding was needed.
			dbAddressRowsFlat = append(dbAddressRowsFlat, dba)
		}
	}
	return dbAddressRowsFlat
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

		// Ensure the transactions in dbTxns and msgBlock.STransactions correspond.
		msgTx := msgTxns[i]
		if tx.TxID != msgTx.TxHash().String() {
			err = fmt.Errorf("txid of dbtypes.Tx does not match that of msgTx")
			return
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
// with storeBlockTxnTree, which will update these addresses table columns too,
// but much more slowly for a number of reasons (that are well worth
// investigating BTW!).
func (pgb *ChainDB) UpdateSpendingInfoInAllAddresses(barLoad chan *dbtypes.ProgressBarLoad) (int64, error) {
	heightDB, err := pgb.HeightDB()
	if err != nil {
		return 0, fmt.Errorf("DBBestBlock: %v", err)
	}

	tStart := time.Now()

	chunk := int64(10000)
	var rowsTouched int64
	for i := int64(0); i <= heightDB; i += chunk {
		end := i + chunk
		if end > heightDB+1 {
			end = heightDB + 1
		}
		log.Infof("Updating address rows for blocks [%d,%d]...", i, end-1)
		res, err := pgb.db.Exec(internal.UpdateAllAddressesMatchingTxHashRange, i, end)
		if err != nil {
			return 0, err
		}
		N, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		rowsTouched += N

		if barLoad != nil {
			timeTakenPerBlock := (time.Since(tStart).Seconds() / float64(end-i))
			barLoad <- &dbtypes.ProgressBarLoad{
				From:      i,
				To:        heightDB,
				Msg:       addressesSyncStatusMsg,
				BarID:     dbtypes.AddressesTableSync,
				Timestamp: int64(timeTakenPerBlock * float64(heightDB-i)),
			}

			tStart = time.Now()
		}
	}

	// Signal the completion of the sync to the status page.
	if barLoad != nil {
		barLoad <- &dbtypes.ProgressBarLoad{
			From:  heightDB,
			To:    heightDB,
			Msg:   addressesSyncStatusMsg,
			BarID: dbtypes.AddressesTableSync,
		}
	}

	return rowsTouched, nil
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

// GetChainWork fetches the chainjson.BlockHeaderVerbose and returns only the
// ChainWork attribute as a hex-encoded string, without 0x prefix.
func (pgb *ChainDB) GetChainWork(hash *chainhash.Hash) (string, error) {
	return rpcutils.GetChainWork(pgb.Client, hash)
}

// GenesisStamp returns the stamp of the lowest mainchain block in the database.
func (pgb *ChainDB) GenesisStamp() int64 {
	tDef := dbtypes.NewTimeDefFromUNIX(0)
	// Ignoring error and returning zero time.
	_ = pgb.db.QueryRowContext(pgb.ctx, internal.SelectGenesisTime).Scan(&tDef)
	return tDef.T.Unix()
}

// GetStakeInfoExtendedByHash fetches a apitypes.StakeInfoExtended, containing
// comprehensive data for the state of staking at a given block.
func (pgb *ChainDB) GetStakeInfoExtendedByHash(hashStr string) *apitypes.StakeInfoExtended {
	hash, err := chainhash.NewHashFromStr(hashStr)
	if err != nil {
		log.Errorf("GetStakeInfoExtendedByHash -> NewHashFromStr: %v", err)
		return nil
	}
	block, err := rpcutils.GetBlockByHash(hash, pgb.Client)
	if err != nil {
		log.Errorf("GetStakeInfoExtendedByHash -> GetBlockByHash: %v", err)
		return nil
	}

	msgBlock := block.MsgBlock()
	height := msgBlock.Header.Height

	var size, val int64
	var winners []string
	err = pgb.db.QueryRowContext(pgb.ctx, internal.SelectPoolInfo,
		hashStr).Scan(pq.Array(&winners), &val, &size)
	if err != nil {
		log.Errorf("Error retrieving mainchain block with stats for hash %s: %v", hashStr, err)
		return nil
	}

	coin := dcrutil.Amount(val).ToCoin()
	tpi := &apitypes.TicketPoolInfo{
		Height:  height,
		Size:    uint32(size),
		Value:   coin,
		ValAvg:  coin / float64(size),
		Winners: winners,
	}

	windowSize := uint32(pgb.chainParams.StakeDiffWindowSize)
	feeInfo := txhelpers.FeeRateInfoBlock(block)

	return &apitypes.StakeInfoExtended{
		Hash:             hashStr,
		Feeinfo:          *feeInfo,
		StakeDiff:        dcrutil.Amount(msgBlock.Header.SBits).ToCoin(),
		PriceWindowNum:   int(height / windowSize),
		IdxBlockInWindow: int(height%windowSize) + 1,
		PoolInfo:         tpi,
	}
}

// GetStakeInfoExtendedByHeight gets extended stake information for the
// mainchain block at the specified height.
func (pgb *ChainDB) GetStakeInfoExtendedByHeight(height int) *apitypes.StakeInfoExtended {
	hashStr, err := pgb.BlockHash(int64(height))
	if err != nil {
		log.Errorf("GetStakeInfoExtendedByHeight -> BlockHash: %v", err)
		return nil
	}
	return pgb.GetStakeInfoExtendedByHash(hashStr)
}

// GetPoolInfo retrieves the ticket pool statistics at the specified height.
func (pgb *ChainDB) GetPoolInfo(idx int) *apitypes.TicketPoolInfo {
	ticketPoolInfo, err := RetrievePoolInfo(pgb.ctx, pgb.db, int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve ticket pool info: %v", err)
		return nil
	}
	return ticketPoolInfo
}

// GetPoolInfoByHash retrieves the ticket pool statistics at the specified block
// hash.
func (pgb *ChainDB) GetPoolInfoByHash(hash string) *apitypes.TicketPoolInfo {
	ticketPoolInfo, err := RetrievePoolInfoByHash(pgb.ctx, pgb.db, hash)
	if err != nil {
		log.Errorf("Unable to retrieve ticket pool info: %v", err)
		return nil
	}
	return ticketPoolInfo
}

// GetPoolInfoRange retrieves the ticket pool statistics for a range of block
// heights, as a slice.
func (pgb *ChainDB) GetPoolInfoRange(idx0, idx1 int) []apitypes.TicketPoolInfo {
	ind0 := int64(idx0)
	ind1 := int64(idx1)
	tip := pgb.Height()
	if ind1 > tip || ind0 < 0 {
		log.Errorf("Unable to retrieve ticket pool info for range [%d, %d], tip=%d", idx0, idx1, tip)
		return nil
	}
	ticketPoolInfos, _, err := RetrievePoolInfoRange(pgb.ctx, pgb.db, ind0, ind1)
	if err != nil {
		log.Errorf("Unable to retrieve ticket pool info range: %v", err)
		return nil
	}
	return ticketPoolInfos
}

// GetPoolValAndSizeRange returns the ticket pool size at each block height
// within a given range.
func (pgb *ChainDB) GetPoolValAndSizeRange(idx0, idx1 int) ([]float64, []uint32) {
	ind0 := int64(idx0)
	ind1 := int64(idx1)
	tip := pgb.Height()
	if ind1 > tip || ind0 < 0 {
		log.Errorf("Unable to retrieve ticket pool info for range [%d, %d], tip=%d", idx0, idx1, tip)
		return nil, nil
	}
	poolvals, poolsizes, err := RetrievePoolValAndSizeRange(pgb.ctx, pgb.db, ind0, ind1)
	if err != nil {
		log.Errorf("Unable to retrieve ticket value and size range: %v", err)
		return nil, nil
	}
	return poolvals, poolsizes
}

// ChargePoolInfoCache prepares the stakeDB by querying the database for block
// info.
func (pgb *ChainDB) ChargePoolInfoCache(startHeight int64) error {
	if startHeight < 0 {
		startHeight = 0
	}
	endHeight := pgb.Height()
	if startHeight > endHeight {
		log.Debug("No pool info to load into cache")
		return nil
	}
	tpis, blockHashes, err := RetrievePoolInfoRange(pgb.ctx, pgb.db, startHeight, endHeight)
	if err != nil {
		return err
	}
	log.Debugf("Pre-loading pool info for %d blocks ([%d, %d]) into cache.",
		len(tpis), startHeight, endHeight)
	for i := range tpis {
		hash, err := chainhash.NewHashFromStr(blockHashes[i])
		if err != nil {
			log.Warnf("Invalid block hash: %s", blockHashes[i])
		}
		pgb.stakeDB.SetPoolInfo(*hash, &tpis[i])
	}
	return nil
}

// GetPool retrieves all the live ticket hashes at a given height.
func (pgb *ChainDB) GetPool(idx int64) ([]string, error) {
	hs, err := pgb.stakeDB.PoolDB.Pool(idx)
	if err != nil {
		log.Errorf("Unable to get ticket pool from stakedb: %v", err)
		return nil, err
	}
	hss := make([]string, 0, len(hs))
	for i := range hs {
		hss = append(hss, hs[i].String())
	}
	return hss, nil
}

// CurrentCoinSupply gets the current coin supply as an *apitypes.CoinSupply,
// which additionally contains block info and max supply.
func (pgb *ChainDB) CurrentCoinSupply() (supply *apitypes.CoinSupply) {
	coinSupply, err := pgb.Client.GetCoinSupply()
	if err != nil {
		log.Errorf("RPC failure (GetCoinSupply): %v", err)
		return
	}

	hash, height := pgb.BestBlockStr()

	return &apitypes.CoinSupply{
		Height:   height,
		Hash:     hash,
		Mined:    int64(coinSupply),
		Ultimate: txhelpers.UltimateSubsidy(pgb.chainParams),
	}
}

// GetBlockByHash gets a *wire.MsgBlock for the supplied hex-encoded hash
// string.
func (pgb *ChainDB) GetBlockByHash(hash string) (*wire.MsgBlock, error) {
	blockHash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		log.Errorf("Invalid block hash %s", hash)
		return nil, err
	}
	return pgb.Client.GetBlock(blockHash)
}

// GetHeader fetches the *chainjson.GetBlockHeaderVerboseResult for a given
// block height.
func (pgb *ChainDB) GetHeader(idx int) *chainjson.GetBlockHeaderVerboseResult {
	return rpcutils.GetBlockHeaderVerbose(pgb.Client, int64(idx))
}

// GetBlockHeaderByHash fetches the *chainjson.GetBlockHeaderVerboseResult for
// a given block hash.
func (pgb *ChainDB) GetBlockHeaderByHash(hash string) (*wire.BlockHeader, error) {
	blockHash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		log.Errorf("Invalid block hash %s", hash)
		return nil, err
	}
	return pgb.Client.GetBlockHeader(blockHash)
}

// GetRawAPITransaction gets an *apitypes.Tx for a given transaction ID.
func (pgb *ChainDB) GetRawAPITransaction(txid *chainhash.Hash) *apitypes.Tx {
	tx, _ := pgb.getRawAPITransaction(txid)
	return tx
}

func (pgb *ChainDB) getRawAPITransaction(txid *chainhash.Hash) (tx *apitypes.Tx, hex string) {
	var err error
	tx, hex, err = rpcutils.APITransaction(pgb.Client, txid)
	if err != nil {
		log.Errorf("APITransaction failed: %v", err)
	}
	return
}

// GetTrimmedTransaction gets a *apitypes.TrimmedTx for a given transaction ID.
func (pgb *ChainDB) GetTrimmedTransaction(txid *chainhash.Hash) *apitypes.TrimmedTx {
	tx, _ := pgb.getRawAPITransaction(txid)
	if tx == nil {
		return nil
	}
	return &apitypes.TrimmedTx{
		TxID:     tx.TxID,
		Version:  tx.Version,
		Locktime: tx.Locktime,
		Expiry:   tx.Expiry,
		Vin:      tx.Vin,
		Vout:     tx.Vout,
	}
}

// GetVoteInfo attempts to decode the vote bits of a SSGen transaction. If the
// transaction is not a valid SSGen, the VoteInfo output will be nil. Depending
// on the stake version with which dcrdata is compiled with (chaincfg.Params),
// the Choices field of VoteInfo may be a nil slice even if the votebits were
// set for a previously-valid agenda.
func (pgb *ChainDB) GetVoteInfo(txhash *chainhash.Hash) (*apitypes.VoteInfo, error) {
	tx, err := pgb.Client.GetRawTransaction(txhash)
	if err != nil {
		log.Errorf("GetRawTransaction failed for: %v", txhash)
		return nil, nil
	}

	validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(tx.MsgTx(), pgb.chainParams)
	if err != nil {
		return nil, err
	}
	vinfo := &apitypes.VoteInfo{
		Validation: apitypes.BlockValidation{
			Hash:     validation.Hash.String(),
			Height:   validation.Height,
			Validity: validation.Validity,
		},
		Version: version,
		Bits:    bits,
		Choices: choices,
	}
	return vinfo, nil
}

// GetVoteVersionInfo requests stake version info from the dcrd RPC server
func (pgb *ChainDB) GetVoteVersionInfo(ver uint32) (*chainjson.GetVoteInfoResult, error) {
	return pgb.Client.GetVoteInfo(ver)
}

// GetStakeVersions requests the output of the getstakeversions RPC, which gets
// stake version information and individual vote version information starting at the
// given block and for count-1 blocks prior.
func (pgb *ChainDB) GetStakeVersions(blockHash string, count int32) (*chainjson.GetStakeVersionsResult, error) {
	return pgb.Client.GetStakeVersions(blockHash, count)
}

// GetStakeVersionsLatest requests the output of the getstakeversions RPC for
// just the current best block.
func (pgb *ChainDB) GetStakeVersionsLatest() (*chainjson.StakeVersions, error) {
	txHash := pgb.BestBlockHashStr()
	stkVers, err := pgb.GetStakeVersions(txHash, 1)
	if err != nil || stkVers == nil || len(stkVers.StakeVersions) == 0 {
		return nil, err
	}
	stkVer := stkVers.StakeVersions[0]
	return &stkVer, nil
}

// GetAllTxIn gets all transaction inputs, as a slice of *apitypes.TxIn, for a
// given transaction ID.
func (pgb *ChainDB) GetAllTxIn(txid *chainhash.Hash) []*apitypes.TxIn {
	tx, err := pgb.Client.GetRawTransaction(txid)
	if err != nil {
		log.Errorf("Unknown transaction %s", txid)
		return nil
	}

	allTxIn0 := tx.MsgTx().TxIn
	allTxIn := make([]*apitypes.TxIn, len(allTxIn0))
	for i := range allTxIn {
		txIn := &apitypes.TxIn{
			PreviousOutPoint: apitypes.OutPoint{
				Hash:  allTxIn0[i].PreviousOutPoint.Hash.String(),
				Index: allTxIn0[i].PreviousOutPoint.Index,
				Tree:  allTxIn0[i].PreviousOutPoint.Tree,
			},
			Sequence:        allTxIn0[i].Sequence,
			ValueIn:         dcrutil.Amount(allTxIn0[i].ValueIn).ToCoin(),
			BlockHeight:     allTxIn0[i].BlockHeight,
			BlockIndex:      allTxIn0[i].BlockIndex,
			SignatureScript: hex.EncodeToString(allTxIn0[i].SignatureScript),
		}
		allTxIn[i] = txIn
	}

	return allTxIn
}

// GetAllTxOut gets all transaction outputs, as a slice of *apitypes.TxOut, for
// a given transaction ID.
func (pgb *ChainDB) GetAllTxOut(txid *chainhash.Hash) []*apitypes.TxOut {
	tx, err := pgb.Client.GetRawTransactionVerbose(txid)
	if err != nil {
		log.Warnf("Unknown transaction %s", txid)
		return nil
	}

	txouts := tx.Vout
	allTxOut := make([]*apitypes.TxOut, 0, len(txouts))
	for i := range txouts {
		// chainjson.Vout and apitypes.TxOut are the same except for N.
		spk := &tx.Vout[i].ScriptPubKey
		// If the script type is not recognized by apitypes, the ScriptClass
		// types may need to be updated to match dcrd.
		if spk.Type != "invalid" && !apitypes.IsValidScriptClass(spk.Type) {
			log.Warnf(`The ScriptPubKey's type "%s" is not known to dcrdata! ` +
				`Update apitypes or debug dcrd.`)
		}
		allTxOut = append(allTxOut, &apitypes.TxOut{
			Value:   txouts[i].Value,
			Version: txouts[i].Version,
			ScriptPubKeyDecoded: apitypes.ScriptPubKey{
				Asm:       spk.Asm,
				Hex:       spk.Hex,
				ReqSigs:   spk.ReqSigs,
				Type:      spk.Type,
				Addresses: spk.Addresses,
				CommitAmt: spk.CommitAmt,
			},
		})
	}

	return allTxOut
}

// GetStakeDiffEstimates gets an *apitypes.StakeDiff, which is a combo of
// chainjson.EstimateStakeDiffResult and chainjson.GetStakeDifficultyResult
func (pgb *ChainDB) GetStakeDiffEstimates() *apitypes.StakeDiff {
	sd := rpcutils.GetStakeDiffEstimates(pgb.Client)

	height := pgb.MPC.GetHeight()
	winSize := uint32(pgb.chainParams.StakeDiffWindowSize)
	sd.IdxBlockInWindow = int(height%winSize) + 1
	sd.PriceWindowNum = int(height / winSize)

	return sd
}

// GetSummary returns the *apitypes.BlockDataBasic for a given block height.
func (pgb *ChainDB) GetSummary(idx int) *apitypes.BlockDataBasic {
	blockSummary, err := pgb.BlockSummary(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve block summary: %v", err)
		return nil
	}

	return blockSummary
}

// BlockSummary returns basic block data for block ind.
func (pgb *ChainDB) BlockSummary(ind int64) (*apitypes.BlockDataBasic, error) {
	// First try the block summary cache.
	usingBlockCache := pgb.BlockCache != nil && pgb.BlockCache.IsEnabled()
	if usingBlockCache {
		if bd := pgb.BlockCache.GetBlockSummary(ind); bd != nil {
			// Cache hit!
			return bd, nil
		}
		// Cache miss necessitates a DB query.
	}

	bd, err := RetrieveBlockSummary(pgb.ctx, pgb.db, ind)
	if err != nil {
		return nil, err
	}

	if usingBlockCache {
		// This is a cache miss since hits return early.
		err = pgb.BlockCache.StoreBlockSummary(bd)
		if err != nil {
			log.Warnf("Failed to cache summary for block at %d: %v", ind, err)
			// Do not return the error.
		}
	}

	return bd, nil
}

// GetSummary returns the *apitypes.BlockDataBasic for a range of block heights.
func (pgb *ChainDB) GetSummaryRange(idx0, idx1 int) []*apitypes.BlockDataBasic {
	summaries, err := pgb.BlockSummaryRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve block summaries: %v", err)
		return nil
	}
	return summaries
}

// BlockSummaryRange returns the *apitypes.BlockDataBasic for a range of block
// height.
func (pgb *ChainDB) BlockSummaryRange(idx0, idx1 int64) ([]*apitypes.BlockDataBasic, error) {
	return RetrieveBlockSummaryRange(pgb.ctx, pgb.db, idx0, idx1)
}

// GetSummaryStepped returns the []*apitypes.BlockDataBasic for a given block
// height.
func (pgb *ChainDB) GetSummaryRangeStepped(idx0, idx1, step int) []*apitypes.BlockDataBasic {
	summaries, err := pgb.BlockSummaryRangeStepped(int64(idx0), int64(idx1), int64(step))
	if err != nil {
		log.Errorf("Unable to retrieve block summaries: %v", err)
		return nil
	}

	return summaries
}

// BlockSummaryRangeStepped returns the []*apitypes.BlockDataBasic for every
// step'th block in a specified range.
func (pgb *ChainDB) BlockSummaryRangeStepped(idx0, idx1, step int64) ([]*apitypes.BlockDataBasic, error) {
	return RetrieveBlockSummaryRangeStepped(pgb.ctx, pgb.db, idx0, idx1, step)
}

// GetSummaryByHash returns a *apitypes.BlockDataBasic for a given hex-encoded
// block hash. If withTxTotals is true, the TotalSent and MiningFee fields will
// be set, but it's costly because it requires a GetBlockVerboseByHash RPC call.
func (pgb *ChainDB) GetSummaryByHash(hash string, withTxTotals bool) *apitypes.BlockDataBasic {
	blockSummary, err := pgb.BlockSummaryByHash(hash)
	if err != nil {
		log.Errorf("Unable to retrieve block summary: %v", err)
		return nil
	}

	if withTxTotals {
		data := pgb.GetBlockVerboseByHash(hash, true)
		if data == nil {
			log.Error("Unable to get block for block hash " + hash)
			return nil
		}

		var totalFees, totalOut dcrutil.Amount
		for i := range data.RawTx {
			msgTx, err := txhelpers.MsgTxFromHex(data.RawTx[i].Hex)
			if err != nil {
				log.Errorf("Unable to decode transaction: %v", err)
				return nil
			}
			// Do not compute fee for coinbase transaction.
			if !data.RawTx[i].Vin[0].IsCoinBase() {
				fee, _ := txhelpers.TxFeeRate(msgTx)
				totalFees += fee
			}
			totalOut += txhelpers.TotalOutFromMsgTx(msgTx)
		}
		for i := range data.RawSTx {
			msgTx, err := txhelpers.MsgTxFromHex(data.RawSTx[i].Hex)
			if err != nil {
				log.Errorf("Unable to decode transaction: %v", err)
				return nil
			}
			fee, _ := txhelpers.TxFeeRate(msgTx)
			totalFees += fee
			totalOut += txhelpers.TotalOutFromMsgTx(msgTx)
		}

		miningFee := int64(totalFees)
		blockSummary.MiningFee = &miningFee
		totalSent := int64(totalOut)
		blockSummary.TotalSent = &totalSent
	}

	return blockSummary
}

// BlockSummaryByHash makes a *apitypes.BlockDataBasic, checking the BlockCache
// first before querying the database.
func (pgb *ChainDB) BlockSummaryByHash(hash string) (*apitypes.BlockDataBasic, error) {
	// First try the block summary cache.
	usingBlockCache := pgb.BlockCache != nil && pgb.BlockCache.IsEnabled()
	if usingBlockCache {
		if bd := pgb.BlockCache.GetBlockSummaryByHash(hash); bd != nil {
			// Cache hit!
			return bd, nil
		}
		// Cache miss necessitates a DB query.
	}

	bd, err := RetrieveBlockSummaryByHash(pgb.ctx, pgb.db, hash)
	if err != nil {
		return nil, err
	}

	if usingBlockCache {
		// This is a cache miss since hits return early.
		err = pgb.BlockCache.StoreBlockSummary(bd)
		if err != nil {
			log.Warnf("Failed to cache summary for block %s: %v", hash, err)
			// Do not return the error.
		}
	}

	return bd, nil
}

// GetBestBlockSummary retrieves data for the best block in the DB. If there are
// no blocks in the table (yet), a nil pointer is returned.
func (pgb *ChainDB) GetBestBlockSummary() *apitypes.BlockDataBasic {
	// Attempt to retrieve height of best block in DB.
	dbBlkHeight, err := pgb.HeightDB()
	if err != nil {
		log.Errorf("GetBlockSummaryHeight failed: %v", err)
		return nil
	}

	// Empty table is not an error.
	if dbBlkHeight == -1 {
		return nil
	}

	// Retrieve the block data.
	blockSummary, err := pgb.BlockSummary(dbBlkHeight)
	if err != nil {
		log.Errorf("Unable to retrieve block %d summary: %v", dbBlkHeight, err)
		return nil
	}

	return blockSummary
}

// GetBlockSize returns the block size in bytes for the block at a given block
// height.
func (pgb *ChainDB) GetBlockSize(idx int) (int32, error) {
	blockSize, err := pgb.BlockSize(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve block %d size: %v", idx, err)
		return -1, err
	}
	return blockSize, nil
}

// GetBlockSizeRange gets the block sizes in bytes for an inclusive range of
// block heights.
func (pgb *ChainDB) GetBlockSizeRange(idx0, idx1 int) ([]int32, error) {
	blockSizes, err := pgb.BlockSizeRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve block size range: %v", err)
		return nil, err
	}
	return blockSizes, nil
}

// BlockSize return the size of block at height ind.
func (pgb *ChainDB) BlockSize(ind int64) (int32, error) {
	// First try the block summary cache.
	usingBlockCache := pgb.BlockCache != nil && pgb.BlockCache.IsEnabled()
	if usingBlockCache {
		sz := pgb.BlockCache.GetBlockSize(ind)
		if sz != -1 {
			// Cache hit!
			return sz, nil
		}
		// Cache miss necessitates a DB query.
	}

	tip := pgb.Height()
	if ind > tip || ind < 0 {
		return -1, fmt.Errorf("Cannot retrieve block size %d, have height %d",
			ind, tip)
	}

	return RetrieveBlockSize(pgb.ctx, pgb.db, ind)
}

// BlockSizeRange returns an array of block sizes for block range ind0 to ind1
func (pgb *ChainDB) BlockSizeRange(ind0, ind1 int64) ([]int32, error) {
	tip := pgb.Height()
	if ind1 > tip || ind0 < 0 {
		return nil, fmt.Errorf("Cannot retrieve block size range [%d,%d], have height %d",
			ind0, ind1, tip)
	}
	return RetrieveBlockSizeRange(pgb.ctx, pgb.db, ind0, ind1)
}

// GetSDiff gets the stake difficulty in DCR for a given block height.
func (pgb *ChainDB) GetSDiff(idx int) float64 {
	sdiff, err := RetrieveSDiff(pgb.ctx, pgb.db, int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve stake difficulty: %v", err)
		return -1
	}
	return sdiff
}

// GetSBitsByHash gets the stake difficulty in DCR for a given block height.
func (pgb *ChainDB) GetSBitsByHash(hash string) int64 {
	sbits, err := RetrieveSBitsByHash(pgb.ctx, pgb.db, hash)
	if err != nil {
		log.Errorf("Unable to retrieve stake difficulty: %v", err)
		return -1
	}
	return sbits
}

// GetSDiffRange gets the stake difficulties in DCR for a range of block heights.
func (pgb *ChainDB) GetSDiffRange(idx0, idx1 int) []float64 {
	sdiffs, err := pgb.SDiffRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve stake difficulty range: %v", err)
		return nil
	}
	return sdiffs
}

// SDiffRange returns an array of stake difficulties for block range
// ind0 to ind1.
func (pgb *ChainDB) SDiffRange(ind0, ind1 int64) ([]float64, error) {
	tip := pgb.Height()
	if ind1 > tip || ind0 < 0 {
		return nil, fmt.Errorf("Cannot retrieve sdiff range [%d,%d], have height %d",
			ind0, ind1, tip)
	}
	return RetrieveSDiffRange(pgb.ctx, pgb.db, ind0, ind1)
}

// GetMempoolSSTxSummary returns the current *apitypes.MempoolTicketFeeInfo.
func (pgb *ChainDB) GetMempoolSSTxSummary() *apitypes.MempoolTicketFeeInfo {
	_, feeInfo := pgb.MPC.GetFeeInfoExtra()
	return feeInfo
}

// GetMempoolSSTxFeeRates returns the current mempool stake fee info for tickets
// above height N in the mempool cache.
func (pgb *ChainDB) GetMempoolSSTxFeeRates(N int) *apitypes.MempoolTicketFees {
	height, timestamp, totalFees, fees := pgb.MPC.GetFeeRates(N)
	mpTicketFees := apitypes.MempoolTicketFees{
		Height:   height,
		Time:     timestamp,
		Length:   uint32(len(fees)),
		Total:    uint32(totalFees),
		FeeRates: fees,
	}
	return &mpTicketFees
}

// GetMempoolSSTxDetails returns the current mempool ticket info for tickets
// above height N in the mempool cache.
func (pgb *ChainDB) GetMempoolSSTxDetails(N int) *apitypes.MempoolTicketDetails {
	height, timestamp, totalSSTx, details := pgb.MPC.GetTicketsDetails(N)
	mpTicketDetails := apitypes.MempoolTicketDetails{
		Height:  height,
		Time:    timestamp,
		Length:  uint32(len(details)),
		Total:   uint32(totalSSTx),
		Tickets: []*apitypes.TicketDetails(details),
	}
	return &mpTicketDetails
}

// GetAddressTransactionsRawWithSkip returns an array of apitypes.AddressTxRaw objects
// representing the raw result of SearchRawTransactionsverbose
func (pgb *ChainDB) GetAddressTransactionsRawWithSkip(addr string, count int, skip int) []*apitypes.AddressTxRaw {
	address, err := dcrutil.DecodeAddress(addr, pgb.chainParams)
	if err != nil {
		log.Infof("Invalid address %s: %v", addr, err)
		return nil
	}
	txs, err := pgb.Client.SearchRawTransactionsVerbose(address, skip, count, true, true, nil)
	if err != nil {
		if strings.Contains(err.Error(), "No Txns available") {
			return make([]*apitypes.AddressTxRaw, 0)
		}
		log.Warnf("GetAddressTransactionsRaw failed for address %s: %v", addr, err)
		return nil
	}
	txarray := make([]*apitypes.AddressTxRaw, 0, len(txs))
	for i := range txs {
		tx := new(apitypes.AddressTxRaw)
		tx.Size = int32(len(txs[i].Hex) / 2)
		tx.TxID = txs[i].Txid
		tx.Version = txs[i].Version
		tx.Locktime = txs[i].LockTime
		tx.Vin = make([]chainjson.VinPrevOut, len(txs[i].Vin))
		copy(tx.Vin, txs[i].Vin)
		tx.Confirmations = int64(txs[i].Confirmations)
		tx.BlockHash = txs[i].BlockHash
		tx.Blocktime = apitypes.TimeAPI{S: dbtypes.NewTimeDefFromUNIX(txs[i].Blocktime)}
		tx.Time = apitypes.TimeAPI{S: dbtypes.NewTimeDefFromUNIX(txs[i].Time)}
		tx.Vout = make([]apitypes.Vout, len(txs[i].Vout))
		for j := range txs[i].Vout {
			tx.Vout[j].Value = txs[i].Vout[j].Value
			tx.Vout[j].N = txs[i].Vout[j].N
			tx.Vout[j].Version = txs[i].Vout[j].Version
			spk := &tx.Vout[j].ScriptPubKeyDecoded
			spkRaw := &txs[i].Vout[j].ScriptPubKey
			spk.Asm = spkRaw.Asm
			spk.Hex = spkRaw.Hex
			spk.ReqSigs = spkRaw.ReqSigs
			spk.Type = spkRaw.Type
			spk.Addresses = make([]string, len(spkRaw.Addresses))
			for k := range spkRaw.Addresses {
				spk.Addresses[k] = spkRaw.Addresses[k]
			}
			if spkRaw.CommitAmt != nil {
				spk.CommitAmt = new(float64)
				*spk.CommitAmt = *spkRaw.CommitAmt
			}
		}
		txarray = append(txarray, tx)
	}

	return txarray
}

// GetMempoolPriceCountTime retrieves from mempool: the ticket price, the number
// of tickets in mempool, the time of the first ticket.
func (pgb *ChainDB) GetMempoolPriceCountTime() *apitypes.PriceCountTime {
	return pgb.MPC.GetTicketPriceCountTime(int(pgb.chainParams.MaxFreshStakePerBlock))
}

// GetChainParams is a getter for the current network parameters.
func (pgb *ChainDB) GetChainParams() *chaincfg.Params {
	return pgb.chainParams
}

// GetBlockVerbose fetches the *chainjson.GetBlockVerboseResult for a given
// block height. Optionally include verbose transactions.
func (pgb *ChainDB) GetBlockVerbose(idx int, verboseTx bool) *chainjson.GetBlockVerboseResult {
	block := rpcutils.GetBlockVerbose(pgb.Client, int64(idx), verboseTx)
	return block
}

func sumOutsTxRawResult(txs []chainjson.TxRawResult) (sum float64) {
	for _, tx := range txs {
		for _, vout := range tx.Vout {
			sum += vout.Value
		}
	}
	return
}

func makeExplorerBlockBasic(data *chainjson.GetBlockVerboseResult, params *chaincfg.Params) *exptypes.BlockBasic {
	index := dbtypes.CalculateWindowIndex(data.Height, params.StakeDiffWindowSize)

	total := sumOutsTxRawResult(data.RawTx) + sumOutsTxRawResult(data.RawSTx)

	numReg := len(data.RawTx)

	block := &exptypes.BlockBasic{
		IndexVal:       index,
		Height:         data.Height,
		Hash:           data.Hash,
		Version:        data.Version,
		Size:           data.Size,
		Valid:          true, // we do not know this, TODO with DB v2
		MainChain:      true,
		Voters:         data.Voters,
		Transactions:   numReg,
		FreshStake:     data.FreshStake,
		Revocations:    uint32(data.Revocations),
		TxCount:        uint32(data.FreshStake+data.Revocations) + uint32(numReg) + uint32(data.Voters),
		BlockTime:      exptypes.NewTimeDefFromUNIX(data.Time),
		FormattedBytes: humanize.Bytes(uint64(data.Size)),
		Total:          total,
	}

	return block
}

func makeExplorerTxBasic(data chainjson.TxRawResult, ticketPrice int64, msgTx *wire.MsgTx, params *chaincfg.Params) *exptypes.TxBasic {
	tx := new(exptypes.TxBasic)
	tx.TxID = data.Txid
	tx.FormattedSize = humanize.Bytes(uint64(len(data.Hex) / 2))
	tx.Total = txhelpers.TotalVout(data.Vout).ToCoin()
	tx.Fee, tx.FeeRate = txhelpers.TxFeeRate(msgTx)
	for _, i := range data.Vin {
		if i.IsCoinBase() /* not IsStakeBase */ {
			tx.Coinbase = true
			tx.Fee, tx.FeeRate = 0, 0
		}
	}
	if stake.IsSSGen(msgTx) {
		validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(msgTx, params)
		if err != nil {
			log.Debugf("Cannot get vote choices for %s", tx.TxID)
			return tx
		}
		tx.VoteInfo = &exptypes.VoteInfo{
			Validation: exptypes.BlockValidation{
				Hash:     validation.Hash.String(),
				Height:   validation.Height,
				Validity: validation.Validity,
			},
			Version: version,
			Bits:    bits,
			Choices: choices,
		}
	} else if !txhelpers.IsStakeTx(msgTx) {
		_, mixDenom, mixCount := txhelpers.IsMixTx(msgTx)
		if mixCount == 0 {
			_, mixDenom, mixCount = txhelpers.IsMixedSplitTx(msgTx, int64(txrules.DefaultRelayFeePerKb), ticketPrice)
		}
		tx.MixCount = mixCount
		tx.MixDenom = mixDenom
	}
	return tx
}

func trimmedTxInfoFromMsgTx(txraw chainjson.TxRawResult, ticketPrice int64, msgTx *wire.MsgTx, params *chaincfg.Params) *exptypes.TrimmedTxInfo {
	txBasic := makeExplorerTxBasic(txraw, ticketPrice, msgTx, params)

	var voteValid bool
	if txBasic.VoteInfo != nil {
		voteValid = txBasic.VoteInfo.Validation.Validity
	}

	tx := &exptypes.TrimmedTxInfo{
		TxBasic:   txBasic,
		Fees:      txBasic.Fee.ToCoin(),
		VinCount:  len(txraw.Vin),
		VoutCount: len(txraw.Vout),
		VoteValid: voteValid,
	}
	return tx
}

// BlockSubsidy gets the *chainjson.GetBlockSubsidyResult for the given height
// and number of voters, which can be fewer than the network parameter allows.
func (pgb *ChainDB) BlockSubsidy(height int64, voters uint16) *chainjson.GetBlockSubsidyResult {
	blockSubsidy, err := pgb.Client.GetBlockSubsidy(height, voters)
	if err != nil {
		return nil
	}
	return blockSubsidy
}

// GetExplorerBlock gets a *exptypes.Blockinfo for the specified block.
func (pgb *ChainDB) GetExplorerBlock(hash string) *exptypes.BlockInfo {
	// This function is quit expensive, and it is used by multiple
	// BlockDataSavers, so remember the BlockInfo generated for this block hash.
	// This also disallows concurrently calling this function for the same block
	// hash.
	pgb.lastExplorerBlock.Lock()
	if pgb.lastExplorerBlock.hash == hash {
		defer pgb.lastExplorerBlock.Unlock()
		return pgb.lastExplorerBlock.blockInfo
	}
	pgb.lastExplorerBlock.Unlock()

	data := pgb.GetBlockVerboseByHash(hash, true)
	if data == nil {
		log.Error("Unable to get block for block hash " + hash)
		return nil
	}

	b := makeExplorerBlockBasic(data, pgb.chainParams)

	// Explorer Block Info
	block := &exptypes.BlockInfo{
		BlockBasic:            b,
		Confirmations:         data.Confirmations,
		StakeRoot:             data.StakeRoot,
		MerkleRoot:            data.MerkleRoot,
		Nonce:                 data.Nonce,
		VoteBits:              data.VoteBits,
		FinalState:            data.FinalState,
		PoolSize:              data.PoolSize,
		Bits:                  data.Bits,
		SBits:                 data.SBits,
		Difficulty:            data.Difficulty,
		ExtraData:             data.ExtraData,
		StakeVersion:          data.StakeVersion,
		PreviousHash:          data.PreviousHash,
		NextHash:              data.NextHash,
		StakeValidationHeight: pgb.chainParams.StakeValidationHeight,
		Subsidy:               pgb.BlockSubsidy(b.Height, b.Voters),
	}

	votes := make([]*exptypes.TrimmedTxInfo, 0, block.Voters)
	revocations := make([]*exptypes.TrimmedTxInfo, 0, block.Revocations)
	tickets := make([]*exptypes.TrimmedTxInfo, 0, block.FreshStake)

	sbits, _ := dcrutil.NewAmount(block.SBits) // sbits==0 for err!=nil
	ticketPrice := int64(sbits)

	for _, tx := range data.RawSTx {
		msgTx, err := txhelpers.MsgTxFromHex(tx.Hex)
		if err != nil {
			log.Errorf("Unknown transaction %s: %v", tx.Txid, err)
			return nil
		}
		switch stake.DetermineTxType(msgTx) {
		case stake.TxTypeSSGen:
			stx := trimmedTxInfoFromMsgTx(tx, ticketPrice, msgTx, pgb.chainParams)
			// Fees for votes should be zero, but if the transaction was created
			// with unmatched inputs/outputs then the remainder becomes a fee.
			// Account for this possibility by calculating the fee for votes as
			// well.
			if stx.Fee > 0 {
				log.Tracef("Vote with fee! %v, %v DCR", stx.Fee, stx.Fees)
			}
			votes = append(votes, stx)
		case stake.TxTypeSStx:
			stx := trimmedTxInfoFromMsgTx(tx, ticketPrice, msgTx, pgb.chainParams)
			tickets = append(tickets, stx)
		case stake.TxTypeSSRtx:
			stx := trimmedTxInfoFromMsgTx(tx, ticketPrice, msgTx, pgb.chainParams)
			revocations = append(revocations, stx)
		}
	}

	var totalMixed int64

	txs := make([]*exptypes.TrimmedTxInfo, 0, block.Transactions)
	for _, tx := range data.RawTx {
		msgTx, err := txhelpers.MsgTxFromHex(tx.Hex)
		if err != nil {
			log.Errorf("Unknown transaction %s: %v", tx.Txid, err)
			return nil
		}

		exptx := trimmedTxInfoFromMsgTx(tx, ticketPrice, msgTx, pgb.chainParams)
		for _, vin := range tx.Vin {
			if vin.IsCoinBase() {
				exptx.Fee, exptx.FeeRate, exptx.Fees = 0.0, 0.0, 0.0
			}
		}
		txs = append(txs, exptx)
		totalMixed += int64(exptx.MixCount) * exptx.MixDenom
	}

	block.Tx = txs
	block.Votes = votes
	block.Revs = revocations
	block.Tickets = tickets
	block.TotalMixed = totalMixed

	sortTx := func(txs []*exptypes.TrimmedTxInfo) {
		sort.Slice(txs, func(i, j int) bool {
			return txs[i].Total > txs[j].Total
		})
	}

	sortTx(block.Tx)
	sortTx(block.Votes)
	sortTx(block.Revs)
	sortTx(block.Tickets)

	getTotalFee := func(txs []*exptypes.TrimmedTxInfo) (total dcrutil.Amount) {
		for _, tx := range txs {
			// Coinbase transactions have no fee. The fee should be zero already
			// (as in makeExplorerTxBasic), but intercept coinbase just in case.
			// Note that this does not include stakebase transactions (votes),
			// which can have a fee but are not required to.
			if tx.Coinbase {
				continue
			}
			if tx.Fee < 0 {
				log.Warnf("Negative fees should not happen! %v", tx.Fee)
			}
			total += tx.Fee
		}
		return
	}
	getTotalSent := func(txs []*exptypes.TrimmedTxInfo) (total dcrutil.Amount) {
		for _, tx := range txs {
			amt, err := dcrutil.NewAmount(tx.Total)
			if err != nil {
				continue
			}
			total += amt
		}
		return
	}
	block.TotalSent = (getTotalSent(block.Tx) + getTotalSent(block.Revs) +
		getTotalSent(block.Tickets) + getTotalSent(block.Votes)).ToCoin()
	block.MiningFee = (getTotalFee(block.Tx) + getTotalFee(block.Revs) +
		getTotalFee(block.Tickets) + getTotalFee(block.Votes)).ToCoin()

	pgb.lastExplorerBlock.Lock()
	pgb.lastExplorerBlock.hash = hash
	pgb.lastExplorerBlock.blockInfo = block
	pgb.lastExplorerBlock.difficulties = make(map[int64]float64) // used by the Difficulty method
	pgb.lastExplorerBlock.Unlock()

	return block
}

// GetExplorerBlocks creates an slice of exptypes.BlockBasic beginning at start
// and decreasing in block height to end, not including end.
func (pgb *ChainDB) GetExplorerBlocks(start int, end int) []*exptypes.BlockBasic {
	if start < end {
		return nil
	}
	summaries := make([]*exptypes.BlockBasic, 0, start-end)
	for i := start; i > end; i-- {
		data := pgb.GetBlockVerbose(i, true)
		block := new(exptypes.BlockBasic)
		if data != nil {
			block = makeExplorerBlockBasic(data, pgb.chainParams)
		}
		summaries = append(summaries, block)
	}
	return summaries
}

// txWithTicketPrice is a way to perform getrawtransaction and if the
// transaction is unconfirmed, getstakedifficulty, while the chain server's best
// block remains unchanged. If the transaction is confirmed, the ticket price is
// queryied from ChainDB's database. This is an ugly solution to atomic RPCs.
func (pgb *ChainDB) txWithTicketPrice(txhash *chainhash.Hash) (*chainjson.TxRawResult, int64, error) {
	// If the transaction is unconfirmed, the RPC client must provide the ticket
	// price. Ensure the best block does not change between calls to
	// getrawtransaction and getstakedifficulty.
	blockHash, _, err := pgb.Client.GetBestBlock()
	if err != nil {
		return nil, 0, fmt.Errorf("GetBestBlock failed: %v", err)
	}

	var txraw *chainjson.TxRawResult
	var ticketPrice int64
	for {
		txraw, err = pgb.Client.GetRawTransactionVerbose(txhash)
		if err != nil {
			return nil, 0, fmt.Errorf("GetRawTransactionVerbose failed for %v: %v", txhash, err)
		}

		if txraw.Confirmations > 0 {
			return txraw, pgb.GetSBitsByHash(txraw.BlockHash), nil
		}

		sdiffRes, err := pgb.Client.GetStakeDifficulty()
		if err != nil {
			return nil, 0, fmt.Errorf("GetStakeDifficulty failed: %v", err)
		}

		blockHash1, _, err := pgb.Client.GetBestBlock()
		if err != nil {
			return nil, 0, fmt.Errorf("GetBestBlock failed: %v", err)
		}

		sdiff, _ := dcrutil.NewAmount(sdiffRes.CurrentStakeDifficulty) // sdiff==0 for err !=nil
		ticketPrice = int64(sdiff)

		if blockHash.IsEqual(blockHash1) {
			break
		}
		blockHash = blockHash1 // try again
	}

	return txraw, ticketPrice, nil
}

// GetExplorerTx creates a *exptypes.TxInfo for the transaction with the given
// ID.
func (pgb *ChainDB) GetExplorerTx(txid string) *exptypes.TxInfo {
	txhash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		log.Errorf("Invalid transaction hash %s", txid)
		return nil
	}

	txraw, ticketPrice, err := pgb.txWithTicketPrice(txhash)
	if err != nil {
		log.Errorf("txWithTicketPrice: %v", err)
		return nil
	}

	msgTx, err := txhelpers.MsgTxFromHex(txraw.Hex)
	if err != nil {
		log.Errorf("Cannot create MsgTx for tx %v: %v", txhash, err)
		return nil
	}

	txBasic := makeExplorerTxBasic(*txraw, ticketPrice, msgTx, pgb.chainParams)
	tx := &exptypes.TxInfo{
		TxBasic:       txBasic,
		Type:          txhelpers.DetermineTxTypeString(msgTx),
		BlockHeight:   txraw.BlockHeight,
		BlockIndex:    txraw.BlockIndex,
		BlockHash:     txraw.BlockHash,
		Confirmations: txraw.Confirmations,
		Time:          exptypes.NewTimeDefFromUNIX(txraw.Time),
	}

	inputs := make([]exptypes.Vin, 0, len(txraw.Vin))
	for i, vin := range txraw.Vin {
		// The addresses are may only be obtained by decoding the previous
		// output's pkscript.
		var addresses []string
		// The vin amount is now correct in most cases, but get it from the
		// previous output anyway and compare the values for information.
		valueIn, _ := dcrutil.NewAmount(vin.AmountIn)
		// Do not attempt to look up prevout if it is a coinbase or stakebase
		// input, which does not spend a previous output.
		if !(vin.IsCoinBase() || (vin.IsStakeBase() && i == 0)) {
			// Store the vin amount for comparison.
			valueIn0 := valueIn

			addresses, valueIn, err = txhelpers.OutPointAddresses(
				&msgTx.TxIn[i].PreviousOutPoint, pgb.Client, pgb.chainParams)
			if err != nil {
				log.Warnf("Failed to get outpoint address from txid: %v", err)
				continue
			}
			// See if getrawtransaction had correct vin amounts. It should
			// except for votes on side chain blocks.
			if valueIn != valueIn0 {
				log.Debugf("vin amount in: prevout RPC = %v, vin's amount = %v",
					valueIn, valueIn0)
			}
		}

		// For mempool transactions where the vin block height is not set
		// (height 0 for an input that is not a coinbase or stakebase),
		// determine the height at which the input was generated via RPC.
		if tx.BlockHeight == 0 && vin.BlockHeight == 0 &&
			!txhelpers.IsZeroHashStr(vin.Txid) {
			vinHash, err := chainhash.NewHashFromStr(vin.Txid)
			if err != nil {
				log.Errorf("Failed to translate hash from string: %s", vin.Txid)
			} else {
				prevTx, err := pgb.Client.GetRawTransactionVerbose(vinHash)
				if err == nil {
					vin.BlockHeight = uint32(prevTx.BlockHeight)
				} else {
					log.Errorf("Error getting data for previous outpoint of mempool transaction: %v", err)
				}
			}
		}

		// Assemble and append this vin.
		coinIn := valueIn.ToCoin()
		inputs = append(inputs, exptypes.Vin{
			Vin: &chainjson.Vin{
				Txid:        vin.Txid,
				Coinbase:    vin.Coinbase,
				Stakebase:   vin.Stakebase,
				Vout:        vin.Vout,
				AmountIn:    coinIn,
				BlockHeight: vin.BlockHeight,
			},
			Addresses:       addresses,
			FormattedAmount: humanize.Commaf(coinIn),
			Index:           uint32(i),
		})
	}
	tx.Vin = inputs

	if tx.Vin[0].IsCoinBase() {
		tx.Type = exptypes.CoinbaseTypeStr
	}
	if tx.Type == exptypes.CoinbaseTypeStr || tx.IsRevocation() {
		if tx.Confirmations < int64(pgb.chainParams.CoinbaseMaturity) {
			tx.Mature = "False"
		} else {
			tx.Mature = "True"
		}
		tx.Maturity = int64(pgb.chainParams.CoinbaseMaturity)
	}
	if tx.IsVote() || tx.IsTicket() {
		if tx.Confirmations > 0 && pgb.Height() >=
			(int64(pgb.chainParams.TicketMaturity)+tx.BlockHeight) {
			tx.Mature = "True"
		} else {
			tx.Mature = "False"
			tx.TicketInfo.TicketMaturity = int64(pgb.chainParams.TicketMaturity)
		}
	}
	if tx.IsVote() {
		if tx.Confirmations < int64(pgb.chainParams.CoinbaseMaturity) {
			tx.VoteFundsLocked = "True"
		} else {
			tx.VoteFundsLocked = "False"
		}
		tx.Maturity = int64(pgb.chainParams.CoinbaseMaturity) + 1 // Add one to reflect < instead of <=
	}

	CoinbaseMaturityInHours := (pgb.chainParams.TargetTimePerBlock.Hours() * float64(pgb.chainParams.CoinbaseMaturity))
	tx.MaturityTimeTill = ((float64(pgb.chainParams.CoinbaseMaturity) -
		float64(tx.Confirmations)) / float64(pgb.chainParams.CoinbaseMaturity)) * CoinbaseMaturityInHours

	outputs := make([]exptypes.Vout, 0, len(txraw.Vout))
	for i, vout := range txraw.Vout {
		txout, err := pgb.Client.GetTxOut(txhash, uint32(i), true)
		if err != nil {
			log.Warnf("Failed to determine if tx out is spent for output %d of tx %s", i, txid)
		}
		var opReturn string
		if strings.Contains(vout.ScriptPubKey.Asm, "OP_RETURN") {
			opReturn = vout.ScriptPubKey.Asm
		}
		outputs = append(outputs, exptypes.Vout{
			Addresses:       vout.ScriptPubKey.Addresses,
			Amount:          vout.Value,
			FormattedAmount: humanize.Commaf(vout.Value),
			OP_RETURN:       opReturn,
			Type:            vout.ScriptPubKey.Type,
			Spent:           txout == nil,
			Index:           vout.N,
		})
	}
	tx.Vout = outputs

	// Initialize the spending transaction slice for safety.
	tx.SpendingTxns = make([]exptypes.TxInID, len(outputs))

	return tx
}

func makeExplorerAddressTx(data *chainjson.SearchRawTransactionsResult, address string) *dbtypes.AddressTx {
	tx := new(dbtypes.AddressTx)
	tx.TxID = data.Txid
	tx.FormattedSize = humanize.Bytes(uint64(len(data.Hex) / 2))
	tx.Total = txhelpers.TotalVout(data.Vout).ToCoin()
	tx.Time = dbtypes.NewTimeDefFromUNIX(data.Time)
	tx.Confirmations = data.Confirmations

	msgTx, err := txhelpers.MsgTxFromHex(data.Hex)
	if err == nil {
		tx.TxType = txhelpers.DetermineTxTypeString(msgTx)
	} else {
		log.Warn("makeExplorerAddressTx cannot get tx type", err)
	}

	for i := range data.Vin {
		if data.Vin[i].PrevOut != nil && len(data.Vin[i].PrevOut.Addresses) > 0 {
			if data.Vin[i].PrevOut.Addresses[0] == address {
				tx.SentTotal += *data.Vin[i].AmountIn
			}
		}
	}
	for i := range data.Vout {
		if len(data.Vout[i].ScriptPubKey.Addresses) != 0 {
			if data.Vout[i].ScriptPubKey.Addresses[0] == address {
				tx.ReceivedTotal += data.Vout[i].Value
			}
		}
	}
	return tx
}

// MaxAddressRows is an upper limit on the number of rows that may be
// requested with the searchrawtransactions RPC.
const MaxAddressRows int64 = 1000

// GetExplorerAddress fetches a *dbtypes.AddressInfo for the given address.
// Also returns the txhelpers.AddressType.
func (pgb *ChainDB) GetExplorerAddress(address string, count, offset int64) (*dbtypes.AddressInfo, txhelpers.AddressType, txhelpers.AddressError) {
	// Validate the address.
	addr, addrType, addrErr := txhelpers.AddressValidation(address, pgb.chainParams)
	netName := pgb.chainParams.Net.String()
	switch addrErr {
	case txhelpers.AddressErrorNoError:
		// All good!
	case txhelpers.AddressErrorZeroAddress:
		// Short circuit the transaction and balance queries if the provided
		// address is the zero pubkey hash address commonly used for zero
		// value sstxchange-tagged outputs.
		return &dbtypes.AddressInfo{
			Address:         address,
			Net:             netName,
			IsDummyAddress:  true,
			Balance:         new(dbtypes.AddressBalance),
			UnconfirmedTxns: new(dbtypes.AddressTransactions),
			Limit:           count,
			Offset:          offset,
		}, addrType, nil
	case txhelpers.AddressErrorWrongNet:
		// Set the net name field so a user can be properly directed.
		return &dbtypes.AddressInfo{
			Address: address,
			Net:     netName,
		}, addrType, addrErr
	default:
		return nil, addrType, addrErr
	}

	txs, err := pgb.Client.SearchRawTransactionsVerbose(addr,
		int(offset), int(MaxAddressRows), true, true, nil)
	if err != nil {
		if err.Error() == "-32603: No Txns available" {
			log.Tracef("GetExplorerAddress: No transactions found for address %s: %v", addr, err)
			return &dbtypes.AddressInfo{
				Address:    address,
				Net:        netName,
				MaxTxLimit: MaxAddressRows,
				Limit:      count,
				Offset:     offset,
			}, addrType, nil
		}
		log.Warnf("GetExplorerAddress: SearchRawTransactionsVerbose failed for address %s: %v", addr, err)
		return nil, addrType, txhelpers.AddressErrorUnknown
	}

	addressTxs := make([]*dbtypes.AddressTx, 0, len(txs))
	for i, tx := range txs {
		if int64(i) == count { // count >= len(txs)
			break
		}
		addressTxs = append(addressTxs, makeExplorerAddressTx(tx, address))
	}

	var numUnconfirmed, numReceiving, numSpending int64
	var totalreceived, totalsent dcrutil.Amount

	for _, tx := range txs {
		if tx.Confirmations == 0 {
			numUnconfirmed++
		}
		for _, y := range tx.Vout {
			if len(y.ScriptPubKey.Addresses) != 0 {
				if address == y.ScriptPubKey.Addresses[0] {
					t, _ := dcrutil.NewAmount(y.Value)
					if t > 0 {
						totalreceived += t
					}
					numReceiving++
				}
			}
		}
		for _, u := range tx.Vin {
			if u.PrevOut != nil && len(u.PrevOut.Addresses) != 0 {
				if address == u.PrevOut.Addresses[0] {
					t, _ := dcrutil.NewAmount(*u.AmountIn)
					if t > 0 {
						totalsent += t
					}
					numSpending++
				}
			}
		}
	}

	numTxns, numberMaxOfTx := count, int64(len(txs))
	if numTxns > numberMaxOfTx {
		numTxns = numberMaxOfTx
	}
	balance := &dbtypes.AddressBalance{
		Address:      address,
		NumSpent:     numSpending,
		NumUnspent:   numReceiving,
		TotalSpent:   int64(totalsent),
		TotalUnspent: int64(totalreceived - totalsent),
	}
	addrData := &dbtypes.AddressInfo{
		Address:           address,
		Net:               netName,
		MaxTxLimit:        MaxAddressRows,
		Limit:             count,
		Offset:            offset,
		NumUnconfirmed:    numUnconfirmed,
		Transactions:      addressTxs,
		NumTransactions:   numTxns,
		NumFundingTxns:    numReceiving,
		NumSpendingTxns:   numSpending,
		AmountReceived:    totalreceived,
		AmountSent:        totalsent,
		AmountUnspent:     totalreceived - totalsent,
		Balance:           balance,
		KnownTransactions: numberMaxOfTx,
		KnownFundingTxns:  numReceiving,
		KnownSpendingTxns: numSpending,
	}

	// Sort by date and calculate block height.
	addrData.PostProcess(uint32(pgb.Height()))

	return addrData, addrType, nil
}

// GetTip grabs the highest block stored in the database.
func (pgb *ChainDB) GetTip() (*exptypes.WebBasicBlock, error) {
	tip, err := pgb.getTip()
	if err != nil {
		return nil, err
	}
	blockdata := exptypes.WebBasicBlock{
		Height:      tip.Height,
		Size:        tip.Size,
		Hash:        tip.Hash,
		Difficulty:  tip.Difficulty,
		StakeDiff:   tip.StakeDiff,
		Time:        tip.Time.S.UNIX(),
		NumTx:       tip.NumTx,
		PoolSize:    tip.PoolInfo.Size,
		PoolValue:   tip.PoolInfo.Value,
		PoolValAvg:  tip.PoolInfo.ValAvg,
		PoolWinners: tip.PoolInfo.Winners,
	}
	return &blockdata, nil
}

// getTip returns the last block stored using StoreBlockSummary.
// If no block has been stored yet, it returns the best block in the database.
func (pgb *ChainDB) getTip() (*apitypes.BlockDataBasic, error) {
	pgb.tipMtx.Lock()
	defer pgb.tipMtx.Unlock()
	if pgb.tipSummary != nil && pgb.tipSummary.Hash == pgb.BestBlockHashStr() {
		return pgb.tipSummary, nil
	}
	tip, err := RetrieveLatestBlockSummary(pgb.ctx, pgb.db)
	if err != nil {
		return nil, err
	}
	pgb.tipSummary = tip
	return tip, nil
}

// DecodeRawTransaction creates a *chainjson.TxRawResult from a hex-encoded
// transaction.
func (pgb *ChainDB) DecodeRawTransaction(txhex string) (*chainjson.TxRawResult, error) {
	bytes, err := hex.DecodeString(txhex)
	if err != nil {
		log.Errorf("DecodeRawTransaction failed: %v", err)
		return nil, err
	}
	tx, err := pgb.Client.DecodeRawTransaction(bytes)
	if err != nil {
		log.Errorf("DecodeRawTransaction failed: %v", err)
		return nil, err
	}
	return tx, nil
}

// TxHeight gives the block height of the transaction id specified
func (pgb *ChainDB) TxHeight(txid *chainhash.Hash) (height int64) {
	txraw, err := pgb.Client.GetRawTransactionVerbose(txid)
	if err != nil {
		log.Errorf("GetRawTransactionVerbose failed for: %v", txid)
		return 0
	}
	height = txraw.BlockHeight
	return
}

// GetExplorerFullBlocks gets the *exptypes.BlockInfo's for a range of block
// heights.
func (pgb *ChainDB) GetExplorerFullBlocks(start int, end int) []*exptypes.BlockInfo {
	if start < end {
		return nil
	}
	summaries := make([]*exptypes.BlockInfo, 0, start-end)
	for i := start; i > end; i-- {
		data := pgb.GetBlockVerbose(i, true)
		block := new(exptypes.BlockInfo)
		if data != nil {
			block = pgb.GetExplorerBlock(data.Hash)
		}
		summaries = append(summaries, block)
	}
	return summaries
}

// CurrentDifficulty returns the current difficulty from dcrd.
func (pgb *ChainDB) CurrentDifficulty() (float64, error) {
	diff, err := pgb.Client.GetDifficulty()
	if err != nil {
		log.Error("GetDifficulty failed")
		return diff, err
	}
	return diff, nil
}

// Difficulty returns the difficulty for the first block mined after the
// provided UNIX timestamp.
func (pgb *ChainDB) Difficulty(timestamp int64) float64 {
	pgb.lastExplorerBlock.Lock()
	diff, ok := pgb.lastExplorerBlock.difficulties[timestamp]
	pgb.lastExplorerBlock.Unlock()
	if ok {
		return diff
	}

	diff, err := RetrieveDiff(pgb.ctx, pgb.db, timestamp)
	if err != nil {
		log.Errorf("Unable to retrieve difficulty: %v", err)
		return -1
	}
	pgb.lastExplorerBlock.Lock()
	pgb.lastExplorerBlock.difficulties[timestamp] = diff
	pgb.lastExplorerBlock.Unlock()
	return diff
}

func (pgb *ChainDB) getRawTransactionWithHex(txid *chainhash.Hash) (tx *apitypes.Tx, hex string) {
	var err error
	tx, hex, err = rpcutils.APITransaction(pgb.Client, txid)
	if err != nil {
		log.Errorf("APITransaction failed: %v", err)
	}
	return
}

// GetMempool gets all transactions from the mempool for explorer and adds the
// total out for all the txs and vote info for the votes. The returned slice
// will be nil if the GetRawMempoolVerbose RPC fails. A zero-length non-nil
// slice is returned if there are no transactions in mempool.
func (pgb *ChainDB) GetMempool() []exptypes.MempoolTx {
	mempooltxs, err := pgb.Client.GetRawMempoolVerbose(chainjson.GRMAll)
	if err != nil {
		log.Errorf("GetRawMempoolVerbose failed: %v", err)
		return nil
	}

	txs := make([]exptypes.MempoolTx, 0, len(mempooltxs))

	for hashStr, tx := range mempooltxs {
		hash, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			continue
		}
		rawtx, hex := pgb.getRawTransactionWithHex(hash)
		total := 0.0
		if rawtx == nil {
			continue
		}
		for _, v := range rawtx.Vout {
			total += v.Value
		}
		msgTx, err := txhelpers.MsgTxFromHex(hex)
		if err != nil {
			continue
		}
		var voteInfo *exptypes.VoteInfo

		if ok := stake.IsSSGen(msgTx); ok {
			validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(msgTx, pgb.chainParams)
			if err != nil {
				log.Debugf("Cannot get vote choices for %s", hash)
			} else {
				voteInfo = &exptypes.VoteInfo{
					Validation: exptypes.BlockValidation{
						Hash:     validation.Hash.String(),
						Height:   validation.Height,
						Validity: validation.Validity,
					},
					Version:     version,
					Bits:        bits,
					Choices:     choices,
					TicketSpent: msgTx.TxIn[1].PreviousOutPoint.Hash.String(),
				}
			}
		}

		fee, feeRate := txhelpers.TxFeeRate(msgTx)

		txs = append(txs, exptypes.MempoolTx{
			TxID:     msgTx.TxHash().String(),
			Fees:     fee.ToCoin(),
			FeeRate:  feeRate.ToCoin(),
			Hash:     hashStr,
			Time:     tx.Time,
			Size:     tx.Size,
			TotalOut: total,
			Type:     txhelpers.DetermineTxTypeString(msgTx),
			VoteInfo: voteInfo,
			Vin:      exptypes.MsgTxMempoolInputs(msgTx),
		})
	}

	return txs
}

// BlockchainInfo retrieves the result of the getblockchaininfo node RPC.
func (pgb *ChainDB) BlockchainInfo() (*chainjson.GetBlockChainInfoResult, error) {
	return pgb.Client.GetBlockChainInfo()
}

// UpdateChan creates a channel that will receive height updates. All calls to
// UpdateChan should be completed before blocks start being connected.
func (pgb *ChainDB) UpdateChan() chan uint32 {
	c := make(chan uint32)
	pgb.heightClients = append(pgb.heightClients, c)
	return c
}

// SignalHeight signals the database height to any registered receivers.
// This function is exported so that it can be called once externally after all
// update channel clients have subscribed.
func (pgb *ChainDB) SignalHeight(height uint32) {
	for i, c := range pgb.heightClients {
		select {
		case c <- height:
		case <-time.NewTimer(time.Minute).C:
			log.Criticalf("(*DBDataSaver).SignalHeight: heightClients[%d] timed out. Forcing a shutdown.", i)
			pgb.shutdownDcrdata()
		}
	}
}

func (pgb *ChainDB) MixedUtxosByHeight() (heights, utxoCountReg, utxoValueReg, utxoCountStk, utxoValueStk []int64, err error) {
	var rows *sql.Rows
	rows, err = pgb.db.Query(internal.SelectMixedVouts)
	if err != nil {
		return
	}
	defer rows.Close()

	var vals, fundHeights, spendHeights []int64
	var trees []uint8

	var maxHeight int64
	minHeight := int64(math.MaxInt64)
	for rows.Next() {
		var id, value, fundHeight, spendHeight int64
		var spendHeightNull sql.NullInt64
		var tree uint8
		err = rows.Scan(&id, &value, &fundHeight, &spendHeightNull, &tree)
		if err != nil {
			return
		}
		vals = append(vals, value)
		fundHeights = append(fundHeights, fundHeight)
		trees = append(trees, tree)
		if spendHeightNull.Valid {
			spendHeight = spendHeightNull.Int64
		} else {
			spendHeight = -1
		}
		spendHeights = append(spendHeights, spendHeight)
		if fundHeight < minHeight {
			minHeight = fundHeight
		}
		if spendHeight > maxHeight {
			maxHeight = spendHeight
		}
	}

	N := maxHeight - minHeight + 1
	heights = make([]int64, N)
	utxoCountReg = make([]int64, N)
	utxoValueReg = make([]int64, N)
	utxoCountStk = make([]int64, N)
	utxoValueStk = make([]int64, N)

	for h := minHeight; h <= maxHeight; h++ {
		i := h - minHeight
		heights[i] = h
		for iu := range vals {
			if h >= fundHeights[iu] && (h < spendHeights[iu] || spendHeights[iu] == -1) {
				if trees[iu] == 0 {
					utxoCountReg[i]++
					utxoValueReg[i] += vals[iu]
				} else {
					utxoCountStk[i]++
					utxoValueStk[i] += vals[iu]
				}
			}
		}
	}

	err = rows.Err()
	return

}
