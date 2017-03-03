package dcrsqlite

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	_ "github.com/mattn/go-sqlite3"
)

// BlockSummaryDatabaser is the interface for a block data saving database
type BlockSummaryDatabaser interface {
	StoreStakeInfoExtended(bd *apitypes.StakeInfoExtended) error
	RetrieveStakeInfoExtended(ind int64) (*apitypes.StakeInfoExtended, error)
}

// StakeInfoDatabaser is the interface for an extended stake info saving
// database
type StakeInfoDatabaser interface {
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
	mtx                                                 sync.Mutex
	dbSummaryHeight                                     int64
	dbStakeInfoHeight                                   int64
	getLatestBlockSQL                                   string
	getBlockSQL, insertBlockSQL                         string
	getLatestStakeInfoExtendedSQL                       string
	getStakeInfoExtendedSQL, insertStakeInfoExtendedSQL string
}

// NewDB creates a new DB instance with pre-generated sql statements from an
// existing sql.DB. Use InitDB to create a new DB without having a sql.DB.
func NewDB(db *sql.DB) *DB {
	getBlockSQL := fmt.Sprintf(`select * from %s where height = ?`,
		TableNameSummaries)
	getLatestBlockSQL := fmt.Sprintf(`SELECT * FROM %s ORDER BY height DESC LIMIT 0, 1`,
		TableNameSummaries)
	insertBlockSQL := fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
            height, size, hash, diff, sdiff, time, poolsize, poolval, poolavg
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?)
        `, TableNameSummaries)
	getStakeInfoExtendedSQL := fmt.Sprintf(`select * from %s where height = ?`,
		TableNameStakeInfo)
	getLatestStakeInfoExtendedSQL := fmt.Sprintf(
		`SELECT * FROM %s ORDER BY height DESC LIMIT 0, 1`, TableNameSummaries)
	insertStakeInfoExtendedSQL := fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
            height, num_tickets, fee_min, fee_max, fee_mean, fee_med, fee_std,
			sdiff, window_num, window_ind, pool_size, pool_val, pool_valavg
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `, TableNameStakeInfo)
	// TODO: if this db exists, figure out best heights
	return &DB{db, sync.Mutex{}, -1, -1,
		getLatestBlockSQL,
		getBlockSQL, insertBlockSQL,
		getLatestStakeInfoExtendedSQL,
		getStakeInfoExtendedSQL, insertStakeInfoExtendedSQL}
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
            poolavg FLOAT
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
            pool_size INTEGER, pool_val FLOAT, pool_valavg FLOAT
        );
        `, TableNameStakeInfo)

	_, err = db.Exec(createStakeInfoExtendedStmt)
	if err != nil {
		log.Errorf("%q: %s\n", err, createStakeInfoExtendedStmt)
		return nil, err
	}

	err = db.Ping()
	return NewDB(db), err
}

// Store satisfies the blockdata.BlockDataSaver interface
func (db *DB) Store(data *blockdata.BlockData) error {
	// TODO: make a queue instead of blocking
	db.mtx.Lock()
	defer db.mtx.Unlock()

	summary := data.ToBlockSummary()
	err := db.StoreBlockSummary(&summary)
	if err != nil {
		return err
	}

	stakeInfoExtended := data.ToStakeInfoExtended()
	return db.StoreStakeInfoExtended(&stakeInfoExtended)
}

func (db *DB) StoreBlockSummary(bd *apitypes.BlockDataBasic) error {
	stmt, err := db.Prepare(db.insertBlockSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(&bd.Height, &bd.Size, &bd.Hash,
		&bd.Difficulty, &bd.StakeDiff, &bd.Time,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg)
	if err != nil {
		return err
	}

	if err = logDBResult(res); err == nil {
		// TODO: atomic with CAS
		log.Debugf("Store height: %v", bd.Height)
		height := int64(bd.Height)
		if height > db.dbSummaryHeight {
			db.dbSummaryHeight = height
		}
	}

	return err
}

func (db *DB) RetrieveLatestBlockSummary() (*apitypes.BlockDataBasic, error) {
	bd := new(apitypes.BlockDataBasic)

	err := db.QueryRow(db.getLatestBlockSQL).Scan(&bd.Height, &bd.Size,
		&bd.Hash, &bd.Difficulty, &bd.StakeDiff, &bd.Time,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg)
	if err != nil {
		return nil, err
	}

	return bd, nil
}

func (db *DB) RetrieveBlockSummary(ind int64) (*apitypes.BlockDataBasic, error) {
	bd := new(apitypes.BlockDataBasic)

	// Three different ways

	// 1. chained QueryRow/Scan only
	err := db.QueryRow(db.getBlockSQL, ind).Scan(&bd.Height, &bd.Size, &bd.Hash,
		&bd.Difficulty, &bd.StakeDiff, &bd.Time,
		&bd.PoolInfo.Size, &bd.PoolInfo.Value, &bd.PoolInfo.ValAvg)
	if err != nil {
		return nil, err
	}

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

func (db *DB) StoreStakeInfoExtended(si *apitypes.StakeInfoExtended) error {
	stmt, err := db.Prepare(db.insertStakeInfoExtendedSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(&si.Feeinfo.Height,
		&si.Feeinfo.Number, &si.Feeinfo.Min, &si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg)
	if err != nil {
		return err
	}

	if err = logDBResult(res); err == nil {
		height := int64(si.Feeinfo.Height)
		if height > db.dbStakeInfoHeight {
			db.dbStakeInfoHeight = height
		}
	}
	return err
}

func (db *DB) RetrieveLatestStakeInfoExtended() (*apitypes.StakeInfoExtended, error) {
	si := new(apitypes.StakeInfoExtended)

	err := db.QueryRow(db.getLatestStakeInfoExtendedSQL).Scan(
		&si.Feeinfo.Height, &si.Feeinfo.Number, &si.Feeinfo.Min,
		&si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg)
	if err != nil {
		return nil, err
	}

	return si, nil
}

func (db *DB) RetrieveStakeInfoExtended(ind int64) (*apitypes.StakeInfoExtended, error) {
	si := new(apitypes.StakeInfoExtended)

	err := db.QueryRow(db.getStakeInfoExtendedSQL, ind).Scan(&si.Feeinfo.Height,
		&si.Feeinfo.Number, &si.Feeinfo.Min, &si.Feeinfo.Max, &si.Feeinfo.Mean,
		&si.Feeinfo.Median, &si.Feeinfo.StdDev,
		&si.StakeDiff, // no next or estimates
		&si.PriceWindowNum, &si.IdxBlockInWindow, &si.PoolInfo.Size,
		&si.PoolInfo.Value, &si.PoolInfo.ValAvg)
	if err != nil {
		return nil, err
	}

	return si, nil
}

func logDBResult(res sql.Result) error {
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
