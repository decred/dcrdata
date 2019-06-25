// +build chartdata

package dcrsqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/db/cache/v2"
	tc "github.com/decred/dcrdata/testutil/dbconfig"
	_ "github.com/mattn/go-sqlite3"
)

type client struct {
	d *sql.DB
}

func (c *client) Path() string {
	dbPath, err := tc.SqliteDumpDataFilePath()
	if err != nil {
		panic(err)
	}
	return dbPath
}

func (c *client) Runner(query string) error {
	_, err := c.d.Exec(query)
	return err
}

var db *WiredDB

// TestMain sets up the sql test db and loads it with data.
func TestMain(m *testing.M) {
	dbPath, err := tc.SqliteDbFilePath()
	if err != nil {
		panic(err)
	}

	os.RemoveAll(dbPath)

	rawdb, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		panic(err)
	}

	defer rawdb.Close()

	if err = rawdb.Ping(); err != nil {
		panic(err)
	}

	// dumps the data in the *.sql file into the db.
	if err = tc.CustomScanner(&client{d: rawdb}); err != nil {
		panic(err)
	}

	// Get access to queries. Done after migration because it auto creates missing
	// tables. The migrations also create the tables.
	updatedDB, err := NewDB(rawdb, func() {})
	if err != nil {
		panic(err)
	}

	db = &WiredDB{DBDataSaver: NewDBDataSaver(updatedDB)}

	code := m.Run()

	// cleanUp
	os.Exit(code)
}

// TestSqliteCharts compares the data returned when a fresh data
// query is made with when an incremental change was added after new blocks
// were synced. No difference between the two should exist otherwise this test
// should fail. It also checks the order and duplicates is the x-axis dataset.
func TestSqliteCharts(t *testing.T) {
	charts := cache.NewChartData(context.Background(), 0, &chaincfg.MainNetParams)
	db.RegisterCharts(charts)
	blocks := charts.Blocks

	validate := func(tag string) {
		_, err := cache.ValidateLengths(blocks.Fees, blocks.PoolSize, blocks.PoolValue)
		if err != nil {
			t.Fatalf("%s blocks length validation error: %v", tag, err)
		}
	}
	charts.Update()
	validate("initial update")
	feesLen := len(blocks.Fees)

	blocks.Snip(50)
	validate("post-snip")

	charts.Update()
	validate("second update")

	if feesLen != len(blocks.Fees) {
		t.Fatalf("unexpected fees data length %d != %d", feesLen, len(blocks.Fees))
	}

}
