// Copyright (c) 2018-2022, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package types

import (
	"container/heap"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

// constants from time
const (
	SecondsPerMinute int64 = 60
	SecondsPerHour   int64 = 60 * 60
	SecondsPerDay    int64 = 24 * SecondsPerHour
	SecondsPerWeek   int64 = 7 * SecondsPerDay
)

// CachedBlock represents a block that is managed by the cache.
type CachedBlock struct {
	height     uint32
	hash       string
	summary    *BlockDataBasic
	stakeInfo  *StakeInfoExtended
	accesses   int64
	accessTime int64
	heapIdx    int
}

type blockCache map[chainhash.Hash]*CachedBlock

// WatchPriorityQueue is a hack since the priority of a CachedBlock is modified
// (if access or access time is in the LessFn) without triggering a reheap.
func WatchPriorityQueue(bpq *BlockPriorityQueue) {
	ticker := time.NewTicker(250 * time.Millisecond)
	for range ticker.C {
		if bpq.doesNeedReheap() {
			start := time.Now()
			bpq.Reheap()
			fmt.Printf("Triggered REHEAP completed in %v\n", time.Since(start))
		}
	}
}

// APICache maintains a fixed-capacity cache of CachedBlocks. Use NewAPICache to
// create the cache with the desired capacity.
type APICache struct {
	mtx             sync.RWMutex
	isEnabled       atomic.Value
	capacity        uint32
	blockCache                               // map[chainhash.Hash]*CachedBlock
	MainchainBlocks map[int64]chainhash.Hash // needs to be handled in reorg
	expireQueue     *BlockPriorityQueue
	hits            uint64
	misses          uint64
}

// NewAPICache creates an APICache with the specified capacity.
func NewAPICache(capacity uint32) *APICache {
	apic := &APICache{
		capacity:        capacity,
		blockCache:      make(blockCache),
		MainchainBlocks: make(map[int64]chainhash.Hash, capacity),
		expireQueue:     NewBlockPriorityQueue(capacity),
	}

	go WatchPriorityQueue(apic.expireQueue)

	return apic
}

// SetLessFn sets the comparator used by the priority queue. For information on
// the input function, see the docs for (pq *BlockPriorityQueue).SetLessFn.
func (apic *APICache) SetLessFn(lessFn func(bi, bj *CachedBlock) bool) {
	apic.mtx.Lock()
	defer apic.mtx.Unlock()
	apic.expireQueue.SetLessFn(lessFn)
}

// BlockSummarySaver is likely to be required to be implemented by the type
// utilizing APICache.
type BlockSummarySaver interface {
	StoreBlockSummary(*BlockDataBasic) error
}

// Make sure APICache itself implements the methods of BlockSummarySaver.
var _ BlockSummarySaver = (*APICache)(nil)

// Capacity returns the capacity of the APICache
func (apic *APICache) Capacity() uint32 { return apic.capacity }

// UtilizationBlocks returns the number of blocks stored in the cache
func (apic *APICache) UtilizationBlocks() int64 { return int64(len(apic.blockCache)) }

// Utilization returns the percent utilization of the cache
func (apic *APICache) Utilization() float64 {
	apic.mtx.RLock()
	defer apic.mtx.RUnlock()
	return 100.0 * float64(len(apic.blockCache)) / float64(apic.capacity)
}

// Hits returns the hit count of the APICache
func (apic *APICache) Hits() uint64 { return apic.hits }

// Misses returns the miss count of the APICache
func (apic *APICache) Misses() uint64 { return apic.misses }

// StoreBlockSummary caches the input BlockDataBasic, if the priority queue
// indicates that the block should be added.
func (apic *APICache) StoreBlockSummary(blockSummary *BlockDataBasic) error {
	if !apic.IsEnabled() {
		fmt.Printf("API cache is disabled")
		return nil
	}

	height := blockSummary.Height
	hash, err := chainhash.NewHashFromStr(blockSummary.Hash)
	if err != nil {
		panic("that's not a real hash")
	}

	apic.mtx.Lock()
	defer apic.mtx.Unlock()

	b, ok := apic.blockCache[*hash]
	if ok && b.summary != nil {
		fmt.Printf("Already have the block summary in cache for block %s at height %d.\n",
			hash, height)
		return nil
	}

	// If CachedBlock found, but without summary, just add the summary.
	if ok {
		b.summary = blockSummary
		return nil
	}

	// insert into the cache and queue
	cachedBlock := newCachedBlock(blockSummary, nil)
	cachedBlock.Access()

	// Insert into queue and delete any cached block that was removed.
	wasAdded, removedBlock, removedHeight := apic.expireQueue.Insert(blockSummary, nil)
	if removedBlock != nil {
		delete(apic.blockCache, *removedBlock)
		delete(apic.MainchainBlocks, removedHeight)
	}

	// Add new block to to the block cache, if it went into the queue.
	if wasAdded {
		apic.blockCache[*hash] = cachedBlock
		apic.MainchainBlocks[int64(height)] = *hash
	}

	return nil
}

// StakeInfoSaver is likely to be required to be implemented by the type
// utilizing APICache.
type StakeInfoSaver interface {
	StoreStakeInfo(*StakeInfoExtended) error
}

// Make sure APICache itself implements the methods of StakeInfoSaver.
var _ StakeInfoSaver = (*APICache)(nil)

// StoreStakeInfo caches the input StakeInfoExtended, if the priority queue
// indicates that the block should be added.
func (apic *APICache) StoreStakeInfo(stakeInfo *StakeInfoExtended) error {
	if !apic.IsEnabled() {
		fmt.Printf("API cache is disabled")
		return nil
	}

	height := stakeInfo.Feeinfo.Height
	hash, err := chainhash.NewHashFromStr(stakeInfo.Hash)
	if err != nil {
		panic("that's not a real hash")
	}

	apic.mtx.Lock()
	defer apic.mtx.Unlock()

	b, ok := apic.blockCache[*hash]
	if ok {
		// If CachedBlock found, but without stakeInfo, just add it in.
		if b.stakeInfo == nil {
			b.stakeInfo = stakeInfo
			return nil
		}
		fmt.Printf("Already have the block's stake info in cache for block %s at height %d.\n",
			hash, height)
		return nil
	}

	// Insert into the cache and expire queue.
	cachedBlock := newCachedBlock(nil, stakeInfo)
	cachedBlock.Access()

	// Insert into queue and delete any cached block that was removed.
	wasAdded, removedBlock, removedHeight := apic.expireQueue.Insert(nil, stakeInfo)
	if removedBlock != nil {
		delete(apic.blockCache, *removedBlock)
		delete(apic.MainchainBlocks, removedHeight)
	}

	// Add new block to to the block cache, if it went into the queue.
	if wasAdded {
		apic.blockCache[*hash] = cachedBlock
		apic.MainchainBlocks[int64(height)] = *hash
	}

	return nil
}

// removeCachedBlock is the non-thread-safe version of RemoveCachedBlock.
func (apic *APICache) removeCachedBlock(cachedBlock *CachedBlock) {
	if cachedBlock == nil {
		return
	}
	// remove the block from the expiration queue
	apic.expireQueue.RemoveBlock(cachedBlock)
	// remove from block cache
	if hash, err := chainhash.NewHashFromStr(cachedBlock.hash); err != nil {
		delete(apic.blockCache, *hash)
	}
	delete(apic.MainchainBlocks, int64(cachedBlock.height))
}

// RemoveCachedBlock removes the input CachedBlock the cache. If the block is
// not in cache, this is essentially a silent no-op.
func (apic *APICache) RemoveCachedBlock(cachedBlock *CachedBlock) {
	apic.mtx.Lock()
	defer apic.mtx.Unlock()
	apic.removeCachedBlock(cachedBlock)
}

// RemoveCachedBlockByHeight attempts to remove a CachedBlock with the given
// height.
func (apic *APICache) RemoveCachedBlockByHeight(height int64) {
	apic.mtx.Lock()
	defer apic.mtx.Unlock()

	hash, ok := apic.MainchainBlocks[height]
	if !ok {
		return
	}

	cb := apic.getCachedBlockByHash(hash)
	apic.removeCachedBlock(cb)
}

// blockHash attempts to get the block hash for the main chain block at the
// given height. The boolean indicates a cache miss.
func (apic *APICache) blockHash(height int64) (chainhash.Hash, bool) {
	apic.mtx.RLock()
	defer apic.mtx.RUnlock()
	hash, ok := apic.MainchainBlocks[height]
	return hash, ok
}

// GetBlockHash attempts to get the block hash for the main chain block at the
// given height. The empty string ("") is returned in the event of a cache miss.
func (apic *APICache) GetBlockHash(height int64) string {
	if !apic.IsEnabled() {
		return ""
	}
	hash, ok := apic.blockHash(height)
	if !ok {
		return ""
	}
	return hash.String()
}

// GetBlockSize attempts to retrieve the block size for the input height. The
// return is -1 if no block with that height is cached.
func (apic *APICache) GetBlockSize(height int64) int32 {
	if !apic.IsEnabled() {
		return -1
	}
	cachedBlock := apic.GetCachedBlockByHeight(height)
	if cachedBlock != nil && cachedBlock.summary != nil {
		return int32(cachedBlock.summary.Size)
	}
	return -1
}

// GetBlockSummary attempts to retrieve the block summary for the input height.
// The return is nil if no block with that height is cached.
func (apic *APICache) GetBlockSummary(height int64) *BlockDataBasic {
	if !apic.IsEnabled() {
		return nil
	}
	cachedBlock := apic.GetCachedBlockByHeight(height)
	if cachedBlock != nil {
		return cachedBlock.summary
	}
	return nil
}

// GetStakeInfo attempts to retrieve the stake info for block at the the input
// height. The return is nil if no block with that height is cached.
func (apic *APICache) GetStakeInfo(height int64) *StakeInfoExtended {
	if !apic.IsEnabled() {
		return nil
	}
	cachedBlock := apic.GetCachedBlockByHeight(height)
	if cachedBlock != nil {
		return cachedBlock.stakeInfo
	}
	return nil
}

// GetStakeInfoByHash attempts to retrieve the stake info for block with the
// input hash. The return is nil if no block with that hash is cached.
func (apic *APICache) GetStakeInfoByHash(hash string) *StakeInfoExtended {
	if !apic.IsEnabled() {
		return nil
	}
	cachedBlock := apic.GetCachedBlockByHashStr(hash)
	if cachedBlock != nil {
		return cachedBlock.stakeInfo
	}
	return nil
}

// GetCachedBlockByHeight attempts to fetch a CachedBlock with the given height.
// The return is nil if no block with that height is cached.
func (apic *APICache) GetCachedBlockByHeight(height int64) *CachedBlock {
	apic.mtx.Lock()
	defer apic.mtx.Unlock()
	hash, ok := apic.MainchainBlocks[height]
	if !ok {
		return nil
	}
	return apic.getCachedBlockByHash(hash)
}

// GetBlockSummaryByHash attempts to retrieve the block summary for the given
// block hash. The return is nil if no block with that hash is cached.
func (apic *APICache) GetBlockSummaryByHash(hash string) *BlockDataBasic {
	if !apic.IsEnabled() {
		return nil
	}
	cachedBlock := apic.GetCachedBlockByHashStr(hash)
	if cachedBlock != nil {
		return cachedBlock.summary
	}
	return nil
}

// GetCachedBlockByHashStr attempts to fetch a CachedBlock with the given hash.
// The return is nil if no block with that hash is cached.
func (apic *APICache) GetCachedBlockByHashStr(hashStr string) *CachedBlock {
	// Validate the hash string, and get a *chainhash.Hash
	hash, err := chainhash.NewHashFromStr(hashStr)
	if err != nil {
		fmt.Printf("that's not a real hash!")
		return nil
	}

	apic.mtx.Lock()
	defer apic.mtx.Unlock()
	return apic.getCachedBlockByHash(*hash)
}

// GetCachedBlockByHash attempts to fetch a CachedBlock with the given hash. The
// return is nil if no block with that hash is cached.
func (apic *APICache) GetCachedBlockByHash(hash chainhash.Hash) *CachedBlock {
	// validate the chainhash.Hash
	if _, err := chainhash.NewHashFromStr(hash.String()); err != nil {
		fmt.Printf("that's not a real hash!")
		return nil
	}

	apic.mtx.Lock()
	defer apic.mtx.Unlock()
	return apic.getCachedBlockByHash(hash)
}

// getCachedBlockByHash retrieves the block with the given hash, or nil if it is
// not found. Successful retrieval will update the cached block's access time,
// and increment the block's access count. This function is not thread-safe. See
// GetCachedBlockByHash and GetCachedBlockByHashStr for thread safety and hash
// validation.
func (apic *APICache) getCachedBlockByHash(hash chainhash.Hash) *CachedBlock {
	cachedBlock, ok := apic.blockCache[hash]
	if ok {
		cachedBlock.Access()
		apic.expireQueue.setNeedsReheap(true)
		apic.expireQueue.setAccessTime(time.Now())
		apic.hits++
		return cachedBlock
	}
	apic.misses++
	return nil
}

// Enable sets the isEnabled flag of the APICache.
func (apic *APICache) Enable() {
	apic.isEnabled.Store(true)
}

// Disable sets the isEnabled flag of the APICache.
func (apic *APICache) Disable() {
	apic.isEnabled.Store(false)
}

// IsEnabled checks if the cache is enabled.
func (apic *APICache) IsEnabled() bool {
	enabled, ok := apic.isEnabled.Load().(bool)
	return ok && enabled
}

type cachedBlockData struct {
	blockSummary *BlockDataBasic
	stakeInfo    *StakeInfoExtended
}

// newCachedBlock wraps the given BlockDataBasic in a CachedBlock with no
// accesses and an invalid heap index. Use Access to set the access counter and
// time. The priority queue will set the heapIdx.
func newCachedBlock(summary *BlockDataBasic, stakeInfo *StakeInfoExtended) *CachedBlock {
	if summary == nil && stakeInfo == nil {
		return nil
	}

	height, hash, mismatch := checkBlockHeightHash(&cachedBlockData{
		blockSummary: summary,
		stakeInfo:    stakeInfo,
	})
	if mismatch {
		return nil
	}

	return &CachedBlock{
		height:    height,
		hash:      hash,
		summary:   summary,
		stakeInfo: stakeInfo,
		heapIdx:   -1,
	}
}

// Access increments the access count and sets the accessTime to now. The
// BlockDataBasic stored in the CachedBlock is returned.
func (b *CachedBlock) Access() *BlockDataBasic {
	b.accesses++
	b.accessTime = time.Now().UnixNano()
	return b.summary
}

// String satisfies the Stringer interface.
func (b CachedBlock) String() string {
	return fmt.Sprintf("{Height: %d, Accesses: %d, Time: %d, Heap Index: %d}",
		b.height, b.accesses, b.accessTime, b.heapIdx)
}

type blockHeap []*CachedBlock

// BlockPriorityQueue implements heap.Interface and holds CachedBlocks
type BlockPriorityQueue struct {
	mtx                  sync.RWMutex
	bh                   blockHeap
	capacity             uint32
	needsReheap          bool
	minHeight, maxHeight int64
	lessFn               func(bi, bj *CachedBlock) bool
	lastAccess           time.Time
}

// NewBlockPriorityQueue is the constructor for BlockPriorityQueue that
// initializes an empty heap with the given capacity, and sets the default
// LessFn as a comparison by access time with 1 day resolution (blocks accessed
// within the  24 hours are considered to have the same access time), followed
// by access count. Use BlockPriorityQueue.SetLessFn to redefine the comparator.
func NewBlockPriorityQueue(capacity uint32) *BlockPriorityQueue {
	pq := &BlockPriorityQueue{
		bh:         blockHeap{},
		capacity:   capacity,
		minHeight:  math.MaxUint32,
		maxHeight:  -1,
		lastAccess: time.Unix(0, 0),
	}
	pq.SetLessFn(MakeLessByAccessTimeThenCount(1))
	return pq
}

// Satisfy heap.Inferface

// Len is require for heap.Interface
func (pq *BlockPriorityQueue) Len() int {
	return len(pq.bh)
}

// Less performs the comparison priority(i) < priority(j). Use
// BlockPriorityQueue.SetLessFn to define the desired behavior for the
// CachedBlocks heap[i] and heap[j].
func (pq *BlockPriorityQueue) Less(i, j int) bool {
	return pq.lessFn(pq.bh[i], pq.bh[j])
}

// Swap swaps the cachedBlocks at i and j. This is used container/heap.
func (pq *BlockPriorityQueue) Swap(i, j int) {
	pq.bh[i], pq.bh[j] = pq.bh[j], pq.bh[i]
	pq.bh[i].heapIdx = i
	pq.bh[j].heapIdx = j
}

// SetLessFn sets the function called by Less. The input lessFn must accept two
// *CachedBlock and return a bool, unlike Less, which accepts heap indexes i, j.
// This allows to define a comparator without requiring a heap.
func (pq *BlockPriorityQueue) SetLessFn(lessFn func(bi, bj *CachedBlock) bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.lessFn = lessFn
}

// Some Functions that may be called by Less, and set as the comparator for the
// queue by SetLessFn.

// LessByHeight defines a higher priority CachedBlock as having a higher height.
// That is, more recent blocks have higher priority than older blocks.
func LessByHeight(bi, bj *CachedBlock) bool {
	return bi.height < bj.height
}

// LessByAccessCount defines higher priority CachedBlock as having been accessed
// more often.
func LessByAccessCount(bi, bj *CachedBlock) bool {
	return bi.accesses < bj.accesses
}

// LessByAccessTime defines higher priority CachedBlock as having a more recent
// access time. More recent accesses have a larger accessTime value (Unix time).
func LessByAccessTime(bi, bj *CachedBlock) bool {
	return bi.accessTime < bj.accessTime
}

// LessByAccessCountThenHeight compares access count with LessByAccessCount if
// the blocks have different accessTime values, otherwise it compares height
// with LessByHeight.
func LessByAccessCountThenHeight(bi, bj *CachedBlock) bool {
	if bi.accesses == bj.accesses {
		return LessByHeight(bi, bj)
	}
	return LessByAccessCount(bi, bj)
}

// MakeLessByAccessTimeThenCount will create a CachedBlock comparison function
// given the specified time resolution in milliseconds.  Two access times less than
// the given time apart are considered the same time, and access count is used
// to break the tie.
func MakeLessByAccessTimeThenCount(millisecondsBinned int64) func(bi, bj *CachedBlock) bool {
	millisecondThreshold := time.Duration(millisecondsBinned) * time.Millisecond
	return func(bi, bj *CachedBlock) bool {
		// higher priority is more recent (larger) access time
		epochDiff := (bi.accessTime - bj.accessTime) / int64(millisecondThreshold)
		if epochDiff == 0 {
			return LessByAccessCount(bi, bj)
		}
		// time diff is large enough, return direction (negative means i<j)
		return LessByAccessTime(bi, bj) // epochDiff < 0
	}
}

// Push a *cachedBlockData. Use heap.Push, not this directly.
func (pq *BlockPriorityQueue) Push(blockData interface{}) {
	data, ok := blockData.(*cachedBlockData)
	if !ok || data == nil {
		fmt.Printf("Failed to push a cachedBlockData: %v", data)
		return
	}

	b := newCachedBlock(data.blockSummary, data.stakeInfo)
	b.Access()
	b.heapIdx = len(pq.bh)

	pq.updateMinMax(b.height)
	pq.bh = append(pq.bh, b)
	pq.lastAccess = time.Unix(0, b.accessTime)
}

// Pop will return an interface{} that may be cast to *CachedBlock.  Use
// heap.Pop, not this.
func (pq *BlockPriorityQueue) Pop() interface{} {
	n := pq.Len()
	old := pq.bh
	block := old[n-1]
	block.heapIdx = -1
	pq.bh = old[0 : n-1]
	return block
}

// ResetHeap creates a fresh queue given the input []*CachedBlock. For every
// CachedBlock in the queue, ResetHeap resets the access count and time, and
// heap index. The min/max heights are reset, the heap is heapifies. NOTE: the
// input slice is modified, but not reordered. A fresh slice is created for PQ
// internal use.
func (pq *BlockPriorityQueue) ResetHeap(bh []*CachedBlock) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	pq.maxHeight = -1
	pq.minHeight = math.MaxUint32
	now := time.Now().UnixNano()
	for i := range bh {
		pq.updateMinMax(bh[i].height)
		bh[i].heapIdx = i
		bh[i].accesses = 1
		bh[i].accessTime = now
	}
	//pq.bh = bh
	pq.bh = make([]*CachedBlock, len(bh))
	copy(pq.bh, bh)
	// Do not call Reheap or setNeedsReheap unless you want a deadlock
	pq.needsReheap = false
	heap.Init(pq)
}

// Reheap is a shortcut for heap.Init(pq)
func (pq *BlockPriorityQueue) Reheap() {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	pq.needsReheap = false
	heap.Init(pq)
}

// Insert will add an element, while respecting the queue's capacity
// if at capacity
//   - compare with top and replace or return
//   - if replaced top, heapdown (Fix(pq,0))
//
// else (not at capacity)
//   - heap.Push, which is pq.Push (append at bottom) then heapup
func (pq *BlockPriorityQueue) Insert(summary *BlockDataBasic, stakeInfo *StakeInfoExtended) (bool, *chainhash.Hash, int64) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	if pq.capacity == 0 {
		return false, nil, -1
	}

	if summary == nil && stakeInfo == nil {
		return false, nil, -1
	}

	// At capacity
	if int(pq.capacity) <= pq.Len() {
		// Create a CachedBlock just to use the priority queue's lessFn.
		cachedBlock := newCachedBlock(summary, stakeInfo)
		cachedBlock.Access()
		cachedBlock.heapIdx = 0 // if block used, will replace top

		// If new block not lower priority than next to pop, replace that in the
		// queue and fix up the heap. Usually you don't replace if equal, but
		// new one is necessarily more recently accessed, so we replace.
		if pq.lessFn(pq.bh[0], cachedBlock) {
			removedBlockHashStr := pq.bh[0].hash
			removedBlockHash, _ := chainhash.NewHashFromStr(removedBlockHashStr)
			removedBlockHeight := pq.bh[0].height
			pq.UpdateBlock(pq.bh[0], summary, stakeInfo)
			if removedBlockHash != nil {
				pq.lastAccess = time.Now()
			}
			return true, removedBlockHash, int64(removedBlockHeight)
		}
		// otherwise this block is too low priority to add to queue
		return false, nil, -1
	}

	// With room to grow, append at bottom and bubble up.
	data := &cachedBlockData{
		blockSummary: summary,
		stakeInfo:    stakeInfo,
	}
	height, _, mismatch := checkBlockHeightHash(data)
	if mismatch {
		return false, nil, -1
	}

	heap.Push(pq, data)
	pq.RescanMinMaxForAdd(height) // no rescan, just set min/max
	pq.lastAccess = time.Now()
	return true, nil, -1
}

func checkBlockHeightHash(b *cachedBlockData) (height uint32, hash string, mismatch bool) {
	if b.blockSummary == nil {
		height = b.stakeInfo.Feeinfo.Height
		hash = b.stakeInfo.Hash
	} else {
		height = b.blockSummary.Height
		hash = b.blockSummary.Hash
		// Given both blockSummary and stakeInfo, make sure they agree.
		if b.stakeInfo != nil {
			if height != b.stakeInfo.Feeinfo.Height {
				fmt.Printf("Invalid cached block: summary at %d, stake info at %d",
					height, b.stakeInfo.Feeinfo.Height)
				mismatch = true
				return
			}
			if hash != b.stakeInfo.Hash {
				fmt.Printf("Invalid cached block: summary for %v, stake info for %v",
					hash, b.stakeInfo.Hash)
				mismatch = true
				return
			}
		}
	}
	return
}

// UpdateBlock will update the specified CachedBlock, which must be in the
// queue. This function is NOT thread-safe.
func (pq *BlockPriorityQueue) UpdateBlock(b *CachedBlock, summary *BlockDataBasic, stakeInfo *StakeInfoExtended) {
	if b != nil {
		heightAdded, hash, mismatch := checkBlockHeightHash(&cachedBlockData{
			blockSummary: summary,
			stakeInfo:    stakeInfo,
		})
		if mismatch {
			fmt.Println("Cannot UpdateBlock!")
			return
		}
		heightRemoved := b.height

		b.height = heightAdded
		b.hash = hash
		b.summary = summary
		b.stakeInfo = stakeInfo
		b.accesses = 0
		b.Access()
		heap.Fix(pq, b.heapIdx)
		pq.RescanMinMaxForUpdate(heightAdded, heightRemoved)
		pq.lastAccess = time.Unix(0, b.accessTime)
	}
}

func (pq *BlockPriorityQueue) setAccessTime(t time.Time) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.lastAccess = t
}

func (pq *BlockPriorityQueue) doesNeedReheap() bool {
	pq.mtx.RLock()
	defer pq.mtx.RUnlock()
	return pq.needsReheap &&
		time.Since(pq.lastAccess) > 7*time.Second
}

func (pq *BlockPriorityQueue) setNeedsReheap(needReheap bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.needsReheap = needReheap
}

// min/max blockheight may be updated as follows, Given:
// 1. current min and max block height (h_old) in heap
// 2. One of the following actions:
//   a. block being pushed - just set min/max when h_new > max or < min (updateMinMax())
//   b. block being popped - rescan when h_old == min or max
//   c. block being updated - 2a. then 2b.

// RescanMinMaxForAdd conditionally updates the heap min/max height given the
// height of the block to add (push). No scan, just update min/max. This
// function is NOT thread-safe.
func (pq *BlockPriorityQueue) RescanMinMaxForAdd(height uint32) {
	pq.updateMinMax(height)
}

// RescanMinMaxForRemove conditionally rescans the heap min/max height given the
// height of the block to remove (pop). Make sure to remove the block BEFORE
// running this, as any rescan of the heap will see the block. This function is
// NOT thread-safe.
func (pq *BlockPriorityQueue) RescanMinMaxForRemove(height uint32) {
	if int64(height) == pq.minHeight || int64(height) == pq.maxHeight {
		pq.RescanMinMax()
	}
}

// RescanMinMaxForUpdate conditionally rescans the heap min/max height given old
// and new heights of the CachedBlock being updated. This function is NOT
// thread-safe.
func (pq *BlockPriorityQueue) RescanMinMaxForUpdate(heightAdd, heightRemove uint32) {
	// If removing a block at either min or max height AND the added block does
	// not expand the range on the on relevant end, a rescan is necessary.
	if (int64(heightRemove) == pq.minHeight && heightAdd >= heightRemove) ||
		(int64(heightRemove) == pq.maxHeight && heightAdd <= heightRemove) {
		pq.RescanMinMax()
	}
	// only the added block height needs to be checked now, no rescan needed
	pq.updateMinMax(heightAdd)
}

// RemoveBlock removes the specified CachedBlock from the queue. Remember to
// remove it from the actual block cache!
func (pq *BlockPriorityQueue) RemoveBlock(b *CachedBlock) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	if b != nil && b.heapIdx > 0 && b.heapIdx < pq.Len() {
		// only remove the block it it is really in the queue
		if pq.bh[b.heapIdx].hash == b.hash {
			pq.RemoveIndex(b.heapIdx)
			return
		}
		fmt.Printf("Tried to remove a block that was NOT in the PQ. Hash: %v, Height: %d",
			b.hash, b.height)
	}
}

// RemoveIndex removes the CachedBlock at the specified position in the heap.
// This function is NOT thread-safe.
func (pq *BlockPriorityQueue) RemoveIndex(idx int) {
	removedHeight := pq.bh[idx].height
	heap.Remove(pq, idx)
	pq.RescanMinMaxForRemove(removedHeight)
}

// RescanMinMax rescans the entire heap to get the current min/max heights. This
// function is NOT thread-safe.
func (pq *BlockPriorityQueue) RescanMinMax() {
	for i := range pq.bh {
		pq.updateMinMax(pq.bh[i].height)
	}
}

// updateMinMax updates the queue's min/max block height given the input height.
// This function is NOT thread-safe.
func (pq *BlockPriorityQueue) updateMinMax(h uint32) {
	if int64(h) > pq.maxHeight {
		pq.maxHeight = int64(h)
	}
	if int64(h) < pq.minHeight {
		pq.minHeight = int64(h)
	}
}
