package dcrsqlite

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	_ "github.com/mattn/go-sqlite3" // register sqlite driver with database/sql
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
	getPoolSQL, getPoolRangeSQL                         string
	getSDiffSQL, getSDiffRangeSQL                       string
	getLatestBlockSQL                                   string
	getBlockSQL, insertBlockSQL                         string
	getLatestStakeInfoExtendedSQL                       string
	getStakeInfoExtendedSQL, insertStakeInfoExtendedSQL string
}

// NewDB creates a new DB instance with pre-generated sql statements from an
// existing sql.DB. Use InitDB to create a new DB without having a sql.DB.
// TODO: if this db exists, figure out best heights
func NewDB(db *sql.DB) *DB {
	d := DB{
		DB:                db,
		dbSummaryHeight:   -1,
		dbStakeInfoHeight: -1,
	}

	// Ticket pool queries
	d.getPoolSQL = fmt.Sprintf(`select poolsize, poolval, poolavg from %s where height = ?`,
		TableNameSummaries)
	d.getPoolRangeSQL = fmt.Sprintf(`select poolsize, poolval, poolavg from %s where height between ? and ?`,
		TableNameSummaries)

	d.getSDiffSQL = fmt.Sprintf(`select sdiff from %s where height = ?`,
		TableNameSummaries)
	d.getSDiffRangeSQL = fmt.Sprintf(`select sdiff from %s where height between ? and ?`,
		TableNameSummaries)

	// Block queries
	d.getBlockSQL = fmt.Sprintf(`select * from %s where height = ?`,
		TableNameSummaries)
	d.getLatestBlockSQL = fmt.Sprintf(`SELECT * FROM %s ORDER BY height DESC LIMIT 0, 1`,
		TableNameSummaries)
	d.insertBlockSQL = fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
            height, size, hash, diff, sdiff, time, poolsize, poolval, poolavg
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?)
        `, TableNameSummaries)

	// Stake info queries
	d.getStakeInfoExtendedSQL = fmt.Sprintf(`select * from %s where height = ?`,
		TableNameStakeInfo)
	d.getLatestStakeInfoExtendedSQL = fmt.Sprintf(
		`SELECT * FROM %s ORDER BY height DESC LIMIT 0, 1`, TableNameStakeInfo)
	d.insertStakeInfoExtendedSQL = fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
            height, num_tickets, fee_min, fee_max, fee_mean, fee_med, fee_std,
			sdiff, window_num, window_ind, pool_size, pool_val, pool_valavg
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `, TableNameStakeInfo)

	d.dbSummaryHeight = d.GetBlockSummaryHeight()
	d.dbStakeInfoHeight = d.GetStakeInfoHeight()

	return &d
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

type DBDataSaver struct {
	*DB
	updateStatusChan chan uint32
}

// Store satisfies the blockdata.BlockDataSaver interface
func (db *DBDataSaver) Store(data *blockdata.BlockData) error {
	// TODO: make a queue instead of blocking
	db.DB.mtx.Lock()
	defer db.DB.mtx.Unlock()

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
		//log.Debugf("Store height: %v", bd.Height)
		height := int64(bd.Height)
		if height > db.dbSummaryHeight {
			db.dbSummaryHeight = height
		}
	}

	return err
}

func (db *DB) GetBlockSummaryHeight() int64 {
	if db.dbSummaryHeight < 0 {
		sum, err := db.RetrieveLatestBlockSummary()
		if err != nil || sum == nil {
			log.Errorf("RetrieveLatestBlockSummary failed: %v", err)
			return -1
		}
		db.dbSummaryHeight = int64(sum.Height)
	}
	return db.dbSummaryHeight
}

func (db *DB) GetStakeInfoHeight() int64 {
	if db.dbStakeInfoHeight < 0 {
		si, err := db.RetrieveLatestStakeInfoExtended()
		if err != nil || si == nil {
			log.Errorf("RetrieveLatestStakeInfoExtended failed: %v", err)
			return -1
		}
		db.dbStakeInfoHeight = int64(si.Feeinfo.Height)
	}
	return db.dbStakeInfoHeight
}

func (db *DB) RetrievePoolInfoRange(ind0, ind1 int64) ([]apitypes.TicketPoolInfo, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []apitypes.TicketPoolInfo{}, nil
	}
	if N < 0 {
		return nil, fmt.Errorf("Cannot retrieve pool info range (%d<%d)",
			ind1, ind0)
	}
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		return nil, fmt.Errorf("Cannot retrieve pool info range [%d,%d], have height %d",
			ind1, ind0, db.dbSummaryHeight)
	}

	tpis := make([]apitypes.TicketPoolInfo, 0, N)

	stmt, err := db.Prepare(db.getPoolRangeSQL)
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
		var tpi apitypes.TicketPoolInfo
		if err = rows.Scan(&tpi.Size, &tpi.Value, &tpi.ValAvg); err != nil {
			log.Errorf("Unable to scan for TicketPoolInfo fields: %v", err)
		}
		tpis = append(tpis, tpi)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}

	return tpis, nil
}

func (db *DB) RetrievePoolInfo(ind int64) (*apitypes.TicketPoolInfo, error) {
	tpi := new(apitypes.TicketPoolInfo)
	err := db.QueryRow(db.getPoolSQL, ind).Scan(&tpi.Size, &tpi.Value, &tpi.ValAvg)
	return tpi, err
}

func (db *DB) RetrievePoolValAndSizeRange(ind0, ind1 int64) ([]float64, []float64, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []float64{}, []float64{}, nil
	}
	if N < 0 {
		return nil, nil, fmt.Errorf("Cannot retrieve pool val and size range (%d<%d)",
			ind1, ind0)
	}
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		return nil, nil, fmt.Errorf("Cannot retrieve pool val and size range [%d,%d], have height %d",
			ind1, ind0, db.dbSummaryHeight)
	}

	poolvals := make([]float64, 0, N)
	poolsizes := make([]float64, 0, N)

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
		var pval, psize, pavg float64
		if err = rows.Scan(&psize, &pval, &pavg); err != nil {
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

func (db *DB) RetrieveSDiffRange(ind0, ind1 int64) ([]float64, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []float64{}, nil
	}
	if N < 0 {
		return nil, fmt.Errorf("Cannot retrieve sdiff range (%d<%d)",
			ind1, ind0)
	}
	if ind1 > db.dbSummaryHeight || ind0 < 0 {
		return nil, fmt.Errorf("Cannot retrieve sdiff range [%d,%d], have height %d",
			ind1, ind0, db.dbSummaryHeight)
	}

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

func (db *DB) RetrieveSDiff(ind int64) (float64, error) {
	var sdiff float64
	err := db.QueryRow(db.getSDiffSQL, ind).Scan(&sdiff)
	return sdiff, err
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
