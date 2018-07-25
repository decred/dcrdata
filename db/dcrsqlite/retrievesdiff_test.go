package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrieveSDiff(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveSDiff(db)
}

func testEmptyDBRetrieveSDiff(db *DB) {
	checkEmptyDBRetrieveSDiff(db, 0)
	checkEmptyDBRetrieveSDiff(db, 1)
	checkEmptyDBRetrieveSDiff(db, 2)
}

func checkEmptyDBRetrieveSDiff(db *DB, i int64) {
	_, err := db.RetrieveSDiff(i)
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveSDiff() failed: error expected")
	}
}
