package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/txhelpers"
)

var tempDir string

func printJson(thing interface{}) {
	s, _ := json.MarshalIndent(thing, "", "    ")
	fmt.Println(string(s))
}

// TestMain setups the tempDir and cleans it up after tests.
func TestMain(m *testing.M) {
	var err error
	tempDir, err = ioutil.TempDir(os.TempDir(), "cache")
	if err != nil {
		fmt.Printf("ioutil.TempDir: %v", err)
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
	charts := NewChartData(0, time.Unix(0, 0), &chaincfg.MainNetParams, ctx)

	comp := func(k string, a interface{}, b interface{}, expectation bool) {
		v := reflect.DeepEqual(a, b)
		if v != expectation {
			t.Fatalf("DeepEqual: expected %t, found %t for %s", expectation, v, k)
		}
	}

	seedUints := func() ChartUints {
		return ChartUints{1, 2, 3, 4, 5, 6}
	}
	seedFloats := func() ChartFloats {
		return ChartFloats{1.1, 2.2, 3.3, 4.4, 5.5, 6.6}
	}
	seedTimes := func() ChartUints {
		return ChartUints{1, 2 + aDay, 3 + aDay, 4 + 2*aDay, 5 + 2*aDay, 6 + 3*aDay}
	}
	floatDaysAvg := ChartFloats{1.1, 2.75, 4.95}
	uintDaysAvg := ChartUints{1, 2, 4}
	uintDaysSum := ChartUints{1, 5, 9}

	charts.Blocks.Height = seedUints()
	charts.Blocks.Time = seedTimes()
	charts.Blocks.PoolSize = seedUints()
	charts.Blocks.PoolValue = seedFloats()
	charts.Blocks.BlockSize = seedUints()
	charts.Blocks.TxCount = seedUints()
	charts.Blocks.NewAtoms = seedUints()
	charts.Blocks.Chainwork = seedUints()
	charts.Blocks.Fees = seedUints()
	charts.Windows.Time = ChartUints{0}
	charts.Windows.PowDiff = ChartFloats{0}
	charts.Windows.TicketPrice = ChartUints{0}

	t.Run("Read_a_non-existent_gob_dump", func(t *testing.T) {
		err := charts.readCacheFile(filepath.Join(tempDir, "log1.gob"))
		if err == nil {
			t.Fatal("expected an error but found none")
		}
	})

	t.Run("Read_a_non-gob_file_encoding_dump", func(t *testing.T) {
		path := filepath.Join(tempDir, "log2.txt")

		err := ioutil.WriteFile(path, []byte(`Who let the dogs bark?`), 0644)
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

		err := ioutil.WriteFile(path, []byte(`Who let the dogs bark?`), 0644)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		err = charts.writeCacheFile(path)
		if err == nil {
			t.Fatal("expected an error but found non")
		}
	})

	t.Run("Write_to_an_non_existent_file", func(t *testing.T) {
		err := charts.writeCacheFile(gobPath)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		// check if the new dump file path exists
		if !isfileExists(gobPath) {
			t.Fatalf("expected to find the newly created file but its missing")
		}
	})

	t.Run("Read_from_an_existing_gob_encoded_file", func(t *testing.T) {
		// Empty the charts data
		charts.Blocks.Snip(0)
		compUints := seedUints()
		compFloats := seedFloats()
		compTimes := seedTimes()

		comp("Height before read", charts.Blocks.Height, compUints, false)
		comp("Time before read", charts.Blocks.Time, compTimes, false)
		comp("PoolSize before read", charts.Blocks.PoolSize, compUints, false)
		comp("PoolValue before read", charts.Blocks.PoolValue, compFloats, false)
		comp("BlockSize before read", charts.Blocks.BlockSize, compUints, false)
		comp("TxCount before read", charts.Blocks.TxCount, compUints, false)
		comp("NewAtoms before read", charts.Blocks.NewAtoms, compUints, false)
		comp("Chainwork before read", charts.Blocks.Chainwork, compUints, false)
		comp("Fees before read", charts.Blocks.Fees, compUints, false)

		err := charts.readCacheFile(gobPath)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		comp("Height after read", charts.Blocks.Height, compUints, true)
		comp("Time after read", charts.Blocks.Time, compTimes, true)
		comp("PoolSize after read", charts.Blocks.PoolSize, compUints, true)
		comp("PoolValue after read", charts.Blocks.PoolValue, compFloats, true)
		comp("BlockSize after read", charts.Blocks.BlockSize, compUints, true)
		comp("TxCount after read", charts.Blocks.TxCount, compUints, true)
		comp("NewAtoms after read", charts.Blocks.NewAtoms, compUints, true)
		comp("Chainwork after read", charts.Blocks.Chainwork, compUints, true)
		comp("Fees after read", charts.Blocks.Fees, compUints, true)

		// Lengthen is called during readCacheFile, so Days should be properly calculated
		comp("Time after Lengthen", charts.Days.Time, ChartUints{0, aDay, 2 * aDay}, true)
		comp("PoolSize after Lengthen", charts.Days.PoolSize, uintDaysAvg, true)
		comp("PoolValue after Lengthen", charts.Days.PoolValue, floatDaysAvg, true)
		comp("BlockSize after Lengthen", charts.Days.BlockSize, uintDaysSum, true)
		comp("TxCount after Lengthen", charts.Days.TxCount, uintDaysSum, true)
		comp("NewAtoms after Lengthen", charts.Days.NewAtoms, uintDaysSum, true)
		// Chainwork will just be the last entry from each day
		comp("Chainwork after Lengthen", charts.Days.Chainwork, ChartUints{2, 4, 6}, true)
		comp("Fees after Lengthen", charts.Days.Fees, uintDaysSum, true)

		// An additional call to lengthen should not add any data.
		timeLen := len(charts.Days.Time)
		charts.Lengthen()
		if len(charts.Days.Time) != timeLen {
			t.Fatalf("Second call to Lengthen resulted in unexpected new data.")
		}
	})

	t.Run("get_chart", func(t *testing.T) {
		chart, err := charts.Chart(BlockSize, string(BlockZoom))
		if err != nil {
			t.Fatalf("error getting fresh chart: %v", err)
		}
		if string(chart) != `{"x":[1,86402,86403,172804,172805,259206],"y":[1,2,3,4,5,6]}` {
			t.Fatalf("unexpected chart json")
		}
		ck := cacheKey(BlockSize, BlockZoom)
		if !reflect.DeepEqual(charts.cache[ck].data, chart) {
			t.Fatalf("could not match chart to cache")
		}
		// Grab chart once more. This should test the cache path.
		chart2, err := charts.Chart(BlockSize, string(BlockZoom))
		if err != nil {
			t.Fatalf("error getting chart from cache: %v", err)
		}
		if !reflect.DeepEqual(chart2, chart) {
			t.Fatalf("cached chart does not match original")
		}
	})

	t.Run("Reorg", func(t *testing.T) {
		c := make(chan *txhelpers.ReorgData, 2)
		dummyWg := new(sync.WaitGroup)
		dummyWg.Add(1)
		go charts.ReorgHandler(dummyWg, c)
		// This should cause the blocks to truncate to length 2, the days to
		// drop to length 1, and the windows to drop to length 0.
		h, err := chainhash.NewHash([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
		if err != nil {
			t.Fatalf("chainhash.Hash error: %v", err)
		}
		wg := new(sync.WaitGroup)
		wg.Add(1)
		c <- &txhelpers.ReorgData{
			NewChain:       []chainhash.Hash{*h},
			NewChainHeight: 2,
			WG:             wg,
		}
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
}

func TestChartReorg(t *testing.T) {
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	newFloats := func() ChartFloats { return ChartFloats{1, 2, 3} }
	newUints := func() ChartUints { return ChartUints{1, 2, 3} }
	charts := &ChartData{
		ctx: ctx,
	}
	charts.Windows = &windowSet{
		cacheID:     0,
		Time:        newUints(),
		PowDiff:     newFloats(),
		TicketPrice: newUints(),
	}
	charts.Days = &zoomSet{
		cacheID:   0,
		Height:    newUints(),
		Time:      newUints(),
		PoolSize:  newUints(),
		PoolValue: newFloats(),
		BlockSize: newUints(),
		TxCount:   newUints(),
		NewAtoms:  newUints(),
		Chainwork: newUints(),
		Fees:      newUints(),
	}
	charts.Blocks = &zoomSet{
		cacheID:   0,
		Time:      newUints(),
		PoolSize:  newUints(),
		PoolValue: newFloats(),
		BlockSize: newUints(),
		TxCount:   newUints(),
		NewAtoms:  newUints(),
		Chainwork: newUints(),
		Fees:      newUints(),
	}
	// this test reorg will replace the entire chain.
	reorgData := &txhelpers.ReorgData{
		NewChainHeight: 2,
		NewChain:       make([]chainhash.Hash, 3),
		WG:             new(sync.WaitGroup),
	}
	reorgData.WG.Add(1)
	wg := new(sync.WaitGroup)
	c := make(chan *txhelpers.ReorgData)
	wg.Add(1)
	go charts.ReorgHandler(wg, c)
	c <- reorgData
	reorgData.WG.Wait()
	if charts.Blocks.Time.Length() != 0 {
		t.Errorf("unexpected blocks length %d", charts.Blocks.Time.Length())
	}
	// Reorg snips 2 days
	if charts.Days.Time.Length() != 1 {
		t.Errorf("unexpected days length %d", charts.Days.Time.Length())
	}
	// Reorg snips last window
	if charts.Windows.Time.Length() != 2 {
		t.Errorf("unexpected windows length %d", charts.Windows.Time.Length())
	}
}
