package dcrsqlite

/*
This file contains package-related test-setup utils
*/
import (
	"github.com/decred/dcrdata/testutil"
	"path/filepath"
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
