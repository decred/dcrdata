// +build chartests

package dcrpg

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/v4/db/dbtypes"
	tc "github.com/decred/dcrdata/v4/testutil/dbload/testsconfig"
)

var (
	db           *ChainDB
	addrCacheCap int = 1e4
)

func openDB() (func() error, error) {
	dbi := DBInfo{
		Host:   tc.PGChartsTestsHost,
		Port:   tc.PGChartsTestsPort,
		User:   tc.PGChartsTestsUser,
		Pass:   tc.PGChartsTestsPass,
		DBName: tc.PGChartsTestsDBName,
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

// TestPgChartsData compares the data returned when a fresh data query is made
// with when an incremental change was added after new blocks were synced.
// No difference between the two should exist otherwise this test should fail.
// It also checks the order and duplicates in the x-axis dataset.
func TestPgChartsData(t *testing.T) {
	var oldData = make([]*dbtypes.ChartsData, dbtypes.PgChartsCount)
	err := db.PgChartsData(oldData)
	if err != nil {
		t.Fatalf("expected no error but found: %v", err)
		return
	}

	// All supported pg historical charts count is defined by dbtypes.PgChartsCount.
	if len(oldData) != dbtypes.PgChartsCount {
		t.Fatalf("expected to %d charts data but only found %d ",
			dbtypes.PgChartsCount, len(oldData))
		return
	}

	// Validate the oldData contents by checking for duplicates and dataset order.

	// xvalArray maps the chart type to its respective x-axis dataset name.
	// Most charts either use timestamp or block height as the x-axis value.
	xvalArray := map[dbtypes.ChartType]string{
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

	if len(xvalArray) != dbtypes.PgChartsCount {
		t.Fatalf("some charts x-axis details are unaccounted for")
		return
	}

	// Charts x-axis coordinates dataset should not have duplicates. charts x-axis
	// coordinates should be ordered in ascending order.
	t.Run("check_duplicates_&_ordering_for_", func(t *testing.T) {
		for chartT, xValName := range xvalArray {
			data := oldData[chartT.Pos()]

			t.Run(chartT.String(), func(t *testing.T) {
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
						t.Fatalf("expected x-axis data for chart (%s) to be in ascending order but it wasn't", chartT)
						return
					}

					// check for duplicates
					var m = make(map[uint64]struct{})
					for _, elem := range heightsCopy {
						m[elem] = struct{}{}
					}
					if len(m) != len(heightsCopy) {
						t.Fatalf("x-axis dataset for chart (%s) was found to have %d duplicates",
							chartT, len(heightsCopy)-len(m))
						return
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
						t.Fatalf("expected x-axis data for chart (%s) to be in ascending order but it wasn't", chartT)
						return
					}

					// check for duplicates
					var m = make(map[dbtypes.TimeDef]struct{})
					for _, elem := range timestampCopy {
						m[elem] = struct{}{}
					}
					if len(m) != len(timestampCopy) {
						t.Fatalf("x-axis dataset for chart (%s) was found to have %d duplicates",
							chartT, len(timestampCopy)-len(m))
						return
					}

				default:
					t.Fatalf("unknown x-axis dataset name (%s) found", xValName)
					return
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
		if err = db.PgChartsData(dataCopy); err != nil {
			t.Fatalf("expected no error but found: %v", err)
			return
		}

		for k := range dataCopy {
			chartName := dbtypes.ChartType(k).String()
			t.Run(chartName, func(t *testing.T) {
				if !reflect.DeepEqual(dataCopy[k], oldData[k]) {
					t.Fatalf("expected no new data to be added for chart (%s) to dataCopy but it was.",
						chartName)
					return
				}
			})
		}
	})

	// Dropping some values in dataCopy is meant to simulate the missing data
	// that will be updated via incremental change queries. The values are
	// deleted in each dataset for all the pg charts.

	// diff defines the number of entries to delete from the parent array.
	diff := 5

	// dbtypes.AvgBlockSize: -> index 0 here and 0 in cache
	index := dbtypes.AvgBlockSize.Pos()
	avgC := len(dataCopy[index].Time)
	if avgC < diff {
		avgC = 0
	} else {
		avgC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:avgC]
	dataCopy[index].Size = dataCopy[index].Size[:avgC]

	// dbtypes.BlockChainSize:  -> index 1 here and 1 in cache
	index = dbtypes.BlockChainSize.Pos()
	bSizeC := len(dataCopy[index].Time)
	if bSizeC < diff {
		bSizeC = 0
	} else {
		bSizeC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:bSizeC]
	dataCopy[index].ChainSize = dataCopy[index].ChainSize[:bSizeC]

	// dbtypes.ChainWork:  -> index 2 here and 2 in cache
	index = dbtypes.ChainWork.Pos()
	chainC := len(dataCopy[index].Time)
	if chainC < diff {
		chainC = 0
	} else {
		chainC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:chainC]
	dataCopy[index].ChainWork = dataCopy[index].ChainWork[:chainC]

	// dbtypes.CoinSupply:  -> index 3 here and 3 in cache
	index = dbtypes.CoinSupply.Pos()
	coinC := len(dataCopy[index].Time)
	if coinC < diff {
		coinC = 0
	} else {
		coinC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:coinC]
	dataCopy[index].ValueF = dataCopy[index].ValueF[:coinC]

	// dbtypes.DurationBTW:  -> index 4 here and 4 in cache
	index = dbtypes.DurationBTW.Pos()
	durC := len(dataCopy[index].Height)
	if durC < diff {
		durC = 0
	} else {
		durC -= diff
	}
	dataCopy[index].Height = dataCopy[index].Height[:durC]
	dataCopy[index].ValueF = dataCopy[index].ValueF[:durC]

	//  dbtypes.HashRate:  -> index 5 here and 5 in cache
	index = dbtypes.HashRate.Pos()
	rateC := len(dataCopy[index].Time)
	if rateC < diff {
		rateC = 0
	} else {
		rateC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:rateC]
	dataCopy[index].NetHash = dataCopy[index].NetHash[:rateC]

	// dbtypes.POWDifficulty:  -> index 6 here and 6 in cache
	index = dbtypes.POWDifficulty.Pos()
	powC := len(dataCopy[index].Time)
	if powC < diff {
		powC = 0
	} else {
		powC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:powC]
	dataCopy[index].Difficulty = dataCopy[index].Difficulty[:powC]

	// dbtypes.TicketByWindows:  -> index 7 here and 7 in cache
	index = dbtypes.TicketByWindows.Pos()
	winC := len(dataCopy[index].Height)
	if winC < diff {
		winC = 0
	} else {
		winC -= diff
	}
	dataCopy[index].Height = dataCopy[index].Height[:winC]
	dataCopy[index].Solo = dataCopy[index].Solo[:winC]
	dataCopy[index].Pooled = dataCopy[index].Pooled[:winC]

	// dbtypes.TicketPrice:  -> index 8 here and 8 in cache
	index = dbtypes.TicketPrice.Pos()
	priceC := len(dataCopy[index].Time)
	if priceC < diff {
		priceC = 0
	} else {
		priceC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:priceC]
	dataCopy[index].ValueF = dataCopy[index].ValueF[:priceC]

	// dbtypes.TicketsByBlocks:  -> index 9 here and 9 in cache
	index = dbtypes.TicketsByBlocks.Pos()
	blockC := len(dataCopy[index].Height)
	if blockC < diff {
		blockC = 0
	} else {
		blockC -= diff
	}
	dataCopy[index].Height = dataCopy[index].Height[:blockC]
	dataCopy[index].Solo = dataCopy[index].Solo[:blockC]
	dataCopy[index].Pooled = dataCopy[index].Pooled[:blockC]

	// dbtypes.TicketSpendT:  -> index 10 here and 10 in cache
	index = dbtypes.TicketSpendT.Pos()
	spendC := len(dataCopy[index].Height)
	if spendC < diff {
		spendC = 0
	} else {
		spendC -= diff
	}
	dataCopy[index].Height = dataCopy[index].Height[:spendC]
	dataCopy[index].Unspent = dataCopy[index].Unspent[:spendC]
	dataCopy[index].Revoked = dataCopy[index].Revoked[:spendC]

	// dbtypes.TxPerBlock:  -> index 11 here and 11 in cache
	index = dbtypes.TxPerBlock.Pos()
	txC := len(dataCopy[index].Height)
	if txC < diff {
		txC = 0
	} else {
		txC -= diff
	}
	dataCopy[index].Height = dataCopy[index].Height[:txC]
	dataCopy[index].Count = dataCopy[index].Count[:txC]

	// dbtypes.TxPerDay:  -> index 12 here and 12 in cache
	index = dbtypes.TxPerDay.Pos()
	dayC := len(dataCopy[index].Time)
	if dayC < diff {
		dayC = 0
	} else {
		dayC -= diff
	}
	dataCopy[index].Time = dataCopy[index].Time[:dayC]
	dataCopy[index].Count = dataCopy[index].Count[:dayC]

	t.Run("Match_incremental_change_with_oldData_for_", func(t *testing.T) {
		err := db.PgChartsData(dataCopy)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
			return
		}

		for k := range dataCopy {
			chartName := dbtypes.ChartType(k).String()
			t.Run(chartName, func(t *testing.T) {
				if !reflect.DeepEqual(dataCopy[k], oldData[k]) {
					t.Fatalf("expected no new data to be added for chart (%s) to dataCopy but it was.",
						chartName)
					return
				}
			})
		}
	})
}
