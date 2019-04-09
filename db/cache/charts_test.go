package cache

import (
	"context"
	// "encoding/json"
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
		err := charts.ReadCacheFile(filepath.Join(tempDir, "log1.gob"))
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

		err = charts.ReadCacheFile(path)
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

		err = charts.WriteCacheFile(path)
		if err == nil {
			t.Fatal("expected an error but found non")
		}
	})

	t.Run("Write_to_an_non_existent_file", func(t *testing.T) {
		err := charts.WriteCacheFile(gobPath)
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
		charts.Blocks.snip(0)
		compUints := seedUints()
		compFloats := seedFloats()
		compTimes := seedTimes()

		comp("Height", charts.Blocks.Height, compUints, false)
		comp("Time", charts.Blocks.Time, compTimes, false)
		comp("PoolSize", charts.Blocks.PoolSize, compUints, false)
		comp("PoolValue", charts.Blocks.PoolValue, compFloats, false)
		comp("BlockSize", charts.Blocks.BlockSize, compUints, false)
		comp("TxCount", charts.Blocks.TxCount, compUints, false)
		comp("NewAtoms", charts.Blocks.NewAtoms, compUints, false)
		comp("Chainwork", charts.Blocks.Chainwork, compUints, false)
		comp("Fees", charts.Blocks.Fees, compUints, false)

		err := charts.ReadCacheFile(gobPath)
		if err != nil {
			t.Fatalf("expected no error but found: %v", err)
		}

		comp("Height", charts.Blocks.Height, compUints, true)
		comp("Time", charts.Blocks.Time, compTimes, true)
		comp("PoolSize", charts.Blocks.PoolSize, compUints, true)
		comp("PoolValue", charts.Blocks.PoolValue, compFloats, true)
		comp("BlockSize", charts.Blocks.BlockSize, compUints, true)
		comp("TxCount", charts.Blocks.TxCount, compUints, true)
		comp("NewAtoms", charts.Blocks.NewAtoms, compUints, true)
		comp("Chainwork", charts.Blocks.Chainwork, compUints, true)
		comp("Fees", charts.Blocks.Fees, compUints, true)
	})

	t.Run("Lengthen", func(t *testing.T) {
		err := charts.Lengthen()
		if err != nil {
			t.Fatalf("Lengthen error: %v", err)
		}

		// Time is expected to be the midnight associated with the beginning of the
		// day.
		comp("Time", charts.Days.Time, ChartUints{0, aDay, 2 * aDay}, true)
		comp("PoolSize", charts.Days.PoolSize, uintDaysAvg, true)
		comp("PoolValue", charts.Days.PoolValue, floatDaysAvg, true)
		comp("BlockSize", charts.Days.BlockSize, uintDaysSum, true)
		comp("TxCount", charts.Days.TxCount, uintDaysSum, true)
		comp("NewAtoms", charts.Days.NewAtoms, uintDaysSum, true)
		// Chainwork will just be the last entry from each day
		comp("Chainwork", charts.Days.Chainwork, ChartUints{2, 4, 6}, true)
		comp("Fees", charts.Days.Fees, uintDaysSum, true)
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
		go func() {
			select {
			case <-time.NewTimer(1 * time.Second).C:
				t.Fatalf("timed out waiting for waitgroup")
				wg.Done()
			case <-ctx.Done():
			}
		}()
		wg.Wait()
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

	// s, _ := json.MarshalIndent(charts.Days, "", "    ")
	// fmt.Printf("%d blocks\n", len(charts.Blocks.Time))
	// fmt.Println(string(s))
}
