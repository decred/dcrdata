package dcrsqlite

import (
	"os"

	"path/filepath"
	"testing"
)

// Clears target folder content
func RemoveContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}

const (
	defaultDataDirname = "test.data"
	defaultDBFileName  = "test.dcrdata.sqlt.db"
)

// Returns path of a temporary directory used by these tests to store some data
func TempFolderPath(t *testing.T) string {
	testDir, err := filepath.Abs(defaultDataDirname)
	if err != nil {
		t.Fatalf("Failed to produce DB-test folder path: %v", err)
	}
	return testDir
}

// Ensures we run our test in a clean room. Removes all files created by any of these tests in the temp directory.
func ResetTempFolder(t *testing.T) {
	testFolderPath := TempFolderPath(t)
	err := os.RemoveAll(testFolderPath)
	//Failed to clear test-files
	if err != nil {
		t.Fatalf("Failed to clear temp folder %v", err)
	}
}

// Creates a path to a file inside the temp directory
func FilePathInsideTempDir(t *testing.T, pathInsideTempFolder string) string {
	tempDir := TempFolderPath(t)
	targetPath := filepath.Join(tempDir, pathInsideTempFolder)
	targetPath, err := filepath.Abs(targetPath)

	if err != nil {
		t.Fatalf("Failed to build a path %v", err)
	}
	return targetPath
}

// TestMissingParentFolder ensures InitDB() is able to create a new DB-file parent directory if necessary
// See https://github.com/decred/dcrdata/issues/515
func TestMissingParentFolder(t *testing.T) {
	ResetTempFolder(t)
	targetDBFile := FilePathInsideTempDir(t, "x/y/z/"+defaultDBFileName)
	dbInfo := &DBInfo{FileName: targetDBFile}
	db, err := InitDB(dbInfo)

	if err != nil {
		t.Fatalf("InitDB() failed: %v", err)
	}

	if db == nil {
		t.Fatalf("InitDB() failed")
	}
}
