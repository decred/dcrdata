// package testutil provides some helper functions to be used in unit tests.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	DefaultDataDirname = "test.data"
	DefaultDBFileName  = "test.dcrdata.sqlt.db"
)

// TempFolderPath returns path of a temporary directory used by these tests to
// store data.
func TempFolderPath(t *testing.T) string {
	testDir, err := filepath.Abs(DefaultDataDirname)
	if err != nil {
		t.Fatalf("Failed to produce DB-test folder path")
	}
	return testDir
}

// Ensures we run our test in a clean room. Removes all files created by any of
// these tests in the temp directory.
func ResetTempFolder(t *testing.T) {
	testFolderPath := TempFolderPath(t)
	err := os.RemoveAll(testFolderPath)
	// Failed to clear test-files
	if err != nil {
		t.Fatalf("Failed to clear temp folder")
	}
}

// FilePathInsideTempDir creates a path to a file inside the temp directory.
func FilePathInsideTempDir(t *testing.T, pathInsideTempFolder string) string {
	tempDir := TempFolderPath(t)
	targetPath := filepath.Join(tempDir, pathInsideTempFolder)
	targetPath, err := filepath.Abs(targetPath)
	if err != nil {
		t.Fatalf("Failed to build a path: " + pathInsideTempFolder)
	}
	return targetPath
}
