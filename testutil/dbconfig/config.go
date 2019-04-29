// Package testsconfig defines the various parameters and methods needed to be
// set up pg and sqlite dbs for tests to run successfully.
package testsconfig

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
)

const (
	// sqliteChartsTestsDb is the default name of the sqlite tests db file that
	// will be located.
	sqliteChartsTestsDb = "test.sqlt.db"

	// Test PG db connection config

	PGChartsTestsHost   = "localhost"
	PGChartsTestsPort   = "5432"
	PGChartsTestsUser   = "dcrdata"
	PGChartsTestsPass   = ""
	PGChartsTestsDBName = "dcrdata_mainnet_test"
	defaultRange        = "0-199"
)

// SqliteDbFilePath returns the absolute sqlite db filepath when accessed from
// dcrsqlite package.
func SqliteDbFilePath() (string, error) {
	tempDir, err := filepath.Abs("../../testutil/dbload/testsconfig/test.data/")
	dbPath := filepath.Join(tempDir, sqliteChartsTestsDb)
	return dbPath, err
}

// SqliteDumpDataFilePath returns the path to the sqlite db dump data filepath
// when accessed from dcrsqlite package.
func SqliteDumpDataFilePath() (string, error) {
	blockRange := os.Getenv("BLOCK_RANGE")
	if blockRange == "" {
		blockRange = defaultRange
	}
	return filepath.Abs("../../testutil/dbload/testsconfig/test.data/sqlite_" + blockRange + ".sql")
}

// Migrations enables a custom migration runner to be used to load data from a
// given *.sql file dump into the migrations runner method.
type Migrations interface {
	// Path returns the filepath to the *.sql dump file.
	Path() string
	// Runner executes the scanned individual queries one after another.
	Runner(query string) error
}

// CustomScanner enables a given *.sql file dump to be scanned and loaded
// into the respectived query runner using the Runner method.
func CustomScanner(m Migrations) error {
	file, err := os.Open(m.Path())
	if err != nil {
		return err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(scanQueries)

	for scanner.Scan() {
		text := scanner.Text()
		if len(text) == 0 {
			continue
		}

		if err = m.Runner(text); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// scanQueries scans individual token separated by semi-colons since the queries
// are separated by semi-colons.
func scanQueries(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, bufio.ErrFinalToken
	}
	if i := bytes.LastIndex(data, []byte(";\n")); i >= 0 {
		return i + 1, deleteComments(data[:i]), nil
	}
	if atEOF {
		return len(data), deleteComments(data), nil
	}
	return 0, nil, nil
}

// Comments start with (--) and end with a new line.
func deleteComments(a []byte) []byte {
	re := regexp.MustCompile(`^--[\s*\w*[[:punct:]]*]*$\n`)
	a = re.ReplaceAll(a, []byte{})

	// delete "test_" from all table references.
	re = regexp.MustCompile(`test_`)
	return re.ReplaceAll(a, []byte{})
}
