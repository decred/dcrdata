// +build mainnettest

package dcrpg

import (
	"context"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/db/cache/v2"
)

// TestPgCharts compares the data returned when a fresh data query is made
// with when an incremental change was added after new blocks were synced.
// No difference between the two should exist otherwise this test should fail.
// It also checks the order and duplicates in the x-axis dataset.
func TestPgCharts(t *testing.T) {
	charts := cache.NewChartData(context.Background(), 0, &chaincfg.MainNetParams)
	charts.DiffInterval = 10
	db.RegisterCharts(charts)
	blocks := charts.Blocks
	windows := charts.Windows

	validate := func(tag string) {
		// Not checking NewAtoms right now, as the test database does not appear to
		// contain stakebase and coinbase vins.
		_, err := cache.ValidateLengths(blocks.Time, blocks.Chainwork, blocks.TxCount, blocks.BlockSize)
		if err != nil {
			t.Fatalf("%s blocks length validation error: %v", tag, err)
		}
		_, err = cache.ValidateLengths(windows.TicketPrice, windows.PowDiff, windows.Time)
		if err != nil {
			t.Fatalf("%s windows length validation error: %v", tag, err)
		}
	}

	charts.Update()
	validate("initial update")
	blocksLen := len(blocks.Time)
	windowsLen := len(windows.Time)

	if blocksLen == 0 {
		t.Log(blocks)
		t.Fatalf("unexpected empty blocks data")
	}

	if blocksLen > int(charts.DiffInterval) && windowsLen == 0 {
		t.Fatalf("unexpected empty windows data")
	}

	blocks.Snip(50)
	windows.Snip(5)
	validate("post-snip")

	charts.Update()
	validate("second update")

	if blocksLen != len(blocks.Time) {
		t.Fatalf("unexpected blocks data length %d != %d", blocksLen, len(blocks.Time))
	}
	if windowsLen != len(windows.Time) {
		t.Fatalf("unexpected windows data length %d != %d", windowsLen, len(windows.Time))
	}
}
