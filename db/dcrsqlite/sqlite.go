// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrsqlite

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/btcsuite/btclog"

	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/blockdata"
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
)

// DB is a wrapper around sql.DB that adds methods for storing and retrieving
// chain data. Use InitDB to get a new instance. This may be unexported in the
// future.
type DB struct {
	*sql.DB
	sync.RWMutex
	dbSummaryHeight                                              int64
	dbStakeInfoHeight                                            int64
	getPoolSQL, getPoolRangeSQL, getPoolValSizeRangeSQL          string
	getPoolByHashSQL                                             string
	getWinnersByHashSQL, getWinnersSQL                           string
	getSDiffSQL, getSDiffRangeSQL                                string
	getLatestBlockSQL                                            string
	getBlockSQL, insertBlockSQL                                  string
	getBlockByHashSQL, getBlockByTimeRangeSQL, getBlockByTimeSQL string
	getBlockHashSQL, getBlockHeightSQL                           string
	getBlockSizeRangeSQL                                         string
	getBestBlockHashSQL, getBestBlockHeightSQL                   string
	getLatestStakeInfoExtendedSQL                                string
	getStakeInfoExtendedSQL, insertStakeInfoExtendedSQL          string
	getStakeInfoWinnersSQL                                       string
}

// NewDB creates a new DB instance with pre-generated sql statements from an
// existing sql.DB. Use InitDB to create a new DB without having a sql.DB.
// TODO: if this db exists, figure out best heights
func NewDB(db *sql.DB) (*DB, error) {
	d := DB{
		DB:                db,
		dbSummaryHeight:   -1,
		dbStakeInfoHeight: -1,
	}

	// Ticket pool queries
	d.getPoolSQL = fmt.Sprintf(`select hash, poolsize, poolval, poolavg, winners`+
		` from %s where height = ?`, TableNameSummaries)
	d.getPoolByHashSQL = fmt.Sprintf(`select height, poolsize, poolval, poolavg, winners`+
		` from %s where hash = ?`, TableNameSummaries)
	d.getPoolRangeSQL = fmt.Sprintf(`select height, hash, poolsize, poolval, poolavg, winners `+
		`from %s where height between ? and ?`, TableNameSummaries)
	d.getPoolValSizeRangeSQL = fmt.Sprintf(`select poolsize, poolval `+
		`from %s where height between ? and ?`, TableNameSummaries)
	d.getWinnersSQL = fmt.Sprintf(`select hash, winners from %s where height = ?`,
		TableNameSummaries)
	d.getWinnersByHashSQL = fmt.Sprintf(`select height, winners from %s where hash = ?`,
		TableNameSummaries)

	d.getSDiffSQL = fmt.Sprintf(`select sdiff from %s where height = ?`,
		TableNameSummaries)
	d.getSDiffRangeSQL = fmt.Sprintf(`select sdiff from %s where height between ? and ?`,
		TableNameSummaries)

	// Block queries
	d.getBlockSQL = fmt.Sprintf(`select * from %s where height = ?`, TableNameSummaries)
	d.getBlockByHashSQL = fmt.Sprintf(`select * from %s where hash = ?`, TableNameSummaries)
	d.getLatestBlockSQL = fmt.Sprintf(`SELECT * FROM %s ORDER BY height DESC LIMIT 0, 1`,
		TableNameSummaries)
	d.insertBlockSQL = fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
            height, size, hash, diff, sdiff, time, poolsize, poolval, poolavg, winners
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, TableNameSummaries)

	d.getBlockSizeRangeSQL = fmt.Sprintf(`select size from %s where height between ? and ?`,
		TableNameSummaries)
	d.getBlockByTimeRangeSQL = fmt.Sprintf(`select * from %s where time between ? and ? ORDER BY time LIMIT ?`,
		TableNameSummaries)
	d.getBlockByTimeSQL = fmt.Sprintf(`select * from %s where time = ?`,
		TableNameSummaries)

	d.getBestBlockHashSQL = fmt.Sprintf(`select hash from %s ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)
	d.getBestBlockHeightSQL = fmt.Sprintf(`select height from %s ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)

	d.getBlockHashSQL = fmt.Sprintf(`select hash from %s where height = ?`, TableNameSummaries)
	d.getBlockHeightSQL = fmt.Sprintf(`select height from %s where hash = ?`, TableNameSummaries)

	// Stake info queries
	d.getStakeInfoExtendedSQL = fmt.Sprintf(`select * from %s where height = ?`,
		TableNameStakeInfo)
	d.getStakeInfoWinnersSQL = fmt.Sprintf(`select winners from %s where height = ?`,
		TableNameStakeInfo)
	d.getLatestStakeInfoExtendedSQL = fmt.Sprintf(
		`SELECT * FROM %s ORDER BY height DESC LIMIT 0, 1`, TableNameStakeInfo)
	d.insertStakeInfoExtendedSQL = fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
            height, num_tickets, fee_min, fee_max, fee_mean, fee_med, fee_std,
			sdiff, window_num, window_ind, pool_size, pool_val, pool_valavg, winners
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `, TableNameStakeInfo)

	var err error
	if d.dbSummaryHeight, err = d.GetBlockSummaryHeight(); err != nil {
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
	db, err := sql.Open("sqlite3", dbInfo.FileName)
	if err != nil || db == nil {
		return nil, err
	}

	createBlockSummaryStmt := fmt.Sprintf(`
        PRAGMA cache_size = 32768;
        pragma synchronous = OFF;
        create table if not exists %s(
            height INTEGER PRIMARY KEY,
            size INTEGER,
            hash TEXT,
            diff FLOAT,
            sdiff FLOAT,
            time INTEGER,
            poolsize INTEGER,
            poolval FLOAT,
			poolavg FLOAT,
			winners TEXT
        );
        `, TableNameSummaries)

	_, err = db.Exec(createBlockSummaryStmt)
	if err != nil {
		log.Errorf("%q: %s\n", err, createBlockSummaryStmt)
		return nil, err
	}

	createStakeInfoExtendedStmt := fmt.Sprintf(`
        PRAGMA cache_size = 32768;
        pragma synchronous = OFF;
        create table if not exists %s(
            height INTEGER PRIMARY KEY,
            num_tickets INTEGER,
            fee_min FLOAT, fee_max FLOAT, fee_mean FLOAT,
			fee_med FLOAT, fee_std FLOAT,
			sdiff FLOAT, window_num INTEGER, window_ind INTEGER,
			pool_size INTEGER, pool_val FLOAT, pool_valavg FLOAT,
			winners TEXT
        );
        `, TableNameStakeInfo)

	_, err = db.Exec(createStakeInfoExtendedStmt)
	if err != nil {
		log.Errorf("%q: %s\n", err, createStakeInfoExtendedStmt)
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}
	return NewDB(db)
}

// DBDataSaver models a DB with a channel to communicate new block height to the web interface
type DBDataSaver struct {
	*DB
	updateStatusChan chan uint32
}

// Store satisfies the blockdata.BlockDataSaver interface
func (db *DBDataSaver) Store(data *blockdata.BlockData, _ *wire.MsgBlock) error {
	summary := data.ToBlockSummary()
	err := db.DB.StoreBlockSummary(&summary)
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

// StoreBlockSummary attempts to stores the block data in the database and
// returns an error on failure
func (db *DB) StoreBlockSummary(bd *apitypes.BlockDataBasic) error {
	stmt, err := db.Prepare(db.insertBlockSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	winners := strings.Join(bd.PoolInfo.Winners, ";")

	res, err := stmt.Exec(&bd.Height, &bd.Size, &bd.Hash,
		&bd.Difficulty, &bd.StakeDiff, &bd.Time,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners)
	if err != nil {
		return err
	}

	db.Lock()
	defer db.Unlock()
	if err = logDBResult(res); err == nil {
		// TODO: atomic with CAS
		//log.Debugf("Store height: %v", bd.Height)
		height := int64(bd.Height)
		if height > db.dbSummaryHeight {
			db.dbSummaryHeight = height
		}
	}

	return err
}

// GetBestBlockHash returns the hash of the best block
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

// GetBlockSummaryHeight returns the largest block height for which the database
// can provide a block summary
func (db *DB) GetBlockSummaryHeight() (int64, error) {
	db.RLock()
	defer db.RUnlock()
	if db.dbSummaryHeight < 0 {
		height, err := db.RetrieveBestBlockHeight()
		// No rows returned is not considered an error
		if err != nil && err != sql.ErrNoRows {
			return -1, fmt.Errorf("RetrieveBestBlockHeight failed: %v", err)
		}
		if err == sql.ErrNoRows {
			log.Warn("Block summary DB is empty.")
		} else {
			db.dbSummaryHeight = height
		}
	}
	return db.dbSummaryHeight, nil
}

// GetStakeInfoHeight returns the largest block height for which the database
// can provide a stake info
func (db *DB) GetStakeInfoHeight() (int64, error) {
	db.RLock()
	defer db.RUnlock()
	if db.dbStakeInfoHeight < 0 {
		si, err := db.RetrieveLatestStakeInfoExtended()
		// No rows returned is not considered an error
		if err != nil && err != sql.ErrNoRows {
			return -1, fmt.Errorf("RetrieveLatestStakeInfoExtended failed: %v", err)
		}
		if err == sql.ErrNoRows {
			log.Warn("Stake info DB is empty.")
			return -1, nil
		}
		db.dbStakeInfoHeight = int64(si.Feeinfo.Height)
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
		return nil, nil, fmt.Errorf("Cannot retrieve pool info range (%d<%d)",
			ind1, ind0)
	}
	db.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.RUnlock()
		return nil, nil, fmt.Errorf("Cannot retrieve pool info range [%d,%d], have height %d",
			ind1, ind0, db.dbSummaryHeight)
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
		tpi.Winners = strings.Split(winners, ";")
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
	tpi.Winners = strings.Split(winners, ";")
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

	return strings.Split(winners, ";"), hash, err
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

	return strings.Split(winners, ";"), height, err
}

// RetrievePoolInfoByHash returns ticket pool info for blockhash hash
func (db *DB) RetrievePoolInfoByHash(hash string) (*apitypes.TicketPoolInfo, error) {
	tpi := new(apitypes.TicketPoolInfo)
	var winners string
	err := db.QueryRow(db.getPoolByHashSQL, hash).Scan(&tpi.Height, &tpi.Size,
		&tpi.Value, &tpi.ValAvg, &winners)
	tpi.Winners = strings.Split(winners, ";")
	return tpi, err
}

// RetrievePoolValAndSizeRange returns an array each of the pool values and sizes
// for block range ind0 to ind1
func (db *DB) RetrievePoolValAndSizeRange(ind0, ind1 int64) ([]float64, []float64, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []float64{}, []float64{}, nil
	}
	if N < 0 {
		return nil, nil, fmt.Errorf("Cannot retrieve pool val and size range (%d<%d)",
			ind1, ind0)
	}
	db.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.RUnlock()
		return nil, nil, fmt.Errorf("Cannot retrieve pool val and size range [%d,%d], have height %d",
			ind1, ind0, db.dbSummaryHeight)
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
		log.Warnf("Retrieved pool values (%d) not expected number (%d)", len(poolsizes), N)
	}

	return poolvals, poolsizes, nil
}

// RetrieveSDiffRange returns an array of stake difficulties for block range ind0 to
// ind1
func (db *DB) RetrieveSDiffRange(ind0, ind1 int64) ([]float64, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []float64{}, nil
	}
	if N < 0 {
		return nil, fmt.Errorf("Cannot retrieve sdiff range (%d<%d)",
			ind1, ind0)
	}
	db.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.RUnlock()
		return nil, fmt.Errorf("Cannot retrieve sdiff range [%d,%d], have height %d",
			ind1, ind0, db.dbSummaryHeight)
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
		var bd apitypes.BlockDataBasic
		if err = rows.Scan(&bd.Height, &bd.Size, &bd.Hash,
			&bd.Difficulty, &bd.StakeDiff, &bd.Time,
			&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg); err != nil {
			log.Errorf("Unable to scan for block fields")
		}
		blocks = append(blocks, bd)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}
	return blocks, nil
}

// RetrieveSDiff returns the stake difficulty for block ind
func (db *DB) RetrieveSDiff(ind int64) (float64, error) {
	var sdiff float64
	err := db.QueryRow(db.getSDiffSQL, ind).Scan(&sdiff)
	return sdiff, err
}

func stringSliceToBoolSlice(ss []string) ([]bool, error) {
	bs := make([]bool, len(ss))
	for i := range ss {
		var err error
		bs[i], err = strconv.ParseBool(ss[i])
		if err != nil {
			return nil, err
		}
	}
	return bs, nil
}

// RetrieveLatestBlockSummary returns the block summary for the best block
func (db *DB) RetrieveLatestBlockSummary() (*apitypes.BlockDataBasic, error) {
	bd := new(apitypes.BlockDataBasic)

	var winners string
	err := db.QueryRow(db.getLatestBlockSQL).Scan(&bd.Height, &bd.Size,
		&bd.Hash, &bd.Difficulty, &bd.StakeDiff, &bd.Time,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners)
	if err != nil {
		return nil, err
	}
	bd.PoolInfo.Winners = strings.Split(winners, ";")

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
	bd := new(apitypes.BlockDataBasic)

	var winners string
	err := db.QueryRow(db.getBlockByHashSQL, hash).Scan(&bd.Height, &bd.Size, &bd.Hash,
		&bd.Difficulty, &bd.StakeDiff, &bd.Time,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners)
	if err != nil {
		return nil, err
	}
	bd.PoolInfo.Winners = strings.Split(winners, ";")

	return bd, nil
}

// RetrieveBlockSummary returns basic block data for block ind
func (db *DB) RetrieveBlockSummary(ind int64) (*apitypes.BlockDataBasic, error) {
	bd := new(apitypes.BlockDataBasic)

	// Three different ways

	// 1. chained QueryRow/Scan only
	var winners string
	err := db.QueryRow(db.getBlockSQL, ind).Scan(&bd.Height, &bd.Size, &bd.Hash,
		&bd.Difficulty, &bd.StakeDiff, &bd.Time,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg,
		&winners)
	if err != nil {
		return nil, err
	}
	bd.PoolInfo.Winners = strings.Split(winners, ";")

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
		return nil, fmt.Errorf("Cannot retrieve block size range (%d<%d)",
			ind1, ind0)
	}
	db.RLock()
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		defer db.RUnlock()
		return nil, fmt.Errorf("Cannot retrieve block size range [%d,%d], have height %d",
			ind1, ind0, db.dbSummaryHeight)
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

// StoreStakeInfoExtended stores the extended stake info in the database
func (db *DB) StoreStakeInfoExtended(si *apitypes.StakeInfoExtended) error {
	stmt, err := db.Prepare(db.insertStakeInfoExtendedSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	winners := strings.Join(si.PoolInfo.Winners, ";")

	res, err := stmt.Exec(&si.Feeinfo.Height,
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

// RetrieveLatestStakeInfoExtended returns the extended stake info for the best block
func (db *DB) RetrieveLatestStakeInfoExtended() (*apitypes.StakeInfoExtended, error) {
	si := new(apitypes.StakeInfoExtended)

	var winners string
	err := db.QueryRow(db.getLatestStakeInfoExtendedSQL).Scan(
		&si.Feeinfo.Height, &si.Feeinfo.Number, &si.Feeinfo.Min,
		&si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg, &winners)
	if err != nil {
		return nil, err
	}

	si.PoolInfo.Winners = strings.Split(winners, ";")

	return si, nil
}

// RetrieveStakeInfoExtended returns the extended stake info for block ind
func (db *DB) RetrieveStakeInfoExtended(ind int64) (*apitypes.StakeInfoExtended, error) {
	si := new(apitypes.StakeInfoExtended)

	var winners string
	err := db.QueryRow(db.getStakeInfoExtendedSQL, ind).Scan(&si.Feeinfo.Height,
		&si.Feeinfo.Number, &si.Feeinfo.Min, &si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg, &winners)
	if err != nil {
		return nil, err
	}

	si.PoolInfo.Winners = strings.Split(winners, ";")

	return si, nil
}

// RetrieveWinners returns the winners for block ind
// func (db *DB) RetrieveWinners(ind int64) ([]string, error) {
// 	var winners string
// 	err := db.QueryRow(db.getStakeInfoWinnersSQL, ind).Scan(&winners)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return strings.Split(winners, ";"), nil
// }

func logDBResult(res sql.Result) error {
	if log.Level() > btclog.LevelTrace {
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
