// +build chartests

package dcrsqlite

import (
	"database/sql"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/decred/dcrdata/v4/db/dbtypes"
	tc "github.com/decred/dcrdata/v4/testutil/dbload/testsconfig"
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

// TestSqliteChartsData compares the data returned when a fresh data
// query is made with when an incremental change was added after new blocks
// were synced. No difference between the two should exist otherwise this test
// should fail. It also checks the order and duplicates is the x-axis dataset.
func TestSqliteChartsData(t *testing.T) {
	var oldData = make([]*dbtypes.ChartsData, dbtypes.SqliteChartsCount)
	err := db.SqliteChartsData(oldData)
	if err != nil {
		t.Fatalf("expected no error but found: %v", err)
	}

	// All supported historical sqlite charts that appear on charts page are
	// defined by dbtypes.SqliteChartsCount
	if len(oldData) != dbtypes.SqliteChartsCount {
		t.Fatalf("expected to %d charts data but only found %d ",
			dbtypes.SqliteChartsCount, len(oldData))
	}

	// Validate the oldData contents by checking for duplicates and dataset order.

	// xvalArray maps the chart type to its respective x-axis dataset name.
	// Most charts either use timestamp or block height as the x-axis value.
	xvalArray := map[dbtypes.ChartType]string{
		dbtypes.FeePerBlock:     "Height",
		dbtypes.TicketPoolSize:  "Time",
		dbtypes.TicketPoolValue: "Time",
	}

	if len(xvalArray) != dbtypes.SqliteChartsCount {
		t.Fatalf("some charts' x-axis values are unaccounted for.")
	}

	t.Run("check_duplicates_n_ordering_for_", func(t *testing.T) {
		// Charts x-axis coordinates dataset should not have duplicates. charts x-axis
		// coordinates should be ordered in ascending order.
		for chartV, xValName := range xvalArray {
			data := oldData[chartV.SqlitePos()]

			t.Run(chartV.String(), func(t *testing.T) {
				switch strings.ToLower(xValName) {
				case "height":
					// All Height dataset is an array of type uint64
					heights := data.Height
					if len(heights) == 0 {
						t.Fatalf("expected the x-axis dataset to have entries but had none")
						return
					}

					heightsCopy := make([]uint64, len(heights))
					copy(heightsCopy, heights)

					// Sort and check for ascending order.
					sort.Slice(heightsCopy, func(i, j int) bool { return heightsCopy[i] < heightsCopy[j] })

					if !reflect.DeepEqual(heightsCopy, heights) {
						t.Fatalf("expected x-axis data for chart (%s) to be in ascending order but it wasn't",
							chartV)
					}

					// check for duplicates
					var m = make(map[uint64]struct{})
					for _, elem := range heightsCopy {
						m[elem] = struct{}{}
					}
					if len(m) != len(heightsCopy) {
						t.Fatalf("x-axis dataset for chart (%s) was found to have %d duplicates",
							chartV, len(heightsCopy)-len(m))
					}

				case "time":
					// All Time dataset is an array of type dbtypes.TimeDef
					timestamp := data.Time
					if len(timestamp) == 0 {
						t.Fatalf("expected the x-axis dataset to have entries but had none")
						return
					}

					timestampCopy := make([]dbtypes.TimeDef, len(timestamp))
					copy(timestampCopy, timestamp)

					// Sort and check for ascending order.
					sort.Slice(timestampCopy, func(i, j int) bool { return timestampCopy[i].T.Before(timestampCopy[j].T) })

					if !reflect.DeepEqual(timestampCopy, timestamp) {
						t.Fatalf("expected x-axis data for chart (%s) to be in ascending order but it wasn't",
							chartV)
					}

					// check for duplicates
					var m = make(map[dbtypes.TimeDef]struct{})
					for _, elem := range timestampCopy {
						m[elem] = struct{}{}
					}
					if len(m) != len(timestampCopy) {
						t.Fatalf("x-axis dataset for chart (%s) was found to have %d duplicates",
							chartV, len(timestampCopy)-len(m))
					}

				default:
					t.Fatalf("unknown x-axis dataset name (%s) found", xValName)
				}
			})
		}
	})

	dataCopy := make([]*dbtypes.ChartsData, len(oldData))
	copy(dataCopy, oldData)

	// In this test, no new data is expected to be added by the queries. The correct
	// data returned after passing dataCopy should match oldData. i.e. the oldData
	// arrays length and content should match the returned result.

	t.Run("Check_if_invalid_data_was_added_for_", func(t *testing.T) {
		err := db.SqliteChartsData(dataCopy[:])
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		for i := range oldData {
			chartName := dbtypes.ChartType(i).String()
			t.Run(chartName, func(t *testing.T) {
				if !reflect.DeepEqual(dataCopy[i], oldData[i]) {
					t.Fatalf("expected no new data to be added to dataCopy for (%s) but it was.",
						chartName)
				}
			})
		}
	})

	// Dropping some values in dataCopy is meant to simulate the missing data
	// that will be updated via incremental change queries. The values are
	// deleted in each dataset for all the sqlite charts.

	diff := 10

	// dbtypes.FeePerBlock: index 0 here and 13 in cache
	index := dbtypes.FeePerBlock.SqlitePos()
	feeC := len(dataCopy[index].Height)
	if feeC < diff {
		feeC = 0
	} else {
		feeC -= diff
	}
	dataCopy[index].Height = dataCopy[index].Height[:feeC]
	dataCopy[index].SizeF = dataCopy[index].SizeF[:feeC]

	// dbtypes.TicketPoolSize: index 1 here and 14 in cache
	index = dbtypes.TicketPoolSize.SqlitePos()
	sizeC := len(dataCopy[index].Time)
	if sizeC < diff {
		sizeC = 0
	} else {
		sizeC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:sizeC]
	dataCopy[index].SizeF = dataCopy[index].SizeF[:sizeC]

	// dbtypes.TicketPoolValue: index 2 here and 15 in cache
	index = dbtypes.TicketPoolValue.SqlitePos()
	valC := len(dataCopy[index].Time)
	if valC < diff {
		valC = 0
	} else {
		valC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:valC]
	dataCopy[index].ValueF = dataCopy[index].ValueF[:valC]

	t.Run("Match_incremental_change_with_oldData_for_", func(t *testing.T) {
		err := db.SqliteChartsData(dataCopy)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		for i := range oldData {
			chartName := dbtypes.ChartType(i).String()
			t.Run(chartName, func(t *testing.T) {
				if !reflect.DeepEqual(dataCopy[i], oldData[i]) {
					t.Fatalf("expected no new data to be added to dataCopy for (%s) but it was.",
						chartName)
				}
			})
		}
	})
}
