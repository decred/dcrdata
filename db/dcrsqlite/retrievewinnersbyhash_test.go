package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrieveWinnersByHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveWinnersByHash(db)
}

func testEmptyDBRetrieveWinnersByHash(db *DB) {
	nullresult, zero, err := db.RetrieveWinnersByHash("")
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveWinnersByHash() failed: error expected")
	}
	if nullresult != nil {
		testutil.ReportTestFailed(
			"RetrieveWinnersByHash() failed: "+
				"nil expected, %v returned",
			nullresult)
	}
	if zero != 0 {
		testutil.ReportTestFailed(
			"RetrieveWinnersByHash() failed: "+
				"0 expected, %v provided",
			zero)
	}
}
