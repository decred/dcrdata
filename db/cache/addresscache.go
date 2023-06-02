// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

// Package cache provides a number of types and functions for caching Decred
// address data, and filtering AddressRow slices. The type AddressCache may
// store the following data for an address: balance (see
// db/dbtypes.AddressBalance), address table row data (see
// db/dbtypes.AddressRow), merged address table row data, UTXOs (see
// db/dbtypes.AddressTxnOutput), and "metrics" (see db/dbtypes.AddressMetrics).
package cache

import (
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/txhelpers"
)

const (
	// The size of a dbtypes.AddressTxnOutput varies since address and pkScript
	// lengths vary, but it is roughly 180 bytes (88 bytes for the struct and
	// ~92 bytes for the string buffers).
	approxTxnOutSize = 180
)

// CacheLock is a "try lock" for coordinating multiple accessors, while allowing
// only a single updater. Use NewCacheLock to create a CacheLock.
type CacheLock struct {
	mtx   sync.Mutex
	addrs map[string]chan struct{}
}

// NewCacheLock constructs a new CacheLock.
func NewCacheLock() *CacheLock {
	return &CacheLock{addrs: make(map[string]chan struct{})}
}

func (cl *CacheLock) done(addr string) {
	cl.mtx.Lock()
	delete(cl.addrs, addr)
	cl.mtx.Unlock()
}

func (cl *CacheLock) hold(addr string) func() {
	done := make(chan struct{})
	cl.addrs[addr] = done
	return func() {
		cl.done(addr)
		close(done)
	}
}

// TryLock will attempt to obtain an exclusive lock and a function to release
// the lock. If the lock is already held, the channel returned by TryLock will
// be closed when/if the holder of the lock calls the done function.
//
// Trylock returns a bool, busy, indicating if another caller has already
// obtained the lock. When busy is false, the caller has obtained the exclusive
// lock, and the returned func(), done, should be called when ready to release
// the lock. When busy is true, the returned channel, wait, should be received
// from to block until the updater has released the lock.
func (cl *CacheLock) TryLock(addr string) (busy bool, wait chan struct{}, done func()) {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	done = func() {}
	wait, busy = cl.addrs[addr]
	if !busy {
		done = cl.hold(addr)
	}
	return busy, wait, done
}

// CountCreditDebitRows returns the numbers of credit (funding) and debit
// (!funding) address rows in a []*dbtypes.AddressRow.
func CountCreditDebitRows(rows []*dbtypes.AddressRow) (numCredit, numDebit int) {
	return dbtypes.CountCreditDebitRows(rows)
}

// CountCreditDebitRowsCompact returns the numbers of credit (funding) and debit
// (!funding) address rows in a []dbtypes.AddressRowCompact.
func CountCreditDebitRowsCompact(rows []*dbtypes.AddressRowCompact) (numCredit, numDebit int) {
	for _, row := range rows {
		if row.IsFunding {
			numCredit++
		} else {
			numDebit++
		}
	}
	return
}

// CountUnspentCreditRowsCompact returns the numbers of credit (funding) which is unspent
// in a []dbtypes.AddressRowCompact.
func CountUnspentCreditRowsCompact(rows []*dbtypes.AddressRowCompact) (numCredit int) {
	for _, row := range rows {
		if row.IsFunding && txhelpers.IsZeroHash(row.MatchingTxHash) {
			numCredit++
		}
	}
	return
}

// CountCreditDebitRowsMerged returns the numbers of credit (funding) and debit
// (!funding) address rows in a []dbtypes.AddressRowMerged.
func CountCreditDebitRowsMerged(rows []*dbtypes.AddressRowMerged) (numCredit, numDebit int) {
	for _, row := range rows {
		if row.IsFunding() {
			numCredit++
		} else {
			numDebit++
		}
	}
	return
}

func addressRows(rows []*dbtypes.AddressRowCompact, N, offset int) []*dbtypes.AddressRowCompact {
	if rows == nil {
		return nil
	}
	numRows := len(rows)
	if offset >= numRows {
		return []*dbtypes.AddressRowCompact{}
	}

	end := offset + N
	if end > numRows {
		end = numRows
	}
	if offset < end {
		return rows[offset:end]
	}
	return []*dbtypes.AddressRowCompact{}
}

// CreditAddressRows returns up to N credit (funding) address rows from the
// given AddressRow slice, starting after skipping offset rows. The input rows
// may only be of type []dbtypes.AddressRowCompact or
// []dbtypes.AddressRowMerged. The same type is returned, unless the input type
// is unrecognized, in which case a nil interface is returned.
func CreditAddressRows(rows interface{}, N, offset int) interface{} {
	switch r := rows.(type) {
	case []*dbtypes.AddressRowCompact:
		return creditAddressRows(r, N, offset)
	case []*dbtypes.AddressRowMerged:
		return creditAddressRowsMerged(r, N, offset)
	default:
		return nil
	}
}

func creditAddressRows(rows []*dbtypes.AddressRowCompact, N, offset int) []*dbtypes.AddressRowCompact {
	if rows == nil {
		return nil
	}
	if offset >= len(rows) {
		return []*dbtypes.AddressRowCompact{}
	}

	// Count the number of IsFunding rows in the input slice.
	numCreditRows, _ := CountCreditDebitRowsCompact(rows)
	if numCreditRows < N {
		N = numCreditRows
	}
	if offset >= numCreditRows {
		return nil
	}

	var skipped int
	out := make([]*dbtypes.AddressRowCompact, 0, N)
	for _, row := range rows {
		if !row.IsFunding {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		// Append this row, and break the loop if we have N rows.
		out = append(out, row)
		if len(out) == N {
			break
		}
	}
	return out
}

func creditAddressRowsMerged(rows []*dbtypes.AddressRowMerged, N, offset int) []*dbtypes.AddressRowMerged {
	if rows == nil {
		return nil
	}
	if offset >= len(rows) {
		return []*dbtypes.AddressRowMerged{}
	}

	// Count the number of IsFunding() rows in the input slice.
	numCreditRows, _ := CountCreditDebitRowsMerged(rows)
	if numCreditRows < N {
		N = numCreditRows
	}
	if offset >= numCreditRows {
		return nil
	}

	var skipped int
	out := make([]*dbtypes.AddressRowMerged, 0, N)
	for _, row := range rows {
		if !row.IsFunding() {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		// Append this row, and break the loop if we have N rows.
		out = append(out, row)
		if len(out) == N {
			break
		}
	}
	return out
}

// DebitAddressRows returns up to N debit (!funding) address rows from the given
// AddressRow slice, starting after skipping offset rows. The input rows may
// only be of type []dbtypes.AddressRowCompact or []dbtypes.AddressRowMerged.
// The same type is returned, unless the input type is unrecognized, in which
// case a nil interface is returned.
func DebitAddressRows(rows interface{}, N, offset int) interface{} {
	switch r := rows.(type) {
	case []*dbtypes.AddressRowCompact:
		return debitAddressRows(r, N, offset)
	case []*dbtypes.AddressRowMerged:
		return debitAddressRowsMerged(r, N, offset)
	default:
		return nil
	}
}

func debitAddressRows(rows []*dbtypes.AddressRowCompact, N, offset int) []*dbtypes.AddressRowCompact {
	if rows == nil {
		return nil
	}
	if offset >= len(rows) {
		return []*dbtypes.AddressRowCompact{}
	}

	// Count the number of !IsFunding rows in the input slice.
	_, numDebitRows := CountCreditDebitRowsCompact(rows)
	if numDebitRows < N {
		N = numDebitRows
	}
	var skipped int
	out := make([]*dbtypes.AddressRowCompact, 0, N)
	for i := range rows {
		if rows[i].IsFunding {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		// Append this row, and break the loop if we have N rows.
		out = append(out, rows[i])
		if len(out) == N {
			break
		}
	}
	return out
}

func debitAddressRowsMerged(rows []*dbtypes.AddressRowMerged, N, offset int) []*dbtypes.AddressRowMerged {
	if rows == nil {
		return nil
	}
	if offset >= len(rows) {
		return []*dbtypes.AddressRowMerged{}
	}

	// Count the number of !IsFunding() rows in the input slice.
	_, numDebitRows := CountCreditDebitRowsMerged(rows)
	if numDebitRows < N {
		N = numDebitRows
	}
	var skipped int
	out := make([]*dbtypes.AddressRowMerged, 0, N)
	for _, row := range rows {
		if row.IsFunding() {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		// Append this row, and break the loop if we have N rows.
		out = append(out, row)
		if len(out) == N {
			break
		}
	}
	return out
}

func unspentCreditAddressRows(rows []*dbtypes.AddressRowCompact, N, offset int) []*dbtypes.AddressRowCompact {
	if rows == nil {
		return nil
	}
	if offset >= len(rows) {
		return []*dbtypes.AddressRowCompact{}
	}

	// Count the number of IsFunding rows in the input slice.
	numUnspentCreditRows := CountUnspentCreditRowsCompact(rows)

	if numUnspentCreditRows < N {
		N = numUnspentCreditRows
	}
	if offset >= numUnspentCreditRows {
		return nil
	}

	var skipped int
	out := make([]*dbtypes.AddressRowCompact, 0, N)
	for _, row := range rows {
		if !row.IsFunding || !txhelpers.IsZeroHash(row.MatchingTxHash) {
			continue
		}

		if skipped < offset {
			skipped++
			continue
		}
		// Append this row, and break the loop if we have N rows.
		out = append(out, row)
		if len(out) == N {
			break
		}
	}
	return out
}

// AllCreditAddressRows returns all of the credit (funding) address rows from
// the given AddressRow slice.
func AllCreditAddressRows(rows []*dbtypes.AddressRow) []*dbtypes.AddressRow {
	numCreditRows, _ := CountCreditDebitRows(rows)
	out := make([]*dbtypes.AddressRow, 0, numCreditRows)
	if numCreditRows == 0 {
		return out
	}
	for _, r := range rows {
		if r.IsFunding {
			out = append(out, r)
		}
	}
	return out
}

// AllDebitAddressRows returns all of the debit (!funding) address rows from the
// given AddressRow slice.
func AllDebitAddressRows(rows []*dbtypes.AddressRow) []*dbtypes.AddressRow {
	_, numDebitRows := CountCreditDebitRows(rows)
	out := make([]*dbtypes.AddressRow, 0, numDebitRows)
	if numDebitRows == 0 {
		return out
	}
	for _, r := range rows {
		if !r.IsFunding {
			out = append(out, r)
		}
	}
	return out
}

// TxHistory contains ChartsData for different chart types (tx type and amount
// flow), each with data at known time intervals (TimeBasedGrouping).
type TxHistory struct {
	TypeByInterval    [dbtypes.NumIntervals]*dbtypes.ChartsData
	AmtFlowByInterval [dbtypes.NumIntervals]*dbtypes.ChartsData
}

// Clear sets each *ChartsData to nil, effectively clearing the TxHistory.
func (th *TxHistory) Clear() {
	for i := 0; i < dbtypes.NumIntervals; i++ {
		th.TypeByInterval[i] = nil
		th.AmtFlowByInterval[i] = nil
	}
}

// AddressCacheItem is the unit of cached data pertaining to a certain address.
// The height and hash of the best block at the time the data was obtained is
// stored to determine validity of the cache item. Cached data for an address
// are: balance, all non-merged address table rows, all merged address table
// rows, all UTXOs, and address metrics.
type AddressCacheItem struct {
	mtx     sync.RWMutex
	balance *dbtypes.AddressBalance
	rows    []*dbtypes.AddressRowCompact // creditDebitQuery
	utxos   []*dbtypes.AddressTxnOutput
	history TxHistory
	// Block height and hash are intended to keep balance and rows consistent.
	// The utxos and charts data may be stored at a later block.
	height int64
	hash   chainhash.Hash
}

// BlockID provides basic identifying information about a block.
type BlockID struct {
	Hash   chainhash.Hash
	Height int64
}

// NewBlockID constructs a new BlockID.
func NewBlockID(hash *chainhash.Hash, height int64) *BlockID {
	return &BlockID{
		Hash:   *hash,
		Height: height,
	}
}

// blockID generates a BlockID for the AddressCacheItem.
func (d *AddressCacheItem) blockID() *BlockID {
	return &BlockID{d.hash, d.height}
}

// BlockHash is a thread-safe accessor for the block hash.
func (d *AddressCacheItem) BlockHash() chainhash.Hash {
	d.mtx.RLock()
	defer d.mtx.RUnlock()
	return d.hash
}

// BlockHeight is a thread-safe accessor for the block height.
func (d *AddressCacheItem) BlockHeight() int64 {
	d.mtx.RLock()
	defer d.mtx.RUnlock()
	return d.height
}

// Balance is a thread-safe accessor for the *dbtypes.AddressBalance.
func (d *AddressCacheItem) Balance() (*dbtypes.AddressBalance, *BlockID) {
	d.mtx.RLock()
	defer d.mtx.RUnlock()
	if d.balance == nil {
		return nil, nil
	}
	return d.balance, d.blockID()
}

// UTXOs is a thread-safe accessor for the []*dbtypes.AddressTxnOutput.
func (d *AddressCacheItem) UTXOs() ([]*dbtypes.AddressTxnOutput, *BlockID) {
	d.mtx.RLock()
	defer d.mtx.RUnlock()
	if d.utxos == nil {
		return nil, nil
	}
	return d.utxos, d.blockID()
}

// HistoryChart is a thread-safe accessor for the TxHistory.
func (d *AddressCacheItem) HistoryChart(addrChart dbtypes.HistoryChart, chartGrouping dbtypes.TimeBasedGrouping) (*dbtypes.ChartsData, *BlockID) {
	d.mtx.RLock()
	defer d.mtx.RUnlock()

	if int(chartGrouping) >= dbtypes.NumIntervals {
		log.Errorf("Invalid chart grouping: %v", chartGrouping)
		return nil, nil
	}

	var cd *dbtypes.ChartsData
	switch addrChart {
	case dbtypes.TxsType:
		cd = d.history.TypeByInterval[chartGrouping]
	case dbtypes.AmountFlow:
		cd = d.history.AmtFlowByInterval[chartGrouping]
	}

	if cd == nil {
		return nil, nil
	}
	return cd, d.blockID()
}

// Rows is a thread-safe accessor for the []dbtypes.AddressRowCompact.
func (d *AddressCacheItem) Rows() ([]*dbtypes.AddressRowCompact, *BlockID) {
	d.mtx.RLock()
	defer d.mtx.RUnlock()
	if d.rows == nil {
		return nil, nil
	}
	return d.rows, d.blockID()
}

// NumRows returns the number of non-merged rows. If the rows are not cached, a
// count of -1 and *BlockID of nil are returned.
func (d *AddressCacheItem) NumRows() (int, *BlockID) {
	d.mtx.RLock()
	defer d.mtx.RUnlock()
	if d.rows == nil {
		return -1, nil
	}
	return len(d.rows), d.blockID()
}

// Transactions attempts to retrieve transaction data for the given view (merged
// or not, debit/credit/all). Like the DB queries, the number of transactions to
// retrieve, N, and the number of transactions to skip, offset, are also
// specified.
func (d *AddressCacheItem) Transactions(N, offset int, txnView dbtypes.AddrTxnViewType) (interface{}, *BlockID, error) {
	if offset < 0 || N < 0 {
		return nil, nil, fmt.Errorf("invalid offset (%d) or N (%d)", offset, N)
	}

	if d == nil {
		return nil, nil, fmt.Errorf("uninitialized AddressCacheItem")
	}

	d.mtx.RLock()
	defer d.mtx.RUnlock()
	merged, err := txnView.IsMerged()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid transaction view: %v", txnView)
	}

	// Identify cache miss by nil rows.
	if d.rows == nil {
		// Cache miss is not an error, but rows return type must be consistent
		// with requested view for the sanity checking in AddressCache
		// (TransactionsCompact or TransactionsMerged).
		if merged {
			return []*dbtypes.AddressRowMerged(nil), nil, nil
		}
		return []*dbtypes.AddressRowCompact(nil), nil, nil
	}

	blockID := d.blockID()
	numRows := len(d.rows)

	if N == 0 || numRows == 0 || offset >= numRows {
		// Not a cache miss, just no requested or matching data.
		if merged {
			return []*dbtypes.AddressRowMerged{}, blockID, nil
		}
		return []*dbtypes.AddressRowCompact{}, blockID, nil
	}

	switch txnView {
	case dbtypes.AddrTxnAll:
		// []*dbtypes.AddressRowCompact
		return addressRows(d.rows, N, offset), blockID, nil
	case dbtypes.AddrTxnCredit:
		return creditAddressRows(d.rows, N, offset), blockID, nil
	case dbtypes.AddrTxnDebit:
		return debitAddressRows(d.rows, N, offset), blockID, nil
	case dbtypes.AddrMergedTxn, dbtypes.AddrMergedTxnCredit, dbtypes.AddrMergedTxnDebit:
		// []*dbtypes.AddressRowMerged
		return dbtypes.MergeRowsCompactRange(d.rows, N, offset, txnView), blockID, nil
	case dbtypes.AddrUnspentTxn:
		return unspentCreditAddressRows(d.rows, N, offset), blockID, nil
	default:
		// This should already be caught by IsMerged err check.
		return nil, nil, fmt.Errorf("unrecognized address transaction view: %v", txnView)
	}
}

// setBlock ensures that the AddressCacheItem pertains to the given BlockID,
// clearing any cached data if the previously set block is not equal to the
// given block.
func (d *AddressCacheItem) setBlock(block BlockID) {
	if block.Hash == d.hash {
		return
	}
	d.hash = block.Hash
	d.height = block.Height
	d.utxos = nil
	d.history.Clear()
	d.balance = nil
	d.rows = nil
}

// SetRows updates the cache item for the given non-merged AddressRow slice
// valid at the given BlockID.
func (d *AddressCacheItem) SetRows(block BlockID, rows []*dbtypes.AddressRowCompact) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.setBlock(block)
	d.rows = rows
}

// SetUTXOs updates the cache item for the given *AddressTxnOutput slice valid
// at the given BlockID.
func (d *AddressCacheItem) SetUTXOs(block BlockID, utxos []*dbtypes.AddressTxnOutput) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.setBlock(block)
	d.utxos = utxos
}

// SetBalance updates the cache item for the given AddressBalance valid at the
// given BlockID.
func (d *AddressCacheItem) SetBalance(block BlockID, balance *dbtypes.AddressBalance) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.setBlock(block)
	d.balance = balance
}

// cacheCounts stores cache hits and misses.
type cacheCounts struct {
	sync.Mutex
	hits, misses int
}

// cacheMetrics is a collection of CacheCounts for the various cached data.
type cacheMetrics struct {
	rowMetrics     cacheCounts
	utxoMetrics    cacheCounts
	balanceMetrics cacheCounts
	historyMetrics cacheCounts
}

func (cm *cacheMetrics) rowStats() (hits, misses int) {
	cm.rowMetrics.Lock()
	defer cm.rowMetrics.Unlock()
	return cm.rowMetrics.hits, cm.rowMetrics.misses
}

func (cm *cacheMetrics) balanceStats() (hits, misses int) {
	cm.balanceMetrics.Lock()
	defer cm.balanceMetrics.Unlock()
	return cm.balanceMetrics.hits, cm.balanceMetrics.misses
}

func (cm *cacheMetrics) utxoStats() (hits, misses int) {
	cm.utxoMetrics.Lock()
	defer cm.utxoMetrics.Unlock()
	return cm.utxoMetrics.hits, cm.utxoMetrics.misses
}

func (cm *cacheMetrics) historyStats() (hits, misses int) {
	cm.historyMetrics.Lock()
	defer cm.historyMetrics.Unlock()
	return cm.historyMetrics.hits, cm.historyMetrics.misses
}

func (cm *cacheMetrics) rowHit() {
	cm.rowMetrics.Lock()
	cm.rowMetrics.hits++
	cm.rowMetrics.Unlock()
}

func (cm *cacheMetrics) rowMiss() {
	cm.rowMetrics.Lock()
	cm.rowMetrics.misses++
	cm.rowMetrics.Unlock()
}

func (cm *cacheMetrics) utxoHit() {
	cm.utxoMetrics.Lock()
	cm.utxoMetrics.hits++
	cm.utxoMetrics.Unlock()
}

func (cm *cacheMetrics) utxoMiss() {
	cm.utxoMetrics.Lock()
	cm.utxoMetrics.misses++
	cm.utxoMetrics.Unlock()
}

func (cm *cacheMetrics) balanceHit() {
	cm.balanceMetrics.Lock()
	cm.balanceMetrics.hits++
	cm.balanceMetrics.Unlock()
}

func (cm *cacheMetrics) balanceMiss() {
	cm.balanceMetrics.Lock()
	cm.balanceMetrics.misses++
	cm.balanceMetrics.Unlock()
}

func (cm *cacheMetrics) historyHit() {
	cm.historyMetrics.Lock()
	cm.historyMetrics.hits++
	cm.historyMetrics.Unlock()
}

func (cm *cacheMetrics) historyMiss() {
	cm.historyMetrics.Lock()
	cm.historyMetrics.misses++
	cm.historyMetrics.Unlock()
}

// AddressCache maintains a store of address data. Use NewAddressCache to create
// a new AddressCache with initialized internal data structures.
type AddressCache struct {
	mtx     sync.RWMutex
	a       map[string]*AddressCacheItem
	cap     int
	capAddr int
	// Unlike addresses and address rows, which are counted precisely, UTXO
	// limits are enforced per-address. maxUTXOsPerAddr is computed on
	// construction from the specified total utxo capacity specified in bytes.
	maxUTXOsPerAddr int
	cacheMetrics    cacheMetrics
	ProjectAddress  string
}

// NewAddressCache constructs an AddressCache with capacity for the specified
// number of address rows. rowCapacity is an absolute limit on the number of
// address data table rows that may have cached data, while addressCapacity is a
// limit on the number of unique addresses in the cache, regardless of the
// number of rows. utxoCapacityBytes is the capacity in bytes of the UTXO cache.
func NewAddressCache(rowCapacity, addressCapacity, utxoCapacityBytes int) *AddressCache {
	var maxUTXOsPerAddr int
	if addressCapacity > 0 {
		maxUTXOsPerAddr = utxoCapacityBytes / approxTxnOutSize / addressCapacity
	}
	ac := &AddressCache{
		a:               make(map[string]*AddressCacheItem),
		cap:             rowCapacity,
		capAddr:         addressCapacity,
		maxUTXOsPerAddr: maxUTXOsPerAddr,
	}
	log.Debugf("Allowing %d cached UTXOs per address (max %d addresses), using ~%.0f MiB.",
		ac.maxUTXOsPerAddr, addressCapacity, float64(utxoCapacityBytes)/1024/1024)
	defer func() { go ac.Reporter() }()
	return ac
}

// BalanceStats reports the balance hit/miss stats.
func (ac *AddressCache) BalanceStats() (hits, misses int) {
	return ac.cacheMetrics.balanceStats()
}

// RowStats reports the row hit/miss stats.
func (ac *AddressCache) RowStats() (hits, misses int) {
	return ac.cacheMetrics.rowStats()
}

// UtxoStats reports the utxo hit/miss stats.
func (ac *AddressCache) UtxoStats() (hits, misses int) {
	return ac.cacheMetrics.utxoStats()
}

// HistoryStats reports the history data hit/miss stats.
func (ac *AddressCache) HistoryStats() (hits, misses int) {
	return ac.cacheMetrics.historyStats()
}

// Reporter prints the number of cached addresses, rows, and utxos, as well as a
// table of cache hits and misses.
func (ac *AddressCache) Reporter() {
	var lastBH, lastBM, lastRH, lastRM, lastUH, lastUM, lastHH, lastHM int
	ticker := time.NewTicker(4 * time.Second)
	for range ticker.C {
		balHits, balMisses := ac.BalanceStats()
		rowHits, rowMisses := ac.RowStats()
		utxoHits, utxoMisses := ac.UtxoStats()
		histHits, histMisses := ac.HistoryStats()
		// Only report if a hit/miss count has changed.
		if rowHits != lastRH || rowMisses != lastRM ||
			balHits != lastBH || balMisses != lastBM ||
			utxoHits != lastUH || utxoMisses != lastUM ||
			histHits != lastHH || histMisses != lastHM {
			lastBH, lastBM = balHits, balMisses
			lastRH, lastRM = rowHits, rowMisses
			lastUH, lastUM = utxoHits, utxoMisses
			lastHH, lastHM = histHits, histMisses
			numAddrs, numRows, numUTXOs := ac.Length()
			log.Debugf("ADDRESS CACHE: addresses = %d, rows = %d, utxos = %d",
				numAddrs, numRows, numUTXOs)
			log.Debugf("ADDRESS CACHE:"+
				"\n\t\t\t\t              HITS |    MISSES"+
				"\n\t\t\t\trows    %10d | %9d"+
				"\n\t\t\t\tbalance %10d | %9d"+
				"\n\t\t\t\tutxos   %10d | %9d"+
				"\n\t\t\t\thist    %10d | %9d",
				rowHits, rowMisses, balHits, balMisses,
				utxoHits, utxoMisses, histHits, histMisses)
		}
	}
}

// addressCacheItem safely accesses any AddressCacheItem for the given address.
func (ac *AddressCache) addressCacheItem(addr string) *AddressCacheItem {
	ac.mtx.RLock()
	defer ac.mtx.RUnlock()
	return ac.a[addr]
}

// ClearAll resets AddressCache, purging all cached data.
func (ac *AddressCache) ClearAll() (numCleared int) {
	ac.mtx.Lock()
	defer ac.mtx.Unlock()
	numCleared = len(ac.a)
	ac.a = make(map[string]*AddressCacheItem)
	return
}

// Clear purging cached data for the given addresses. If addrs is nil, all data
// are cleared. If addresses is non-nil empty slice, no data are cleared.
func (ac *AddressCache) Clear(addrs []string) (numCleared int) {
	if addrs == nil {
		return ac.ClearAll()
	}
	if len(addrs) == 0 {
		return
	}
	ac.mtx.Lock()
	defer ac.mtx.Unlock()
	for i := range addrs {
		if _, found := ac.a[addrs[i]]; !found {
			continue
		}
		delete(ac.a, addrs[i])
		numCleared++
	}
	return
}

// Balance attempts to retrieve an AddressBalance for the given address. The
// BlockID for the block at which the cached data is valid is also returned. In
// the event of a cache miss, both returned pointers will be nil.
func (ac *AddressCache) Balance(addr string) (*dbtypes.AddressBalance, *BlockID) {
	aci := ac.addressCacheItem(addr)
	if aci == nil {
		ac.cacheMetrics.balanceMiss()
		return nil, nil
	}
	ac.cacheMetrics.balanceHit()
	balance, blockID := aci.Balance()
	var bal *dbtypes.AddressBalance
	if balance != nil {
		bal = new(dbtypes.AddressBalance)
		*bal = *balance // copy balance struct, blockID is already new
	}
	return bal, blockID
}

// UTXOs attempts to retrieve an []*AddressTxnOutput for the given address. The
// BlockID for the block at which the cached data is valid is also returned. In
// the event of a cache miss, the slice and the *BlockID will be nil.
func (ac *AddressCache) UTXOs(addr string) ([]*dbtypes.AddressTxnOutput, *BlockID) {
	aci := ac.addressCacheItem(addr)
	if aci == nil {
		ac.cacheMetrics.utxoMiss()
		return nil, nil
	}
	ac.cacheMetrics.utxoHit()
	return aci.UTXOs()
}

// HistoryChart attempts to retrieve ChartsData for the given address, chart
// type, and grouping interval. The BlockID for the block at which the cached
// data is valid is also returned. In the event of a cache miss, both returned
// pointers will be nil.
func (ac *AddressCache) HistoryChart(addr string, addrChart dbtypes.HistoryChart,
	chartGrouping dbtypes.TimeBasedGrouping) (*dbtypes.ChartsData, *BlockID) {
	aci := ac.addressCacheItem(addr)
	if aci == nil {
		ac.cacheMetrics.historyMiss()
		return nil, nil
	}

	cd, blockID := aci.HistoryChart(addrChart, chartGrouping)
	if cd == nil || blockID == nil {
		ac.cacheMetrics.historyMiss()
		return nil, nil
	}

	ac.cacheMetrics.historyHit()
	return cd, blockID
}

// Rows attempts to retrieve an []*AddressRow for the given address. The BlockID
// for the block at which the cached data is valid is also returned. In the
// event of a cache miss, the slice and the *BlockID will be nil.
func (ac *AddressCache) Rows(addr string) ([]*dbtypes.AddressRowCompact, *BlockID) {
	aci := ac.addressCacheItem(addr)
	if aci == nil {
		ac.cacheMetrics.rowMiss()
		return nil, nil
	}
	ac.cacheMetrics.rowHit()
	return aci.Rows()
}

// NumRows returns the number of non-merged rows. If the rows are not cached, a
// count of -1 and *BlockID of nil are returned.
func (ac *AddressCache) NumRows(addr string) (int, *BlockID) {
	aci := ac.addressCacheItem(addr)
	if aci == nil {
		return -1, nil
	}
	return aci.NumRows()
}

// Transactions attempts to retrieve transaction data for the given address and
// view (merged or not, debit/credit/all). Like the DB queries, the number of
// transactions to retrieve, N, and the number of transactions to skip, offset,
// are also specified.
func (ac *AddressCache) Transactions(addr string, N, offset int64, txnType dbtypes.AddrTxnViewType) ([]*dbtypes.AddressRow, *BlockID, error) {
	merged, err := txnType.IsMerged()
	if err != nil {
		return nil, nil, err
	}

	if merged {
		rowsMerged, blockID, err := ac.TransactionsMerged(addr, N, offset, txnType)
		rows := dbtypes.UncompactMergedRows(rowsMerged)
		return rows, blockID, err
	}

	rowsCompact, blockID, err := ac.TransactionsCompact(addr, N, offset, txnType)
	rows := dbtypes.UncompactRows(rowsCompact)
	return rows, blockID, err
}

// TransactionsMerged is like Transactions, but it must be used with a merged
// AddrTxnViewType, and it returns a []dbtypes.AddressRowMerged. A cache miss is
// indicated by (*BlockID)==nil. The returned rows may be nil or an empty slice
// for a cache hit if the address has no history.
func (ac *AddressCache) TransactionsMerged(addr string, N, offset int64, txnType dbtypes.AddrTxnViewType) ([]*dbtypes.AddressRowMerged, *BlockID, error) {
	aci := ac.addressCacheItem(addr)
	if aci == nil {
		ac.cacheMetrics.rowMiss()
		return nil, nil, nil // cache miss is not an error; *BlockID must be nil
	}
	ac.cacheMetrics.rowHit()

	rows, blockID, err := aci.Transactions(int(N), int(offset), txnType)
	if err != nil {
		return nil, nil, err
	}

	switch r := rows.(type) {
	case []*dbtypes.AddressRowMerged:
		return r, blockID, err
	default:
		return nil, nil, fmt.Errorf(`TransactionsMerged(%s, N=%d, offset=%d, view="%s") failed to return []dbtypes.AddressRowMerged`,
			addr, N, offset, txnType.String())
	}
}

// TransactionsCompact is like Transactions, but it must be used with a
// non-merged AddrTxnViewType, and it returns a []dbtypes.AddressRowCompact. A
// cache miss is indicated by (*BlockID)==nil. The returned rows may be nil or
// an empty slice for a cache hit if the address has no history.
func (ac *AddressCache) TransactionsCompact(addr string, N, offset int64, txnType dbtypes.AddrTxnViewType) ([]*dbtypes.AddressRowCompact, *BlockID, error) {
	aci := ac.addressCacheItem(addr)
	if aci == nil {
		ac.cacheMetrics.rowMiss()
		return nil, nil, nil // cache miss is not an error; *BlockID must be nil
	}
	ac.cacheMetrics.rowHit()

	rows, blockID, err := aci.Transactions(int(N), int(offset), txnType)
	if err != nil {
		return nil, nil, err
	}

	switch r := rows.(type) {
	case []*dbtypes.AddressRowCompact:
		return r, blockID, err
	default:
		return nil, nil, fmt.Errorf(`TransactionsCompact(%s, N=%d, offset=%d, view="%s") failed to return []dbtypes.AddressRowCompact`,
			addr, N, offset, txnType.String())
	}
}

func (ac *AddressCache) length() (numAddrs, numTxns, numUTXOs int) {
	numAddrs = len(ac.a)
	for _, aci := range ac.a {
		numTxns += len(aci.rows)
		numUTXOs += len(aci.utxos)
	}
	return
}

// Length returns the total number of address rows and UTXOs stored in cache.
func (ac *AddressCache) Length() (numAddrs, numTxns, numUTXOs int) {
	ac.mtx.RLock()
	defer ac.mtx.RUnlock()
	return ac.length()
}

// NumAddresses returns the total number of addresses in the cache.
func (ac *AddressCache) NumAddresses() int {
	ac.mtx.RLock()
	defer ac.mtx.RUnlock()
	return len(ac.a)
}

func (ac *AddressCache) purgeRowsToFit(numRows int) (haveSpace bool) {
	if ac.cap < 1 || ac.capAddr < 1 {
		return false
	}

	// First purge to meet address capacity when adding 1 new address.
	addrsCached := len(ac.a)
clearingaddrs:
	for addrsCached >= ac.capAddr {
		for a := range ac.a {
			// Never purge the data for the project fund address.
			if a == ac.ProjectAddress {
				if len(ac.a) == 1 {
					break clearingaddrs
				}
				continue
			}
			delete(ac.a, a)
			break // recheck addrsCached
		}
		addrsCached = len(ac.a)
	}

	// If the cache is at or above row capacity, remove cache items to make room
	// for the given number of rows.
	addrsCached, cacheSize, _ := ac.length()
clearing:
	for cacheSize > 0 && cacheSize+numRows > ac.cap {
		for a, aaci := range ac.a {
			// nothing much to clear for this cached item
			if len(aaci.rows) == 0 {
				continue
			}
			// Never purge the data for the project fund address.
			if a == ac.ProjectAddress {
				if len(ac.a) == 1 {
					break clearing
				}
				continue
			}
			delete(ac.a, a)
			break // recheck cacheSize
		}
		addrsCached, cacheSize, _ = ac.length()
	}

	return cacheSize+numRows <= ac.cap && addrsCached < ac.capAddr // addrsCached+1 <= ac.capAddr
}

func (ac *AddressCache) addCacheItem(addr string, aci *AddressCacheItem) (success bool) {
	if ac.cap < 1 || ac.capAddr < 1 {
		return false
	}

	// We will overwrite any existing AddressCacheItem, so an existing item with
	// rows set exists, account for these rows that would be removed.
	var alreadyStored int
	aci0 := ac.a[addr]
	if aci0 != nil {
		alreadyStored = len(aci0.rows)
	}
	haveSpace := ac.purgeRowsToFit(len(aci.rows) - alreadyStored)
	if haveSpace {
		ac.a[addr] = aci
		log.Tracef("Added new AddressCacheItem: %s", addr)
		success = true
	} else {
		log.Debugf("No space in cache to add item with %d rows for %s!\n", len(aci.rows), addr)
	}
	return
}

func (ac *AddressCache) setCacheItemRows(addr string, rows []*dbtypes.AddressRowCompact, block *BlockID) (updated bool) {
	if block == nil || ac.cap < 1 || ac.capAddr < 1 {
		return false
	}

	aci := ac.a[addr]
	// Keep rows consistent with height/hash.
	if aci == nil || aci.BlockHash() != block.Hash {
		return ac.addCacheItem(addr, &AddressCacheItem{
			rows:   rows,
			height: block.Height,
			hash:   block.Hash,
		})
	}

	aci.mtx.Lock()
	defer aci.mtx.Unlock()
	// If rows is already set for this same block, there should be no need to
	// even check the length, but confirm it is the same to be safe. If so,
	// there is no need to save the same rows slice. This is a successful set.
	alreadyStored := len(aci.rows)
	if aci.rows != nil && alreadyStored == len(rows) {
		updated = true
		return
	}

	// Try to clear space from the cache for these rows.
	haveSpace := ac.purgeRowsToFit(len(rows) - alreadyStored)
	if haveSpace {
		aci.rows = rows
		updated = true
	} else {
		log.Debugf("No space in cache to set %d rows for %s!\n", len(rows), addr)
	}
	return
}

// StoreRows stores the non-merged AddressRow slice for the given address in
// cache. The current best block data is required to determine cache freshness.
func (ac *AddressCache) StoreRows(addr string, rows []*dbtypes.AddressRow, block *BlockID) bool {
	if block == nil || ac.cap < 1 || ac.capAddr < 1 {
		return false
	}

	rowsCompact := []*dbtypes.AddressRowCompact{}
	if rows != nil {
		rowsCompact = dbtypes.CompactRows(rows)
	}

	// respect cache capacity
	return ac.StoreRowsCompact(addr, rowsCompact, block)
}

// StoreHistoryChart stores the charts data for the given address in cache. The
// current best block data is required to determine cache freshness.
func (ac *AddressCache) StoreHistoryChart(addr string, addrChart dbtypes.HistoryChart,
	chartGrouping dbtypes.TimeBasedGrouping, cd *dbtypes.ChartsData, block *BlockID) bool {
	if block == nil || ac.cap < 1 || ac.capAddr < 1 {
		return false
	}

	if int(chartGrouping) >= dbtypes.NumIntervals {
		log.Errorf("Invalid chart grouping: %v", chartGrouping)
		return false
	}

	if cd == nil {
		cd = &dbtypes.ChartsData{}
	}

	ac.mtx.Lock()
	defer ac.mtx.Unlock()
	aci := ac.a[addr]

	// Don't evict existing balance/rows cache on account of block mismatch.
	if aci == nil /* || aci.BlockHash() != block.Hash */ {
		aci = &AddressCacheItem{
			height: block.Height,
			hash:   block.Hash,
		}
		if !ac.addCacheItem(addr, aci) {
			return false
		}
	}

	// Set the history data in the cache item.
	aci.mtx.Lock()
	defer aci.mtx.Unlock()

	switch addrChart {
	case dbtypes.TxsType:
		aci.history.TypeByInterval[chartGrouping] = cd
	case dbtypes.AmountFlow:
		aci.history.AmtFlowByInterval[chartGrouping] = cd
	default:
		return false
	}

	return true
}

// StoreRowsCompact stores the non-merged AddressRow slice for the given address
// in cache. The current best block data is required to determine cache
// freshness.
func (ac *AddressCache) StoreRowsCompact(addr string, rows []*dbtypes.AddressRowCompact, block *BlockID) bool {
	if block == nil || ac.cap < 1 || ac.capAddr < 1 {
		return false
	}

	ac.mtx.Lock()
	defer ac.mtx.Unlock()

	if rows == nil {
		rows = []*dbtypes.AddressRowCompact{}
	}

	// respect cache capacity
	return ac.setCacheItemRows(addr, rows, block)
}

// StoreBalance stores the AddressBalance for the given address in cache. The
// current best block data is required to determine cache freshness.
func (ac *AddressCache) StoreBalance(addr string, balance *dbtypes.AddressBalance, block *BlockID) bool {
	if block == nil || ac.cap < 1 || ac.capAddr < 1 {
		return false
	}

	ac.mtx.Lock()
	defer ac.mtx.Unlock()
	aci := ac.a[addr]

	var bal dbtypes.AddressBalance
	if balance == nil {
		bal.Address = addr
	} else {
		bal = *balance
	}

	// Keep balance consistent with height/hash.
	if aci == nil || aci.BlockHash() != block.Hash {
		return ac.addCacheItem(addr, &AddressCacheItem{
			balance: &bal,
			height:  block.Height,
			hash:    block.Hash,
		})
	}

	// cache is current, so just set the balance.
	aci.mtx.Lock()
	aci.balance = &bal
	aci.mtx.Unlock()
	return true
}

// StoreUTXOs stores the *AddressTxnOutput slice for the given address in cache.
// The current best block data is required to determine cache freshness.
func (ac *AddressCache) StoreUTXOs(addr string, utxos []*dbtypes.AddressTxnOutput, block *BlockID) bool {
	if block == nil || ac.cap < 1 || ac.capAddr < 1 {
		return false
	}

	// Only allow storing maxUTXOsPerAddr.
	if len(utxos) > ac.maxUTXOsPerAddr && addr != ac.ProjectAddress {
		return false
	}

	ac.mtx.Lock()
	defer ac.mtx.Unlock()
	aci := ac.a[addr]

	if utxos == nil {
		utxos = []*dbtypes.AddressTxnOutput{}
	}

	// Don't evict existing balance/rows cache on account of block mismatch.
	if aci == nil /* || aci.BlockHash() != block.Hash */ {
		return ac.addCacheItem(addr, &AddressCacheItem{
			utxos:  utxos,
			height: block.Height,
			hash:   block.Hash,
		})
	}

	// cache is current, so just set the utxos.
	aci.mtx.Lock()
	aci.utxos = utxos
	aci.mtx.Unlock()
	return true
}

// ClearUTXOs clears any stored UTXOs for the given address in cache.
func (ac *AddressCache) ClearUTXOs(addr string) {
	ac.mtx.Lock()
	defer ac.mtx.Unlock()
	aci := ac.a[addr]
	if aci == nil {
		return
	}

	// AddressCacheItem found. Clear utxos.
	aci.mtx.Lock()
	aci.utxos = nil
	aci.mtx.Unlock()
}

// ClearRows clears any stored address rows for the given address in cache.
func (ac *AddressCache) ClearRows(addr string) {
	ac.mtx.Lock()
	defer ac.mtx.Unlock()
	aci := ac.a[addr]
	if aci == nil {
		return
	}

	// AddressCacheItem found. Clear rows.
	aci.mtx.Lock()
	aci.rows = nil
	aci.mtx.Unlock()
}
