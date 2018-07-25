package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestGetBestBlockHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBGetBestBlockHash(db)
}

func testEmptyDBGetBestBlockHash(db *DB) {
	str := db.GetBestBlockHash()
	if str != "" {
		// Open question: Should it really be the empty string?
		// Maybe error instead to avoid confusion?
		testutil.ReportTestFailed(
			"GetBestBlockHash() failed: %v",
			str)
	}
}
