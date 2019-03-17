// Package testsconfig defines the various parameters that are needed to be set
// up pg and sqlite dbs for tests to run successfully.
package testsconfig

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"

	flags "github.com/jessevdk/go-flags"
)

const (
	// sqliteChartsTestsDb is the default name of the sqlite tests db file that
	// will be located in the temp dir created by the bash run_test.sh script.
	// The temp dir path is passed as an environment variable from run_tests.sh
	// script.
	sqliteChartsTestsDb = "test.sqlt.db"

	// Test PG connection config

	PGChartsTestsHost   = "localhost"
	PGChartsTestsPort   = "5432"
	PGChartsTestsUser   = "dcrdata" // postgres for admin operations
	PGChartsTestsPass   = ""
	PGChartsTestsDBName = "dcrdata_mainnet_test"
)

// This is the temp directory variable passed from the run_scripts.sh. It defines
// the location where temporary tests data will be help while tests are running.
// After tests are complete the tempDir and all its contents should be deleted.
var tempDir config

type config struct {
	sync.RWMutex
	fileName string `long:"tempdir" description:"Dir where temporary data is to be hold till tests are done." required:"true"`
}

// Parser the arguments passed.
func init() {
	preParser := flags.NewParser(&tempDir, flags.HelpFlag|flags.PassDoubleDash)
	_, err := preParser.Parse()

	if err != nil {
		log.Fatal(err)
		return
	}
}

// SqliteDbFilePath returns the complete sqlite db filepath completion and the
// tempDir.
func SqliteDbFilePath() (dbPath, tempDir string, err error) {
	tempDir, err = TempDir()
	if err != nil {
		return
	}
	dbPath = filepath.Join(tempDir, sqliteChartsTestsDb)
	return
}

// SqliteDumpDataFilePath returns the path to the sqlite db dump data filepath.
func SqliteDumpDataFilePath() (string, error) {
	dir, err := TempDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "sqlitedb", "data.sql"), nil
}

// TempDir returns tests temporary directory.
func TempDir() (string, error) {
	tempDir.RLock()
	defer tempDir.RUnlock()
	if tempDir.fileName != "" {
		return tempDir.fileName, nil
	}
	return "", fmt.Errorf("missing tempDir file path")
}
