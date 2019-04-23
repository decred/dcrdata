// +build chartests

package dcrpg

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/db/cache"
	tc "github.com/decred/dcrdata/testutil/dbconfig"
	pitypes "github.com/dmigwi/go-piparser/proposals/types"
	"github.com/decred/dcrdata/rpcutils"
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
		nil, new(parserInstance), rpcutils.BlockPrefetchClient{})
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

// TestPgCharts compares the data returned when a fresh data query is made
// with when an incremental change was added after new blocks were synced.
// No difference between the two should exist otherwise this test should fail.
// It also checks the order and duplicates in the x-axis dataset.
func TestPgCharts(t *testing.T) {
	charts := cache.NewChartData(0, time.Now(), &chaincfg.MainNetParams, context.Background())
	db.RegisterCharts(charts)
	charts.Update()
	blocks := charts.Blocks

	validate := func(tag string) {
		// Not checking NewAtoms right now, as the test database does not appear to
		// contain stakebase and coinbase vins.
		_, err := cache.ValidateLengths(blocks.Time, blocks.Chainwork, blocks.TxCount, blocks.BlockSize)
		if err != nil {
			t.Fatalf("%s blocks length validation error: %v", tag, err)
		}
	}

	if len(blocks.Time) == 0 {
		t.Fatalf("no data deposited in Time array")
	}
	// The database will not validate because it does not start at block height 0.
	// This means that Update will not progress past the Blocks update right now.
	// To do: implement windows data testing
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
	charts.Update()
	validate("second update")

	if blocksLen != len(blocks.Time) {
		t.Fatalf("unexpected blocks data length %d != %d", blocksLen, len(blocks.Time))
	}
	if windowsLen != len(windows.Time) {
		t.Fatalf("unexpected windows data length %d != %d", windowsLen, len(windows.Time))
	}
}
