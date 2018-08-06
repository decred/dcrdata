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
