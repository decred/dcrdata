package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrdata/v8/txhelpers"
)

var tempDir string

// TestMain setups the tempDir and cleans it up after tests.
func TestMain(m *testing.M) {
	var err error
	tempDir, err = os.MkdirTemp("", "cache")
	if err != nil {
		fmt.Printf("os.MkdirTemp: %v", err)
		return
	}

	code := m.Run()

	// clean up
	os.RemoveAll(tempDir)

	os.Exit(code)
}

// TestChartsCache tests the reading and writing of the charts cache.
func TestChartsCache(t *testing.T) {
	gobPath := filepath.Join(tempDir, "log.gob")
	ctx, shutdown := context.WithCancel(context.Background())
	var charts *ChartData

	comp := func(k string, a interface{}, b interface{}, expectation bool) {
		v := reflect.DeepEqual(a, b)
		if v != expectation {
			t.Fatalf("DeepEqual: expected %t, found %t for %s", expectation, v, k)
		}
	}

	appendPt := func(t uint64, v uint64) {
		charts.Blocks.Height = append(charts.Blocks.Height, v)
		charts.Blocks.Time = append(charts.Blocks.Time, t)
		charts.Blocks.PoolSize = append(charts.Blocks.PoolSize, v)
		charts.Blocks.PoolValue = append(charts.Blocks.PoolValue, v)
		charts.Blocks.BlockSize = append(charts.Blocks.BlockSize, v)
		charts.Blocks.TxCount = append(charts.Blocks.TxCount, v)
		charts.Blocks.NewAtoms = append(charts.Blocks.NewAtoms, v)
		charts.Blocks.Chainwork = append(charts.Blocks.Chainwork, v)
		charts.Blocks.Fees = append(charts.Blocks.Fees, v)
		charts.Blocks.TotalMixed = append(charts.Blocks.TotalMixed, v)
		charts.Blocks.AnonymitySet = append(charts.Blocks.AnonymitySet, v)
		charts.Windows.Time = ChartUints{0}
		charts.Windows.PowDiff = ChartFloats{0}
		charts.Windows.TicketPrice = ChartUints{0}
		charts.Windows.StakeCount = ChartUints{0}
		charts.Windows.MissedVotes = ChartUints{0}
	}

	seedUints := ChartUints{1, 2, 3, 4, 5, 6}
	seedTimes := ChartUints{1, 2 + aDay, 3 + aDay, 4 + 2*aDay, 5 + 2*aDay, 6 + 3*aDay}
	uintDaysAvg := ChartUints{1, 2, 4}
	uintDaysSum := ChartUints{1, 5, 9}

	resetCharts := func() {
		charts = NewChartData(ctx, 0, chaincfg.MainNetParams())
		for i, t := range seedTimes {
			appendPt(t, seedUints[i])
		}
	}
	resetCharts()

	t.Run("Read_a_non-existent_gob_dump", func(t *testing.T) {
		err := charts.readCacheFile(filepath.Join(tempDir, "log1.gob"))
		if err == nil {
			t.Fatal("expected an error but found none")
		}
	})

	t.Run("Read_a_non-gob_file_encoding_dump", func(t *testing.T) {
		path := filepath.Join(tempDir, "log2.txt")

		err := os.WriteFile(path, []byte(`Who let the dogs bark?`), 0644)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		err = charts.readCacheFile(path)
		if err == nil {
			t.Fatal("expected an error but found non")
		}
	})

	t.Run("Write_to_existing_non-GOB_file", func(t *testing.T) {
		path := filepath.Join(tempDir, "log3.txt")

		err := os.WriteFile(path, []byte(`Who let the dogs bark?`), 0644)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		err = charts.writeCacheFile(path)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}
	})

	t.Run("Write_to_an_non_existent_file", func(t *testing.T) {
		err := charts.writeCacheFile(gobPath)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		// check if the new dump file path exists
		if !isFileExists(gobPath) {
			t.Fatalf("expected to find the newly created file but its missing")
		}
	})

	t.Run("Read_from_an_existing_gob_encoded_file", func(t *testing.T) {
		// Empty the charts data
		charts.Blocks.Snip(0)

		comp("Height before read", charts.Blocks.Height, seedUints, false)
		comp("Time before read", charts.Blocks.Time, seedTimes, false)
		comp("PoolSize before read", charts.Blocks.PoolSize, seedUints, false)
		comp("PoolValue before read", charts.Blocks.PoolValue, seedUints, false)
		comp("BlockSize before read", charts.Blocks.BlockSize, seedUints, false)
		comp("TxCount before read", charts.Blocks.TxCount, seedUints, false)
		comp("NewAtoms before read", charts.Blocks.NewAtoms, seedUints, false)
		comp("Chainwork before read", charts.Blocks.Chainwork, seedUints, false)
		comp("Fees before read", charts.Blocks.Fees, seedUints, false)
		comp("TotalMixed before read", charts.Blocks.TotalMixed, seedUints, false)
		comp("AnonymitySet before read", charts.Blocks.AnonymitySet, seedUints, false)

		err := charts.readCacheFile(gobPath)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		comp("Height after read", charts.Blocks.Height, seedUints, true)
		comp("Time after read", charts.Blocks.Time, seedTimes, true)
		comp("PoolSize after read", charts.Blocks.PoolSize, seedUints, true)
		comp("PoolValue after read", charts.Blocks.PoolValue, seedUints, true)
		comp("BlockSize after read", charts.Blocks.BlockSize, seedUints, true)
		comp("TxCount after read", charts.Blocks.TxCount, seedUints, true)
		comp("NewAtoms after read", charts.Blocks.NewAtoms, seedUints, true)
		comp("Chainwork after read", charts.Blocks.Chainwork, seedUints, true)
		comp("Fees after read", charts.Blocks.Fees, seedUints, true)
		comp("TotalMissed after read", charts.Blocks.TotalMixed, seedUints, true)
		comp("AnonymitySet after read", charts.Blocks.AnonymitySet, seedUints, true)

		// Lengthen is called during readCacheFile, so Days should be properly calculated
		comp("Time after Lengthen", charts.Days.Time, ChartUints{0, aDay, 2 * aDay}, true)
		comp("PoolSize after Lengthen", charts.Days.PoolSize, uintDaysAvg, true)
		comp("PoolValue after Lengthen", charts.Days.PoolValue, uintDaysAvg, true)
		comp("BlockSize after Lengthen", charts.Days.BlockSize, uintDaysSum, true)
		comp("TxCount after Lengthen", charts.Days.TxCount, uintDaysSum, true)
		comp("NewAtoms after Lengthen", charts.Days.NewAtoms, uintDaysSum, true)
		// Chainwork will just be the last entry from each day
		comp("Chainwork after Lengthen", charts.Days.Chainwork, ChartUints{2, 4, 6}, true)
		comp("Fees after Lengthen", charts.Days.Fees, uintDaysSum, true)
		comp("TotalMixed after Lengthen", charts.Days.TotalMixed, uintDaysSum, true)
		comp("AnonymitySet after Lengthen", charts.Days.AnonymitySet, uintDaysAvg, true)

		// An additional call to lengthen should not add any data.
		timeLen := len(charts.Days.Time)
		charts.Lengthen()
		if len(charts.Days.Time) != timeLen {
			t.Fatalf("Second call to Lengthen resulted in unexpected new data.")
		}
	})

	t.Run("get_chart", func(t *testing.T) {
		chart, err := charts.Chart(BlockSize, string(BlockBin), string(TimeAxis))
		if err != nil {
			t.Fatalf("error getting fresh chart: %v", err)
		}
		if string(chart) != `{"axis":"time","bin":"block","size":[1,2,3,4,5,6],"t":[1,86402,86403,172804,172805,259206]}` {
			t.Fatalf("unexpected chart json")
		}
		ck := cacheKey(BlockSize, BlockBin, TimeAxis)
		if !reflect.DeepEqual(charts.cache[ck].data, chart) {
			t.Fatalf("could not match chart to cache")
		}
		// Grab chart once more. This should test the cache path.
		chart2, err := charts.Chart(BlockSize, string(BlockBin), string(TimeAxis))
		if err != nil {
			t.Fatalf("error getting chart from cache: %v", err)
		}
		if !reflect.DeepEqual(chart2, chart) {
			t.Fatalf("cached chart does not match original")
		}
	})

	t.Run("Reorg", func(t *testing.T) {
		// This should cause the blocks to truncate to length 2, the days to
		// drop to length 1, and the windows to drop to length 0.
		h, err := chainhash.NewHash([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
		if err != nil {
			t.Fatalf("chainhash.Hash error: %v", err)
		}
		wg := new(sync.WaitGroup)
		wg.Add(1)
		go func() {
			charts.ReorgHandler(&txhelpers.ReorgData{
				NewChain:       []chainhash.Hash{*h},
				NewChainHeight: 2,
			})
			wg.Done()
		}()
		var timedOut bool
		go func() {
			select {
			case <-time.NewTimer(1 * time.Second).C:
				timedOut = true
				wg.Done()
			case <-ctx.Done():
			}
		}()
		wg.Wait()
		if timedOut {
			t.Fatalf("timed out waiting for waitgroup")
		}
		shutdown()
		if len(charts.Blocks.Time) != 2 {
			t.Fatalf("blocks length %d after reorg test. expected 2", len(charts.Blocks.Time))
		}
		if len(charts.Days.Time) != 1 {
			t.Fatalf("days length %d after reorg test. expected 1", len(charts.Days.Time))
		}
		if len(charts.Windows.Time) != 0 {
			t.Fatalf("windows length %d after reorg test. expected 0", len(charts.Windows.Time))
		}
	})

	t.Run("rollover", func(t *testing.T) {
		resetCharts()
		// Add a little less than a third of a day, 12 times.
		increment := uint64(aDay/3 - 500)
		for i := 0; i < 12; i++ {
			appendPt(
				charts.Blocks.Time[len(charts.Blocks.Time)-1]+increment,
				charts.Blocks.Height[len(charts.Blocks.Height)-1]+1,
			)
			charts.Lengthen()
		}
		expected := ChartUints{0, 86400, 172800, 259200, 345600}
		comp("after day rollovers", charts.Days.Time, expected, true)
	})
}

func TestChartReorg(t *testing.T) {
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	newFloats := func() ChartFloats { return ChartFloats{1, 2, 3} }
	newUints := func() ChartUints { return ChartUints{1, 2, 3} }
	charts := &ChartData{
		ctx: ctx,
	}
	resetCharts := func() {
		charts.Windows = &windowSet{
			cacheID:     0,
			Time:        newUints(),
			PowDiff:     newFloats(),
			TicketPrice: newUints(),
			StakeCount:  newUints(),
			MissedVotes: newUints(),
		}
		charts.Days = &zoomSet{
			cacheID:   0,
			Height:    newUints(),
			Time:      newUints(),
			PoolSize:  newUints(),
			PoolValue: newUints(),
			BlockSize: newUints(),
			TxCount:   newUints(),
			NewAtoms:  newUints(),
			Chainwork: newUints(),
			Fees:      newUints(),
		}
		charts.Blocks = &zoomSet{
			cacheID:    0,
			Time:       newUints(),
			PoolSize:   newUints(),
			PoolValue:  newUints(),
			BlockSize:  newUints(),
			TxCount:    newUints(),
			NewAtoms:   newUints(),
			Chainwork:  newUints(),
			Fees:       newUints(),
			TotalMixed: newUints(),
		}
	}
	// this test reorg will replace the entire chain.

	reorgData := func(newHeight, chainLen int) *txhelpers.ReorgData {
		d := &txhelpers.ReorgData{
			NewChainHeight: int32(newHeight),
			NewChain:       make([]chainhash.Hash, chainLen),
		}
		return d
	}
	testReorg := func(newHeight, chainLen, newBlockLen, newDayLen, newWindowLen int) {
		done := make(chan struct{})
		go func() {
			charts.ReorgHandler(reorgData(newHeight, chainLen))
			close(done)
		}()
		select {
		case <-time.NewTimer(time.Second).C:
			t.Fatalf("timed out waiting for reorg test to complete")
		case <-done:
		}
		if charts.Blocks.Time.Length() != newBlockLen {
			t.Errorf("unexpected blocks length %d", charts.Blocks.Time.Length())
		}
		// Reorg snips 2 days
		if charts.Days.Time.Length() != newDayLen {
			t.Errorf("unexpected days length %d", charts.Days.Time.Length())
		}
		// Reorg snips last window
		if charts.Windows.Time.Length() != newWindowLen {
			t.Errorf("unexpected windows length %d", charts.Windows.Time.Length())
		}
	}
	// Test replacing the entire chain.
	resetCharts()
	testReorg(2, 3, 0, 1, 2)

	// All but one block.
	resetCharts()
	testReorg(2, 2, 1, 1, 2)
}
