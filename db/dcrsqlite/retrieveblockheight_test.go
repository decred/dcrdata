package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrieveBlockHeight(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveBlockHeight(db)
}

func testEmptyDBRetrieveBlockHeight(db *DB) {
	height, err := db.RetrieveBlockHeight("")
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockHeight() failed: error expected")
	}
	if height != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockHeight() failed: "+
				"height=%v, should be 0",
			height)
	}
}
