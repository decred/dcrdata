package explorer

import (
	"encoding/gob"
	"os"
	"sync"

	"github.com/decred/dcrdata/v4/db/dbtypes"
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
