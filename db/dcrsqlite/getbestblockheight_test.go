package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestGetBestBlockHeight(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBGetBestBlockHeight(db)
}

// Empty DB, should return -1
func testEmptyDBGetBestBlockHeight(db *DB) {
	h := db.GetBestBlockHeight()
	if h != -1 {
		testutil.ReportTestFailed("db.GetBestBlockHeight() is %v", h)
	}
}
