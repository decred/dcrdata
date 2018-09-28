// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"strings"

	"github.com/decred/dcrdata/v3/explorer"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/db/dcrpg/internal"
)

// indexingInfo defines a minimalistic structure used to append new indexes
// to be implemented with minimal code duplication.
type indexingInfo struct {
	Msg       string
	IndexFunc func(db *sql.DB) error
}

// deIndexingInfo defines a minimalistic structure used to append new deindexes
// to be implemented with minimal code duplication.
type deIndexingInfo struct {
	DeIndexFunc func(db *sql.DB) error
}

// Vins table indexes

func IndexVinTableOnVins(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVinTableOnVins)
	return
}

func IndexVinTableOnPrevOuts(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVinTableOnPrevOuts)
	return
}

func DeindexVinTableOnVins(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVinTableOnVins)
	return
}

func DeindexVinTableOnPrevOuts(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVinTableOnPrevOuts)
	return
}

// Transactions table indexes

func IndexTransactionTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTransactionTableOnHashes)
	return
}

func DeindexTransactionTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTransactionTableOnHashes)
	return
}

func IndexTransactionTableOnBlockIn(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTransactionTableOnBlockIn)
	return
}

func DeindexTransactionTableOnBlockIn(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTransactionTableOnBlockIn)
	return
}

// Blocks table indexes

func IndexBlockTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexBlockTableOnHash)
	return
}

func IndexBlockTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexBlocksTableOnHeight)
	return
}

func DeindexBlockTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlockTableOnHash)
	return
}

func DeindexBlockTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlocksTableOnHeight)
	return
}

// Vouts table indexes

func IndexVoutTableOnTxHashIdx(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVoutTableOnTxHashIdx)
	return
}

func DeindexVoutTableOnTxHashIdx(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVoutTableOnTxHashIdx)
	return
}

// Addresses table indexes
func IndexBlockTimeOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexBlockTimeOnTableAddress)
	return
}

func DeindexBlockTimeOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlockTimeOnTableAddress)
	return
}

func IndexMatchingTxHashOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexMatchingTxHashOnTableAddress)
	return
}

func DeindexMatchingTxHashOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexMatchingTxHashOnTableAddress)
	return
}

func IndexAddressTableOnAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnAddress)
	return
}

func DeindexAddressTableOnAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnAddress)
	return
}

func IndexAddressTableOnVoutID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnVoutID)
	return
}

func DeindexAddressTableOnVoutID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnVoutID)
	return
}

func IndexAddressTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnTxHash)
	return
}

func DeindexAddressTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnTxHash)
	return
}

// Votes table indexes

func IndexVotesTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnHashes)
	return
}

func DeindexVotesTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnHashes)
	return
}

func IndexVotesTableOnBlockHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnBlockHash)
	return
}

func DeindexVotesTableOnBlockHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnBlockHash)
	return
}

func IndexVotesTableOnCandidate(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnCandidate)
	return
}

func DeindexVotesTableOnCandidate(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnCandidate)
	return
}

func IndexVotesTableOnVoteVersion(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnVoteVersion)
	return
}

func DeindexVotesTableOnVoteVersion(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnVoteVersion)
	return
}

// Tickets table indexes

func IndexTicketsTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTicketsTableOnHashes)
	return
}

func DeindexTicketsTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTicketsTableOnHashes)
	return
}

func IndexTicketsTableOnTxDbID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTicketsTableOnTxDbID)
	return
}

func DeindexTicketsTableOnTxDbID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTicketsTableOnTxDbID)
	return
}

func IndexTicketsTableOnPoolStatus(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTicketsTableOnPoolStatus)
	return
}

func DeindexTicketsTableOnPoolStatus(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTicketsTableOnPoolStatus)
	return
}

// Missed votes table indexes

func IndexMissesTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexMissesTableOnHashes)
	return
}

func DeindexMissesTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexMissesTableOnHashes)
	return
}

// agendas

func IndexAgendasTableOnBlockTime(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAgendasTableOnBlockTime)
	return
}

func DeindexAgendasTableOnBlockTime(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAgendasTableOnBlockTime)
	return
}

func IndexAgendasTableOnAgendaID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAgendasTableOnAgendaID)
	return
}

func DeindexAgendasTableOnAgendaID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAgendasTableOnAgendaID)
	return
}

// Delete duplicates

func (pgb *ChainDB) DeleteDuplicateVins() (int64, error) {
	return DeleteDuplicateVins(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateVouts() (int64, error) {
	return DeleteDuplicateVouts(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateTxns() (int64, error) {
	return DeleteDuplicateTxns(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateTickets() (int64, error) {
	return DeleteDuplicateTickets(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateVotes() (int64, error) {
	return DeleteDuplicateVotes(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateMisses() (int64, error) {
	return DeleteDuplicateMisses(pgb.db)
}

// Indexes check
func (pgb *ChainDB) ExistsIndexVinOnVins() (bool, error) {
	return ExistsIndex(pgb.db, "uix_vin")
}

func (pgb *ChainDB) ExistsIndexVoutOnTxHashIdx() (bool, error) {
	return ExistsIndex(pgb.db, "uix_vout_txhash_ind")
}

func (pgb *ChainDB) ExistsIndexAddressesVoutIDAddress() (bool, error) {
	return ExistsIndex(pgb.db, "uix_addresses_vout_id")
}

// DeindexAll drops all of the indexes in all tables
func (pgb *ChainDB) DeindexAll() error {
	allDeIndexes := []deIndexingInfo{
		// blocks table
		deIndexingInfo{DeindexBlockTableOnHash},
		deIndexingInfo{DeindexBlockTableOnHeight},

		// transactions table
		deIndexingInfo{DeindexTransactionTableOnHashes},
		deIndexingInfo{DeindexTransactionTableOnBlockIn},

		// vins table
		deIndexingInfo{DeindexVinTableOnVins},
		deIndexingInfo{DeindexVinTableOnPrevOuts},

		// vouts table
		deIndexingInfo{DeindexVoutTableOnTxHashIdx},

		// addresses table
		deIndexingInfo{DeindexBlockTimeOnTableAddress},
		deIndexingInfo{DeindexMatchingTxHashOnTableAddress},
		deIndexingInfo{DeindexAddressTableOnAddress},
		deIndexingInfo{DeindexAddressTableOnVoutID},
		deIndexingInfo{DeindexAddressTableOnTxHash},

		// votes table
		deIndexingInfo{DeindexVotesTableOnCandidate},
		deIndexingInfo{DeindexVotesTableOnBlockHash},
		deIndexingInfo{DeindexVotesTableOnHash},
		deIndexingInfo{DeindexVotesTableOnVoteVersion},

		// misses table
		deIndexingInfo{DeindexMissesTableOnHash},

		// agendas table
		deIndexingInfo{DeindexAgendasTableOnBlockTime},
		deIndexingInfo{DeindexAgendasTableOnAgendaID},
	}

	var err error
	for _, val := range allDeIndexes {
		if err = val.DeIndexFunc(pgb.db); err != nil {
			warnUnlessNotExists(err)
			err = nil
		}
	}

	if err = pgb.DeindexTicketsTable(); err != nil {
		warnUnlessNotExists(err)
		err = nil
	}
	return err
}

// IndexAll creates all of the indexes in all tables
func (pgb *ChainDB) IndexAll() error {
	allIndexes := []indexingInfo{
		// blocks table
		indexingInfo{Msg: "blocks table on hash", IndexFunc: IndexBlockTableOnHash},
		indexingInfo{Msg: "blocks table on height", IndexFunc: IndexBlockTableOnHeight},

		// transactions table
		indexingInfo{Msg: "transactions table on tx/block hashes", IndexFunc: IndexTransactionTableOnHashes},
		indexingInfo{Msg: "transactions table on block id/indx", IndexFunc: IndexTransactionTableOnBlockIn},

		// vins table
		indexingInfo{Msg: "vins table on txin", IndexFunc: IndexVinTableOnVins},
		indexingInfo{Msg: "vins table on prevouts", IndexFunc: IndexVinTableOnPrevOuts},

		// vouts table
		indexingInfo{Msg: "vouts table on tx hash and index", IndexFunc: IndexVoutTableOnTxHashIdx},

		// votes table
		indexingInfo{Msg: "votes table on candidate block", IndexFunc: IndexVotesTableOnCandidate},
		indexingInfo{Msg: "votes table on block hash", IndexFunc: IndexVotesTableOnBlockHash},
		indexingInfo{Msg: "votes table on block+tx hash", IndexFunc: IndexVotesTableOnHashes},
		indexingInfo{Msg: "votes table on vote version", IndexFunc: IndexVotesTableOnVoteVersion},

		// misses table
		indexingInfo{Msg: "misses table", IndexFunc: IndexMissesTableOnHashes},

		// agendas table
		indexingInfo{Msg: "agendas table on Block Time", IndexFunc: IndexAgendasTableOnBlockTime},
		indexingInfo{Msg: "agendas table on Agenda ID", IndexFunc: IndexAgendasTableOnAgendaID},

		// Not indexing the address table on vout ID or address here. See
		// IndexAddressTable to create those indexes.
		indexingInfo{Msg: "addresses table on tx hash", IndexFunc: IndexAddressTableOnTxHash},
		indexingInfo{Msg: "addresses table on matching tx hash", IndexFunc: IndexMatchingTxHashOnTableAddress},
		indexingInfo{Msg: "addresses table on block time", IndexFunc: IndexBlockTimeOnTableAddress},
	}

	for _, val := range allIndexes {
		logMsg := "Indexing " + val.Msg + "..."
		log.Infof(logMsg)
		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}

		explorer.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, logMsg)
	}
	// signal task is done
	explorer.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, "")
	return nil
}

// IndexTicketsTable creates the indexes on the tickets table on ticket hash and
// tx DB ID columns, separately.
func (pgb *ChainDB) IndexTicketsTable() error {
	ticketsTableIndexes := []indexingInfo{
		indexingInfo{Msg: "ticket hash", IndexFunc: IndexTicketsTableOnHashes},
		indexingInfo{Msg: "ticket pool status", IndexFunc: IndexTicketsTableOnPoolStatus},
		indexingInfo{Msg: "transaction Db ID", IndexFunc: IndexTicketsTableOnTxDbID},
	}

	for _, val := range ticketsTableIndexes {
		logMsg := "Indexing tickets table on " + val.Msg + "..."
		log.Info(logMsg)
		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}

		explorer.SyncStatusUpdateOtherMsg(dbtypes.AddressesTableSync, logMsg)
	}
	// signal task is done
	explorer.SyncStatusUpdateOtherMsg(dbtypes.AddressesTableSync, "")
	return nil
}

// DeindexTicketsTable drops the ticket hash and tx DB ID column indexes for the
// tickets table.
func (pgb *ChainDB) DeindexTicketsTable() error {
	ticketsTablesDeIndexes := []deIndexingInfo{
		deIndexingInfo{DeindexTicketsTableOnHash},
		deIndexingInfo{DeindexTicketsTableOnPoolStatus},
		deIndexingInfo{DeindexTicketsTableOnTxDbID},
	}

	var err error
	for _, val := range ticketsTablesDeIndexes {
		if err = val.DeIndexFunc(pgb.db); err != nil {
			warnUnlessNotExists(err)
			err = nil
		}
	}
	return err
}

func errIsNotExist(err error) bool {
	return strings.Contains(err.Error(), "does not exist")
}

func warnUnlessNotExists(err error) {
	if !errIsNotExist(err) {
		log.Warn(err)
	}
}

// ReindexAddressesBlockTime rebuilds the addresses(block_time) index.
func (pgb *ChainDB) ReindexAddressesBlockTime() error {
	log.Infof("Reindexing addresses table on block time...")
	err := DeindexBlockTimeOnTableAddress(pgb.db)
	if err != nil && !errIsNotExist(err) {
		log.Errorf("Failed to drop index addresses index on block_time: %v", err)
		return err
	}
	return IndexBlockTimeOnTableAddress(pgb.db)
}

// IndexAddressTable creates the indexes on the address table on the vout ID,
// block_time, matching_tx_hash and address columns, separately.
func (pgb *ChainDB) IndexAddressTable() error {
	addressesTableIndexes := []indexingInfo{
		indexingInfo{Msg: "address", IndexFunc: IndexAddressTableOnAddress},
		indexingInfo{Msg: "matching tx hash", IndexFunc: IndexMatchingTxHashOnTableAddress},
		indexingInfo{Msg: "block time", IndexFunc: IndexBlockTimeOnTableAddress},
		indexingInfo{Msg: "vout Db ID", IndexFunc: IndexAddressTableOnVoutID},
	}

	for _, val := range addressesTableIndexes {
		logMsg := "Indexing addresses table on  " + val.Msg + "..."
		log.Info(logMsg)
		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}

		explorer.SyncStatusUpdateOtherMsg(dbtypes.AddressesTableSync, logMsg)
	}
	// signal task is done
	explorer.SyncStatusUpdateOtherMsg(dbtypes.AddressesTableSync, "")
	return nil
}

// DeindexAddressTable drops the vin ID, block_time, matching_tx_hash
// and address column indexes for the address table.
func (pgb *ChainDB) DeindexAddressTable() error {
	addressesDeindexes := []deIndexingInfo{
		deIndexingInfo{DeindexAddressTableOnAddress},
		deIndexingInfo{DeindexMatchingTxHashOnTableAddress},
		deIndexingInfo{DeindexBlockTimeOnTableAddress},
		deIndexingInfo{DeindexAddressTableOnVoutID},
	}

	var err error
	for _, val := range addressesDeindexes {
		if err = val.DeIndexFunc(pgb.db); err != nil {
			warnUnlessNotExists(err)
			err = nil
		}
	}
	return err
}
