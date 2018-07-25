package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrieveBlockFeeInfo(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveBlockFeeInfo(db)
}

func testEmptyDBRetrieveBlockFeeInfo(db *DB) {
	result, err := db.RetrieveBlockFeeInfo()
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: default result expected:",
			err)
	}
	checkChartsDataIsDefault("RetrieveBlockFeeInfo", result)
}
