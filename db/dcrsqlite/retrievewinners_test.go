package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrieveWinners(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveWinners(db)
}
func testEmptyDBRetrieveWinners(db *DB) {
	nullresult, empty, err := db.RetrieveWinners(0)
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveWinners() failed: error expected")
	}
	if nullresult != nil {
		testutil.ReportTestFailed(
			"RetrieveWinners() failed: "+
				"nil expected, %v returned",
			nullresult)
	}
	if empty != "" {
		testutil.ReportTestFailed(
			"RetrieveWinners() failed: "+
				"empty string expected, %v provided",
			empty)
	}
}
