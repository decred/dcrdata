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
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrdata/semver"
	"github.com/decred/dcrdata/txhelpers/v4"
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
	AnonymitySet    = "privacy-participation"
	TicketPoolSize  = "ticket-pool-size"
	TicketPoolValue = "ticket-pool-value"
	WindMissedVotes = "missed-votes"
	PercentStaked   = "stake-participation"

	// Some chartResponse keys
	heightKey       = "h"
	timeKey         = "t"
	binKey          = "bin"
	axisKey         = "axis"
	supplyKey       = "supply"
	windowKey       = "window"
	diffKey         = "diff"
	priceKey        = "price"
	countKey        = "count"
	offsetKey       = "offset"
	circulationKey  = "circulation"
	poolValKey      = "poolval"
	missedKey       = "missed"
	sizeKey         = "size"
	feesKey         = "fees"
	anonymitySetKey = "anonymitySet"
	durationKey     = "duration"
	workKey         = "work"
	rateKey         = "rate"
)

// binLevel specifies the granularity of data.
type binLevel string

// axisType is used to manage the type of x-axis data on display on the specified
// chart.
type axisType string

// These are the recognized binLevel and axisType values.
const (
	DayBin     binLevel = "day"
	BlockBin   binLevel = "block"
	WindowBin  binLevel = "window"
	HeightAxis axisType = "height"
	TimeAxis   axisType = "time"
)

// Check if the chart is window binned.
func isWindowBin(chart string) bool {
	switch chart {
	case POWDifficulty, TicketPrice, WindMissedVotes:
		return true
	}
	return false
}

// DefaultBinLevel will be used if a bin level is not specified to
// (*ChartData).Chart (via empty string), or if the provided BinLevel is
// invalid.
var DefaultBinLevel = DayBin

// ParseBin will return the matching bin level, else the default bin.
func ParseBin(bin string) binLevel {
	switch binLevel(bin) {
	case BlockBin:
		return BlockBin
	case WindowBin:
		return WindowBin
	}
	return DefaultBinLevel
}

// ParseAxis returns the matching axis type, else the default of time axis.
func ParseAxis(aType string) axisType {
	switch axisType(aType) {
	case HeightAxis:
		return HeightAxis
	default:
		return TimeAxis
	}
}

const (
	// aDay defines the number of seconds in a day.
	aDay = 86400
	// HashrateAvgLength is the number of blocks used the rolling average for
	// the network hashrate calculation.
	HashrateAvgLength = 120
)

// cacheVersion helps detect when the cache data stored has changed its
// structure or content. A change on the cache version results to recomputing
// all the charts data a fresh thereby making the cache to hold the latest changes.
var cacheVersion = semver.NewSemver(6, 1, 1)

// versionedCacheData defines the cache data contents to be written into a .gob file.
type versionedCacheData struct {
	Version string
	Data    *ChartGobject
}

// ChartError is an Error interface for use with constant errors.
type ChartError string

func (e ChartError) Error() string {
	return string(e)
}

// UnknownChartErr is returned when a chart key is provided that does not match
// any known chart type constant.
const UnknownChartErr = ChartError("unknown chart")

// InvalidBinErr is returned when a ChartMaker receives an unknown BinLevel.
// In practice, this should be impossible, since ParseBin returns a default
// if a supplied bin specifier is invalid, and window-binned ChartMakers
// ignore the bin flag.
const InvalidBinErr = ChartError("invalid bin")

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
	cacheID      uint64
	Height       ChartUints
	Time         ChartUints
	PoolSize     ChartUints
	PoolValue    ChartUints
	BlockSize    ChartUints
	TxCount      ChartUints
	NewAtoms     ChartUints
	Chainwork    ChartUints
	Fees         ChartUints
	TotalMixed   ChartUints
	AnonymitySet ChartUints
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
	set.TotalMixed = set.TotalMixed.snip(length)
	set.AnonymitySet = set.AnonymitySet.snip(length)
}

// Constructor for a sized zoomSet for blocks, which has has no Height slice
// since the height is implicit for block-binned data.
func newBlockSet(size int) *zoomSet {
	return &zoomSet{
		Height:       newChartUints(size),
		Time:         newChartUints(size),
		PoolSize:     newChartUints(size),
		PoolValue:    newChartUints(size),
		BlockSize:    newChartUints(size),
		TxCount:      newChartUints(size),
		NewAtoms:     newChartUints(size),
		Chainwork:    newChartUints(size),
		Fees:         newChartUints(size),
		TotalMixed:   newChartUints(size),
		AnonymitySet: newChartUints(size),
	}
}

// Constructor for a sized zoomSet for day-binned data.
func newDaySet(size int) *zoomSet {
	set := newBlockSet(size)
	set.Height = newChartUints(size)
	return set
}

// windowSet is for data that only changes at the difficulty change interval,
// 144 blocks on mainnet. stakeValid defines the number windows before the
// stake validation height.
type windowSet struct {
	cacheID     uint64
	Time        ChartUints
	PowDiff     ChartFloats
	TicketPrice ChartUints
	StakeCount  ChartUints
	MissedVotes ChartUints
}

// Snip truncates the windowSet to a provided length.
func (set *windowSet) Snip(length int) {
	if length < 0 {
		length = 0
	}

	set.Time = set.Time.snip(length)
	set.PowDiff = set.PowDiff.snip(length)
	set.TicketPrice = set.TicketPrice.snip(length)
	set.StakeCount = set.StakeCount.snip(length)
	set.MissedVotes = set.MissedVotes.snip(length)
}

// Constructor for a sized windowSet.
func newWindowSet(size int) *windowSet {
	return &windowSet{
		Time:        newChartUints(size),
		PowDiff:     newChartFloats(size),
		TicketPrice: newChartUints(size),
		StakeCount:  newChartUints(size),
		MissedVotes: newChartUints(size),
	}
}

// ChartGobject is the storage object for saving to a gob file. ChartData itself
// has a lot of extraneous fields, and also embeds sync.RWMutex, so is not
// suitable for gobbing.
type ChartGobject struct {
	Height       ChartUints
	Time         ChartUints
	PoolSize     ChartUints
	PoolValue    ChartUints
	BlockSize    ChartUints
	TxCount      ChartUints
	NewAtoms     ChartUints
	Chainwork    ChartUints
	Fees         ChartUints
	WindowTime   ChartUints
	PowDiff      ChartFloats
	TicketPrice  ChartUints
	StakeCount   ChartUints
	MissedVotes  ChartUints
	TotalMixed   ChartUints
	AnonymitySet ChartUints
}

// The chart data is cached with the current cacheID of the zoomSet or windowSet.
type cachedChart struct {
	cacheID uint64
	data    []byte
}

// A generic structure for JSON encoding arbitrary data.
type chartResponse map[string]interface{}

// A commonly used seed for chartResponse encoding.
func binAxisSeed(bin binLevel, axis axisType) chartResponse {
	return chartResponse{
		binKey:  bin,
		axisKey: axis,
	}
}

// A generic structure for JSON encoding keyed data sets
type lengtherMap map[string]lengther

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
	mtx             sync.RWMutex
	ctx             context.Context
	DiffInterval    int32
	StartPOS        int32
	Blocks          *zoomSet
	Windows         *windowSet
	Days            *zoomSet
	cacheMtx        sync.RWMutex
	cache           map[string]*cachedChart
	updaters        []ChartUpdater
	updateMtx       sync.Mutex
}

// ValidateLengths checks that the length of all arguments is equal.
func ValidateLengths(lens ...lengther) (int, error) {
	lenLen := len(lens)
	if lenLen == 0 {
		return 0, nil
	}
	firstLen := lens[0].Length()
	shortest, longest := firstLen, firstLen
	for i, l := range lens[1:lenLen] {
		dLen := l.Length()
		if dLen != firstLen {
			log.Warnf("charts.ValidateLengths: dataset at index %d has mismatched length %d != %d", i+1, dLen, firstLen)
			if dLen < shortest {
				shortest = dLen
			} else if dLen > longest {
				longest = dLen
			}
		}
	}
	if shortest != longest {
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
	shortest, err := ValidateLengths(blocks.Height, blocks.Time,
		blocks.PoolSize, blocks.PoolValue, blocks.BlockSize, blocks.TxCount,
		blocks.NewAtoms, blocks.Chainwork, blocks.Fees, blocks.TotalMixed, blocks.AnonymitySet)
	if err != nil {
		log.Warnf("ChartData.Lengthen: block data length mismatch detected. "+
			"Truncating blocks length to %d", shortest)
		blocks.Snip(shortest)
	}
	if shortest == 0 {
		// No blocks yet. Not an error.
		return nil
	}

	windows := charts.Windows
	shortest, err = ValidateLengths(windows.Time, windows.PowDiff,
		windows.TicketPrice, windows.StakeCount, windows.MissedVotes)
	if err != nil {
		log.Warnf("ChartData.Lengthen: window data length mismatch detected. "+
			"Truncating windows length to %d", shortest)
		charts.Windows.Snip(shortest)
	}
	if shortest == 0 {
		return fmt.Errorf("unexpected zero-length window data")
	}

	days := charts.Days

	// Get the current first and last midnight stamps.
	end := midnight(blocks.Time[len(blocks.Time)-1])
	var start uint64
	if len(days.Time) > 0 {
		// Begin the scan at the beginning of the next day. The stamps in the Time
		// set are the midnight that starts the day.
		start = days.Time[len(days.Time)-1] + aDay
	} else {
		// Start from the beginning.
		// Already checked for empty blocks above.
		start = midnight(blocks.Time[0])
	}

	// Find the index that begins new data.
	offset := 0
	for i, t := range blocks.Time {
		if t > start {
			offset = i
			break
		}
	}

	intervals := [][2]int{}
	// If there is day or more worth of new data, append to the Days zoomSet by
	// finding the first and last+1 blocks of each new day, and taking averages
	// or sums of the blocks in the interval.
	if end > start+aDay {
		next := start + aDay
		startIdx := 0
		for i, t := range blocks.Time[offset:] {
			if t >= next {
				// Once passed the next midnight, prepare a day window by storing the
				// range of indices.
				intervals = append(intervals, [2]int{startIdx + offset, i + offset})
				days.Time = append(days.Time, start)
				start = next
				next += aDay
				startIdx = i
				if t > end {
					break
				}
			}
		}

		for _, interval := range intervals {
			// For each new day, take an appropriate snapshot. Some sets use sums,
			// some use averages, and some use the last value of the day.
			days.Height = append(days.Height, uint64(interval[1]-1))
			days.PoolSize = append(days.PoolSize, blocks.PoolSize.Avg(interval[0], interval[1]))
			days.PoolValue = append(days.PoolValue, blocks.PoolValue.Avg(interval[0], interval[1]))
			days.BlockSize = append(days.BlockSize, blocks.BlockSize.Sum(interval[0], interval[1]))
			days.TxCount = append(days.TxCount, blocks.TxCount.Sum(interval[0], interval[1]))
			days.NewAtoms = append(days.NewAtoms, blocks.NewAtoms.Sum(interval[0], interval[1]))
			days.Chainwork = append(days.Chainwork, blocks.Chainwork[interval[1]])
			days.Fees = append(days.Fees, blocks.Fees.Sum(interval[0], interval[1]))
			days.TotalMixed = append(days.TotalMixed, blocks.TotalMixed.Sum(interval[0], interval[1]))
			days.AnonymitySet = append(days.AnonymitySet, blocks.AnonymitySet.Avg(interval[0], interval[1]))
		}
	}

	// Check that all relevant datasets have been updated to the same length.
	daysLen, err := ValidateLengths(days.Height, days.Time, days.PoolSize,
		days.PoolValue, days.BlockSize, days.TxCount, days.NewAtoms,
		days.Chainwork, days.Fees, days.TotalMixed, days.AnonymitySet)
	if err != nil {
		return fmt.Errorf("day bin: %v", err)
	} else if daysLen == 0 {
		log.Warnf("(*ChartData).Lengthen: Zero-length day-binned data!")
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

// ReorgHandler handles the charts cache data reorganization. ReorgHandler
// satisfies notification.ReorgHandler, and is registered as a handler in
// main.go.
func (charts *ChartData) ReorgHandler(reorg *txhelpers.ReorgData) error {
	commonAncestorHeight := uint64(reorg.NewChainHeight) - uint64(len(reorg.NewChain))
	charts.mtx.Lock()
	newHeight := int(commonAncestorHeight) + 1
	log.Debugf("ChartData.ReorgHandler snipping blocks height to %d", newHeight)
	charts.Blocks.Snip(newHeight)
	// Snip the last two days
	daysLen := len(charts.Days.Time)
	daysLen -= 2
	log.Debugf("ChartData.ReorgHandler snipping days height to %d", daysLen)
	charts.Days.Snip(daysLen)
	// Drop the last window
	windowsLen := len(charts.Windows.Time)
	windowsLen--
	log.Debugf("ChartData.ReorgHandler snipping windows to height to %d", windowsLen)
	charts.Windows.Snip(windowsLen)
	charts.mtx.Unlock()
	return nil
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
	return encoder.Encode(versionedCacheData{cacheVersion.String(), charts.gobject()})
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

	var data = new(versionedCacheData)
	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&data)
	if err != nil {
		return err
	}

	// If the required cache version was not found in the .gob file return an error.
	if data.Version != cacheVersion.String() {
		return fmt.Errorf("expected cache version v%s but found v%s",
			cacheVersion, data.Version)
	}

	gobject := data.Data

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
	charts.Blocks.TotalMixed = gobject.TotalMixed
	charts.Blocks.AnonymitySet = gobject.AnonymitySet
	charts.Windows.Time = gobject.WindowTime
	charts.Windows.PowDiff = gobject.PowDiff
	charts.Windows.TicketPrice = gobject.TicketPrice
	charts.Windows.StakeCount = gobject.StakeCount
	charts.Windows.MissedVotes = gobject.MissedVotes

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
func (charts *ChartData) Load(cacheDumpPath string) error {
	t := time.Now()
	defer func() {
		log.Debugf("Completed the initial chart load and update in %f s",
			time.Since(t).Seconds())
	}()

	if err := charts.readCacheFile(cacheDumpPath); err != nil {
		log.Debugf("Cache dump data loading failed: %v", err)
		// Do not return non-nil error since a new cache file will be generated.
		// Also, return only after Update has restored the charts data.
	}

	// Bring the charts up to date.
	log.Infof("Updating charts data...")
	return charts.Update()
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
func (charts *ChartData) TriggerUpdate(_ string, _ uint32) error {
	if err := charts.Update(); err != nil {
		// Only log errors from ChartsData.Update. TODO: make this more severe.
		log.Errorf("(*ChartData).Update failed: %v", err)
	}
	return nil
}

func (charts *ChartData) gobject() *ChartGobject {
	return &ChartGobject{
		Height:       charts.Blocks.Height,
		Time:         charts.Blocks.Time,
		PoolSize:     charts.Blocks.PoolSize,
		PoolValue:    charts.Blocks.PoolValue,
		BlockSize:    charts.Blocks.BlockSize,
		TxCount:      charts.Blocks.TxCount,
		NewAtoms:     charts.Blocks.NewAtoms,
		Chainwork:    charts.Blocks.Chainwork,
		Fees:         charts.Blocks.Fees,
		TotalMixed:   charts.Blocks.TotalMixed,
		AnonymitySet: charts.Blocks.AnonymitySet,
		WindowTime:   charts.Windows.Time,
		PowDiff:      charts.Windows.PowDiff,
		TicketPrice:  charts.Windows.TicketPrice,
		StakeCount:   charts.Windows.StakeCount,
		MissedVotes:  charts.Windows.MissedVotes,
	}
}

// StateID returns a unique (enough) ID associated with the state of the Blocks
// data in a thread-safe way.
func (charts *ChartData) StateID() uint64 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return charts.stateID()
}

// stateID returns a unique (enough) ID associated with the state of the Blocks
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

// TotalMixedTip is the height of the CoinJoin Total Mixed data
func (charts *ChartData) TotalMixedTip() int32 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return int32(len(charts.Blocks.TotalMixed)) - 1
}

// AnonymitySetUpdateOffset is the height offset for update of the anonymity set
func (charts *ChartData) AnonymitySetUpdateOffset() int32 {
	charts.mtx.RLock()
	charts.mtx.RUnlock()
	return int32(len(charts.Blocks.AnonymitySet)) - 1
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

// MissedVotesTip is the height of the MissedVotes data.
func (charts *ChartData) MissedVotesTip() int32 {
	charts.mtx.RLock()
	defer charts.mtx.RUnlock()
	return int32(len(charts.Windows.MissedVotes))*charts.DiffInterval - 1
}

// AddUpdater adds a ChartUpdater to the Updaters slice. Updaters are run
// sequentially during (*ChartData).Update.
func (charts *ChartData) AddUpdater(updater ChartUpdater) {
	charts.updateMtx.Lock()
	charts.updaters = append(charts.updaters, updater)
	charts.updateMtx.Unlock()
}

// Update refreshes chart data by calling the ChartUpdaters sequentially. The
// Update is abandoned with a warning if stateID changes while running a Fetcher
// (likely due to a new update starting during a query).
func (charts *ChartData) Update() error {
	// Block simultaneous updates.
	charts.updateMtx.Lock()
	defer charts.updateMtx.Unlock()

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
			return err
		}
	}

	// Since the charts db data query is complete. Update chart.Days derived dataset.
	if err := charts.Lengthen(); err != nil {
		return fmt.Errorf("(*ChartData).Lengthen failed: %v", err)
	}
	return nil
}

// NewChartData constructs a new ChartData.
func NewChartData(ctx context.Context, height uint32, chainParams *chaincfg.Params) *ChartData {
	base64Height := int64(height)
	// Allocate datasets for at least as many blocks as in a sdiff window.
	if base64Height < chainParams.StakeDiffWindowSize {
		height = uint32(chainParams.StakeDiffWindowSize)
	}
	genesis := chainParams.GenesisBlock.Header.Timestamp
	// Start datasets at 25% larger than height. This matches golang's default
	// capacity size increase for slice lengths > 1024
	// https://github.com/golang/go/blob/87e48c5afdcf5e01bb2b7f51b7643e8901f4b7f9/src/runtime/slice.go#L100-L112
	size := int(height * 5 / 4)
	days := int(time.Since(genesis)/time.Hour/24)*5/4 + 1 // at least one day
	windows := int(base64Height/chainParams.StakeDiffWindowSize+1) * 5 / 4

	return &ChartData{
		ctx:             ctx,
		DiffInterval:    int32(chainParams.StakeDiffWindowSize),
		StartPOS:        int32(chainParams.StakeValidationHeight),
		Blocks:          newBlockSet(size),
		Windows:         newWindowSet(windows),
		Days:            newDaySet(days),
		cache:           make(map[string]*cachedChart),
		updaters:        make([]ChartUpdater, 0),
	}
}

// A cacheKey is used to specify cached data of a given type and BinLevel.
func cacheKey(chartID string, bin binLevel, axis axisType) string {
	// The axis type is only required when bin level is set to DayBin.
	return chartID + "-" + string(bin) + "-" + string(axis)
}

// Grabs the cacheID associated with the provided BinLevel. Should
// be called under at least a (ChartData).cacheMtx.RLock.
func (charts *ChartData) cacheID(bin binLevel) uint64 {
	switch bin {
	case BlockBin:
		return charts.Blocks.cacheID
	case DayBin:
		return charts.Days.cacheID
	case WindowBin:
		return charts.Windows.cacheID
	}
	return 0
}

// Grab the cached data, if it exists. The cacheID is returned as a convenience.
func (charts *ChartData) getCache(chartID string, bin binLevel, axis axisType) (data *cachedChart, found bool, cacheID uint64) {
	// Ignore zero length since bestHeight would just be set to zero anyway.
	ck := cacheKey(chartID, bin, axis)
	charts.cacheMtx.RLock()
	defer charts.cacheMtx.RUnlock()
	cacheID = charts.cacheID(bin)
	data, found = charts.cache[ck]
	return
}

// Store the chart associated with the provided type and BinLevel.
func (charts *ChartData) cacheChart(chartID string, bin binLevel, axis axisType, data []byte) {
	ck := cacheKey(chartID, bin, axis)
	charts.cacheMtx.Lock()
	defer charts.cacheMtx.Unlock()
	// Using the current best cacheID. This leaves open the small possibility that
	// the cacheID is wrong, if the cacheID has been updated between the
	// ChartMaker and here. This would just cause a one block delay.
	charts.cache[ck] = &cachedChart{
		cacheID: charts.cacheID(bin),
		data:    data,
	}
}

// ChartMaker is a function that accepts a chart type and BinLevel, and returns
// a JSON-encoded chartResponse.
type ChartMaker func(charts *ChartData, bin binLevel, axis axisType) ([]byte, error)

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
	AnonymitySet:    anonymitySetChart,
	TicketPoolSize:  ticketPoolSizeChart,
	TicketPoolValue: poolValueChart,
	WindMissedVotes: missedVotesChart,
	PercentStaked:   stakedCoinsChart,
}

// Chart will return a JSON-encoded chartResponse of the provided chart,
// binLevel, and axis (TimeAxis, HeightAxis). binString is ignored for
// window-binned charts.
func (charts *ChartData) Chart(chartID, binString, axisString string) ([]byte, error) {
	if isWindowBin(chartID) {
		binString = string(WindowBin)
	}
	bin := ParseBin(binString)
	axis := ParseAxis(axisString)
	cache, found, cacheID := charts.getCache(chartID, bin, axis)
	if found && cache.cacheID == cacheID {
		return cache.data, nil
	}
	maker, hasMaker := chartMakers[chartID]
	if !hasMaker {
		return nil, UnknownChartErr
	}
	// Do the locking here, rather than in encode, so that the helper functions
	// (accumulate, btw) are run under lock.
	charts.mtx.RLock()
	data, err := maker(charts, bin, axis)
	charts.mtx.RUnlock()
	if err != nil {
		return nil, err
	}
	charts.cacheChart(chartID, bin, axis, data)
	return data, nil
}

// Encode the data sets. Optionally add arbitrary additional data as part of the
// chartResponse seed. A nil seed is allowed.
func encode(sets lengtherMap, seed chartResponse) ([]byte, error) {
	if len(sets) == 0 {
		return nil, fmt.Errorf("encode called without arguments")
	}
	smaller := -1
	for _, set := range sets {
		l := set.Length()
		if smaller == -1 {
			smaller = l
		} else if l < smaller {
			smaller = l
		}
	}
	if len(seed) == 0 {
		seed = make(chartResponse, len(sets))
	}
	for k, v := range sets {
		seed[k] = v.Truncate(smaller)
	}
	return json.Marshal(seed)
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
	avgDiffs := make(ChartUints, 0, len(ticks)-1)
	times := make(ChartUints, 0, len(ticks)-1)
	nextIdx := 1
	workingOn := ticks[0]
	next := ticks[nextIdx]
	lastIdx := 0
	for i, t := range blocks {
		if t > next {
			_, pts := blockTimes(blocks[lastIdx:i])
			avgDiffs = append(avgDiffs, pts.Avg(0, len(pts)))
			times = append(times, workingOn)
			nextIdx++
			if nextIdx > len(ticks)-1 {
				break
			}
			lastIdx = i
			next = ticks[nextIdx]
			workingOn = next
		}
	}
	return times, avgDiffs
}

func blockSizeChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				sizeKey: charts.Blocks.BlockSize,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: charts.Blocks.Time,
				sizeKey: charts.Blocks.BlockSize,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey: charts.Days.Height,
				sizeKey:   charts.Days.BlockSize,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: charts.Days.Time,
				sizeKey: charts.Days.BlockSize,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func blockchainSizeChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				sizeKey: accumulate(charts.Blocks.BlockSize),
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: charts.Blocks.Time,
				sizeKey: accumulate(charts.Blocks.BlockSize),
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey: charts.Days.Height,
				sizeKey:   accumulate(charts.Days.BlockSize),
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: charts.Days.Time,
				sizeKey: accumulate(charts.Days.BlockSize),
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func chainWorkChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				workKey: charts.Blocks.Chainwork,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: charts.Blocks.Time,
				workKey: charts.Blocks.Chainwork,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey: charts.Days.Height,
				workKey:   charts.Days.Chainwork,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: charts.Days.Time,
				workKey: charts.Days.Chainwork,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func coinSupplyChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				supplyKey:       accumulate(charts.Blocks.NewAtoms),
				anonymitySetKey: charts.Blocks.AnonymitySet,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:         charts.Blocks.Time,
				supplyKey:       accumulate(charts.Blocks.NewAtoms),
				anonymitySetKey: charts.Blocks.AnonymitySet,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey:       charts.Days.Height,
				supplyKey:       accumulate(charts.Days.NewAtoms),
				anonymitySetKey: charts.Days.AnonymitySet,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:         charts.Days.Time,
				supplyKey:       accumulate(charts.Days.NewAtoms),
				anonymitySetKey: charts.Days.AnonymitySet,
				heightKey:       charts.Days.Height,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func durationBTWChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			_, diffs := blockTimes(charts.Blocks.Time)
			return encode(lengtherMap{
				durationKey: diffs,
			}, seed)
		default:
			times, diffs := blockTimes(charts.Blocks.Time)
			return encode(lengtherMap{
				timeKey:     times,
				durationKey: diffs,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			if len(charts.Days.Height) < 2 {
				return nil, fmt.Errorf("found the length of charts.Days.Height slice to be less than 2")
			}
			_, diffs := avgBlockTimes(charts.Days.Time, charts.Blocks.Time)
			return encode(lengtherMap{
				heightKey:   charts.Days.Height[:len(charts.Days.Height)-1],
				durationKey: diffs,
			}, seed)
		default:
			times, diffs := avgBlockTimes(charts.Days.Time, charts.Blocks.Time)
			return encode(lengtherMap{
				timeKey:     times,
				durationKey: diffs,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

// hashrate converts the provided chainwork data to hashrate data. Since
// hashrates are averaged over HashrateAvgLength blocks, the returned slice
// is HashrateAvgLength shorter than the provided chainwork. A time slice is
// required as well, and a truncated time slice with the same length as the
// hashrate slice is returned.
func hashrate(time, chainwork ChartUints) (ChartUints, ChartUints) {
	hrLen := len(chainwork) - HashrateAvgLength
	if hrLen <= 0 {
		return newChartUints(0), newChartUints(0)
	}
	t := make(ChartUints, 0, hrLen)
	y := make(ChartUints, 0, hrLen)
	var rotator [HashrateAvgLength]uint64
	for i, work := range chainwork {
		idx := i % HashrateAvgLength
		rotator[idx] = work
		if i >= HashrateAvgLength {
			lastWork := rotator[(idx+1)%HashrateAvgLength]
			lastTime := time[i-HashrateAvgLength]
			thisTime := time[i]
			t = append(t, thisTime)
			// 1e6: exahash -> terahash/s
			y = append(y, (work-lastWork)*1e6/(thisTime-lastTime))
		}
	}
	return t, y
}

// dailyHashrate converts the provided daily chainwork data to hashrate data.
// Since hashrates are based on a difference, the returned arrays will be 1
// element fewer than the number of days. A truncated time slice with the same
// length as the hashrate slice is returned.
func dailyHashrate(time, chainwork ChartUints) (ChartUints, ChartUints) {
	if len(time) == 0 || len(chainwork) == 0 {
		return ChartUints{}, ChartUints{}
	}
	times := make([]uint64, 0, len(time)-1)
	rates := make([]uint64, 0, len(time)-1)
	var dupes int
	for i, t := range time[1:] {
		tDiff := int64(t - time[i])
		if tDiff <= 0 {
			tDiff = aDay
			dupes += 1
		}
		workDiff := chainwork[i+1] - chainwork[i]
		rates = append(rates, (workDiff)*1e6/uint64(tDiff))
		times = append(times, t)
	}
	if dupes > 0 {
		log.Warnf("charts: dailyHashrate: %d duplicate timestamp(s) found")
	}
	return times, rates
}

func hashRateChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		if len(charts.Blocks.Time) < 2 {
			return nil, fmt.Errorf("Not enough blocks to calculate hashrate")
		}
		seed[offsetKey] = HashrateAvgLength
		times, rates := hashrate(charts.Blocks.Time, charts.Blocks.Chainwork)
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				rateKey: rates,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: times,
				rateKey: rates,
			}, seed)
		}
	case DayBin:
		if len(charts.Days.Time) < 2 {
			return nil, fmt.Errorf("Not enough days to calculate hashrate")
		}
		seed[offsetKey] = 1
		times, rates := dailyHashrate(charts.Days.Time, charts.Days.Chainwork)
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey: charts.Days.Height[1:],
				rateKey:   rates,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: times,
				rateKey: rates,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func powDifficultyChart(charts *ChartData, _ binLevel, axis axisType) ([]byte, error) {
	// Pow Difficulty only has window level bin, so all others are ignored.
	seed := chartResponse{windowKey: charts.DiffInterval}
	switch axis {
	case HeightAxis:
		return encode(lengtherMap{
			diffKey: charts.Windows.PowDiff,
		}, seed)
	default:
		return encode(lengtherMap{
			diffKey: charts.Windows.PowDiff,
			timeKey: charts.Windows.Time,
		}, seed)
	}
}

func ticketPriceChart(charts *ChartData, _ binLevel, axis axisType) ([]byte, error) {
	// Ticket price only has window level bin, so all others are ignored.
	seed := chartResponse{windowKey: charts.DiffInterval}
	switch axis {
	case HeightAxis:
		return encode(lengtherMap{
			priceKey: charts.Windows.TicketPrice,
			countKey: charts.Windows.StakeCount,
		}, seed)
	default:
		return encode(lengtherMap{
			timeKey:  charts.Windows.Time,
			priceKey: charts.Windows.TicketPrice,
			countKey: charts.Windows.StakeCount,
		}, seed)
	}
}

func txCountChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				countKey: charts.Blocks.TxCount,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:  charts.Blocks.Time,
				countKey: charts.Blocks.TxCount,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey: charts.Days.Height,
				countKey:  charts.Days.TxCount,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:  charts.Days.Time,
				countKey: charts.Days.TxCount,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func feesChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				feesKey: charts.Blocks.Fees,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: charts.Blocks.Time,
				feesKey: charts.Blocks.Fees,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey: charts.Days.Height,
				feesKey:   charts.Days.Fees,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey: charts.Days.Time,
				feesKey: charts.Days.Fees,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func anonymitySetChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey:       charts.Blocks.Height,
				anonymitySetKey: charts.Blocks.TotalMixed,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:         charts.Blocks.Time,
				anonymitySetKey: charts.Blocks.TotalMixed,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey:       charts.Days.Height,
				anonymitySetKey: charts.Days.TotalMixed,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:         charts.Days.Time,
				anonymitySetKey: charts.Days.TotalMixed,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func ticketPoolSizeChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				countKey: charts.Blocks.PoolSize,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:  charts.Blocks.Time,
				countKey: charts.Blocks.PoolSize,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey: charts.Days.Height,
				countKey:  charts.Days.PoolSize,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:  charts.Days.Time,
				countKey: charts.Days.PoolSize,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func poolValueChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				poolValKey: charts.Blocks.PoolValue,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:    charts.Blocks.Time,
				poolValKey: charts.Blocks.PoolValue,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey:  charts.Days.Height,
				poolValKey: charts.Days.PoolValue,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:    charts.Days.Time,
				poolValKey: charts.Days.PoolValue,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}

func missedVotesChart(charts *ChartData, _ binLevel, axis axisType) ([]byte, error) {
	prestakeWindows := int(charts.StartPOS / charts.DiffInterval)
	if prestakeWindows >= len(charts.Windows.MissedVotes) ||
		prestakeWindows >= len(charts.Windows.Time) {
		prestakeWindows = 0
	}
	seed := chartResponse{
		windowKey: charts.DiffInterval,
		offsetKey: prestakeWindows,
	}
	switch axis {
	case HeightAxis:
		return encode(lengtherMap{
			missedKey: charts.Windows.MissedVotes[prestakeWindows:],
		}, seed)
	default:
		return encode(lengtherMap{
			timeKey:   charts.Windows.Time[prestakeWindows:],
			missedKey: charts.Windows.MissedVotes[prestakeWindows:],
		}, seed)
	}
}

func stakedCoinsChart(charts *ChartData, bin binLevel, axis axisType) ([]byte, error) {
	seed := binAxisSeed(bin, axis)
	switch bin {
	case BlockBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				circulationKey: accumulate(charts.Blocks.NewAtoms),
				poolValKey:     charts.Blocks.PoolValue,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:        charts.Blocks.Time,
				circulationKey: accumulate(charts.Blocks.NewAtoms),
				poolValKey:     charts.Blocks.PoolValue,
			}, seed)
		}
	case DayBin:
		switch axis {
		case HeightAxis:
			return encode(lengtherMap{
				heightKey:      charts.Days.Height,
				circulationKey: accumulate(charts.Days.NewAtoms),
				poolValKey:     charts.Days.PoolValue,
			}, seed)
		default:
			return encode(lengtherMap{
				timeKey:        charts.Days.Time,
				circulationKey: accumulate(charts.Days.NewAtoms),
				poolValKey:     charts.Days.PoolValue,
			}, seed)
		}
	}
	return nil, InvalidBinErr
}
