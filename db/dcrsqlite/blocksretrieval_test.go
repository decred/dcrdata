package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/testutil"
	"github.com/google/go-cmp/cmp"
)

func TestEmptyDBRetrieveAllPoolValAndSize(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()

	result, err := db.RetrieveAllPoolValAndSize()
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveAllPoolValAndSize() failed: default result expected: %v",
			err)
	}

	// Expected value:
	var defaultChartsData dbtypes.ChartsData

	if !cmp.Equal(*result, defaultChartsData) {
		testutil.ReportTestFailed(
			"RetrieveAllPoolValAndSize() failed: default result expected:\n%v",
			cmp.Diff(*result, defaultChartsData))
	}
}

func TestEmptyDBRetrieveBlockFeeInfo(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	result, err := db.RetrieveBlockFeeInfo()
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: default result expected: %v",
			err)
	}

	// Expected value:
	var defaultChartsData dbtypes.ChartsData

	if !cmp.Equal(*result, defaultChartsData) {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: default result expected:\n%v",
			cmp.Diff(*result, defaultChartsData))
	}
}

func TestEmptyDBGetBestBlockHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	str := db.GetBestBlockHash()
	if str != "" {
		testutil.ReportTestFailed(
			"GetBestBlockHash() failed: expected empty string, returned %v",
			str)
	}
}

func TestEmptyDBGetBestBlockHeight(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	h := db.GetBestBlockHeight()
	if h != -1 {
		testutil.ReportTestFailed(
			"db.GetBestBlockHeight() returned %d, expected -1",
			h)
	}
}

func TestEmptyDBGetStakeInfoHeight(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := ObtainReusableEmptyDB()
	endHeight, err := db.GetStakeInfoHeight()
	if err != nil {
		testutil.ReportTestFailed(
			"GetStakeInfoHeight() failed: %v",
			err)
	}
	if endHeight != -1 {
		testutil.ReportTestFailed(
			"GetStakeInfoHeight() failed: returned %d, expected -1",
			endHeight)
	}
}
