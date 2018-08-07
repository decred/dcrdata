package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrieveLatestBlockSummary(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	summary, err := db.RetrieveLatestBlockSummary()
	// expected "sql: no rows in result set"
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveLatestBlockSummary() failed: error expected")
	}
	if summary != nil {
		testutil.ReportTestFailed(
			"RetrieveLatestBlockSummary() failed: nil expected, %v returned",
			summary)
	}
}

func TestEmptyDBRetrieveBlockSummaryByTimeRange(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
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
			"RetrieveBlockSummaryByTimeRange() failed: empty array expected\n%v",
			testutil.ArrayToString("resultArray", resultArray))
	}
}

func TestEmptyDBRetrieveBlockSummaryByHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	summary, err := db.RetrieveBlockSummaryByHash("")
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSummaryByHash() failed: error expected")
	}
	if summary != nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSummaryByHash() failed: nil expected, %v returned",
			summary)
	}
}

func TestEmptyDBRetrieveBlockSummary(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	summary, err := db.RetrieveBlockSummary(0)
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSummary() failed: error expected")
	}
	if summary != nil {
		testutil.ReportTestFailed(
			"RetrieveBlockSummary() failed: nil expected, %v returned",
			summary)
	}
}
