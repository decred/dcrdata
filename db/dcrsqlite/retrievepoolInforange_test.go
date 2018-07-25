package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrievePoolInfoRange(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrievePoolInfoRange(db)
}

func testEmptyDBRetrievePoolInfoRange(db *DB) {
	//testing zero offset
	{
		checkEmptyDBRetrievePoolInfoRangeSame(0, db)
		checkEmptyDBRetrievePoolInfoRangeSame(1, db)
		checkEmptyDBRetrievePoolInfoRangeSame(2, db)
	}
	//testing positive offset
	{
		checkEmptyDBRetrievePoolInfoRangePlus(0, 0, db)
		checkEmptyDBRetrievePoolInfoRangePlus(1, 0, db)
		checkEmptyDBRetrievePoolInfoRangePlus(2, 0, db)

		checkEmptyDBRetrievePoolInfoRangePlus(0, 1, db)
		checkEmptyDBRetrievePoolInfoRangePlus(1, 1, db)

		checkEmptyDBRetrievePoolInfoRangePlus(0, 2, db)
	}
	//testing incorrect range (negative offset)
	{
		checkEmptyDBRetrievePoolInfoRangeNegative(0, -1, db)
		checkEmptyDBRetrievePoolInfoRangeNegative(1, -1, db)
		checkEmptyDBRetrievePoolInfoRangeNegative(2, -1, db)

		checkEmptyDBRetrievePoolInfoRangeNegative(1, -2, db)
		checkEmptyDBRetrievePoolInfoRangeNegative(2, -2, db)

		checkEmptyDBRetrievePoolInfoRangeNegative(2, -3, db)
	}
}

// checkEmptyDBRetrievePoolInfoRangeSame calls RetrievePoolInfoRange
// on empty DB expecting empty result
func checkEmptyDBRetrievePoolInfoRangeSame(fromIndex int64, db *DB) {
	values, sizes, err := db.RetrievePoolInfoRange(fromIndex, fromIndex-1)
	// Should return [] []
	if err != nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed: %v",
			err)
	}
	if len(values) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed, "+
				"values is not empty\n%v",
			testutil.ArrayToString("values", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed, "+
				"sizes is not empty\n%v",
			testutil.ArrayToString("sizes", sizes))
	}
}

// checkEmptyDBRetrievePoolInfoRangeNegative calls RetrievePoolInfoRange
// on empty DB with incorrect input
func checkEmptyDBRetrievePoolInfoRangeNegative(from int64, negativeOffset int64, db *DB) {
	if negativeOffset >= 0 {
		testutil.ReportTestFailed(
			"negativeOffset must be below 0, %v passed",
			negativeOffset)
	}
	to := from + negativeOffset - 1
	values, sizes, err := db.RetrievePoolInfoRange(from, to)
	// Should return "Cannot retrieve block size range [from > to]"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed: error expected")
	}
	if len(values) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed, "+
				"values is not empty\n%v",
			testutil.ArrayToString("values", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed, "+
				"sizes is not empty\n%v",
			testutil.ArrayToString("sizes", sizes))
	}
}

// checkEmptyDBRetrievePoolInfoRangePlus calls RetrievePoolInfoRange
// on empty DB for non-empty range
func checkEmptyDBRetrievePoolInfoRangePlus(from int64, positiveOffset int64, db *DB) {
	if positiveOffset < 0 {
		testutil.ReportTestFailed(
			"positiveOffset must be above or equal 0, %v passed",
			positiveOffset)
	}
	to := from + positiveOffset
	values, sizes, err := db.RetrievePoolInfoRange(from, to)
	// Should return "Cannot retrieve block size range [from,to] have height -1"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed: error expected")
	}
	if len(values) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed, "+
				"values is not empty\n%v",
			testutil.ArrayToString("values", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfoRange() failed, "+
				"sizes is not empty\n%v",
			testutil.ArrayToString("sizes", sizes))
	}
}
