//go:build pgonline || chartdata

package dcrpg

import (
	"context"
	"database/sql"
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrdata/v8/db/cache"
)

func registerDummyFeeAndPoolInfo(charts *cache.ChartData) {
	dummyFetcher := func(charts *cache.ChartData) (*sql.Rows, func(), error) {
		return nil, func() {}, nil
	}

	dummyAppender := func(charts *cache.ChartData, _ *sql.Rows) error {
		blocks := charts.Blocks
		neededLength := len(blocks.Time)
		blocks.PoolSize = make([]uint64, neededLength)
		blocks.PoolValue = make([]uint64, neededLength)
		blocks.Fees = make([]uint64, neededLength)
		return nil
	}

	charts.AddUpdater(cache.ChartUpdater{
		Tag:      "fee and pool info",
		Fetcher:  dummyFetcher,
		Appender: dummyAppender,
	})
}

// TestPgCharts compares the data returned when a fresh data query is made
// with when an incremental change was added after new blocks were synced.
// No difference between the two should exist otherwise this test should fail.
// It also checks the order and duplicates in the x-axis dataset.
func TestPgCharts(t *testing.T) {
	charts := cache.NewChartData(context.Background(), 0, chaincfg.MainNetParams())
	// Spoof the interval to enable more points with a smaller test db.
	charts.DiffInterval = 9

	db.RegisterCharts(charts)
	// Register a dummy updater for Fees, PoolSize, and PoolValue. This must be
	// registered *after* ChainDB registers its updaters, which set the correct
	// length of blocks.Time and the other slices.
	registerDummyFeeAndPoolInfo(charts)

	// Validator for the Blocks and Windows chart data slice lengths.
	blocks := charts.Blocks
	windows := charts.Windows

	validate := func(tag string) {
		_, err := cache.ValidateLengths(blocks.Time, blocks.PoolSize,
			blocks.PoolValue, blocks.BlockSize, blocks.TxCount, blocks.NewAtoms,
			blocks.Chainwork, blocks.Fees)
		if err != nil {
			t.Fatalf("%s blocks length validation error: %v", tag, err)
		}
		_, err = cache.ValidateLengths(windows.TicketPrice, windows.PowDiff,
			windows.Time, windows.StakeCount, windows.MissedVotes)
		if err != nil {
			t.Fatalf("%s windows length validation error: %v", tag, err)
		}
	}

	t.Log("Validating initial state.")
	validate("pre-update")

	// Perform the DB queries and append charts data.
	t.Log("Performing initial DB query and dataset updates.")
	err := charts.Update()
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the Blocks and Windows data sets have sensible lengths.
	blocksLen := len(blocks.Time)
	windowsLen := len(windows.Time)

	if blocksLen == 0 {
		t.Fatalf("unexpected empty blocks data")
	}

	if blocksLen > int(charts.DiffInterval) && windowsLen == 0 {
		t.Fatalf("unexpected empty windows data")
	}

	// Trim data points from the Blocks and Windows datasets.
	blocks.Snip(4 * int(charts.DiffInterval))
	windows.Snip(4)
	t.Log("Validating post-snip.")
	validate("post-snip")

	// Perform the DB queries and append the missing charts data.
	t.Log("Performing update DB query and dataset updates.")
	err = charts.Update()
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the final length is the same as the original length.
	if blocksLen != len(blocks.Time) {
		t.Fatalf("unexpected blocks data length %d != %d", blocksLen, len(blocks.Time))
	}
	if windowsLen != len(windows.Time) {
		t.Fatalf("unexpected windows data length %d != %d", windowsLen, len(windows.Time))
	}
}
