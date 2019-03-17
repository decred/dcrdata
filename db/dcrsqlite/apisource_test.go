// +build chartests

package dcrsqlite

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/v4/db/dbtypes"
	"github.com/decred/dcrdata/v4/testutil/dbload/testsconfig"
	"github.com/gchaincl/dotsql"
)

var db *WiredDB

// TestMain sets up the sql test db and loads it with data.
func TestMain(m *testing.M) {
	dbPath, tempDir, err := testsconfig.SqliteDbFilePath()
	if err != nil {
		log.Error(err)
		return
	}

	bInfo := &DBInfo{FileName: dbPath}

	var cleanUp func() error
	db, cleanUp, err = InitWiredDB(bInfo, nil, nil, &chaincfg.MainNetParams, tempDir)
	if err != nil {
		log.Error(err)
		return
	}

	dumpDataPath, err := testsconfig.SqliteDumpDataFilePath()
	if err != nil {
		log.Error(err)
		return
	}

	// dumps the data in the *.sql file into the db.
	_, err = dotsql.LoadFromFile(dumpDataPath)
	if err != nil {
		log.Error(err)
		return
	}

	code := m.Run()

	// cleanUp
	cleanUp()

	os.RemoveAll(tempDir)
	os.Exit(code)
}

// TestIncrementalChartsQueries compares the data returned when a fresh data
// query is made and when an incremantal change was added after new blocks
// were synced. No difference between the two should exist otherwise this test
// should fail. It also checks the order and duplicates is the x-axis dataset.
func TestIncrementalChartsQueries(t *testing.T) {
	var oldData []*dbtypes.ChartsData
	err := db.SqliteChartsData(oldData)
	if err != nil {
		t.Fatalf("expected no error but found: %v", err)
	}

	// All supported historical sqlite charts that appear on charts page are 3.
	if len(oldData) != 3 {
		t.Fatalf("expected to 3 charts data but only found %d ", len(oldData))
	}

	// Validate the oldData contents by checking for duplicates and dataset order.

	// xvalArray maps the chart type to its respective x-axis dataset name.
	// Most charts either use timestamp or block height as the x-axis value.
	xvalArray := map[dbtypes.Charts]string{
		dbtypes.FeePerBlock:     "Height",
		dbtypes.TicketPoolSize:  "Time",
		dbtypes.TicketPoolValue: "Time",
	}

	// Charts x-axis coordinates dataset should not have duplicates. charts x-axis
	// coordinates should be ordered in ascending order.
	for chartType, xValName := range xvalArray {
		data := oldData[chartType.Pos()]

		t.Run("check_duplicates_n_ordering_for_"+chartType.String(), func(t *testing.T) {
			val := reflect.ValueOf(data).Elem()
			for i := 0; i < val.NumField(); i++ {
				if xValName == val.Type().Field(i).Name {
					switch strings.ToLower(xValName) {
					case "height":
						// All Height dataset is an array of type int
						heights := val.Field(i).Interface().([]int)
						heightsCopy := make([]int, len(heights))
						copy(heightsCopy, heights)

						// Sort and check for ascending order.
						sort.Ints(heightsCopy)
						if !reflect.DeepEqual(heightsCopy, heights) {
							t.Fatalf("expected x-axis data for chart %s to be in ascending order but it wasn't",
								chartType)
						}

						// check for duplicates
						var m = make(map[int]struct{})
						for _, elem := range heightsCopy {
							m[elem] = struct{}{}
						}
						if len(m) != len(heightsCopy) {
							t.Fatalf("expected x-axis data for chart %s to have no duplicates but found %d",
								chartType, len(heightsCopy)-len(m))
						}

					case "time":
						// All Time dataset is an array of type time.Time
						timestamp := val.Field(i).Interface().([]time.Time)
						timestampCopy := make([]time.Time, len(timestamp))
						copy(timestampCopy, timestamp)

						// Sort and check for ascending order.
						sort.Slice(
							timestampCopy,
							func(i, j int) bool { return timestampCopy[i].Before(timestampCopy[j]) },
						)
						if !reflect.DeepEqual(timestampCopy, timestamp) {
							t.Fatalf("expected x-axis data for chart %s to be in ascending order but it wasn't",
								chartType)
						}

						// check for duplicates
						var m = make(map[time.Time]struct{})
						for _, elem := range timestampCopy {
							m[elem] = struct{}{}
						}
						if len(m) != len(timestampCopy) {
							t.Fatalf("expected x-axis data for chart %s to have no duplicates but found %d",
								chartType, len(timestampCopy)-len(m))
						}

					default:
						t.Fatalf("unknown x-axis dataset name (%s) found", xValName)
					}
				}
			}
		})
	}

	dataCopy := make([]*dbtypes.ChartsData, len(oldData))
	copy(dataCopy, oldData)

	// In this test, no new data is expected to be added by the queries. The correct
	// data returned after passing dataCopy should match oldData. i.e. the oldData
	// arrays length and content should match the returned result.

	t.Run("Check_if_extra_chart_data_is_added", func(t *testing.T) {
		err := db.SqliteChartsData(dataCopy[:])
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		if !reflect.DeepEqual(dataCopy, oldData) {
			t.Fatalf("expected no new data to be added to dataCopy but it was.")
		}
	})

	// Dropping some values in dataCopy is meant to simulate the missing data
	// that will be updated via incremental change queries. The values are
	// deleted in each dataset for all the sqlite charts.

	// dbtypes.FeePerBlock: index 0 here and 13 in cache
	index := 0
	total := len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].Size = dataCopy[index].Size[:total-5]

	// dbtypes.TicketPoolSize: index 1 here and 14 in cache
	index = 1
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].ChainSize = dataCopy[index].ChainSize[:total-5]

	// dbtypes.TicketPoolValue: index 2 here and 15 in cache
	index = 2
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].ChainWork = dataCopy[index].ChainWork[:total-5]

	t.Run("Check_if_the_incremental_change_matches_oldData", func(t *testing.T) {
		err := db.SqliteChartsData(dataCopy)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		if !reflect.DeepEqual(dataCopy, oldData) {
			t.Fatalf("expected no new data to be added to dataCopy but it was.")
		}
	})
}
