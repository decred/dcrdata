package testutil

import (
	"os"
	"path/filepath"
)

const (
	DefaultDataDirname = "test.data"
	DefaultDBFileName  = "test.dcrdata.sqlt.db"
)

// TempFolderPath returns path of a temporary directory used by these tests to store data
func TempFolderPath() string {
	testDir, err := filepath.Abs(DefaultDataDirname)
	if err != nil {
		panic("Failed to produce DB-test folder path")
	}
	return testDir
}

// Ensures we run our test in a clean room. Removes all files created by any of these tests in the temp directory.
func ResetTempFolder() {
	testFolderPath := TempFolderPath()
	err := os.RemoveAll(testFolderPath)
	//Failed to clear test-files
	if err != nil {
		panic("Failed to clear temp folder")
	}
}

// FilePathInsideTempDir creates a path to a file inside the temp directory
func FilePathInsideTempDir(pathInsideTempFolder string) string {
	tempDir := TempFolderPath()
	targetPath := filepath.Join(tempDir, pathInsideTempFolder)
	targetPath, err := filepath.Abs(targetPath)

	if err != nil {
		panic("Failed to build a path: " + pathInsideTempFolder)
	}
	return targetPath
}
