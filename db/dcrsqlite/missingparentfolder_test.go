package dcrsqlite

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	defaultDataDirname = "test.data"
	defaultDBFileName  = "test.dcrdata.sqlt.db"
)

// tempFolderPath returns path of a temporary directory used by these tests to
// store data.
func tempFolderPath() (string, error) {
	return filepath.Abs(defaultDataDirname)
}

// InitTestDB creates default DB instance
func InitTestDB(dbFile string) (*DB, error) {
	dbInfo := &DBInfo{FileName: dbFile}
	return InitDB(dbInfo, func() {})
}

func makeTempDB(testName string) (string, error) {
	tempDir, err := tempFolderPath()
	if err != nil {
		return "", err
	}
	tempDir = filepath.Join(tempDir, testName)

	if err = os.RemoveAll(tempDir); err != nil {
		return "", err
	}

	dbFile := filepath.Join(tempDir, defaultDBFileName)
	return filepath.Abs(dbFile)
}

var reusableEmptyDB *DB

// ReusableEmptyDB returns a single reusable instance of an empty DB. The instance
// is created once during the first call. All the subsequent calls will return
// result cached in the reusableEmptyDB variable above.
func ReusableEmptyDB() (*DB, error) {
	if reusableEmptyDB != nil {
		return reusableEmptyDB, nil
	}
	dbFile, err := makeTempDB("reusableEmptyDB")
	if err != nil {
		return nil, err
	}
	return InitTestDB(dbFile)
}

// TestMissingParentFolder ensures InitDB() is able to create
// a new DB-file parent directory if necessary.
func TestMissingParentFolder(t *testing.T) {
	tempDir, err := tempFolderPath()
	if err != nil {
		t.Fatalf("invalid path: %v", err)
	}
	tempDir = filepath.Join(tempDir, t.Name())

	if err = os.RemoveAll(tempDir); err != nil {
		t.Fatalf("Failed to clear temp folder: %v", err)
	}

	// Specify DB file in non-existent path
	dbFile := filepath.Join(tempDir, "missing", defaultDBFileName)
	dbFile, err = filepath.Abs(dbFile)
	if err != nil {
		t.Fatalf("Failed to build temp folder path: %v", err)
	}

	_, err = InitTestDB(dbFile)
	if err != nil {
		t.Fatalf("Unable to create test db: %v", err)
	}
}
