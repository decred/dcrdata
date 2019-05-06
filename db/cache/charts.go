// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package cache

import (
	"context"
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/txhelpers"
)

// Keys for specifying chart data type.
const (
	BlockSize       = "block-size"
	BlockChainSize  = "blockchain-size"
	ChainWork       = "chainwork"
	CoinSupply      = "coin-supply"
	DurationBTW     = "duration-btw-blocks"
	HashRate        = "hashrate"
	POWDifficulty   = "pow-difficulty"
	TicketPrice     = "ticket-price"
	TxCount         = "tx-count"
	Fees            = "fees"
	TicketPoolSize  = "ticket-pool-size"
	TicketPoolValue = "ticket-pool-value"
)

// ZoomLevel specifies the granularity of data.
type ZoomLevel string

const (
	DayZoom    ZoomLevel = "day"
	BlockZoom  ZoomLevel = "block"
	WindowZoom ZoomLevel = "window"
)

// DefaultZoomLevel will be used if a zoom level is not specified to
// (*ChartData).Chart (via empty string), or if the provided ZoomLevel is
// invalid.
var DefaultZoomLevel = DayZoom

// ParseZoom will return the matching zoom level, else the default zoom.
func ParseZoom(zoom string) ZoomLevel {
	switch ZoomLevel(zoom) {
	case BlockZoom:
		return BlockZoom
	case WindowZoom:
		return WindowZoom
	}
	return DefaultZoomLevel
}

const (
	aDay = 86400 // seconds
	// HashrateAvgLength is the number of blocks used the rolling average for
	// the network hashrate calculation.
	HashrateAvgLength = 120
)

// ChartError is an Error interface for use with constant errors.
type ChartError string

func (e ChartError) Error() string {
	return string(e)
}

// UnknownChartErr is returned when a chart key is provided that does not match
// any known chart type constant.
const UnknownChartErr = ChartError("unknown chart")

// InvalidZoomErr is returned when a ChartMaker receives an unknown ZoomLevel.
// In practice, this should be impossible, since ParseZoom returns a default
// if a supplied zoom specifier is invalid, and window-zoomed ChartMakers
// ignore the zoom flag.
const InvalidZoomErr = ChartError("invalid zoom")

// An interface for reading and setting the length of datasets.
type lengther interface {
	Length() int
	Truncate(int) lengther
}

// ChartFloats is a slice of floats. It satisfies the lengther interface, and
// provides methods for taking averages or sums of segments.
type ChartFloats []float64

// Length returns the length of data. Satisfies the lengther interface.
func (data ChartFloats) Length() int {
	return len(data)
}

// Truncate makes a subset of the underlying dataset. It satisfies the lengther
// interface.
func (data ChartFloats) Truncate(l int) lengther {
	return data[:l]
}

// If the data is longer than max, return a subset of length max.
func (data ChartFloats) snip(max int) ChartFloats {
	if len(data) < max {
		max = len(data)
	}
	return data[:max]
}

// Avg is the average value of a segment of the dataset.
func (data ChartFloats) Avg(s, e int) float64 {
	if e <= s {
		return 0
	}
	var sum float64
	for _, v := range data[s:e] {
		sum += v
	}
	return sum / float64(e-s)
}

// Sum is the accumulation of a segment of the dataset.
func (data ChartFloats) Sum(s, e int) (sum float64) {
	if e <= s {
		return 0
	}
	for _, v := range data[s:e] {
		sum += v
	}
	return
}

// A constructor for a sized ChartFloats.
func newChartFloats(size int) ChartFloats {
	return make([]float64, 0, size)
}

// ChartUints is a slice of uints. It satisfies the lengther interface, and
// provides methods for taking averages or sums of segments.
type ChartUints []uint64

// Length returns the length of data. Satisfies the lengther interface.
func (data ChartUints) Length() int {
	return len(data)
}

// Truncate makes a subset of the underlying dataset. It satisfies the lengther
// interface.
func (data ChartUints) Truncate(l int) lengther {
	return data[:l]
}

// If the data is longer than max, return a subset of length max.
func (data ChartUints) snip(max int) ChartUints {
	if len(data) < max {
		max = len(data)
	}
	return data[:max]
}

// Avg is the average value of a segment of the dataset.
func (data ChartUints) Avg(s, e int) uint64 {
	if e <= s {
		return 0
	}
	var sum uint64
	for _, v := range data[s:e] {
		sum += v
	}
	return sum / uint64(e-s)
}

// Sum is the accumulation of a segment of the dataset.
func (data ChartUints) Sum(s, e int) (sum uint64) {
	if e <= s {
		return 0
	}
	for _, v := range data[s:e] {
		sum += v
	}
	return
}

// A constructor for a sized ChartFloats.
func newChartUints(size int) ChartUints {
	return make(ChartUints, 0, size)
}

// zoomSet is a set of binned data. The smallest bin is block-sized. The zoomSet
// is managed by explorer, and subsequently the database packages. ChartData
// provides methods for validating the data and handling concurrency. The
// cacheID is updated anytime new data is added and validated (see
// Lengthen), typically once per bin duration.
type zoomSet struct {
	cacheID   uint64
	Height    ChartUints
	Time      ChartUints
	PoolSize  ChartUints
	PoolValue ChartFloats
	BlockSize ChartUints
	TxCount   ChartUints
	NewAtoms  ChartUints
	Chainwork ChartFloats
	Fees      ChartUints
}

// Snip truncates the zoomSet to a provided length.
func (set *zoomSet) Snip(length int) {
	if length < 0 {
		length = 0
	}
	set.Height = set.Height.snip(length)
	set.Time = set.Time.snip(length)
	set.PoolSize = set.PoolSize.snip(length)
	set.PoolValue = set.PoolValue.snip(length)
	set.BlockSize = set.BlockSize.snip(length)
	set.TxCount = set.TxCount.snip(length)
	set.NewAtoms = set.NewAtoms.snip(length)
	set.Chainwork = set.Chainwork.snip(length)
	set.Fees = set.Fees.snip(length)
}

// Constructor for a sized zoomSet for blocks, which has has no Height slice
// since the height is implicit for block-binned data.
func newBlockSet(size int) *zoomSet {
	return &zoomSet{
		Time:      newChartUints(size),
		PoolSize:  newChartUints(size),
		PoolValue: newChartFloats(size),
		BlockSize: newChartUints(size),
		TxCount:   newChartUints(size),
		NewAtoms:  newChartUints(size),
		Chainwork: newChartFloats(size),
		Fees:      newChartUints(size),
	}
}

// Constructor for a sized zoomSet for day-binned data.
func newDaySet(size int) *zoomSet {
	set := newBlockSet(size)
	set.Height = newChartUints(size)
	return set
}

// windowSet is for data that only changes at the difficulty change interval,
// 144 blocks on mainnet.
type windowSet struct {
	cacheID     uint64
	Time        ChartUints
	PowDiff     ChartFloats
	TicketPrice ChartUints
}

// Snip truncates the windowSet to a provided length.
func (set *windowSet) Snip(length int) {
	if length < 0 {
		length = 0
	}
	set.Time = set.Time.snip(length)
	set.PowDiff = set.PowDiff.snip(length)
	set.TicketPrice = set.TicketPrice.snip(length)
}

// Constructor for a sized windowSet.
func newWindowSet(size int) *windowSet {
	return &windowSet{
		Time:        newChartUints(size),
		PowDiff:     newChartFloats(size),
		TicketPrice: newChartUints(size),
	}
}

// ChartGobject is the storage object for saving to a gob file. ChartData itself
// has a lot of extraneous fields, and also embeds sync.RWMutex, so is not
// suitable for gobbing.
type ChartGobject struct {
	Height      ChartUints
	Time        ChartUints
	PoolSize    ChartUints
	PoolValue   ChartFloats
	BlockSize   ChartUints
	TxCount     ChartUints
	NewAtoms    ChartUints
	Chainwork   ChartFloats
	Fees        ChartUints
	WindowTime  ChartUints
	PowDiff     ChartFloats
	TicketPrice ChartUints
}

// The chart data is cached with the current cacheID of the zoomSet or windowSet.
type cachedChart struct {
	cacheID uint64
	data    []byte
}

// A generic structure for JSON encoding keyed data sets.
type chartResponse map[string]interface{}

// ChartUpdater is a pair of functions for fetching and appending chart data.
// The two steps are divided so that ChartData can check whether another thread
// has updated the data during the query, and abandon an update with appropriate
// messaging.
type ChartUpdater struct {
	Tag string
	// In addition to the sql.Rows and an error, the fetcher should return a
	// context.CancelFunc if appropriate, else a dummy.
	Fetcher func(*ChartData) (*sql.Rows, func(), error)
	// The Appender will be run under mutex lock.
	Appender func(*ChartData, *sql.Rows) error
}

// ChartData is a set of data used for charts. It provides methods for
// managing data validation and update concurrency, but does not perform any
// data retrieval and must be used with care to keep the data valid. The Blocks
// and Windows fields must be updated by (presumably) a database package. The
// Days data is auto-generated from the Blocks data during Lengthen-ing.
type ChartData struct {
	mtx          sync.RWMutex
	ctx          context.Context
	genesis      uint64
	DiffInterval int32
	Blocks       *zoomSet
	Windows      *windowSet
	Days         *zoomSet
	cacheMtx     sync.RWMutex
	cache        map[string]*cachedChart
	updaters     []ChartUpdater
}

// Check that the length of all arguments is equal.
func ValidateLengths(lens ...lengther) (int, error) {
	lenLen := len(lens)
	if lenLen == 0 {
		return 0, nil
	}
	firstLen := lens[0].Length()
	shortest := firstLen
	for i, l := range lens[1:lenLen] {
		dLen := l.Length()
		if dLen < firstLen {
			log.Warnf("charts.ValidateLengths: dataset at index %d has mismatched length %d != %d", i+1, dLen, firstLen)
			if dLen < shortest {
				shortest = dLen
			}
		}
	}
	if shortest < firstLen {
		return shortest, fmt.Errorf("data length mismatch")
	}
	return firstLen, nil
}

// Reduce the timestamp to the previous midnight.
func midnight(t uint64) (mid uint64) {
	if t > 0 {
		mid = t - t%aDay
	}
	return
}

// Lengthen performs data validation and populates the Days zoomSet. If there is
// an update to a zoomSet or windowSet, the cacheID will be incremented.
func (charts *ChartData) Lengthen() error {
	charts.mtx.Lock()
	defer charts.mtx.Unlock()

	// Make sure the database has set an equal number of blocks in each data set.
	blocks := charts.Blocks
	shortest, err := ValidateLengths(blocks.Time, blocks.PoolSize,
		blocks.PoolValue, blocks.BlockSize, blocks.TxCount,
		blocks.NewAtoms, blocks.Chainwork)
	if err != nil {
		log.Warnf("ChartData.Lengthen: block data length mismatch detected. truncating blocks length to %d", shortest)
		blocks.Snip(shortest)
	}
	if shortest == 0 {
		// No blocks yet. Not an error.
		return nil
	}

	windows := charts.Windows
	shortest, err = ValidateLengths(windows.Time, windows.PowDiff, windows.TicketPrice)
	if err != nil {
		log.Warnf("ChartData.Lengthen: window data length mismatch detected. truncating windows length to %d", shortest)
		charts.Windows.Snip(shortest)
	}
	if shortest == 0 {
		return fmt.Errorf("unexpected zero-length window data")
	}

	days := charts.Days
	// Truncate the last stamp to midnight
	end := midnight(blocks.Time[len(blocks.Time)-1])
	var start uint64
	if len(days.Time) > 0 {
		start = days.Time[len(days.Time)-1] // already truncated to midnight
	} else {
		start = midnight(charts.genesis)
	}
	intervals := [][2]int{}
	// If there is day or more worth of new data, append to the Days zoomSet by
	// finding the first and last+1 blocks of each new day, and taking averages
	// or sums of the blocks in the interval.
	if end > start+aDay {
		next := start + aDay
		startIdx := -1
		for i, t := range blocks.Time {
			if t < start {
				continue
			} else if t >= next {
				intervals = append(intervals, [2]int{startIdx, i})
				days.Time = append(days.Time, start)
				start = next
				next += aDay
				startIdx = i
				if t > end {
					break
				}
			} else if startIdx == -1 {
				startIdx = i
			}
		}
		for _, interval := range intervals {
			days.PoolSize = append(days.PoolSize, blocks.PoolSize.Avg(interval[0], interval[1]))
			days.PoolValue = append(days.PoolValue, blocks.PoolValue.Avg(interval[0], interval[1]))
			days.BlockSize = append(days.BlockSize, blocks.BlockSize.Sum(interval[0], interval[1]))
			days.TxCount = append(days.TxCount, blocks.TxCount.Sum(interval[0], interval[1]))
			days.NewAtoms = append(days.NewAtoms, blocks.NewAtoms.Sum(interval[0], interval[1]))
			days.Chainwork = append(days.Chainwork, blocks.Chainwork[interval[1]])
			days.Fees = append(days.Fees, blocks.Fees.Sum(interval[0], interval[1]))
			days.Height = append(days.Height, uint64(interval[1]-1))
		}
	}

	// Check that all relevant datasets have been updated to the same length.
	daysLen, err := ValidateLengths(days.PoolSize, days.PoolValue, days.BlockSize,
		days.TxCount, days.NewAtoms, days.Chainwork, days.Fees, days.Height, days.Time)
	if err != nil {
		return fmt.Errorf("day zoom: %v", err)
	} else if daysLen == 0 {
		return fmt.Errorf("unexpected zero-length day-binned data")
	}

	charts.cacheMtx.Lock()
	defer charts.cacheMtx.Unlock()
	// The cacheID for day-binned data, only increment the cacheID when entries
	// were added.
	if len(intervals) > 0 {
		days.cacheID++
	}
	// For blocks and windows, the cacheID is the last timestamp.
	charts.Blocks.cacheID = blocks.Time[len(blocks.Time)-1]
	charts.Windows.cacheID = windows.Time[len(windows.Time)-1]
	return nil
}

// ReorgHandler handles the charts cache data reorganization.
func (charts *ChartData) ReorgHandler(wg *sync.WaitGroup, c chan *txhelpers.ReorgData) {
	defer func() {
		wg.Done()
	}()
	for {
		select {
		case data, ok := <-c:
			if !ok {
				return
			}
			commonAncestorHeight := uint64(data.NewChainHeight) - uint64(len(data.NewChain))
			charts.mtx.Lock()
			newHeight := int(commonAncestorHeight) + 1
			log.Debug("ChartData.ReorgHandler snipping blocks height to %d", newHeight)
			charts.Blocks.Snip(newHeight)
			// Snip the last two days
			daysLen := len(charts.Days.Time)
			daysLen -= 2
			log.Debug("ChartData.ReorgHandler snipping days height to %d", daysLen)
			charts.Days.Snip(daysLen)
			// Drop the last window
			windowsLen := len(charts.Windows.Time)
			windowsLen--
			log.Debug("ChartData.ReorgHandler snipping windows to height to %d", windowsLen)
			charts.Windows.Snip(windowsLen)
			charts.mtx.Unlock()
			data.WG.Done()

		case <-charts.ctx.Done():
			return
		}
	}
}

// isfileExists checks if the provided file paths exists. It returns true if
// it does exist and false if otherwise.
func isfileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// writeCacheFile creates the charts cache in the provided file path if it
// doesn't exists. It dumps the ChartsData contents using the .gob encoding.
// Drops the old .gob dump before creating a new one. Delete the old cache here
// rather than after loading so that a dump will still be available after a crash.
func (charts *ChartData) writeCacheFile(filePath string) error {
	if isfileExists(filePath) {
		// delete the old dump files before creating new ones.
		os.RemoveAll(filePath)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer file.Close()

	encoder := gob.NewEncoder(file)
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return encoder.Encode(charts.gobject())
}

// readCacheFile reads the contents of the charts cache dump file encoded in
// .gob format if it exists returns an error if otherwise.
func (charts *ChartData) readCacheFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}

	defer func() {
		file.Close()
	}()

	var gobject = new(ChartGobject)

	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&gobject)
	if err != nil {
		return err
	}

	charts.mtx.Lock()
	charts.Blocks.Height = gobject.Height
	charts.Blocks.Time = gobject.Time
	charts.Blocks.PoolSize = gobject.PoolSize
	charts.Blocks.PoolValue = gobject.PoolValue
	charts.Blocks.BlockSize = gobject.BlockSize
	charts.Blocks.TxCount = gobject.TxCount
	charts.Blocks.NewAtoms = gobject.NewAtoms
	charts.Blocks.Chainwork = gobject.Chainwork
	charts.Blocks.Fees = gobject.Fees
	charts.Windows.Time = gobject.WindowTime
	charts.Windows.PowDiff = gobject.PowDiff
	charts.Windows.TicketPrice = gobject.TicketPrice
	charts.mtx.Unlock()

	err = charts.Lengthen()
	if err != nil {
		log.Warnf("problem detected during (*ChartData).Lengthen. clearing datasets: %v", err)
		charts.Blocks.Snip(0)
		charts.Windows.Snip(0)
		charts.Days.Snip(0)
	}

	return nil
}

// Load loads chart data from the gob file at the specified path and performs an
// update.
func (charts *ChartData) Load(cacheDumpPath string) {
	t := time.Now()

	if err := charts.readCacheFile(cacheDumpPath); err != nil {
		log.Debugf("Cache dump data loading failed: %v", err)
	}

	// Bring the charts up to date.
	charts.Update()

	log.Debugf("Completed the initial chart load in %f s", time.Since(t).Seconds())
}

// Dump dumps a ChartGobject to a gob file at the given path.
func (charts *ChartData) Dump(dumpPath string) {
	err := charts.writeCacheFile(dumpPath)
	if err != nil {
		log.Errorf("ChartData.writeCacheFile failed: %v", err)
	} else {
		log.Debug("Dumping the charts cache data was successful")
	}
}

// TriggerUpdate triggers (*ChartData).Update.
func (charts *ChartData) TriggerUpdate(_ string, _ uint32) {
	charts.Update()
}

func (charts *ChartData) gobject() *ChartGobject {
	return &ChartGobject{
		Height:      charts.Blocks.Height,
		Time:        charts.Blocks.Time,
		PoolSize:    charts.Blocks.PoolSize,
		PoolValue:   charts.Blocks.PoolValue,
		BlockSize:   charts.Blocks.BlockSize,
		TxCount:     charts.Blocks.TxCount,
		NewAtoms:    charts.Blocks.NewAtoms,
		Chainwork:   charts.Blocks.Chainwork,
		Fees:        charts.Blocks.Fees,
		WindowTime:  charts.Windows.Time,
		PowDiff:     charts.Windows.PowDiff,
		TicketPrice: charts.Windows.TicketPrice,
	}
}

// StateID returns a unique (enough) ID associted with the state of the Blocks
// data in a thread-safe way.
func (charts *ChartData) StateID() uint64 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return charts.stateID()
}

// stateID returns a unique (enough) ID associted with the state of the Blocks
// data.
func (charts *ChartData) stateID() uint64 {
	timeLen := len(charts.Blocks.Time)
	if timeLen > 0 {
		return charts.Blocks.Time[timeLen-1]
	}
	return 0
}

// ValidState checks whether the provided chartID is still valid. ValidState
// should be used under at least a (*ChartData).RLock.
func (charts *ChartData) validState(stateID uint64) bool {
	return charts.stateID() == stateID
}

// Height is the height of the blocks data. Data is assumed to be complete and
// without extraneous entries, which means that the (zoomSet).Height does not
// need to be populated for (ChartData).Blocks because the height is just
// len(Blocks.*)-1.
func (charts *ChartData) Height() int32 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return int32(len(charts.Blocks.Time)) - 1
}

// FeesTip is the height of the Fees data.
func (charts *ChartData) FeesTip() int32 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return int32(len(charts.Blocks.Fees)) - 1
}

// NewAtomsTip is the height of the NewAtoms data.
func (charts *ChartData) NewAtomsTip() int32 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return int32(len(charts.Blocks.NewAtoms)) - 1
}

// TicketPriceTip is the height of the TicketPrice data.
func (charts *ChartData) TicketPriceTip() int32 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return int32(len(charts.Windows.TicketPrice))*charts.DiffInterval - 1
}

// PoolSizeTip is the height of the PoolSize data.
func (charts *ChartData) PoolSizeTip() int32 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return int32(len(charts.Blocks.PoolSize)) - 1
}

// AddUpdater adds a ChartUpdater to the Updaters slice. Updaters are run
// sequentially during (*ChartData).Update.
func (charts *ChartData) AddUpdater(updater ChartUpdater) {
	charts.updaters = append(charts.updaters, updater)
}

// Update refreshes chart data by calling the ChartUpdaters sequentially. The
// Update is abandoned with a warning if stateID changes while running a Fetcher
// (likely due to a new update starting during a query).
func (charts *ChartData) Update() {
	for _, updater := range charts.updaters {
		stateID := charts.StateID()
		rows, cancel, err := updater.Fetcher(charts)
		if err != nil {
			err = fmt.Errorf("error encountered during charts %s update. aborting update: %v", updater.Tag, err)
		} else {
			charts.mtx.Lock()
			if !charts.validState(stateID) {
				err = fmt.Errorf("state change detected during charts %s update. aborting update", updater.Tag)
			} else {
				err = updater.Appender(charts, rows)
				if err != nil {
					err = fmt.Errorf("error detected during charts %s append. aborting update: %v", updater.Tag, err)
				}
			}
			charts.mtx.Unlock()
		}
		cancel()
		if err != nil {
			log.Errorf("%v", err)
			return
		}
	}

	// Since the charts db data query is complete. Update chart.Days derived dataset.
	if err := charts.Lengthen(); err != nil {
		log.Errorf("(*ChartData).Lengthen failed %v", err)
		return
	}
}

// NewChartData constructs a new ChartData.
func NewChartData(height uint32, genesis time.Time, chainParams *chaincfg.Params, ctx context.Context) *ChartData {
	// Allocate datasets for at least as many blocks as in a sdiff window.
	if int64(height) < chainParams.StakeDiffWindowSize {
		height = uint32(chainParams.StakeDiffWindowSize)
	}
	// Start datasets at 25% larger than height. This matches golangs default
	// capacity size increase for slice lengths > 1024
	// https://github.com/golang/go/blob/87e48c5afdcf5e01bb2b7f51b7643e8901f4b7f9/src/runtime/slice.go#L100-L112
	size := int(height * 5 / 4)
	days := int(time.Since(genesis)/time.Hour/24)*5/4 + 1 // at least one day
	windows := int(int64(height)/chainParams.StakeDiffWindowSize+1) * 5 / 4
	return &ChartData{
		ctx:          ctx,
		genesis:      uint64(genesis.Unix()),
		DiffInterval: int32(chainParams.StakeDiffWindowSize),
		Blocks:       newBlockSet(size),
		Windows:      newWindowSet(windows),
		Days:         newDaySet(days),
		cache:        make(map[string]*cachedChart),
		updaters:     make([]ChartUpdater, 0),
	}
}

// A cacheKey is used to specify cached data of a given type and ZoomLevel.
func cacheKey(chartID string, zoom ZoomLevel) string {
	return chartID + "-" + string(zoom)
}

// Grabs the cacheID associated with the provided ZoomLevel. Should be called
// under at least a (ChartData).cacheMtx.RLock.
func (charts *ChartData) cacheID(zoom ZoomLevel) uint64 {
	switch zoom {
	case BlockZoom:
		return charts.Blocks.cacheID
	case DayZoom:
		return charts.Days.cacheID
	case WindowZoom:
		return charts.Windows.cacheID
	}
	return 0
}

// Grab the cached data, if it exists. The cacheID is returned as a convenience.
func (charts *ChartData) getCache(chartID string, zoom ZoomLevel) (data *cachedChart, found bool, cacheID uint64) {
	// Ignore zero length since bestHeight would just be set to zero anyway.
	ck := cacheKey(chartID, zoom)
	charts.cacheMtx.RLock()
	defer charts.cacheMtx.RUnlock()
	cacheID = charts.cacheID(zoom)
	data, found = charts.cache[ck]
	return
}

// Store the chart associated with the provided type and ZoomLevel.
func (charts *ChartData) cacheChart(chartID string, zoom ZoomLevel, data []byte) {
	ck := cacheKey(chartID, zoom)
	charts.cacheMtx.Lock()
	defer charts.cacheMtx.Unlock()
	// Using the current best cacheID. This leaves open the small possibility that
	// the cacheID is wrong, if the cacheID has been updated between the
	// ChartMaker and here. This would just cause a one block delay.
	charts.cache[ck] = &cachedChart{
		cacheID: charts.cacheID(zoom),
		data:    data,
	}
}

// ChartMaker is a function that accepts a chart type and ZoomLevel, and returns
// a JSON-encoded chartResponse.
type ChartMaker func(charts *ChartData, zoom ZoomLevel) ([]byte, error)

var chartMakers = map[string]ChartMaker{
	BlockSize:       blockSizeChart,
	BlockChainSize:  blockchainSizeChart,
	ChainWork:       chainWorkChart,
	CoinSupply:      coinSupplyChart,
	DurationBTW:     durationBTWChart,
	HashRate:        hashRateChart,
	POWDifficulty:   powDifficultyChart,
	TicketPrice:     ticketPriceChart,
	TxCount:         txCountChart,
	Fees:            feesChart,
	TicketPoolSize:  ticketPoolSizeChart,
	TicketPoolValue: poolValueChart,
}

// Chart will return a JSON-encoded chartResponse of the provided type
// and ZoomLevel.
func (charts *ChartData) Chart(chartID, zoomString string) ([]byte, error) {
	zoom := ParseZoom(zoomString)
	cache, found, cacheID := charts.getCache(chartID, zoom)
	if found && cache.cacheID == cacheID {
		return cache.data, nil
	}
	maker, hasMaker := chartMakers[chartID]
	if !hasMaker {
		return nil, UnknownChartErr
	}
	// Do the locking here, rather than in encodeXY, so that the helper functions
	// (accumulate, btw) are run under lock.
	charts.mtx.RLock()
	data, err := maker(charts, zoom)
	charts.mtx.RUnlock()
	if err != nil {
		return nil, err
	}
	charts.cacheChart(chartID, zoom, data)
	return data, nil
}

// Keys used for the chartResponse data sets.
var responseKeys = []string{"x", "y", "z"}

// Encode the slices. The set lengths are truncated to the smallest of the
// arguments.
func (charts *ChartData) encode(sets ...lengther) ([]byte, error) {
	if len(sets) == 0 {
		return nil, fmt.Errorf("encode called without arguments")
	}
	smaller := sets[0].Length()
	for _, x := range sets {
		l := x.Length()
		if l < smaller {
			smaller = l
		}
	}
	response := make(chartResponse)
	for i := range sets {
		rk := responseKeys[i%len(responseKeys)]
		// If the length of the responseKeys array has been exceeded, add a integer
		// suffix to the response key. The key progression is x, y, z, x1, y1, z1,
		// x2, ...
		if i >= len(responseKeys) {
			rk += strconv.Itoa(i / len(responseKeys))
		}
		response[rk] = sets[i].Truncate(smaller)
	}
	return json.Marshal(response)
}

// Each point is translated to the sum of all points before and itself.
func accumulate(data ChartUints) ChartUints {
	d := make(ChartUints, 0, len(data))
	var accumulator uint64
	for _, v := range data {
		accumulator += v
		d = append(d, accumulator)
	}
	return d
}

// Translate the times slice to a slice of differences. The original dataset
// minus the first element is returned for convenience.
func blockTimes(blocks ChartUints) (ChartUints, ChartUints) {
	times := make(ChartUints, 0, len(blocks))
	dataLen := len(blocks)
	if dataLen < 2 {
		// Fewer than two data points is invalid for btw. Return empty data sets so
		// that the JSON encoding will have the correct type.
		return times, times
	}
	last := blocks[0]
	for _, v := range blocks[1:] {
		dif := v - last
		if int64(dif) < 0 {
			dif = 0
		}
		times = append(times, dif)
		last = v
	}
	return blocks[1:], times
}

// Take the average block times on the intervals defined by the ticks argument.
func avgBlockTimes(ticks, blocks ChartUints) (ChartUints, ChartUints) {
	if len(ticks) < 2 {
		// Return empty arrays so that JSON-encoding will have the correct type.
		return ChartUints{}, ChartUints{}
	}
	times := make(ChartUints, 0, len(ticks)-1)
	avgs := make(ChartUints, 0, len(ticks)-1)
	workingOn := ticks[0]
	nextIdx := 1
	next := ticks[nextIdx]
	lastIdx := 0
	for i, t := range blocks {
		if t > next {
			_, pts := blockTimes(blocks[lastIdx:i])
			avgs = append(avgs, pts.Avg(0, len(pts)))
			times = append(times, workingOn)
			nextIdx++
			if nextIdx > len(ticks)-1 {
				break
			}
			workingOn = next
			lastIdx = i
			next = ticks[nextIdx]
		}
	}
	return times, avgs
}

func blockSizeChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(charts.Blocks.Time, charts.Blocks.BlockSize)
	case DayZoom:
		return charts.encode(charts.Days.Time, charts.Days.BlockSize)
	}
	return nil, InvalidZoomErr
}

func blockchainSizeChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(charts.Blocks.Time, accumulate(charts.Blocks.BlockSize))
	case DayZoom:
		return charts.encode(charts.Days.Time, accumulate(charts.Days.BlockSize))
	}
	return nil, InvalidZoomErr
}

func chainWorkChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(charts.Blocks.Time, charts.Blocks.Chainwork)
	case DayZoom:
		return charts.encode(charts.Days.Time, charts.Days.Chainwork)
	}
	return nil, InvalidZoomErr
}

func coinSupplyChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(charts.Blocks.Time, accumulate(charts.Blocks.NewAtoms))
	case DayZoom:
		return charts.encode(charts.Days.Time, accumulate(charts.Days.NewAtoms), charts.Days.Height)
	}
	return nil, InvalidZoomErr
}

func durationBTWChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(blockTimes(charts.Blocks.Time))
	case DayZoom:
		return charts.encode(avgBlockTimes(charts.Days.Time, charts.Blocks.Time))
	}
	return nil, InvalidZoomErr
}

// hashrate converts the provided chainwork data to hashrate data. Since
// hashrates are averaged over HashrateAvgLength blocks, the returned slice
// is HashrateAvgLength shorter than the provided chainwork. A time slice is
// required as well, and a truncated time slice with the same length as the
// hashrate slice is returned.
func hashrate(time ChartUints, chainwork ChartFloats) (ChartUints, ChartFloats) {
	hrLen := len(chainwork) - HashrateAvgLength
	if hrLen <= 0 {
		return newChartUints(0), newChartFloats(0)
	}
	t := make(ChartUints, 0, hrLen)
	y := make(ChartFloats, 0, hrLen)
	var rotator [HashrateAvgLength]float64
	for i, work := range chainwork {
		idx := i % HashrateAvgLength
		rotator[idx] = work
		if i >= HashrateAvgLength {
			lastWork := rotator[(idx+1)%HashrateAvgLength]
			lastTime := time[i-HashrateAvgLength]
			thisTime := time[i]
			// 1e6: exahash -> terahash/s
			t = append(t, thisTime)
			y = append(y, (work-lastWork)*1e6/float64(thisTime-lastTime))
		}
	}
	return t, y
}

func hashRateChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		t, y := hashrate(charts.Blocks.Time, charts.Blocks.Chainwork)
		return charts.encode(t, y)
	case DayZoom:
		t, y := hashrate(charts.Days.Time, charts.Days.Chainwork)
		return charts.encode(t, y)
	}
	return nil, InvalidZoomErr
}

func powDifficultyChart(charts *ChartData, _ ZoomLevel) ([]byte, error) {
	// Pow Difficulty only has window level zoom, so all others are ignored.
	return charts.encode(charts.Windows.Time, charts.Windows.PowDiff)
}

func ticketPriceChart(charts *ChartData, _ ZoomLevel) ([]byte, error) {
	// Ticket price only has window level zoom, so all others are ignored.
	return charts.encode(charts.Windows.Time, charts.Windows.TicketPrice)
}

func txCountChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(charts.Blocks.Time, charts.Blocks.TxCount)
	case DayZoom:
		return charts.encode(charts.Days.Time, charts.Days.TxCount)
	}
	return nil, InvalidZoomErr
}

func feesChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(charts.Blocks.Time, charts.Blocks.Fees)
	case DayZoom:
		return charts.encode(charts.Days.Time, charts.Days.Fees)
	}
	return nil, InvalidZoomErr
}

func ticketPoolSizeChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(charts.Blocks.Time, charts.Blocks.PoolSize)
	case DayZoom:
		return charts.encode(charts.Days.Time, charts.Days.PoolSize)
	}
	return nil, InvalidZoomErr
}

func poolValueChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encode(charts.Blocks.Time, charts.Blocks.PoolValue)
	case DayZoom:
		return charts.encode(charts.Days.Time, charts.Days.PoolValue)
	}
	return nil, InvalidZoomErr
}
