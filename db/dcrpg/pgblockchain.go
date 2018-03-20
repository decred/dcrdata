// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/blockdata"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/explorer"
	"github.com/decred/dcrdata/stakedb"
	humanize "github.com/dustin/go-humanize"
)

var (
	zeroHash            = chainhash.Hash{}
	zeroHashStringBytes = []byte(chainhash.Hash{}.String())
)

// ChainDB provides an interface for storing and manipulating extracted
// blockchain data in a PostgreSQL database.
type ChainDB struct {
	db                 *sql.DB
	chainParams        *chaincfg.Params
	devAddress         string
	dupChecks          bool
	bestBlock          int64
	lastBlock          map[chainhash.Hash]uint64
	addressCounts      *addressCounter
	stakeDB            *stakedb.StakeDatabase
	unspentTicketCache *TicketTxnIDGetter
}

// ChainDBRC provides an interface for storing and manipulating extracted
// and includes the RPC Client blockchain data in a PostgreSQL database.
type ChainDBRPC struct {
	ChainDB *ChainDB
	Client  *rpcclient.Client
}

type addressCounter struct {
	sync.Mutex
	validHeight int64
	balance     map[string]explorer.AddressBalance
}

func makeAddressCounter() *addressCounter {
	return &addressCounter{
		validHeight: 0,
		balance:     make(map[string]explorer.AddressBalance),
	}
}

// TicketTxnIDGetter provides a cache for DB row IDs of tickets.
type TicketTxnIDGetter struct {
	sync.RWMutex
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
	t.RLock()
	dbID, ok := t.idCache[txid]
	t.RUnlock()
	if ok {
		if expire {
			t.Lock()
			delete(t.idCache, txid)
			t.Unlock()
		}
		return dbID, nil
	}
	return RetrieveTicketIDByHash(t.db, txid)
}

// Set stores the (transaction hash, DB row ID) pair a map for future access.
func (t *TicketTxnIDGetter) Set(txid string, txDbID uint64) {
	if t == nil {
		return
	}
	t.Lock()
	defer t.Unlock()
	t.idCache[txid] = txDbID
}

// SetN stores several (transaction hash, DB row ID) pairs in the map.
func (t *TicketTxnIDGetter) SetN(txid []string, txDbID []uint64) {
	if t == nil {
		return
	}
	t.Lock()
	defer t.Unlock()
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
}

// NewChainDB constructs a ChainDB for the given connection and Decred network
// parameters. By default, duplicate row checks on insertion are enabled.
func NewChainDB(dbi *DBInfo, params *chaincfg.Params, stakeDB *stakedb.StakeDatabase) (*ChainDB, error) {
	// Connect to the PostgreSQL daemon and return the *sql.DB
	db, err := Connect(dbi.Host, dbi.Port, dbi.User, dbi.Pass, dbi.DBName)
	if err != nil {
		return nil, err
	}
	// Attempt to get DB best block height from tables, but if the tables are
	// empty or not yet created, it is not an error.
	bestHeight, _, _, err := RetrieveBestBlockHeight(db)
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
	unspentTicketDbIDs, unspentTicketHashes, err := RetrieveUnspentTickets(db)
	if err != nil && err != sql.ErrNoRows && !strings.HasSuffix(err.Error(), "does not exist") {
		return nil, err
	}
	if len(unspentTicketDbIDs) != 0 {
		log.Infof("Storing data for %d unspent tickets in cache.", len(unspentTicketDbIDs))
		unspentTicketCache.SetN(unspentTicketHashes, unspentTicketDbIDs)
	}

	return &ChainDB{
		db:                 db,
		chainParams:        params,
		devAddress:         devSubsidyAddress,
		dupChecks:          true,
		bestBlock:          int64(bestHeight),
		lastBlock:          make(map[chainhash.Hash]uint64),
		addressCounts:      makeAddressCounter(),
		stakeDB:            stakeDB,
		unspentTicketCache: unspentTicketCache,
	}, nil
}

// NewChainDBRPC contains ChainDB and RPC client
// parameters. By default, duplicate row checks on insertion are enabled.
// also enables rpc client
func NewChainDBRPC(chaindb *ChainDB, cl *rpcclient.Client) (*ChainDBRPC, error) {
	// Connect to the PostgreSQL daemon and return the *sql.DB
	return &ChainDBRPC{
		chaindb,
		cl,
	}, nil
}

// Close closes the underlying sql.DB connection to the database.
func (pgb *ChainDB) Close() error {
	return pgb.db.Close()
}

// UseStakeDB is used to assign a stakedb.StakeDatabase for ticket tracking.
// This may be useful when it is necessary to construct a ChainDB prior to
// creating or loading a StakeDatabase, such as when dropping tables.
func (pgb *ChainDB) UseStakeDB(stakeDB *stakedb.StakeDatabase) {
	pgb.stakeDB = stakeDB
}

// EnableDuplicateCheckOnInsert specifies whether SQL insertions should check
// for row conflicts (duplicates), and avoid adding or updating.
func (pgb *ChainDB) EnableDuplicateCheckOnInsert(dupCheck bool) {
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
// an upgrade is required. Since there is presently no automatic upgrade, an
// error is returned when any table is not of the correct version.
func (pgb *ChainDB) VersionCheck() error {
	vers := TableVersions(pgb.db)
	for tab, ver := range vers {
		log.Debugf("Table %s: v%s", tab, ver)
	}
	if tableUpgrades := TableUpgradesRequired(vers); len(tableUpgrades) > 0 {
		for _, u := range tableUpgrades {
			log.Warnf(u.String())
		}
		return fmt.Errorf("table maintenance required")
	}
	return nil
}

// DropTables drops (deletes) all of the known dcrdata tables.
func (pgb *ChainDB) DropTables() {
	DropTables(pgb.db)
}

// HeightDB queries the DB for the best block height.
func (pgb *ChainDB) HeightDB() (uint64, error) {
	bestHeight, _, _, err := RetrieveBestBlockHeight(pgb.db)
	return bestHeight, err
}

// Height uses the last stored height.
func (pgb *ChainDB) Height() uint64 {
	return uint64(pgb.bestBlock)
}

// SpendingTransactions retrieves all transactions spending outpoints from the
// specified funding transaction. The spending transaction hashes, the spending
// tx input indexes, and the corresponding funding tx output indexes, and an
// error value are returned.
func (pgb *ChainDB) SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error) {
	_, spendingTxns, vinInds, voutInds, err := RetrieveSpendingTxsByFundingTx(pgb.db, fundingTxID)
	return spendingTxns, vinInds, voutInds, err
}

// SpendingTransaction returns the transaction that spends the specified
// transaction outpoint, if it is spent. The spending transaction hash, input
// index, tx tree, and an error value are returned.
func (pgb *ChainDB) SpendingTransaction(fundingTxID string,
	fundingTxVout uint32) (string, uint32, int8, error) {
	_, spendingTx, vinInd, tree, err := RetrieveSpendingTxByTxOut(pgb.db, fundingTxID, fundingTxVout)
	return spendingTx, vinInd, tree, err
}

// BlockTransactions retrieves all transactions in the specified block, their
// indexes in the block, their tree, and an error value.
func (pgb *ChainDB) BlockTransactions(blockHash string) ([]string, []uint32, []int8, error) {
	_, blockTransactions, blockInds, trees, err := RetrieveTxsByBlockHash(pgb.db, blockHash)
	return blockTransactions, blockInds, trees, err
}

// BlockMissedVotes retrieves the ticket IDs for all missed votes in the
// specified block, and an error value.
func (pgb *ChainDB) BlockMissedVotes(blockHash string) ([]string, error) {
	return RetrieveMissedVotesInBlock(pgb.db, blockHash)
}

// PoolStatusForTicket retrieves the specified ticket's spend status and ticket
// pool status, and an error value.
func (pgb *ChainDB) PoolStatusForTicket(txid string) (dbtypes.TicketSpendType, dbtypes.TicketPoolStatus, error) {
	_, spendType, poolStatus, err := RetrieveTicketStatusByHash(pgb.db, txid)
	return spendType, poolStatus, err
}

// VoutValue retrieves the value of the specified transaction outpoint in atoms.
func (pgb *ChainDB) VoutValue(txID string, vout uint32) (uint64, error) {
	// txDbID, _, _, err := RetrieveTxByHash(pgb.db, txID)
	// if err != nil {
	// 	return 0, fmt.Errorf("RetrieveTxByHash: %v", err)
	// }
	voutValue, err := RetrieveVoutValue(pgb.db, txID, vout)
	if err != nil {
		return 0, fmt.Errorf("RetrieveVoutValue: %v", err)
	}
	return voutValue, nil
}

// VoutValues retrieves the values of each outpoint of the specified
// transaction. The corresponding indexes in the block and tx trees of the
// outpoints, and an error value are also returned.
func (pgb *ChainDB) VoutValues(txID string) ([]uint64, []uint32, []int8, error) {
	// txDbID, _, _, err := RetrieveTxByHash(pgb.db, txID)
	// if err != nil {
	// 	return nil, fmt.Errorf("RetrieveTxByHash: %v", err)
	// }
	voutValues, txInds, txTrees, err := RetrieveVoutValues(pgb.db, txID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("RetrieveVoutValues: %v", err)
	}
	return voutValues, txInds, txTrees, nil
}

// TransactionBlock retrieves the hash of the block containing the specified
// transaction. The index of the transaction within the block, the transaction
// index, and an error value are also returned.
func (pgb *ChainDB) TransactionBlock(txID string) (string, uint32, int8, error) {
	_, blockHash, blockInd, tree, err := RetrieveTxByHash(pgb.db, txID)
	return blockHash, blockInd, tree, err
}

// AddressHistory queries the database for all rows of the addresses table for
// the given address.
func (pgb *ChainDB) AddressHistory(address string, N, offset int64) ([]*dbtypes.AddressRow, *explorer.AddressBalance, error) {

	bb, err := pgb.HeightDB()
	if err != nil {
		return nil, nil, err
	}
	bestBlock := int64(bb)

	pgb.addressCounts.Lock()
	defer pgb.addressCounts.Unlock()

	// See if address count cache includes a fresh count for this address.
	var balanceInfo explorer.AddressBalance
	var fresh bool
	if pgb.addressCounts.validHeight == bestBlock {
		balanceInfo, fresh = pgb.addressCounts.balance[address]
	} else {
		// StoreBlock should do this, but the idea is to clear the old cached
		// results when a new block is encountered.
		log.Warnf("Address receive counter stale, at block %d when best is %d.",
			pgb.addressCounts.validHeight, bestBlock)
		pgb.addressCounts.balance = make(map[string]explorer.AddressBalance)
		pgb.addressCounts.validHeight = bestBlock
	}

	// The organization address occurs very frequently, so use the regular (non
	// sub-query) select as it is much more efficient.
	var addressRows []*dbtypes.AddressRow
	if address == pgb.devAddress {
		_, addressRows, err = RetrieveAddressTxnsAlt(pgb.db, address, N, offset)
	} else {
		_, addressRows, err = RetrieveAddressTxns(pgb.db, address, N, offset)
	}
	if err != nil {
		return nil, &balanceInfo, err
	}

	// If the address receive count was not cached, store it in the cache if it
	// is worth storing (when the length of the short list returned above is no
	// less than the query limit).
	if !fresh {
		addrInfo := explorer.ReduceAddressHistory(addressRows)
		if addrInfo == nil {
			return addressRows, nil, fmt.Errorf("ReduceAddressHistory failed")
		}

		if addrInfo.NumFundingTxns < N {
			balanceInfo = explorer.AddressBalance{
				Address:      address,
				NumSpent:     addrInfo.NumSpendingTxns,
				NumUnspent:   addrInfo.NumFundingTxns - addrInfo.NumSpendingTxns,
				TotalSpent:   int64(addrInfo.TotalSent),
				TotalUnspent: int64(addrInfo.Unspent),
			}
		} else {
			var numSpent, numUnspent, totalSpent, totalUnspent int64

			numSpent, numUnspent, totalSpent, totalUnspent, err =
				RetrieveAddressSpentUnspent(pgb.db, address)

			if err != nil {
				return nil, nil, err
			}
			balanceInfo = explorer.AddressBalance{
				Address:      address,
				NumSpent:     numSpent,
				NumUnspent:   numUnspent,
				TotalSpent:   totalSpent,
				TotalUnspent: totalUnspent,
			}
		}

		log.Infof("%s: %d spent totalling %f DCR, %d unspent totalling %f DCR",
			address, balanceInfo.NumSpent, dcrutil.Amount(balanceInfo.TotalSpent).ToCoin(),
			balanceInfo.NumUnspent, dcrutil.Amount(balanceInfo.TotalUnspent).ToCoin())
		log.Infof("Caching address receive count for address %s: "+
			"count = %d at block %d.", address,
			balanceInfo.NumSpent+balanceInfo.NumUnspent, bestBlock)
		pgb.addressCounts.balance[address] = balanceInfo
	}

	return addressRows, &balanceInfo, err
}

// FillAddressTransactions is used to fill out the transaction details in an
// explorer.AddressInfo generated by explorer.ReduceAddressHistory, usually from
// the output of AddressHistory. This function also sets the number of
// unconfirmed transactions for the current best block in the database.
func (pgb *ChainDB) FillAddressTransactions(addrInfo *explorer.AddressInfo) error {
	if addrInfo == nil {
		return nil
	}

	var numUnconfirmed int64

	for _, txn := range addrInfo.Transactions {
		_, dbTx, err := RetrieveDbTxByHash(pgb.db, txn.TxID)
		if err != nil {
			return err
		}
		txn.TxID = dbTx.TxID
		txn.FormattedSize = humanize.Bytes(uint64(dbTx.Size))
		txn.Total = dcrutil.Amount(dbTx.Sent).ToCoin()
		txn.Time = dbTx.BlockTime
		if dbTx.BlockTime > 0 {
			txn.Confirmations = pgb.Height() - uint64(dbTx.BlockHeight) + 1
		} else {
			numUnconfirmed++
			txn.Confirmations = 0
		}
		txn.FormattedTime = time.Unix(dbTx.BlockTime, 0).Format("2006-01-02 15:04:05")
	}

	addrInfo.NumUnconfirmed = numUnconfirmed

	return nil
}

// Store satisfies BlockDataSaver
func (pgb *ChainDB) Store(blockData *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	if pgb == nil {
		return nil
	}
	_, _, err := pgb.StoreBlock(msgBlock, blockData.WinningTickets, true, true, true)
	return err
}

func (pgb *ChainDB) DeleteDuplicates() error {
	var err error
	// Remove duplicate vins
	log.Info("Finding and removing duplicate vins entries...")
	var numVinsRemoved int64
	if numVinsRemoved, err = pgb.DeleteDuplicateVins(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateVins failed: %v", err)
	}
	log.Infof("Removed %d duplicate vins entries.", numVinsRemoved)

	// Remove duplicate vouts
	log.Info("Finding and removing duplicate vouts entries before indexing...")
	var numVoutsRemoved int64
	if numVoutsRemoved, err = pgb.DeleteDuplicateVouts(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateVouts failed: %v", err)
	}
	log.Infof("Removed %d duplicate vouts entries.", numVoutsRemoved)

	// Remove duplicate transactions
	log.Info("Finding and removing duplicate transactions entries before indexing...")
	var numTxnsRemoved int64
	if numTxnsRemoved, err = pgb.DeleteDuplicateTxns(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateTxns failed: %v", err)
	}
	log.Infof("Removed %d duplicate transactions entries.", numTxnsRemoved)

	// TODO: remove entries from addresses table that reference removed
	// vins/vouts.

	return err
}

func (pgb *ChainDB) DeleteDuplicatesRecovery() error {
	var err error
	// Remove duplicate vins
	log.Info("Finding and removing duplicate vins entries...")
	var numVinsRemoved int64
	if numVinsRemoved, err = pgb.DeleteDuplicateVins(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateVins failed: %v", err)
	}
	log.Infof("Removed %d duplicate vins entries.", numVinsRemoved)

	// Remove duplicate vouts
	log.Info("Finding and removing duplicate vouts entries before indexing...")
	var numVoutsRemoved int64
	if numVoutsRemoved, err = pgb.DeleteDuplicateVouts(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateVouts failed: %v", err)
	}
	log.Infof("Removed %d duplicate vouts entries.", numVoutsRemoved)

	// TODO: remove entries from addresses table that reference removed
	// vins/vouts.

	// Remove duplicate transactions
	log.Info("Finding and removing duplicate transactions entries before indexing...")
	var numTxnsRemoved int64
	if numTxnsRemoved, err = pgb.DeleteDuplicateTxns(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateTxns failed: %v", err)
	}
	log.Infof("Removed %d duplicate transactions entries.", numTxnsRemoved)

	// Remove duplicate tickets
	log.Info("Finding and removing duplicate tickets entries before indexing...")
	if numTxnsRemoved, err = pgb.DeleteDuplicateTickets(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateTickets failed: %v", err)
	}
	log.Infof("Removed %d duplicate tickets entries.", numTxnsRemoved)

	// Remove duplicate votes
	log.Info("Finding and removing duplicate votes entries before indexing...")
	if numTxnsRemoved, err = pgb.DeleteDuplicateVotes(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateVotes failed: %v", err)
	}
	log.Infof("Removed %d duplicate votes entries.", numTxnsRemoved)

	// Remove duplicate misses
	log.Info("Finding and removing duplicate misses entries before indexing...")
	if numTxnsRemoved, err = pgb.DeleteDuplicateMisses(); err != nil {
		return fmt.Errorf("dcrpg.DeleteDuplicateMisses failed: %v", err)
	}
	log.Infof("Removed %d duplicate misses entries.", numTxnsRemoved)

	return err
}

func (pgb *ChainDB) DeleteDuplicateVins() (int64, error) {
	return DeleteDuplicateVins(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateVouts() (int64, error) {
	return DeleteDuplicateVouts(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateTxns() (int64, error) {
	return DeleteDuplicateTxns(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateTickets() (int64, error) {
	return DeleteDuplicateTickets(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateVotes() (int64, error) {
	return DeleteDuplicateVotes(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateMisses() (int64, error) {
	return DeleteDuplicateMisses(pgb.db)
}

// DeindexAll drops all of the indexes in all tables
func (pgb *ChainDB) DeindexAll() error {
	var err, errAny error
	if err = DeindexBlockTableOnHash(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexTransactionTableOnHashes(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexTransactionTableOnBlockIn(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexVinTableOnVins(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexVinTableOnPrevOuts(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexVoutTableOnTxHashIdx(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	// if err = DeindexVoutTableOnTxHash(pgb.db); err != nil {
	// 	warnUnlessNotExists(err)
	// 	errAny = err
	// }
	if err = DeindexAddressTableOnAddress(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexAddressTableOnVoutID(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexAddressTableOnTxHash(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = pgb.DeindexTicketsTable(); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexVotesTableOnCandidate(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexVotesTableOnHash(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexVotesTableOnVoteVersion(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err = DeindexMissesTableOnHash(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	return errAny
}

// IndexAll creates all of the indexes in all tables
func (pgb *ChainDB) IndexAll() error {
	log.Infof("Indexing blocks table...")
	if err := IndexBlockTableOnHash(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing transactions table on tx/block hashes...")
	if err := IndexTransactionTableOnHashes(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing transactions table on block id/indx...")
	if err := IndexTransactionTableOnBlockIn(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing vins table on txin...")
	if err := IndexVinTableOnVins(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing vins table on prevouts...")
	if err := IndexVinTableOnPrevOuts(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing vouts table on tx hash and index...")
	if err := IndexVoutTableOnTxHashIdx(pgb.db); err != nil {
		return err
	}
	// log.Infof("Indexing vouts table on tx hash...")
	// if err := IndexVoutTableOnTxHash(pgb.db); err != nil {
	// 	return err
	// }
	log.Infof("Indexing votes table on candidate block...")
	if err := IndexVotesTableOnCandidate(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing votes table on vote hash...")
	if err := IndexVotesTableOnHashes(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing votes table on vote version...")
	if err := IndexVotesTableOnVoteVersion(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing misses table...")
	if err := IndexMissesTableOnHashes(pgb.db); err != nil {
		return err
	}
	// Not indexing the address table on vout ID or address here. See
	// IndexAddressTable to create those indexes.
	log.Infof("Indexing addresses table on funding tx hash...")
	return IndexAddressTableOnTxHash(pgb.db)
}

// IndexTicketsTable creates the indexes on the tickets table on ticket hash and
// tx DB ID columns, separately.
func (pgb *ChainDB) IndexTicketsTable() error {
	log.Infof("Indexing tickets table on ticket hash...")
	if err := IndexTicketsTableOnHashes(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing tickets table on transaction Db ID...")
	return IndexTicketsTableOnTxDbID(pgb.db)
}

// DeindexTicketsTable drops the ticket hash and tx DB ID column indexes for the
// tickets table.
func (pgb *ChainDB) DeindexTicketsTable() error {
	var errAny error
	if err := DeindexTicketsTableOnHash(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err := DeindexTicketsTableOnTxDbID(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	return errAny
}

func warnUnlessNotExists(err error) {
	if !strings.Contains(err.Error(), "does not exist") {
		log.Warn(err)
	}
}

// IndexAddressTable creates the indexes on the address table on the vout ID and
// address columns, separately.
func (pgb *ChainDB) IndexAddressTable() error {
	log.Infof("Indexing addresses table on address...")
	if err := IndexAddressTableOnAddress(pgb.db); err != nil {
		return err
	}
	log.Infof("Indexing addresses table on vout Db ID...")
	return IndexAddressTableOnVoutID(pgb.db)
}

// DeindexAddressTable drops the vin ID and address column indexes for the
// address table.
func (pgb *ChainDB) DeindexAddressTable() error {
	var errAny error
	if err := DeindexAddressTableOnAddress(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	if err := DeindexAddressTableOnVoutID(pgb.db); err != nil {
		warnUnlessNotExists(err)
		errAny = err
	}
	return errAny
}

func (pgb *ChainDB) ExistsIndexVinOnVins() (bool, error) {
	return ExistsIndex(pgb.db, "uix_vin")
}

func (pgb *ChainDB) ExistsIndexVoutOnTxHashIdx() (bool, error) {
	return ExistsIndex(pgb.db, "uix_vout_txhash_ind")
}

func (pgb *ChainDB) ExistsIndexAddressesVoutIDAddress() (bool, error) {
	return ExistsIndex(pgb.db, "uix_addresses_vout_id")
}

// StoreBlock processes the input wire.MsgBlock, and saves to the data tables.
// The number of vins, and vouts stored are also returned.
func (pgb *ChainDB) StoreBlock(msgBlock *wire.MsgBlock, winningTickets []string,
	isValid, updateAddressesSpendingInfo, updateTicketsSpendingInfo bool) (numVins int64, numVouts int64, err error) {
	// Convert the wire.MsgBlock to a dbtypes.Block
	dbBlock := dbtypes.MsgBlockToDBBlock(msgBlock, pgb.chainParams)

	// Get the previous winners (stake DB pool info cache have this info)
	prevBlockHash := msgBlock.Header.PrevBlock
	var winners []string
	if !bytes.Equal(zeroHash[:], prevBlockHash[:]) {
		tpi, found := pgb.stakeDB.PoolInfo(prevBlockHash)
		if !found {
			err = fmt.Errorf("stakedb.PoolInfo failed for block %s", msgBlock.BlockHash())
			return
		}
		winners = tpi.Winners
	}

	// Wrap the message block
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
		resChanReg <- pgb.storeTxns(MsgBlockPG, wire.TxTreeRegular,
			pgb.chainParams, &dbBlock.TxDbIDs, updateAddressesSpendingInfo,
			updateTicketsSpendingInfo)
	}()

	// stake transactions
	resChanStake := make(chan storeTxnsResult)
	go func() {
		resChanStake <- pgb.storeTxns(MsgBlockPG, wire.TxTreeStake,
			pgb.chainParams, &dbBlock.STxDbIDs, updateAddressesSpendingInfo,
			updateTicketsSpendingInfo)
	}()

	errReg := <-resChanReg
	errStk := <-resChanStake
	if errStk.err != nil {
		if errReg.err == nil {
			err = errStk.err
			numVins = errReg.numVins
			numVouts = errReg.numVouts
			return
		}
		err = errors.New(errReg.Error() + ", " + errStk.Error())
		return
	} else if errReg.err != nil {
		err = errReg.err
		numVins = errStk.numVins
		numVouts = errStk.numVouts
		return
	}

	numVins = errStk.numVins + errReg.numVins
	numVouts = errStk.numVouts + errReg.numVouts

	// Store the block now that it has all it's transaction PK IDs
	var blockDbID uint64
	blockDbID, err = InsertBlock(pgb.db, dbBlock, isValid, pgb.dupChecks)
	if err != nil {
		log.Error("InsertBlock:", err)
		return
	}
	pgb.lastBlock[msgBlock.BlockHash()] = blockDbID

	pgb.bestBlock = int64(dbBlock.Height)

	err = InsertBlockPrevNext(pgb.db, blockDbID, dbBlock.Hash,
		dbBlock.PreviousHash, "")
	if err != nil && err != sql.ErrNoRows {
		log.Error("InsertBlockPrevNext:", err)
		return
	}

	// Update last block in db with this block's hash as it's next. Also update
	// isValid flag in last block if votes in this block invalidated it.
	lastBlockHash := msgBlock.Header.PrevBlock
	lastBlockDbID, ok := pgb.lastBlock[lastBlockHash]
	if ok {
		lastIsValid := dbBlock.VoteBits&1 != 0
		if !lastIsValid {
			log.Infof("Setting last block %s as INVALID", lastBlockHash)
			err = UpdateLastBlock(pgb.db, lastBlockDbID, lastIsValid)
			if err != nil {
				log.Error("UpdateLastBlock:", err)
				return
			}
		}
		err = UpdateBlockNext(pgb.db, lastBlockDbID, dbBlock.Hash)
		if err != nil {
			log.Error("UpdateBlockNext:", err)
			return
		}
	}

	pgb.addressCounts.Lock()
	pgb.addressCounts.validHeight = int64(msgBlock.Header.Height)
	pgb.addressCounts.balance = map[string]explorer.AddressBalance{}
	pgb.addressCounts.Unlock()

	return
}

// storeTxnsResult is the type of object sent back from the goroutines wrapping
// storeTxns in StoreBlock.
type storeTxnsResult struct {
	numVins, numVouts, numAddresses int64
	err                             error
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

func (pgb *ChainDB) storeTxns(msgBlock *MsgBlockPG, txTree int8,
	chainParams *chaincfg.Params, TxDbIDs *[]uint64,
	updateAddressesSpendingInfo, updateTicketsSpendingInfo bool) storeTxnsResult {
	// For the given block, transaction tree, and network, extract the
	// transactions, vins, and vouts.
	dbTransactions, dbTxVouts, dbTxVins := dbtypes.ExtractBlockTransactions(
		msgBlock.MsgBlock, txTree, chainParams)

	// The return value, containing counts of inserted vins/vouts/txns, and an
	// error value.
	var txRes storeTxnsResult

	// dbAddressRows contains the data added to the address table, arranged as
	// [tx_i][addr_j], transactions paying to different numbers of addresses.
	dbAddressRows := make([][]dbtypes.AddressRow, len(dbTransactions))
	var totalAddressRows int

	var err error
	for it, dbtx := range dbTransactions {
		// Insert vouts, and collect rows to add to address table
		dbtx.VoutDbIds, dbAddressRows[it], err = InsertVouts(pgb.db, dbTxVouts[it], pgb.dupChecks)
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertVouts:", err)
			txRes.err = err
			return txRes
		}
		totalAddressRows += len(dbAddressRows[it])
		txRes.numVouts += int64(len(dbtx.VoutDbIds))
		if err == sql.ErrNoRows || len(dbTxVouts[it]) != len(dbtx.VoutDbIds) {
			log.Warnf("Incomplete Vout insert.")
		}

		// Insert vins
		dbtx.VinDbIds, err = InsertVins(pgb.db, dbTxVins[it])
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertVins:", err)
			txRes.err = err
			return txRes
		}
		txRes.numVins += int64(len(dbtx.VinDbIds))

		// return the transactions vout slice if processing stake tree
		if txTree == wire.TxTreeStake {
			dbtx.Vouts = dbTxVouts[it]
		}
	}

	// Get the tx PK IDs for storage in the blocks, tickets, and votes table
	*TxDbIDs, err = InsertTxns(pgb.db, dbTransactions, pgb.dupChecks)
	if err != nil && err != sql.ErrNoRows {
		log.Error("InsertTxns:", err)
		txRes.err = err
		return txRes
	}

	// If processing stake tree, insert tickets, votes, misses
	if txTree == wire.TxTreeStake {
		// Tickets
		newTicketDbIDs, newTicketTx, err := InsertTickets(pgb.db, dbTransactions, *TxDbIDs, pgb.dupChecks)
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertTickets:", err)
			txRes.err = err
			return txRes
		}

		// Get tickets table row IDs for newly spent tickets, if we are updating
		// them as we go as opposed to batch mode at the end of a sync.
		var unspentTicketCache *TicketTxnIDGetter
		if updateTicketsSpendingInfo {
			for it, tdbid := range newTicketDbIDs {
				pgb.unspentTicketCache.Set(newTicketTx[it].TxID, tdbid)
			}
			unspentTicketCache = pgb.unspentTicketCache
		}

		// Get information for transactions spending tickets (votes and
		// revokes), and the ticket DB row IDs themselves.
		spendingTxDbIDs, spendTypes, spentTicketHashes, ticketDbIDs, err :=
			pgb.CollectTicketSpendDBInfo(dbTransactions, *TxDbIDs, msgBlock.MsgBlock)
		if err != nil {
			log.Error("CollectTicketSpendDBInfo:", err)
			txRes.err = err
			return txRes
		}

		// Votes
		// voteDbIDs, voteTxns, spentTicketHashes, ticketDbIDs, missDbIDs, err := ...
		var missesHashIDs map[string]uint64
		_, _, _, _, missesHashIDs, err = InsertVotes(pgb.db,
			dbTransactions, *TxDbIDs, unspentTicketCache, msgBlock, pgb.dupChecks)
		if err != nil && err != sql.ErrNoRows {
			log.Error("InsertVotes:", err)
			txRes.err = err
			return txRes
		}

		if updateTicketsSpendingInfo {
			// Get a consistent view of the stake node at its present height
			pgb.stakeDB.LockStakeNode()

			// To update spending info in tickets table, get the spent tickets' DB
			// row IDs and block heights.
			//ticketDbIDs := make([]uint64, len(spentTicketHashes))
			revokes := make(map[string]uint64)
			blockHeights := make([]int64, len(spentTicketHashes))
			poolStatuses := make([]dbtypes.TicketPoolStatus, len(spentTicketHashes))
			for iv := range spentTicketHashes {
				blockHeights[iv] = int64(msgBlock.Header.Height) /* voteDbTxns[iv].BlockHeight */

				switch spendTypes[iv] {
				case dbtypes.TicketVoted:
					poolStatuses[iv] = dbtypes.PoolStatusVoted
				case dbtypes.TicketRevoked:
					revokes[spentTicketHashes[iv]] = ticketDbIDs[iv]
					// Revoke reason
					h, err0 := chainhash.NewHashFromStr(spentTicketHashes[iv])
					if err0 != nil {
						log.Warnf("Invalid hash %v", spentTicketHashes[iv])
					}
					expired := pgb.stakeDB.BestNode.ExistsExpiredTicket(*h)
					if !expired {
						poolStatuses[iv] = dbtypes.PoolStatusMissed
					} else {
						poolStatuses[iv] = dbtypes.PoolStatusExpired
					}
				}
			}

			// Update tickets table with spending info from new votes
			// _, err = SetSpendingForTickets(pgb.db, ticketDbIDs, voteDbIDs, blockHeights, spendTypes)
			_, err = SetSpendingForTickets(pgb.db, ticketDbIDs, spendingTxDbIDs,
				blockHeights, spendTypes, poolStatuses)
			if err != nil {
				log.Warn("SetSpendingForTickets:", err)
			}

			// Missed but not revoked
			//var unspentMissedTicketDbIDs []uint64
			var unspentMissedTicketHashes []string
			var missStatuses []dbtypes.TicketPoolStatus
			unspentMisses := make(map[string]struct{})
			for miss := range missesHashIDs {
				if _, ok := revokes[miss]; !ok {
					// unrevoked miss
					//unspentMissedTicketDbIDs = append(unspentMissedTicketDbIDs, missedTicketID)
					unspentMissedTicketHashes = append(unspentMissedTicketHashes, miss)
					unspentMisses[miss] = struct{}{}
					missStatuses = append(missStatuses, dbtypes.PoolStatusMissed)
				}
			}

			// Expired but not revoked
			unspentEnM := make([]string, len(unspentMissedTicketHashes))
			copy(unspentEnM, unspentMissedTicketHashes)
			unspentExpiresAndMisses := pgb.stakeDB.BestNode.MissedByBlock()
			for _, missHash := range unspentExpiresAndMisses {
				// MissedByBlock includes tickets that missed votes or expired;
				// we just want the expires, and not the revoked ones.
				if pgb.stakeDB.BestNode.ExistsExpiredTicket(missHash) {
					emHash := missHash.String()
					// Next check should not be unnecessary. Make sure not in
					// unspent misses from above and not just revoked.
					_, justMissed := unspentMisses[emHash]
					_, justRevoked := revokes[emHash]
					if !justMissed && !justRevoked {
						unspentEnM = append(unspentEnM, emHash)
						missStatuses = append(missStatuses, dbtypes.PoolStatusExpired)
					}
				}
			}

			// Release the stake node
			pgb.stakeDB.UnlockStakeNode()

			numUnrevokedMisses, err := SetPoolStatusForTicketsByHash(pgb.db, unspentEnM, missStatuses)
			if err != nil {
				log.Warn("SetPoolStatusForTickets", err)
			} else if numUnrevokedMisses > 0 {
				log.Tracef("Noted %d unrevoked newly-missed tickets.", numUnrevokedMisses)
			}
		}
	}

	// Store tx Db IDs as funding tx in AddressRows and rearrange
	dbAddressRowsFlat := make([]*dbtypes.AddressRow, 0, totalAddressRows)
	for it, txDbID := range *TxDbIDs {
		// Set the tx ID of the funding transactions
		for iv := range dbAddressRows[it] {
			// Transaction that pays to the address
			dba := &dbAddressRows[it][iv]
			dba.FundingTxDbID = txDbID
			// Funding tx hash, vout id, value, and address are already assigned
			// by InsertVouts. Only the funding tx DB ID was needed.
			dbAddressRowsFlat = append(dbAddressRowsFlat, dba)
		}
	}

	// Insert each new AddressRow, absent spending fields
	_, err = InsertAddressOuts(pgb.db, dbAddressRowsFlat, pgb.dupChecks)
	if err != nil {
		log.Error("InsertAddressOuts:", err)
		txRes.err = err
		return txRes
	}

	if !updateAddressesSpendingInfo {
		return txRes
	}

	// Check the new vins and update spending tx data in Addresses table
	for it, txDbID := range *TxDbIDs {
		for iv := range dbTxVins[it] {
			// Transaction that spends an outpoint paying to >=0 addresses
			vin := &dbTxVins[it][iv]
			// Get the tx hash and vout index (previous output) from vins table
			// vinDbID, txHash, txIndex, _, err := RetrieveFundingOutpointByTxIn(
			// 	pgb.db, vin.TxID, vin.TxIndex)
			vinDbID := dbTransactions[it].VinDbIds[iv]

			// Single transaction to get funding tx info for the vin, get
			// address row index for the funding tx, and set spending info.
			// var numAddressRowsSet int64
			// numAddressRowsSet, err = SetSpendingByVinID(pgb.db, vinDbID, txDbID, vin.TxID, vin.TxIndex)
			// if err != nil {
			// 	log.Errorf("SetSpendingByVinID: %v", err)
			// }
			// txRes.numAddresses += numAddressRowsSet

			// prevout, ok := pgb.vinPrevOutpoints[vinDbID]
			// if !ok {
			// 	log.Errorf("No funding tx info found for vin %s:%d (prev %s)",
			// 		vin.TxID, vin.TxIndex, vin.PrevOut)
			// 	continue
			// }
			// delete(pgb.vinPrevOutpoints, vinDbID)

			// skip coinbase inputs
			if bytes.Equal(zeroHashStringBytes, []byte(vin.PrevTxHash)) {
				continue
			}

			var numAddressRowsSet int64
			numAddressRowsSet, err = SetSpendingForFundingOP(pgb.db,
				vin.PrevTxHash, vin.PrevTxIndex, // funding
				txDbID, vin.TxID, vin.TxIndex, vinDbID) // spending
			if err != nil {
				log.Errorf("SetSpendingForFundingOP: %v", err)
			}
			txRes.numAddresses += numAddressRowsSet

			/* separate transactions
			txHash, txIndex, _, err := RetrieveFundingOutpointByVinID(pgb.db, vinDbID)
			if err != nil && err != sql.ErrNoRows {
				if err == sql.ErrNoRows {
					log.Warnf("No funding transaction found for input %s:%d", vin.TxID, vin.TxIndex)
					continue
				}
				log.Error("RetrieveFundingOutpointByTxIn:", err)
				continue
			}

			// skip coinbase inputs
			if bytes.Equal(zeroHashStringBytes, []byte(txHash)) {
				continue
			}

			var numAddressRowsSet int64
			numAddressRowsSet, err = SetSpendingForFundingOP(pgb.db, txHash, txIndex, // funding
				txDbID, vin.TxID, vin.TxIndex, vinDbID) // spending
			if err != nil {
				log.Errorf("SetSpendingForFundingOP: %v", err)
			}
			txRes.numAddresses += numAddressRowsSet
			*/
		}
	}

	return txRes
}

func (pgb *ChainDB) CollectTicketSpendDBInfo(dbTxns []*dbtypes.Tx, txDbIDs []uint64,
	msgBlock *wire.MsgBlock) (spendingTxDbIDs []uint64, spendTypes []dbtypes.TicketSpendType,
	ticketHashes []string, ticketDbIDs []uint64, err error) {
	// This only makes sense for stake transactions
	msgTxns := msgBlock.STransactions

	for i, tx := range dbTxns {
		// ensure the transaction slices correspond
		msgTx := msgTxns[i]
		if tx.TxID != msgTx.TxHash().String() {
			err = fmt.Errorf("txid of dbtypes.Tx does not match that of msgTx")
			return
		}

		// Filter for votes and revokes only
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
		t, err0 := pgb.unspentTicketCache.TxnDbID(ticketHash,
			spendType != dbtypes.TicketVoted) // expire cache entry unless a vote
		if err0 != nil {
			err = fmt.Errorf("failed to retrieve ticket DB ID: %v", err0)
			return
		}
		ticketDbIDs = append(ticketDbIDs, t)
	}
	return
}

// UpdateSpendingInfoInAllAddresses completely rebuilds the spending transaction
// info columns of the address table. This is intended to be use after syncing
// all other tables and creating their indexes, particularly the indexes on the
// vins table, and the addresses table index on the funding tx columns. This can
// be used instead of using updateAddressesSpendingInfo=true with storeTxns,
// which will update these addresses table columns too, but much more slowly for
// a number of reasons (that are well worth investigating BTW!).
func (pgb *ChainDB) UpdateSpendingInfoInAllAddresses() (int64, error) {
	// Get the full list of vinDbIDs
	allVinDbIDs, err := RetrieveAllVinDbIDs(pgb.db)
	if err != nil {
		log.Errorf("RetrieveAllVinDbIDs: %v", err)
		return 0, err
	}

	updatesPerDBTx := 500

	log.Infof("Updating spending tx info for %d addresses...", len(allVinDbIDs))
	var numAddresses int64
	for i := 0; i < len(allVinDbIDs); i += updatesPerDBTx {
		//for i, vinDbID := range allVinDbIDs {
		if i%250000 == 0 {
			endRange := i + 250000 - 1
			if endRange > len(allVinDbIDs) {
				endRange = len(allVinDbIDs)
			}
			log.Infof("Updating from vins %d to %d...", i, endRange)
		}

		/*var numAddressRowsSet int64
		numAddressRowsSet, err = SetSpendingForVinDbID(pgb.db, vinDbID)
		if err != nil {
			log.Errorf("SetSpendingForFundingOP: %v", err)
			continue
		}
		numAddresses += numAddressRowsSet*/
		var numAddressRowsSet int64
		endChunk := i + updatesPerDBTx
		if endChunk > len(allVinDbIDs) {
			endChunk = len(allVinDbIDs)
		}
		_, numAddressRowsSet, err = SetSpendingForVinDbIDs(pgb.db,
			allVinDbIDs[i:endChunk])
		if err != nil {
			log.Errorf("SetSpendingForFundingOP: %v", err)
			continue
		}
		numAddresses += numAddressRowsSet

		/* // Get the funding and spending tx info for the vin
		prevoutHash, prevoutVoutInd, _, txHash, txIndex, _, erri :=
			RetrieveVinByID(pgb.db, vinDbID)
		if erri != nil {
			if erri == sql.ErrNoRows {
				log.Warnf("No funding transaction found for vin DB ID %d", vinDbID)
				continue
			}
			log.Error("RetrieveFundingOutpointByTxIn:", erri)
			continue
		}

		// skip coinbase inputs
		if bytes.Equal(zeroHashStringBytes, []byte(txHash)) {
			continue
		}

		// Set the spending tx for the address rows with a matching funding tx
		var numAddressRowsSet int64
		numAddressRowsSet, err = SetSpendingForFundingOP(pgb.db,
			prevoutHash, prevoutVoutInd, // funding
			0, txHash, txIndex, vinDbID) // spending
		// DANGER!!!! missing txDbID above
		if err != nil {
			log.Errorf("SetSpendingForFundingOP: %v", err)
			continue
		}
		numAddresses += numAddressRowsSet */
	}

	return numAddresses, err
}

// UpdateSpendingInfoInAllTickets reviews all votes and revokes and sets this
// spending info in the tickets table.
func (pgb *ChainDB) UpdateSpendingInfoInAllTickets() (int64, error) {
	// Get the full list of votes (DB IDs and heights), and spent ticket hashes
	allVotesDbIDs, allVotesHeights, ticketDbIDs, err :=
		RetrieveAllVotesDbIDsHeightsTicketDbIDs(pgb.db)
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

	revokeIDs, _, revokeHeights, vinDbIDs, err := RetrieveAllRevokesDbIDHashHeight(pgb.db)
	if err != nil {
		log.Errorf("RetrieveAllRevokesDbIDHashHeight: %v", err)
		return 0, err
	}

	revokedTicketHashes := make([]string, len(vinDbIDs))
	for i, vinDbID := range vinDbIDs {
		revokedTicketHashes[i], err = RetrieveFundingTxByVinDbID(pgb.db, vinDbID)
		if err != nil {
			log.Errorf("RetrieveFundingTxByVinDbID: %v", err)
			return 0, err
		}
	}

	revokedTicketDbIDs, err := RetrieveTicketIDsByHashes(pgb.db, revokedTicketHashes)
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
