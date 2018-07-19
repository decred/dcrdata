package dcrsqlite

import (
	"fmt"
	"testing"

	"github.com/decred/dcrdata/testutil"
)

// TestRetrieveBlockHash
func TestRetrieveBlockHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testRetrieveBlockHash(db)
}

func testRetrieveBlockHash(db *DB) {
	checkRetrieveBlockHash(db, 0)
	checkRetrieveBlockHash(db, 1)
	checkRetrieveBlockHash(db, 2)
}

func checkRetrieveBlockHash(db *DB, i int64) {
	_, err := db.RetrieveBlockHash(i)
	if err == nil {
		testutil.ReportTestFailed("RetrieveBlockHash() failed: error expected")
	}
	errMsg := fmt.Sprintf("%v", err)
	if errMsg != "sql: no rows in result set" {
		testutil.ReportTestFailed("RetrieveBlockHash() unexpected error: %v", err)
	}
}
