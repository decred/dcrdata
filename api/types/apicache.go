// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package types

import (
	"container/heap"
	"fmt"
	"math"
	"sync"
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
	summary    *BlockDataBasic
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
		lastAccessTime := bpq.lastAccessTime()
		if bpq.doesNeedReheap() && time.Since(lastAccessTime) > 7*time.Second {
			start := time.Now()
			bpq.Reheap()
			fmt.Printf("Triggered REHEAP completed in %v\n", time.Since(start))
		}
	}
}

// APICache maintains a fixed-capacity cache of CachedBlocks. Use NewAPICache to
// create the cache with the desired capacity.
type APICache struct {
	sync.RWMutex
	isEnabled       bool
	capacity        uint32
	blockCache                       // map[chainhash.Hash]*CachedBlock
	MainchainBlocks []chainhash.Hash // needs to be handled in reorg
	expireQueue     *BlockPriorityQueue
	hits            uint64
	misses          uint64
}

// NewAPICache creates an APICache with the specified capacity.
//
// NOTE: The consumer of APICache should fill out MainChainBlocks before using
// it.  For example, given a struct DB at height dbHeight with an APICache:
//
//  DB.APICache = NewAPICache(10000)
//	DB.APICache.MainchainBlocks = make([]chainhash.Hash, 0, dbHeight+NExtra)
//	for i := int64(0); i <= dbHeight; i++ {
//		hash := DB.SomeFunctionToGetBlockHash(i)
//		DB.APICache.MainchainBlocks = append(DB.APICache.MainchainBlocks, *hash)
//	}
func NewAPICache(capacity uint32) *APICache {
	apic := &APICache{
		isEnabled:   true,
		capacity:    capacity,
		blockCache:  make(blockCache),
		expireQueue: NewBlockPriorityQueue(capacity),
	}

	go WatchPriorityQueue(apic.expireQueue)

	return apic
}

// SetLessFn sets the comparator used by the priority queue. For information on
// the input function, see the docs for (pq *BlockPriorityQueue).SetLessFn.
func (apic *APICache) SetLessFn(lessFn func(bi, bj *CachedBlock) bool) {
	apic.Lock()
	defer apic.Unlock()
	apic.expireQueue.SetLessFn(lessFn)
}

// BlockSummarySaver is likely to be required to be implemented by the type
// utilizing APICache.
type BlockSummarySaver interface {
	StoreBlockSummary(blockSummary *BlockDataBasic) error
}

// Make sure APICache itself implements the methods of BlockSummarySaver
var _ BlockSummarySaver = (*APICache)(nil)

// Capacity returns the capacity of the APICache
func (apic *APICache) Capacity() uint32 { return apic.capacity }

// UtilizationBlocks returns the number of blocks stored in the cache
func (apic *APICache) UtilizationBlocks() int64 { return int64(len(apic.blockCache)) }

// Utilization returns the percent utilization of the cache
func (apic *APICache) Utilization() float64 {
	apic.RLock()
	defer apic.RUnlock()
	return 100.0 * float64(len(apic.blockCache)) / float64(apic.capacity)
}

// Hits returns the hit count of the APICache
func (apic *APICache) Hits() uint64 { return apic.hits }

// Misses returns the miss count of the APICache
func (apic *APICache) Misses() uint64 { return apic.misses }

// StoreBlockSummary caches the input BlockDataBasic, if the priority queue
// indicates that the block should be added.
func (apic *APICache) StoreBlockSummary(blockSummary *BlockDataBasic) error {
	apic.Lock()
	defer apic.Unlock()

	if !apic.isEnabled {
		fmt.Printf("API cache is disabled")
		return nil
	}

	height := blockSummary.Height
	hash, err := chainhash.NewHashFromStr(blockSummary.Hash)
	if err != nil {
		panic("that's not a real hash")
	}

	if len(apic.MainchainBlocks) < int(height) {
		fmt.Printf("MainchainBlock slice too short (%d) to add block at %d. Padding with empty Hashes!",
			len(apic.MainchainBlocks), height)
		tail := make([]chainhash.Hash, int(height)-len(apic.MainchainBlocks))
		apic.MainchainBlocks = append(apic.MainchainBlocks, tail...)
	}

	if len(apic.MainchainBlocks) == int(height) {
		// append
		apic.MainchainBlocks = append(apic.MainchainBlocks, *hash)
	} else /* > */ {
		// update
		apic.MainchainBlocks[int(height)] = *hash
	}

	_, ok := apic.blockCache[*hash]
	if ok {
		fmt.Printf("Already have the block summary in cache for block %s at height %d",
			hash, height)
		return nil
	}

	// insert into the cache and queue
	cachedBlock := newCachedBlock(blockSummary)
	cachedBlock.Access()

	// Insert into queue and delete any cached block that was removed
	wasAdded, removedBlock := apic.expireQueue.Insert(blockSummary)
	if removedBlock != nil {
		delete(apic.blockCache, *removedBlock)
	}

	// Add new block to to the block cache, if it went into the queue
	if wasAdded {
		apic.blockCache[*hash] = cachedBlock
	}

	return nil
}

// RemoveCachedBlock removes the input CachedBlock the cache. If the block is
// not in cache, this is essentially a silent no-op.
func (apic *APICache) RemoveCachedBlock(cachedBlock *CachedBlock) {
	apic.Lock()
	defer apic.Unlock()
	// remove the block from the expiration queue
	apic.expireQueue.RemoveBlock(cachedBlock)
	// remove from block cache
	if hash, err := chainhash.NewHashFromStr(cachedBlock.summary.Hash); err != nil {
		delete(apic.blockCache, *hash)
	}
}

// GetBlockSummary attempts to retrieve the block summary for the input height.
// The return is nil if no block with that height is cached.
func (apic *APICache) GetBlockSummary(height int64) *BlockDataBasic {
	cachedBlock := apic.GetCachedBlockByHeight(height)
	if cachedBlock != nil {
		return cachedBlock.summary
	}
	return nil
}

// GetCachedBlockByHeight attempts to fetch a CachedBlock with the given height.
// The return is nil if no block with that height is cached.
func (apic *APICache) GetCachedBlockByHeight(height int64) *CachedBlock {
	apic.RLock()
	if int(height) >= len(apic.MainchainBlocks) || height < 0 {
		fmt.Printf("block not in MainchainBlocks slice!")
		return nil
	}
	hash := apic.MainchainBlocks[height]
	apic.RUnlock()
	return apic.GetCachedBlockByHash(hash)
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

	return apic.getCachedBlockByHash(hash)
}

// getCachedBlockByHash retrieves the block with the given hash, or nil if it is
// not found. Successful retrieval will update the cached block's access time,
// and increment the block's access count.
func (apic *APICache) getCachedBlockByHash(hash chainhash.Hash) *CachedBlock {

	apic.Lock()
	defer apic.Unlock()

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

// Enable sets the isEnabled flag of the APICache. The does little presently.
func (apic *APICache) Enable() {
	apic.Lock()
	defer apic.Unlock()
	apic.isEnabled = true
}

// Disable sets the isEnabled flag of the APICache. The does little presently.
func (apic *APICache) Disable() {
	apic.Lock()
	defer apic.Unlock()
	apic.isEnabled = false
}

// newCachedBlock wraps the given BlockDataBasic in a CachedBlock with no
// accesses and an invalid heap index. Use Access to make it valid.
func newCachedBlock(summary *BlockDataBasic) *CachedBlock {
	return &CachedBlock{
		summary: summary,
		heapIdx: -1,
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
		b.summary.Height, b.accesses, b.accessTime, b.heapIdx)
}

type blockHeap []*CachedBlock

// BlockPriorityQueue implements heap.Interface and holds CachedBlocks
type BlockPriorityQueue struct {
	*sync.RWMutex
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
		RWMutex:    new(sync.RWMutex),
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
func (pq BlockPriorityQueue) Len() int {
	return len(pq.bh)
}

// Less performs the comparison priority(i) < priority(j). Use
// BlockPriorityQueue.SetLessFn to define the desired behavior for the
// CachedBlocks heap[i] and heap[j].
func (pq BlockPriorityQueue) Less(i, j int) bool {
	return pq.lessFn(pq.bh[i], pq.bh[j])
}

// Swap swaps the cachedBlocks at i and j. This is used container/heap.
func (pq BlockPriorityQueue) Swap(i, j int) {
	pq.bh[i], pq.bh[j] = pq.bh[j], pq.bh[i]
	pq.bh[i].heapIdx = i
	pq.bh[j].heapIdx = j
}

// SetLessFn sets the function called by Less. The input lessFn must accept two
// *CachedBlock and return a bool, unlike Less, which accepts heap indexes i, j.
// This allows to define a comparator without requiring a heap.
func (pq *BlockPriorityQueue) SetLessFn(lessFn func(bi, bj *CachedBlock) bool) {
	pq.Lock()
	defer pq.Unlock()
	pq.lessFn = lessFn
}

// Some Functions that may be called by Less, and set as the comparator for the
// queue by SetLessFn.

// LessByHeight defines a higher priority CachedBlock as having a higher height.
// That is, more recent blocks have higher priority than older blocks.
func LessByHeight(bi, bj *CachedBlock) bool {
	return bi.summary.Height < bj.summary.Height
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

// Push a *BlockDataBasic. Use heap.Push, not this directly.
func (pq *BlockPriorityQueue) Push(blockSummary interface{}) {
	b := &CachedBlock{
		summary:    blockSummary.(*BlockDataBasic),
		accesses:   1,
		accessTime: time.Now().UnixNano(),
		heapIdx:    len(pq.bh),
	}
	pq.updateMinMax(b.summary.Height)
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
// input slice is modifed, but not reordered. A fresh slice is created for PQ
// internal use.
func (pq *BlockPriorityQueue) ResetHeap(bh []*CachedBlock) {
	pq.Lock()
	defer pq.Unlock()

	pq.maxHeight = -1
	pq.minHeight = math.MaxUint32
	now := time.Now().UnixNano()
	for i := range bh {
		pq.updateMinMax(bh[i].summary.Height)
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
	pq.Lock()
	defer pq.Unlock()

	pq.needsReheap = false
	heap.Init(pq)
}

// Insert will add an element, while respecting the queue's capacity
// if at capacity
// 		- compare with top and replace or return
// 		- if replaced top, heapdown (Fix(pq,0))
// else (not at capacity)
// 		- heap.Push, which is pq.Push (append at bottom) then heapup
func (pq *BlockPriorityQueue) Insert(summary *BlockDataBasic) (bool, *chainhash.Hash) {
	pq.Lock()
	defer pq.Unlock()

	if pq.capacity == 0 {
		return false, nil
	}

	// At capacity
	for int(pq.capacity) <= pq.Len() {
		//fmt.Printf("Cache full: %d / %d\n", pq.Len(), pq.capacity)
		cachedBlock := &CachedBlock{
			summary:    summary,
			accesses:   1,
			accessTime: time.Now().UnixNano(),
			heapIdx:    0, // if block used, will replace top
		}

		// If new block not lower priority than next to pop, replace that in the
		// queue and fix up the heap.  Usuall you don't replace if equal, but
		// new one is necessariy more recently accessed, so we replace.
		if pq.lessFn(pq.bh[0], cachedBlock) {
			// heightAdded, heightRemoved := summary.Height, pq.bh[0].summary.Height
			// pq.bh[0] = cachedBlock
			// heap.Fix(pq, 0)
			// pq.RescanMinMaxForUpdate(heightAdded, heightRemoved)
			removedBlockHashStr := pq.bh[0].summary.Hash
			removedBlockHash, _ := chainhash.NewHashFromStr(removedBlockHashStr)
			pq.UpdateBlock(pq.bh[0], summary)
			if removedBlockHash != nil {
				pq.lastAccess = time.Now()
			}
			return true, removedBlockHash
		}
		// otherwise this block is too low priority to add to queue
		return false, nil
	}

	// With room to grow, append at bottom and bubble up
	heap.Push(pq, summary)
	pq.RescanMinMaxForAdd(summary.Height) // no rescan, just set min/max
	pq.lastAccess = time.Now()
	return true, nil
}

// UpdateBlock will update the specified CachedBlock, which must be in the
// queue. This function is NOT thread-safe.
func (pq *BlockPriorityQueue) UpdateBlock(b *CachedBlock, summary *BlockDataBasic) {
	if b != nil {
		heightAdded, heightRemoved := summary.Height, b.summary.Height
		b.summary = summary
		b.accesses = 0
		b.Access()
		heap.Fix(pq, b.heapIdx)
		pq.RescanMinMaxForUpdate(heightAdded, heightRemoved)
		pq.lastAccess = time.Unix(0, b.accessTime)
	}
}

func (pq *BlockPriorityQueue) lastAccessTime() time.Time {
	pq.RLock()
	defer pq.RUnlock()
	return pq.lastAccess
}

func (pq *BlockPriorityQueue) setAccessTime(t time.Time) {
	pq.Lock()
	defer pq.Unlock()
	pq.lastAccess = t
}

func (pq *BlockPriorityQueue) doesNeedReheap() bool {
	pq.RLock()
	defer pq.RUnlock()
	return pq.needsReheap
}

func (pq *BlockPriorityQueue) setNeedsReheap(needReheap bool) {
	pq.Lock()
	defer pq.Unlock()
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
	pq.Lock()
	defer pq.Unlock()

	if b != nil && b.heapIdx > 0 && b.heapIdx < pq.Len() {
		// only remove the block it it is really in the queue
		if pq.bh[b.heapIdx].summary.Hash == b.summary.Hash {
			pq.RemoveIndex(b.heapIdx)
			return
		}
		fmt.Printf("Tried to remove a block that was NOT in the PQ. Hash: %v, Height: %d",
			b.summary.Hash, b.summary.Height)
	}
}

// RemoveIndex removes the CachedBlock at the specified position in the heap.
// This function is NOT thread-safe.
func (pq *BlockPriorityQueue) RemoveIndex(idx int) {
	removedHeight := pq.bh[idx].summary.Height
	heap.Remove(pq, idx)
	pq.RescanMinMaxForRemove(removedHeight)
}

// RescanMinMax rescans the enitire heap to get the current min/max heights.
// This function is NOT thread-safe.
func (pq *BlockPriorityQueue) RescanMinMax() {
	for i := range pq.bh {
		pq.updateMinMax(pq.bh[i].summary.Height)
	}
}

// updateMinMax updates the queue's min/max block height given the input height.
// This function is NOT thread-safe.
func (pq *BlockPriorityQueue) updateMinMax(h uint32) (updated bool) {
	if int64(h) > pq.maxHeight {
		pq.maxHeight = int64(h)
		updated = true
	}
	if int64(h) < pq.minHeight {
		pq.minHeight = int64(h)
		updated = true
	}
	return
}
