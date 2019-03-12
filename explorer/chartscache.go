package explorer

import (
	"encoding/gob"
	"fmt"
	"os"
	"sync"

	"github.com/decred/dcrdata/v4/db/dbtypes"
)

// Charts cache data is managed here.

// chartDataCounter is a data cache for the historical charts.
type chartDataCounter struct {
	sync.RWMutex
	updateHeight int64
	Data         []*dbtypes.ChartsData
}

// cacheChartsData holds the prepopulated data that is used to draw the charts.
var cacheChartsData chartDataCounter

// get provides a thread-safe way to access the all cache charts data.
func (c *chartDataCounter) get() []*dbtypes.ChartsData {
	c.RLock()
	defer c.RUnlock()
	return c.Data
}

// Height returns the last update height of the charts data cache.
func (c *chartDataCounter) Height() int64 {
	c.RLock()
	defer c.RUnlock()
	return c.height()
}

// height returns the last update height of the charts data cache. Use Height
// instead for thread-safe access.
func (c *chartDataCounter) height() int64 {
	if c.updateHeight == 0 {
		return -1
	}
	return c.updateHeight
}

// Update sets new data for the given height in the the charts data cache.
func (c *chartDataCounter) Update(height int64, newData []*dbtypes.ChartsData) {
	c.Lock()
	defer c.Unlock()
	c.update(height, newData)
}

// update sets new data for the given height in the the charts data cache. Use
// Update instead for thread-safe access.
func (c *chartDataCounter) update(height int64, newData []*dbtypes.ChartsData) {
	c.updateHeight = height
	c.Data = newData
}

// ChartTypeData is a thread-safe way to access chart data of the given type.
func ChartTypeData(chartType string) (*dbtypes.ChartsData, bool) {
	cacheChartsData.RLock()
	defer cacheChartsData.RUnlock()

	chartVal, err := dbtypes.ChartsFromStr(chartType)
	if err != nil {
		log.Debug(err)
		return nil, false
	}

	// If an invalid chart type is found the returned data belongs to ticket
	// price chart.
	return cacheChartsData.Data[chartVal.Pos()], true
}

// isfileExists checks if the provided file paths exists. It returns true if
// it does exist and false if otherwise.
func isfileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// WriteCacheFile creates the charts cache file path if it doesn't exists. It
// dumps the cacheChartsData contents into a file using the .gob file encoding.
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
// .gob format if it exists returns an error if otherwise.
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

	var data = make([]*dbtypes.ChartsData, 0)

	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&data)
	if err != nil {
		return err
	}

	if len(data) != chartsCount {
		return fmt.Errorf("Found %d instead of %d charts in the cache dump",
			len(data), chartsCount)
	}

	cacheChartsData.Update(height, data)

	return nil
}
