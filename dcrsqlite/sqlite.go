package dcrsqlite

import (
	"database/sql"

	"fmt"
	//apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	_ "github.com/mattn/go-sqlite3"
)

type DBInfo struct {
	FileName string
}

const (
    TableNameSummaries = "dcrdata_block_summary"
)

func InitDB(dbInfo *DBInfo) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbInfo.FileName)
	if err != nil || db == nil {
		return nil, err
	}

	createStmt := fmt.Sprintf(`
        create table if not exists %s(
            id INTEGER NOT NULL PRIMARY KEY,
            height INTEGER,
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

	return db, nil
}
