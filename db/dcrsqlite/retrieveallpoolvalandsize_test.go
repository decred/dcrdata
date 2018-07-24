package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrieveAllPoolValAndSize(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveAllPoolValAndSize(db)
}

func testEmptyDBRetrieveAllPoolValAndSize(db *DB) {
	result, err := db.RetrieveAllPoolValAndSize()
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveAllPoolValAndSize() failed: default result expected:",
			err)
	}
	checkChartsDataIsDefault("RetrieveAllPoolValAndSize", result)
}
