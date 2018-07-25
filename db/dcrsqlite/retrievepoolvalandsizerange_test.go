package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrievePoolValAndSizeRange(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testRetrievePoolValAndSizeRange(db)
}

func testRetrievePoolValAndSizeRange(db *DB) {
	//checkRetrievePoolValAndSizeRangeSame
	{
		checkRetrievePoolValAndSizeRangeSame(0, db)
		checkRetrievePoolValAndSizeRangeSame(1, db)
		checkRetrievePoolValAndSizeRangeSame(2, db)
	}
	//checkRetrievePoolValAndSizeRangePlus
	{
		checkRetrievePoolValAndSizeRangePlus(0, 0, db)
		checkRetrievePoolValAndSizeRangePlus(1, 0, db)
		checkRetrievePoolValAndSizeRangePlus(2, 0, db)

		checkRetrievePoolValAndSizeRangePlus(0, 1, db)
		checkRetrievePoolValAndSizeRangePlus(1, 1, db)

		checkRetrievePoolValAndSizeRangePlus(0, 2, db)
	}
	//checkRetrievePoolValAndSizeRangeNegative
	{
		checkRetrievePoolValAndSizeRangeNegative(0, -1, db)
		checkRetrievePoolValAndSizeRangeNegative(1, -1, db)
		checkRetrievePoolValAndSizeRangeNegative(2, -1, db)

		checkRetrievePoolValAndSizeRangeNegative(1, -2, db)
		checkRetrievePoolValAndSizeRangeNegative(2, -2, db)

		checkRetrievePoolValAndSizeRangeNegative(2, -3, db)
	}
}

// calls RetrievePoolValAndSizeRange on empty DB expecting empty result
func checkRetrievePoolValAndSizeRangeSame(fromIndex int64, db *DB) {
	values, sizes, err := db.RetrievePoolValAndSizeRange(fromIndex, fromIndex-1)
	// Should return [] []
	if err != nil {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: %v", err)
	}
	if len(values) > 0 {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: invalid default result\n" + testutil.ArrayToString("vals", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: invalid default result\n" + testutil.ArrayToString("sizes", sizes))
	}
}

// calls RetrievePoolValAndSizeRange on empty DB with incorrect input
func checkRetrievePoolValAndSizeRangeNegative(from int64, negativeOffset int64, db *DB) {
	if negativeOffset >= 0 {
		testutil.ReportTestIsNotAbleToTest("negativeOffset must be below 0, passed", negativeOffset)
	}
	to := from + negativeOffset - 1
	values, sizes, err := db.RetrievePoolValAndSizeRange(from, to)
	// Should return "Cannot retrieve block size range (from<to)"
	if err == nil {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: error expected")
	}
	if len(values) > 0 {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: invalid default result\n" + testutil.ArrayToString("vals", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: invalid default result\n" + testutil.ArrayToString("sizes", sizes))
	}
}

// calls RetrievePoolValAndSizeRange on empty DB for non-empty range
func checkRetrievePoolValAndSizeRangePlus(from int64, positiveOffset int64, db *DB) {
	if positiveOffset < 0 {
		testutil.ReportTestIsNotAbleToTest("positiveOffset must be above or equal 0, passed", positiveOffset)
	}
	to := from + positiveOffset
	values, sizes, err := db.RetrievePoolValAndSizeRange(from, to)
	// Should return "Cannot retrieve block size range [from,to] have height -1"
	if err == nil {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: error expected")
	}
	if len(values) > 0 {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: invalid default result\n" + testutil.ArrayToString("vals", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed("RetrievePoolValAndSizeRange() failed: invalid default result\n" + testutil.ArrayToString("sizes", sizes))
	}
}
