package dcrsqlite

/*
This file contains package-related test-setup utils
*/
import (
	"path/filepath"

	"github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/testutil"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/testutil"
)

// DBPathForTest produces path inside dedicated test folder for current test
func DBPathForTest() string {
	testName := testutil.CurrentTestSetup().Name()
	testutil.ResetTempFolder(&testName)
	target := filepath.Join(testName, testutil.DefaultDBFileName)
	targetDBFile := testutil.FilePathInsideTempDir(target)
	return targetDBFile
}

// InitTestDB creates default DB instance
func InitTestDB(targetDBFile string) *DB {
	dbInfo := &DBInfo{FileName: targetDBFile}
	db, err := InitDB(dbInfo)
	if err != nil {
		testutil.ReportTestFailed("InitDB() failed: %v", err)
	}
	if db == nil {
		testutil.ReportTestFailed("InitDB() failed")
	}
	return db //is not nil
}

// checkChartsDataIsDefault checks if chartsData is in default mode:
//  - all the returned arrays expected to be empty
//  - chartsData is not null (nil)
//
// These checks are sorted according to the ChartsData struct fields order
func checkChartsDataIsDefault(functionName string, chartsData *dbtypes.ChartsData) {
	if chartsData == nil {
		testutil.ReportTestFailed(
			functionName + "() failed: default result expected")
	}
	// Array checks:
	if len(chartsData.TimeStr) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.TimeStr is not empty\n" +
				testutil.ArrayToString("chartsData.TimeStr", chartsData.TimeStr))
	}
	if len(chartsData.Difficulty) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Difficulty is not empty\n" +
				testutil.ArrayToString("chartsData.Difficulty", chartsData.Difficulty))
	}
	if len(chartsData.Time) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Time is not empty\n" +
				testutil.ArrayToString("chartsData.Time", chartsData.Time))
	}
	if len(chartsData.Value) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Value is not empty\n" +
				testutil.ArrayToString("chartsData.Value", chartsData.Value))
	}
	if len(chartsData.Size) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Size is not empty\n" +
				testutil.ArrayToString("chartsData.Size", chartsData.Size))
	}
	if len(chartsData.ChainSize) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.ChainSize is not empty\n" +
				testutil.ArrayToString("chartsData.ChainSize", chartsData.ChainSize))
	}
	if len(chartsData.Count) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Count is not empty\n" +
				testutil.ArrayToString("chartsData.Count", chartsData.Count))
	}
	if len(chartsData.SizeF) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.SizeF is not empty\n" +
				testutil.ArrayToString("chartsData.SizeF", chartsData.SizeF))
	}
	if len(chartsData.ValueF) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.ValueF is not empty\n" +
				testutil.ArrayToString("chartsData.ValueF", chartsData.ValueF))
	}
	if len(chartsData.Unspent) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Unspent is not empty\n" +
				testutil.ArrayToString("chartsData.Unspent", chartsData.Unspent))
	}
	if len(chartsData.Revoked) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Revoked is not empty\n" +
				testutil.ArrayToString("chartsData.Revoked", chartsData.Revoked))
	}
	if len(chartsData.Height) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Height is not empty\n" +
				testutil.ArrayToString("chartsData.Height", chartsData.Height))
	}
	if len(chartsData.Pooled) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Pooled is not empty\n" +
				testutil.ArrayToString("chartsData.Pooled", chartsData.Pooled))
	}
	if len(chartsData.Solo) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: chartsData.Solo is not empty\n" +
				testutil.ArrayToString("chartsData.Solo", chartsData.Solo))
	}
}

/// checkTicketPoolInfoIsDefault checks if TicketPoolInfo is in default mode:
//  - fields are set to default values (0)
//  - all the returned arrays expected to be empty
//  - info is not null (nil)
//
// These checks are sorted according to the TicketPoolInfo struct fields order
func checkTicketPoolInfoIsDefault(functionName string, info *types.TicketPoolInfo) {
	if info == nil {
		testutil.ReportTestFailed(
			functionName + "() failed: value is nil")
	}
	if info.Height != 0 {
		testutil.ReportTestFailed(
			functionName+"() failed: info.Height = %v,"+
				" 0 expected", info.Height)
	}
	if info.Size != 0 {
		testutil.ReportTestFailed(
			functionName+"() failed: info.Size = %v,"+
				" 0 expected", info.Size)
	}
	if info.Value != 0 {
		testutil.ReportTestFailed(
			functionName+"() failed: info.Value = %v,"+
				" 0 expected", info.Value)
	}
	if info.ValAvg != 0 {
		testutil.ReportTestFailed(
			functionName+"() failed: info.ValAvg = %v,"+
				" 0 expected", info.ValAvg)
	}
	if len(info.Winners) != 0 {
		testutil.ReportTestFailed(
			functionName + "() failed: info.Winners is not empty\n" +
				testutil.ArrayToString("info.", info.Winners))
	}
}
