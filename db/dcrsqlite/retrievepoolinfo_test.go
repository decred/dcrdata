package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrievePoolInfo(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrievePoolInfo(db)
}

func testEmptyDBRetrievePoolInfo(db *DB) {
	result, err := db.RetrievePoolInfo(0)
	if err == nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfo() failed: error expected")
	}
	if result == nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfo() failed: default result expected," +
				" nil returned")
	}
	checkTicketPoolInfoIsDefault("RetrievePoolInfo", result)
}
