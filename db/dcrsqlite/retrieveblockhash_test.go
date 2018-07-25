package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrieveBlockHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveBlockHash(db)
}

func testEmptyDBRetrieveBlockHash(db *DB) {
	checkEmptyDBRetrieveBlockHash(db, 0)
	checkEmptyDBRetrieveBlockHash(db, 1)
	checkEmptyDBRetrieveBlockHash(db, 2)
}

func checkEmptyDBRetrieveBlockHash(db *DB, i int64) {
	_, err := db.RetrieveBlockHash(i)
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockHash() failed: error expected")
	}
}
