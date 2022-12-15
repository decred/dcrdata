package stakedb

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	bv1 "github.com/dgraph-io/badger"
	"github.com/dgraph-io/badger/v3"
)

type upgradeFunc func(string) error

// Each database upgrade function should be keyed by the database
// version it upgrades.
var upgrades = []upgradeFunc{
	// v0 => v1, rewrite db using custom encoding.
	v1Upgrade,
	// v1 => v2, upgrades db to new badger v3.
	v2Upgrade,
}

func v1DBVersion(db *bv1.DB) (uint32, error) {
	var ver uint32
	return ver, db.View(func(txn *bv1.Txn) error {
		verItem, err := txn.Get(versionKey)
		if err != nil {
			if errors.Is(err, bv1.ErrKeyNotFound) {
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

// canUpgrade checks if a v1 badger database can upgrade.
func canUpgrade(db *bv1.DB, expectedVer uint32) (bool, error) {
	ver, err := v1DBVersion(db)
	if err != nil {
		return false, fmt.Errorf("canUpgrade error: %w", err)
	}

	if ver > currentVersion {
		return false, fmt.Errorf("unknown database version %d, "+
			"ticket pool db recognizes up to %d", ver, currentVersion)
	}

	if ver == expectedVer {
		return true, nil
	}

	return false, nil
}

// openV1DB opens a v1 badger database.
func openV1DB(dbPath string) (*bv1.DB, error) {
	opts := bv1.DefaultOptions(dbPath).
		WithValueDir(dbPath).
		WithNumVersionsToKeep(math.MaxUint32).
		WithLogger(&badgerLogger{log})
	db, err := bv1.Open(opts)
	if err == bv1.ErrTruncateNeeded {
		// Try again with value log truncation enabled.
		opts.Truncate = true
		log.Warnf("Attempting to reopening ticket pool db with the Truncate option set...")
		db, err = bv1.Open(opts)
	}
	if err != nil {
		return nil, fmt.Errorf("failed bv1.Open: %v", err)
	}
	return db, nil
}

func loadAllPoolDiffsV0(db *bv1.DB) ([]PoolDiff, []uint64, error) {
	var poolDiffs []PoolDiff
	var heights []uint64
	err := db.View(func(txn *bv1.Txn) error {
		// Create the badger iterator
		opts := bv1.DefaultIteratorOptions
		opts.PrefetchSize = 1000
		it := txn.NewIterator(opts)
		defer it.Close()

		var lastheight uint64
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			height := binary.BigEndian.Uint64(item.Key())

			// Don't waste time with a copy since we are going to read the data in
			// this transaction.
			var hashReader bytes.Reader
			errTx := item.Value(func(v []byte) error {
				hashReader.Reset(v)
				return nil
			})
			if errTx != nil {
				return fmt.Errorf("key [%x / %d]. Error while fetching value [%v]",
					item.Key(), height, errTx)
			}

			var poolDiff PoolDiff
			if errTx = gob.NewDecoder(&hashReader).Decode(&poolDiff); errTx != nil {
				log.Errorf("failed to decode PoolDiff[%d]: %v", height, errTx)
				return errTx
			}

			poolDiffs = append(poolDiffs, poolDiff)
			if lastheight+1 != height {
				panic(fmt.Sprintf("height: %d, lastheight: %d", height, lastheight))
			}
			lastheight = height
			heights = append(heights, height)
		}
		// extra sanity check
		poolDiffLen := uint64(len(poolDiffs))
		if poolDiffLen > 0 {
			if poolDiffLen != lastheight {
				panic(fmt.Sprintf("last poolDiff Height (%d) != %d", lastheight, poolDiffLen))
			}
		}

		return nil
	})
	return poolDiffs, heights, err
}

// rewriteDB drops all entries, sets the DB version, and stores the input
// diffs for the specified heights in the on-disk DB.
func rewriteDB(db *bv1.DB, diffs []PoolDiff, heights []uint64, ver uint32) error {
	err := db.DropAll()
	if err != nil {
		return err
	}

	txn := db.NewTransaction(true)

	var verB [4]byte
	binary.BigEndian.PutUint32(verB[:], ver)
	err = txn.SetEntry(bv1.NewEntry(versionKey, verB[:]).WithMeta(1))
	if err != nil {
		txn.Discard()
		return err
	}

	for i, h := range heights {
		err := storeDiffTx(txn, &diffs[i], h)
		// If this transaction got too big, commit and make a new one.
		if errors.Is(err, bv1.ErrTxnTooBig) {
			if err = txn.Commit(); err != nil {
				txn.Discard()
				return err
			}
			log.Infof("Starting new transaction for pool diff %d", i)
			txn = db.NewTransaction(true)
			if err = storeDiffTx(txn, &diffs[i], h); err != nil {
				txn.Discard()
				return err
			}
		}
		if err != nil {
			txn.Discard()
			return err
		}
	}

	return txn.Commit()
}

// v1Upgrade upgrade a ticket pool db from v0 to v1.
func v1Upgrade(dbPath string) error {
	const oldVersion = 0
	// Open the DB.
	db, err := openV1DB(dbPath)
	if err != nil {
		return fmt.Errorf("openV1DB error: %w", err)
	}
	defer db.Close()

	ok, err := canUpgrade(db, oldVersion)
	if err != nil {
		return err
	}

	if !ok {
		// No upgrades necessary.
		return nil
	}

	// Rewrite DB because v0 DB used a different encoding for ticket pool diffs.
	poolDiffs, heights, err := loadAllPoolDiffsV0(db)
	if err != nil {
		return fmt.Errorf("loadAllPoolDiffsV0 error: %w", err)
	}

	return rewriteDB(db, poolDiffs, heights, 1)
}

// v2Upgrade upgrade a ticket pool db from v1 to v2.
func v2Upgrade(dbPath string) error {
	const oldVersion = 1

	// Open ticket pool diffs DB with old badger version.
	v1DB, err := openV1DB(dbPath)
	if err != nil {
		return fmt.Errorf("openV1DB error: %w", err)
	}
	defer v1DB.Close()

	ok, err := canUpgrade(v1DB, oldVersion)
	if err != nil {
		return err
	}

	if !ok {
		// No upgrades necessary.
		return nil
	}

	// Split joined DB path because input args can be anything.
	root, base := filepath.Split(dbPath)
	backupPath := filepath.Join(root, base+"-bak")
	log.Infof("Backing up ticket pool DB to %v before upgrading. This may take a while...", backupPath)

	backupFile, err := os.Create(backupPath)
	if err != nil {
		return err
	}

	bw := bufio.NewWriterSize(backupFile, 64<<20)
	if _, err = v1DB.Backup(bw, 0); err != nil {
		return err
	}

	if err = bw.Flush(); err != nil {
		return err
	}

	if err = backupFile.Close(); err != nil {
		return fmt.Errorf("backupFile error: %w", err)
	}

	log.Infof("Your ticket pool DB backup was successful.")

	// Delete the old ticket pool DB.
	if err := os.RemoveAll(dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete db file: %w", err)
	}

	opts := badger.DefaultOptions(dbPath)
	opts.MetricsEnabled = false
	opts.Logger = &badgerLogger{log}
	db, err := badger.Open(opts)
	if err != nil {
		return err
	}
	defer db.Close()

	backupFile, err = os.Open(backupPath)
	if err != nil {
		return err
	}

	err = db.Load(backupFile, 100)
	if err != nil {
		return fmt.Errorf("db.Load error: %w", err)
	}

	// Set db version to current version.
	if err = setVersion(db); err != nil {
		return err
	}

	log.Infof("Upgrade completed. You may delete the old ticket pool DB backup file at (%s).", backupPath)
	return nil
}
