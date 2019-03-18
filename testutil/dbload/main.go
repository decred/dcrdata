package main

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"

	tc "github.com/decred/dcrdata/v4/testutil/dbload/testsconfig"
	_ "github.com/lib/pq"
)

type client struct {
	db *sql.DB
}

// Before running the tool, the appropriate test db should be created by:
// psql -U postgres -c "DROP DATABASE IF EXISTS dcrdata_mainnet_test"
// psql -U postgres -c "CREATE DATABASE dcrdata_mainnet_test"

// This tool loads the test data into the test db.

func main() {
	connStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=disable",
		tc.PGChartsTestsHost, tc.PGChartsTestsPort, tc.PGChartsTestsUser,
		tc.PGChartsTestsDBName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
		return
	}

	err = db.Ping()
	if err != nil {
		log.Fatalf("Modify (pg_hba.conf - pgsql) for (%s) to work : %v", connStr, err)
		return
	}

	if err = tc.CustomScanner(&client{db}); err != nil {
		log.Fatal(err)
	}
}

// Path returns the path to the pg dump file.
func (c *client) Path() string {
	str, err := filepath.Abs("testutil/dbload/testsconfig/test.data/pgsql_dump.sql")
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
