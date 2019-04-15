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
	"github.com/decred/dcrdata/db/dbtypes"
	tc "github.com/decred/dcrdata/v4/testutil/dbload/testsconfig"
	pitypes "github.com/dmigwi/go-piparser/proposals/types"
)

var (
	db           *ChainDB
	addrCacheCap int = 1e4
)

type parserInstance struct{}

func (p *parserInstance) UpdateSignal() <-chan struct{} {
	return make(chan struct{})
}

func (p *parserInstance) ProposalsHistory() ([]*pitypes.History, error) {
	return []*pitypes.History{}, nil
}

func (p *parserInstance) ProposalsHistorySince(since time.Time) ([]*pitypes.History, error) {
	return []*pitypes.History{}, nil
}

func openDB() (func() error, error) {
	dbi := DBInfo{
		Host:   tc.PGChartsTestsHost,
		Port:   tc.PGChartsTestsPort,
		User:   tc.PGChartsTestsUser,
		Pass:   tc.PGChartsTestsPass,
		DBName: tc.PGChartsTestsDBName,
	}
	var err error
	db, err = NewChainDB(&dbi, &chaincfg.MainNetParams, nil, true, true, addrCacheCap,
		nil, new(parserInstance))
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
	var oldData = make(map[string]*dbtypes.ChartsData)
	err := db.PgChartsData(oldData)
	if err != nil {
		t.Fatalf("expected no error but found: %v", err)
		return
	}

	// Validate the oldData contents by checking for duplicates and dataset order.

	// xvalArray maps the chart type to its respective x-axis dataset name.
	// Most charts either use timestamp or block height as the x-axis value.
	xvalArray := map[string]string{
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
	t.Run("check_duplicates_&_ordering_for_", func(t *testing.T) {
		for chartT, xValName := range xvalArray {
			data := oldData[chartT]

			t.Run(chartT, func(t *testing.T) {
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

	dataCopy := make(map[string]*dbtypes.ChartsData, len(oldData))
	for k, v := range oldData {
		dataCopy[k] = v
	}

	// In this test, no new data is expected to be added by the queries. The correct
	// data returned after passing dataCopy should match oldData. i.e. the oldData
	// arrays length and content should match the returned result.

	t.Run("Check_if_invalid_data_was_added_for_", func(t *testing.T) {
		err = db.PgChartsData(dataCopy)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
			return
		}

		for k := range dataCopy {
			t.Run(k, func(t *testing.T) {
				if !reflect.DeepEqual(dataCopy[k], oldData[k]) {
					t.Fatalf("expected no new data to be added for chart (%s) to dataCopy but it was.", k)
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

	name := dbtypes.AvgBlockSize
	avgC := len(dataCopy[name].Time)
	if avgC < diff {
		avgC = 0
	} else {
		avgC -= diff
	}
	dataCopy[name].Time = dataCopy[name].Time[:avgC]
	dataCopy[name].Size = dataCopy[name].Size[:avgC]

	name = dbtypes.BlockChainSize
	bSizeC := len(dataCopy[name].Time)
	if bSizeC < diff {
		bSizeC = 0
	} else {
		bSizeC -= diff
	}
	dataCopy[name].Time = dataCopy[name].Time[:bSizeC]
	dataCopy[name].ChainSize = dataCopy[name].ChainSize[:bSizeC]

	name = dbtypes.ChainWork
	chainC := len(dataCopy[name].Time)
	if chainC < diff {
		chainC = 0
	} else {
		chainC -= diff
	}
	dataCopy[name].Time = dataCopy[name].Time[:chainC]
	dataCopy[name].ChainWork = dataCopy[name].ChainWork[:chainC]

	name = dbtypes.CoinSupply
	coinC := len(dataCopy[name].Time)
	if coinC < diff {
		coinC = 0
	} else {
		coinC -= diff
	}
	dataCopy[name].Time = dataCopy[name].Time[:coinC]
	dataCopy[name].ValueF = dataCopy[name].ValueF[:coinC]

	name = dbtypes.DurationBTW
	durC := len(dataCopy[name].Height)
	if durC < diff {
		durC = 0
	} else {
		durC -= diff
	}
	dataCopy[name].Height = dataCopy[name].Height[:durC]
	dataCopy[name].ValueF = dataCopy[name].ValueF[:durC]

	name = dbtypes.HashRate
	rateC := len(dataCopy[name].Time)
	if rateC < diff {
		rateC = 0
	} else {
		rateC -= diff
	}
	dataCopy[name].Time = dataCopy[name].Time[:rateC]
	dataCopy[name].NetHash = dataCopy[name].NetHash[:rateC]

	name = dbtypes.POWDifficulty
	powC := len(dataCopy[name].Time)
	if powC < diff {
		powC = 0
	} else {
		powC -= diff
	}
	dataCopy[name].Time = dataCopy[name].Time[:powC]
	dataCopy[name].Difficulty = dataCopy[name].Difficulty[:powC]

	name = dbtypes.TicketByWindows
	winC := len(dataCopy[name].Height)
	if winC < diff {
		winC = 0
	} else {
		winC -= diff
	}
	dataCopy[name].Height = dataCopy[name].Height[:winC]
	dataCopy[name].Solo = dataCopy[name].Solo[:winC]
	dataCopy[name].Pooled = dataCopy[name].Pooled[:winC]

	name = dbtypes.TicketPrice
	priceC := len(dataCopy[name].Time)
	if priceC < diff {
		priceC = 0
	} else {
		priceC -= diff
	}
	dataCopy[name].Time = dataCopy[name].Time[:priceC]
	dataCopy[name].ValueF = dataCopy[name].ValueF[:priceC]

	name = dbtypes.TicketsByBlocks
	blockC := len(dataCopy[name].Height)
	if blockC < diff {
		blockC = 0
	} else {
		blockC -= diff
	}
	dataCopy[name].Height = dataCopy[name].Height[:blockC]
	dataCopy[name].Solo = dataCopy[name].Solo[:blockC]
	dataCopy[name].Pooled = dataCopy[name].Pooled[:blockC]

	name = dbtypes.TicketSpendT
	spendC := len(dataCopy[name].Height)
	if spendC < diff {
		spendC = 0
	} else {
		spendC -= diff
	}
	dataCopy[name].Height = dataCopy[name].Height[:spendC]
	dataCopy[name].Unspent = dataCopy[name].Unspent[:spendC]
	dataCopy[name].Revoked = dataCopy[name].Revoked[:spendC]

	name = dbtypes.TxPerBlock
	txC := len(dataCopy[name].Height)
	if txC < diff {
		txC = 0
	} else {
		txC -= diff
	}
	dataCopy[name].Height = dataCopy[name].Height[:txC]
	dataCopy[name].Count = dataCopy[name].Count[:txC]

	name = dbtypes.TxPerDay
	dayC := len(dataCopy[name].Time)
	if dayC < diff {
		dayC = 0
	} else {
		dayC -= diff
	}
	dataCopy[name].Time = dataCopy[name].Time[:dayC]
	dataCopy[name].Count = dataCopy[name].Count[:dayC]

	t.Run("Match_incremental_change_with_oldData_for_", func(t *testing.T) {
		err = db.PgChartsData(dataCopy)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
			return
		}

		for k := range dataCopy {
			t.Run(k, func(t *testing.T) {
				if !reflect.DeepEqual(dataCopy[k], oldData[k]) {
					t.Fatalf("expected no new data to be added for chart (%s) to dataCopy but it was.", k)
					return
				}
			})
		}
	})
}
