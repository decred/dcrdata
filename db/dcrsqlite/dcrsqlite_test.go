package dcrsqlite

/*
This file contains package-related test-setup utils
*/
import (
	"path/filepath"

	"github.com/decred/dcrdata/testutil"
)

const (
	TestDBFileName = "test-data.db"
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

var reusableEmptyDB *DB = nil

// ObtainReusableEmptyDB returns a single reusable instance of an empty DB. The instance
// is created once during the first call. All the subsequent calls will return
// result cached in the reusableEmptyDB variable above.
func ObtainReusableEmptyDB() *DB {
	if reusableEmptyDB == nil {
		testName := "reusableEmptyDB"
		testutil.ResetTempFolder(&testName)
		target := filepath.Join(testName, testutil.DefaultDBFileName)
		targetDBFile := testutil.FilePathInsideTempDir(target)
		reusableEmptyDB = InitTestDB(targetDBFile)
	}
	return reusableEmptyDB
}

var testDBs = make(map[string]*DB)

func ObtainDB(tag string) *DB {
	tdb := testDBs[tag]
	if tdb == nil {
		tdb = loadTDB(tag)
		testDBs[tag] = tdb
	}
	return tdb
}

func PathToTestDBFile(tag string) string {
	return testutil.PathToTestDataFile(tag, TestDBFileName)
}

func loadTDB(tag string) *DB {
	testDBFile := PathToTestDBFile(tag)
	testutil.Log("        testDBFile", testDBFile)
	db := InitTestDB(testDBFile)
	return db
}
