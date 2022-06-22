package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tc "github.com/decred/dcrdata/v8/testutil/dbconfig"
	_ "github.com/lib/pq"
)

const defaultBlockRange = "0-199"

type client struct {
	db *sql.DB
}

// Before running the tool, the appropriate test db should be created by:
// psql -U postgres -c "DROP DATABASE IF EXISTS dcrdata_mainnet_test"
// psql -U postgres -c "CREATE DATABASE dcrdata_mainnet_test"

// This tool loads the test data into the test db.

func main() {
	var connStr string
	if tc.PGTestsPort == "" {
		connStr = fmt.Sprintf("host=%s user=%s dbname=%s sslmode=disable",
			tc.PGTestsHost, tc.PGTestsUser, tc.PGTestsDBName)
	} else {
		connStr = fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=disable",
			tc.PGTestsHost, tc.PGTestsPort, tc.PGTestsUser, tc.PGTestsDBName)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("sql.Open:", err)
		return
	}

	err = db.Ping()
	if err != nil {
		log.Fatalf(`Modify pg_hba.conf or change the host for "%s" to work: %v`,
			connStr, err)
		return
	}

	if err = tc.CustomScanner(&client{db}); err != nil {
		log.Fatal("CustomScanner:", err)
	}
}

// Path returns the path to the pg dump file.
func (c *client) Path() string {
	blockRange := os.Getenv("BLOCK_RANGE")
	if blockRange == "" {
		blockRange = defaultBlockRange
	}
	str, err := filepath.Abs("testutil/dbconfig/test.data/pgsql_" + blockRange + ".sql")
	if err != nil {
		panic(err)
	}
	return str
}

// Runner executes the scanned db query.
func (c *client) Runner(q string) error {
	_, err := c.db.Exec(q)
	return err
}
