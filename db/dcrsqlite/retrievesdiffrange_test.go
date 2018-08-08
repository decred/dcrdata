package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrieveSDiffRange(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	// testing zero offset
	{
		checkEmptyDBRetrieveSDiffRangeSame(0, db)
		checkEmptyDBRetrieveSDiffRangeSame(1, db)
		checkEmptyDBRetrieveSDiffRangeSame(2, db)
	}
	// testing positive offset
	{
		checkEmptyDBRetrieveSDiffRangePlus(0, 0, db)
		checkEmptyDBRetrieveSDiffRangePlus(1, 0, db)
		checkEmptyDBRetrieveSDiffRangePlus(2, 0, db)

		checkEmptyDBRetrieveSDiffRangePlus(0, 1, db)
		checkEmptyDBRetrieveSDiffRangePlus(1, 1, db)

		checkEmptyDBRetrieveSDiffRangePlus(0, 2, db)
	}
	// testing incorrect range (negative offset)
	{
		checkEmptyDBRetrieveSDiffRangeNegative(0, -1, db)
		checkEmptyDBRetrieveSDiffRangeNegative(1, -1, db)
		checkEmptyDBRetrieveSDiffRangeNegative(2, -1, db)

		checkEmptyDBRetrieveSDiffRangeNegative(1, -2, db)
		checkEmptyDBRetrieveSDiffRangeNegative(2, -2, db)

		checkEmptyDBRetrieveSDiffRangeNegative(2, -3, db)
	}
}

// checkEmptyDBRetrieveSDiffRangeSame calls RetrieveSDiffRange
// on empty DB expecting empty result
func checkEmptyDBRetrieveSDiffRangeSame(fromIndex int64, db *DB) {
	arr, err := db.RetrieveSDiffRange(fromIndex, fromIndex-1)
	// Should return []
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveSDiffRange() failed: %v",
			err)
	}
	if len(arr) > 0 {
		testutil.ReportTestFailed(
			"RetrieveSDiffRange() failed, array is not empty\n%v",
			testutil.ArrayToString("array", arr))
	}
}

// checkEmptyDBRetrieveSDiffRangeNegative calls RetrieveSDiffRange
// on empty DB with incorrect input
func checkEmptyDBRetrieveSDiffRangeNegative(from int64, negativeOffset int64, db *DB) {
	if negativeOffset >= 0 {
		testutil.ReportTestIsNotAbleToTest(
			"negativeOffset must be below 0, %v passed",
			negativeOffset)
	}
	to := from + negativeOffset - 1
	arr, err := db.RetrieveSDiffRange(from, to)
	// Should return "Cannot retrieve block size range [from > to]"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveSDiffRange() failed: error expected")
	}
	if len(arr) > 0 {
		testutil.ReportTestFailed(
			"RetrieveSDiffRange() failed, array is not empty\n%v",
			testutil.ArrayToString("array", arr))
	}
}

// checkEmptyDBRetrieveSDiffRangePlus calls RetrieveSDiffRange
// on empty DB for non-empty range
func checkEmptyDBRetrieveSDiffRangePlus(from int64, positiveOffset int64, db *DB) {
	if positiveOffset < 0 {
		testutil.ReportTestIsNotAbleToTest(
			"positiveOffset must be above or equal 0, %v passed",
			positiveOffset)
	}
	to := from + positiveOffset
	arr, err := db.RetrieveSDiffRange(from, to)
	// Should return "Cannot retrieve block size range [from,to] have height -1"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveSDiffRange() failed: error expected")
	}
	if len(arr) > 0 {
		testutil.ReportTestFailed(
			"RetrieveSDiffRange() failed, array is not empty\n%v",
			testutil.ArrayToString("array", arr))
	}
}
