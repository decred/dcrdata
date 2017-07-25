package dcrdataapi

import (
	"container/heap"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

type CachedBlock struct {
	summary    *BlockDataBasic
	accesses   int64
	accessTime int64
	heapIdx    int
}

type blockCache map[chainhash.Hash]*CachedBlock

type APICache struct {
	sync.RWMutex
	isEnabled       bool
	capacity        uint32
	blockCache                       // map[chainhash.Hash]*CachedBlock
	MainchainBlocks []chainhash.Hash // needs to be handled in reorg
	expireQueue     *BlockPriorityQueue
}

//var _ BlockSummarySaver = (*APICache)(nil)

func (apic APICache) Capacity() uint32   { return apic.capacity }
func (apic APICache) Utilization() int64 { return int64(len(apic.blockCache)) }

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
		return fmt.Errorf("MainchainBlock slice too short (%d) add block at %d",
			len(apic.MainchainBlocks), height)
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
	cachedBlock := NewCachedBlock(blockSummary)
	cachedBlock.Access()
	apic.blockCache[*hash] = cachedBlock
	apic.expireQueue.Insert(blockSummary)

	return nil
}

func (apic *APICache) RemoveCachedBlock(cachedBlock *CachedBlock) {
	// remove the block from the expiration queue
	apic.expireQueue.RemoveBlock(cachedBlock)
	// remove from block cache
	if hash, err := chainhash.NewHashFromStr(cachedBlock.summary.Hash); err != nil {
		delete(apic.blockCache, *hash)
	}
}

func (apic *APICache) GetBlockSummary(height int64) *BlockDataBasic {
	cachedBlock := apic.GetCachedBlockByHeight(height)
	if cachedBlock != nil {
		return cachedBlock.summary
	}
	return nil
}

func (apic *APICache) GetCachedBlockByHeight(height int64) *CachedBlock {
	if int(height) > len(apic.MainchainBlocks) || height < 0 {
		fmt.Printf("block not in MainchainBlocks map!")
		return nil
	}
	hash := apic.MainchainBlocks[height]
	return apic.GetCachedBlockByHash(hash)
}

func (apic *APICache) GetCachedBlockByHashStr(hashStr string) *CachedBlock {
	hash, err := chainhash.NewHashFromStr(hashStr)
	if err != nil {
		fmt.Printf("that's not a real hash!")
		return nil
	}

	return apic.getCachedBlockByHash(*hash)
}

func (apic *APICache) GetCachedBlockByHash(hash chainhash.Hash) *CachedBlock {
	if _, err := chainhash.NewHashFromStr(hash.String()); err != nil {
		fmt.Printf("that's not a real hash!")
		return nil
	}

	return apic.getCachedBlockByHash(hash)
}

func (apic *APICache) getCachedBlockByHash(hash chainhash.Hash) *CachedBlock {
	cachedBlock, ok := apic.blockCache[hash]
	if ok {
		cachedBlock.Access()
		return cachedBlock
	}
	return nil
}

func (apic *APICache) Enable() {
	apic.Lock()
	defer apic.Unlock()
	apic.isEnabled = true
}

func (apic *APICache) Disable() {
	apic.Lock()
	defer apic.Unlock()
	apic.isEnabled = false
}

func NewAPICache(capacity uint32) *APICache {
	return &APICache{
		isEnabled:   true,
		capacity:    capacity,
		blockCache:  make(blockCache),
		expireQueue: NewBlockPriorityQueue(capacity),
	}
}

func NewCachedBlock(summary *BlockDataBasic) *CachedBlock {
	return &CachedBlock{
		summary:  summary,
		accesses: 0,
		heapIdx:  -1,
	}
}

func (b *CachedBlock) Access() *BlockDataBasic {
	b.accesses++
	b.accessTime = time.Now().UnixNano()
	return b.summary
}

func (b CachedBlock) String() string {
	return fmt.Sprintf("{Height: %d, Accesses: %d, Time: %d, Heap Index: %d}",
		b.summary.Height, b.accesses, b.accessTime, b.heapIdx)
}

type blockHeap []*CachedBlock

// BlockPriorityQueue implements heap.Interface and holds CachedBlocks
type BlockPriorityQueue struct {
	bh                   blockHeap
	capacity             uint32
	minHeight, maxHeight int64
	lessFn               func(bi, bj *CachedBlock) bool
}

func NewBlockPriorityQueue(capacity uint32) *BlockPriorityQueue {
	pq := &BlockPriorityQueue{
		bh:        blockHeap{},
		capacity:  capacity,
		minHeight: math.MaxUint32,
		maxHeight: -1,
	}
	pq.SetLessFn(LessByAccessCountThenHeight)
	return pq
}

func (pq BlockPriorityQueue) Len() int {
	return len(pq.bh)
}

func (pq BlockPriorityQueue) Less(i, j int) bool {
	return pq.lessFn(pq.bh[i], pq.bh[j])
}

func (pq *BlockPriorityQueue) SetLessFn(lessFn func(bi, bj *CachedBlock) bool) {
	pq.lessFn = lessFn
}

func LessByAccessCountThenHeight(bi, bj *CachedBlock) bool {
	if bi.accesses == bj.accesses {
		return LessByHeight(bi, bj)
	}
	return LessByAccessCount(bi, bj)
}

func LessByHeight(bi, bj *CachedBlock) bool {
	return bi.summary.Height < bj.summary.Height
}

func LessByAccessCount(bi, bj *CachedBlock) bool {
	return bi.accesses < bj.accesses
}

func LessByAccessTime(bi, bj *CachedBlock) bool {
	return bi.accessTime < bj.accessTime
}

func MakeLessByAccessTimeThenCount(secondsBinned int64) func(bi, bj *CachedBlock) bool {
	nanosecondThreshold := time.Duration(secondsBinned) * time.Second
	return func(bi, bj *CachedBlock) bool {
		epochDiff := (bi.accessTime - bj.accessTime) / int64(nanosecondThreshold)
		if epochDiff < 0 {
			return true
		}
		return LessByAccessCount(bi, bj)
	}
}

func GreaterByAccessCountThenHeight(bi, bj *CachedBlock) bool {
	if bi.accesses == bj.accesses {
		return GreaterByHeight(bi, bj)
	}
	return GreaterByAccessCount(bi, bj)
}

func GreaterByHeight(bi, bj *CachedBlock) bool {
	return bi.summary.Height > bj.summary.Height
}

func GreaterByAccessCount(bi, bj *CachedBlock) bool {
	return bi.accesses > bj.accesses
}

func (pq BlockPriorityQueue) Swap(i, j int) {
	pq.bh[i], pq.bh[j] = pq.bh[j], pq.bh[i]
	pq.bh[i].heapIdx = i
	pq.bh[j].heapIdx = j
}

// Push a *BlockDataBasic
func (pq *BlockPriorityQueue) Push(blockSummary interface{}) {
	b := &CachedBlock{
		summary:    blockSummary.(*BlockDataBasic),
		accesses:   1,
		accessTime: time.Now().UnixNano(),
		heapIdx:    len(pq.bh),
	}
	pq.updateMinMax(b.summary.Height)
	pq.bh = append(pq.bh, b)
}

// Pop will return an interface{} that may be cast to *CachedBlock
func (pq *BlockPriorityQueue) Pop() interface{} {
	n := pq.Len()
	old := pq.bh
	block := old[n-1]
	block.heapIdx = -1
	pq.bh = old[0 : n-1]
	return block
}

func (pq *BlockPriorityQueue) ResetHeap(bh []*CachedBlock) {
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
	pq.Reheap()
}

func (pq *BlockPriorityQueue) Reheap() {
	heap.Init(pq)
}

// Insert will add an element, while respecting the queue's capacity
// if at capacity
// 		- compare with top and replace or return
// 		- if replaced top, heapdown (Fix(pq,0))
// else (not at capacity)
// 		- heap.Push, which is pq.Push (append at bottom) then heapup
func (pq *BlockPriorityQueue) Insert(summary *BlockDataBasic) {
	if pq.capacity == 0 {
		return
	}

	cachedBlock := &CachedBlock{
		summary:    summary,
		accesses:   1,
		accessTime: time.Now().UnixNano(),
	}

	// At capacity
	if int(pq.capacity) == pq.Len() {
		// If new block not lower priority than next to pop, replace that in the
		// queue and fix up the heap.
		if pq.lessFn(pq.bh[0], cachedBlock) {
			cachedBlock.heapIdx = 0
			pq.bh[0] = cachedBlock
			heap.Fix(pq, 0)
		}
		// otherwise this block doesn't qualify
		return
	}

	// With room to grow, append at bottom and bubble up
	heap.Push(pq, summary)
}

func (pq *BlockPriorityQueue) UpdateBlock(b *CachedBlock, summary *BlockDataBasic) {
	if b != nil {
		b.summary = summary
		pq.updateMinMax(b.summary.Height)
		heap.Fix(pq, b.heapIdx)
	}
}

func (pq *BlockPriorityQueue) RemoveBlock(b *CachedBlock) {
	if b != nil && b.heapIdx > 0 && b.heapIdx < pq.Len() {
		pq.RemoveIndex(b.heapIdx)
	}
}

func (pq *BlockPriorityQueue) RemoveIndex(idx int) {
	heap.Remove(pq, idx)
	pq.RescanMinMax()
}

func (pq *BlockPriorityQueue) RescanMinMax() {
	for i := range pq.bh {
		pq.updateMinMax(pq.bh[i].summary.Height)
	}
}

func (pq *BlockPriorityQueue) updateMinMax(h uint32) {
	if int64(h) > pq.maxHeight {
		pq.maxHeight = int64(h)
	}
	if int64(h) < pq.minHeight {
		pq.minHeight = int64(h)
	}
}
