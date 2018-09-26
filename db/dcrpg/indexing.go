// Copyright (c) 2018, The Decred developers
// See LICENSE for details.
package dcrpg

import (
	"database/sql"

	"github.com/decred/dcrdata/v3/db/dcrpg/internal"
)

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
