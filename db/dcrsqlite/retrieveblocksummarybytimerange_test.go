package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrieveBlockSummaryByTimeRange(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveBlockSummaryByTimeRange(db)
}

func testEmptyDBRetrieveBlockSummaryByTimeRange(db *DB) {
	resultArray, err := db.RetrieveBlockSummaryByTimeRange(
		0, 10, 7)
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSummaryByTimeRange() failed: %v", err)
	}
	if resultArray == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSummaryByTimeRange() failed: nil returned")
	}
	if len(resultArray) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockSummaryByTimeRange() failed: empty array expected\n" +
				testutil.ArrayToString("resultArray", resultArray))
	}
}
