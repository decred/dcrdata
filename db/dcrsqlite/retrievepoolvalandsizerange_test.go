package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrievePoolValAndSizeRange(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	// testing zero offset
	{
		checkEmptyDBRetrievePoolValAndSizeRangeSame(0, db)
		checkEmptyDBRetrievePoolValAndSizeRangeSame(1, db)
		checkEmptyDBRetrievePoolValAndSizeRangeSame(2, db)
	}
	// testing positive offset
	{
		checkEmptyDBRetrievePoolValAndSizeRangePlus(0, 0, db)
		checkEmptyDBRetrievePoolValAndSizeRangePlus(1, 0, db)
		checkEmptyDBRetrievePoolValAndSizeRangePlus(2, 0, db)

		checkEmptyDBRetrievePoolValAndSizeRangePlus(0, 1, db)
		checkEmptyDBRetrievePoolValAndSizeRangePlus(1, 1, db)

		checkEmptyDBRetrievePoolValAndSizeRangePlus(0, 2, db)
	}
	// testing incorrect range (negative offset)
	{
		checkEmptyDBRetrievePoolValAndSizeRangeNegative(0, -1, db)
		checkEmptyDBRetrievePoolValAndSizeRangeNegative(1, -1, db)
		checkEmptyDBRetrievePoolValAndSizeRangeNegative(2, -1, db)

		checkEmptyDBRetrievePoolValAndSizeRangeNegative(1, -2, db)
		checkEmptyDBRetrievePoolValAndSizeRangeNegative(2, -2, db)

		checkEmptyDBRetrievePoolValAndSizeRangeNegative(2, -3, db)
	}
}

// checkEmptyDBRetrievePoolValAndSizeRangeSame calls RetrievePoolValAndSizeRange
// on empty DB expecting empty result
func checkEmptyDBRetrievePoolValAndSizeRangeSame(fromIndex int64, db *DB) {
	values, sizes, err := db.RetrievePoolValAndSizeRange(fromIndex, fromIndex-1)
	// Should return [] []
	if err != nil {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed: %v",
			err)
	}
	if len(values) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed, values is not empty\n%v",
			testutil.ArrayToString("values", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed, sizes is not empty\n%v",
			testutil.ArrayToString("sizes", sizes))
	}
}

// checkEmptyDBRetrievePoolValAndSizeRangeNegative calls RetrievePoolValAndSizeRange
// on empty DB with incorrect input
func checkEmptyDBRetrievePoolValAndSizeRangeNegative(from int64, negativeOffset int64, db *DB) {
	if negativeOffset >= 0 {
		testutil.ReportTestIsNotAbleToTest(
			"negativeOffset must be below 0, %v passed",
			negativeOffset)
	}
	to := from + negativeOffset - 1
	values, sizes, err := db.RetrievePoolValAndSizeRange(from, to)
	// Should return "Cannot retrieve block size range [from > to]"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed: error expected")
	}
	if len(values) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed, values is not empty\n%v",
			testutil.ArrayToString("values", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed, sizes is not empty\n%v",
			testutil.ArrayToString("sizes", sizes))
	}
}

// checkEmptyDBRetrievePoolValAndSizeRangePlus calls RetrievePoolValAndSizeRange
// on empty DB for non-empty range
func checkEmptyDBRetrievePoolValAndSizeRangePlus(from int64, positiveOffset int64, db *DB) {
	if positiveOffset < 0 {
		testutil.ReportTestIsNotAbleToTest(
			"positiveOffset must be above or equal 0, %v passed",
			positiveOffset)
	}
	to := from + positiveOffset
	values, sizes, err := db.RetrievePoolValAndSizeRange(from, to)
	// Should return "Cannot retrieve block size range [from,to] have height -1"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed: error expected")
	}
	if len(values) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed, values is not empty\n%v",
			testutil.ArrayToString("values", values))
	}
	if len(sizes) > 0 {
		testutil.ReportTestFailed(
			"RetrievePoolValAndSizeRange() failed, sizes is not empty\n%v",
			testutil.ArrayToString("sizes", sizes))
	}
}
