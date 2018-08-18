package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestEmptyDBRetrieveBlockHeight(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	height, err := db.RetrieveBlockHeight("")
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockHeight() failed: error expected")
	}
	if height != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockHeight() failed: height=%d, 0 expected",
			height)
	}
}

func TestEmptyDBRetrieveWinnersByHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	nullresult, zero, err := db.RetrieveWinnersByHash("")
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveWinnersByHash() failed: error expected")
	}
	if nullresult != nil {
		testutil.ReportTestFailed(
			"RetrieveWinnersByHash() failed: nil expected, %v returned",
			nullresult)
	}
	if zero != 0 {
		testutil.ReportTestFailed(
			"RetrieveWinnersByHash() failed: 0 expected, %d returned",
			zero)
	}
}

func TestEmptyDBRetrieveWinners(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	nullresult, empty, err := db.RetrieveWinners(0)
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveWinners() failed: error expected")
	}
	if nullresult != nil {
		testutil.ReportTestFailed(
			"RetrieveWinners() failed: nil expected, %v returned",
			nullresult)
	}
	if empty != "" {
		testutil.ReportTestFailed(
			"RetrieveWinners() failed: empty string expected, %v provided",
			empty)
	}
}

func TestEmptyDBRetrieveStakeInfoExtended(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	info, err := db.RetrieveStakeInfoExtended(0)
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveStakeInfoExtended() failed: error expected")
	}
	if info != nil {
		testutil.ReportTestFailed(
			"RetrieveStakeInfoExtended() failed: nil expected, %v returned",
			info)
	}
}

func TestEmptyDBRetrieveLatestStakeInfoExtended(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	result, err := db.RetrieveLatestStakeInfoExtended()
	if err == nil {
		testutil.ReportTestFailed(
			"RetrieveLatestStakeInfoExtended() failed: error expected")
	}
	if result != nil {
		testutil.ReportTestFailed(
			"RetrieveLatestStakeInfoExtended() failed: nil  expected, %v returned",
			result)
	}
}

func TestEmptyDBRetrievePoolInfoByHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	result, err := db.RetrievePoolInfoByHash("")
	if err == nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfoByHash() failed: error expected")
	}
	if result == nil {
		testutil.ReportTestFailed(
			"RetrievePoolInfoByHash() failed: default result expected, nil returned")
	}
	if len(result.Winners) != 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfoByHash() failed: empty array expected\n%v",
			testutil.ArrayToString("result.Winners", result.Winners))
	}
}

func TestEmptyDBRetrievePoolInfo(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
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
	if len(result.Winners) != 0 {
		testutil.ReportTestFailed(
			"RetrievePoolInfo() failed: empty array expected\n%v",
			testutil.ArrayToString("result.Winners", result.Winners))
	}
}
