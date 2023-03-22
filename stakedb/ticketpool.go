// Copyright (c) 2018-2022, The Decred developers
// Copyright (c) 2018, The dcrdata developers
// See LICENSE for details.

package stakedb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/slog"
	bv1 "github.com/dgraph-io/badger"
	"github.com/dgraph-io/badger/v3"
)

// TicketPool contains the live ticket pool diffs (tickets in/out) between
// adjacent block heights in a chain. Diffs are applied in sequence by inserting
// and removing ticket hashes from a pool, represented as a map. A []PoolDiff
// stores these diffs, with a cursor pointing to the next unapplied diff. An
// on-disk database of diffs is maintained using the badger database.
type TicketPool struct {
	mtx    sync.RWMutex
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

type badgerLogger struct {
	slog.Logger
}

type logLevel int

const (
	logLevelSquash logLevel = iota
	logLevelTrace
	logLevelDebug
	logLevelInfo
	logLevelWarn
	logLevelError
)

var logLevelOverrides = map[string]logLevel{
	"Replaying file id:":                logLevelTrace,
	"Replay took:":                      logLevelTrace,
	"Storing value log head:":           logLevelTrace,
	"Force compaction on level":         logLevelTrace,
	"Value log discard stats":           logLevelTrace,
	"Got compaction priority":           logLevelDebug,
	"Compaction for level":              logLevelDebug,
	"Running for level":                 logLevelTrace,
	"While forcing compaction on level": logLevelDebug,
}

// logf is used to filter the log messages from badger. It does the following:
// removes trailing newlines, overrides the log level if the message is
// recognized in the logLevelOverrides map, and then adds the prefix "badger: ".
// If there are no log level overrides for the message, the provided
// defaultLevel is used.
func (l *badgerLogger) logf(defaultLevel logLevel, format string, v ...interface{}) {
	// Badger randomly appends newlines. Strip them.
	format = strings.TrimSuffix(format, "\n")

	// Generate the log message for filtering.
	message := fmt.Sprintf(format, v...)

	// Check each known message prefix for a logLevel override.
	level := defaultLevel
	for substr, lvl := range logLevelOverrides {
		if strings.HasPrefix(message, substr) { // consider Contains
			level = lvl
			break
		}
	}

	message = "badger: " + message

	switch level {
	case logLevelSquash:
		// Do not log these messages.
	case logLevelTrace:
		l.Logger.Tracef(message)
	case logLevelDebug:
		l.Logger.Debugf(message)
	case logLevelInfo:
		l.Logger.Infof(message)
	case logLevelWarn:
		l.Logger.Warnf(message)
	case logLevelError:
		l.Logger.Errorf(message)
	default:
		// Unknown log levels are logged as warnings.
		l.Logger.Warnf(message)
	}
}

// Debugf filters messages through logf with logLevelDebug before sending the
// message to the slog.Logger.
func (l *badgerLogger) Debugf(format string, v ...interface{}) {
	l.logf(logLevelDebug, format, v...)
}

// Infof filters messages through logf with logLevelInfo before sending the
// message to the slog.Logger.
func (l *badgerLogger) Infof(format string, v ...interface{}) {
	l.logf(logLevelInfo, format, v...)
}

// Warningf filters messages through logf with logLevelWarn before sending the
// message to the slog.Logger.
func (l *badgerLogger) Warningf(format string, v ...interface{}) {
	l.logf(logLevelWarn, format, v...)
}

// Errorf filters messages through logf with logLevelError before sending the
// message to the slog.Logger.
func (l *badgerLogger) Errorf(format string, v ...interface{}) {
	l.logf(logLevelError, format, v...)
}

var versionKey = []byte("version")

const currentVersion uint32 = 2

func setVersion(db *badger.DB) error {
	var verB [4]byte
	binary.BigEndian.PutUint32(verB[:], currentVersion)
	return db.Update(func(txn *badger.Txn) error {
		return txn.SetEntry(badger.NewEntry(versionKey, verB[:]).WithMeta(1))
	})
}

func dbVersion(db *badger.DB) (uint32, error) {
	var ver uint32
	return ver, db.View(func(txn *badger.Txn) error {
		verItem, err := txn.Get(versionKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				log.Debug("Detected v0 pool db with no version key.")
				return nil // ver = 0
			}
			return err
		}

		if sz := verItem.ValueSize(); sz != 4 {
			return fmt.Errorf("invalid version value size %d", sz)
		}

		return verItem.Value(func(v []byte) error {
			ver = binary.BigEndian.Uint32(v) // should not be 0
			return nil
		})
	})
}

func badgerOptions(badgerDbPath string) badger.Options {
	opts := badger.DefaultOptions(badgerDbPath)
	opts.DetectConflicts = false
	opts.BlockCacheSize = 300 << 20
	opts.CompactL0OnClose = true
	opts.MetricsEnabled = false
	opts.Logger = &badgerLogger{log}
	return opts
}

// NewTicketPool constructs a TicketPool by opening the persistent diff db,
// loading all known diffs, initializing the TicketPool values.
func NewTicketPool(dataDir, dbSubDir string) (tp *TicketPool, err error) {
	// Open ticket pool diffs database
	badgerDbPath := filepath.Join(dataDir, dbSubDir)
	opts := badgerOptions(badgerDbPath)
	db, err := badger.Open(opts)
	if err != nil {
		if !strings.Contains(err.Error(), "manifest has unsupported version") {
			return nil, fmt.Errorf("failed to open badger DB: %w", err)
		}

		// Upgrade database.
		if db, err = upgradeDB(badgerDbPath, opts, 0 /* attempt upgrade from v0*/); err != nil {
			return nil, fmt.Errorf("upgrade error: %w", err)
		}
	}

	defer func() {
		if r := recover(); r != nil {
			db.Close()
			panic(r)
		}
		if err != nil {
			db.Close()
		}
	}()

	// Detect a new DB by looking at the tables count.
	newDB := len(db.Tables()) == 0
	if newDB {
		log.Debugf("Creating new pool DB version %d", currentVersion)
		if err = setVersion(db); err != nil {
			return nil, err
		}
	}

	// Check the DB version, 0 if not set.
	ver, err := dbVersion(db)
	if err != nil {
		return nil, err
	}
	if ver > currentVersion {
		return nil, fmt.Errorf("unsupported db version %d", ver)
	}

	if ver != currentVersion {
		if db, err = upgradeDB(badgerDbPath, opts, ver); err != nil {
			return nil, fmt.Errorf("upgrade error: %w", err)
		}
	}

	// Attempt garbage collection of badger value log. If greater than
	// rewriteThreshold of the space was discarded, rewrite the entire value
	// log. However, there should be few discards as chain reorgs that cause
	// data to be deleted are a small percentage of the ticket pool data.
	rewriteThreshold := 0.5
	err = db.RunValueLogGC(rewriteThreshold)
	if err != nil {
		if err != badger.ErrNoRewrite {
			return nil, fmt.Errorf("failed badger.RunValueLogGC: %w", err)
		}
		log.Debugf("badger value log not rewritten (OK).")
	}

	// Load all diffs.
	log.Infof("Loading all ticket pool diffs from DB version %d...", ver)
	poolDiffs, _, err := loadAllPoolDiffs(db)
	if err != nil {
		return nil, fmt.Errorf("failed loadAllPoolDiffs: %v", err)
	}
	log.Infof("Successfully loaded %d ticket pool diffs", len(poolDiffs))

	// Construct TicketPool with loaded diffs and diff DB
	return &TicketPool{
		pool:   make(map[chainhash.Hash]struct{}),
		diffs:  poolDiffs,
		tip:    int64(len(poolDiffs)), // number of blocks connected over genesis
		diffDB: db,
	}, nil
}

// loadAllPoolDiffs loads all found ticket pool diffs from badger DB.
func loadAllPoolDiffs(db *badger.DB) ([]PoolDiff, []uint64, error) {
	var poolDiffs []PoolDiff
	var heights []uint64
	err := db.View(func(txn *badger.Txn) error {
		// Create the badger iterator
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 1000
		it := txn.NewIterator(opts)
		defer it.Close()

		var lastheight uint64
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			if item.UserMeta() == 1 /* item is db version */ {
				continue
			}
			height := binary.BigEndian.Uint64(item.Key())

			var poolDiff *PoolDiff
			var err error
			err = item.Value(func(v []byte) error {
				poolDiff, err = decodeDiff(v)
				if err != nil {
					return fmt.Errorf("decodeDiff error: %w", err)
				}
				poolDiffs = append(poolDiffs, *poolDiff)
				return nil
			})
			if err != nil {
				return fmt.Errorf("key [%x / %d]. Error while fetching pool diff [%v]",
					item.Key(), height, err)
			}

			if lastheight+1 != height {
				panic(fmt.Sprintf("height: %d, lastheight: %d", height, lastheight))
			}
			lastheight = height
			heights = append(heights, height)
		}

		// Extra sanity check.
		poolDiffLen := uint64(len(poolDiffs))
		if poolDiffLen > 0 && poolDiffLen != lastheight {
			panic(fmt.Sprintf("last poolDiff Height (%d) != %d", lastheight, poolDiffLen))
		}

		return nil
	})

	return poolDiffs, heights, err
}

// upgradeDB upgrades a ticket pool database starting from the specified version
// and returns the upgraded database.
func upgradeDB(dbPath string, opts badger.Options, version uint32) (*badger.DB, error) {
	for ver, upgrade := range upgrades[version:] {
		var newVersion = uint32(ver) + 1
		if newVersion > currentVersion {
			newVersion = currentVersion
		}

		log.Infof("Upgrading ticket pool DB to version %d...", newVersion)
		err := upgrade(dbPath)
		if err != nil {
			return nil, fmt.Errorf("error upgrading from version %d: %w", ver, err)
		}
	}

	log.Infof("Ticket pool DB has been successfully upgraded to version %d", currentVersion)

	return badger.Open(opts)
}

// Close closes the persistent diff DB.
func (tp *TicketPool) Close() error {
	return tp.diffDB.Close()
}

// Tip returns the current length of the diffs slice.
func (tp *TicketPool) Tip() int64 {
	tp.mtx.RLock()
	defer tp.mtx.RUnlock()
	return tp.tip
}

// Cursor returns the current cursor, the location of the next unapplied diff.
func (tp *TicketPool) Cursor() int64 {
	tp.mtx.RLock()
	defer tp.mtx.RUnlock()
	return tp.cursor
}

// append grows the diffs slice and advances the tip height.
func (tp *TicketPool) append(diff *PoolDiff) {
	tp.tip++
	tp.diffs = append(tp.diffs, *diff)
}

// trim is the non-thread-safe version of Trim. Use under tp.mtx lock.
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
	tp.mtx.Lock()
	defer tp.mtx.Unlock()
	return tp.trim()
}

func uint64Bytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// encodeDiff serializes a PoolDiff.
func encodeDiff(diff *PoolDiff) []byte {
	numIn, numOut := len(diff.In), len(diff.Out)
	dataLen := 16 + chainhash.HashSize*(numIn+numOut)

	dataBuff := bytes.NewBuffer(make([]byte, 0, dataLen))
	dataBuff.Write(uint64Bytes(uint64(numIn))) // +8
	for i := range diff.In {
		dataBuff.Write(diff.In[i][:]) // +HashSize
	}
	dataBuff.Write(uint64Bytes(uint64(numOut)))
	for i := range diff.Out {
		dataBuff.Write(diff.Out[i][:])
	}

	if dataBuff.Len() != dataLen {
		panic(fmt.Sprintf("wrong data length encoded, got %v, wanted %v", dataBuff.Len(), dataLen))
	}

	return dataBuff.Bytes()
}

// decodeDiff decodes a pool diff bytes.
func decodeDiff(diffBytes []byte) (*PoolDiff, error) {
	var hashReader bytes.Reader
	hashReader.Reset(diffBytes)

	// Number of hashes in.
	var inLenBytes [8]byte
	n, err := hashReader.Read(inLenBytes[:])
	if err != nil {
		return nil, fmt.Errorf("failed to ready In length: %w", err)
	}
	if n != 8 {
		return nil, fmt.Errorf("failed to ready 8-bytes of In length, got %d", n)
	}
	inLen := binary.BigEndian.Uint64(inLenBytes[:])

	// Number of hashes out, skipping over the input hashes themselves.
	var outLenBytes [8]byte
	n, err = hashReader.ReadAt(outLenBytes[:], 8+int64(inLen)*chainhash.HashSize)
	if err != nil {
		return nil, fmt.Errorf("failed to ready Out length: %w", err)
	}
	if n != 8 {
		return nil, fmt.Errorf("failed to ready 8-bytes of Out length, got %d", n)
	}
	outLen := binary.BigEndian.Uint64(outLenBytes[:])

	poolDiff := PoolDiff{
		In:  make([]chainhash.Hash, inLen),
		Out: make([]chainhash.Hash, outLen),
	}

	for i := range poolDiff.In {
		n, err = hashReader.Read(poolDiff.In[i][:])
		if err != nil {
			return nil, fmt.Errorf("failed to ready In[%d] Hash: %w", i, err)
		}
		if n != chainhash.HashSize {
			return nil, fmt.Errorf("failed to ready %-bytes of In Hash, got %d", chainhash.HashSize, n)
		}
	}

	// hashReader.Read(outLenBytes[:]) // read and discard outLen again
	_, err = hashReader.Seek(8, io.SeekCurrent) // skip over outLenBytes
	if err != nil {
		return nil, err
	}
	for i := range poolDiff.Out {
		n, err = hashReader.Read(poolDiff.Out[i][:])
		if err != nil {
			return nil, fmt.Errorf("failed to ready Out[%d] Hash: %w", i, err)
		}
		if n != chainhash.HashSize {
			return nil, fmt.Errorf("failed to ready %-bytes of Out Hash, got %d", chainhash.HashSize, n)
		}
	}

	remaining := hashReader.Len()
	if remaining != 0 {
		return nil, fmt.Errorf("not at the end of the value; %d left", remaining)
	}

	return &poolDiff, nil
}

// storeDiffTx stores the input diff for the specified height in the on-disk DB.
func storeDiffTx(txn *bv1.Txn, diff *PoolDiff, height uint64) error {
	heightBytes := uint64Bytes(height)
	diffData := encodeDiff(diff)
	return txn.Set(heightBytes, diffData)
}

// storeDiff stores the input diff for the specified height in the on-disk DB.
func storeDiff(db *badger.DB, diff *PoolDiff, height uint64) error {
	heightBytes := uint64Bytes(height)
	diffData := encodeDiff(diff)
	return db.Update(func(txn *badger.Txn) error {
		return txn.Set(heightBytes, diffData)
	})
}

func (tp *TicketPool) storeDiff(diff *PoolDiff, height int64) error {
	return storeDiff(tp.diffDB, diff, uint64(height))
}

// Append grows the diffs slice with the specified diff, and stores it in the
// on-disk DB. The height of the diff is used to check that it builds on the
// chain tip, and as a primary key in the DB.
func (tp *TicketPool) Append(diff *PoolDiff, height int64) error {
	if height != tp.tip+1 {
		return fmt.Errorf("block height %d does not build on %d", height, tp.tip)
	}
	tp.mtx.Lock()
	defer tp.mtx.Unlock()
	tp.append(diff)
	return tp.storeDiff(diff, height)
}

// AppendAndAdvancePool functions like Append, except that after growing the
// diffs slice and storing the diff in DB, the ticket pool is advanced.
func (tp *TicketPool) AppendAndAdvancePool(diff *PoolDiff, height int64) error {
	if height != tp.tip+1 {
		return fmt.Errorf("block height %d does not build on %d", height, tp.tip)
	}
	tp.mtx.Lock()
	defer tp.mtx.Unlock()
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
	tp.mtx.RLock()
	defer tp.mtx.RUnlock()
	return tp.currentPool()
}

// CurrentPoolSize returns the number of tickets stored in the current pool map.
func (tp *TicketPool) CurrentPoolSize() int {
	tp.mtx.RLock()
	defer tp.mtx.RUnlock()
	return len(tp.pool)
}

// Pool attempts to get the tickets in the live pool at the specified height. It
// will advance/retreat the cursor as needed to reach the desired height, and
// then extract the tickets from the resulting pool map.
func (tp *TicketPool) Pool(height int64) ([]chainhash.Hash, error) {
	tp.mtx.Lock()
	defer tp.mtx.Unlock()

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
	tp.mtx.Lock()
	defer tp.mtx.Unlock()
	err := tp.advanceTo(tp.tip)
	return tp.cursor, err
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

// retreatTo successively applies pool diffs in reverse with retreat until the
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
