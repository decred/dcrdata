// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrdataapi

import (
	"container/heap"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/decred/dcrd/dcrjson"
)

type AddressSearchResults []*dcrjson.SearchRawTransactionsResult

type AddressSearchRequest struct {
	Address     string
	Skip, Count int
}

type AddressSearch struct {
	AddedAtBlock int64
	Request      AddressSearchRequest
	Results      AddressSearchResults
}

// CachedAddress represents a an address that is managed by the cache.
type CachedAddress struct {
	search     *AddressSearch
	accesses   int64
	accessTime int64
	heapIdx    int
}

type addressHeap []*CachedAddress
type addressCache map[string]*CachedAddress

type AddressCache struct {
	sync.RWMutex
	capacity uint32
	addressCache
	expireQueue *AddressPriorityQueue
	hits        uint64
	misses      uint64
}

func NewAddressCache(transactionCapacity uint32) *AddressCache {
	addrc := &AddressCache{
		capacity:     transactionCapacity,
		addressCache: make(addressCache),
		expireQueue:  NewAddressPriorityQueue(transactionCapacity),
	}

	go WatchAddressPriorityQueue(addrc.expireQueue)

	return addrc
}

// WatchAddressPriorityQueue is a hack since the priority of a CachedAddress is
// modified without triggering a reheap if LessFn uses access or access time.
func WatchAddressPriorityQueue(pq *AddressPriorityQueue) {
	ticker := time.NewTicker(250 * time.Millisecond)
	for range ticker.C {
		lastAccessTime := pq.lastAccessTime()
		if pq.doesNeedReheap() && time.Since(lastAccessTime) > 7*time.Second {
			start := time.Now()
			pq.Reheap()
			fmt.Printf("Triggered REHEAP completed in %v\n", time.Since(start))
		}
	}
}

// AddressPriorityQueue implements heap.Interface and holds CachedAddresses
type AddressPriorityQueue struct {
	sync.RWMutex
	ah                   addressHeap
	capacity             uint32
	needsReheap          bool
	minHeight, maxHeight int64
	lessFn               func(bi, bj *CachedAddress) bool
	lastAccess           time.Time
}

// NewAddressPriorityQueue is the constructor for AddressPriorityQueue that
// initializes an empty heap with the given capacity, and sets the default
// LessFn as a comparison ... . Use AddressPriorityQueue.SetLessFn to redefine
// the comparator.
func NewAddressPriorityQueue(transactionCapacity uint32) *AddressPriorityQueue {
	pq := &AddressPriorityQueue{
		ah:         addressHeap{},
		capacity:   transactionCapacity,
		minHeight:  math.MaxUint32,
		maxHeight:  -1,
		lastAccess: time.Unix(0, 0),
	}
	pq.SetLessFn(func(ai, aj *CachedAddress) bool {
		return ai.accessTime < aj.accessTime
	})
	return pq
}

// Satisfy heap.Inferface

// Len is require for heap.Interface
func (pq AddressPriorityQueue) Len() int {
	return len(pq.ah)
}

// Less performs the comparison priority(i) < priority(j). Use
// AddressPriorityQueue.SetLessFn to define the desired behavior for the
// CachedAddresses heap[i] and heap[j].
func (pq AddressPriorityQueue) Less(i, j int) bool {
	return pq.lessFn(pq.ah[i], pq.ah[j])
}

// Swap swaps the CachedAddresses at i and j. This is used container/heap.
func (pq AddressPriorityQueue) Swap(i, j int) {
	pq.ah[i], pq.ah[j] = pq.ah[j], pq.ah[i]
	pq.ah[i].heapIdx = i
	pq.ah[j].heapIdx = j
}

// SetLessFn sets the function called by Less. The input lessFn must accept two
// *CachedAddress and return a bool, unlike Less, which accepts heap indexes i, j.
// This allows to define a comparator without requiring a heap.
func (pq *AddressPriorityQueue) SetLessFn(lessFn func(bi, bj *CachedAddress) bool) {
	pq.Lock()
	defer pq.Unlock()
	pq.lessFn = lessFn
}

// newCachedAddress wraps the given address tx results in a CachedAddress with
// no accesses and an invalid heap index. Use Access to make it valid.
func newCachedAddress(currentBestBlock int64, request AddressSearchRequest,
	results AddressSearchResults) *CachedAddress {
	return &CachedAddress{
		search: &AddressSearch{
			AddedAtBlock: currentBestBlock,
			Request:      request,
			Results:      results,
		},
		heapIdx: -1,
	}
}

// Access increments the access count and sets the accessTime to now. The
// AddressSearch stored in the CachedAddress is returned.
func (b *CachedAddress) Access() *AddressSearch {
	b.accesses++
	b.accessTime = time.Now().UnixNano()
	return b.search
}

// String satisfies the Stringer interface.
func (b CachedAddress) String() string {
	return fmt.Sprintf(
		"{Address: %v, NumTx: %d Accesses: %d, Time: %d, Heap Index: %d}",
		b.search.Request.Address, len(b.search.Results),
		b.accesses, b.accessTime, b.heapIdx)
}

// Address returns the address represented by the CachedAddress
func (b *CachedAddress) Address() string {
	return b.search.Request.Address
}

// AddressSearchSaver is likely to be required to be implemented by the type
// utilizing Address.
type AddressSearchSaver interface {
	StoreAddressSearchResults(currentBestBlock int64,
		request AddressSearchRequest, results AddressSearchResults) (bool, error)
}

// Make sure AddressCache itself implements the methods of AddressSearchSaver
var _ AddressSearchSaver = (*AddressCache)(nil)

// Capacity returns the transaction capacity of the AddressCache
func (addrc *AddressCache) Capacity() uint32 { return addrc.capacity }

// UtilizationTransactions returns the number of address transactions stored in
// the cache.
func (addrc *AddressCache) UtilizationTransactions() int64 {
	addrc.RLock()
	defer addrc.RUnlock()
	var txCount int
	for _, txs := range addrc.addressCache {
		txCount += len(txs.search.Results)
	}
	return int64(txCount)
}

func (addrc *AddressCache) UtilizationPct() float64 {
	addrc.RLock()
	defer addrc.RUnlock()
	return 100.0 * float64(addrc.UtilizationTransactions()) / float64(addrc.capacity)
}

func (addrc *AddressCache) StoreAddressSearchResults(currentBestBlock int64,
	request AddressSearchRequest, results AddressSearchResults) (bool, error) {
	addrc.Lock()
	defer addrc.Unlock()

	// Check if we have anything on this address
	storedAddress, gotSomething := addrc.addressCache[request.Address]
	if !gotSomething {
		return addrc.tryInsertAddressSearch(currentBestBlock, request, results)
	}

	// We have this address. Do we update it?

	// TODO: this only considers length of results, nothing else
	if len(storedAddress.search.Results) <= len(results) {
		return addrc.tryInsertAddressSearch(currentBestBlock, request, results)
	}

	return false, nil
}

func (addrc *AddressCache) tryInsertAddressSearch(currentBestBlock int64,
	request AddressSearchRequest, results AddressSearchResults) (bool, error) {
	// Create fresh CachedAddress to insert into cache and expire queue
	cachedAddress := newCachedAddress(currentBestBlock, request, results)
	cachedAddress.Access()

	// Insert into queue and delete any cached address that was removed
	wasInserted, removedAddresses, err := addrc.expireQueue.Insert(cachedAddress)
	for i := range removedAddresses {
		addrc.addressCache[removedAddresses[i]] = nil
		delete(addrc.addressCache, removedAddresses[i])
	}
	if err != nil {
		return false, err
	}

	// Add new address to to the cache, if it went into the queue
	if wasInserted {
		addrc.addressCache[request.Address] = cachedAddress
	}
	return wasInserted, nil
}

// Hits returns the hit count of the AddressCache
func (addrc *AddressCache) Hits() uint64 { return addrc.hits }

// Misses returns the miss count of the AddressCache
func (addrc *AddressCache) Misses() uint64 { return addrc.misses }

func (addrc *AddressCache) GetCachedAddress(address string) *CachedAddress {
	addrc.Lock()
	defer addrc.Unlock()

	cachedAddress, ok := addrc.addressCache[address]
	if ok {
		cachedAddress.Access()
		addrc.expireQueue.setNeedsReheap(true)
		addrc.expireQueue.setAccessTime(time.Now())
		addrc.hits++
		return cachedAddress
	}
	addrc.misses++
	return nil
}

func (apq *AddressPriorityQueue) lastAccessTime() time.Time {
	apq.RLock()
	defer apq.RUnlock()
	return apq.lastAccess
}

func (apq *AddressPriorityQueue) setAccessTime(t time.Time) {
	apq.Lock()
	defer apq.Unlock()
	apq.lastAccess = t
}

func (apq *AddressPriorityQueue) doesNeedReheap() bool {
	apq.RLock()
	defer apq.RUnlock()
	return apq.needsReheap
}

func (bpq *AddressPriorityQueue) setNeedsReheap(needReheap bool) {
	bpq.Lock()
	defer bpq.Unlock()
	bpq.needsReheap = needReheap
}

// Push a *AddressSearch. Use heap.Push, not this directly.
func (pq *AddressPriorityQueue) Push(cachedAddress interface{}) {
	ca := cachedAddress.(*CachedAddress)
	ca.heapIdx = len(pq.ah)
	//pq.updateMinMax(b.summary.Height)
	pq.ah = append(pq.ah, ca)
	pq.lastAccess = time.Unix(0, ca.accessTime)
}

// Pop will return an interface{} that may be cast to *CachedAddress.  Use
// heap.Pop, not this.
func (pq *AddressPriorityQueue) Pop() interface{} {
	n := pq.Len()
	old := pq.ah
	block := old[n-1]
	block.heapIdx = -1
	pq.ah = old[0 : n-1]
	return block
}

// ResetHeap creates a fresh queue given the input []*CachedAddress. For every
// CachedAddress in the queue, ResetHeap resets the access count and time, and
// heap index. The min/max heights are reset, the heap is heapifies. NOTE: the
// input slice is modifed, but not reordered. A fresh slice is created for PQ
// internal use.
func (pq *AddressPriorityQueue) ResetHeap(ah []*CachedAddress) {
	pq.Lock()
	defer pq.Unlock()

	pq.maxHeight = -1
	pq.minHeight = math.MaxUint32
	now := time.Now().UnixNano()
	for i := range ah {
		//pq.updateMinMax(ah[i].summary.Height)
		ah[i].heapIdx = i
		ah[i].accesses = 1
		ah[i].accessTime = now
	}
	//pq.ah = ah
	pq.ah = make([]*CachedAddress, len(ah))
	copy(pq.ah, ah)
	// Do not call Reheap or setNeedsReheap unless you want a deadlock
	pq.needsReheap = false
	heap.Init(pq)
}

// Reheap is a shortcut for heap.Init(pq)
func (pq *AddressPriorityQueue) Reheap() {
	pq.Lock()
	defer pq.Unlock()

	pq.needsReheap = false
	heap.Init(pq)
}

func (pq *AddressPriorityQueue) TotalTxs() (totalTxs int) {
	for i := range pq.ah {
		totalTxs += len(pq.ah[i].search.Results)
	}
	return
}

func (pq *AddressPriorityQueue) FreeSpace() int {
	txCap := int(pq.capacity)
	totalTxs := pq.TotalTxs()
	return txCap - totalTxs
}

func (pq *AddressPriorityQueue) Cap() int {
	return int(pq.capacity)
}

// Insert will add an element, while respecting the queue's capacity
// if at capacity
// 		- compare with top and replace or return
// 		- if replaced top, heapdown (Fix(pq,0))
// else (not at capacity)
// 		- heap.Push, which is pq.Push (append at bottom) then heapup
func (pq *AddressPriorityQueue) Insert(cachedAddress *CachedAddress) (bool, []string, error) {
	pq.Lock()
	defer pq.Unlock()

	if pq.capacity == 0 {
		return false, nil, fmt.Errorf("priority queue capacity is zero")
	}

	// The number of transactions in the cached address to insert
	numTxToAdd := len(cachedAddress.search.Results)
	if numTxToAdd > pq.Cap() {
		return false, nil, fmt.Errorf("priority queue capacity insufficient for address search")
	}

	var removedAddresses []string
	for numTxToAdd > pq.FreeSpace() {
		removed := pq.RemoveNext()
		if removed == nil && numTxToAdd > pq.FreeSpace() {
			return false, removedAddresses, fmt.Errorf("priority queue is unable to free enough space")
		}
		removedAddresses = append(removedAddresses, removed.Address())
	}

	// With room to grow, append at bottom and bubble up
	heap.Push(pq, cachedAddress)
	// pq.RescanMinMaxForAdd(summary.Height) // no rescan, just set min/max
	pq.lastAccess = time.Now()

	return true, removedAddresses, nil
}

// Update will update the specified CachedAddress, which must be in the
// queue. This function is NOT thread-safe.
func (pq *AddressPriorityQueue) Update(addr *CachedAddress, addressSearch *AddressSearch) {
	if addr != nil {
		addr.search = addressSearch
		addr.accesses = 0
		addr.Access()
		heap.Fix(pq, addr.heapIdx)
		//pq.RescanMinMaxForUpdate(heightAdded, heightRemoved)
		pq.lastAccess = time.Unix(0, addr.accessTime)
	}
}

// Remove removes the specified CachedAddress from the queue. Remember to
// remove it from the actual address cache!
func (pq *AddressPriorityQueue) Remove(b *CachedAddress) {
	pq.Lock()
	defer pq.Unlock()

	if b != nil && b.heapIdx > 0 && b.heapIdx < pq.Len() {
		// only remove the block it it is really in the queue
		if pq.ah[b.heapIdx].Address() == b.Address() {
			pq.RemoveIndex(b.heapIdx)
			return
		}
		fmt.Printf("Tried to remove an address that was NOT in the PQ. "+
			"Address: %v", b.Address())
	}
}

// RemoveIndex removes the CachedAddress at the specified position in the heap.
// This function is NOT thread-safe.
func (pq *AddressPriorityQueue) RemoveIndex(idx int) *CachedAddress {
	if pq.Len() <= idx {
		return nil
	}
	//removedAddress := pq.ah[idx].Address()
	removed := heap.Remove(pq, idx).(*CachedAddress)
	//pq.RescanMinMaxForRemove(removedHeight)
	return removed
}

// RemoveNext is like RemoveIndex(0) except that it used heap.Pop instead of
// heap.Remove
func (pq *AddressPriorityQueue) RemoveNext() *CachedAddress {
	if pq.Len() == 0 {
		return nil
	}
	return heap.Pop(pq).(*CachedAddress)
}
