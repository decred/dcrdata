// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrsqlite

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/v4/blockdata"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/slog"
	sqlite3 "github.com/mattn/go-sqlite3" // register sqlite driver with database/sql
)

// StakeInfoDatabaser is the interface for an extended stake info saving database
type StakeInfoDatabaser interface {
	StoreStakeInfoExtended(bd *apitypes.StakeInfoExtended) error
	RetrieveStakeInfoExtended(ind int64) (*apitypes.StakeInfoExtended, error)
}

// BlockSummaryDatabaser is the interface for a block data saving database
type BlockSummaryDatabaser interface {
	StoreBlockSummary(bd *apitypes.BlockDataBasic) error
	RetrieveBlockSummary(ind int64) (*apitypes.BlockDataBasic, error)
}

// DBInfo contains db configuration
type DBInfo struct {
	FileName string
}

const (
	// TableNameSummaries is name of the table used to store block summary data
	TableNameSummaries = "dcrdata_block_summary"
	// TableNameStakeInfo is name of the table used to store extended stake info
	TableNameStakeInfo = "dcrdata_stakeinfo_extended"
	// A couple database queries that are called before NewWiredDB
	SetCacheSizeSQL       = "PRAGMA cache_size = 32768;"
	SetSynchrounousOffSQL = "pragma synchronous = OFF;"

	// DBBusyTimeout is the length of time in milliseconds for sqlite to retry
	// DB access when the SQLITE_BUSY error would otherwise be returned.
	DBBusyTimeout = "30000"
)

// DB is a wrapper around sql.DB that adds methods for storing and retrieving
// chain data. Use InitDB to get a new instance.
type DB struct {
	*sql.DB

	// BlockCache stores apitypes.BlockDataBasic and apitypes.StakeInfoExtended
	// in StoreBlock for quick retrieval without a DB query.
	BlockCache *apitypes.APICache

	// Guard cached data: lastStoredBlock, dbSummaryHeight, dbStakeInfoHeight
	mtx sync.RWMutex

	// lastStoredBlock caches data for the most recently stored block, and is
	// used to optimize getTip.
	lastStoredBlock *apitypes.BlockDataBasic

	// dbSummaryHeight is set when a block's summary data is stored.
	dbSummaryHeight int64
	// dbSummaryHeight is set when a block's stake info is stored.
	dbStakeInfoHeight int64

	// Drop shutdownDcrdata when the "database is locked" error is mitigated.
	// See https://github.com/decred/dcrdata/issues/1133 for more info.
	shutdownDcrdata func()

	// Block summary table queries
	insertBlockSQL                                               string
	getPoolSQL, getPoolRangeSQL                                  string
	getPoolByHashSQL                                             string
	getPoolValSizeRangeSQL, getAllPoolValSize                    string
	getWinnersByHashSQL, getWinnersSQL                           string
	getDifficulty                                                string
	getSDiffSQL, getSDiffRangeSQL                                string
	getBlockSQL, getBestBlockSQL                                 string
	getBlockByHashSQL, getBlockByTimeRangeSQL, getBlockByTimeSQL string
	getBlockHashSQL, getBlockHeightSQL                           string
	getBlockSizeSQL, getBlockSizeRangeSQL                        string
	getBestBlockHashSQL, getBestBlockHeightSQL                   string
	getHighestBlockHashSQL, getHighestBlockHeightSQL             string
	getMainchainStatusSQL, invalidateBlockSQL                    string
	setHeightToSideChainSQL                                      string
	deleteBlockByHeightMainChainSQL, deleteBlockByHashSQL        string
	deleteBlocksAboveHeightSQL                                   string

	// Stake info table queries
	insertStakeInfoExtendedSQL                                    string
	getBestStakeHeightSQL, getHighestStakeHeightSQL               string
	getStakeInfoExtendedByHashSQL                                 string
	deleteStakeInfoByHeightMainChainSQL, deleteStakeInfoByHashSQL string
	deleteStakeInfoAboveHeightSQL                                 string

	// JOINed table queries
	getLatestStakeInfoExtendedSQL                        string
	getStakeInfoExtendedByHeightSQL                      string
	getAllFeeInfoPerBlock                                string
	getStakeInfoWinnersSQL, getStakeInfoWinnersByHashSQL string

	// Table creation
	rawCreateBlockSummaryStmt, rawCreateStakeInfoExtendedStmt string
	// Table metadata
	getBlockSummaryTableInfo, getStakeInfoExtendedTableInfo string
}

// NewDB creates a new DB instance with pre-generated sql statements from an
// existing sql.DB. Use InitDB to create a new DB without having a sql.DB.
func NewDB(db *sql.DB, shutdown func()) (*DB, error) {
	d := DB{
		DB:                db,
		dbSummaryHeight:   -1,
		dbStakeInfoHeight: -1,
		shutdownDcrdata:   shutdown,
	}

	// Block summary insert
	d.insertBlockSQL = fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
			hash, height, size,
			diff, sdiff, time,
			poolsize, poolval, poolavg,
			winners, is_mainchain, is_valid
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, TableNameSummaries)

	// Stake info insert
	d.insertStakeInfoExtendedSQL = fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
            hash, height, num_tickets, fee_min, fee_max, fee_mean, fee_med, fee_std,
			sdiff, window_num, window_ind, pool_size, pool_val, pool_valavg, winners
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, TableNameStakeInfo)

	// Ticket pool queries (block summaries table)
	d.getPoolSQL = fmt.Sprintf(`SELECT hash, poolsize, poolval, poolavg, winners`+
		` FROM %s WHERE height = ? AND is_mainchain = 1`, TableNameSummaries)
	d.getPoolByHashSQL = fmt.Sprintf(`SELECT height, poolsize, poolval, poolavg, winners`+
		` FROM %s WHERE hash = ?`, TableNameSummaries)
	d.getPoolRangeSQL = fmt.Sprintf(`SELECT height, hash, poolsize, poolval, poolavg, winners `+
		`FROM %s WHERE height BETWEEN ? AND ? AND is_mainchain = 1`, TableNameSummaries)
	d.getPoolValSizeRangeSQL = fmt.Sprintf(`SELECT poolsize, poolval `+
		`FROM %s WHERE height BETWEEN ? AND ? AND is_mainchain = 1`, TableNameSummaries)
	d.getAllPoolValSize = fmt.Sprintf(`SELECT distinct poolsize, poolval, time `+
		`FROM %s WHERE is_mainchain = 1 ORDER BY time`, TableNameSummaries)
	d.getWinnersSQL = fmt.Sprintf(`SELECT hash, winners FROM %s
		WHERE height = ? AND is_mainchain = 1`, TableNameSummaries)
	d.getWinnersByHashSQL = fmt.Sprintf(`SELECT height, winners FROM %s WHERE hash = ?`,
		TableNameSummaries)

	d.getSDiffSQL = fmt.Sprintf(`SELECT sdiff FROM %s
		WHERE height = ? AND is_mainchain = 1`, TableNameSummaries)
	d.getDifficulty = fmt.Sprintf(`SELECT diff FROM %s
		WHERE time >= ? ORDER BY time LIMIT 1`, TableNameSummaries)
	d.getSDiffRangeSQL = fmt.Sprintf(`SELECT sdiff FROM %s
		WHERE height BETWEEN ? AND ?`, TableNameSummaries)

	// General block summaries table queries
	d.getBlockSQL = fmt.Sprintf(`SELECT * FROM %s
		WHERE height = ? AND is_mainchain = 1`, TableNameSummaries)
	d.getBlockByHashSQL = fmt.Sprintf(`SELECT * FROM %s
		WHERE hash = ?`, TableNameSummaries)
	d.getBestBlockSQL = fmt.Sprintf(`SELECT * FROM %s
		WHERE is_mainchain = 1
		ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)

	d.getBlockSizeSQL = fmt.Sprintf(`SELECT size FROM %s
		WHERE is_mainchain = 1 AND height = ?`,
		TableNameSummaries)
	d.getBlockSizeRangeSQL = fmt.Sprintf(`SELECT size FROM %s
		WHERE is_mainchain = 1 AND height BETWEEN ? AND ?`,
		TableNameSummaries)
	d.getBlockByTimeRangeSQL = fmt.Sprintf(`SELECT * FROM %s
		WHERE is_mainchain = 1 AND time BETWEEN ? AND ?
		ORDER BY time LIMIT ?`,
		TableNameSummaries)
	d.getBlockByTimeSQL = fmt.Sprintf(`SELECT * FROM %s WHERE time = ?`,
		TableNameSummaries)

	d.getBestBlockHashSQL = fmt.Sprintf(`SELECT hash FROM %s WHERE is_mainchain = 1
		ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)
	d.getBestBlockHeightSQL = fmt.Sprintf(`SELECT height FROM %s WHERE is_mainchain = 1
		ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)

	d.getHighestBlockHashSQL = fmt.Sprintf(`SELECT hash FROM %s
		ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)
	d.getHighestBlockHeightSQL = fmt.Sprintf(`SELECT height FROM %s
		ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)

	d.getBlockHashSQL = fmt.Sprintf(`SELECT hash FROM %s
		WHERE height = ? AND is_mainchain = 1`, TableNameSummaries)
	d.getBlockHeightSQL = fmt.Sprintf(`SELECT height FROM %s
		WHERE hash = ?`, TableNameSummaries)
	d.getMainchainStatusSQL = fmt.Sprintf(`SELECT is_mainchain FROM %s
		WHERE hash = ?`, TableNameSummaries)

	d.invalidateBlockSQL = fmt.Sprintf(`UPDATE %s SET is_valid = 0
		WHERE hash = ?`, TableNameSummaries)
	d.setHeightToSideChainSQL = fmt.Sprintf(`UPDATE %s SET is_mainchain = 0
		WHERE height = ?`, TableNameSummaries)

	d.deleteBlockByHashSQL = fmt.Sprintf(`DELETE FROM %s
		WHERE hash = ?`, TableNameSummaries)
	d.deleteBlockByHeightMainChainSQL = fmt.Sprintf(`DELETE FROM %s
		WHERE height = ? AND is_mainchain = 1`, TableNameSummaries)
	d.deleteBlocksAboveHeightSQL = fmt.Sprintf(`DELETE FROM %s
		WHERE height > ?`, TableNameSummaries)

	// Stake info table queries
	d.getStakeInfoExtendedByHeightSQL = fmt.Sprintf(`SELECT %[1]s.* FROM %[1]s
		JOIN %[2]s ON %[1]s.hash = %[2]s.hash
		WHERE %[2]s.is_mainchain = 1 AND %[1]s.height = ?`,
		TableNameStakeInfo, TableNameSummaries)
	d.getStakeInfoExtendedByHashSQL = fmt.Sprintf(`SELECT * FROM %s WHERE hash = ?`,
		TableNameStakeInfo)
	d.getStakeInfoWinnersSQL = fmt.Sprintf(`SELECT %[1]s.winners FROM %[1]s
		JOIN %[2]s ON %[1]s.hash = %[2]s.hash
		WHERE %[1]s.height = ?`,
		TableNameStakeInfo, TableNameSummaries)
	d.getStakeInfoWinnersByHashSQL = fmt.Sprintf(`SELECT %[1]s.winners FROM %[1]s
		JOIN %[2]s ON %[1]s.hash = %[2]s.hash
		WHERE %[1]s.hash = ?`,
		TableNameStakeInfo, TableNameSummaries)
	d.getLatestStakeInfoExtendedSQL = fmt.Sprintf(
		`SELECT %[1]s.* FROM %[1]s
		 JOIN %[2]s ON %[1]s.hash = %[2]s.hash
		 WHERE %[2]s.is_mainchain = 1
		 ORDER BY %[1]s.height DESC LIMIT 0, 1`,
		TableNameStakeInfo, TableNameSummaries)
	d.getBestStakeHeightSQL = fmt.Sprintf(
		`SELECT %[1]s.height FROM %[1]s
		 JOIN %[2]s ON %[1]s.hash = %[2]s.hash
		 WHERE %[2]s.is_mainchain = 1
		 ORDER BY %[1]s.height DESC LIMIT 0, 1`,
		TableNameStakeInfo, TableNameSummaries)
	d.getHighestStakeHeightSQL = fmt.Sprintf(
		`SELECT height FROM %s
		 ORDER BY height DESC LIMIT 0, 1`,
		TableNameStakeInfo)

	d.getAllFeeInfoPerBlock = fmt.Sprintf(
		`SELECT distinct %[1]s.height, fee_med FROM %[1]s
		 JOIN %[2]s ON %[1]s.hash = %[2]s.hash
		 ORDER BY %[1]s.height;`,
		TableNameStakeInfo, TableNameSummaries)

	d.deleteStakeInfoByHashSQL = fmt.Sprintf(`DELETE FROM %s
		WHERE hash = ?`, TableNameStakeInfo)
	d.deleteStakeInfoByHeightMainChainSQL = fmt.Sprintf(`DELETE FROM %s
		WHERE hash IN (
			SELECT hash FROM %s
			WHERE height = ? AND is_mainchain = 1
		)`, TableNameStakeInfo, TableNameSummaries)
	d.deleteStakeInfoAboveHeightSQL = fmt.Sprintf(`DELETE FROM %s
		WHERE height > ?`, TableNameStakeInfo)

	// Table metadata
	d.getBlockSummaryTableInfo = fmt.Sprintf(`PRAGMA table_info(%s);`, TableNameSummaries)
	d.getStakeInfoExtendedTableInfo = fmt.Sprintf(`PRAGMA table_info(%s);`, TableNameStakeInfo)

	// Attempt to retrieve the best mainchain block heights from both tables,
	// but fallback to the highest block if the tables have yet to be upgraded
	// with the is_mainchain column. Log the latter case.
	var err error
	noSuchColPrefix := "no such column: "
	if d.dbSummaryHeight, err = d.getBlockSummaryHeight(); err != nil {
		// If the table scheme lacks the is_mainchain column, and upgrade is
		// required. Fall back to querying without the is_mainchain condition.
		errStr := err.Error()
		ind := strings.LastIndex(errStr, noSuchColPrefix)
		if ind == -1 {
			return nil, fmt.Errorf("NewDB: %v", err)
		}
		// Try again without the mainchain constraint.
		if ind+len(noSuchColPrefix) < len(errStr) {
			log.Infof(`Block summary table missing column "%s". Table upgrade required.`,
				errStr[ind+len(noSuchColPrefix):])
		}
		d.dbSummaryHeight, err = d.getBlockSummaryHeightAnyChain()
		if err != nil {
			return nil, fmt.Errorf("NewDB: %v", err)
		}
	}
	if d.dbStakeInfoHeight, err = d.getStakeInfoHeight(); err != nil {
		// If the table scheme lacks the is_mainchain column, and upgrade is
		// required. Fall back to querying without the is_mainchain condition.
		errStr := err.Error()
		ind := strings.LastIndex(err.Error(), noSuchColPrefix)
		if ind == -1 {
			return nil, fmt.Errorf("NewDB: %v", err)
		}
		// Try again without the mainchain constraint.
		if ind+len(noSuchColPrefix) < len(errStr) {
			log.Debugf(`Block summary table missing column "%s". Table upgrade required.`,
				err.Error()[ind+len(noSuchColPrefix):])
		}
		d.dbStakeInfoHeight, err = d.getStakeInfoHeightAnyChain()
		if err != nil {
			return nil, fmt.Errorf("NewDB: %v", err)
		}
	}

	return &d, nil
}

// InitDB creates a new DB instance from a DBInfo containing the name of the
// file used to back the underlying sql database.
func InitDB(dbInfo *DBInfo, shutdown func()) (*DB, error) {
	dbPath, err := filepath.Abs(dbInfo.FileName)
	if err != nil {
		return nil, err
	}

	// Ensures target DB-file has a parent folder
	parent := filepath.Dir(dbPath)
	err = os.MkdirAll(parent, 0755)
	if err != nil {
		return nil, err
	}

	// "shared-cache" mode has multiple connections share a single data and
	// schema cache. _busy_timeout helps prevent SQLITE_BUSY ("database is
	// locked") errors by sleeping for a certain amount of time when the
	// database is locked. See https://www.sqlite.org/c3ref/busy_timeout.html.
	dbPath = dbPath + "?cache=shared&_busy_timeout=" + DBBusyTimeout
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil || db == nil {
		return nil, err
	}

	// Historically, SQLite did not handle concurrent writes internally,
	// necessitating a limitation of just 1 open connecton. With a busy_timeout
	// set, this is less important. With go-sqlite 1.10, this is not needed at
	// all since the library started compiling sqlite3 for
	// thread-safe/"serialized" operation (SQLITE_THREADSAFE=1). For details,
	// see https://sqlite.org/threadsafe.html and
	// https://sqlite.org/c3ref/c_config_covering_index_scan.html#sqliteconfigserialized.
	// The change to go-sqlite3 is
	// github.com/mattn/go-sqlite3@acfa60124032040b9f5a9406f5a772ee16fe845e
	//
	// db.SetMaxOpenConns(1)

	// These are db-wide settings that persist for the entire session.
	_, err = db.Exec(SetCacheSizeSQL)
	if err != nil {
		log.Error("Error setting SQLite Cache size")
		return nil, err
	}
	_, err = db.Exec(SetSynchrounousOffSQL)
	if err != nil {
		log.Error("Error setting SQLite synchrounous off")
		return nil, err
	}

	rawCreateBlockSummaryStmt := `
    	create table if not exists %s(
      	hash TEXT PRIMARY KEY,
      	height INTEGER,
      	size INTEGER,
      	diff FLOAT,
      	sdiff FLOAT,
      	time INTEGER,
      	poolsize INTEGER,
      	poolval FLOAT,
      	poolavg FLOAT,
      	winners TEXT,
      	is_mainchain BOOL,
      	is_valid BOOL
    	);`

	createBlockSummaryStmt := fmt.Sprintf(rawCreateBlockSummaryStmt, TableNameSummaries)

	_, err = db.Exec(createBlockSummaryStmt)
	if err != nil {
		log.Errorf("%q: %s\n", err, createBlockSummaryStmt)
		return nil, err
	}

	rawCreateStakeInfoExtendedStmt := `
    	create table if not exists %s(
      	hash TEXT PRIMARY KEY,
      	height INTEGER,
      	num_tickets INTEGER,
      	fee_min FLOAT, fee_max FLOAT, fee_mean FLOAT,
      	fee_med FLOAT, fee_std FLOAT,
      	sdiff FLOAT, window_num INTEGER, window_ind INTEGER,
      	pool_size INTEGER, pool_val FLOAT, pool_valavg FLOAT,
      	winners TEXT
    	);`

	createStakeInfoExtendedStmt := fmt.Sprintf(rawCreateStakeInfoExtendedStmt, TableNameStakeInfo)

	_, err = db.Exec(createStakeInfoExtendedStmt)
	if err != nil {
		log.Errorf("%q: %s\n", err, createStakeInfoExtendedStmt)
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	dataBase, err := NewDB(db, shutdown)
	if err != nil {
		return nil, err
	}

	// Table creation string are used in JustifyTableStructures
	// Eventually won't be needed.
	dataBase.rawCreateBlockSummaryStmt = rawCreateBlockSummaryStmt
	dataBase.rawCreateStakeInfoExtendedStmt = rawCreateStakeInfoExtendedStmt
	if err = dataBase.JustifyTableStructures(dbInfo); err != nil {
		return nil, err
	}

	return dataBase, err
}

// Need to check the error for a SQLite database "database is locked" error
// until resolved. See https://github.com/decred/dcrdata/issues/1133 for info.
func (db *DB) filterError(err error) error {
	if err == nil {
		return err
	}
	sqliteErr, is := err.(sqlite3.Error)
	if is && sqliteErr.Code == sqlite3.ErrLocked {
		log.Criticalf("SQLite3 database is locked error encountered. Restart required")
		db.shutdownDcrdata()
	}
	return err
}

// DBDataSaver models a DB with a channel to communicate new block height to the
// web interface.
type DBDataSaver struct {
	*DB
	updateStatusChan chan uint32
}

// Store satisfies the blockdata.BlockDataSaver interface. This function is only
// to be used for storing main chain block data. Use StoreSideBlock or
// StoreBlock directly to store side chain block data.
func (db *DBDataSaver) Store(data *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	summary := data.ToBlockSummary()
	var err error

	// Store is assumed to be called with a mainchain block
	lastIsValid := msgBlock.Header.VoteBits&1 != 0
	if !lastIsValid {
		// Update the is_valid flag in the blocks table.
		// Need to check whether to invalidate previous block
		lastBlockHash := msgBlock.Header.PrevBlock
		log.Infof("Setting last block %s as INVALID", lastBlockHash)

		// Check for genesis block
		isSecondBlock := lastBlockHash != chainhash.Hash{}
		if isSecondBlock {
			var lastIsMain bool
			lastIsMain, err = db.DB.getMainchainStatus(lastBlockHash.String())
			if err != nil {
				return err
			}
			if lastIsMain {
				lastIsValid := msgBlock.Header.VoteBits&1 != 0
				if !lastIsValid {
					err = db.DB.invalidateBlock(lastBlockHash.String())
					if err != nil {
						return err
					}
				}
			}
		}
	}

	// Store the main chain block data, flagging any other blocks at this height
	// as side chain.
	err = db.DB.StoreBlockSummary(&summary)
	if err != nil {
		return err
	}

	select {
	case db.updateStatusChan <- summary.Height:
	default:
	}

	stakeInfoExtended := data.ToStakeInfoExtended()
	return db.DB.StoreStakeInfoExtended(&stakeInfoExtended)
}

func (db *DB) updateHeights() error {
	// Update dbSummaryHeight and dbStakeInfoHeight.
	db.mtx.Lock()
	defer db.mtx.Unlock()

	height, err := db.getBlockSummaryHeight()
	if err != nil {
		return err
	}
	db.dbSummaryHeight = height

	height, err = db.getStakeInfoHeight()
	if err != nil {
		return err
	}
	db.dbStakeInfoHeight = height

	// Reset lastStoredBlock. It will be loaded on demand by getTip, and updated
	// by StoreBlock.
	db.lastStoredBlock = nil

	return nil
}

func (db *DB) deleteBlock(blockhash string) (NSummaryRows, NStakeInfoRows int64, err error) {
	// Remove rows from block summary table.
	var res sql.Result
	res, err = db.Exec(db.deleteBlockByHashSQL, blockhash)
	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		db.filterError(err)
		return
	}

	NSummaryRows, err = res.RowsAffected()
	if err != nil {
		return
	}

	// Remove rows from stake info table.
	res, err = db.Exec(db.deleteStakeInfoByHashSQL, blockhash)
	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		db.filterError(err)
		return
	}

	NStakeInfoRows, err = res.RowsAffected()

	return
}

// DeleteBlock purges the summary data and stake info for the block with the
// given hash. The number of rows deleted is returned.
func (db *DB) DeleteBlock(blockhash string) (NSummaryRows, NStakeInfoRows int64, err error) {
	// Attempt to purge the block data.
	NSummaryRows, NStakeInfoRows, err = db.deleteBlock(blockhash)
	if err != nil {
		return
	}

	err = db.updateHeights()
	return
}

func (db *DB) deleteBlocksAboveHeight(height int64) (NSummaryRows, NStakeInfoRows int64, err error) {
	// Remove rows from block summary table.
	var res sql.Result
	res, err = db.Exec(db.deleteBlocksAboveHeightSQL, height)
	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		db.filterError(err)
		return
	}

	NSummaryRows, err = res.RowsAffected()
	if err != nil {
		return
	}

	// Remove rows from stake info table.
	res, err = db.Exec(db.deleteStakeInfoAboveHeightSQL, height)
	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		db.filterError(err)
		return
	}

	NStakeInfoRows, err = res.RowsAffected()

	return
}

// DeleteBlocksAboveHeight purges the summary data and stake info for the blocks
// above the given height, including side chain blocks.
func (db *DB) DeleteBlocksAboveHeight(height int64) (NSummaryRows, NStakeInfoRows int64, err error) {
	// Attempt to purge the block data.
	NSummaryRows, NStakeInfoRows, err = db.deleteBlocksAboveHeight(height)
	if err != nil {
		return
	}

	err = db.updateHeights()
	return
}

// Delete summary data for block at the given height on the main chain. The
// number of rows deleted is returned.
func (db *DB) deleteBlockHeightMainchain(height int64) (int64, error) {
	res, err := db.Exec(db.deleteBlockByHeightMainChainSQL, height)
	if err != nil {
		return 0, db.filterError(err)
	}
	return res.RowsAffected()
}

// Invalidate block with the given hash.
func (db *DB) invalidateBlock(blockhash string) error {
	_, err := db.Exec(db.invalidateBlockSQL, blockhash)
	return db.filterError(err)
}

// Sets the is_mainchain field to false for the given block in the database.
func (db *DB) setHeightToSideChain(height int64) error {
	_, err := db.Exec(db.setHeightToSideChainSQL, height)
	return db.filterError(err)
}

// Returns the is_mainchain value from the database for the given hash.
func (db *DB) getMainchainStatus(blockhash string) (bool, error) {
	var isMainchain bool
	err := db.QueryRow(db.getMainchainStatusSQL, blockhash).Scan(&isMainchain)
	return isMainchain, err
}

// StoreBlock attempts to store the block data in the database, and
// returns an error on failure.
func (db *DB) StoreBlock(bd *apitypes.BlockDataBasic, isMainchain bool, isValid bool) error {
	// Cache mainchain blocks.
	if isMainchain && db.BlockCache != nil && db.BlockCache.IsEnabled() {
		if err := db.BlockCache.StoreBlockSummary(bd); err != nil {
			return fmt.Errorf("APICache failed to store block: %v", err)
		}
	}
	stmt, err := db.Prepare(db.insertBlockSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// If input block data lacks non-nil PoolInfo, set to a zero-value
	// TicketPoolInfo.
	if bd.PoolInfo == nil {
		bd.PoolInfo = new(apitypes.TicketPoolInfo)
	}

	// Insert the block.
	winners := strings.Join(bd.PoolInfo.Winners, ";")
	res, err := stmt.Exec(&bd.Hash, &bd.Height, &bd.Size,
		&bd.Difficulty, &bd.StakeDiff, bd.Time.S.UNIX(),
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners, &isMainchain, &isValid)
	if err != nil {
		return db.filterError(err)
	}

	// Update the DB block summary height.
	db.mtx.Lock()
	defer db.mtx.Unlock()
	if err = logDBResult(res); err == nil {
		height := int64(bd.Height)
		if height > db.dbSummaryHeight {
			db.dbSummaryHeight = height
		}
		if db.lastStoredBlock == nil || bd.Height >= db.lastStoredBlock.Height {
			db.lastStoredBlock = new(apitypes.BlockDataBasic)
			*db.lastStoredBlock = *bd
		}
	}

	return err
}

// StoreBlockSummary is called with new mainchain blocks.
func (db *DB) StoreBlockSummary(bd *apitypes.BlockDataBasic) error {
	return db.StoreBlock(bd, true, true)
}

// StoreSideBlockSummary is for storing side chain.
func (db *DB) StoreSideBlockSummary(bd *apitypes.BlockDataBasic) error {
	return db.StoreBlock(bd, false, true)
}

// GetBestBlockHash returns the hash of the best block.
func (db *DB) GetBestBlockHash() string {
	hash, err := db.RetrieveBestBlockHash()
	if err != nil {
		log.Errorf("RetrieveBestBlockHash failed: %v", err)
		return ""
	}
	return hash
}

// GetBestBlockHeight returns the height of the best block
func (db *DB) GetBestBlockHeight() int64 {
	h, _ := db.GetBlockSummaryHeight()
	return h
}

func (db *DB) getChainBlockSummaryHeight(onlyMainchain bool) (int64, error) {
	heightFunc := db.RetrieveHighestBlockHeight
	if onlyMainchain {
		heightFunc = db.RetrieveBestBlockHeight
	}
	// Query the block summary table for the best or highest block height.
	height, err := heightFunc()
	if err == nil {
		return height, nil
	}

	// No rows returned is not considered an error. When the table is empty,
	// return -1 height and nil error, but log a warning.
	if err == sql.ErrNoRows {
		log.Warn("Block summary table is empty.")
		return -1, nil
	}

	return -1, fmt.Errorf("RetrieveBestBlockHeight failed: %v", err)
}

// getBlockSummaryHeight returns the best (mainchain) block height for which the
// database can provide a block summary. When the table is empty, height -1 and
// error nil are returned. The error value will never be sql.ErrNoRows. Usually
// GetBlockSummaryHeight, which caches the most recently stored block height,
// shoud be used instead.
func (db *DB) getBlockSummaryHeight() (int64, error) {
	// Query the block summary table for the best main chain block height.
	return db.getChainBlockSummaryHeight(true)
}

// getBlockSummaryHeightAnyChain is like getBlockSummaryHeight, but it returns
// the block with the largest height regardless of mainchain status.
func (db *DB) getBlockSummaryHeightAnyChain() (int64, error) {
	// Query the block summary table for the largest block height.
	return db.getChainBlockSummaryHeight(false)
}

// GetBlockSummaryHeight returns the largest block height for which the database
// can provide a block summary. A cached best block summary height will be
// returned when available to avoid unnecessary DB queries.
func (db *DB) GetBlockSummaryHeight() (int64, error) {
	db.mtx.RLock()
	defer db.mtx.RUnlock()
	if db.dbSummaryHeight < 0 {
		h, err := db.getBlockSummaryHeight()
		if err == nil {
			db.dbSummaryHeight = h
		}
		return h, err
	}
	return db.dbSummaryHeight, nil
}

func (db *DB) getChainStakeInfoHeight(onlyMainchain bool) (int64, error) {
	heightFunc := db.RetrieveHighestStakeHeight
	if onlyMainchain {
		heightFunc = db.RetrieveBestStakeHeight
	}
	// Query the stake info table for the best main chain block height.
	height, err := heightFunc()
	if err == nil {
		return height, nil
	}

	// No rows returned is not considered an error. When the table is empty,
	// return -1 height and nil error, but log a warning.
	if err == sql.ErrNoRows {
		log.Warn("Stake info table is empty.")
		return -1, nil
	}

	return -1, fmt.Errorf("failed to query stake height: %v", err)
}

// getStakeInfoHeight returns the largest block height for which the database
// can provide stake info data. When the table is empty, height -1 and error nil
// are returned. The error value will never be sql.ErrNoRows. Usually
// GetStakeInfoHeight, which caches the most recently stored block height, shoud
// be used instead.
func (db *DB) getStakeInfoHeight() (int64, error) {
	// Query the stake info table for the best main chain block height.
	return db.getChainStakeInfoHeight(true)
}

// getStakeInfoHeightAnyChain is like getStakeInfoHeight, but it returns the
// block with the largest height regardless of mainchain status.
func (db *DB) getStakeInfoHeightAnyChain() (int64, error) {
	// Query the stake info table for the largest block height.
	return db.getChainStakeInfoHeight(false)
}

// GetStakeInfoHeight returns the cached stake info height if a height is set,
// otherwise it queries the database for the best (mainchain) block height in
// the stake info table and updates the cache.
func (db *DB) GetStakeInfoHeight() (int64, error) {
	db.mtx.RLock()
	defer db.mtx.RUnlock()
	if db.dbStakeInfoHeight < 0 {
		h, err := db.getStakeInfoHeight()
		if err == nil {
			db.dbStakeInfoHeight = h
		}
		return h, err
	}
	return db.dbStakeInfoHeight, nil
}

// RetrievePoolInfoRange returns an array of apitypes.TicketPoolInfo for block
// range ind0 to ind1 and a non-nil error on success
func (db *DB) RetrievePoolInfoRange(ind0, ind1 int64) ([]apitypes.TicketPoolInfo, []string, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []apitypes.TicketPoolInfo{}, []string{}, nil
	}
	if N < 0 {
		return nil, nil, fmt.Errorf("Cannot retrieve pool info range (%d>%d)",
			ind0, ind1)
	}
	db.mtx.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.mtx.RUnlock()
		return nil, nil, fmt.Errorf("Cannot retrieve pool info range [%d,%d], have height %d",
			ind0, ind1, db.dbSummaryHeight)
	}
	db.mtx.RUnlock()

	tpis := make([]apitypes.TicketPoolInfo, 0, N)
	hashes := make([]string, 0, N)

	stmt, err := db.Prepare(db.getPoolRangeSQL)
	if err != nil {
		return nil, nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(ind0, ind1)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var tpi apitypes.TicketPoolInfo
		var hash, winners string
		if err = rows.Scan(&tpi.Height, &hash, &tpi.Size, &tpi.Value,
			&tpi.ValAvg, &winners); err != nil {
			log.Errorf("Unable to scan for TicketPoolInfo fields: %v", err)
		}
		tpi.Winners = splitToArray(winners)
		tpis = append(tpis, tpi)
		hashes = append(hashes, hash)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}

	return tpis, hashes, nil
}

// RetrievePoolInfo returns ticket pool info for block height ind
func (db *DB) RetrievePoolInfo(ind int64) (*apitypes.TicketPoolInfo, error) {
	tpi := &apitypes.TicketPoolInfo{
		Height: uint32(ind),
	}
	var hash, winners string
	err := db.QueryRow(db.getPoolSQL, ind).Scan(&hash, &tpi.Size,
		&tpi.Value, &tpi.ValAvg, &winners)
	tpi.Winners = splitToArray(winners)
	return tpi, err
}

// RetrieveWinners returns the winning ticket tx IDs drawn after connecting the
// given block height (called to validate the block). The block hash
// corresponding to the input block height is also returned.
func (db *DB) RetrieveWinners(ind int64) ([]string, string, error) {
	var hash, winners string
	err := db.QueryRow(db.getWinnersSQL, ind).Scan(&hash, &winners)
	if err != nil {
		return nil, "", err
	}
	return splitToArray(winners), hash, err
}

// RetrieveWinnersByHash returns the winning ticket tx IDs drawn after
// connecting the block with the given hash. The block height corresponding to
// the input block hash is also returned.
func (db *DB) RetrieveWinnersByHash(hash string) ([]string, uint32, error) {
	var winners string
	var height uint32
	err := db.QueryRow(db.getWinnersByHashSQL, hash).Scan(&height, &winners)
	if err != nil {
		return nil, 0, err
	}
	return splitToArray(winners), height, err
}

// RetrievePoolInfoByHash returns ticket pool info for blockhash hash.
func (db *DB) RetrievePoolInfoByHash(hash string) (*apitypes.TicketPoolInfo, error) {
	tpi := new(apitypes.TicketPoolInfo)
	var winners string
	err := db.QueryRow(db.getPoolByHashSQL, hash).Scan(&tpi.Height, &tpi.Size,
		&tpi.Value, &tpi.ValAvg, &winners)
	tpi.Winners = splitToArray(winners)
	return tpi, err
}

// RetrievePoolValAndSizeRange returns an array each of the pool values and
// sizes for block range ind0 to ind1.
func (db *DB) RetrievePoolValAndSizeRange(ind0, ind1 int64) ([]float64, []float64, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []float64{}, []float64{}, nil
	}
	if N < 0 {
		return nil, nil, fmt.Errorf("Cannot retrieve pool val and size range (%d>%d)",
			ind0, ind1)
	}
	db.mtx.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.mtx.RUnlock()
		return nil, nil, fmt.Errorf("Cannot retrieve pool val and size range [%d,%d], have height %d",
			ind0, ind1, db.dbSummaryHeight)
	}
	db.mtx.RUnlock()

	poolvals := make([]float64, 0, N)
	poolsizes := make([]float64, 0, N)

	stmt, err := db.Prepare(db.getPoolValSizeRangeSQL)
	if err != nil {
		return nil, nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(ind0, ind1)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var pval, psize float64
		if err = rows.Scan(&psize, &pval); err != nil {
			log.Errorf("Unable to scan for TicketPoolInfo fields: %v", err)
		}
		poolvals = append(poolvals, pval)
		poolsizes = append(poolsizes, psize)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}

	if len(poolsizes) != int(N) {
		log.Warnf("RetrievePoolValAndSizeRange: Retrieved pool values (%d) not expected number (%d)",
			len(poolsizes), N)
	}

	return poolvals, poolsizes, nil
}

// RetrieveAllPoolValAndSize returns all the pool values and sizes stored since
// the first value was recorded up current height.
func (db *DB) RetrieveAllPoolValAndSize() (*dbtypes.ChartsData, error) {
	chartsData := new(dbtypes.ChartsData)
	stmt, err := db.Prepare(db.getAllPoolValSize)
	if err != nil {
		return chartsData, err
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return chartsData, err
	}
	defer rows.Close()

	for rows.Next() {
		var pval, psize float64
		var timestamp int64
		if err = rows.Scan(&psize, &pval, &timestamp); err != nil {
			log.Errorf("Unable to scan for TicketPoolInfo fields: %v", err)
			// TODO: not return???
		}

		if timestamp == 0 {
			continue
		}

		chartsData.Time = append(chartsData.Time, dbtypes.NewTimeDefFromUNIX(timestamp))
		chartsData.SizeF = append(chartsData.SizeF, psize)
		chartsData.ValueF = append(chartsData.ValueF, pval)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}

	if len(chartsData.Time) < 1 {
		log.Warnf("RetrieveAllPoolValAndSize: Retrieved pool values (%d) not expected number (%d)",
			len(chartsData.Time), 1)
	}

	return chartsData, nil
}

// RetrieveBlockFeeInfo fetches the block median fee chart data.
func (db *DB) RetrieveBlockFeeInfo() (*dbtypes.ChartsData, error) {
	db.mtx.RLock()
	defer db.mtx.RUnlock()

	var chartsData = new(dbtypes.ChartsData)
	var stmt, err = db.Prepare(db.getAllFeeInfoPerBlock)
	if err != nil {
		return chartsData, err
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return chartsData, err
	}
	defer rows.Close()

	for rows.Next() {
		var feeMed float64
		var height uint64
		if err = rows.Scan(&height, &feeMed); err != nil {
			log.Errorf("Unable to scan for FeeInfoPerBlock fields: %v", err)
		}
		if height == 0 && feeMed == 0 {
			continue
		}

		chartsData.Count = append(chartsData.Count, height)
		chartsData.SizeF = append(chartsData.SizeF, feeMed)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}

	if len(chartsData.Count) < 1 {
		log.Warnf("RetrieveBlockFeeInfo: Retrieved pool values (%d) not expected number (%d)", len(chartsData.Count), 1)
	}

	return chartsData, nil
}

// RetrieveSDiffRange returns an array of stake difficulties for block range ind0 to
// ind1
func (db *DB) RetrieveSDiffRange(ind0, ind1 int64) ([]float64, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []float64{}, nil
	}
	if N < 0 {
		return nil, fmt.Errorf("Cannot retrieve sdiff range (%d>%d)",
			ind0, ind1)
	}
	db.mtx.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.mtx.RUnlock()
		return nil, fmt.Errorf("Cannot retrieve sdiff range [%d,%d], have height %d",
			ind0, ind1, db.dbSummaryHeight)
	}
	db.mtx.RUnlock()

	sdiffs := make([]float64, 0, N)

	stmt, err := db.Prepare(db.getSDiffRangeSQL)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(ind0, ind1)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sdiff float64
		if err = rows.Scan(&sdiff); err != nil {
			log.Errorf("Unable to scan for sdiff fields: %v", err)
		}
		sdiffs = append(sdiffs, sdiff)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}

	return sdiffs, nil
}

func (db *DB) RetrieveBlockSummaryByTimeRange(minTime, maxTime int64, limit int) ([]apitypes.BlockDataBasic, error) {
	blocks := make([]apitypes.BlockDataBasic, 0, limit)

	stmt, err := db.Prepare(db.getBlockByTimeRangeSQL)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(minTime, maxTime, limit)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		bd := apitypes.NewBlockDataBasic()
		var winners string
		var isMainchain, isValid bool
		var timestamp int64
		if err = rows.Scan(&bd.Hash, &bd.Height, &bd.Size,
			&bd.Difficulty, &bd.StakeDiff, &timestamp,
			&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
			&winners, &isMainchain, &isValid); err != nil {
			log.Errorf("Unable to scan for block fields")
		}
		bd.Time = apitypes.TimeAPI{S: dbtypes.NewTimeDefFromUNIX(timestamp)}
		blocks = append(blocks, *bd)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}
	return blocks, nil
}

// RetrieveDiff returns the difficulty in the last 24hrs or immediately after
// 24hrs.
func (db *DB) RetrieveDiff(timestamp int64) (float64, error) {
	var diff float64
	err := db.QueryRow(db.getDifficulty, timestamp).Scan(&diff)
	return diff, err
}

// RetrieveSDiff returns the stake difficulty for block at the specified chain
// height.
func (db *DB) RetrieveSDiff(ind int64) (float64, error) {
	var sdiff float64
	err := db.QueryRow(db.getSDiffSQL, ind).Scan(&sdiff)
	return sdiff, err
}

// RetrieveLatestBlockSummary returns the block summary for the best block.
func (db *DB) RetrieveLatestBlockSummary() (*apitypes.BlockDataBasic, error) {
	bd := apitypes.NewBlockDataBasic()

	var winners string
	var timestamp int64
	var isMainchain, isValid bool
	err := db.QueryRow(db.getBestBlockSQL).Scan(&bd.Hash, &bd.Height, &bd.Size,
		&bd.Difficulty, &bd.StakeDiff, &timestamp,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners, &isMainchain, &isValid)
	if err != nil {
		return nil, err
	}
	bd.Time = apitypes.TimeAPI{S: dbtypes.NewTimeDefFromUNIX(timestamp)}
	bd.PoolInfo.Winners = splitToArray(winners)
	return bd, nil
}

// RetrieveBlockHash returns the block hash for block ind
func (db *DB) RetrieveBlockHash(ind int64) (string, error) {
	// First try the block summary cache.
	usingBlockCache := db.BlockCache != nil && db.BlockCache.IsEnabled()
	if usingBlockCache {
		hash := db.BlockCache.GetBlockHash(ind)
		if hash != "" {
			// Cache hit!
			return hash, nil
		}
		// Cache miss necessitates a DB query.
	}

	var blockHash string
	err := db.QueryRow(db.getBlockHashSQL, ind).Scan(&blockHash)
	return blockHash, err
}

// RetrieveBlockHeight returns the block height for blockhash hash
func (db *DB) RetrieveBlockHeight(hash string) (int64, error) {
	var blockHeight int64
	err := db.QueryRow(db.getBlockHeightSQL, hash).Scan(&blockHeight)
	return blockHeight, err
}

// RetrieveBestBlockHash returns the block hash for the best (mainchain) block.
func (db *DB) RetrieveBestBlockHash() (string, error) {
	var blockHash string
	err := db.QueryRow(db.getBestBlockHashSQL).Scan(&blockHash)
	return blockHash, err
}

// RetrieveHighestBlockHash returns the block hash for the highest block,
// regardless of mainchain status.
func (db *DB) RetrieveHighestBlockHash() (string, error) {
	var blockHash string
	err := db.QueryRow(db.getHighestBlockHashSQL).Scan(&blockHash)
	return blockHash, err
}

// RetrieveBestBlockHeight returns the block height for the best block
func (db *DB) RetrieveBestBlockHeight() (int64, error) {
	var blockHeight int64
	err := db.QueryRow(db.getBestBlockHeightSQL).Scan(&blockHeight)
	return blockHeight, err
}

// RetrieveHighestBlockHeight returns the block height for the highest block,
// regardless of mainchain status.
func (db *DB) RetrieveHighestBlockHeight() (int64, error) {
	var blockHeight int64
	err := db.QueryRow(db.getHighestBlockHeightSQL).Scan(&blockHeight)
	return blockHeight, err
}

// RetrieveBlockSummaryByHash returns basic block data for a block given its hash
func (db *DB) RetrieveBlockSummaryByHash(hash string) (*apitypes.BlockDataBasic, error) {
	// First try the block summary cache.
	usingBlockCache := db.BlockCache != nil && db.BlockCache.IsEnabled()
	if usingBlockCache {
		if bd := db.BlockCache.GetBlockSummaryByHash(hash); bd != nil {
			// Cache hit!
			return bd, nil
		}
		// Cache miss necessitates a DB query.
	}

	bd := apitypes.NewBlockDataBasic()

	var winners string
	var isMainchain, isValid bool
	var timestamp int64
	err := db.QueryRow(db.getBlockByHashSQL, hash).Scan(&bd.Hash, &bd.Height, &bd.Size,
		&bd.Difficulty, &bd.StakeDiff, &timestamp,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners, &isMainchain, &isValid)
	if err != nil {
		return nil, err
	}
	bd.Time = apitypes.TimeAPI{S: dbtypes.NewTimeDefFromUNIX(timestamp)}
	bd.PoolInfo.Winners = splitToArray(winners)

	if usingBlockCache {
		// This is a cache miss since hits return early.
		err = db.BlockCache.StoreBlockSummary(bd)
		if err != nil {
			log.Warnf("Failed to cache summary for block %s: %v", hash, err)
			// Do not return the error.
		}
	}

	return bd, nil
}

// RetrieveBlockSummary returns basic block data for block ind.
func (db *DB) RetrieveBlockSummary(ind int64) (*apitypes.BlockDataBasic, error) {
	// First try the block summary cache.
	usingBlockCache := db.BlockCache != nil && db.BlockCache.IsEnabled()
	if usingBlockCache {
		if bd := db.BlockCache.GetBlockSummary(ind); bd != nil {
			// Cache hit!
			return bd, nil
		}
		// Cache miss necessitates a DB query.
	}

	bd := apitypes.NewBlockDataBasic()

	// Three different ways

	// 1. chained QueryRow/Scan only
	var winners string
	var isMainchain, isValid bool
	var timestamp int64
	err := db.QueryRow(db.getBlockSQL, ind).Scan(&bd.Hash, &bd.Height, &bd.Size,
		&bd.Difficulty, &bd.StakeDiff, &timestamp,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners, &isMainchain, &isValid)
	if err != nil {
		return nil, err
	}
	bd.Time = apitypes.TimeAPI{S: dbtypes.NewTimeDefFromUNIX(timestamp)}
	bd.PoolInfo.Winners = splitToArray(winners)
	// 2. Prepare + chained QueryRow/Scan
	// stmt, err := db.Prepare(getBlockSQL)
	// if err != nil {
	//     return nil, err
	// }
	// defer stmt.Close()

	// err = stmt.QueryRow(ind).Scan(&bd.Height, &bd.Size, &bd.Hash, &bd.Difficulty,
	//     &bd.StakeDiff, &bd.Time, &bd.PoolInfo.Size, &bd.PoolInfo.Value,
	//     &bd.PoolInfo.ValAvg)
	// if err != nil {
	//     return nil, err
	// }

	// 3. Prepare + Query + Scan
	// rows, err := stmt.Query(ind)
	// if err != nil {
	//     log.Errorf("Query failed: %v", err)
	//     return nil, err
	// }
	// defer rows.Close()

	// if rows.Next() {
	//     err = rows.Scan(&bd.Height, &bd.Size, &bd.Hash, &bd.Difficulty, &bd.StakeDiff,
	//         &bd.Time, &bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg)
	//     if err != nil {
	//         log.Errorf("Unable to scan for BlockDataBasic fields: %v", err)
	//     }
	// }
	// if err = rows.Err(); err != nil {
	//     log.Error(err)
	// }

	if usingBlockCache {
		// This is a cache miss since hits return early.
		err = db.BlockCache.StoreBlockSummary(bd)
		if err != nil {
			log.Warnf("Failed to cache summary for block at %d: %v", ind, err)
			// Do not return the error.
		}
	}

	return bd, nil
}

// RetrieveBlockSize return the size of block at height ind.
func (db *DB) RetrieveBlockSize(ind int64) (int32, error) {
	// First try the block summary cache.
	usingBlockCache := db.BlockCache != nil && db.BlockCache.IsEnabled()
	if usingBlockCache {
		sz := db.BlockCache.GetBlockSize(ind)
		if sz != -1 {
			// Cache hit!
			return sz, nil
		}
		// Cache miss necessitates a DB query.
	}

	db.mtx.RLock()
	if ind > db.dbSummaryHeight || ind < 0 {
		defer db.mtx.RUnlock()
		return -1, fmt.Errorf("Cannot retrieve block size %d, have height %d",
			ind, db.dbSummaryHeight)
	}
	db.mtx.RUnlock()

	var blockSize int32
	err := db.QueryRow(db.getBlockSizeSQL, ind).Scan(&blockSize)
	if err != nil {
		return -1, fmt.Errorf("unable to scan for block size: %v", err)
	}

	return blockSize, nil
}

// RetrieveBlockSizeRange returns an array of block sizes for block range ind0 to ind1
func (db *DB) RetrieveBlockSizeRange(ind0, ind1 int64) ([]int32, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []int32{}, nil
	}
	if N < 0 {
		return nil, fmt.Errorf("Cannot retrieve block size range (%d>%d)",
			ind0, ind1)
	}
	db.mtx.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.mtx.RUnlock()
		return nil, fmt.Errorf("Cannot retrieve block size range [%d,%d], have height %d",
			ind0, ind1, db.dbSummaryHeight)
	}
	db.mtx.RUnlock()

	blockSizes := make([]int32, 0, N)

	stmt, err := db.Prepare(db.getBlockSizeRangeSQL)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(ind0, ind1)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var blockSize int32
		if err = rows.Scan(&blockSize); err != nil {
			log.Errorf("Unable to scan for block size field: %v", err)
		}
		blockSizes = append(blockSizes, blockSize)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}

	return blockSizes, nil
}

// StoreStakeInfoExtended stores the extended stake info in the database.
func (db *DB) StoreStakeInfoExtended(si *apitypes.StakeInfoExtended) error {
	if db.BlockCache != nil && db.BlockCache.IsEnabled() {
		if err := db.BlockCache.StoreStakeInfo(si); err != nil {
			return fmt.Errorf("APICache failed to store stake info: %v", err)
		}
	}

	stmt, err := db.Prepare(db.insertStakeInfoExtendedSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// If input block data lacks non-nil PoolInfo, set to a zero-value
	// TicketPoolInfo.
	if si.PoolInfo == nil {
		si.PoolInfo = new(apitypes.TicketPoolInfo)
	}

	winners := strings.Join(si.PoolInfo.Winners, ";")

	res, err := stmt.Exec(&si.Hash, &si.Feeinfo.Height,
		&si.Feeinfo.Number, &si.Feeinfo.Min, &si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg, &winners)
	if err != nil {
		return db.filterError(err)
	}

	db.mtx.Lock()
	defer db.mtx.Unlock()
	if err = logDBResult(res); err == nil {
		height := int64(si.Feeinfo.Height)
		if height > db.dbStakeInfoHeight {
			db.dbStakeInfoHeight = height
		}
	}
	return err
}

// Delete stake info for block with the given hash. The number of rows deleted
// is returned.
func (db *DB) deleteStakeInfo(blockhash string) (int64, error) {
	res, err := db.Exec(db.deleteBlockByHashSQL, blockhash)
	if err != nil {
		return 0, db.filterError(err)
	}
	return res.RowsAffected()
}

// Delete stake info for block at the given height on the main chain. The number
// of rows deleted is returned.
func (db *DB) deleteStakeInfoHeightMainchain(height int64) (int64, error) {
	res, err := db.Exec(db.deleteBlockByHeightMainChainSQL, height)
	if err != nil {
		return 0, db.filterError(err)
	}
	return res.RowsAffected()
}

// RetrieveLatestStakeInfoExtended returns the extended stake info for the best
// block.
func (db *DB) RetrieveLatestStakeInfoExtended() (*apitypes.StakeInfoExtended, error) {
	si := apitypes.NewStakeInfoExtended()

	var winners string
	err := db.QueryRow(db.getLatestStakeInfoExtendedSQL).Scan(
		&si.Hash, &si.Feeinfo.Height, &si.Feeinfo.Number, &si.Feeinfo.Min,
		&si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg, &winners)
	if err != nil {
		return nil, err
	}
	si.PoolInfo.Winners = splitToArray(winners)

	// See if this block should be cached.
	usingBlockCache := db.BlockCache != nil && db.BlockCache.IsEnabled()
	if usingBlockCache {
		if db.BlockCache.GetStakeInfoByHash(si.Hash) == nil {
			// Cache miss, cache the data.
			err = db.BlockCache.StoreStakeInfo(si)
			if err != nil {
				log.Warnf("Failed to cache stake info for block %s: %v", si.Hash, err)
				// Do not return the error.
			}
		}
	}

	return si, nil
}

// RetrieveBestStakeHeight retreives the height of the best (mainchain) block in
// the stake table.
func (db *DB) RetrieveBestStakeHeight() (int64, error) {
	var height int64
	err := db.QueryRow(db.getBestStakeHeightSQL).Scan(&height)
	if err != nil {
		return -1, err
	}
	return height, nil
}

// RetrieveHighestStakeHeight retrieves the height of the highest block in the
// stake table without regard to mainchain status.
func (db *DB) RetrieveHighestStakeHeight() (int64, error) {
	var height int64
	err := db.QueryRow(db.getHighestStakeHeightSQL).Scan(&height)
	if err != nil {
		return -1, err
	}
	return height, nil
}

// RetrieveStakeInfoExtended returns the extended stake info for the block at
// height ind.
func (db *DB) RetrieveStakeInfoExtended(ind int64) (*apitypes.StakeInfoExtended, error) {
	// First try the block cache.
	usingBlockCache := db.BlockCache != nil && db.BlockCache.IsEnabled()
	if usingBlockCache {
		si := db.BlockCache.GetStakeInfo(ind)
		if si != nil {
			// Cache hit!
			return si, nil
		}
		// Cache miss necessitates a DB query.
	}

	si := apitypes.NewStakeInfoExtended()

	var winners string
	err := db.QueryRow(db.getStakeInfoExtendedByHeightSQL, ind).Scan(&si.Hash, &si.Feeinfo.Height,
		&si.Feeinfo.Number, &si.Feeinfo.Min, &si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg, &winners)
	if err != nil {
		return nil, err
	}
	si.PoolInfo.Winners = splitToArray(winners)

	if usingBlockCache {
		// This is a cache miss since hits return early.
		err = db.BlockCache.StoreStakeInfo(si)
		if err != nil {
			log.Warnf("Failed to cache stake info for block at %d: %v", ind, err)
			// Do not return the error.
		}
	}

	return si, nil
}

func (db *DB) RetrieveStakeInfoExtendedByHash(blockhash string) (*apitypes.StakeInfoExtended, error) {
	// First try the block cache.
	usingBlockCache := db.BlockCache != nil && db.BlockCache.IsEnabled()
	if usingBlockCache {
		si := db.BlockCache.GetStakeInfoByHash(blockhash)
		if si != nil {
			// Cache hit!
			return si, nil
		}
		// Cache miss necessitates a DB query.
	}

	si := apitypes.NewStakeInfoExtended()

	var winners string
	err := db.QueryRow(db.getStakeInfoExtendedByHashSQL, blockhash).Scan(&si.Hash, &si.Feeinfo.Height,
		&si.Feeinfo.Number, &si.Feeinfo.Min, &si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg, &winners)
	if err != nil {
		return nil, err
	}
	si.PoolInfo.Winners = splitToArray(winners)

	if usingBlockCache {
		// This is a cache miss since hits return early.
		err = db.BlockCache.StoreStakeInfo(si)
		if err != nil {
			log.Warnf("Failed to cache stake info for block %s: %v", blockhash, err)
			// Do not return the error.
		}
	}

	return si, nil
}

// Copy the file.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

// JustifyTableStructures updates an old structure that wasn't indexing
// sidechains. It could and should be removed in a future version. The block
// summary table got two new boolean columns, `is_mainchain` and `is_valid`, and
// the Primary key was changed from height to hash. The stake info table got a
// `hash` column and the primary key was switched from height to hash.
func (db *DB) JustifyTableStructures(dbInfo *DBInfo) error {

	// Grab the column info. Right now, just counting them, but could be looked at more closely
	// Each tuple is a row with columns [cid, name, type, notnull, dflt_value, pk]
	rows, err := db.Query(db.getBlockSummaryTableInfo)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return err
	}
	defer rows.Close()

	rowCounter := 0
	for rows.Next() {
		rowCounter += 1
	}

	// It simply checks whether there are enough columns for the current structure
	if rowCounter >= 12 {
		return nil
	}

	log.Info("Detected old SQLite table structure. Updating.")

	// Create a backup file, if one hasn't already been created.
	directory := filepath.Dir(dbInfo.FileName)
	bkpPath := filepath.Join(directory, "dcrdata.nosidechains-bkp.db")
	_, err = os.Stat(bkpPath)
	if err != nil {
		if os.IsNotExist(err) {
			err = copyFile(dbInfo.FileName, bkpPath)
			if err != nil {
				log.Errorf("Failed to backup %s: %v", dbInfo.FileName, err)
				return err
			}
		} else {
			log.Errorf("Error retrieving FileInfo for %s: %v", dbInfo.FileName, err)
			return err
		}
	}

	tmpSummaryTableName := TableNameSummaries + "_temp"
	tmpStakeTableName := TableNameStakeInfo + "_temp"

	queries := make([]string, 0, 8)
	queries = append(queries, fmt.Sprintf(db.rawCreateBlockSummaryStmt, tmpSummaryTableName))
	queries = append(queries, fmt.Sprintf(`INSERT INTO %s SELECT hash, height, size, diff,
		sdiff, time, poolsize, poolval, poolavg, winners, 1, 1 FROM %s;`,
		tmpSummaryTableName, TableNameSummaries))
	queries = append(queries, fmt.Sprintf("DROP TABLE %s;", TableNameSummaries))
	queries = append(queries, fmt.Sprintf("ALTER TABLE %s RENAME TO %s;",
		tmpSummaryTableName, TableNameSummaries))
	queries = append(queries, fmt.Sprintf(db.rawCreateStakeInfoExtendedStmt, tmpStakeTableName))
	queries = append(queries, fmt.Sprintf(`INSERT INTO %[1]s SELECT %[2]s.hash, %[3]s.height, num_tickets,
		fee_min, fee_max, fee_mean, fee_med, fee_std, %[3]s.sdiff, window_num, window_ind, pool_size,
		pool_val,pool_valavg, %[3]s.winners FROM %[3]s JOIN %[2]s ON
		%[3]s.height = %[2]s.height`,
		tmpStakeTableName, TableNameSummaries, TableNameStakeInfo))
	queries = append(queries, fmt.Sprintf("DROP TABLE %s;", TableNameStakeInfo))
	queries = append(queries, fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", tmpStakeTableName, TableNameStakeInfo))

	transaction, err := db.Begin()
	if err != nil {
		log.Errorf("Failed to start a transaction: \n", err)
		return err
	}
	for _, query := range queries {
		_, err = transaction.Exec(query)
		if err != nil {
			log.Errorf("Failed updating SQLite table structure: \n", err)
			transaction.Rollback()
			return err
		}
	}

	err = transaction.Commit()
	if err != nil {
		log.Error("Failed to commit SQL transaction after table reorg.")
		return err
	}

	// Clean up the file a little bit
	_, err = db.Exec("VACUUM;")
	if err != nil {
		log.Error("Failed to VACUUM SQLite database.")
	}

	// Run the stake info height query with the block summary table join where
	// is_mainchain=true, and udpate the cached stake info height.
	db.dbStakeInfoHeight, err = db.getStakeInfoHeight()
	// All rows in the block summary table got is_mainchain (and is_valid) set
	// to true, so it is not necessary to update the block summary height.

	return err
}

func logDBResult(res sql.Result) error {
	if log.Level() > slog.LevelTrace {
		return nil
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	rowCnt, err := res.RowsAffected()
	if err != nil {
		return err
	}

	log.Tracef("ID = %d, affected = %d", lastID, rowCnt)

	return nil
}

// splitToArray splits a string into multiple strings using ";" to delimit.
func splitToArray(str string) []string {
	if str == "" {
		// Return a non-nil empty slice.
		return []string{}
	}
	return strings.Split(str, ";")
}

// getTip returns the last block stored using StoreBlockSummary.
// If no block has been stored yet, it returns the best block in the database.
func (db *DB) getTip() (*apitypes.BlockDataBasic, error) {
	db.mtx.RLock()
	defer db.mtx.RUnlock()
	if db.lastStoredBlock != nil {
		return db.lastStoredBlock, nil
	}
	return db.RetrieveLatestBlockSummary()
}
