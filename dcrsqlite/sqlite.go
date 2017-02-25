package dcrsqlite

import (
	"database/sql"

	"fmt"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	_ "github.com/mattn/go-sqlite3"
)

type DBInfo struct {
	FileName string
}

const (
	TableNameSummaries = "dcrdata_block_summary"
)

type DB struct {
	*sql.DB
	getBlockSQL string
	insertBlockSQL string
}

func NewDB(db *sql.DB) *DB {
	getBlockSQL := fmt.Sprintf(`select * from %s where height = ?`,
		TableNameSummaries)
	insertBlockSQL := fmt.Sprintf(`
        INSERT OR REPLACE INTO %s(
            height, size, hash, diff, sdiff, time, poolsize, poolval, poolavg
        ) values(?, ?, ?, ?, ?, ?, ?, ?, ?)
        `, TableNameSummaries)
	return &DB{db, getBlockSQL, insertBlockSQL}
}

type BlockSummaryDatabaser interface {
	StoreBlockSummary(bd *apitypes.BlockDataBasic) error
	GetBlockSummary(ind int64) (*apitypes.BlockDataBasic, error)
}

func InitDB(dbInfo *DBInfo) (*DB, error) {
	db, err := sql.Open("sqlite3", dbInfo.FileName)
	if err != nil || db == nil {
		return nil, err
	}

	createStmt := fmt.Sprintf(`
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
        delete from %s;
        `, TableNameSummaries, TableNameSummaries)

	_, err = db.Exec(createStmt)
	if err != nil {
		log.Errorf("%q: %s\n", err, createStmt)
		return nil, err
	}

	err = db.Ping()
	return NewDB(db), err
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

	lastId, err := res.LastInsertId()
	if err != nil {
		return err
	}
	rowCnt, err := res.RowsAffected()
	if err != nil {
		return err
	}
	log.Infof("ID = %d, affected = %d\n", lastId, rowCnt)

	return nil
}

func (db *DB) GetBlockSummary(ind int64) (*apitypes.BlockDataBasic, error) {
	var bd *apitypes.BlockDataBasic

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

	return nil, nil
}
