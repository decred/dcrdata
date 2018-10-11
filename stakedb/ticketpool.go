// Copyright (c) 2018, The Decred developers
// Copyright (c) 2018, The dcrdata developers
// See LICENSE for details.

package stakedb

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/dgraph-io/badger"
)

// TicketPool contains the live ticket pool diffs (tickets in/out) between
// adjacent block heights in a chain. Diffs are applied in sequence by inserting
// and removing ticket hashes from a pool, represented as a map. A []PoolDiff
// stores these diffs, with a cursor pointing to the next unapplied diff. An
// on-disk database of diffs is maintained using the badger database.
type TicketPool struct {
	*sync.RWMutex
	cursor int64
	tip    int64
	diffs  []PoolDiff
	pool   map[chainhash.Hash]struct{}
	diffDB *badger.DB
}

// PoolDiff represents the tickets going in and out of the live ticket pool from
// one height to the next.
type PoolDiff struct {
	In  []chainhash.Hash
	Out []chainhash.Hash
}

// PoolDiffDBItem is the type in the live ticket DB. The primary key (id) is
// Height.
type PoolDiffDBItem struct {
	Height   int64 `storm:"id"`
	PoolDiff `storm:"inline"`
}

// NewTicketPool constructs a TicketPool by opening the persistent diff db,
// loading all known diffs, initializing the TicketPool values.
func NewTicketPool(dataDir, dbSubDir string) (*TicketPool, error) {
	// Open ticket pool diffs database
	badgerDbPath := filepath.Join(dataDir, dbSubDir)
	opts := badger.DefaultOptions
	opts.Dir = badgerDbPath
	opts.ValueDir = badgerDbPath
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed badger.Open: %v", err)
	}

	// Attempt migration from storm to badger if badger was empty
	TableInfo := db.Tables()
	oldDBPath := filepath.Join(dataDir, DefaultTicketPoolDbName)
	if len(TableInfo) == 0 {
		migrated, err := MigrateFromStorm(oldDBPath, db)
		if err != nil {
			return nil, fmt.Errorf("migration from storm failed: %v", err)
		}
		if migrated {
			log.Info("Successfully migrated ticket pool db from storm to badger DB.")
		}
	}

	if _, err = os.Stat(oldDBPath); err == nil {
		log.Infof("You may delete the old ticket pool DB file (%s).", oldDBPath)
	}

	// Load all diffs
	log.Infof("Loading all ticket pool diffs...")
	var poolDiffs []PoolDiffDBItem
	poolDiffs, err = LoadAllPoolDiffs(db)
	if err != nil {
		return nil, fmt.Errorf("failed LoadAllPoolDiffs: %v", err)
	}

	if len(poolDiffs) > 0 {
		log.Debugf("len(poolDiffs)=%d", len(poolDiffs))
		endHeight := poolDiffs[len(poolDiffs)-1].Height
		log.Debugf("poolDiffs[0].Height=%d, poolDiffs[end].Height=%d",
			poolDiffs[0].Height, endHeight)
		if int64(len(poolDiffs)) != endHeight {
			panic(fmt.Sprintf("last poolDiff Height (%d) != %d", endHeight, len(poolDiffs)))
		}
	}

	diffs := make([]PoolDiff, len(poolDiffs))
	for i := range poolDiffs {
		diffs[i] = poolDiffs[i].PoolDiff
	}

	// Construct TicketPool with loaded diffs and diff DB
	return &TicketPool{
		RWMutex: new(sync.RWMutex),
		pool:    make(map[chainhash.Hash]struct{}),
		diffs:   diffs,             // make([]PoolDiff, 0, 100000)
		tip:     int64(len(diffs)), // number of blocks connected over genesis
		diffDB:  db,
	}, nil
}

// MigrateFromStorm attempts to load the storm DB specified by the given file
// name, and migrate all ticket pool diffs to the badger db.
func MigrateFromStorm(stormDBFile string, db *badger.DB) (bool, error) {
	// Check for the storm DB file
	finfo, err := os.Stat(stormDBFile)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if finfo.Size() == 0 {
		return false, nil
	}

	// Open the storm DB file
	dbOld, err := storm.Open(stormDBFile)
	if err != nil {
		return false, fmt.Errorf("failed storm.Open: %v", err)
	}
	defer dbOld.Close()

	// Attempt to load the pool diffs for block 1
	var blockOneDiffs PoolDiffDBItem
	err = dbOld.One("Height", 1, &blockOneDiffs)
	// If bucket or element with id 1 does not exist, not an error
	if err == storm.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to retrieve block one pool diff "+
			"(delete storm db file and try again): %v", err)
	}

	log.Info("Found storm ticket pool DB. Attempting migration to badger.")

	// Load all diffs from storm
	log.Info("Loading all items from storm db...")
	var poolDiffsItems []PoolDiffDBItem
	err = dbOld.AllByIndex("Height", &poolDiffsItems)
	if err != nil {
		return false, fmt.Errorf("failed (*storm.DB).AllByIndex: %v", err)
	}

	poolDiffs := make([]*PoolDiff, 0, len(poolDiffsItems))
	poolHeights := make([]int64, len(poolDiffsItems))
	for i := range poolDiffsItems {
		poolHeights[i] = poolDiffsItems[i].Height
		poolDiffs = append(poolDiffs, &poolDiffsItems[i].PoolDiff)
	}

	// Store all diffs in badger
	log.Info("Storing all items in badger db...")
	err = storeDiffs(db, poolDiffs, poolHeights)
	if err != nil {
		return false, fmt.Errorf("failed to store diff in badger: %v", err)
	}

	return true, nil
}

// LoadAllPoolDiffs loads all found ticket pool diffs from badger DB.
func LoadAllPoolDiffs(db *badger.DB) ([]PoolDiffDBItem, error) {
	var poolDiffs []PoolDiffDBItem
	err := db.View(func(txn *badger.Txn) error {
		// Create the badger iterator
		opts := badger.IteratorOptions{
			PrefetchValues: true,
			PrefetchSize:   1000,
			Reverse:        false,
			AllVersions:    false,
		}
		it := txn.NewIterator(opts)
		defer it.Close()

		var hashesBytes []byte
		var lastheight uint64
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			height := binary.BigEndian.Uint64(item.Key())

			var err error
			hashesBytes, err = item.ValueCopy(hashesBytes)
			if err != nil {
				log.Warnf("Key [%x]. Error while fetching value [%v]\n", item.Key(), err)
				continue
			}

			hashReader := bytes.NewReader(hashesBytes)

			var poolDiff PoolDiff
			if err = gob.NewDecoder(hashReader).Decode(&poolDiff); err != nil {
				log.Errorf("failed to decode PoolDiff[%d]: %v", height, hashesBytes)
				return err
			}

			poolDiffs = append(poolDiffs, PoolDiffDBItem{
				Height:   int64(height),
				PoolDiff: poolDiff,
			})

			if lastheight+1 != height {
				panic(fmt.Sprintf("height: %d, lastheight: %d", height, lastheight))
			}
			lastheight = height
		}
		return nil
	})
	return poolDiffs, err
}

// Close closes the persistent diff DB.
func (tp *TicketPool) Close() error {
	return tp.diffDB.Close()
}

// Tip returns the current length of the diffs slice.
func (tp *TicketPool) Tip() int64 {
	tp.RLock()
	defer tp.RUnlock()
	return tp.tip
}

// Cursor returns the current cursor, the location of the next unapplied diff.
func (tp *TicketPool) Cursor() int64 {
	tp.RLock()
	defer tp.RUnlock()
	return tp.cursor
}

// append grows the diffs slice and advances the tip height.
func (tp *TicketPool) append(diff *PoolDiff) {
	tp.tip++
	tp.diffs = append(tp.diffs, *diff)
}

// trim is the non-thread-safe version of Trim.
func (tp *TicketPool) trim() (int64, PoolDiff) {
	if tp.tip == 0 || len(tp.diffs) == 0 {
		return tp.tip, PoolDiff{}
	}
	tp.tip--
	newMaxCursor := tp.maxCursor()
	if tp.cursor > newMaxCursor {
		if err := tp.retreatTo(newMaxCursor); err != nil {
			log.Errorf("retreatTo failed: %v", err)
		}
	}
	// Trim AFTER retreating
	undo := tp.diffs[len(tp.diffs)-1]
	tp.diffs = tp.diffs[:len(tp.diffs)-1]
	return tp.tip, undo
}

// Trim removes the end diff and decrements the tip height. If the cursor would
// fall beyond the end of the diffs, the removed diffs are applied in reverse.
func (tp *TicketPool) Trim() (int64, PoolDiff) {
	tp.Lock()
	defer tp.Unlock()
	return tp.trim()
}

// storeDiff stores the input diff for the specified height in the on-disk DB.
func storeDiff(db *badger.DB, diff *PoolDiff, height int64) error {
	var heightBytes [8]byte
	binary.BigEndian.PutUint64(heightBytes[:], uint64(height))

	var poolDiffBuffer bytes.Buffer
	if err := gob.NewEncoder(&poolDiffBuffer).Encode(diff); err != nil {
		return err
	}

	return db.Update(func(txn *badger.Txn) error {
		return txn.Set(heightBytes[:], poolDiffBuffer.Bytes())
	})
}

// storeDiffs stores the input diffs for the specified heights in the on-disk DB.
func storeDiffs(db *badger.DB, diffs []*PoolDiff, heights []int64) error {
	heightToBytes := func(height int64) (heightBytes [8]byte) {
		binary.BigEndian.PutUint64(heightBytes[:], uint64(height))
		return
	}

	txn := db.NewTransaction(true)
	for i, h := range heights {
		heightBytes := heightToBytes(h)
		poolDiffBuffer := new(bytes.Buffer)
		gobEnc := gob.NewEncoder(poolDiffBuffer)
		err := gobEnc.Encode(diffs[i])
		if err != nil {
			txn.Discard()
			return err
		}
		err = txn.Set(heightBytes[:], poolDiffBuffer.Bytes())
		// If this transaction got too big, commit and make a new one
		if err == badger.ErrTxnTooBig {
			if err = txn.Commit(nil); err != nil {
				txn.Discard()
				return err
			}
			txn = db.NewTransaction(true)
			if err = txn.Set(heightBytes[:], poolDiffBuffer.Bytes()); err != nil {
				txn.Discard()
				return err
			}
		}
		if err != nil {
			txn.Discard()
			return err
		}
		//poolDiffBuffer.Reset()
	}
	return txn.Commit(nil)
}

func (tp *TicketPool) storeDiff(diff *PoolDiff, height int64) error {
	return storeDiff(tp.diffDB, diff, height)
}

// fetchDiff retrieves the diff at the specified height from the on-disk DB.
func (tp *TicketPool) fetchDiff(height int64) (*PoolDiffDBItem, error) {
	var heightBytes [8]byte
	binary.BigEndian.PutUint64(heightBytes[:], uint64(height))

	var diff *PoolDiffDBItem
	err := tp.diffDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get(heightBytes[:])
		if err != nil {
			return fmt.Errorf("failed to find height %d in TicketPool", height)
		}
		hashesBytes, err := item.Value()
		if err != nil {
			return fmt.Errorf("key [%x]. Error while fetching value [%v]", item.Key(), err)
		}

		hashReader := bytes.NewReader(hashesBytes)

		var poolDiff PoolDiff
		if err = gob.NewDecoder(hashReader).Decode(&poolDiff); err != nil {
			return fmt.Errorf("failed to decode PoolDiff")
		}

		diff = &PoolDiffDBItem{
			Height:   height,
			PoolDiff: poolDiff,
		}
		return nil
	})

	return diff, err
}

// Append grows the diffs slice with the specified diff, and stores it in the
// on-disk DB. The height of the diff is used to check that it builds on the
// chain tip, and as a primary key in the DB.
func (tp *TicketPool) Append(diff *PoolDiff, height int64) error {
	if height != tp.tip+1 {
		return fmt.Errorf("block height %d does not build on %d", height, tp.tip)
	}
	tp.Lock()
	defer tp.Unlock()
	tp.append(diff)
	return tp.storeDiff(diff, height)
}

// AppendAndAdvancePool functions like Append, except that after growing the
// diffs slice and storing the diff in DB, the ticket pool is advanced.
func (tp *TicketPool) AppendAndAdvancePool(diff *PoolDiff, height int64) error {
	if height != tp.tip+1 {
		return fmt.Errorf("block height %d does not build on %d", height, tp.tip)
	}
	tp.Lock()
	defer tp.Unlock()
	tp.append(diff)
	if err := tp.storeDiff(diff, height); err != nil {
		return err
	}
	return tp.advance()
}

// currentPool is the non-thread-safe version of CurrentPool.
func (tp *TicketPool) currentPool() ([]chainhash.Hash, int64) {
	poolSize := len(tp.pool)
	// allocate space for all the ticket hashes, but use append to avoid the
	// slice initialization having to zero initialize all of the arrays.
	pool := make([]chainhash.Hash, 0, poolSize)
	for h := range tp.pool {
		pool = append(pool, h)
	}
	return pool, tp.cursor
}

// CurrentPool gets the ticket hashes from the live ticket pool, and the current
// cursor (the height corresponding to the current pool). NOTE that the order of
// the ticket hashes is random as they are extracted from a the pool map with a
// range statement.
func (tp *TicketPool) CurrentPool() ([]chainhash.Hash, int64) {
	tp.RLock()
	defer tp.RUnlock()
	return tp.currentPool()
}

// CurrentPoolSize returns the number of tickets stored in the current pool map.
func (tp *TicketPool) CurrentPoolSize() int {
	tp.RLock()
	defer tp.RUnlock()
	return len(tp.pool)
}

// Pool attempts to get the tickets in the live pool at the specified height. It
// will advance/retreat the cursor as needed to reach the desired height, and
// then extract the tickets from the resulting pool map.
func (tp *TicketPool) Pool(height int64) ([]chainhash.Hash, error) {
	tp.Lock()
	defer tp.Unlock()

	if height > tp.tip {
		return nil, fmt.Errorf("block height %d is not connected yet, tip is %d", height, tp.tip)
	}

	for height > tp.cursor {
		if err := tp.advance(); err != nil {
			return nil, err
		}
	}
	for tp.cursor > height {
		if err := tp.retreat(); err != nil {
			return nil, err
		}
	}
	p, _ := tp.currentPool()
	return p, nil
}

// advance applies the pool diff at the current cursor location, and advances
// the cursor. Note that when advancing at the last diff, the resulting cursor
// will be beyond the last element in the diffs slice.
func (tp *TicketPool) advance() error {
	if tp.cursor > tp.maxCursor() {
		return fmt.Errorf("cursor at tip, unable to advance")
	}

	diffToNext := tp.diffs[tp.cursor]
	initPoolSize := len(tp.pool)
	expectedFinalSize := initPoolSize + len(diffToNext.In) - len(diffToNext.Out)

	tp.applyDiff(diffToNext.In, diffToNext.Out)
	tp.cursor++

	if len(tp.pool) != expectedFinalSize {
		return fmt.Errorf("pool size is %d, expected %d, at height %d",
			len(tp.pool), expectedFinalSize, tp.cursor-1)
	}

	return nil
}

// advanceTo successively applies pool diffs with advance until the cursor
// reaches the desired height. Note that this function will return without error
// if the initial cursor is at or beyond the specified height.
func (tp *TicketPool) advanceTo(height int64) error {
	if height > tp.tip {
		return fmt.Errorf("cannot advance past tip")
	}
	for height > tp.cursor {
		if err := tp.advance(); err != nil {
			return err
		}
	}
	return nil
}

// AdvanceToTip advances the pool map by applying all stored diffs. Note that
// the cursor will stop just beyond the last element of the diffs slice. It will
// not be possible to advance further, only retreat.
func (tp *TicketPool) AdvanceToTip() (int64, error) {
	tp.Lock()
	defer tp.Unlock()
	err := tp.advanceTo(tp.tip)
	return tp.cursor - 1, err
}

// retreat applies the previous diff in reverse, moving the pool map to the
// state before that diff was applied. The cursor is decremented, and may go to
// 0 but not beyond as the cursor is the location of the next unapplied diff.
func (tp *TicketPool) retreat() error {
	if tp.cursor == 0 {
		return fmt.Errorf("cursor at genesis, unable to retreat")
	}

	diffFromPrev := tp.diffs[tp.cursor-1]
	initPoolSize := len(tp.pool)
	expectedFinalSize := initPoolSize - len(diffFromPrev.In) + len(diffFromPrev.Out)

	tp.applyDiff(diffFromPrev.Out, diffFromPrev.In)
	tp.cursor--

	if len(tp.pool) != expectedFinalSize {
		return fmt.Errorf("pool size is %d, expected %d", len(tp.pool), expectedFinalSize)
	}
	return nil
}

// maxCursor returns the largest valid index into the diffs slice, or 0 when the
// slice is empty.
func (tp *TicketPool) maxCursor() int64 {
	if tp.tip == 0 {
		return 0
	}
	return tp.tip - 1
}

// retreatTo successively applies pool diffs in reverse with retreate until the
// cursor reaches the desired height. Note that this function will return
// without error if the initial cursor is at or below the specified height.
func (tp *TicketPool) retreatTo(height int64) error {
	if height < 0 || height > tp.tip {
		return fmt.Errorf("Invalid destination cursor %d", height)
	}
	for tp.cursor > height {
		if err := tp.retreat(); err != nil {
			return err
		}
	}
	return nil
}

// applyDiff adds and removes tickets from the pool map.
func (tp *TicketPool) applyDiff(in, out []chainhash.Hash) {
	initsize := len(tp.pool)
	for i := range in {
		tp.pool[in[i]] = struct{}{}
	}
	endsize := len(tp.pool)
	if endsize != initsize+len(in) {
		log.Warnf("pool grew by %d instead of %d", endsize-initsize, len(in))
	}
	initsize = endsize
	for i := range out {
		ii := len(tp.pool)
		delete(tp.pool, out[i])
		if len(tp.pool) == ii {
			log.Errorf("Failed to remove ticket %v from pool.", out[i])
		}
	}
	endsize = len(tp.pool)
	if endsize != initsize-len(out) {
		log.Warnf("pool shrank by %d instead of %d", initsize-endsize, len(out))
	}
}
