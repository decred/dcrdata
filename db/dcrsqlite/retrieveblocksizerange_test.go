package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrieveBlockSizeRange(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	// testing zero offset
	{
		checkEmptyDBRetrieveBlockSizeRangeSame(0, db)
		checkEmptyDBRetrieveBlockSizeRangeSame(1, db)
		checkEmptyDBRetrieveBlockSizeRangeSame(2, db)
	}
	// testing positive offset
	{
		checkEmptyDBRetrieveBlockSizeRangePlus(0, 0, db)
		checkEmptyDBRetrieveBlockSizeRangePlus(1, 0, db)
		checkEmptyDBRetrieveBlockSizeRangePlus(2, 0, db)

		checkEmptyDBRetrieveBlockSizeRangePlus(0, 1, db)
		checkEmptyDBRetrieveBlockSizeRangePlus(1, 1, db)

		checkEmptyDBRetrieveBlockSizeRangePlus(0, 2, db)
	}
	// testing incorrect range (negative offset)
	{
		checkEmptyDBRetrieveBlockSizeRangeNegative(0, -1, db)
		checkEmptyDBRetrieveBlockSizeRangeNegative(1, -1, db)
		checkEmptyDBRetrieveBlockSizeRangeNegative(2, -1, db)

		checkEmptyDBRetrieveBlockSizeRangeNegative(1, -2, db)
		checkEmptyDBRetrieveBlockSizeRangeNegative(2, -2, db)

		checkEmptyDBRetrieveBlockSizeRangeNegative(2, -3, db)
	}
}

// checkEmptyDBRetrieveBlockSizeRangeSame calls RetrieveBlockSizeRange
// on empty DB expecting empty result
func checkEmptyDBRetrieveBlockSizeRangeSame(fromIndex int64, db *DB) {
	arr, err := db.RetrieveBlockSizeRange(fromIndex, fromIndex-1)
	// Should return []
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSizeRange() failed: %v",
			err)
	}
	if len(arr) > 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockSizeRange() failed, array is not empty\n%v",
			testutil.ArrayToString("array", arr))
	}
}

// checkEmptyDBRetrieveBlockSizeRangeNegative calls RetrieveBlockSizeRange
// on empty DB with incorrect input
func checkEmptyDBRetrieveBlockSizeRangeNegative(from int64, negativeOffset int64, db *DB) {
	if negativeOffset >= 0 {
		testutil.ReportTestIsNotAbleToTest(
			"negativeOffset must be below 0, %v passed",
			negativeOffset)
	}
	to := from + negativeOffset - 1
	arr, err := db.RetrieveBlockSizeRange(from, to)
	// Should return "Cannot retrieve block size range [from > to]"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSizeRange() failed: error expected")
	}
	if len(arr) > 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockSizeRange() failed, array is not empty\n%v",
			testutil.ArrayToString("array", arr))
	}
}

// checkEmptyDBRetrieveBlockSizeRangePlus calls RetrieveBlockSizeRange
// on empty DB for non-empty range
func checkEmptyDBRetrieveBlockSizeRangePlus(from int64, positiveOffset int64, db *DB) {
	if positiveOffset < 0 {
		testutil.ReportTestIsNotAbleToTest(
			"positiveOffset must be above or equal 0, %v passed",
			positiveOffset)
	}
	to := from + positiveOffset
	arr, err := db.RetrieveBlockSizeRange(from, to)
	// Should return "Cannot retrieve block size range [from,to] have height -1"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSizeRange() failed: error expected")
	}
	if len(arr) > 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockSizeRange() failed, array is not empty\n%v",
			testutil.ArrayToString("array", arr))
	}
}
