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
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/v4/api/types"
	"github.com/decred/dcrdata/v4/blockdata"
	"github.com/decred/dcrdata/v4/db/dbtypes"
	"github.com/decred/slog"
	_ "github.com/mattn/go-sqlite3" // register sqlite driver with database/sql
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

	// Guard cached data: lastStoredBlock, dbSummaryHeight, dbStakeInfoHeight
	sync.RWMutex

	// lastStoredBlock caches data for the most recently stored block, and is
	// used to optimize getTip.
	lastStoredBlock *apitypes.BlockDataBasic

	// dbSummaryHeight is set when a block's summary data is stored.
	dbSummaryHeight int64
	// dbSummaryHeight is set when a block's stake info is stored.
	dbStakeInfoHeight int64

	// Block summary table queries
	insertBlockSQL                                               string
	getPoolSQL, getPoolRangeSQL                                  string
	getPoolByHashSQL                                             string
	getPoolValSizeRangeSQL, getAllPoolValSize                    string
	getWinnersByHashSQL, getWinnersSQL                           string
	getDifficulty                                                string
	getSDiffSQL, getSDiffRangeSQL                                string
	getBlockSQL, getLatestBlockSQL                               string
	getBlockByHashSQL, getBlockByTimeRangeSQL, getBlockByTimeSQL string
	getBlockHashSQL, getBlockHeightSQL                           string
	getBlockSizeRangeSQL                                         string
	getBestBlockHashSQL, getBestBlockHeightSQL                   string
	getMainchainStatusSQL, invalidateBlockSQL                    string
	setHeightToSideChainSQL                                      string
	deleteBlockByHeightMainChainSQL, deleteBlockByHashSQL        string

	// Stake info table queries
	insertStakeInfoExtendedSQL                                    string
	getHighestStakeHeight                                         string
	getStakeInfoExtendedByHashSQL                                 string
	deleteStakeInfoByHeightMainChainSQL, deleteStakeInfoByHashSQL string

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
func NewDB(db *sql.DB) (*DB, error) {
	d := DB{
		DB:                db,
		dbSummaryHeight:   -1,
		dbStakeInfoHeight: -1,
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
	d.getLatestBlockSQL = fmt.Sprintf(`SELECT * FROM %s
		ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)

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
	d.getHighestStakeHeight = fmt.Sprintf(
		`SELECT height FROM %s ORDER BY height DESC LIMIT 0, 1`, TableNameStakeInfo)

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

	// Table metadata
	d.getBlockSummaryTableInfo = fmt.Sprintf(`PRAGMA table_info(%s);`, TableNameSummaries)
	d.getStakeInfoExtendedTableInfo = fmt.Sprintf(`PRAGMA table_info(%s);`, TableNameStakeInfo)

	var err error
	if d.dbSummaryHeight, err = d.getBlockSummaryHeight(); err != nil {
		return nil, err
	}
	if d.dbStakeInfoHeight, err = d.GetStakeInfoHeight(); err != nil {
		return nil, err
	}

	return &d, nil
}

// InitDB creates a new DB instance from a DBInfo containing the name of the
// file used to back the underlying sql database.
func InitDB(dbInfo *DBInfo) (*DB, error) {
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

	// SQLite does not handle concurrent writes internally, necessitating a
	// limitation of just 1 open connecton. With a busy_timeout set, this is
	// less important.
	db.SetMaxOpenConns(1)

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

	dataBase, err := NewDB(db)
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

// DBDataSaver models a DB with a channel to communicate new block height to the
// web interface.
type DBDataSaver struct {
	*DB
	updateStatusChan chan uint32
}

// Store satisfies the blockdata.BlockDataSaver interface.
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

	// Set is_mainchain false for every block at this height
	err = db.DB.setHeightToSideChain(int64(msgBlock.Header.Height))
	if err != nil {
		return err
	}

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

func (db *DB) deleteBlock(blockhash string) (NSummaryRows, NStakeInfoRows int64, err error) {
	// Remove rows from block summary table.
	var res sql.Result
	res, err = db.Exec(db.deleteBlockByHashSQL, blockhash)
	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
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

	// Update dbSummaryHeight and dbStakeInfoHeight.
	db.Lock()
	defer db.Unlock()

	var height int64
	height, err = db.getBlockSummaryHeight()
	if err != nil {
		return
	}
	db.dbSummaryHeight = height

	height, err = db.getStakeInfoHeight()
	if err != nil {
		return
	}
	db.dbStakeInfoHeight = height

	// Reset lastStoredBlock. It will be loaded on demand by getTip, and updated
	// by StoreBlock.
	db.lastStoredBlock = nil

	return
}

// Delete summary data for block at the given height on the main chain. The
// number of rows deleted is returned.
func (db *DB) deleteBlockHeightMainchain(height int64) (int64, error) {
	res, err := db.Exec(db.deleteBlockByHeightMainChainSQL, height)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Invalidate block with the given hash.
func (db *DB) invalidateBlock(blockhash string) error {
	_, err := db.Exec(db.invalidateBlockSQL, blockhash)
	return err
}

// Sets the is_mainchain field to false for the given block in the database.
func (db *DB) setHeightToSideChain(height int64) error {
	_, err := db.Exec(db.setHeightToSideChainSQL, height)
	return err
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
		&bd.Difficulty, &bd.StakeDiff, bd.Time.S.T.Unix(),
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners, &isMainchain, &isValid)
	if err != nil {
		return err
	}

	// Update the DB block summary height.
	db.Lock()
	defer db.Unlock()
	if err = logDBResult(res); err == nil {
		// TODO: atomic with CAS
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

// getBlockSummaryHeight returns the largest block height for which the database
// can provide a block summary. When the table is empty, height -1 and error nil
// are returned. The error value will never be sql.ErrNoRows. Usually
// GetBlockSummaryHeight, which caches the most recently stored block height,
// shoud be used instead.
func (db *DB) getBlockSummaryHeight() (int64, error) {
	// Query the block summary table for the best main chain block height.
	height, err := db.RetrieveBestBlockHeight()
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

// GetBlockSummaryHeight returns the largest block height for which the database
// can provide a block summary. A cached best block summary height will be
// returned when available to avoid unnecessary DB queries.
func (db *DB) GetBlockSummaryHeight() (int64, error) {
	db.RLock()
	defer db.RUnlock()
	if db.dbSummaryHeight < 0 {
		h, err := db.getBlockSummaryHeight()
		if err == nil {
			db.dbSummaryHeight = h
		}
		return h, err
	}
	return db.dbSummaryHeight, nil
}

// getStakeInfoHeight returns the largest block height for which the database
// can provide stake info data. When the table is empty, height -1 and error nil
// are returned. The error value will never be sql.ErrNoRows. Usually
// GetStakeInfoHeight, which caches the most recently stored block height, shoud
// be used instead.
func (db *DB) getStakeInfoHeight() (int64, error) {
	// Query the stake info table for the best main chain block height.
	height, err := db.RetrieveBestStakeHeight()
	if err == nil {
		return height, nil
	}

	// No rows returned is not considered an error. When the table is empty,
	// return -1 height and nil error, but log a warning.
	if err == sql.ErrNoRows {
		log.Warn("Stake info table is empty.")
		return -1, nil
	}

	return -1, fmt.Errorf("RetrieveBestStakeHeight failed: %v", err)
}

// GetStakeInfoHeight returns the largest block height for which the database
// can provide a stake info
func (db *DB) GetStakeInfoHeight() (int64, error) {
	db.RLock()
	defer db.RUnlock()
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
	db.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.RUnlock()
		return nil, nil, fmt.Errorf("Cannot retrieve pool info range [%d,%d], have height %d",
			ind0, ind1, db.dbSummaryHeight)
	}
	db.RUnlock()

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
	db.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.RUnlock()
		return nil, nil, fmt.Errorf("Cannot retrieve pool val and size range [%d,%d], have height %d",
			ind0, ind1, db.dbSummaryHeight)
	}
	db.RUnlock()

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

		chartsData.Time = append(chartsData.Time, dbtypes.TimeDef{T: time.Unix(timestamp, 0)})
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
	db.RLock()
	defer db.RUnlock()

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
	db.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.RUnlock()
		return nil, fmt.Errorf("Cannot retrieve sdiff range [%d,%d], have height %d",
			ind0, ind1, db.dbSummaryHeight)
	}
	db.RUnlock()

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
		bd.Time = apitypes.TimeAPI{S: dbtypes.TimeDef{T: time.Unix(timestamp, 0)}}
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
	err := db.QueryRow(db.getLatestBlockSQL).Scan(&bd.Hash, &bd.Height, &bd.Size,
		&bd.Difficulty, &bd.StakeDiff, &timestamp,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners, &isMainchain, &isValid)
	if err != nil {
		return nil, err
	}
	bd.Time = apitypes.TimeAPI{S: dbtypes.TimeDef{T: time.Unix(timestamp, 0)}}
	bd.PoolInfo.Winners = splitToArray(winners)
	return bd, nil
}

// RetrieveBlockHash returns the block hash for block ind
func (db *DB) RetrieveBlockHash(ind int64) (string, error) {
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

// RetrieveBestBlockHash returns the block hash for the best block
func (db *DB) RetrieveBestBlockHash() (string, error) {
	var blockHash string
	err := db.QueryRow(db.getBestBlockHashSQL).Scan(&blockHash)
	return blockHash, err
}

// RetrieveBestBlockHeight returns the block height for the best block
func (db *DB) RetrieveBestBlockHeight() (int64, error) {
	var blockHeight int64
	err := db.QueryRow(db.getBestBlockHeightSQL).Scan(&blockHeight)
	return blockHeight, err
}

// RetrieveBlockSummaryByHash returns basic block data for a block given its hash
func (db *DB) RetrieveBlockSummaryByHash(hash string) (*apitypes.BlockDataBasic, error) {
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
	bd.Time = apitypes.TimeAPI{S: dbtypes.TimeDef{T: time.Unix(timestamp, 0)}}
	bd.PoolInfo.Winners = splitToArray(winners)
	return bd, nil
}

// RetrieveBlockSummary returns basic block data for block ind
func (db *DB) RetrieveBlockSummary(ind int64) (*apitypes.BlockDataBasic, error) {
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
	bd.Time = apitypes.TimeAPI{S: dbtypes.TimeDef{T: time.Unix(timestamp, 0)}}
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

	return bd, nil
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
	db.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.RUnlock()
		return nil, fmt.Errorf("Cannot retrieve block size range [%d,%d], have height %d",
			ind0, ind1, db.dbSummaryHeight)
	}
	db.RUnlock()

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
			log.Errorf("Unable to scan for sdiff fields: %v", err)
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
		return err
	}

	db.Lock()
	defer db.Unlock()
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
		return 0, err
	}
	return res.RowsAffected()
}

// Delete stake info for block at the given height on the main chain. The number
// of rows deleted is returned.
func (db *DB) deleteStakeInfoHeightMainchain(height int64) (int64, error) {
	res, err := db.Exec(db.deleteBlockByHeightMainChainSQL, height)
	if err != nil {
		return 0, err
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
	return si, nil
}

// Retreives the height of the highest block in the stake table.
func (db *DB) RetrieveBestStakeHeight() (int64, error) {
	var height int64
	err := db.QueryRow(db.getHighestStakeHeight).Scan(&height)
	if err != nil {
		return -1, err
	}
	return height, nil
}

// RetrieveStakeInfoExtended returns the extended stake info for the block at
// height ind.
func (db *DB) RetrieveStakeInfoExtended(ind int64) (*apitypes.StakeInfoExtended, error) {
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
	return si, nil
}

func (db *DB) RetrieveStakeInfoExtendedByHash(blockhash string) (*apitypes.StakeInfoExtended, error) {
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

// JustifyTableStructure updates an old structure that wasn't indexing
// sidechains. It could and should be removed in a future version. The block
// summary table got two new boolean columns, `is_mainchain` and `is_valid`,
// and the Primary key was changed from height to hash.
// The stake info table got a `hash` column and the primary key was switched from height to hash
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

	return nil
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
	db.RLock()
	defer db.RUnlock()
	if db.lastStoredBlock != nil {
		return db.lastStoredBlock, nil
	}
	return db.RetrieveLatestBlockSummary()
}
