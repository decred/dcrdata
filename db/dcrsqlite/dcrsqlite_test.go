package dcrsqlite

/*
This file contains package-related test-setup utils
*/
import (
	"path/filepath"

	"github.com/decred/dcrdata/api/types"
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
