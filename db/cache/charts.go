// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package cache

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
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
	TicketByWindows = "ticket-by-outputs-windows"
	TicketPrice     = "ticket-price"
	TicketsByBlocks = "ticket-by-outputs-blocks"
	TicketSpendT    = "ticket-spend-type"
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
	aDay              = 86400
	HashrateAvgLength = 120
)

// ChartError is an Error interface for use with constant errors.
type ChartError string

func (e ChartError) Error() string {
	return string(e)
}

// UnknownChartErr is returned when a chart key is provided that does not match
// any known chart type constant.
const UnknownChartErr = ChartError("unkown chart")

// InvalidZoomErr is returned when a ChartMaker receives an unknown ZoomLevel.
// In practice, this should be impossible, since ParseZoom returns a default
// if a supplied zoom specifier is invalid, and window-zoomed ChartMaker's
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
	Chainwork ChartUints
	Fees      ChartUints
}

// snip truncates the zoomSet to a provided length.
func (set *zoomSet) snip(length int) {
	if length < 0 {
		length = 0
	}
	set.Height = set.Height[:length]
	set.Time = set.Time[:length]
	set.PoolSize = set.PoolSize[:length]
	set.PoolValue = set.PoolValue[:length]
	set.BlockSize = set.BlockSize[:length]
	set.TxCount = set.TxCount[:length]
	set.NewAtoms = set.NewAtoms[:length]
	set.Chainwork = set.Chainwork[:length]
	set.Fees = set.Fees[:length]
}

// Constructor for a sized zoomSet.
func newZoomSet(size int) *zoomSet {
	return &zoomSet{
		Height:    newChartUints(size),
		Time:      newChartUints(size),
		PoolSize:  newChartUints(size),
		PoolValue: newChartFloats(size),
		BlockSize: newChartUints(size),
		TxCount:   newChartUints(size),
		NewAtoms:  newChartUints(size),
		Chainwork: newChartUints(size),
		Fees:      newChartUints(size),
	}
}

// windowSet is for data that only changes at the difficulty change interval,
// 144 blocks on mainnet.
type windowSet struct {
	cacheID     uint64
	Time        ChartUints
	PowDiff     ChartFloats
	TicketPrice ChartUints
}

// snip truncates the windowSet to a provided length.
func (set *windowSet) snip(length int) {
	if length < 0 {
		length = 0
	}
	set.Time = set.Time[:length]
	set.PowDiff = set.PowDiff[:length]
	set.TicketPrice = set.TicketPrice[:length]
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
	Chainwork   ChartUints
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

// A generic x-y structure for []byte encoding.
type chartResponse struct {
	X interface{} `json:"x"`
	Y interface{} `json:"y"`
}

// Encode the provided slices to a JSON-encoded []byte.
func encodeChartResponse(x, y interface{}) ([]byte, error) {
	return json.Marshal(chartResponse{
		X: x,
		Y: y,
	})
}

// ChartData is a set of data used for charts. It provides methods for
// managing data validation and update concurrency, but does not perform any
// data retrieval and must be used with care to keep the data valid. The Blocks
// and Windows fields must be updated by (presumably) a database package. The
// Days data is auto-generated from the Blocks data during Lengthen-ing.
type ChartData struct {
	sync.RWMutex
	ctx          context.Context
	genesis      uint64
	DiffInterval int32
	Blocks       *zoomSet
	Windows      *windowSet
	Days         *zoomSet
	cacheMtx     sync.RWMutex
	cache        map[string]*cachedChart
}

// Check that the length of all arguments is equal.
func validateLengths(lens ...lengther) (int, error) {
	lenLen := len(lens)
	if lenLen == 0 {
		return 0, nil
	}
	firstLen := lens[0].Length()
	for i, l := range lens[1:lenLen] {
		if l.Length() != firstLen {
			return firstLen, fmt.Errorf("validateLengths: lengther index %d has mismatched length %d != %d", i+1, l.Length(), firstLen)
		}
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
	charts.Lock()
	defer charts.Unlock()

	// Make sure the database has set an equal number of blocks in each data set.
	blocks := charts.Blocks
	blockLen, err := validateLengths(blocks.Time, blocks.PoolSize,
		blocks.PoolValue, blocks.BlockSize, blocks.TxCount,
		blocks.NewAtoms, blocks.Chainwork)
	if err != nil {
		return fmt.Errorf("block zoom: %v", err)
	} else if blockLen == 0 {
		// No blocks yet. Not an error.
		return nil
	}

	windowsLen, err := validateLengths(charts.Windows.Time, charts.Windows.PowDiff, charts.Windows.TicketPrice)
	if err != nil {
		return fmt.Errorf("window zoom: %v", err)
	} else if windowsLen == 0 {
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
		}
	}

	// Check that all relevant datasets have been updated to the same length.
	daysLen, err := validateLengths(days.PoolSize, days.PoolValue, days.BlockSize,
		days.TxCount, days.NewAtoms, days.Chainwork, days.Fees)
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
	// For the block data, the cacheID is the last timestamp.
	charts.Blocks.cacheID = blocks.Time[len(blocks.Time)-1]
	// For the diff window data, use the data length as the cacheID.
	charts.Windows.cacheID = uint64(windowsLen)
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
			charts.Lock()
			defer charts.Unlock()
			charts.Blocks.snip(int(commonAncestorHeight) + 1)
			// Snip the last two days
			daysLen := len(charts.Days.Time)
			daysLen -= 2
			charts.Days.snip(daysLen)
			// Drop the last window
			windowsLen := len(charts.Windows.Time)
			windowsLen--
			charts.Windows.snip(windowsLen)
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

// WriteCacheFile creates the charts cache in the provided file path if it
// doesn't exists. It dumps the ChartsData contents using the .gob encoding.
func (charts *ChartData) WriteCacheFile(filePath string) (err error) {
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
	charts.RLock()
	defer charts.RUnlock()
	return encoder.Encode(charts.gobject())
}

// ReadCacheFile reads the contents of the charts cache dump file encoded in
// .gob format if it exists returns an error if otherwise. It then deletes
// the read *.gob cache dump file.
func (charts *ChartData) ReadCacheFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}

	defer func() {
		file.Close()

		// delete the dump after reading.
		os.RemoveAll(filePath)
	}()

	var gobject = new(ChartGobject)

	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&gobject)
	if err != nil {
		return err
	}

	charts.Lock()
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
	charts.Unlock()

	err = charts.Lengthen()
	if err != nil {
		log.Warnf("problem detected during (*ChartData).Lengthen. clearing datasets: %v", err)
		charts.Blocks.snip(0)
		charts.Windows.snip(0)
		charts.Days.snip(0)
	}

	return nil
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

// TipTime returns the time of the best block known to (ChartData).Blocks.
func (charts *ChartData) TipTime() uint64 {
	charts.RLock()
	defer charts.RUnlock()
	var t uint64
	if len(charts.Blocks.Time) != 0 {
		t = charts.Blocks.Time[len(charts.Blocks.Time)-1]
	}
	return t
}

// StateID returns a unique (enough) ID associted with the state of the Blocks
// data in a thread-safe way.
func (charts *ChartData) StateID() uint64 {
	charts.RLock()
	defer charts.RUnlock()
	return charts.stateID()
}

// StateID returns a unique (enough) ID associted with the state of the Blocks
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
func (charts *ChartData) ValidState(stateID uint64) bool {
	return charts.stateID() == stateID
}

// FeesTip is the height of the Fees data.
func (charts *ChartData) Height() int32 {
	charts.RLock()
	defer charts.RUnlock()
	return int32(len(charts.Blocks.Time)) - 1
}

// FeesTip is the height of the Fees data.
func (charts *ChartData) FeesTip() int32 {
	charts.RLock()
	defer charts.RUnlock()
	return int32(len(charts.Blocks.Fees)) - 1
}

// NewAtomsTip is the height of the NewAtoms data.
func (charts *ChartData) NewAtomsTip() int32 {
	charts.RLock()
	defer charts.RUnlock()
	return int32(len(charts.Blocks.NewAtoms)) - 1
}

// TicketPriceTip is the height of the TicketPrice data.
func (charts *ChartData) TicketPriceTip() int32 {
	charts.RLock()
	defer charts.RUnlock()
	return int32(len(charts.Windows.TicketPrice))*charts.DiffInterval - 1
}

// PoolSizeTip is the height of the PoolSize data.
func (charts *ChartData) PoolSizeTip() int32 {
	charts.RLock()
	defer charts.RUnlock()
	return int32(len(charts.Blocks.PoolSize)) - 1
}

// NewChartData constructs a new ChartData.
func NewChartData(height uint32, genesis time.Time, chainParams *chaincfg.Params, ctx context.Context) *ChartData {
	// Start datasets at 25% larger than height. This matches golangs default
	// capacity size increase for slice lengths > 1024
	// https://github.com/golang/go/blob/87e48c5afdcf5e01bb2b7f51b7643e8901f4b7f9/src/runtime/slice.go#L100-L112
	size := int(height * 5 / 4)
	days := int(int64(time.Since(genesis)/time.Hour/24)) * 5 / 4
	windows := int(int64(height)/chainParams.StakeDiffWindowSize+1) * 5 / 4
	return &ChartData{
		ctx:          ctx,
		genesis:      uint64(genesis.Unix()),
		DiffInterval: int32(chainParams.StakeDiffWindowSize),
		Blocks:       newZoomSet(size),
		Windows:      newWindowSet(windows),
		Days:         newZoomSet(days),
		cache:        make(map[string]*cachedChart),
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
// a JSON-encoded []byte chartResponse.
type ChartMaker func(charts *ChartData, zoom ZoomLevel) ([]byte, error)

var chartMakers = map[string]ChartMaker{
	BlockSize:      blockSizeChart,
	BlockChainSize: blockchainSizeChart,
	ChainWork:      chainWorkChart,
	CoinSupply:     coinSupplyChart,
	DurationBTW:    durationBTWChart,
	HashRate:       hashRateChart,
	POWDifficulty:  powDifficultyChart,
	// TicketByWindows: TicketByWindowsChart,
	TicketPrice: ticketPriceChart,
	// TicketsByBlocks: ,
	// TicketSpendT: ,
	TxCount: txCountChart,
	// TxPerDay: ,
	// FeePerBlock: ,
	Fees:            feesChart,
	TicketPoolSize:  ticketPoolSizeChart,
	TicketPoolValue: poolValueChart,
}

// Chart will return a JSON-encoded []byte chartResponse of the provided type
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
	charts.RLock()
	data, err := maker(charts, zoom)
	charts.RUnlock()
	if err != nil {
		return nil, err
	}
	charts.cacheChart(chartID, zoom, data)
	return data, nil
}

// encode the slices using encodeChartResponse. The length is truncated to the
// smaller of x or y.
func (charts *ChartData) encodeXY(x, y lengther) ([]byte, error) {
	smaller := x.Length()
	dataLen := y.Length()
	if dataLen < smaller {
		smaller = dataLen
	}
	bytes, err := encodeChartResponse(x.Truncate(smaller), y.Truncate(smaller))
	if err != nil {
		return nil, err
	}

	return bytes, nil
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

// Translate the uints to a slice of the differences between each. The provided
// data is assumed to be monotinically increasing. The first element is always
// 0 to keep the data length unchanged.
func btw(data ChartUints) ChartUints {
	d := make(ChartUints, 0, len(data))
	dataLen := len(data)
	if dataLen == 0 {
		return d
	}
	d = append(d, 0)
	last := data[0]
	for _, v := range data[1:] {
		d = append(d, v-last)
		last = v
	}
	return d
}

func blockSizeChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, charts.Blocks.BlockSize)
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, charts.Days.BlockSize)
	}
	return nil, InvalidZoomErr
}

func blockchainSizeChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, accumulate(charts.Blocks.BlockSize))
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, accumulate(charts.Days.BlockSize))
	}
	return nil, InvalidZoomErr
}

func chainWorkChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, charts.Blocks.Chainwork)
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, charts.Days.Chainwork)
	}
	return nil, InvalidZoomErr
}

func coinSupplyChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, accumulate(charts.Blocks.NewAtoms))
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, accumulate(charts.Days.NewAtoms))
	}
	return nil, InvalidZoomErr
}

func durationBTWChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, btw(charts.Blocks.Time))
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, btw(charts.Days.Time))
	}
	return nil, InvalidZoomErr
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
			// 1e6: exahash -> terahash/s
			t = append(t, thisTime)
			y = append(y, (work-lastWork)*1e6/(thisTime-lastTime))
		}
	}
	return t, y
}

func hashRateChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		t, y := hashrate(charts.Blocks.Time, charts.Blocks.Chainwork)
		return charts.encodeXY(t, y)
	case DayZoom:
		t, y := hashrate(charts.Days.Time, charts.Days.Chainwork)
		return charts.encodeXY(t, y)
	}
	return nil, InvalidZoomErr
}

func powDifficultyChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	// Pow Difficulty only has window level zoom, so all others are ignored.
	return charts.encodeXY(charts.Windows.Time, charts.Windows.PowDiff)
}

func ticketPriceChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	// Ticket price only has window level zoom, so all others are ignored.
	return charts.encodeXY(charts.Windows.Time, charts.Windows.TicketPrice)
}

func txCountChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, charts.Blocks.TxCount)
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, charts.Days.TxCount)
	}
	return nil, InvalidZoomErr
}

func feesChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, charts.Blocks.Fees)
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, charts.Days.Fees)
	}
	return nil, InvalidZoomErr
}

func ticketPoolSizeChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, charts.Blocks.PoolSize)
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, charts.Days.PoolSize)
	}
	return nil, InvalidZoomErr
}

func poolValueChart(charts *ChartData, zoom ZoomLevel) ([]byte, error) {
	switch zoom {
	case BlockZoom:
		return charts.encodeXY(charts.Blocks.Time, charts.Blocks.PoolValue)
	case DayZoom:
		return charts.encodeXY(charts.Days.Time, charts.Days.PoolValue)
	}
	return nil, InvalidZoomErr
}
