// +build chartests

package dcrpg

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/v4/db/dbtypes"
)

var (
	db           *ChainDB
	addrCacheCap int = 1e4
)

func openDB() (func() error, error) {
	dbi := DBInfo{
		Host:   "localhost",
		Port:   "5432",
		User:   "dcrdata", // postgres for admin operations
		Pass:   "",
		DBName: "dcrdata_mainnet_test",
	}
	var err error
	db, err = NewChainDB(&dbi, &chaincfg.MainNetParams, nil, true, true, addrCacheCap)
	cleanUp := func() error { return nil }
	if db != nil {
		cleanUp = db.Close
	}
	return cleanUp, err
}

func TestMain(m *testing.M) {
	// your func
	cleanUp, err := openDB()
	defer cleanUp()
	if err != nil {
		panic(fmt.Sprintln("no db for testing:", err))
	}

	retCode := m.Run()

	// call with result of m.Run()
	os.Exit(retCode)
}

// TestIncrementalChartsQueries compares the data returned when a fresh data
// query is made and when an incremantal change was added after new blocks
// were synced. No difference between the two should exist otherwise this test
// should fail. It also checks the order and duplicates is the x-axis dataset.
func TestIncrementalChartsQueries(t *testing.T) {
	var oldData []*dbtypes.ChartsData
	err := db.PgChartsData(oldData)
	if err != nil {
		t.Fatalf("expected no error but found: %v", err)
	}

	// All supported historical charts that appear on charts page are 13.
	if len(oldData) != 13 {
		t.Fatalf("expected to 13 charts data but only found %d ", len(oldData))
	}

	// Validate the oldData contents by checking for duplicates and dataset order.

	// xvalArray maps the chart type to its respective x-axis dataset name.
	// Most charts either use timestamp or block height as the x-axis value.
	xvalArray := map[dbtypes.Charts]string{
		dbtypes.AvgBlockSize:    "Time",
		dbtypes.BlockChainSize:  "Time",
		dbtypes.ChainWork:       "Time",
		dbtypes.CoinSupply:      "Time",
		dbtypes.DurationBTW:     "Height",
		dbtypes.HashRate:        "Time",
		dbtypes.POWDifficulty:   "Time",
		dbtypes.TicketByWindows: "Height",
		dbtypes.TicketPrice:     "Time",
		dbtypes.TicketsByBlocks: "Height",
		dbtypes.TicketSpendT:    "Height",
		dbtypes.TxPerBlock:      "Height",
		dbtypes.TxPerDay:        "Time",
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
						// All Height dataset is an array of type int64
						heights := val.Field(i).Interface().([]int64)
						heightsCopy := make([]int64, len(heights))
						copy(heightsCopy, heights)

						// Sort and check for ascending order.
						sort.Int64s(heightsCopy)
						if !reflect.DeepEqual(heightsCopy, heights) {
							t.Fatalf("expected x-axis data for chart %s to be in ascending order but it wasn't",
								chartType)
						}

						// check for duplicates
						var m = make(map[int64]struct{})
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
		err := db.PgChartsData(dataCopy)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		if !reflect.DeepEqual(dataCopy, oldData) {
			t.Fatalf("expected no new data to be added to dataCopy but it was.")
		}
	})

	// Dropping some values in dataCopy is meant to simulate the missing data
	// that will be updated via incremental change queries. The values are
	// deleted in each dataset for all the pg charts.

	// dbtypes.AvgBlockSize: -> index 0 here and 0 in cache
	index := dbtypes.AvgBlockSize.Pos()
	total := len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].Size = dataCopy[index].Size[:total-5]

	// dbtypes.BlockChainSize:  -> index 1 here and 1 in cache
	index = dbtypes.BlockChainSize.Pos()
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].ChainSize = dataCopy[index].ChainSize[:total-5]

	// dbtypes.ChainWork:  -> index 2 here and 2 in cache
	index = dbtypes.ChainWork.Pos()
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].ChainWork = dataCopy[index].ChainWork[:total-5]

	// dbtypes.CoinSupply:  -> index 3 here and 3 in cache
	index = dbtypes.CoinSupply.Pos()
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].ValueF = dataCopy[index].ValueF[:total-5]

	// dbtypes.DurationBTW:  -> index 4 here and 4 in cache
	index = dbtypes.DurationBTW.Pos()
	total = len(dataCopy[index].Time)
	dataCopy[index].Height = dataCopy[index].Height[:total-5]
	dataCopy[index].ValueF = dataCopy[index].ValueF[:total-5]

	//  dbtypes.HashRate:  -> index 5 here and 5 in cache
	index = dbtypes.HashRate.Pos()
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].NetHash = dataCopy[index].NetHash[:total-5]

	// dbtypes.POWDifficulty:  -> index 6 here and 6 in cache
	index = dbtypes.POWDifficulty.Pos()
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].Difficulty = dataCopy[index].Difficulty[:total-5]

	// dbtypes.TicketByWindows:  -> index 7 here and 7 in cache
	index = dbtypes.TicketByWindows.Pos()
	total = len(dataCopy[index].Height)
	dataCopy[index].Height = dataCopy[index].Height[:total-5]
	dataCopy[index].Solo = dataCopy[index].Solo[:total-5]
	dataCopy[index].Pooled = dataCopy[index].Pooled[:total-5]

	// dbtypes.TicketPrice:  -> index 8 here and 8 in cache
	index = dbtypes.TicketPrice.Pos()
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].ValueF = dataCopy[index].ValueF[:total-5]

	// dbtypes.TicketsByBlocks:  -> index 9 here and 9 in cache
	index = dbtypes.TicketsByBlocks.Pos()
	total = len(dataCopy[index].Height)
	dataCopy[index].Height = dataCopy[index].Height[:total-5]
	dataCopy[index].Solo = dataCopy[index].Solo[:total-5]
	dataCopy[index].Pooled = dataCopy[index].Pooled[:total-5]

	// dbtypes.TicketSpendT:  -> index 10 here and 10 in cache
	index = dbtypes.TicketSpendT.Pos()
	total = len(dataCopy[index].Height)
	dataCopy[index].Height = dataCopy[index].Height[:total-5]
	dataCopy[index].Unspent = dataCopy[index].Unspent[:total-5]
	dataCopy[index].Revoked = dataCopy[index].Revoked[:total-5]

	// dbtypes.TxPerBlock:  -> index 11 here and 11 in cache
	index = dbtypes.TxPerBlock.Pos()
	total = len(dataCopy[index].Height)
	dataCopy[index].Height = dataCopy[index].Height[:total-5]
	dataCopy[index].Count = dataCopy[index].Count[:total-5]

	// dbtypes.TxPerDay:  -> index 12 here and 12 in cache
	index = dbtypes.TxPerDay.Pos()
	total = len(dataCopy[index].Time)
	dataCopy[index].Time = dataCopy[index].Time[:total-5]
	dataCopy[index].Count = dataCopy[index].Count[:total-5]

	t.Run("Check_if_the_incremental_change_matches_oldData", func(t *testing.T) {
		err := db.PgChartsData(dataCopy)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		if !reflect.DeepEqual(dataCopy, oldData) {
			t.Fatalf("expected no new data to be added to dataCopy but it was.")
		}
	})
}
