// +build !fullpgdb

package dbconfig

import (
	"os"
	"path/filepath"
)

const (
	// sqliteChartsTestsDb is the default name of the sqlite tests db file that
	// will be located.
	sqliteChartsTestsDb = "test.sqlt.db"

	defaultSqliteBlockRange = "0-199"
)

// Test DB server and database config.
const (
	PGTestsHost   = "localhost" // "/run/postgresql" for UNIX socket
	PGTestsPort   = "5432"      // "" for UNIX socket
	PGTestsUser   = "postgres"  // "dcrdata" for full database rather than test data repo
	PGTestsPass   = ""
	PGTestsDBName = "dcrdata_mainnet_test"
)

// SqliteDbFilePath returns the absolute sqlite db filepath when accessed from
// dcrsqlite package.
func SqliteDbFilePath() (string, error) {
	tempDir, err := filepath.Abs("../../testutil/dbconfig/test.data/")
	dbPath := filepath.Join(tempDir, sqliteChartsTestsDb)
	return dbPath, err
}

// SqliteDumpDataFilePath returns the path to the sqlite db dump data filepath
// when accessed from dcrsqlite package.
func SqliteDumpDataFilePath() (string, error) {
	blockRange := os.Getenv("BLOCK_RANGE")
	if blockRange == "" {
		blockRange = defaultSqliteBlockRange
	}
	return filepath.Abs("../../testutil/dbconfig/test.data/sqlite_" + blockRange + ".sql")
}
