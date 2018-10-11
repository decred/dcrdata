// package testutil provides some helper functions to be used in unit tests.
package testutil

import (
	"os"
	"path/filepath"
)

const (
	DefaultDataDirname = "test.data"
	DefaultDBFileName  = "test.dcrdata.sqlt.db"
)

// TempFolderPath returns path of a temporary directory used by these tests to
// store data.
func TempFolderPath() string {
	testDir, err := filepath.Abs(DefaultDataDirname)
	if err != nil {
		ReportTestIsNotAbleToTest("Failed to produce DB-test folder path: %v", err)
	}
	return testDir
}

// ResetTempFolder ensures we run our test in a clean room. Removes all files
// created by any previous tests in the temp directory. Returns the full test
// folder path.
func ResetTempFolder(testSubFolder *string) string {
	testFolderPath := TempFolderPath()
	// Clear all test files when testSubFolder is not specified
	if testSubFolder != nil { //testSubFolder is specified
		testFolderPath = filepath.Join(testFolderPath, *testSubFolder)
	}
	err := os.RemoveAll(testFolderPath)
	// Failed to clear test-files
	if err != nil {
		ReportTestIsNotAbleToTest("Failed to clear temp folder: %v", err)
	}
	return testFolderPath
}

// FilePathInsideTempDir creates a path to a file inside the temp directory.
func FilePathInsideTempDir(pathInsideTempFolder string) string {
	tempDir := TempFolderPath()
	targetPath := filepath.Join(tempDir, pathInsideTempFolder)
	targetPath, err := filepath.Abs(targetPath)
	if err != nil {
		ReportTestIsNotAbleToTest("Failed to build path %s: %v",
			pathInsideTempFolder, err)
	}
	return targetPath
}
