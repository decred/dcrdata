package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrievePoolInfoByHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrievePoolInfoByHash(db)
}

func testEmptyDBRetrievePoolInfoByHash(db *DB) {
	result, err := db.RetrievePoolInfoByHash("")
	if err == nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfoByHash() failed: error expected")
	}
	if result == nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfoByHash() failed:" +
				" default result expected," +
				" nil returned")
	}
	if len(result.Winners) != 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfoByHash() failed:" +
				" empty array expected\n" +
				testutil.ArrayToString("result.Winners", result.Winners))
	}
}
