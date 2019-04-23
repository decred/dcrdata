// +build chartests

package dcrsqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/db/cache"
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

	db = &WiredDB{DBDataSaver: &DBDataSaver{updatedDB, nil}}

	code := m.Run()

	// cleanUp
	os.Exit(code)
}

// TestSqliteCharts compares the data returned when a fresh data
// query is made with when an incremental change was added after new blocks
// were synced. No difference between the two should exist otherwise this test
// should fail. It also checks the order and duplicates is the x-axis dataset.
func TestSqliteCharts(t *testing.T) {
	charts := cache.NewChartData(0, time.Now(), &chaincfg.MainNetParams, context.Background())
	db.RegisterCharts(charts)

	charts.Update()
	blocks := charts.Blocks

	validate := func(tag string) {
		_, err := cache.ValidateLengths(blocks.Fees, blocks.PoolSize, blocks.PoolValue)
		if err != nil {
			t.Fatalf("%s blocks length validation error: %v", tag, err)
		}
	}

	if len(blocks.Fees) == 0 {
		t.Fatalf("no data deposited in Time array")
	}
	// The database will not validate because it does not start at block height 0.
	// This means that Update will not progress past the Blocks update right now.
	charts.Update()
	validate("initial update")

	blocks.Snip(50)
	validate("post-snip")

	charts.Update()
	// The data set lengths will actually be incorrect because the test
	// database is not zero-indexed, but the lengths should still all be equal
	// so validateLengths will not return an error.
	validate("second update")

}
