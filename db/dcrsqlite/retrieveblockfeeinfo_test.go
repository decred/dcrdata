package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrieveBlockFeeInfo(t *testing.T) {
	testutil.BindCurrentTestSetup(t)
	db := InitTestDB(DBPathForTest())
	testEmptyDBRetrieveBlockFeeInfo(db)
}

func testEmptyDBRetrieveBlockFeeInfo(db *DB) {
	result, err := db.RetrieveBlockFeeInfo()
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: default result expected:",
			err)
	}
	if result == nil {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: default result expected")
	}
	// All arrays expected to be empty
	// The following checks are sorted according to the ChartsData struct fields order
	if len(result.TimeStr) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.TimeStr is not empty\n" +
				testutil.ArrayToString("result.TimeStr", result.TimeStr))
	}
	if len(result.Difficulty) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Difficulty is not empty\n" +
				testutil.ArrayToString("result.Difficulty", result.Difficulty))
	}
	if len(result.Time) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Time is not empty\n" +
				testutil.ArrayToString("result.Time", result.Time))
	}
	if len(result.Value) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Value is not empty\n" +
				testutil.ArrayToString("result.Value", result.Value))
	}
	if len(result.Size) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Size is not empty\n" +
				testutil.ArrayToString("result.Size", result.Size))
	}
	if len(result.ChainSize) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.ChainSize is not empty\n" +
				testutil.ArrayToString("result.ChainSize", result.ChainSize))
	}
	if len(result.Count) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Count is not empty\n" +
				testutil.ArrayToString("result.Count", result.Count))
	}
	if len(result.SizeF) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.SizeF is not empty\n" +
				testutil.ArrayToString("result.SizeF", result.SizeF))
	}
	if len(result.ValueF) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.ValueF is not empty\n" +
				testutil.ArrayToString("result.ValueF", result.ValueF))
	}
	if len(result.Unspent) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Unspent is not empty\n" +
				testutil.ArrayToString("result.Unspent", result.Unspent))
	}
	if len(result.Revoked) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Revoked is not empty\n" +
				testutil.ArrayToString("result.Revoked", result.Revoked))
	}
	if len(result.Height) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Height is not empty\n" +
				testutil.ArrayToString("result.Height", result.Height))
	}
	if len(result.Pooled) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Pooled is not empty\n" +
				testutil.ArrayToString("result.Pooled", result.Pooled))
	}
	if len(result.Solo) != 0 {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: result.Solo is not empty\n" +
				testutil.ArrayToString("result.Solo", result.Solo))
	}
}
