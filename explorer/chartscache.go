package explorer

import (
	"context"
	"encoding/gob"
	"os"
	"sync"

	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/txhelpers"
)

// Cache data for charts that appear on /charts page is managed here.

// chartDataCache is a data cache for the historical charts that appear on
// /charts page on the UI.
type chartDataCache struct {
	sync.RWMutex
	updateHeight int64
	Data         map[string]*dbtypes.ChartsData
}

// cacheChartsData holds the prepopulated data that is used to draw the charts.
var cacheChartsData chartDataCache

// get provides a thread-safe way to access the all cache charts data.
func (c *chartDataCache) get() map[string]*dbtypes.ChartsData {
	c.RLock()
	defer c.RUnlock()
	return c.Data
}

// Height returns the last update height of the charts data cache.
func (c *chartDataCache) Height() int64 {
	c.RLock()
	defer c.RUnlock()
	return c.height()
}

// height returns the last update height of the charts data cache. Use Height
// instead for thread-safe access.
func (c *chartDataCache) height() int64 {
	if c.updateHeight == 0 {
		return -1
	}
	return c.updateHeight
}

// Update sets new data for the given height in the the charts data cache.
func (c *chartDataCache) Update(height int64, newData map[string]*dbtypes.ChartsData) {
	c.Lock()
	defer c.Unlock()
	c.update(height, newData)
}

// update sets new data for the given height in the the charts data cache. Use
// Update instead for thread-safe access.
func (c *chartDataCache) update(height int64, newData map[string]*dbtypes.ChartsData) {
	c.updateHeight = height
	c.Data = newData
}

// ChartTypeData is a thread-safe way to access chart data of the given type.
func ChartTypeData(chartType string) (data *dbtypes.ChartsData, ok bool) {
	cacheChartsData.RLock()
	defer cacheChartsData.RUnlock()

	// Data updates replace the entire map rather than modifying the data to
	// which the pointers refer, so the pointer can safely be returned here.
	data, ok = cacheChartsData.Data[chartType]
	return
}

// isfileExists checks if the provided file paths exists. It returns true if
// it does exist and false if otherwise.
func isfileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// WriteCacheFile creates the charts cache in the provided file path if it
// doesn't exists. It dumps the cacheChartsData contents using the .gob encoding.
func WriteCacheFile(filePath string) (err error) {
	var file *os.File
	if !isfileExists(filePath) {
		file, err = os.Create(filePath)
	} else {
		file, err = os.Open(filePath)
	}

	if err != nil {
		return err
	}

	defer file.Close()

	encoder := gob.NewEncoder(file)
	return encoder.Encode(cacheChartsData.get())
}

// ReadCacheFile reads the contents of the charts cache dump file encoded in
// .gob format if it exists returns an error if otherwise. It then deletes
// the read *.gob cache dump file.
func ReadCacheFile(filePath string, height int64) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}

	defer func() {
		file.Close()

		// delete the dump after reading.
		os.RemoveAll(filePath)
	}()

	var data = make(map[string]*dbtypes.ChartsData)

	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&data)
	if err != nil {
		return err
	}

	cacheChartsData.Update(height, data)

	return nil
}

// ChainMonitor defines data needed to handle cache data reorganization.
type ChainMonitor struct {
	ctx       context.Context
	wg        *sync.WaitGroup
	explorer  *explorerUI
	cacheChan chan *txhelpers.ReorgData
}

// NewCacheChainMonitor returns an initialized charts cache chain monitor instance.
func (exp *explorerUI) NewCacheChainMonitor(ctx context.Context, wg *sync.WaitGroup,
	reorgChan chan *txhelpers.ReorgData) *ChainMonitor {
	return &ChainMonitor{
		ctx:       ctx,
		wg:        wg,
		explorer:  exp,
		cacheChan: reorgChan,
	}
}

// ReorgHandler handles the charts cache data reorganization.
func (m *ChainMonitor) ReorgHandler() {
	defer m.wg.Done()
	for {
		select {
		case data, ok := <-m.cacheChan:
			if !ok {
				return
			}

			commonAncestorHeight := uint64(data.NewChainHeight) - uint64(len(data.NewChain))
			// Drop all entries since since the ancestor block after reorganization
			// of all PG and Sqlite DB is complete.
			if err := m.explorer.DropChartsDataToAncestor(commonAncestorHeight); err != nil {
				log.Errorf("cache reorg failed: %v", err)
				return
			}

			// Initiate cache update after drop the entries till the ancestor bloxk.
			m.explorer.prePopulateChartsData()

			data.WG.Done()

		case <-m.ctx.Done():
			return
		}
	}
}

// timeArrSourceSlicingIndex returns the index that is to be used to slice the
// rest of the cache charts dataset. The time array used is the x-axis dataset
// for the specific chart.
func timeArrSourceSlicingIndex(timeArr []dbtypes.TimeDef, ancestorTime int64) (index int) {
	if ancestorTime == 0 || len(timeArr) == 0 {
		return
	}

	index = len(timeArr) - 1
	for ; index >= 0; index-- {
		if timeArr[index].UNIX() <= ancestorTime {
			if index+1 <= len(timeArr) {
				// return index of entry before ancestor.
				index++
			}
			return
		}
	}

	return
}

// heightArrSourceSlicingIndex returns the index that is to be used to slice the
// rest of the cache charts dataset. The height array used is the x-axis dataset
// for the specific chart.
func heightArrSourceSlicingIndex(heightsArr []uint64, ancestorHeight uint64) (index int) {
	if ancestorHeight == 0 || len(heightsArr) == 0 {
		return
	}

	index = len(heightsArr) - 1
	for ; index >= 0; index-- {
		if heightsArr[index] <= ancestorHeight {
			if index+1 <= len(heightsArr) {
				// return index of entry before ancestor.
				index++
			}
			return
		}
	}

	return
}

// DropChartsDataToAncestor drops all the chart data entries since the common
// ancestor block height.
func (exp *explorerUI) DropChartsDataToAncestor(ancestorHeight uint64) error {
	ancestorTime, err := exp.explorerSource.BlockTimeByHeight(int64(ancestorHeight))
	if err != nil {
		return err
	}

	cacheChartsData.Lock()

	cacheData := cacheChartsData.Data

	// "avg-block-size" chart data.
	data := cacheData[dbtypes.AvgBlockSize]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.AvgBlockSize].Time = cacheData[dbtypes.AvgBlockSize].Time[:index]
		cacheData[dbtypes.AvgBlockSize].Size = cacheData[dbtypes.AvgBlockSize].Size[:index]
	}

	// "blockchain-size" chart data.
	data = cacheData[dbtypes.BlockChainSize]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.BlockChainSize].Time = cacheData[dbtypes.BlockChainSize].Time[:index]
		cacheData[dbtypes.BlockChainSize].ChainSize = cacheData[dbtypes.BlockChainSize].ChainSize[:index]
	}

	// "chainwork" chart data.
	data = cacheData[dbtypes.ChainWork]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.ChainWork].Time = cacheData[dbtypes.ChainWork].Time[:index]
		cacheData[dbtypes.ChainWork].ChainWork = cacheData[dbtypes.ChainWork].ChainWork[:index]
	}

	// "coin-supply" chart data.
	data = cacheData[dbtypes.CoinSupply]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.CoinSupply].Time = cacheData[dbtypes.CoinSupply].Time[:index]
		cacheData[dbtypes.CoinSupply].ValueF = cacheData[dbtypes.CoinSupply].ValueF[:index]
	}

	// "duration-btw-blocks" chart data.
	data = cacheData[dbtypes.DurationBTW]
	if data != nil {
		var index = heightArrSourceSlicingIndex(data.Height, ancestorHeight)

		cacheData[dbtypes.DurationBTW].Height = cacheData[dbtypes.DurationBTW].Height[:index]
		cacheData[dbtypes.DurationBTW].ValueF = cacheData[dbtypes.DurationBTW].ValueF[:index]
	}

	// "hashrate" chart data.
	data = cacheData[dbtypes.HashRate]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.HashRate].Time = cacheData[dbtypes.HashRate].Time[:index]
		cacheData[dbtypes.HashRate].NetHash = cacheData[dbtypes.HashRate].NetHash[:index]
	}

	// "pow-difficulty" chart data.
	data = cacheData[dbtypes.POWDifficulty]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.POWDifficulty].Time = cacheData[dbtypes.POWDifficulty].Time[:index]
		cacheData[dbtypes.POWDifficulty].Difficulty = cacheData[dbtypes.POWDifficulty].Difficulty[:index]
	}

	// "ticket-by-outputs-windows" chart data.
	data = cacheData[dbtypes.TicketByWindows]
	if data != nil {
		var index = heightArrSourceSlicingIndex(data.Height, ancestorHeight)

		cacheData[dbtypes.TicketByWindows].Height = cacheData[dbtypes.TicketByWindows].Height[:index]
		cacheData[dbtypes.TicketByWindows].Solo = cacheData[dbtypes.TicketByWindows].Solo[:index]
		cacheData[dbtypes.TicketByWindows].Pooled = cacheData[dbtypes.TicketByWindows].Pooled[:index]
	}

	// "ticket-price" chart data.
	data = cacheData[dbtypes.TicketPrice]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.TicketPrice].Time = cacheData[dbtypes.TicketPrice].Time[:index]
		cacheData[dbtypes.TicketPrice].ValueF = cacheData[dbtypes.TicketPrice].ValueF[:index]
	}

	// "ticket-by-outputs-blocks" chart data.
	data = cacheData[dbtypes.TicketsByBlocks]
	if data != nil {
		var index = heightArrSourceSlicingIndex(data.Height, ancestorHeight)

		cacheData[dbtypes.TicketsByBlocks].Height = cacheData[dbtypes.TicketsByBlocks].Height[:index]
		cacheData[dbtypes.TicketsByBlocks].Solo = cacheData[dbtypes.TicketsByBlocks].Solo[:index]
		cacheData[dbtypes.TicketsByBlocks].Pooled = cacheData[dbtypes.TicketsByBlocks].Pooled[:index]
	}

	// "ticket-spend-type" chart data.
	data = cacheData[dbtypes.TicketSpendT]
	if data != nil {
		var index = heightArrSourceSlicingIndex(data.Height, ancestorHeight)

		cacheData[dbtypes.TicketSpendT].Height = cacheData[dbtypes.TicketSpendT].Height[:index]
		cacheData[dbtypes.TicketSpendT].Unspent = cacheData[dbtypes.TicketSpendT].Unspent[:index]
		cacheData[dbtypes.TicketSpendT].Revoked = cacheData[dbtypes.TicketSpendT].Revoked[:index]
	}

	// "tx-per-block" chart data.
	data = cacheData[dbtypes.TxPerBlock]
	if data != nil {
		var index = heightArrSourceSlicingIndex(data.Height, ancestorHeight)

		cacheData[dbtypes.TxPerBlock].Height = cacheData[dbtypes.TxPerBlock].Height[:index]
		cacheData[dbtypes.TxPerBlock].Count = cacheData[dbtypes.TxPerBlock].Count[:index]
	}

	// "tx-per-day" chart data.
	data = cacheData[dbtypes.TxPerDay]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.TxPerDay].Time = cacheData[dbtypes.TxPerDay].Time[:index]
		cacheData[dbtypes.TxPerDay].Count = cacheData[dbtypes.TxPerDay].Count[:index]
	}

	// "fee-per-block" chart data.
	data = cacheData[dbtypes.FeePerBlock]
	if data != nil {
		var index = heightArrSourceSlicingIndex(data.Height, ancestorHeight)

		cacheData[dbtypes.FeePerBlock].Height = cacheData[dbtypes.FeePerBlock].Height[:index]
		cacheData[dbtypes.FeePerBlock].SizeF = cacheData[dbtypes.FeePerBlock].SizeF[:index]
	}

	// "ticket-pool-size" chart data.
	data = cacheData[dbtypes.TicketPoolSize]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.TicketPoolSize].Time = cacheData[dbtypes.TicketPoolSize].Time[:index]
		cacheData[dbtypes.TicketPoolSize].SizeF = cacheData[dbtypes.TicketPoolSize].SizeF[:index]
	}

	// "ticket-pool-value" chart data.
	data = cacheData[dbtypes.TicketPoolValue]
	if data != nil {
		var index = timeArrSourceSlicingIndex(data.Time, ancestorTime)

		cacheData[dbtypes.TicketPoolValue].Time = cacheData[dbtypes.TicketPoolValue].Time[:index]
		cacheData[dbtypes.TicketPoolValue].ValueF = cacheData[dbtypes.TicketPoolValue].ValueF[:index]
	}

	cacheChartsData.Unlock()

	return nil
}
