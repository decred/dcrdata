// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"strings"

	"github.com/decred/dcrdata/db/dcrpg/v6/internal"
	"github.com/decred/dcrdata/v6/db/dbtypes"
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

func IndexTransactionTableOnBlockHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTransactionTableOnBlockHeight)
	return
}

func DeindexTransactionTableOnBlockHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTransactionTableOnBlockHeight)
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

func IndexBlockTableOnTime(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexBlocksTableOnTime)
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

func DeindexBlockTableOnTime(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlocksTableOnTime)
	return
}

// vouts table indexes

// IndexVoutTableOnTxHashIdx creates the index for the addresses table over
// transaction hash and index.
func IndexVoutTableOnTxHashIdx(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVoutTableOnTxHashIdx)
	return
}

func DeindexVoutTableOnTxHashIdx(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVoutTableOnTxHashIdx)
	return
}

func IndexVoutTableOnSpendTxID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVoutTableOnSpendTxID)
	return
}

func DeindexVoutTableOnSpendTxID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVoutTableOnSpendTxID)
	return
}

// addresses table indexes

// IndexBlockTimeOnTableAddress creates the index for the addresses table over
// block time.
func IndexBlockTimeOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexBlockTimeOnTableAddress)
	return
}

func DeindexBlockTimeOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlockTimeOnTableAddress)
	return
}

// IndexAddressTableOnMatchingTxHash creates the index for the addresses table
// over matching transaction hash.
func IndexAddressTableOnMatchingTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnMatchingTxHash)
	return
}

func DeindexAddressTableOnMatchingTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnMatchingTxHash)
	return
}

// IndexAddressTableOnAddress creates the index for the addresses table over
// address.
func IndexAddressTableOnAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnAddress)
	return
}

func DeindexAddressTableOnAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnAddress)
	return
}

// IndexAddressTableOnVoutID creates the index for the addresses table over
// vout row ID.
func IndexAddressTableOnVoutID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnVoutID)
	return
}

func DeindexAddressTableOnVoutID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnVoutID)
	return
}

// IndexAddressTableOnTxHash creates the index for the addresses table over
// transaction hash.
func IndexAddressTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnTxHash)
	return
}

func DeindexAddressTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnTxHash)
	return
}

// votes table indexes

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

// IndexVotesTableOnHeight improves the speed of "Cumulative Vote Choices" agendas
// chart query.
func IndexVotesTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnHeight)
	return
}

func DeindexVotesTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnHeight)
	return
}

// IndexVotesTableOnBlockTime improves the speed of "Vote Choices By Block" agendas
// chart query.
func IndexVotesTableOnBlockTime(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnBlockTime)
	return
}

func DeindexVotesTableOnBlockTime(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnBlockTime)
	return
}

// tickets table indexes

func IndexTicketsTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTicketsTableOnHashes)
	return
}

func DeindexTicketsTableOnHashes(db *sql.DB) (err error) {
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

// missed votes table indexes

func IndexMissesTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexMissesTableOnHashes)
	return
}

func DeindexMissesTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexMissesTableOnHashes)
	return
}

// agendas table indexes

func IndexAgendasTableOnAgendaID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAgendasTableOnAgendaID)
	return
}

func DeindexAgendasTableOnAgendaID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAgendasTableOnAgendaID)
	return
}

// agenda votes table indexes

func IndexAgendaVotesTableOnAgendaID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAgendaVotesTableOnAgendaID)
	return
}

func DeindexAgendaVotesTableOnAgendaID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAgendaVotesTableOnAgendaID)
	return
}

// proposals table indexes

func IndexProposalsTableOnToken(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexProposalsTableOnToken)
	return
}

func DeindexProposalsTableOnToken(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexProposalsTableOnToken)
	return
}

// proposal votes table indexes

func IndexProposalVotesTableOnProposalsID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexProposalVotesTableOnProposalsID)
	return
}

func DeindexProposalVotesTableOnProposalsID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexProposalVotesTableOnProposalsID)
	return
}

// IndexTreasuryTableOnTxHash creates the index for the treasury table over
// tx_hash.
func IndexTreasuryTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTreasuryOnTxHash)
	return
}

// DeindexTreasuryTableOnTxHash drops the index for the treasury table over tx
// hash.
func DeindexTreasuryTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTreasuryOnTxHash)
	return
}

// IndexTreasuryTableOnHeight creates the index for the treasury table over
// block height.
func IndexTreasuryTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTreasuryOnBlockHeight)
	return
}

// DeindexTreasuryTableOnHeight drops the index for the treasury table over
// block height.
func DeindexTreasuryTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTreasuryOnBlockHeight)
	return
}

// Delete duplicates

func (pgb *ChainDB) DeleteDuplicateVins() (int64, error) {
	return DeleteDuplicateVins(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateVouts() (int64, error) {
	return DeleteDuplicateVouts(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateVinsCockroach() (int64, error) {
	return DeleteDuplicateVinsCockroach(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateVoutsCockroach() (int64, error) {
	return DeleteDuplicateVoutsCockroach(pgb.db)
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

func (pgb *ChainDB) DeleteDuplicateAgendas() (int64, error) {
	return DeleteDuplicateAgendas(pgb.db)
}

func (pgb *ChainDB) DeleteDuplicateAgendaVotes() (int64, error) {
	return DeleteDuplicateAgendaVotes(pgb.db)
}

// Indexes checks

// MissingIndexes lists missing table indexes and their descriptions.
func (pgb *ChainDB) MissingIndexes() (missing, descs []string, err error) {
	for idxName, desc := range internal.IndexDescriptions {
		var exists bool
		exists, err = ExistsIndex(pgb.db, idxName)
		if err != nil {
			return
		}
		if !exists {
			missing = append(missing, idxName)
			descs = append(descs, desc)
		}
	}
	return
}

// MissingAddressIndexes list missing addresses table indexes and their
// descriptions.
func (pgb *ChainDB) MissingAddressIndexes() (missing []string, descs []string, err error) {
	for _, idxName := range internal.AddressesIndexNames {
		var exists bool
		exists, err = ExistsIndex(pgb.db, idxName)
		if err != nil {
			return
		}
		if !exists {
			missing = append(missing, idxName)
			descs = append(descs, pgb.indexDescription(idxName))
		}
	}
	return
}

// indexDescription gives the description of the named index.
func (pgb *ChainDB) indexDescription(indexName string) string {
	name, ok := internal.IndexDescriptions[indexName]
	if !ok {
		name = "unknown index"
	}
	return name
}

// DeindexAll drops indexes in most tables.
func (pgb *ChainDB) DeindexAll() error {
	allDeIndexes := []deIndexingInfo{
		// blocks table
		{DeindexBlockTableOnHash},
		{DeindexBlockTableOnHeight},
		{DeindexBlockTableOnTime},

		// transactions table
		{DeindexTransactionTableOnHashes},
		{DeindexTransactionTableOnBlockIn},
		{DeindexTransactionTableOnBlockHeight},

		// vins table
		{DeindexVinTableOnVins},
		{DeindexVinTableOnPrevOuts},

		// vouts table
		{DeindexVoutTableOnTxHashIdx},
		{DeindexVoutTableOnSpendTxID},

		// addresses table
		{DeindexBlockTimeOnTableAddress},
		{DeindexAddressTableOnMatchingTxHash},
		{DeindexAddressTableOnAddress},
		{DeindexAddressTableOnVoutID},
		{DeindexAddressTableOnTxHash},

		// votes table
		{DeindexVotesTableOnCandidate},
		{DeindexVotesTableOnBlockHash},
		{DeindexVotesTableOnHash},
		{DeindexVotesTableOnVoteVersion},
		{DeindexVotesTableOnHeight},
		{DeindexVotesTableOnBlockTime},

		// misses table
		{DeindexMissesTableOnHash},

		// agendas table
		{DeindexAgendasTableOnAgendaID},

		// agenda votes table
		{DeindexAgendaVotesTableOnAgendaID},

		// proposals table
		{DeindexProposalsTableOnToken},

		// proposal votes table
		{DeindexProposalVotesTableOnProposalsID},

		// stats table
		{DeindexStatsTableOnHeight},

		// treasury table
		{DeindexTreasuryTableOnTxHash},
		{DeindexTreasuryTableOnHeight},
	}

	var err error
	for _, val := range allDeIndexes {
		if err = val.DeIndexFunc(pgb.db); err != nil {
			warnUnlessNotExists(err)
		}
	}

	if err = pgb.DeindexTicketsTable(); err != nil {
		warnUnlessNotExists(err)
		err = nil
	}
	return err
}

// IndexAll creates most indexes in the tables. Exceptions: (1) addresses on
// matching_tx_hash (use IndexAddressTable or do it individually), (2) all
// tickets table indexes (use IndexTicketsTable), and (3) vouts on
// spend_tx_row_id (use IndexVoutTableOnSpendTxID).
func (pgb *ChainDB) IndexAll(barLoad chan *dbtypes.ProgressBarLoad) error {
	allIndexes := []indexingInfo{
		// blocks table
		{Msg: "blocks table on hash", IndexFunc: IndexBlockTableOnHash},
		{Msg: "blocks table on height", IndexFunc: IndexBlockTableOnHeight},
		{Msg: "blocks table on time", IndexFunc: IndexBlockTableOnTime},

		// transactions table
		{Msg: "transactions table on tx/block hashes", IndexFunc: IndexTransactionTableOnHashes},
		{Msg: "transactions table on block id/idx", IndexFunc: IndexTransactionTableOnBlockIn},
		{Msg: "transactions table on block height", IndexFunc: IndexTransactionTableOnBlockHeight},

		// vins table
		{Msg: "vins table on txin", IndexFunc: IndexVinTableOnVins},
		{Msg: "vins table on prevouts", IndexFunc: IndexVinTableOnPrevOuts},

		// vouts table
		{Msg: "vouts table on tx hash and index", IndexFunc: IndexVoutTableOnTxHashIdx},
		// {Msg: "vouts table on spend tx row id", IndexFunc: IndexVoutTableOnSpendTxID},

		// votes table
		{Msg: "votes table on candidate block", IndexFunc: IndexVotesTableOnCandidate},
		{Msg: "votes table on block hash", IndexFunc: IndexVotesTableOnBlockHash},
		{Msg: "votes table on block+tx hash", IndexFunc: IndexVotesTableOnHashes},
		{Msg: "votes table on vote version", IndexFunc: IndexVotesTableOnVoteVersion},
		{Msg: "votes table on height", IndexFunc: IndexVotesTableOnHeight},
		{Msg: "votes table on Block Time", IndexFunc: IndexVotesTableOnBlockTime},

		// tickets table is done separately by IndexTicketsTable

		// misses table
		{Msg: "misses table", IndexFunc: IndexMissesTableOnHashes},

		// agendas table
		{Msg: "agendas table on Agenda ID", IndexFunc: IndexAgendasTableOnAgendaID},

		// agenda votes table
		{Msg: "agenda votes table on Agenda ID", IndexFunc: IndexAgendaVotesTableOnAgendaID},

		// Not indexing the address table on matching_tx_hash here. See
		// IndexAddressTable to create them all.
		{Msg: "addresses table on tx hash", IndexFunc: IndexAddressTableOnTxHash},
		{Msg: "addresses table on block time", IndexFunc: IndexBlockTimeOnTableAddress},
		{Msg: "addresses table on address", IndexFunc: IndexAddressTableOnAddress}, // TODO: remove or redefine this or IndexAddressTableOnVoutID since that includes address too
		{Msg: "addresses table on vout DB ID", IndexFunc: IndexAddressTableOnVoutID},
		//{Msg: "addresses table on matching tx hash", IndexFunc: IndexAddressTableOnMatchingTxHash},

		// proposals table
		{Msg: "proposals table on Token+Time", IndexFunc: IndexProposalsTableOnToken},

		// Proposals votes table
		{Msg: "Proposals votes table on Proposals ID", IndexFunc: IndexProposalVotesTableOnProposalsID},

		// stats table
		{Msg: "stats table on height", IndexFunc: IndexStatsTableOnHeight},

		// treasury table
		{Msg: "treasury on tx hash", IndexFunc: IndexTreasuryTableOnTxHash},
		{Msg: "treasury on block height", IndexFunc: IndexTreasuryTableOnHeight},
	}

	for _, val := range allIndexes {
		logMsg := "Indexing " + val.Msg + "..."
		log.Infof(logMsg)
		if barLoad != nil {
			barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.InitialDBLoad, Subtitle: logMsg}
		}

		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}
	}
	// Signal task is done
	if barLoad != nil {
		barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.InitialDBLoad, Subtitle: " "}
	}
	return nil
}

// IndexTicketsTable creates indexes in the tickets table on ticket hash,
// ticket pool status and tx DB ID columns.
func (pgb *ChainDB) IndexTicketsTable(barLoad chan *dbtypes.ProgressBarLoad) error {
	ticketsTableIndexes := []indexingInfo{
		{Msg: "ticket hash", IndexFunc: IndexTicketsTableOnHashes},
		{Msg: "ticket pool status", IndexFunc: IndexTicketsTableOnPoolStatus},
		{Msg: "transaction Db ID", IndexFunc: IndexTicketsTableOnTxDbID},
	}

	for _, val := range ticketsTableIndexes {
		logMsg := "Indexing tickets table on " + val.Msg + "..."
		log.Info(logMsg)
		if barLoad != nil {
			barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.AddressesTableSync, Subtitle: logMsg}
		}

		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}
	}
	// Signal task is done.
	if barLoad != nil {
		barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.AddressesTableSync, Subtitle: " "}
	}
	return nil
}

// DeindexTicketsTable drops indexes in the tickets table on ticket hash,
// ticket pool status and tx DB ID columns.
func (pgb *ChainDB) DeindexTicketsTable() error {
	ticketsTablesDeIndexes := []deIndexingInfo{
		{DeindexTicketsTableOnHashes},
		{DeindexTicketsTableOnPoolStatus},
		{DeindexTicketsTableOnTxDbID},
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
// block_time, matching_tx_hash and address columns.
func (pgb *ChainDB) IndexAddressTable(barLoad chan *dbtypes.ProgressBarLoad) error {
	addressesTableIndexes := []indexingInfo{
		{Msg: "address", IndexFunc: IndexAddressTableOnAddress},
		{Msg: "matching tx hash", IndexFunc: IndexAddressTableOnMatchingTxHash},
		{Msg: "block time", IndexFunc: IndexBlockTimeOnTableAddress},
		{Msg: "vout Db ID", IndexFunc: IndexAddressTableOnVoutID},
		{Msg: "tx hash", IndexFunc: IndexAddressTableOnTxHash},
	}

	for _, val := range addressesTableIndexes {
		logMsg := "Indexing addresses table on " + val.Msg + "..."
		log.Info(logMsg)
		if barLoad != nil {
			barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.AddressesTableSync, Subtitle: logMsg}
		}

		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}
	}
	// Signal task is done.
	if barLoad != nil {
		barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.AddressesTableSync, Subtitle: " "}
	}
	return nil
}

// DeindexAddressTable drops the vin ID, block_time, matching_tx_hash
// and address column indexes for the address table.
func (pgb *ChainDB) DeindexAddressTable() error {
	addressesDeindexes := []deIndexingInfo{
		{DeindexAddressTableOnAddress},
		{DeindexAddressTableOnMatchingTxHash},
		{DeindexBlockTimeOnTableAddress},
		{DeindexAddressTableOnVoutID},
		{DeindexAddressTableOnTxHash},
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

// IndexStatsTableOnHeight creates the index for the stats table over height.
func IndexStatsTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexStatsOnHeight)
	return
}

// DeindexStatsTableOnHeight drops the index for the stats table over height.
func DeindexStatsTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexStatsOnHeight)
	return
}
