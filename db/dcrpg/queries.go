// Copyright (c) 2018-2023, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"

	"github.com/decred/dcrdata/db/dcrpg/v8/internal"
	apitypes "github.com/decred/dcrdata/v8/api/types"
	"github.com/decred/dcrdata/v8/db/cache"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/txhelpers"
	humanize "github.com/dustin/go-humanize"
	"github.com/lib/pq"
)

// dbBestBlock retrieves the best block hash and height from the meta table. The
// error value will never be sql.ErrNoRows; instead with height == -1 indicating
// no data in the meta table.
func dbBestBlock(ctx context.Context, db *sql.DB) (hash dbtypes.ChainHash, height int64, err error) {
	err = db.QueryRowContext(ctx, internal.SelectMetaDBBestBlock).Scan(&height, &hash)
	if err == sql.ErrNoRows {
		err = nil
		height = -1
	}
	return
}

// setDBBestBlock sets the best block hash and height in the meta table.
func setDBBestBlock(db *sql.DB, hash dbtypes.ChainHash, height int64) error {
	numRows, err := sqlExec(db, internal.SetMetaDBBestBlock,
		"failed to update best block in meta table: ", height, hash)
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("failed to update exactly 1 row in meta table (%d)",
			numRows)
	}
	return nil
}

// ibdComplete indicates whether initial block download was completed according
// to the meta.ibd_complete flag.
func ibdComplete(db *sql.DB) (ibdComplete bool, err error) {
	err = db.QueryRow(internal.SelectMetaDBIbdComplete).Scan(&ibdComplete)
	return
}

// setIBDComplete set the ibd_complete (Initial Block Download complete) flag in
// the meta table.
func setIBDComplete(db SqlExecutor, ibdComplete bool) error {
	numRows, err := sqlExec(db, internal.SetMetaDBIbdComplete,
		"failed to update ibd_complete in meta table: ", ibdComplete)
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("failed to update exactly 1 row in meta table (%d)",
			numRows)
	}
	return nil
}

const (
	notOneRowErrMsg = "failed to update exactly 1 row"
)

const (
	creditDebitQuery = iota
	creditQuery
	debitQuery
	mergedCreditQuery
	mergedDebitQuery
	mergedQuery
)

// Maintenance functions

// closeRows closes the input sql.Rows, logging any error.
func closeRows(rows *sql.Rows) {
	if e := rows.Close(); e != nil {
		log.Errorf("Close of Query failed: %v", e)
	}
}

// SqlExecutor is implemented by both sql.DB and sql.Tx.
type SqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// SqlQueryer is implemented by both sql.DB and sql.Tx.
type SqlQueryer interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// SqlExecQueryer is implemented by both sql.DB and sql.Tx.
type SqlExecQueryer interface {
	SqlExecutor
	SqlQueryer
}

// sqlExec executes the SQL statement string with any optional arguments, and
// returns the number of rows affected.
func sqlExec(db SqlExecutor, stmt, execErrPrefix string, args ...interface{}) (int64, error) {
	res, err := db.Exec(stmt, args...)
	if err != nil {
		return 0, fmt.Errorf("%v: %w", execErrPrefix, err)
	}
	if res == nil {
		return 0, nil
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("error in RowsAffected: %w", err)
	}
	return N, err
}

// sqlExecStmt executes the prepared SQL statement with any optional arguments,
// and returns the number of rows affected.
func sqlExecStmt(stmt *sql.Stmt, execErrPrefix string, args ...interface{}) (int64, error) {
	res, err := stmt.Exec(args...)
	if err != nil {
		return 0, fmt.Errorf("%v: %w", execErrPrefix, err)
	}
	if res == nil {
		return 0, nil
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("error in RowsAffected: %w", err)
	}
	return N, err
}

// ExistsIndex checks if the specified index name exists.
func ExistsIndex(db *sql.DB, indexName string) (exists bool, err error) {
	err = db.QueryRow(internal.IndexExists, indexName, "public").Scan(&exists)
	if err == sql.ErrNoRows {
		err = nil
	}
	return
}

// IsUniqueIndex checks if the given index name is defined as UNIQUE.
func IsUniqueIndex(db *sql.DB, indexName string) (isUnique bool, err error) {
	err = db.QueryRow(internal.IndexIsUnique, indexName, "public").Scan(&isUnique)
	return
}

// deleteDuplicateVins deletes rows in vin with duplicate tx information,
// leaving the one row with the lowest id.
func deleteDuplicateVins(db *sql.DB) (int64, error) {
	execErrPrefix := "failed to delete duplicate vins: "

	existsIdx, err := ExistsIndex(db, "uix_vin")
	if err != nil {
		return 0, err
	}
	if !existsIdx {
		return sqlExec(db, internal.DeleteVinsDuplicateRows, execErrPrefix)
	}

	isuniq, err := IsUniqueIndex(db, "uix_vin")
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if isuniq {
		return 0, nil
	}

	return sqlExec(db, internal.DeleteVinsDuplicateRows, execErrPrefix)
}

// deleteDuplicateVouts deletes rows in vouts with duplicate tx information,
// leaving the one row with the lowest id.
func deleteDuplicateVouts(db *sql.DB) (int64, error) {
	execErrPrefix := "failed to delete duplicate vouts: "

	existsIdx, err := ExistsIndex(db, "uix_vout_txhash_ind")
	if err != nil {
		return 0, err
	} else if !existsIdx {
		return sqlExec(db, internal.DeleteVoutDuplicateRows, execErrPrefix)
	}

	if isuniq, err := IsUniqueIndex(db, "uix_vout_txhash_ind"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}

	return sqlExec(db, internal.DeleteVoutDuplicateRows, execErrPrefix)
}

// deleteDuplicateTxns deletes rows in transactions with duplicate tx-block
// hashes, leaving the one row with the lowest id.
func deleteDuplicateTxns(db *sql.DB) (int64, error) {
	execErrPrefix := "failed to delete duplicate transactions: "

	existsIdx, err := ExistsIndex(db, "uix_tx_hashes")
	if err != nil {
		return 0, err
	} else if !existsIdx {
		return sqlExec(db, internal.DeleteTxDuplicateRows, execErrPrefix)
	}

	if isuniq, err := IsUniqueIndex(db, "uix_tx_hashes"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}

	return sqlExec(db, internal.DeleteTxDuplicateRows, execErrPrefix)
}

// deleteDuplicateAgendas deletes rows in agendas with duplicate names leaving
// the one row with the lowest id.
func deleteDuplicateAgendas(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, "uix_agendas_name"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate agendas: "
	return sqlExec(db, internal.DeleteAgendasDuplicateRows, execErrPrefix)
}

// deleteDuplicateAgendaVotes deletes rows in agenda_votes with duplicate
// votes-row-id and agendas-row-id leaving the one row with the lowest id.
func deleteDuplicateAgendaVotes(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, "uix_agenda_votes"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate agenda_votes: "
	return sqlExec(db, internal.DeleteAgendaVotesDuplicateRows, execErrPrefix)
}

// deleteDuplicateVotes deletes rows in votes with duplicate tx_hash and
// block_hash leaving one row with the lowest id.
func deleteDuplicateVotes(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, internal.IndexOfVotesTableOnHashes); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate votes: "
	return sqlExec(db, internal.DeleteVotesDuplicateRows, execErrPrefix)
}

// deleteDuplicateMisses deletes rows in misses with duplicate ticket_hash and
// block_hash leaving one row with the lowest id.
func deleteDuplicateMisses(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, internal.IndexOfMissesTableOnHashes); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate misses: "
	return sqlExec(db, internal.DeleteMissesDuplicateRows, execErrPrefix)
}

// deleteDuplicateTreasuryTxs deletes rows in misses with duplicate tx_hash and
// block_hash leaving one row with the lowest id.
func deleteDuplicateTreasuryTxs(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, internal.IndexOfTreasuryTableOnTxHash); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate treasury txs: "
	return sqlExec(db, internal.DeleteTreasuryTxsDuplicateRows, execErrPrefix)
}

// --- stake (votes, tickets, misses, treasury) tables ---

func insertTreasuryTxns(db *sql.DB, dbTxns []*dbtypes.Tx, checked, updateExistingRecords bool) error {
	dbtx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("unable to begin database transaction: %w", err)
	}

	// Prepare treasury insert statement, optionally updating a row if it
	// conflicts with the unique index on (tx_hash, block_hash).
	stmt, err := dbtx.Prepare(internal.MakeTreasuryInsertStatement(checked, updateExistingRecords))
	if err != nil {
		log.Errorf("Ticket INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return err
	}

	// Insert each treasury txn.
	for _, tx := range dbTxns {
		var value int64
		switch tx.TxType {
		case int16(stake.TxTypeTreasuryBase):
			value = tx.Sent // == tx.Spent because fees are 0
		case int16(stake.TxTypeTSpend):
			value = -tx.Spent
		case int16(stake.TxTypeTAdd):
			value = int64(tx.Vouts[0].Value)
		default:
			continue // not a treasury tx
		}

		_, err = stmt.Exec(tx.TxID, tx.TxType, value, tx.BlockHash, tx.BlockHeight,
			tx.BlockTime, tx.IsMainchainBlock)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			_ = stmt.Close() // try, but we want the QueryRow error back
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return err
		}
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return dbtx.Commit()
}

// insertTickets takes a slice of *dbtypes.Tx and corresponding DB row IDs for
// transactions, extracts the tickets, and inserts the tickets into the
// database. Outputs are a slice of DB row IDs of the inserted tickets, and an
// error.
func insertTickets(db *sql.DB, dbTxns []*dbtypes.Tx, txDbIDs []uint64, checked, updateExistingRecords bool) ([]uint64, []*dbtypes.Tx, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	// Prepare ticket insert statement, optionally updating a row if it conflicts
	// with the unique index on (tx_hash, block_hash).
	stmt, err := dbtx.Prepare(internal.MakeTicketInsertStatement(checked, updateExistingRecords))
	if err != nil {
		log.Errorf("Ticket INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, nil, err
	}

	// Choose only SSTx
	var ticketTx []*dbtypes.Tx
	var ticketDbIDs []uint64
	for i, tx := range dbTxns {
		if tx.TxType == int16(stake.TxTypeSStx) {
			ticketTx = append(ticketTx, tx)
			ticketDbIDs = append(ticketDbIDs, txDbIDs[i])
		}
	}

	// Insert each ticket
	ids := make([]uint64, 0, len(ticketTx))
	for i, tx := range ticketTx {
		// Reference Vouts[0] to determine stakesubmission address and if multisig
		var stakesubmissionAddress string
		var isScriptHash bool
		if len(tx.Vouts) > 0 {
			if len(tx.Vouts[0].ScriptPubKeyData.Addresses) > 0 {
				stakesubmissionAddress = tx.Vouts[0].ScriptPubKeyData.Addresses[0]
			}
			isScriptHash = tx.Vouts[0].ScriptPubKeyData.Type == dbtypes.SCScriptHash
			// isScriptHash = stdscript.IsStakeSubmissionScriptHashScript(tx.Vouts[0].Version, tx.Vouts[0].ScriptPubKey)
			// NOTE: This was historically broken, always setting false, and
			// calling it "isMultisig"! A DB upgrade is needed to identify old
			// p2sh tickets, or just remove the is_multisig column entirely.
		}

		price := toCoin(tx.Vouts[0].Value)
		fee := toCoin(tx.Fees)
		isSplit := tx.NumVin > 1

		var id uint64
		err := stmt.QueryRow(
			tx.TxID, tx.BlockHash, tx.BlockHeight, ticketDbIDs[i],
			stakesubmissionAddress, isScriptHash, isSplit, tx.NumVin,
			price, fee, dbtypes.TicketUnspent, dbtypes.PoolStatusLive,
			tx.IsMainchainBlock).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			_ = stmt.Close() // try, but we want the QueryRow error back
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return nil, nil, err
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return ids, ticketTx, dbtx.Commit()
}

// insertVotes takes a slice of *dbtypes.Tx, which must contain all the stake
// transactions in a block, extracts the votes, and inserts the votes into the
// database. The input MsgBlockPG contains each stake transaction's MsgTx in
// STransactions, and they must be in the same order as the dbtypes.Tx slice.
//
// This function also identifies and stores missed votes using
// msgBlock.Validators, which lists the ticket hashes called to vote on the
// previous block (msgBlock.WinningTickets are the lottery winners to be mined
// in the next block).
//
// The TicketTxnIDGetter is used to get the spent tickets' row IDs. The get
// function, TxnDbID, is called with the expire argument set to false, so that
// subsequent cache lookups by other consumers will succeed.
//
// votesMilestones holds up-to-date blockchain info deployment data.
//
// It also updates the agendas and the agenda_votes tables. Agendas table
// holds the high level information about all agendas that is contained in the
// votingMilestones.MileStone (i.e. Agenda Name, Status and LockedIn, Activated
// & HardForked heights). Agenda_votes table hold the agendas vote choices
// information and references to the agendas and votes tables.
//
// Outputs are slices of DB row IDs for the votes and misses, and an error.
func insertVotes(db *sql.DB, dbTxns []*dbtypes.Tx, _ /*txDbIDs*/ []uint64, fTx *TicketTxnIDGetter,
	msgBlock *MsgBlockPG, checked, updateExistingRecords bool, params *chaincfg.Params,
	votesMilestones *dbtypes.BlockChainData) ([]uint64, []*dbtypes.Tx, []dbtypes.ChainHash,
	[]uint64, map[dbtypes.ChainHash]uint64, error) {
	// Choose only SSGen txns
	msgTxs := msgBlock.STransactions
	var voteTxs []*dbtypes.Tx
	var voteMsgTxs []*wire.MsgTx
	//var voteTxDbIDs []uint64 // not used presently
	for i, tx := range dbTxns {
		if tx.TxType == int16(stake.TxTypeSSGen) {
			voteTxs = append(voteTxs, tx)
			voteMsgTxs = append(voteMsgTxs, msgTxs[i])
			//voteTxDbIDs = append(voteTxDbIDs, txDbIDs[i])
			// if tx.TxID != msgTxs[i].CachedTxHash().String() {
			// 	return nil, nil, nil, nil, nil, fmt.Errorf("txid of dbtypes.Tx does not match that of msgTx")
			// } // comment this check
		}
	}

	if len(voteTxs) == 0 {
		return nil, nil, nil, nil, nil, nil
	}

	// Carefully test this:
	// treasuryActive := txhelpers.IsTreasuryActive(params.Net, int64(msgBlock.Header.Height))

	// Start DB transaction.
	dbtx, err := db.Begin()
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	// Prepare vote insert statement, optionally updating a row if it conflicts
	// with the unique index on (tx_hash, block_hash).
	voteInsert := internal.MakeVoteInsertStatement(checked, updateExistingRecords)
	voteStmt, err := dbtx.Prepare(voteInsert)
	if err != nil {
		log.Errorf("Votes INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, nil, nil, nil, nil, err
	}

	// Prepare agenda insert statement.
	agendaStmt, err := dbtx.Prepare(internal.MakeAgendaInsertStatement(checked))
	if err != nil {
		log.Errorf("Agendas INSERT prepare: %v", err)
		_ = voteStmt.Close()
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, nil, nil, nil, nil, err
	}

	// Prepare agenda votes insert statement.
	agendaVotesInsert := internal.MakeAgendaVotesInsertStatement(checked)
	agendaVotesStmt, err := dbtx.Prepare(agendaVotesInsert)
	if err != nil {
		log.Errorf("Agenda Votes INSERT prepare: %v", err)
		_ = voteStmt.Close()
		_ = agendaStmt.Close()
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, nil, nil, nil, nil, err
	}

	bail := func() {
		// Already up a creek. Just log any Rollback error.
		_ = voteStmt.Close()
		_ = agendaStmt.Close()
		_ = agendaVotesStmt.Close()
		if errRoll := dbtx.Rollback(); errRoll != nil {
			log.Errorf("Rollback failed: %v", errRoll)
		}
	}

	// If storedAgendas is empty, it attempts to retrieve stored agendas if they
	// exists and changes between the storedAgendas and the up-to-date
	// votingMilestones.AgendaMileStones map are updated to agendas table and
	// storedAgendas cache. This should happen only once, since storedAgendas
	// persists the added data.
	if len(storedAgendas) == 0 {
		var id int64
		// Attempt to retrieve agendas from the database.
		storedAgendas, err = retrieveAllAgendas(db)
		if err != nil {
			bail()
			return nil, nil, nil, nil, nil,
				fmt.Errorf("retrieveAllAgendas failed: %w", err)
		}

		for name, d := range votesMilestones.AgendaMileStones {
			m, ok := storedAgendas[name]
			// Updates the current agenda details to the agendas table and
			// storedAgendas map if doesn't exist or when its status has changed.
			if !ok || (d.Status != m.Status) || (d.Activated != m.Activated) ||
				(d.VotingDone != m.VotingDone) {
				err = agendaStmt.QueryRow(name, d.Status, int32(d.VotingDone),
					int32(d.Activated), int32(d.HardForked)).Scan(&id)
				if err != nil {
					bail()
					return nil, nil, nil, nil, nil,
						fmt.Errorf("agenda INSERT failed: %w", err)
				}

				storedAgendas[name] = dbtypes.MileStone{
					ID:         id,
					VotingDone: d.VotingDone,
					Activated:  d.Activated,
					HardForked: d.HardForked,
					Status:     d.Status,
				}
			}
		}
	}

	// Insert each vote, and build list of missed votes equal to
	// setdiff(Validators, votes).
	candidateBlockHash := dbtypes.ChainHash(msgBlock.Header.PrevBlock)
	ids := make([]uint64, 0, len(voteTxs))
	spentTicketHashes := make([]dbtypes.ChainHash, 0, len(voteTxs))
	spentTicketDbIDs := make([]uint64, 0, len(voteTxs))
	misses := make([]dbtypes.ChainHash, len(msgBlock.Validators))
	copy(misses, msgBlock.Validators)
	for i, tx := range voteTxs {
		msgTx := voteMsgTxs[i]
		voteVersion := stake.SSGenVersion(msgTx)
		validBlock, _ /* TODO treasuryVotes */, voteBits, err := txhelpers.SSGenVoteBlockValid(msgTx /*, TODO treasuryEnabled */)
		if err != nil {
			bail()
			return nil, nil, nil, nil, nil, err
		}

		voteReward := toCoin(msgTx.TxIn[0].ValueIn)
		stakeSubmissionAmount := toCoin(msgTx.TxIn[1].ValueIn)
		stakeSubmissionTxHash := dbtypes.ChainHash(msgTx.TxIn[1].PreviousOutPoint.Hash)
		spentTicketHashes = append(spentTicketHashes, stakeSubmissionTxHash)

		// Lookup the row ID in the transactions table for the ticket purchase.
		var ticketTxDbID uint64
		if fTx != nil {
			ticketTxDbID, err = fTx.TxnDbID(stakeSubmissionTxHash, false)
			if err != nil {
				bail()
				return nil, nil, nil, nil, nil, err
			}
		}
		spentTicketDbIDs = append(spentTicketDbIDs, ticketTxDbID)

		// Remove the spent ticket from missed list.
		for im := range misses {
			if misses[im] == stakeSubmissionTxHash {
				misses[im] = misses[len(misses)-1]
				misses = misses[:len(misses)-1]
				break
			}
		}

		// votes table insert
		var votesRowID uint64
		err = voteStmt.QueryRow(
			tx.BlockHeight, tx.TxID, tx.BlockHash, candidateBlockHash,
			int32(voteVersion), int16(voteBits), validBlock.Validity,
			stakeSubmissionTxHash, ticketTxDbID, stakeSubmissionAmount,
			voteReward, tx.IsMainchainBlock, tx.BlockTime).Scan(&votesRowID)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			bail()
			return nil, nil, nil, nil, nil, err
		}
		ids = append(ids, votesRowID)

		// agendas table, not modified if not updating existing records.
		if checked && !updateExistingRecords {
			continue // rest of loop deals with agendas table
		}

		_, _, _, choices, _ /* tspendChoices */, err := txhelpers.SSGenVoteChoices(msgTx, params)
		if err != nil {
			bail()
			return nil, nil, nil, nil, nil, err
		}

		var rowID uint64
		for _, val := range choices {
			// As of here, storedAgendas should not be empty and
			// votesMilestones.AgendaMileStones should have cached the latest
			// blockchain deployment info. The change in status is detected as
			// the change between respective agendas statuses stored in the two
			// maps. It is then updated in storedAgendas cache and agendas table.
			p := votesMilestones.AgendaMileStones[val.ID]
			s := storedAgendas[val.ID]
			if s.Status != p.Status {
				err = agendaStmt.QueryRow(val.ID, p.Status, int32(p.VotingDone),
					int32(p.Activated), int32(p.HardForked)).Scan(&s.ID)
				if err != nil {
					bail()
					return nil, nil, nil, nil, nil, fmt.Errorf("agenda INSERT failed: %w", err)
				}

				s.Status = p.Status
				s.VotingDone = p.VotingDone
				s.Activated = p.Activated
				s.HardForked = p.HardForked
				storedAgendas[val.ID] = s
			}

			if p.ID == 0 {
				p.ID = s.ID
				votesMilestones.AgendaMileStones[val.ID] = p
			}

			var index, err = dbtypes.ChoiceIndexFromStr(val.Choice.Id)
			if err != nil {
				bail()
				return nil, nil, nil, nil, nil, err
			}

			err = agendaVotesStmt.QueryRow(votesRowID, s.ID, index).Scan(&rowID)
			if err != nil {
				bail()
				return nil, nil, nil, nil, nil, fmt.Errorf("agenda_votes INSERT failed: %w", err)
			}
		}
	}

	// Close prepared statements. Ignore errors as we'll Commit regardless.
	_ = voteStmt.Close()
	_ = agendaStmt.Close()
	_ = agendaVotesStmt.Close()

	// If the validators are available, miss accounting should be accurate.
	if len(msgBlock.Validators) > 0 && len(ids)+len(misses) != 5 {
		fmt.Println(misses)
		fmt.Println(voteTxs)
		_ = dbtx.Rollback()
		panic(fmt.Sprintf("votes (%d) + misses (%d) != 5", len(ids), len(misses)))
	}

	// Store missed tickets.
	missHashMap := make(map[dbtypes.ChainHash]uint64)
	if len(misses) > 0 {
		// Insert misses, optionally updating a row if it conflicts with the
		// unique index on (ticket_hash, block_hash).
		stmtMissed, err := dbtx.Prepare(internal.MakeMissInsertStatement(checked, updateExistingRecords))
		if err != nil {
			log.Errorf("Miss INSERT prepare: %v", err)
			_ = dbtx.Rollback() // try, but we want the Prepare error back
			return nil, nil, nil, nil, nil, err
		}

		// Insert the miss in the misses table, and store the row ID of the
		// new/existing/updated miss.
		blockHash := dbtypes.ChainHash(msgBlock.BlockHash())
		for i := range misses {
			var id uint64
			err = stmtMissed.QueryRow(
				msgBlock.Header.Height, blockHash, candidateBlockHash,
				misses[i]).Scan(&id)
			if err != nil {
				if err == sql.ErrNoRows {
					continue
				}
				_ = stmtMissed.Close() // try, but we want the QueryRow error back
				if errRoll := dbtx.Rollback(); errRoll != nil {
					log.Errorf("Rollback failed: %v", errRoll)
				}
				return nil, nil, nil, nil, nil, err
			}
			missHashMap[misses[i]] = id
		}
		_ = stmtMissed.Close()
	}

	return ids, voteTxs, spentTicketHashes, spentTicketDbIDs, missHashMap, dbtx.Commit()
}

// retrieveMissedVotesInBlock gets a list of ticket hashes that were called to
// vote in the given block, but missed their vote.
func retrieveMissedVotesInBlock(ctx context.Context, db *sql.DB, blockHash dbtypes.ChainHash) (ticketHashes []dbtypes.ChainHash, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectMissesInBlock, blockHash)
	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var hash dbtypes.ChainHash
		err = rows.Scan(&hash)
		if err != nil {
			return
		}

		ticketHashes = append(ticketHashes, hash)
	}
	err = rows.Err()

	return
}

// retrieveMissedVotesForBlockRange retrieves missed votes for the specified
// block range.
func retrieveMissedVotesForBlockRange(ctx context.Context, db *sql.DB, startHeight, endHeight int64) (missedVotes int64, err error) {
	err = db.QueryRowContext(ctx, internal.SelectMissCountForBlockRange, startHeight, endHeight).Scan(&missedVotes)
	if err != nil {
		return 0, err
	}
	return
}

// retrieveMissesForTicket gets all of the blocks in which the ticket was called
// to place a vote on the previous block. The previous block that would have
// been validated by the vote is not the block data that is returned.
/*
func retrieveMissesForTicket(ctx context.Context, db *sql.DB, ticketHash string) (blockHashes []string, blockHeights []int64, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectMissesForTicket, ticketHash)
	if err != nil {
		return nil, nil, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var hash string
		var height int64
		err = rows.Scan(&height, &hash)
		if err != nil {
			return
		}

		blockHashes = append(blockHashes, hash)
		blockHeights = append(blockHeights, height)
	}
	err = rows.Err()

	return
}
*/

// retrieveMissForTicket gets the mainchain block in which the ticket was called
// to place a vote on the previous block. The previous block that would have
// been validated by the vote is not the block data that is returned.
func retrieveMissForTicket(ctx context.Context, db *sql.DB, ticketHash dbtypes.ChainHash) (blockHash dbtypes.ChainHash, blockHeight int64, err error) {
	err = db.QueryRowContext(ctx, internal.SelectMissesMainchainForTicket,
		ticketHash).Scan(&blockHeight, &blockHash)
	return
}

// retrieveAllAgendas returns all the current agendas in the db.
func retrieveAllAgendas(db *sql.DB) (map[string]dbtypes.MileStone, error) {
	rows, err := db.Query(internal.SelectAllAgendas)
	if err != nil {
		return nil, err
	}

	currentMilestones := make(map[string]dbtypes.MileStone)
	defer closeRows(rows)

	for rows.Next() {
		var name string
		var m dbtypes.MileStone
		err = rows.Scan(&m.ID, &name, &m.Status, &m.VotingDone,
			&m.Activated, &m.HardForked)
		if err != nil {
			return nil, err
		}

		currentMilestones[name] = m
	}
	err = rows.Err()

	return currentMilestones, err
}

// retrieveAllRevokes gets for all ticket revocations the row IDs (primary
// keys), transaction hashes, block heights. It also gets the row ID in the vins
// table for the first input of the revocation transaction, which should
// correspond to the stakesubmission previous outpoint of the ticket purchase.
// This function is used in UpdateSpendingInfoInAllTickets, so it should not be
// subject to timeouts.
/*
func retrieveAllRevokes(ctx context.Context, db *sql.DB) (ids []uint64, hashes []string, heights []int64, vinDbIDs []uint64, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectAllRevokes)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var id, vinDbID uint64
		var height int64
		var hash string
		err = rows.Scan(&id, &hash, &height, &vinDbID)
		if err != nil {
			return
		}

		ids = append(ids, id)
		heights = append(heights, height)
		hashes = append(hashes, hash)
		vinDbIDs = append(vinDbIDs, vinDbID)
	}
	err = rows.Err()

	return
}
*/

// retrieveAllVotesDbIDsHeightsTicketDbIDs gets for all votes the row IDs
// (primary keys) in the votes table, the block heights, and the row IDs in the
// tickets table of the spent tickets. This function is used in
// UpdateSpendingInfoInAllTickets, so it should not be subject to timeouts.
/*
func retrieveAllVotesDbIDsHeightsTicketDbIDs(ctx context.Context, db *sql.DB) (ids []uint64, heights []int64,
	ticketDbIDs []uint64, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectAllVoteDbIDsHeightsTicketDbIDs)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id, ticketDbID uint64
		var height int64
		err = rows.Scan(&id, &height, &ticketDbID)
		if err != nil {
			return
		}

		ids = append(ids, id)
		heights = append(heights, height)
		ticketDbIDs = append(ticketDbIDs, ticketDbID)
	}
	err = rows.Err()

	return
}
*/

// retrieveWindowBlocks fetches chunks of windows using the limit and offset provided
// for a window size of chaincfg.Params.StakeDiffWindowSize.
func retrieveWindowBlocks(ctx context.Context, db *sql.DB, windowSize, currentHeight int64, limit, offset uint64) ([]*dbtypes.BlocksGroupedInfo, error) {
	endWindow := currentHeight/windowSize - int64(offset)
	startWindow := endWindow - int64(limit) + 1
	startHeight := startWindow * windowSize
	endHeight := (endWindow+1)*windowSize - 1
	rows, err := db.QueryContext(ctx, internal.SelectWindowsByLimit, windowSize, startHeight, endHeight)
	if err != nil {
		return nil, fmt.Errorf("retrieveWindowBlocks failed: error: %w", err)
	}
	defer closeRows(rows)

	data := make([]*dbtypes.BlocksGroupedInfo, 0)
	for rows.Next() {
		var difficulty float64
		var timestamp dbtypes.TimeDef
		var startBlock, sbits, count int64
		var blockSizes, votes, txs, revocations, tickets uint64

		err = rows.Scan(&startBlock, &difficulty, &txs, &tickets, &votes,
			&revocations, &blockSizes, &sbits, &timestamp, &count)
		if err != nil {
			return nil, err
		}

		endBlock := startBlock + windowSize - 1
		index := dbtypes.CalculateWindowIndex(endBlock, windowSize)

		data = append(data, &dbtypes.BlocksGroupedInfo{
			IndexVal:      index, //window index at the endblock
			EndBlock:      endBlock,
			Voters:        votes,
			Transactions:  txs,
			FreshStake:    tickets,
			Revocations:   revocations,
			BlocksCount:   count,
			TxCount:       txs + tickets + revocations + votes,
			Difficulty:    difficulty,
			TicketPrice:   sbits,
			Size:          int64(blockSizes),
			FormattedSize: humanize.Bytes(blockSizes),
			StartTime:     timestamp,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return data, nil
}

// retrieveTimeBasedBlockListing fetches blocks in chunks based on their block
// time using the limit and offset provided. The time-based blocks groupings
// include but are not limited to day, week, month and year.
func retrieveTimeBasedBlockListing(ctx context.Context, db *sql.DB, timeInterval string,
	limit, offset uint64) ([]*dbtypes.BlocksGroupedInfo, error) {
	rows, err := db.QueryContext(ctx, internal.SelectBlocksTimeListingByLimit, timeInterval,
		limit, offset)
	if err != nil {
		return nil, fmt.Errorf("retrieveTimeBasedBlockListing failed: error: %w", err)
	}
	defer closeRows(rows)

	var data []*dbtypes.BlocksGroupedInfo
	for rows.Next() {
		var startTime, endTime, indexVal dbtypes.TimeDef
		var txs, tickets, votes, revocations, blockSizes uint64
		var blocksCount, endBlock int64

		err = rows.Scan(&indexVal, &endBlock, &txs, &tickets, &votes,
			&revocations, &blockSizes, &blocksCount, &startTime, &endTime)
		if err != nil {
			return nil, err
		}

		data = append(data, &dbtypes.BlocksGroupedInfo{
			EndBlock:           endBlock,
			Voters:             votes,
			Transactions:       txs,
			FreshStake:         tickets,
			Revocations:        revocations,
			TxCount:            txs + tickets + revocations + votes,
			BlocksCount:        blocksCount,
			Size:               int64(blockSizes),
			FormattedSize:      humanize.Bytes(blockSizes),
			StartTime:          startTime,
			FormattedStartTime: startTime.Format("2006-01-02"),
			EndTime:            endTime,
			FormattedEndTime:   endTime.Format("2006-01-02"),
		})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return data, nil
}

// retrieveUnspentTickets gets all unspent tickets.
func retrieveUnspentTickets(ctx context.Context, db *sql.DB) (ids []uint64, hashes []dbtypes.ChainHash, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectUnspentTickets)
	if err != nil {
		return nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var hash dbtypes.ChainHash
		err = rows.Scan(&id, &hash)
		if err != nil {
			return nil, nil, err
		}

		ids = append(ids, id)
		hashes = append(hashes, hash)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	return ids, hashes, nil
}

// retrieveTicketIDByHashNoCancel gets the db row ID (primary key) in the
// tickets table for the given ticket hash. As the name implies, this query
// should not accept a cancelable context.
func retrieveTicketIDByHashNoCancel(db *sql.DB, ticketHash dbtypes.ChainHash) (id uint64, err error) {
	err = db.QueryRow(internal.SelectTicketIDByHash, ticketHash).Scan(&id)
	return
}

// retrieveTicketStatusByHash gets the spend status and ticket pool status for
// the given ticket hash.
func retrieveTicketStatusByHash(ctx context.Context, db *sql.DB, ticketHash dbtypes.ChainHash) (id uint64,
	spendStatus dbtypes.TicketSpendType, poolStatus dbtypes.TicketPoolStatus, err error) {
	err = db.QueryRowContext(ctx, internal.SelectTicketStatusByHash, ticketHash).
		Scan(&id, &spendStatus, &poolStatus)
	return
}

// retrieveTicketInfoByHash retrieves the ticket spend and pool statuses as well
// as the purchase and spending block info and spending txid.
func retrieveTicketInfoByHash(ctx context.Context, db *sql.DB, ticketHash dbtypes.ChainHash) (spendStatus dbtypes.TicketSpendType,
	poolStatus dbtypes.TicketPoolStatus, purchaseBlock, lotteryBlock *apitypes.TinyBlock, spendTxid dbtypes.ChainHash, err error) {
	var dbid sql.NullInt64
	var purchaseHash, spendHash dbtypes.ChainHash
	var purchaseHeight, spendHeight uint32
	err = db.QueryRowContext(ctx, internal.SelectTicketInfoByHash, ticketHash).
		Scan(&purchaseHash, &purchaseHeight, &spendStatus, &poolStatus, &dbid)
	if err != nil {
		return
	}

	purchaseBlock = &apitypes.TinyBlock{
		Hash:   purchaseHash.String(),
		Height: purchaseHeight,
	}

	if spendStatus == dbtypes.TicketUnspent {
		// ticket unspent. No further queries required.
		return
	}
	if !dbid.Valid {
		err = fmt.Errorf("Invalid spending tx database ID")
		return
	}

	err = db.QueryRowContext(ctx, internal.SelectTxnByDbID, dbid.Int64).
		Scan(&spendHash, &spendHeight, &spendTxid)

	if err != nil {
		return
	}

	if spendStatus == dbtypes.TicketVoted {
		lotteryBlock = &apitypes.TinyBlock{
			Hash:   spendHash.String(),
			Height: spendHeight,
		}
	}

	return
}

// retrieveTicketIDsByHashes gets the db row IDs (primary keys) in the tickets
// table for the given ticket purchase transaction hashes.
/*
func retrieveTicketIDsByHashes(ctx context.Context, db *sql.DB, ticketHashes []string) (ids []uint64, err error) {
	var dbtx *sql.Tx
	dbtx, err = db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelDefault,
		ReadOnly:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	stmt, err := dbtx.Prepare(internal.SelectTicketIDByHash)
	if err != nil {
		log.Errorf("Tickets SELECT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
	}

	ids = make([]uint64, 0, len(ticketHashes))
	for ih := range ticketHashes {
		var id uint64
		err = stmt.QueryRow(ticketHashes[ih]).Scan(&id)
		if err != nil {
			_ = stmt.Close() // try, but we want the QueryRow error back
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return ids, fmt.Errorf("Tickets SELECT exec failed: %w", err)
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return ids, dbtx.Commit()
}
*/

// retrieveTicketsByDate fetches the tickets in the current ticketpool order by the
// purchase date. The maturity block is needed to identify immature tickets.
// The grouping is done using the time-based group names provided e.g. months,
// days, weeks and years.
func retrieveTicketsByDate(ctx context.Context, db *sql.DB, maturityBlock int64, groupBy string) (*dbtypes.PoolTicketsData, error) {
	rows, err := db.QueryContext(ctx, internal.MakeSelectTicketsByPurchaseDate(groupBy), maturityBlock)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	tickets := new(dbtypes.PoolTicketsData)
	for rows.Next() {
		var immature, live uint64
		var timestamp time.Time
		var price float64
		err = rows.Scan(&timestamp, &price, &immature, &live)
		if err != nil {
			return nil, fmt.Errorf("retrieveTicketsByDate: %w", err)
		}

		tickets.Time = append(tickets.Time, dbtypes.NewTimeDef(timestamp))
		tickets.Immature = append(tickets.Immature, immature)
		tickets.Live = append(tickets.Live, live)

		// Returns the average value of a ticket depending on the grouping mode used
		avg := uint64(price*1e8) / (live + immature)
		tickets.Price = append(tickets.Price, toCoin(avg))
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tickets, nil
}

// retrieveTicketByPrice fetches the tickets in the current ticketpool ordered by the
// purchase price. The maturity block is needed to identify immature tickets.
// The grouping is done using the time-based group names provided e.g. months,
// days, weeks and years.
func retrieveTicketByPrice(ctx context.Context, db *sql.DB, maturityBlock int64) (*dbtypes.PoolTicketsData, error) {
	// Create the query statement and retrieve rows
	rows, err := db.QueryContext(ctx, internal.SelectTicketsByPrice, maturityBlock)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	tickets := new(dbtypes.PoolTicketsData)
	for rows.Next() {
		var live, immature uint64
		var price float64
		err = rows.Scan(&price, &immature, &live)
		if err != nil {
			return nil, fmt.Errorf("retrieveTicketByPrice: %w", err)
		}

		tickets.Immature = append(tickets.Immature, immature)
		tickets.Live = append(tickets.Live, live)
		tickets.Price = append(tickets.Price, price)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tickets, nil
}

// retrieveTicketsGroupedByType fetches the count of tickets in the current
// ticketpool grouped by ticket type (inferred by their output counts). The
// grouping used here i.e. solo, pooled and tixsplit is just a guessing based on
// commonly structured ticket purchases.
func retrieveTicketsGroupedByType(ctx context.Context, db *sql.DB) (*dbtypes.PoolTicketsData, error) {
	rows, err := db.QueryContext(ctx, internal.SelectTicketsByType)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	tickets := new(dbtypes.PoolTicketsData)
	for rows.Next() {
		var output, count uint64

		if err = rows.Scan(&output, &count); err != nil {
			return nil, fmt.Errorf("retrieveTicketsGroupedByType: %w", err)
		}

		tickets.Count = append(tickets.Count, count)
		// sstxcommitment count is calculated from all the outputs available.
		// It is a script hash used for payout of voting and is calculated using
		// the formula (n-1)/2 where n = all possible outputs.
		tickets.Outputs = append(tickets.Outputs, (output-1)/2)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tickets, nil
}

// setPoolStatusForTickets sets the ticket pool status for the tickets specified
// by db row ID.
func setPoolStatusForTickets(db *sql.DB, ticketDbIDs []uint64, poolStatuses []dbtypes.TicketPoolStatus) (int64, error) {
	if len(ticketDbIDs) == 0 {
		return 0, nil
	}
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	var stmt *sql.Stmt
	stmt, err = dbtx.Prepare(internal.SetTicketPoolStatusForTicketDbID)
	if err != nil {
		// Already up a creek. Just return error from Prepare.
		_ = dbtx.Rollback()
		return 0, fmt.Errorf("tickets SELECT prepare failed: %w", err)
	}

	var totalTicketsUpdated int64
	rowsAffected := make([]int64, len(ticketDbIDs))
	for i, ticketDbID := range ticketDbIDs {
		rowsAffected[i], err = sqlExecStmt(stmt, "failed to set ticket spending info: ",
			ticketDbID, poolStatuses[i])
		if err != nil {
			_ = stmt.Close()
			return 0, dbtx.Rollback()
		}
		totalTicketsUpdated += rowsAffected[i]
		if rowsAffected[i] != 1 {
			log.Warnf("Updated pool status for %d tickets, expecting just 1 (%d, %v)!",
				rowsAffected[i], ticketDbID, poolStatuses[i])
		}
	}

	_ = stmt.Close()

	return totalTicketsUpdated, dbtx.Commit()
}

// SetPoolStatusForTicketsByHash sets the ticket pool status for the tickets
// specified by ticket purchase transaction hash.
/*
func SetPoolStatusForTicketsByHash(db *sql.DB, tickets []string,
	poolStatuses []dbtypes.TicketPoolStatus) (int64, error) {
	if len(tickets) == 0 {
		return 0, nil
	}
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	var stmt *sql.Stmt
	stmt, err = dbtx.Prepare(internal.SetTicketPoolStatusForHash)
	if err != nil {
		// Already up a creek. Just return error from Prepare.
		_ = dbtx.Rollback()
		return 0, fmt.Errorf("tickets SELECT prepare failed: %w", err)
	}

	var totalTicketsUpdated int64
	rowsAffected := make([]int64, len(tickets))
	for i, ticket := range tickets {
		rowsAffected[i], err = sqlExecStmt(stmt, "failed to set ticket pool status: ",
			ticket, poolStatuses[i])
		if err != nil {
			_ = stmt.Close()
			return 0, dbtx.Rollback()
		}
		totalTicketsUpdated += rowsAffected[i]
		if rowsAffected[i] != 1 {
			log.Warnf("Updated pool status for %d tickets, expecting just 1 (%s, %v)!",
				rowsAffected[i], ticket, poolStatuses[i])
			// TODO: go get the info to add it to the tickets table
		}
	}

	_ = stmt.Close()

	return totalTicketsUpdated, dbtx.Commit()
}
*/

// setSpendingForTickets sets the spend type, spend height, spending transaction
// row IDs (in the table relevant to the spend type), and ticket pool status for
// the given tickets specified by their db row IDs.
func setSpendingForTickets(db *sql.DB, ticketDbIDs, spendDbIDs []uint64,
	blockHeights []int64, spendTypes []dbtypes.TicketSpendType,
	poolStatuses []dbtypes.TicketPoolStatus) (int64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	var stmt *sql.Stmt
	stmt, err = dbtx.Prepare(internal.SetTicketSpendingInfoForTicketDbID)
	if err != nil {
		// Already up a creek. Just return error from Prepare.
		_ = dbtx.Rollback()
		return 0, fmt.Errorf("tickets SELECT prepare failed: %w", err)
	}

	var totalTicketsUpdated int64
	rowsAffected := make([]int64, len(ticketDbIDs))
	for i, ticketDbID := range ticketDbIDs {
		rowsAffected[i], err = sqlExecStmt(stmt, "failed to set ticket spending info: ",
			ticketDbID, blockHeights[i], spendDbIDs[i], spendTypes[i], poolStatuses[i])
		if err != nil {
			_ = stmt.Close()
			return 0, dbtx.Rollback()
		}
		totalTicketsUpdated += rowsAffected[i]
		if rowsAffected[i] != 1 {
			log.Warnf("Updated spending info for %d tickets, expecting just 1!",
				rowsAffected[i])
		}
	}

	_ = stmt.Close()

	return totalTicketsUpdated, dbtx.Commit()
}

// --- addresses table ---

// InsertAddressRow inserts an AddressRow (input or output), returning the row
// ID in the addresses table of the inserted data.
/*
func InsertAddressRow(db *sql.DB, dbA *dbtypes.AddressRow, dupCheck, updateExistingRecords bool) (uint64, error) {
	sqlStmt := internal.MakeAddressRowInsertStatement(dupCheck, updateExistingRecords)
	var id uint64
	err := db.QueryRow(sqlStmt, dbA.Address, dbA.MatchingTxHash, dbA.TxHash,
		dbA.TxVinVoutIndex, dbA.VinVoutDbID, dbA.Value, dbA.TxBlockTime,
		dbA.IsFunding, dbA.ValidMainChain, dbA.TxType).Scan(&id)
	return id, err
}
*/

// insertAddressRowsDbTx is like InsertAddressRows, except that it takes a
// sql.Tx. The caller is required to Commit or Rollback the transaction
// depending on the returned error value.
func insertAddressRowsDbTx(dbTx *sql.Tx, dbAs []*dbtypes.AddressRow, dupCheck, updateExistingRecords bool) ([]uint64, error) {
	// Prepare the addresses row insert statement.
	stmt, err := dbTx.Prepare(internal.MakeAddressRowInsertStatement(dupCheck, updateExistingRecords))
	if err != nil {
		return nil, err
	}

	// Insert each addresses table row, storing the inserted row IDs.
	ids := make([]uint64, 0, len(dbAs))
	for _, dbA := range dbAs {
		var id uint64
		err := stmt.QueryRow(dbA.Address, dbA.MatchingTxHash, dbA.TxHash,
			dbA.TxVinVoutIndex, dbA.VinVoutDbID, dbA.Value, dbA.TxBlockTime,
			dbA.IsFunding, dbA.ValidMainChain, dbA.TxType).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				log.Errorf("failed to insert/update an AddressRow: %v", *dbA)
				continue
			}
			_ = stmt.Close() // try, but we want the QueryRow error back
			return nil, err
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return ids, nil
}

/*
// InsertAddressRows inserts multiple transaction inputs or outputs for certain
// addresses ([]AddressRow). The row IDs of the inserted data are returned.
func InsertAddressRows(db *sql.DB, dbAs []*dbtypes.AddressRow, dupCheck, updateExistingRecords bool) ([]uint64, error) {
	// Begin a new transaction.
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	ids, err := InsertAddressRowsDbTx(dbtx, dbAs, dupCheck, updateExistingRecords)
	if err != nil {
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
	}

	return ids, dbtx.Commit()
}

func retrieveAddressUnspent(ctx context.Context, db *sql.DB, address string) (count, totalAmount int64, err error) {
	err = db.QueryRowContext(ctx, internal.SelectAddressUnspentCountANDValue, address).
		Scan(&count, &totalAmount)
	return
}

func retrieveAddressSpent(ctx context.Context, db *sql.DB, address string) (count, totalAmount int64, err error) {
	err = db.QueryRowContext(ctx, internal.SelectAddressSpentCountANDValue, address).
		Scan(&count, &totalAmount)
	return
}
*/

// retrieveAddressTxsCount return the number of record groups, where grouping is
// done by a specified time interval, for an address.
// func retrieveAddressTxsCount(ctx context.Context, db *sql.DB, address, interval string) (count int64, err error) {
// 	err = db.QueryRowContext(ctx, internal.MakeSelectAddressTimeGroupingCount(interval), address).Scan(&count)
// 	return
// }

// retrieveAddressBalance gets the numbers of spent and unspent outpoints
// for the given address, the total amounts spent and unspent, the number of
// distinct spending transactions, and the fraction spent to and received from
// stake-related transactions.
func retrieveAddressBalance(ctx context.Context, db *sql.DB, address string) (balance *dbtypes.AddressBalance, err error) {
	// Never return nil *AddressBalance.
	balance = &dbtypes.AddressBalance{Address: address}

	// The sql.Tx does not have a timeout, as the individual queries will.
	var dbtx *sql.Tx
	dbtx, err = db.BeginTx(context.Background(), &sql.TxOptions{
		Isolation: sql.LevelDefault,
		ReadOnly:  true,
	})
	if err != nil {
		err = fmt.Errorf("unable to begin database transaction: %w", err)
		return
	}

	// Query for spent and unspent totals.
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectAddressSpentUnspentCountAndValue, address)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = dbtx.Commit()
			return
		}
		if errRoll := dbtx.Rollback(); errRoll != nil {
			log.Errorf("Rollback failed: %v", errRoll)
		}
		err = fmt.Errorf("failed to query spent and unspent amounts: %w", err)
		return
	}

	var fromStake, toStake int64
	for rows.Next() {
		var count, totalValue int64
		var noMatchingTx, isFunding, isRegular bool
		err = rows.Scan(&isRegular, &count, &totalValue, &isFunding, &noMatchingTx)
		if err != nil {
			return
		}

		// Unspent == funding with no matching transaction
		if isFunding && noMatchingTx {
			balance.NumUnspent += count
			balance.TotalUnspent += totalValue
		}
		// Spent == spending (but ensure a matching transaction is set)
		if !isFunding {
			if noMatchingTx {
				log.Errorf("Found spending transactions with matching_tx_hash"+
					" unset for %s!", address)
				continue
			}
			balance.NumSpent += count
			balance.TotalSpent += totalValue
			if !isRegular {
				toStake += totalValue
			}
		} else if !isRegular {
			fromStake += totalValue
		}
	}
	if err = rows.Err(); err != nil {
		return
	}

	totalTransfer := balance.TotalSpent + balance.TotalUnspent
	if totalTransfer > 0 {
		balance.FromStake = float64(fromStake) / float64(totalTransfer)
	}
	if balance.TotalSpent > 0 {
		balance.ToStake = float64(toStake) / float64(balance.TotalSpent)
	}
	closeRows(rows)

	err = dbtx.Commit()
	return
}

func countMergedSpendingTxns(ctx context.Context, db *sql.DB, address string) (count int64, err error) {
	return countMerged(ctx, db, address, internal.SelectAddressesMergedSpentCount)
}

func countMergedFundingTxns(ctx context.Context, db *sql.DB, address string) (count int64, err error) {
	return countMerged(ctx, db, address, internal.SelectAddressesMergedFundingCount)
}

func countMergedTxns(ctx context.Context, db *sql.DB, address string) (count int64, err error) {
	return countMerged(ctx, db, address, internal.SelectAddressesMergedCount)
}

func countMerged(ctx context.Context, db *sql.DB, address, query string) (count int64, err error) {
	// Query for merged transaction count.
	var dbtx *sql.Tx
	dbtx, err = db.BeginTx(context.Background(), &sql.TxOptions{
		Isolation: sql.LevelDefault,
		ReadOnly:  true,
	})
	if err != nil {
		err = fmt.Errorf("unable to begin database transaction: %w", err)
		return
	}

	var sqlCount sql.NullInt64
	err = dbtx.QueryRowContext(ctx, query, address).
		Scan(&sqlCount)
	if err != nil && err != sql.ErrNoRows {
		if errRoll := dbtx.Rollback(); errRoll != nil {
			log.Errorf("Rollback failed: %v", errRoll)
		}
		err = fmt.Errorf("failed to query merged spent count: %w", err)
		return
	}

	count = sqlCount.Int64
	if !sqlCount.Valid {
		log.Debug("Merged debit spent count is not valid")
	}

	err = dbtx.Commit()
	return
}

// retrieveAddressUTXOs gets the unspent transaction outputs (UTXOs) paying to
// the specified address as a []*apitypes.AddressTxnOutput. The input current
// block height is used to compute confirmations of the located transactions.
/*
func retrieveAddressUTXOs(ctx context.Context, db *sql.DB, address string, currentBlockHeight int64) ([]*apitypes.AddressTxnOutput, error) {
	stmt, err := db.Prepare(internal.SelectAddressUnspentWithTxn)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	rows, err := stmt.QueryContext(ctx, address)
	_ = stmt.Close()
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer closeRows(rows)

	var outputs []*apitypes.AddressTxnOutput
	for rows.Next() {
		pkScript := []byte{}
		var blockHeight, atoms int64
		var blockTime dbtypes.TimeDef
		txnOutput := new(apitypes.AddressTxnOutput)
		if err = rows.Scan(&txnOutput.Address, &txnOutput.TxnID,
			&atoms, &blockHeight, &blockTime, &txnOutput.Vout, &pkScript); err != nil {
			log.Error(err)
			return nil, err
		}
		txnOutput.BlockTime = blockTime.UNIX()
		txnOutput.ScriptPubKey = pkScript
		txnOutput.Amount = toCoin(atoms)
		txnOutput.Satoshis = atoms
		txnOutput.Height = blockHeight
		txnOutput.Confirmations = currentBlockHeight - blockHeight + 1
		outputs = append(outputs, txnOutput)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return outputs, nil
}
*/

// retrieveAddressDbUTXOs gets the unspent transaction outputs (UTXOs) paying to
// the specified address as a []*dbtypes.AddressTxnOutput. The input current
// block height is used to compute confirmations of the located transactions.
func retrieveAddressDbUTXOs(ctx context.Context, db *sql.DB, address string) ([]*dbtypes.AddressTxnOutput, error) {
	stmt, err := db.Prepare(internal.SelectAddressUnspentWithTxn)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	rows, err := stmt.QueryContext(ctx, address)
	_ = stmt.Close()
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer closeRows(rows)

	var outputs []*dbtypes.AddressTxnOutput
	for rows.Next() {
		var blockTime dbtypes.TimeDef
		txnOutput := new(dbtypes.AddressTxnOutput)
		if err = rows.Scan(&txnOutput.Address, &txnOutput.TxHash,
			&txnOutput.Atoms, &txnOutput.Height, &blockTime,
			&txnOutput.Vout); err != nil {
			log.Error(err)
			return nil, err
		}
		txnOutput.BlockTime = blockTime.UNIX()
		outputs = append(outputs, txnOutput)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return outputs, nil
}

// retrieveAddressTxnsOrdered will get all transactions for addresses provided
// and return them sorted by time in descending order. It will also return a
// short list of recently (defined as greater than recentBlockHeight) confirmed
// transactions that can be used to validate mempool status.
/*
func retrieveAddressTxnsOrdered(ctx context.Context, db *sql.DB, addresses []string,
	recentBlockTime int64) (txs, recenttxs []chainhash.Hash, err error) {
	var stmt *sql.Stmt
	stmt, err = db.Prepare(internal.SelectAddressesAllTxn)
	if err != nil {
		return nil, nil, err
	}

	var rows *sql.Rows
	rows, err = stmt.QueryContext(ctx, pq.Array(addresses))
	_ = stmt.Close()
	if err != nil {
		return nil, nil, err
	}
	defer closeRows(rows)

	var tx *chainhash.Hash
	var txHash string
	var time dbtypes.TimeDef
	for rows.Next() {
		err = rows.Scan(&txHash, &time)
		if err != nil {
			return // return what we got, plus the error
		}
		tx, err = chainhash.NewHashFromStr(txHash)
		if err != nil {
			return
		}
		txs = append(txs, *tx)
		if time.UNIX() > recentBlockTime {
			recenttxs = append(recenttxs, *tx)
		}
	}
	err = rows.Err()

	return
}
*/

// retrieveAllAddressTxns retrieves all rows of the address table pertaining to
// the given address.
/*
func retrieveAllAddressTxns(ctx context.Context, db *sql.DB, address string) ([]*dbtypes.AddressRow, error) {
	rows, err := db.QueryContext(ctx, internal.SelectAddressAllByAddress, address)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	return scanAddressQueryRows(rows, creditDebitQuery)
}
*/

// retrieveAllMainchainAddressTxns retrieves all non-merged and valid_mainchain
// rows of the address table pertaining to the given address. For a limited
// query, use retrieveAddressTxns.
/*
func retrieveAllMainchainAddressTxns(ctx context.Context, db *sql.DB, address string) ([]*dbtypes.AddressRow, error) {
	rows, err := db.QueryContext(ctx, internal.SelectAddressAllMainchainByAddress, address)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	return scanAddressQueryRows(rows, creditDebitQuery)
}
*/

// retrieveAllAddressMergedTxns retrieves all merged rows of the address table
// pertaining to the given address. Specify only valid_mainchain=true rows via
// the onlyValidMainchain argument. For a limited query, use
// retrieveAddressMergedTxns.
/*
func retrieveAllAddressMergedTxns(ctx context.Context, db *sql.DB, address string, onlyValidMainchain bool) ([]uint64, []*dbtypes.AddressRow, error) {
	rows, err := db.QueryContext(ctx, internal.SelectAddressMergedViewAll, address)
	if err != nil {
		return nil, nil, err
	}
	defer closeRows(rows)

	addr, err := scanAddressMergedRows(rows, address, mergedQuery,
		onlyValidMainchain)
	return nil, addr, err
}
*/

// Regular (non-merged) address transactions queries.

func retrieveAddressTxns(ctx context.Context, db *sql.DB, address string, N, offset int64) ([]*dbtypes.AddressRow, error) {
	return retrieveAddressTxnsStmt(ctx, db, address, N, offset,
		internal.SelectAddressLimitNByAddress, creditDebitQuery)
}

// Merged address transactions queries.

func retrieveAddressMergedTxns(ctx context.Context, db *sql.DB, address string, N, offset int64) ([]*dbtypes.AddressRow, error) {
	return retrieveAddressTxnsStmt(ctx, db, address, N, offset,
		internal.SelectAddressMergedView, mergedQuery)
}

// Address transaction query helpers.

func retrieveAddressTxnsStmt(ctx context.Context, db *sql.DB, address string, N, offset int64,
	statement string, queryType int) ([]*dbtypes.AddressRow, error) {
	rows, err := db.QueryContext(ctx, statement, address, N, offset)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	switch queryType {
	case mergedCreditQuery, mergedDebitQuery, mergedQuery:
		onlyValidMainchain := true
		addr, err := scanAddressMergedRows(rows, address, queryType, onlyValidMainchain)
		return addr, err
	default:
		return scanAddressQueryRows(rows, queryType)
	}
}

func scanAddressMergedRows(rows *sql.Rows, addr string, queryType int, onlyValidMainchain bool) (addressRows []*dbtypes.AddressRow, err error) {
	for rows.Next() {
		addr := dbtypes.AddressRow{Address: addr}

		var value int64
		switch queryType {
		case mergedCreditQuery:
			addr.IsFunding = true
			fallthrough
		case mergedDebitQuery:
			err = rows.Scan(&addr.TxHash, &addr.ValidMainChain, &addr.TxBlockTime,
				&value, &addr.MergedCount)
		case mergedQuery:
			err = rows.Scan(&addr.TxHash, &addr.ValidMainChain, &addr.TxBlockTime,
				&addr.AtomsCredit, &addr.AtomsDebit, &addr.MergedCount)
			value = int64(addr.AtomsCredit) - int64(addr.AtomsDebit)
			addr.IsFunding = value >= 0
			if !addr.IsFunding {
				value = -value
			}
		default:
			err = fmt.Errorf("invalid query %v", queryType)
		}

		if err != nil {
			return
		}

		if onlyValidMainchain && !addr.ValidMainChain {
			continue
		}

		addr.Value = uint64(value)

		addressRows = append(addressRows, &addr)
	}
	err = rows.Err()

	return
}

func scanAddressQueryRows(rows *sql.Rows, queryType int) (addressRows []*dbtypes.AddressRow, err error) {
	for rows.Next() {
		var id uint64
		var addr dbtypes.AddressRow
		var txVinIndex, vinDbID sql.NullInt64

		err = rows.Scan(&id, &addr.Address, &addr.MatchingTxHash, &addr.TxHash, &addr.TxType,
			&addr.ValidMainChain, &txVinIndex, &addr.TxBlockTime, &vinDbID,
			&addr.Value, &addr.IsFunding)

		if err != nil {
			return
		}

		switch queryType {
		case creditQuery:
			addr.AtomsCredit = addr.Value
		case debitQuery:
			addr.AtomsDebit = addr.Value
		case creditDebitQuery:
			if addr.IsFunding {
				addr.AtomsCredit = addr.Value
			} else {
				addr.AtomsDebit = addr.Value
			}
		default:
			log.Warnf("Unrecognized addresses query type: %d", queryType)
		}

		if txVinIndex.Valid {
			addr.TxVinVoutIndex = uint32(txVinIndex.Int64)
		}
		if vinDbID.Valid {
			addr.VinVoutDbID = uint64(vinDbID.Int64)
		}

		addressRows = append(addressRows, &addr)
	}
	err = rows.Err()

	return
}

// retrieveAddressIDsByOutpoint gets all address row IDs, addresses, and values
// for a given outpoint.
func retrieveAddressIDsByOutpoint(ctx context.Context, db *sql.DB, txHash dbtypes.ChainHash, voutIndex uint32) ([]uint64, []string, int64, error) {
	var ids []uint64
	var addresses []string
	var value int64
	rows, err := db.QueryContext(ctx, internal.SelectAddressIDsByFundingOutpoint, txHash, voutIndex)
	if err != nil {
		return nil, nil, 0, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var addr string
		err = rows.Scan(&id, &addr, &value)
		if err != nil {
			return nil, nil, 0, err
		}

		ids = append(ids, id)
		addresses = append(addresses, addr)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, 0, err
	}

	return ids, addresses, value, err
}

// retrieveOldestTxBlockTime helps choose the most appropriate address page
// graph grouping to load by default depending on when the first transaction to
// the specific address was made.
// func retrieveOldestTxBlockTime(ctx context.Context, db *sql.DB, addr string) (blockTime dbtypes.TimeDef, err error) {
// 	err = db.QueryRowContext(ctx, internal.SelectAddressOldestTxBlockTime, addr).Scan(&blockTime)
// 	return
// }

// retrieveTxHistoryByType fetches the transaction types count for all the
// transactions associated with a given address for the given time interval.
// The time interval is grouping records by week, month, year, day and all.
// For all time interval, transactions are grouped by the unique
// timestamps (blocks) available.
func retrieveTxHistoryByType(ctx context.Context, db *sql.DB, addr, timeInterval string) (*dbtypes.ChartsData, error) {
	rows, err := db.QueryContext(ctx, internal.MakeSelectAddressTxTypesByAddress(timeInterval),
		addr)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	items := new(dbtypes.ChartsData)
	for rows.Next() {
		var blockTime time.Time
		var tickets, votes, revokeTx uint32
		var sentRtx, receivedRtx uint64
		err = rows.Scan(&blockTime, &sentRtx, &receivedRtx, &tickets, &votes, &revokeTx)
		if err != nil {
			return nil, err
		}

		items.Time = append(items.Time, dbtypes.NewTimeDef(blockTime))
		items.SentRtx = append(items.SentRtx, sentRtx)
		items.ReceivedRtx = append(items.ReceivedRtx, receivedRtx)
		items.Tickets = append(items.Tickets, tickets)
		items.Votes = append(items.Votes, votes)
		items.RevokeTx = append(items.RevokeTx, revokeTx)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// retrieveTxHistoryByAmount fetches the transaction amount flow i.e. received
// and sent amount for all the transactions associated with a given address and for
// the given time interval. The time interval is grouping records by week,
// month, year, day and all. For all time interval, transactions are grouped by
// the unique timestamps (blocks) available.
func retrieveTxHistoryByAmountFlow(ctx context.Context, db *sql.DB, addr, timeInterval string) (*dbtypes.ChartsData, error) {
	rows, err := db.QueryContext(ctx, internal.MakeSelectAddressAmountFlowByAddress(timeInterval), addr)
	if err != nil {
		return nil, err
	}
	return parseRowsSentReceived(rows)
}

// --- vins and vouts tables ---

// InsertVin either inserts, attempts to insert, or upserts the given vin data
// into the vins table. If checked=false, an unconditional insert as attempted,
// which may result in a violation of a unique index constraint (error). If
// checked=true, a constraint violation may be handled in one of two ways:
// update the conflicting row (upsert), or do nothing. In all cases, the id of
// the new/updated/conflicting row is returned. The updateOnConflict argument
// may be omitted, in which case an upsert will be favored over no nothing, but
// only if checked=true.
/*
func InsertVin(db *sql.DB, dbVin dbtypes.VinTxProperty, checked bool, updateOnConflict ...bool) (id uint64, err error) {
	doUpsert := true
	if len(updateOnConflict) > 0 {
		doUpsert = updateOnConflict[0]
	}
	err = db.QueryRow(internal.MakeVinInsertStatement(checked, doUpsert),
		dbVin.TxID, dbVin.TxIndex, dbVin.TxTree,
		dbVin.PrevTxHash, dbVin.PrevTxIndex, dbVin.PrevTxTree,
		dbVin.ValueIn, dbVin.IsValid, dbVin.IsMainchain, dbVin.Time,
		dbVin.TxType).Scan(&id)
	return
}
*/

// insertVinsStmt is like InsertVins, except that it takes a sql.Stmt. The
// caller is required to Close the transaction.
func insertVinsStmt(stmt *sql.Stmt, dbVins dbtypes.VinTxPropertyARRAY) ([]uint64, error) {
	// TODO/Question: Should we skip inserting coinbase txns, which have same PrevTxHash?
	ids := make([]uint64, 0, len(dbVins))
	for _, vin := range dbVins {
		var id uint64
		err := stmt.QueryRow(vin.TxID, vin.TxIndex, vin.TxTree,
			vin.PrevTxHash, vin.PrevTxIndex, vin.PrevTxTree,
			vin.ValueIn, vin.IsValid, vin.IsMainchain, vin.Time, vin.TxType).Scan(&id)
		if err != nil {
			return ids, fmt.Errorf("InsertVins INSERT exec failed: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// InsertVinsDbTxn is like InsertVins, except that it takes a sql.Tx. The caller
// is required to Commit or Rollback the transaction depending on the returned
// error value.
/*
func InsertVinsDbTxn(dbTx *sql.Tx, dbVins dbtypes.VinTxPropertyARRAY, checked bool, doUpsert bool) ([]uint64, error) {
	stmt, err := dbTx.Prepare(internal.MakeVinInsertStatement(checked, doUpsert))
	if err != nil {
		return nil, err
	}

	// TODO/Question: Should we skip inserting coinbase txns, which have same PrevTxHash?

	ids, err := InsertVinsStmt(stmt, dbVins, checked, doUpsert)
	errClose := stmt.Close()
	if err != nil {
		return nil, err
	}
	if errClose != nil {
		return nil, err
	}
	return ids, nil
}
*/

// InsertVins is like InsertVin, except that it operates on a slice of vin data.
/*
func InsertVins(db *sql.DB, dbVins dbtypes.VinTxPropertyARRAY, checked bool, updateOnConflict ...bool) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	doUpsert := true
	if len(updateOnConflict) > 0 {
		doUpsert = updateOnConflict[0]
	}

	ids, err := InsertVinsDbTxn(dbtx, dbVins, checked, doUpsert)
	if err != nil {
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
	}

	return ids, dbtx.Commit()
}
*/

// InsertVout either inserts, attempts to insert, or upserts the given vout data
// into the vouts table. If checked=false, an unconditional insert as attempted,
// which may result in a violation of a unique index constraint (error). If
// checked=true, a constraint violation may be handled in one of two ways:
// update the conflicting row (upsert), or do nothing. In all cases, the id of
// the new/updated/conflicting row is returned. The updateOnConflict argument
// may be omitted, in which case an upsert will be favored over no nothing, but
// only if checked=true.
/*
func InsertVout(db *sql.DB, dbVout *dbtypes.Vout, checked bool, updateOnConflict ...bool) (uint64, error) {
	doUpsert := true
	if len(updateOnConflict) > 0 {
		doUpsert = updateOnConflict[0]
	}
	insertStatement := internal.MakeVoutInsertStatement(checked, doUpsert)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbVout.TxHash, dbVout.TxIndex, dbVout.TxTree,
		dbVout.Value, int32(dbVout.Version),
		dbVout.ScriptPubKey, int32(dbVout.ScriptPubKeyData.ReqSigs),
		dbVout.ScriptPubKeyData.Type,
		pq.Array(dbVout.ScriptPubKeyData.Addresses)).Scan(&id)
	return id, err
}
*/

type addressList []string

func (al addressList) Value() (driver.Value, error) {
	switch len(al) {
	case 0:
		return "unknown", nil
	case 1:
		return al[0], nil
	}

	return pq.StringArray(al).Value()
}

func (al *addressList) Scan(src interface{}) error {
	switch src := src.(type) {
	case string:
		switch src {
		case "unknown", "{}":
			*al = []string{}
			return nil
		}
		if len(src) > 2 && src[0] == '{' && src[len(src)-1] == '}' {
			*al = strings.Split(src, ",")
			return nil
		}
		*al = []string{src}
		return nil
	default:
		return errors.New("not an addressList")
	}
}

// insertVoutsStmt is like InsertVouts, except that it takes a sql.Stmt. The
// caller is required to Close the statement.
func insertVoutsStmt(stmt *sql.Stmt, dbVouts []*dbtypes.Vout) ([]uint64, []dbtypes.AddressRow, error) {
	addressRows := make([]dbtypes.AddressRow, 0, len(dbVouts)) // may grow with multisig
	ids := make([]uint64, 0, len(dbVouts))
	for _, vout := range dbVouts {
		var id uint64
		err := stmt.QueryRow(
			vout.TxHash, vout.TxIndex, vout.TxTree, vout.Value, int32(vout.Version),
			// vout.ScriptPubKey, int32(vout.ScriptPubKeyData.ReqSigs),
			vout.ScriptPubKeyData.Type,
			addressList(vout.ScriptPubKeyData.Addresses), vout.Mixed).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, nil, err
		}

		for _, addr := range vout.ScriptPubKeyData.Addresses {
			addressRows = append(addressRows, dbtypes.AddressRow{
				Address:        addr,
				TxHash:         vout.TxHash,
				TxVinVoutIndex: vout.TxIndex,
				VinVoutDbID:    id,
				TxType:         vout.TxType,
				Value:          vout.Value,
				// Not set here are: ValidMainchain, MatchingTxHash, IsFunding,
				// AtomsCredit, AtomsDebit, and TxBlockTime.
			})
		}
		ids = append(ids, id)
	}

	return ids, addressRows, nil
}

// InsertVoutsDbTxn is like InsertVouts, except that it takes a sql.Tx. The
// caller is required to Commit or Rollback the transaction depending on the
// returned error value.
/*
func InsertVoutsDbTxn(dbTx *sql.Tx, dbVouts []*dbtypes.Vout, checked bool, doUpsert bool) ([]uint64, []dbtypes.AddressRow, error) {
	stmt, err := dbTx.Prepare(internal.MakeVoutInsertStatement(checked, doUpsert))
	if err != nil {
		return nil, nil, err
	}

	ids, addressRows, err := InsertVoutsStmt(stmt, dbVouts, checked, doUpsert)
	errClose := stmt.Close()
	if err != nil {
		return nil, nil, err
	}
	if errClose != nil {
		return nil, nil, err
	}

	return ids, addressRows, stmt.Close()
}
*/

// InsertVouts is like InsertVout, except that it operates on a slice of vout
// data.
/*
func InsertVouts(db *sql.DB, dbVouts []*dbtypes.Vout, checked bool, updateOnConflict ...bool) ([]uint64, []dbtypes.AddressRow, error) {
	// All inserts in atomic DB transaction
	dbTx, err := db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	doUpsert := true
	if len(updateOnConflict) > 0 {
		doUpsert = updateOnConflict[0]
	}

	ids, addressRows, err := InsertVoutsDbTxn(dbTx, dbVouts, checked, doUpsert)
	if err != nil {
		_ = dbTx.Rollback() // try, but we want the Prepare error back
		return nil, nil, err
	}

	return ids, addressRows, dbTx.Commit()
}
*/

// func retrievePkScriptByVinID(ctx context.Context, db *sql.DB, vinID uint64) (pkScript []byte, ver uint16, err error) {
// 	err = db.QueryRowContext(ctx, internal.SelectPkScriptByVinID, vinID).Scan(&ver, &pkScript)
// 	return
// }

/*
func retrievePkScriptByVoutID(ctx context.Context, db *sql.DB, voutID uint64) (pkScript []byte, ver uint16, err error) {
	err = db.QueryRowContext(ctx, internal.SelectPkScriptByID, voutID).Scan(&ver, &pkScript)
	return
}

func retrievePkScriptByOutpoint(ctx context.Context, db *sql.DB, txHash string, voutIndex uint32) (pkScript []byte, ver uint16, err error) {
	err = db.QueryRowContext(ctx, internal.SelectPkScriptByOutpoint, txHash, voutIndex).Scan(&ver, &pkScript)
	return
}

func retrieveVoutIDByOutpoint(ctx context.Context, db *sql.DB, txHash string, voutIndex uint32) (id uint64, err error) {
	err = db.QueryRowContext(ctx, internal.SelectVoutIDByOutpoint, txHash, voutIndex).Scan(&id)
	return
}
*/

// TEST ONLY REMOVE
func retrieveVoutValue(ctx context.Context, db *sql.DB, txHash dbtypes.ChainHash, voutIndex uint32) (value uint64, err error) {
	err = db.QueryRowContext(ctx, internal.RetrieveVoutValue, txHash, voutIndex).Scan(&value)
	return
}

// TEST ONLY REMOVE
func retrieveVoutValues(ctx context.Context, db *sql.DB, txHash dbtypes.ChainHash) (values []uint64, txInds []uint32, txTrees []int8, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.RetrieveVoutValues, txHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var v uint64
		var ind uint32
		var tree int8
		err = rows.Scan(&v, &ind, &tree)
		if err != nil {
			return
		}

		values = append(values, v)
		txInds = append(txInds, ind)
		txTrees = append(txTrees, tree)
	}
	err = rows.Err()

	return
}

// retrieveAllVinDbIDs gets every row ID (the primary keys) for the vins table.
// This function is used in UpdateSpendingInfoInAllAddresses, so it should not
// be subject to timeouts.
/* unused
func retrieveAllVinDbIDs(db *sql.DB) (vinDbIDs []uint64, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectVinIDsALL)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		err = rows.Scan(&id)
		if err != nil {
			return
		}

		vinDbIDs = append(vinDbIDs, id)
	}
	err = rows.Err()

	return
}
*/

// retrieveFundingOutpointByTxIn gets the previous outpoint for a transaction
// input specified by transaction hash and input index.
/* unused
func retrieveFundingOutpointByTxIn(ctx context.Context, db *sql.DB, txHash string,
	vinIndex uint32) (id uint64, tx string, index uint32, tree int8, err error) {
	err = db.QueryRowContext(ctx, internal.SelectFundingOutpointByTxIn, txHash, vinIndex).
		Scan(&id, &tx, &index, &tree)
	return
}
*/

// retrieveFundingOutpointByVinID gets the previous outpoint for a transaction
// input specified by row ID in the vins table.
/* unused
func retrieveFundingOutpointByVinID(ctx context.Context, db *sql.DB, vinDbID uint64) (tx string, index uint32, tree int8, err error) {
	err = db.QueryRowContext(ctx, internal.SelectFundingOutpointByVinID, vinDbID).
		Scan(&tx, &index, &tree)
	return
}
*/

// retrieveFundingOutpointIndxByVinID gets the transaction output index of the
// previous outpoint for a transaction input specified by row ID in the vins
// table.
func retrieveFundingOutpointIndxByVinID(ctx context.Context, db *sql.DB, vinDbID uint64) (idx uint32, err error) {
	err = db.QueryRowContext(ctx, internal.SelectFundingOutpointIndxByVinID, vinDbID).Scan(&idx)
	return
}

// retrieveFundingTxByTxIn gets the transaction hash of the previous outpoint
// for a transaction input specified by hash and input index.
/* unused
func retrieveFundingTxByTxIn(ctx context.Context, db *sql.DB, txHash string, vinIndex uint32) (id uint64, tx string, err error) {
	err = db.QueryRowContext(ctx, internal.SelectFundingTxByTxIn, txHash, vinIndex).
		Scan(&id, &tx)
	return
}
*/

// retrieveFundingTxByVinDbID gets the transaction hash of the previous outpoint
// for a transaction input specified by row ID in the vins table. This function
// is used only in UpdateSpendingInfoInAllTickets, so it should not be subject
// to timeouts.
// func retrieveFundingTxByVinDbID(ctx context.Context, db *sql.DB, vinDbID uint64) (tx string, err error) {
// 	err = db.QueryRowContext(ctx, internal.SelectFundingTxByVinID, vinDbID).Scan(&tx)
// 	return
// }

// retrieveSpendingTxByVinID gets the spending transaction input (hash, vin
// number, and tx tree) for the transaction input specified by row ID in the
// vins table.
/* unused
func retrieveSpendingTxByVinID(ctx context.Context, db *sql.DB, vinDbID uint64) (tx string,
	vinIndex uint32, tree int8, err error) {
	err = db.QueryRowContext(ctx, internal.SelectSpendingTxByVinID, vinDbID).
		Scan(&tx, &vinIndex, &tree)
	return
}
*/

// retrieveSpendingTxByTxOut gets any spending transaction input info for a
// previous outpoint specified by funding transaction hash and vout number. This
// function is called by SpendingTransaction, an important part of the address
// page loading.
func retrieveSpendingTxByTxOut(ctx context.Context, db *sql.DB, txHash dbtypes.ChainHash,
	voutIndex uint32) (id uint64, tx dbtypes.ChainHash, vin uint32, err error) {
	err = db.QueryRowContext(ctx, internal.SelectSpendingTxByPrevOut,
		txHash, voutIndex).Scan(&id, &tx, &vin)
	return
}

// retrieveSpendingTxsByFundingTx gets info on all spending transaction inputs
// for the given funding transaction specified by DB row ID. This function is
// called by SpendingTransactions, an important part of the transaction page
// loading, among other functions..
func retrieveSpendingTxsByFundingTx(ctx context.Context, db *sql.DB, fundingTxID dbtypes.ChainHash) (dbIDs []uint64,
	txns []dbtypes.ChainHash, vinInds []uint32, voutInds []uint32, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectSpendingTxsByPrevTx, fundingTxID)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var txHash dbtypes.ChainHash
		var vin, vout uint32
		err = rows.Scan(&id, &txHash, &vin, &vout)
		if err != nil {
			return
		}

		dbIDs = append(dbIDs, id)
		txns = append(txns, txHash)
		vinInds = append(vinInds, vin)
		voutInds = append(voutInds, vout)
	}
	err = rows.Err()

	return
}

// retrieveSpendingTxsByFundingTxWithBlockHeight will retrieve all transactions,
// indexes and block heights funded by a specific transaction. This function is
// used by the DCR to Insight transaction converter.
func retrieveSpendingTxsByFundingTxWithBlockHeight(ctx context.Context, db *sql.DB, fundingTxID dbtypes.ChainHash) (aSpendByFunHash []*apitypes.SpendByFundingHash, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectSpendingTxsByPrevTxWithBlockHeight, fundingTxID)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var addr apitypes.SpendByFundingHash
		err = rows.Scan(&addr.FundingTxVoutIndex,
			&addr.SpendingTxHash, &addr.SpendingTxVinIndex, &addr.BlockHeight)
		if err != nil {
			return
		}

		aSpendByFunHash = append(aSpendByFunHash, &addr)
	}
	err = rows.Err()

	return
}

// retrieveVinByID gets from the vins table for the provided row ID.
/* unused
func retrieveVinByID(ctx context.Context, db *sql.DB, vinDbID uint64) (prevOutHash string, prevOutVoutInd uint32,
	prevOutTree int8, txHash string, txVinInd uint32, txTree int8, valueIn int64, err error) {
	var blockTime dbtypes.TimeDef
	var isValid, isMainchain bool
	var txType uint32
	err = db.QueryRowContext(ctx, internal.SelectAllVinInfoByID, vinDbID).
		Scan(&txHash, &txVinInd, &txTree, &isValid, &isMainchain, &blockTime,
			&prevOutHash, &prevOutVoutInd, &prevOutTree, &valueIn, &txType)
	return
}
*/

// retrieveVinsByIDs retrieves vin details for the rows of the vins table
// specified by the provided row IDs. This function is an important part of the
// transaction page.
func retrieveVinsByIDs(ctx context.Context, db *sql.DB, vinDbIDs []uint64) ([]dbtypes.VinTxProperty, error) {
	vins := make([]dbtypes.VinTxProperty, len(vinDbIDs))
	for i, id := range vinDbIDs {
		vin := &vins[i]
		err := db.QueryRowContext(ctx, internal.SelectAllVinInfoByID, id).Scan(&vin.TxID,
			&vin.TxIndex, &vin.TxTree, &vin.IsValid, &vin.IsMainchain,
			&vin.Time, &vin.PrevTxHash, &vin.PrevTxIndex, &vin.PrevTxTree,
			&vin.ValueIn, &vin.TxType)
		if err != nil {
			return nil, err
		}
	}
	return vins, nil
}

// retrieveVoutsByIDs retrieves vout details for the rows of the vouts table
// specified by the provided row IDs. This function is an important part of the
// transaction page.
func retrieveVoutsByIDs(ctx context.Context, db *sql.DB, voutDbIDs []uint64) ([]dbtypes.Vout, error) {
	vouts := make([]dbtypes.Vout, len(voutDbIDs))
	for i, id := range voutDbIDs {
		vout := &vouts[i]
		var id0 uint64
		var spendTxRowID sql.NullInt64 // discarded, but can be NULL
		// var reqSigs uint32
		var addresses addressList
		var scriptClass dbtypes.ScriptClass // or scan a string and then dbtypes.NewScriptClassFromString(scriptTypeString)
		err := db.QueryRowContext(ctx, internal.SelectVoutByID, id).Scan(&id0, &vout.TxHash,
			&vout.TxIndex, &vout.TxTree, &vout.Value, &vout.Version,
			/* &vout.ScriptPubKey, &reqSigs, */ &scriptClass, &addresses, &vout.Mixed, &spendTxRowID)
		if err != nil {
			return nil, err
		}
		// Parse the addresses array
		// replacer := strings.NewReplacer("{", "", "}", "")
		// addresses = replacer.Replace(addresses)

		// vout.ScriptPubKeyData.ReqSigs = reqSigs
		vout.ScriptPubKeyData.Type = scriptClass
		// If there are no addresses, the Addresses should be nil or length
		// zero. However, strings.Split will return [""] if addresses is "".
		// If that is the case, leave it as a nil slice.
		if len(addresses) > 0 {
			vout.ScriptPubKeyData.Addresses = addresses // strings.Split(addresses, ",")
		}
	}
	return vouts, nil
}

// func retrieveUTXOsByVinsJoin(ctx context.Context, db *sql.DB) ([]dbtypes.UTXO, error) {
// 	return retrieveUTXOsStmt(ctx, db, internal.SelectUTXOsViaVinsMatch)
// }

// retrieveUTXOs gets the entire UTXO set from the vouts and vins tables.
func retrieveUTXOs(ctx context.Context, db *sql.DB) ([]dbtypes.UTXO, error) {
	return retrieveUTXOsStmt(ctx, db, internal.SelectUTXOs)
}

// retrieveUTXOsStmt gets the entire UTXO set from the vouts and vins tables.
func retrieveUTXOsStmt(ctx context.Context, db *sql.DB, stmt string) ([]dbtypes.UTXO, error) {
	_, height, err := dbBestBlock(ctx, db)
	if err != nil {
		return nil, err
	}

	if height < 1 {
		return nil, nil
	}

	initUTXOCap := 250 * height / 100
	utxos := make([]dbtypes.UTXO, 0, initUTXOCap)

	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	replacer := strings.NewReplacer("{", "", "}", "")

	for rows.Next() {
		var addresses string
		var utxo dbtypes.UTXO
		err = rows.Scan(&utxo.VoutDbID, &utxo.TxHash, &utxo.TxIndex, &addresses, &utxo.Value, &utxo.Mixed)
		if err != nil {
			return nil, err
		}

		// Remove curly brackets from array notation.
		addresses = replacer.Replace(addresses)
		// nil slice is preferred over [""].
		if len(addresses) > 0 {
			utxo.Addresses = strings.Split(addresses, ",")
			if len(utxo.Addresses) > 1 {
				log.Debugf("multisig: %s:%d", utxo.TxHash, utxo.TxIndex)
			}
		}

		utxos = append(utxos, utxo)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return utxos, nil
}

// SetSpendingForVinDbIDs updates rows of the addresses table with spending
// information from the rows of the vins table specified by vinDbIDs. This does
// not insert the spending transaction into the addresses table.
/*
func SetSpendingForVinDbIDs(db *sql.DB, vinDbIDs []uint64) ([]int64, int64, error) {
	// Get funding details for vin and set them in the address table.
	dbtx, err := db.Begin()
	if err != nil {
		return nil, 0, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	var vinGetStmt *sql.Stmt
	vinGetStmt, err = dbtx.Prepare(internal.SelectVinVoutPairByID)
	if err != nil {
		log.Errorf("Vin SELECT prepare failed: %v", err)
		// Already up a creek. Just return error from Prepare.
		_ = dbtx.Rollback()
		return nil, 0, err
	}

	bail := func() error {
		// Already up a creek. Just return error from Prepare.
		_ = vinGetStmt.Close()
		return dbtx.Rollback()
	}

	addressRowsUpdated := make([]int64, len(vinDbIDs))
	var totalUpdated int64

	for iv, vinDbID := range vinDbIDs {
		// Get the funding tx outpoint from the vins table.
		var prevOutHash, txHash string
		var prevOutVoutInd, txVinInd uint32
		err = vinGetStmt.QueryRow(vinDbID).Scan(
			&txHash, &txVinInd, &prevOutHash, &prevOutVoutInd)
		if err != nil {
			return addressRowsUpdated, 0, fmt.Errorf(`SelectVinVoutPairByID: `+
				`%w + %v (rollback)`, err, bail())
		}

		// Skip coinbase inputs.
		if bytes.Equal(zeroHashStringBytes, []byte(prevOutHash)) {
			continue
		}

		// Set the spending tx info (addresses table) for the funding transaction
		// rows indicated by the vin DB ID.
		addressRowsUpdated[iv], err = SetSpendingForFundingOP(dbtx,
			prevOutHash, prevOutVoutInd, txHash, true)
		if err != nil {
			return addressRowsUpdated, 0, fmt.Errorf(`insertSpendingTxByPrptStmt: `+
				`%w + %v (rollback)`, err, bail())
		}

		totalUpdated += addressRowsUpdated[iv]
	}

	// Close prepared statements. Ignore errors as we'll Commit regardless.
	_ = vinGetStmt.Close()

	return addressRowsUpdated, totalUpdated, dbtx.Commit()
}
*/

// SetSpendingForVinDbID updates rows of the addresses table with spending
// information from the row of the vins table specified by vinDbID. This does
// not insert the spending transaction into the addresses table.
/*
func SetSpendingForVinDbID(db *sql.DB, vinDbID uint64) (int64, error) {
	// Get funding details for the vin and set them in the address table.
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	// Get the funding tx outpoint from the vins table.
	var prevOutHash, txHash string
	var prevOutVoutInd, txVinInd uint32
	err = dbtx.QueryRow(internal.SelectVinVoutPairByID, vinDbID).
		Scan(&txHash, &txVinInd, &prevOutHash, &prevOutVoutInd)
	if err != nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %w + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	// Skip coinbase inputs.
	if bytes.Equal(zeroHashStringBytes, []byte(prevOutHash)) {
		return 0, dbtx.Rollback()
	}

	// Set the spending tx info (addresses table) for the funding transaction
	// rows indicated by the vin DB ID.
	N, err := SetSpendingForFundingOP(dbtx, prevOutHash, prevOutVoutInd,
		txHash, true)
	if err != nil {
		return 0, fmt.Errorf(`RowsAffected: %w + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return N, dbtx.Commit()
}
*/

// setSpendingForFundingOP updates funding rows of the addresses table with the
// provided spending transaction output info. Only update rows of mainchain or
// side chain transactions according to forMainchain. Technically
// forMainchain=false also permits updating rows that are stake invalidated, but
// consensus-validated transactions cannot spend outputs from stake-invalidated
// transactions so the funding tx must not be invalid.
func setSpendingForFundingOP(db SqlExecutor, fundingTxHash dbtypes.ChainHash, fundingTxVoutIndex uint32,
	spendingTxHash dbtypes.ChainHash, forMainchain bool) (int64, error) {
	// Update the matchingTxHash for the funding tx output. matchingTxHash here
	// is the hash of the funding tx.
	res, err := db.Exec(internal.SetAddressMatchingTxHashForOutpoint,
		spendingTxHash, fundingTxHash, fundingTxVoutIndex, forMainchain)
	if err != nil || res == nil {
		return 0, fmt.Errorf("SetAddressMatchingTxHashForOutpoint: %w", err)
	}

	return res.RowsAffected()
}

func setSpendingForVout(tx *sql.Tx, fundVoutRowID int64, spendTxRowID uint64) error {
	res, err := tx.Exec(internal.UpdateVoutSpendTxRowID, spendTxRowID, fundVoutRowID)
	if err != nil || res == nil {
		return err
	}

	N, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if N != 1 {
		return fmt.Errorf("updated %d rows, expected 1", N)
	}
	return nil
}

func setSpendingForVouts(tx *sql.Tx, fundVoutRowIDs []int64, spendTxRowID uint64) error {
	if len(fundVoutRowIDs) == 1 {
		return setSpendingForVout(tx, fundVoutRowIDs[0], spendTxRowID)
	}

	res, err := tx.Exec(internal.UpdateVoutsSpendTxRowID, spendTxRowID, pq.Int64Array(fundVoutRowIDs))
	if err != nil || res == nil {
		return err
	}

	N, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if N != int64(len(fundVoutRowIDs)) {
		return fmt.Errorf("updated %d rows, expected %d", N, len(fundVoutRowIDs))
	}
	return nil
}

func resetSpendingForVoutsByTxRowID(tx *sql.Tx, spendingTxRowIDs []int64) (int64, error) {
	res, err := tx.Exec(internal.ResetVoutSpendTxRowIDs, pq.Int64Array(spendingTxRowIDs))
	if err != nil || res == nil {
		return 0, err
	}

	return res.RowsAffected()
}

// InsertSpendingAddressRow inserts a new spending tx row, and updates any
// corresponding funding tx row.
/*
func InsertSpendingAddressRow(db *sql.DB, fundingTxHash string, fundingTxVoutIndex uint32, fundingTxTree int8,
	spendingTxHash string, spendingTxVinIndex uint32, vinDbID uint64, utxoData *dbtypes.UTXOData,
	checked, updateExisting, mainchain, valid bool, txType int16, updateFundingRow bool,
	spendingTXBlockTime dbtypes.TimeDef) ([]string, int64, int64, bool, error) {
	// Only allow atomic transactions to happen.
	dbtx, err := db.Begin()
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	fromAddrs, c, voutDbID, mixedOut, err := insertSpendingAddressRow(dbtx, fundingTxHash, fundingTxVoutIndex,
		fundingTxTree, spendingTxHash, spendingTxVinIndex, vinDbID, utxoData, checked,
		updateExisting, mainchain, valid, txType, updateFundingRow, spendingTXBlockTime)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf(`RowsAffected: %w + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return fromAddrs, c, voutDbID, mixedOut, dbtx.Commit()
}
*/

func retrieveTxOutData(tx SqlQueryer, txid dbtypes.ChainHash, idx uint32, tree int8) (*dbtypes.UTXOData, error) {
	var data dbtypes.UTXOData
	var addrArray string
	err := tx.QueryRow(internal.SelectVoutAddressesByTxOut, txid, idx, tree).
		Scan(&data.VoutDbID, &addrArray, &data.Value, &data.Mixed)
	if err != nil {
		return nil, fmt.Errorf("SelectVoutAddressesByTxOut: %w", err)
	}

	// The addresses column of the vouts table contains an array of addresses
	// that the pkScript pays to (i.e. >1 for multisig). Get address list.
	replacer := strings.NewReplacer("{", "", "}", "")
	addrArray = replacer.Replace(addrArray)
	data.Addresses = strings.Split(addrArray, ",")
	return &data, nil
}

// insertSpendingAddressRow inserts a new row in the addresses table for a new
// transaction input, and updates the spending information for the addresses
// table row and vouts table row corresponding to the previous outpoint.
func insertSpendingAddressRow(tx *sql.Tx, fundingTxHash dbtypes.ChainHash, fundingTxVoutIndex uint32,
	fundingTxTree int8, spendingTxHash dbtypes.ChainHash, spendingTxVinIndex uint32, vinDbID uint64,
	spentUtxoData *dbtypes.UTXOData, checked, updateExisting, mainchain, valid bool, txType int16,
	updateFundingRow bool, blockT ...dbtypes.TimeDef) ([]string, int64, int64, bool, error) {

	// Select addresses and value from the matching funding tx output. A maximum
	// of one row and a minimum of none are expected.
	var addrs []string
	var value, voutDbID int64
	var mixed bool

	// When no previous output information is provided, query the vouts table
	// for the addresses, value, and mixed status.
	if spentUtxoData == nil {
		var err error
		spentUtxoData, err = retrieveTxOutData(tx, fundingTxHash, fundingTxVoutIndex, fundingTxTree)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, 0, 0, false, err
			}
			// This should be a hard error, but it was never that way before
			// so just warn about it for now and insert zero values.
			log.Warnf("Could not locate previous output %v:%d (tree %d) in vouts table!")
			spentUtxoData = &dbtypes.UTXOData{}
		}
	}
	addrs = spentUtxoData.Addresses
	value = spentUtxoData.Value
	mixed = spentUtxoData.Mixed
	voutDbID = spentUtxoData.VoutDbID

	// Check if the block time was provided.
	var blockTime dbtypes.TimeDef
	if len(blockT) > 0 {
		blockTime = blockT[0]
	} else {
		// Fetch the block time from the tx table.
		err := tx.QueryRow(internal.SelectTxBlockTimeByHash, spendingTxHash).Scan(&blockTime)
		if err != nil {
			return nil, 0, 0, mixed, fmt.Errorf("SelectTxBlockTimeByHash: %w", err)
		}
	}

	// Insert the addresses table row(s) for the spending tx.
	sqlStmt := internal.MakeAddressRowInsertStatement(checked, updateExisting)
	for i := range addrs {
		var isFunding bool // spending
		var rowID uint64
		err := tx.QueryRow(sqlStmt, addrs[i], fundingTxHash, spendingTxHash,
			spendingTxVinIndex, vinDbID, value, blockTime, isFunding,
			mainchain && valid, txType).Scan(&rowID)
		if err != nil {
			return nil, 0, 0, mixed, fmt.Errorf("InsertAddressRow: %w", err)
		}
	}

	if updateFundingRow && valid {
		// Update the matching funding addresses row with the spending info. If
		// the spending transaction is side chain, so must be the funding tx to
		// update it. (Similarly for mainchain, but a mainchain block always has
		// a parent on the main chain).
		N, err := setSpendingForFundingOP(tx, fundingTxHash, fundingTxVoutIndex,
			spendingTxHash, mainchain)
		return addrs, N, voutDbID, mixed, err
	}
	return addrs, 0, voutDbID, mixed, nil
}

// --- agendas table ---

// retrieveAgendaVoteChoices retrieves for the specified agenda the vote counts
// for each choice and the total number of votes. The interval size is either a
// single block or a day, as specified by byType, where a value of 1 indicates a
// block and 0 indicates a day-long interval. For day intervals, the counts
// accumulate over time (cumulative sum), whereas for block intervals the counts
// are just for the block. The total length of time over all intervals always
// spans the locked-in period of the agenda. votingDoneHeight references the
// height at which the agenda ID voting is considered complete.
func retrieveAgendaVoteChoices(ctx context.Context, db *sql.DB, agendaID string, byType int,
	votingStartHeight, votingDoneHeight int64) (*dbtypes.AgendaVoteChoices, error) {
	// Query with block or day interval size
	var query = internal.SelectAgendasVotesByTime
	if byType == 1 {
		query = internal.SelectAgendasVotesByHeight
	}

	rows, err := db.QueryContext(ctx, query, dbtypes.Yes, dbtypes.Abstain, dbtypes.No,
		agendaID, votingStartHeight, votingDoneHeight)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	// Sum abstain, yes, no, and total votes
	var a, y, n, t uint64
	totalVotes := new(dbtypes.AgendaVoteChoices)
	for rows.Next() {
		var blockTime time.Time
		var abstain, yes, no, total, height uint64
		if byType == 0 {
			err = rows.Scan(&blockTime, &yes, &abstain, &no, &total)
		} else {
			err = rows.Scan(&height, &yes, &abstain, &no, &total)
		}
		if err != nil {
			return nil, err
		}

		// For day intervals, counts are cumulative
		if byType == 0 {
			a += abstain
			y += yes
			n += no
			t += total
			totalVotes.Time = append(totalVotes.Time, dbtypes.NewTimeDef(blockTime))
		} else {
			a = abstain
			y = yes
			n = no
			t = total
			totalVotes.Height = append(totalVotes.Height, height)
		}

		totalVotes.Abstain = append(totalVotes.Abstain, a)
		totalVotes.Yes = append(totalVotes.Yes, y)
		totalVotes.No = append(totalVotes.No, n)
		totalVotes.Total = append(totalVotes.Total, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return totalVotes, nil
}

// retrieveTotalAgendaVotesCount returns the Cumulative vote choices count for
// the provided agenda id. votingDoneHeight references the height at which the
// agenda ID voting is considered complete.
func retrieveTotalAgendaVotesCount(ctx context.Context, db *sql.DB, agendaID string,
	votingStartHeight, votingDoneHeight int64) (yes, abstain, no uint32, err error) {
	var total uint32

	err = db.QueryRowContext(ctx, internal.SelectAgendaVoteTotals, dbtypes.Yes,
		dbtypes.Abstain, dbtypes.No, agendaID, votingStartHeight,
		votingDoneHeight).Scan(&yes, &abstain, &no, &total)

	return
}

// --- atomic swap tables

func insertSwap(db SqlExecutor, spendHeight int64, swapInfo *txhelpers.AtomicSwapData) error {
	var secret interface{} // only nil interface stores a NULL, not even nil slice
	if len(swapInfo.Secret) > 0 {
		secret = swapInfo.Secret
	}
	_, err := db.Exec(internal.InsertContractSpend, (*dbtypes.ChainHash)(swapInfo.ContractTx), swapInfo.ContractVout,
		(*dbtypes.ChainHash)(swapInfo.SpendTx), swapInfo.SpendVin, spendHeight,
		swapInfo.ContractAddress, swapInfo.Value,
		swapInfo.SecretHash[:], secret, swapInfo.Locktime)
	return err
}

// --- transactions table ---

/*
func InsertTx(db *sql.DB, dbTx *dbtypes.Tx, checked, updateExistingRecords bool) (uint64, error) {
	insertStatement := internal.MakeTxInsertStatement(checked, updateExistingRecords)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbTx.BlockHash, dbTx.BlockHeight, dbTx.BlockTime, dbTx.Time,
		dbTx.TxType, int16(dbTx.Version), dbTx.Tree, dbTx.TxID, dbTx.BlockIndex,
		int32(dbTx.Locktime), int32(dbTx.Expiry), dbTx.Size, dbTx.Spent, dbTx.Sent, dbTx.Fees,
		dbTx.MixCount, dbTx.MixDenom,
		dbTx.NumVin, dbtypes.UInt64Array(dbTx.VinDbIds),
		dbTx.NumVout, dbtypes.UInt64Array(dbTx.VoutDbIds),
		dbTx.IsValid, dbTx.IsMainchainBlock).Scan(&id)
	return id, err
}
*/

func insertTxnsStmt(stmt *sql.Stmt, dbTxns []*dbtypes.Tx) ([]uint64, error) {
	ids := make([]uint64, 0, len(dbTxns))
	for _, tx := range dbTxns {
		var id uint64
		err := stmt.QueryRow(
			tx.BlockHash, tx.BlockHeight, tx.BlockTime,
			tx.TxType, int16(tx.Version), tx.Tree, tx.TxID, tx.BlockIndex,
			int32(tx.Locktime), int32(tx.Expiry), tx.Size, tx.Spent, tx.Sent, tx.Fees,
			tx.MixCount, tx.MixDenom,
			tx.NumVin, dbtypes.UInt64Array(tx.VinDbIds),
			tx.NumVout, dbtypes.UInt64Array(tx.VoutDbIds), tx.IsValid,
			tx.IsMainchainBlock).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func insertTxnsDbTxn(dbTx *sql.Tx, dbTxns []*dbtypes.Tx, checked, updateExistingRecords bool) ([]uint64, error) {
	stmt, err := dbTx.Prepare(internal.MakeTxInsertStatement(checked, updateExistingRecords))
	if err != nil {
		return nil, err
	}

	ids, err := insertTxnsStmt(stmt, dbTxns)
	// Try to close the statement even if the inserts failed.
	errClose := stmt.Close()
	if err != nil {
		return nil, err
	}
	if errClose != nil {
		return nil, err
	}
	return ids, nil
}

/*
func InsertTxns(db *sql.DB, dbTxns []*dbtypes.Tx, checked, updateExistingRecords bool) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %w", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeTxInsertStatement(checked, updateExistingRecords))
	if err != nil {
		log.Errorf("Transaction INSERT prepare: %w", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
	}

	ids := make([]uint64, 0, len(dbTxns))
	for _, tx := range dbTxns {
		var id uint64
		err := stmt.QueryRow(
			tx.BlockHash, tx.BlockHeight, tx.BlockTime, tx.Time,
			tx.TxType, int16(tx.Version), tx.Tree, tx.TxID, tx.BlockIndex,
			int32(tx.Locktime), int32(tx.Expiry), tx.Size, tx.Spent, tx.Sent, tx.Fees,
			tx.MixCount, tx.MixDenom,
			tx.NumVin, dbtypes.UInt64Array(tx.VinDbIds),
			tx.NumVout, dbtypes.UInt64Array(tx.VoutDbIds), tx.IsValid,
			tx.IsMainchainBlock).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			_ = stmt.Close() // try, but we want the QueryRow error back
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return nil, err
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return ids, dbtx.Commit()
}
*/

// retrieveDbTxByHash retrieves a row of the transactions table corresponding to
// the given transaction hash. Stake-validated transactions in mainchain blocks
// are chosen first. This function is used by FillAddressTransactions, an
// important component of the addresses page.
func retrieveDbTxByHash(ctx context.Context, db *sql.DB, txHash dbtypes.ChainHash) (id uint64, dbTx *dbtypes.Tx, err error) {
	dbTx = new(dbtypes.Tx)
	vinDbIDs := dbtypes.UInt64Array(dbTx.VinDbIds)
	voutDbIDs := dbtypes.UInt64Array(dbTx.VoutDbIds)
	err = db.QueryRowContext(ctx, internal.SelectFullTxByHash, txHash).Scan(&id,
		&dbTx.BlockHash, &dbTx.BlockHeight, &dbTx.BlockTime,
		&dbTx.TxType, &dbTx.Version, &dbTx.Tree, &dbTx.TxID, &dbTx.BlockIndex,
		&dbTx.Locktime, &dbTx.Expiry, &dbTx.Size, &dbTx.Spent, &dbTx.Sent,
		&dbTx.Fees, &dbTx.MixCount, &dbTx.MixDenom, &dbTx.NumVin, &vinDbIDs,
		&dbTx.NumVout, &voutDbIDs, &dbTx.IsValid, &dbTx.IsMainchainBlock)
	dbTx.VinDbIds = vinDbIDs
	dbTx.VoutDbIds = voutDbIDs
	return
}

// retrieveFullTxByHash gets all data from the transactions table for the
// transaction specified by its hash. Transactions in valid and mainchain blocks
// are chosen first. See also retrieveDbTxByHash.
/*
func retrieveFullTxByHash(ctx context.Context, db *sql.DB, txHash string) (id uint64,
	blockHash string, blockHeight int64, blockTime, timeVal dbtypes.TimeDef,
	txType int16, version int32, tree int8, blockInd uint32,
	lockTime, expiry int32, size uint32, spent, sent, fees int64,
	mixCount int32, mixDenom int64,
	numVin int32, vinDbIDs []int64, numVout int32, voutDbIDs []int64,
	isValidBlock, isMainchainBlock bool, err error) {
	var hash string
	err = db.QueryRowContext(ctx, internal.SelectFullTxByHash, txHash).Scan(&id, &blockHash,
		&blockHeight, &blockTime, &timeVal, &txType, &version, &tree,
		&hash, &blockInd, &lockTime, &expiry, &size, &spent, &sent, &fees,
		&mixCount, &mixDenom, &numVin, &vinDbIDs, &numVout, &voutDbIDs,
		&isValidBlock, &isMainchainBlock)
	return
}
*/

// retrieveDbTxsByHash retrieves all the rows of the transactions table,
// including the primary keys/ids, for the given transaction hash. This function
// is used by the transaction page via ChainDB.Transaction.
func retrieveDbTxsByHash(ctx context.Context, db *sql.DB, txHash dbtypes.ChainHash) (ids []uint64, dbTxs []*dbtypes.Tx, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectFullTxsByHash, txHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var dbTx dbtypes.Tx
		var vinids, voutids dbtypes.UInt64Array
		// vinDbIDs := dbtypes.UInt64Array(dbTx.VinDbIds)
		// voutDbIDs := dbtypes.UInt64Array(dbTx.VoutDbIds)

		err = rows.Scan(&id,
			&dbTx.BlockHash, &dbTx.BlockHeight, &dbTx.BlockTime,
			&dbTx.TxType, &dbTx.Version, &dbTx.Tree, &dbTx.TxID, &dbTx.BlockIndex,
			&dbTx.Locktime, &dbTx.Expiry, &dbTx.Size, &dbTx.Spent, &dbTx.Sent,
			&dbTx.Fees, &dbTx.MixCount, &dbTx.MixDenom, &dbTx.NumVin, &vinids,
			&dbTx.NumVout, &voutids, &dbTx.IsValid, &dbTx.IsMainchainBlock)
		if err != nil {
			return
		}

		dbTx.VinDbIds = vinids
		dbTx.VoutDbIds = voutids

		ids = append(ids, id)
		dbTxs = append(dbTxs, &dbTx)
	}
	err = rows.Err()

	return
}

// retrieveTxnsVinsByBlock retrieves for all the transactions in the specified
// block the vin_db_ids arrays, is_valid, and is_mainchain. This function is
// used by handleVinsTableMainchainupgrade, so it should not be subject to
// timeouts.
/*
func retrieveTxnsVinsByBlock(ctx context.Context, db *sql.DB, blockHash string) (vinDbIDs []dbtypes.UInt64Array,
	areValid []bool, areMainchain []bool, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectTxnsVinsByBlock, blockHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var ids dbtypes.UInt64Array
		var isValid, isMainchain bool
		err = rows.Scan(&ids, &isValid, &isMainchain)
		if err != nil {
			return
		}

		vinDbIDs = append(vinDbIDs, ids)
		areValid = append(areValid, isValid)
		areMainchain = append(areMainchain, isMainchain)
	}
	err = rows.Err()

	return
}
*/

// retrieveTxnsVinsVoutsByBlock retrieves for all the transactions in the
// specified block the vin_db_ids and vout_db_ids arrays. This function is used
// only by UpdateLastAddressesValid and other setting functions, where it should
// not be subject to a timeout.
func retrieveTxnsVinsVoutsByBlock(ctx context.Context, db *sql.DB, blockHash dbtypes.ChainHash, onlyRegular bool) (vinDbIDs, voutDbIDs []dbtypes.UInt64Array,
	areMainchain []bool, err error) {
	stmt := internal.SelectTxnsVinsVoutsByBlock
	if onlyRegular {
		stmt = internal.SelectRegularTxnsVinsVoutsByBlock
	}

	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, stmt, blockHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var vinIDs, voutIDs dbtypes.UInt64Array
		var isMainchain bool
		err = rows.Scan(&vinIDs, &voutIDs, &isMainchain)
		if err != nil {
			return
		}

		vinDbIDs = append(vinDbIDs, vinIDs)
		voutDbIDs = append(voutDbIDs, voutIDs)
		areMainchain = append(areMainchain, isMainchain)
	}
	err = rows.Err()

	return
}

func retrieveTxByHash(ctx context.Context, db *sql.DB, txHash dbtypes.ChainHash) (id uint64, blockHash dbtypes.ChainHash,
	blockInd uint32, tree int8, err error) {
	err = db.QueryRowContext(ctx, internal.SelectTxByHash, txHash).Scan(&id, &blockHash, &blockInd, &tree)
	return
}

/*
func retrieveTxBlockTimeByHash(ctx context.Context, db *sql.DB, txHash string) (blockTime dbtypes.TimeDef, err error) {
	err = db.QueryRowContext(ctx, internal.SelectTxBlockTimeByHash, txHash).Scan(&blockTime)
	return
}
*/

// retrieveTxsByBlockHash retrieves all transactions in a given block. This is
// used by update functions, so care should be taken to not timeout in these
// cases.
func retrieveTxsByBlockHash(ctx context.Context, db *sql.DB, blockHash dbtypes.ChainHash) (txs []dbtypes.ChainHash,
	blockInds []uint32, trees []int8, blockTimes []dbtypes.TimeDef, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectTxsByBlockHash, blockHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var blockTime dbtypes.TimeDef
		var tx dbtypes.ChainHash
		var bind uint32
		var tree int8
		err = rows.Scan(&tx, &bind, &tree, &blockTime)
		if err != nil {
			return
		}

		txs = append(txs, tx)
		blockInds = append(blockInds, bind)
		trees = append(trees, tree)
		blockTimes = append(blockTimes, blockTime)
	}
	err = rows.Err()

	return
}

// retrieveTxnsBlocks retrieves for the specified transaction hash the following
// data for each block containing the transactions: block_hash, block_index,
// is_valid, is_mainchain.
func retrieveTxnsBlocks(ctx context.Context, db *sql.DB, txHash dbtypes.ChainHash) (blockHashes []dbtypes.ChainHash,
	blockHeights, blockIndexes []uint32, areValid, areMainchain []bool, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectTxsBlocks, txHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var hash dbtypes.ChainHash
		var height, idx uint32
		var isValid, isMainchain bool
		err = rows.Scan(&height, &hash, &idx, &isValid, &isMainchain)
		if err != nil {
			return
		}

		blockHeights = append(blockHeights, height)
		blockHashes = append(blockHashes, hash)
		blockIndexes = append(blockIndexes, idx)
		areValid = append(areValid, isValid)
		areMainchain = append(areMainchain, isMainchain)
	}
	err = rows.Err()

	return
}

// ----- Historical Charts on /charts page -----

// retrieveChartBlocks sets or updates a few per-block datasets.
func retrieveChartBlocks(ctx context.Context, db *sql.DB, charts *cache.ChartData) (*sql.Rows, error) {
	rows, err := db.QueryContext(ctx, internal.SelectBlockStats, charts.Height())
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Append the results from retrieveChartBlocks to the provided ChartData.
// This is the Appender half of a pair that make up a cache.ChartUpdater.
func appendChartBlocks(charts *cache.ChartData, rows *sql.Rows) error {
	defer closeRows(rows)

	// In order to store chainwork values as uint64, they are represented
	// as exahash (10^18) for work, and terahash/s (10^12) for hashrate.
	bigExa := big.NewInt(int64(1e18))
	badRows := 0
	// badRow is used to log chainwork errors without returning an error from
	// retrieveChartBlocks.
	badRow := func() {
		badRows++
	}

	var timeDef dbtypes.TimeDef
	var workhex string // []byte
	var count, size, height uint64
	var rowCount int32
	blocks := charts.Blocks
	for rows.Next() {
		rowCount++
		// Get the chainwork.
		err := rows.Scan(&height, &size, &timeDef, &workhex, &count)
		if err != nil {
			return err
		}

		// bigwork := new(big.Int).SetBytes(work)
		bigwork, ok := new(big.Int).SetString(workhex, 16)
		if !ok {
			badRow()
			continue
		}
		bigwork.Div(bigwork, bigExa)
		if !bigwork.IsUint64() {
			badRow()
			// Something is wrong, but pretend that no work was done to keep the
			// datasets sized properly.
			bigwork = big.NewInt(int64(blocks.Chainwork[len(blocks.Chainwork)-1]))
		}
		blocks.Height = append(blocks.Height, height)
		blocks.Chainwork = append(blocks.Chainwork, bigwork.Uint64())
		blocks.TxCount = append(blocks.TxCount, count)
		blocks.Time = append(blocks.Time, uint64(timeDef.T.Unix()))
		blocks.BlockSize = append(blocks.BlockSize, size)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("appendChartBlocks: iteration error: %w", err)
	}
	if badRows > 0 {
		log.Errorf("%d rows have invalid chainwork values.", badRows)
	}
	chainLen := len(blocks.Chainwork)
	if rowCount > 0 && uint64(chainLen-1) != height {
		return fmt.Errorf("appendChartBlocks: height misalignment. last height = %d. data length = %d", height, chainLen)
	}
	if len(blocks.Time) != chainLen || len(blocks.TxCount) != chainLen {
		return fmt.Errorf("appendChartBlocks: data length misalignment. len(chainwork) = %d, len(stamps) = %d, len(counts) = %d",
			chainLen, len(blocks.Time), len(blocks.TxCount))
	}

	return nil
}

// retrieveWindowStats fetches the ticket-price and pow-difficulty
// charts data source from the blocks table. These data is fetched at an
// interval of chaincfg.Params.StakeDiffWindowSize.
func retrieveWindowStats(ctx context.Context, db *sql.DB, charts *cache.ChartData) (*sql.Rows, error) {
	rows, err := db.QueryContext(ctx, internal.SelectBlocksTicketsPrice, charts.TicketPriceTip())
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Appends the results from retrieveWindowStats to the provided ChartData.
// This is the Appender half of a pair that make up a cache.ChartUpdater.
// Since tickets count per window cannot be done on the db, windows grouping
// and tickets count is done here.
func appendWindowStats(charts *cache.ChartData, rows *sql.Rows) error {
	defer closeRows(rows)

	windows := charts.Windows
	windowSize := int(charts.DiffInterval)
	nextWindowHeight := windowSize * (len(windows.TicketPrice) + 1)

	var price, ticketsCount uint64
	var timestamp time.Time
	var difficulty float64
	for rows.Next() {
		var height int
		var count uint64
		if err := rows.Scan(&price, &timestamp, &difficulty, &height, &count); err != nil {
			return err
		}
		ticketsCount += count

		// If that was the last block in the current sdiff window, append the
		// data, and reset for the next window.
		fullWindow := height == nextWindowHeight-1 // e.g. mainnet block 143, 287, etc.
		if fullWindow {
			windows.TicketPrice = append(windows.TicketPrice, price)
			windows.PowDiff = append(windows.PowDiff, difficulty)
			windows.Time = append(windows.Time, uint64(timestamp.Unix()))
			windows.StakeCount = append(windows.StakeCount, ticketsCount)

			// Next sdiff window
			ticketsCount = 0
			nextWindowHeight += windowSize
		} else if height >= nextWindowHeight {
			return fmt.Errorf("reach height %d before the end of an sdiff window at %d",
				height, nextWindowHeight)
		} // else height < nextWindowHeight-1
	}

	return rows.Err()
}

// retrieveCoinSupply fetches the coin supply data from the vins table.
func retrieveCoinSupply(ctx context.Context, db *sql.DB, charts *cache.ChartData) (*sql.Rows, error) {
	rows, err := db.QueryContext(ctx, internal.SelectCoinSupply, charts.NewAtomsTip())
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Append the results from retrieveCoinSupply to the provided ChartData.
// This is the Appender half of a pair that make up a cache.ChartUpdater.
func appendCoinSupply(charts *cache.ChartData, rows *sql.Rows) error {
	defer closeRows(rows)
	blocks := charts.Blocks
	for rows.Next() {
		var value int64
		var timestamp time.Time
		if err := rows.Scan(&timestamp, &value); err != nil {
			return err
		}

		blocks.NewAtoms = append(blocks.NewAtoms, uint64(value))
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Set the genesis block to zero because the DB stores it as -1
	if len(blocks.NewAtoms) > 0 {
		blocks.NewAtoms[0] = 0
	}
	return nil
}

// retrieveMissedVotes fetches the missed votes data from the misses and
// transactions tables.
func retrieveMissedVotes(ctx context.Context, db *sql.DB, charts *cache.ChartData) (*sql.Rows, error) {
	rows, err := db.QueryContext(ctx, internal.SelectMissCountPerBlock, charts.MissedVotesTip())
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Append the results from retrieveMissedVotes, binned per stake difficulty
// window, to the provided ChartData. This is the Appender half of a pair that
// make up a cache.ChartUpdater.
func appendMissedVotesPerWindow(charts *cache.ChartData, rows *sql.Rows) error {
	defer closeRows(rows)

	windows := charts.Windows
	windowSize := int(charts.DiffInterval)
	nextWindowHeight := windowSize * (len(windows.MissedVotes) + 1)

	var windowMisses int
	for rows.Next() {
		var height, misses int
		if err := rows.Scan(&height, &misses); err != nil {
			return err
		}
		windowMisses += misses

		// If that was the last block in the current sdiff window, append the
		// windowMisses, and reset for the next window.
		fullWindow := height == nextWindowHeight-1 // e.g. mainnet block 143, 287, etc.
		if fullWindow {
			windows.MissedVotes = append(windows.MissedVotes, uint64(windowMisses))

			// Next sdiff window
			windowMisses = 0
			nextWindowHeight += windowSize
		} else if height >= nextWindowHeight {
			return fmt.Errorf("reach height %d before the end of an sdiff window at %d",
				height, nextWindowHeight)
		} // else height < nextWindowHeight-1
	}

	return rows.Err()
}

// retrieveBlockFees retrieves any block fee data that is newer than the data
// in the provided ChartData. This data is used to plot fees on the /charts page.
// This is the Fetcher half of a pair that make up a cache.ChartUpdater.
func retrieveBlockFees(ctx context.Context, db *sql.DB, charts *cache.ChartData) (*sql.Rows, error) {
	rows, err := db.QueryContext(ctx, internal.SelectFeesPerBlockAboveHeight, charts.FeesTip())
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Append the result from retrieveBlockFees to the provided ChartData. This
// is the Appender half of a pair that make up a cache.ChartUpdater.
func appendBlockFees(charts *cache.ChartData, rows *sql.Rows) error {
	defer rows.Close()
	blocks := charts.Blocks
	for rows.Next() {
		var blockHeight uint64
		var fees int64
		if err := rows.Scan(&blockHeight, &fees); err != nil {
			log.Errorf("Unable to scan for FeeInfoPerBlock fields: %v", err)
			return err
		}
		if fees < 0 {
			fees *= -1
		}

		// Converting to atoms.
		blocks.Fees = append(blocks.Fees, uint64(fees))
	}
	return rows.Err()
}

// retrievePrivacyParticipation retrieves the sum of all mixed vouts that is
// newer than the data in the provided ChartData. This data is used to plot fees
// on the /charts page. This is the Fetcher half of a pair that make up a
// cache.ChartUpdater.
func retrievePrivacyParticipation(ctx context.Context, db *sql.DB, charts *cache.ChartData) (*sql.Rows, error) {
	rows, err := db.QueryContext(ctx, internal.SelectMixedTotalPerBlock, charts.TotalMixedTip())
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Append the result from retrievePrivacyParticipation to the provided
// ChartData. This is the Appender half of a pair that make up a
// cache.ChartUpdater.
func appendPrivacyParticipation(charts *cache.ChartData, rows *sql.Rows) error {
	defer rows.Close()
	for rows.Next() {
		var blockHeight uint64
		var totalMixed int64
		if err := rows.Scan(&blockHeight, &totalMixed); err != nil {
			log.Errorf("Unable to scan for MixedVoutsPerBlock fields: %v", err)
			return err
		}

		// Converting to atoms.
		charts.Blocks.TotalMixed = append(charts.Blocks.TotalMixed, uint64(totalMixed))
	}
	return rows.Err()
}

// appendAnonymitySet appends the result from retrieveAnonymitySet to the
// provided ChartData. This is the Appender half of a pair that make up a
// cache.ChartUpdater.
func appendAnonymitySet(charts *cache.ChartData, rows *sql.Rows) error {
	blocks := charts.Blocks
	nextHeight := int64(len(blocks.AnonymitySet))
	endHeight := int64(len(blocks.Height) - 1)

	setDiffs := make(map[int64]int64, endHeight-nextHeight+1) // map[height]value_diff
	for rows.Next() {
		var value, fundHeight int64
		var spendHeight sql.NullInt64
		var tree uint8 // not used
		err := rows.Scan(&value, &fundHeight, &spendHeight, &tree)
		if err != nil {
			return err
		}

		// The query allows spend height (> best) OR (= NULL).
		if spendHeight.Valid {
			setDiffs[spendHeight.Int64] -= value
		}

		if fundHeight < nextHeight {
			// The funding of this output has already been recorded.
			continue
		}

		setDiffs[fundHeight] += value
	}

	if err := rows.Err(); err != nil {
		log.Errorf("appendAnonymitySet Scan error: %v", err)
		return err
	}

	for h := nextHeight; h <= endHeight; h++ {
		// next = previous + delta
		nextAnonSet := setDiffs[h] // setDiffs[h] may be not found (zero) or negative
		if h > 0 {
			nextAnonSet += int64(blocks.AnonymitySet[h-1])
		}
		blocks.AnonymitySet = append(blocks.AnonymitySet, uint64(nextAnonSet))
	}

	return nil
}

// retrievePoolStats returns all the pool value and the pool size
// charts data needed to plot ticket-pool-size and ticket-pool value charts on
// the charts page. This is the Fetcher half of a pair that make up a
// cache.ChartUpdater.
func retrievePoolStats(ctx context.Context, db *sql.DB, charts *cache.ChartData) (*sql.Rows, error) {
	rows, err := db.QueryContext(ctx, internal.SelectPoolStatsAboveHeight, charts.PoolSizeTip())
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Append the result from retrievePoolStats to the provided ChartData. This is
// the Appender half of a pair that make up a cache.ChartUpdater.
func appendPoolStats(charts *cache.ChartData, rows *sql.Rows) error {
	defer rows.Close()
	blocks := charts.Blocks
	for rows.Next() {
		var pval, psize uint64
		if err := rows.Scan(&psize, &pval); err != nil {
			log.Errorf("Unable to scan for TicketPoolInfo fields: %v", err)
			return err
		}
		blocks.PoolSize = append(blocks.PoolSize, psize)
		blocks.PoolValue = append(blocks.PoolValue, pval)
	}
	return rows.Err()
}

// retrievePowerlessTickets fetches missed or expired tickets sorted by
// revocation status.
func retrievePowerlessTickets(ctx context.Context, db *sql.DB) (*apitypes.PowerlessTickets, error) {
	rows, err := db.QueryContext(ctx, internal.SelectTicketSpendTypeByBlock, -1)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	unspentType := int16(dbtypes.TicketUnspent)
	revokedType := int16(dbtypes.TicketRevoked)
	revoked := make([]apitypes.PowerlessTicket, 0)
	unspent := make([]apitypes.PowerlessTicket, 0)

	for rows.Next() {
		var height uint32
		var spendType int16
		var price float64
		if err = rows.Scan(&height, &spendType, &price); err != nil {
			return nil, err
		}
		ticket := apitypes.PowerlessTicket{
			Height: height,
			Price:  price,
		}
		switch spendType {
		case unspentType:
			unspent = append(unspent, ticket)
		case revokedType:
			revoked = append(revoked, ticket)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &apitypes.PowerlessTickets{
		Revoked: revoked,
		Unspent: unspent,
	}, nil
}

// retrieveTxPerDay fetches data for tx-per-day chart from the blocks table.
/*
func retrieveTxPerDay(ctx context.Context, db *sql.DB, timeArr []dbtypes.TimeDef,
	txCountArr []uint64) ([]dbtypes.TimeDef, []uint64, error) {
	var since time.Time

	if c := len(timeArr); c > 0 {
		since = timeArr[c-1].T

		// delete the last entry to avoid duplicates
		timeArr = timeArr[:c-1]
		txCountArr = txCountArr[:c-1]
	}

	rows, err := db.QueryContext(ctx, internal.SelectTxsPerDay, since)
	if err != nil {
		return timeArr, txCountArr, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var blockTime time.Time
		var count uint64
		if err = rows.Scan(&blockTime, &count); err != nil {
			return timeArr, txCountArr, err
		}

		timeArr = append(timeArr, dbtypes.NewTimeDef(blockTime))
		txCountArr = append(txCountArr, count)
	}
	err = rows.Err()

	return timeArr, txCountArr, err
}
*/

// --- blocks and block_chain tables ---

// insertBlock inserts the specified dbtypes.Block as with the given
// valid/mainchain status. If checked is true, an upsert statement is used so
// that a unique constraint violation will result in an update instead of
// attempting to insert a duplicate row. If checked is false and there is a
// duplicate row, an error will be returned.
func insertBlock(db *sql.DB, dbBlock *dbtypes.Block, isValid, isMainchain, checked bool) (uint64, error) {
	insertStatement := internal.BlockInsertStatement(checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbBlock.Hash, dbBlock.Height, dbBlock.Size, isValid, isMainchain,
		int32(dbBlock.Version), dbBlock.NumTx, dbBlock.NumRegTx,
		dbtypes.UInt64Array(dbBlock.TxDbIDs), dbBlock.NumStakeTx, dbtypes.UInt64Array(dbBlock.STxDbIDs),
		dbBlock.Time, int64(dbBlock.Nonce), int16(dbBlock.VoteBits), dbBlock.Voters,
		dbBlock.FreshStake, dbBlock.Revocations, dbBlock.PoolSize, int64(dbBlock.Bits),
		int64(dbBlock.SBits), dbBlock.Difficulty, int32(dbBlock.StakeVersion),
		dbBlock.PreviousHash, dbBlock.ChainWork, dbtypes.ChainHashArray(dbBlock.Winners)).Scan(&id)
	return id, err
}

// func insertBlock(db *sql.DB, dbBlock *dbtypes.Block, isValid, isMainchain, checked bool) (uint64, error) {
// 	insertStatement := internal.BlockInsertStatement(checked)
// 	var id uint64
// 	err := db.QueryRow(insertStatement,
// 		dbBlock.Hash, dbBlock.Height, dbBlock.Size, isValid, isMainchain,
// 		int32(dbBlock.Version), dbBlock.NumTx, dbBlock.NumRegTx,
// 		dbtypes.ChainHashArray2(dbBlock.Tx), dbtypes.UInt64Array(dbBlock.TxDbIDs),
// 		dbBlock.NumStakeTx, dbtypes.ChainHashArray2(dbBlock.STx), dbtypes.UInt64Array(dbBlock.STxDbIDs),
// 		dbBlock.Time, int64(dbBlock.Nonce), int16(dbBlock.VoteBits), dbBlock.Voters,
// 		dbBlock.FreshStake, dbBlock.Revocations, dbBlock.PoolSize, int64(dbBlock.Bits),
// 		int64(dbBlock.SBits), dbBlock.Difficulty, int32(dbBlock.StakeVersion),
// 		dbBlock.PreviousHash, dbBlock.ChainWork, dbtypes.ChainHashArray2(dbBlock.Winners)).Scan(&id)
// 	return id, err
// }

// insertBlockPrevNext inserts a new row of the block_chain table.
func insertBlockPrevNext(db *sql.DB, blockDbID uint64,
	hash, prev, next *dbtypes.ChainHash) error {
	rows, err := db.Query(internal.InsertBlockPrevNext, blockDbID, prev, hash, next)
	if err != nil {
		return err
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return rows.Close()
}

// insertBlockStats inserts the block stats into the stats table.
func insertBlockStats(db *sql.DB, blockDbID uint64, tpi *apitypes.TicketPoolInfo) error {
	_, err := db.Exec(internal.UpsertStats, blockDbID, tpi.Height, tpi.Size, int64(tpi.Value*dcrToAtoms))
	return err
}

// retrieveBestBlockHeight gets the best block height and hash (main chain
// only). Be sure to check for sql.ErrNoRows.
func retrieveBestBlockHeight(ctx context.Context, db *sql.DB) (height uint64, hash dbtypes.ChainHash, err error) {
	var id uint64 // maybe remove from query
	err = db.QueryRowContext(ctx, internal.RetrieveBestBlockHeight).Scan(&id, &hash, &height)
	return
}

// retrieveBestBlock gets the best block height and hash (main chain only). If
// there are no results from the query, the height is -1 and err is nil.
func retrieveBestBlock(ctx context.Context, db *sql.DB) (height int64, hash dbtypes.ChainHash, err error) {
	var bbHeight uint64
	bbHeight, hash, err = retrieveBestBlockHeight(ctx, db)
	height = int64(bbHeight)
	if err != nil && err == sql.ErrNoRows {
		height = -1
		err = nil
	}
	return
}

// retrieveBestBlockHeightAny gets the best block height, including side chains.
// func retrieveBestBlockHeightAny(ctx context.Context, db *sql.DB) (height uint64, hash string, id uint64, err error) {
// 	err = db.QueryRowContext(ctx, internal.retrieveBestBlockHeightAny).Scan(&id, &hash, &height)
// 	return
// }

// retrieveBlockHash retrieves the hash of the block at the given height, if it
// exists (be sure to check error against sql.ErrNoRows!). WARNING: this returns
// the most recently added block at this height, but there may be others.
func retrieveBlockHash(ctx context.Context, db *sql.DB, idx int64) (hash dbtypes.ChainHash, err error) {
	err = db.QueryRowContext(ctx, internal.SelectBlockHashByHeight, idx).Scan(&hash)
	return
}

// retrieveBlockTimeByHeight retrieves time hash of the main chain block at the
// given height, if it exists (be sure to check error against sql.ErrNoRows!).
func retrieveBlockTimeByHeight(ctx context.Context, db *sql.DB, idx int64) (time dbtypes.TimeDef, err error) {
	err = db.QueryRowContext(ctx, internal.SelectBlockTimeByHeight, idx).Scan(&time)
	return
}

// retrieveBlockHeight retrieves the height of the block with the given hash, if
// it exists (be sure to check error against sql.ErrNoRows!).
func retrieveBlockHeight(ctx context.Context, db *sql.DB, hash dbtypes.ChainHash) (height int64, err error) {
	err = db.QueryRowContext(ctx, internal.SelectBlockHeightByHash, hash).Scan(&height)
	return
}

// retrieveBlockVoteCount gets the number of votes mined in a block.
func retrieveBlockVoteCount(ctx context.Context, db *sql.DB, hash dbtypes.ChainHash) (numVotes int16, err error) {
	err = db.QueryRowContext(ctx, internal.SelectBlockVoteCount, hash).Scan(&numVotes)
	return
}

// retrieveBlocksHashesAll retrieve the hash of every block in the blocks table,
// ordered by their row ID.
/*
func retrieveBlocksHashesAll(ctx context.Context, db *sql.DB) ([]string, error) {
	var hashes []string
	rows, err := db.QueryContext(ctx, internal.SelectBlocksHashes)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var hash string
		err = rows.Scan(&hash)
		if err != nil {
			return nil, err
		}

		hashes = append(hashes, hash)
	}
	err = rows.Err()

	return hashes, err
}
*/

// retrieveSideChainBlocks retrieves the block chain status for all known side
// chain blocks.
func retrieveSideChainBlocks(ctx context.Context, db *sql.DB) (blocks []*dbtypes.BlockStatus, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectSideChainBlocks)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var bs dbtypes.BlockStatus
		err = rows.Scan(&bs.IsValid, &bs.Height, &bs.PrevHash, &bs.Hash, &bs.NextHash)
		if err != nil {
			return
		}

		blocks = append(blocks, &bs)
	}
	err = rows.Err()

	return
}

// retrieveSideChainTips retrieves the block chain status for all known side
// chain tip blocks.
/*
func retrieveSideChainTips(ctx context.Context, db *sql.DB) (blocks []*dbtypes.BlockStatus, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectSideChainTips)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		// NextHash is empty in all cases as these are chain tips.
		var bs dbtypes.BlockStatus
		err = rows.Scan(&bs.IsValid, &bs.Height, &bs.PrevHash, &bs.Hash)
		if err != nil {
			return
		}

		blocks = append(blocks, &bs)
	}
	err = rows.Err()

	return
}
*/

// retrieveDisapprovedBlocks retrieves the block chain status for all blocks
// that had their regular transactions invalidated by stakeholder disapproval.
func retrieveDisapprovedBlocks(ctx context.Context, db *sql.DB) (blocks []*dbtypes.BlockStatus, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectDisapprovedBlocks)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var bs dbtypes.BlockStatus
		err = rows.Scan(&bs.IsMainchain, &bs.Height, &bs.PrevHash, &bs.Hash, &bs.NextHash)
		if err != nil {
			return
		}

		blocks = append(blocks, &bs)
	}
	err = rows.Err()

	return
}

// retrieveBlockStatus retrieves the block chain status for the block with the
// specified hash.
func retrieveBlockStatus(ctx context.Context, db *sql.DB, hash dbtypes.ChainHash) (bs dbtypes.BlockStatus, err error) {
	err = db.QueryRowContext(ctx, internal.SelectBlockStatus, hash).Scan(&bs.IsValid,
		&bs.IsMainchain, &bs.Height, &bs.PrevHash, &bs.Hash, &bs.NextHash)
	return
}

// retrieveBlockStatuses retrieves the block chain statuses of all blocks at
// the given height.
func retrieveBlockStatuses(ctx context.Context, db *sql.DB, idx int64) (blocks []*dbtypes.BlockStatus, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectBlockStatuses, idx)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var bs dbtypes.BlockStatus
		err = rows.Scan(&bs.IsValid, &bs.IsMainchain, &bs.Hash)
		if err != nil {
			return
		}
		bs.Height = uint32(idx)
		blocks = append(blocks, &bs)
	}
	err = rows.Err()

	return
}

// retrieveBlockFlags retrieves the block's is_valid and is_mainchain flags.
func retrieveBlockFlags(ctx context.Context, db *sql.DB, hash dbtypes.ChainHash) (isValid bool, isMainchain bool, err error) {
	err = db.QueryRowContext(ctx, internal.SelectBlockFlags, hash).Scan(&isValid, &isMainchain)
	return
}

// retrieveBlockSummaryByTimeRange retrieves the slice of block summaries for
// the given time range. The limit specifies the number of most recent block
// summaries to return. A limit of 0 indicates all blocks in the time range
// should be included.
func retrieveBlockSummaryByTimeRange(ctx context.Context, db *sql.DB, minTime, maxTime int64, limit int) ([]dbtypes.BlockDataBasic, error) {
	var blocks []dbtypes.BlockDataBasic
	var stmt *sql.Stmt
	var rows *sql.Rows
	var err error

	// int64 -> time.Time is required to query TIMESTAMPTZ columns.
	minT := time.Unix(minTime, 0)
	maxT := time.Unix(maxTime, 0)

	if limit == 0 {
		stmt, err = db.Prepare(internal.SelectBlockByTimeRangeSQLNoLimit)
		if err != nil {
			return nil, err
		}
		rows, err = stmt.QueryContext(ctx, minT, maxT)
	} else {
		stmt, err = db.Prepare(internal.SelectBlockByTimeRangeSQL)
		if err != nil {
			return nil, err
		}
		rows, err = stmt.QueryContext(ctx, minT, maxT, limit)
	}
	_ = stmt.Close()

	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var dbBlock dbtypes.BlockDataBasic
		var blockTime dbtypes.TimeDef
		err = rows.Scan(&dbBlock.Hash, &dbBlock.Height, &dbBlock.Size,
			&blockTime, &dbBlock.NumTx)
		if err != nil {
			log.Errorf("Unable to scan for block fields: %v", err)
			return nil, err
		}
		dbBlock.Time = blockTime
		blocks = append(blocks, dbBlock)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return blocks, nil
}

// retrievePreviousHashByBlockHash retrieves the previous block hash for the
// given block from the blocks table.
// func retrievePreviousHashByBlockHash(ctx context.Context, db *sql.DB, hash string) (previousHash string, err error) {
// 	err = db.QueryRowContext(ctx, internal.SelectBlocksPreviousHash, hash).Scan(&previousHash)
// 	return
// }

// setMainchainByBlockHash is used to set the is_mainchain flag for the given
// block. This is required to handle a reorganization.
func setMainchainByBlockHash(db *sql.DB, hash dbtypes.ChainHash, isMainchain bool) (previousHash dbtypes.ChainHash, err error) {
	err = db.QueryRow(internal.UpdateBlockMainchain, hash, isMainchain).Scan(&previousHash)
	return
}

// -- UPDATE functions for various tables ---

// UpdateTransactionsMainchain sets the is_mainchain column for the transactions
// in the specified block.
func updateTransactionsMainchain(db *sql.DB, blockHash dbtypes.ChainHash, isMainchain bool) (int64, []uint64, error) {
	rows, err := db.Query(internal.UpdateTxnsMainchainByBlock, isMainchain, blockHash)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to update transactions is_mainchain: %w", err)
	}
	defer closeRows(rows)

	var numRows int64
	var txRowIDs []uint64
	for rows.Next() {
		var id uint64
		err = rows.Scan(&id)
		if err != nil {
			return 0, nil, err
		}

		txRowIDs = append(txRowIDs, id)
		numRows++
	}
	err = rows.Err()

	return numRows, txRowIDs, err
}

// updateTransactionsValid sets the is_valid column of the transactions table
// for the regular (non-stake) transactions in the specified block.
func updateTransactionsValid(db *sql.DB, blockHash dbtypes.ChainHash, isValid bool) (int64, []uint64, error) {
	rows, err := db.Query(internal.UpdateRegularTxnsValidByBlock, isValid, blockHash)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to update regular transactions is_valid: %w", err)
	}
	defer closeRows(rows)

	var numRows int64
	var txRowIDs []uint64
	for rows.Next() {
		var id uint64
		err = rows.Scan(&id)
		if err != nil {
			return 0, nil, err
		}

		txRowIDs = append(txRowIDs, id)
		numRows++
	}
	err = rows.Err()

	return numRows, txRowIDs, err
}

// updateVotesMainchain sets the is_mainchain column for the votes in the
// specified block.
func updateVotesMainchain(db SqlExecutor, blockHash dbtypes.ChainHash, isMainchain bool) (int64, error) {
	numRows, err := sqlExec(db, internal.UpdateVotesMainchainByBlock,
		"failed to update votes is_mainchain: ", isMainchain, blockHash)
	if err != nil {
		return 0, err
	}
	return numRows, nil
}

// updateTicketsMainchain sets the is_mainchain column for the tickets in the
// specified block.
func updateTicketsMainchain(db SqlExecutor, blockHash dbtypes.ChainHash, isMainchain bool) (int64, error) {
	numRows, err := sqlExec(db, internal.UpdateTicketsMainchainByBlock,
		"failed to update tickets is_mainchain: ", isMainchain, blockHash)
	if err != nil {
		return 0, err
	}
	return numRows, nil
}

// updateTreasuryMainchain sets the is_mainchain column for the entires in the
// specified block.
func updateTreasuryMainchain(db SqlExecutor, blockHash dbtypes.ChainHash, isMainchain bool) (int64, error) {
	numRows, err := sqlExec(db, internal.UpdateTreasuryMainchainByBlock,
		"failed to update treasury txns is_mainchain: ", isMainchain, blockHash)
	if err != nil {
		return 0, err
	}
	return numRows, nil
}

func binnedTreasuryIO(ctx context.Context, db *sql.DB, timeInterval string) (*dbtypes.ChartsData, error) {
	rows, err := db.QueryContext(ctx, internal.MakeSelectTreasuryIOStatement(timeInterval))
	if err != nil {
		return nil, err
	}
	return parseRowsSentReceived(rows)
}

func toCoin[T int64 | uint64](amt T) float64 {
	return float64(amt) / 1e8
}

func parseRowsSentReceived(rows *sql.Rows) (*dbtypes.ChartsData, error) {
	defer closeRows(rows)
	var items = new(dbtypes.ChartsData)
	for rows.Next() {
		var blockTime time.Time
		var received, sent uint64
		err := rows.Scan(&blockTime, &received, &sent)
		if err != nil {
			return nil, err
		}

		items.Time = append(items.Time, dbtypes.NewTimeDef(blockTime))
		items.Received = append(items.Received, toCoin(received))
		items.Sent = append(items.Sent, toCoin(sent))
		// Net represents the difference between the received and sent amount for a
		// given block. If the difference is positive then the value is unspent amount
		// otherwise if the value is zero then all amount is spent and if the net amount
		// is negative then for the given block more amount was sent than received.
		items.Net = append(items.Net, toCoin(received-sent))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// updateAddressesMainchainByIDs sets the valid_mainchain column for the
// addresses specified by their vin (spending) or vout (funding) row IDs.
func updateAddressesMainchainByIDs(db SqlExecQueryer, vinsBlk, voutsBlk []dbtypes.UInt64Array,
	isValidMainchain bool) (addresses []string, numSpendingRows, numFundingRows int64, err error) {
	addrs := make(map[string]struct{}, len(vinsBlk)+len(voutsBlk)) // may be over-alloc
	// Spending/vins: Set valid_mainchain for the is_funding=false addresses
	// table rows using the vins row ids.
	for iTxn := range vinsBlk {
		for _, vin := range vinsBlk[iTxn] {
			var address string
			err = db.QueryRow(internal.SetAddressMainchainForVinIDs, isValidMainchain, vin).Scan(&address)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					err = nil
					continue // many vins lack an address row, such as generated coins
				}
				return
			}
			addrs[address] = struct{}{}
			numSpendingRows++
		}
	}

	// Funding/vouts: Set valid_mainchain for the is_funding=true addresses
	// table rows using the vouts row ids.
	for iTxn := range voutsBlk {
		for _, vout := range voutsBlk[iTxn] {
			var address string
			err = db.QueryRow(internal.SetAddressMainchainForVoutIDs, isValidMainchain, vout).Scan(&address)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					err = nil
					continue // many vouts lack an address row, such as zero-value and nulldata outputs
				}
				return
			}
			addrs[address] = struct{}{}
			numFundingRows++
		}
	}

	// map -> slice
	addresses = make([]string, 0, len(addrs))
	for addr := range addrs {
		addresses = append(addresses, addr)
	}

	return
}

// updateLastBlockValid updates the is_valid column of the block specified by
// the row id for the blocks table.
func updateLastBlockValid(db SqlExecutor, blockDbID uint64, isValid bool) error {
	numRows, err := sqlExec(db, internal.UpdateLastBlockValid,
		"failed to update last block validity: ", blockDbID, isValid)
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("UpdateLastBlockValid failed to update exactly 1 row"+
			"(%d)", numRows)
	}
	return nil
}

func clearVoutRegularSpendTxRowIDs(db SqlExecutor, invalidatedBlockHash dbtypes.ChainHash) (int64, error) {
	return sqlExec(db, `UPDATE vouts SET spend_tx_row_id = NULL
		FROM transactions
		WHERE transactions.tree=0 
			AND transactions.block_hash=$1
			AND transactions.id=spend_tx_row_id;`,
		"failed to update vouts.spend_tx_row_id for vouts spent by invalidate block",
		invalidatedBlockHash)
}

func clearVoutAllSpendTxRowIDs(db SqlExecutor, transactionsBlockHash dbtypes.ChainHash) (int64, error) {
	return sqlExec(db, `UPDATE vouts SET spend_tx_row_id = NULL
		FROM transactions
		WHERE transactions.block_hash=$1
			AND transactions.id=spend_tx_row_id;`,
		"failed to update vouts.spend_tx_row_id for vouts spent by invalidate block",
		transactionsBlockHash)
}

// updateLastVins updates the is_valid and is_mainchain columns in the vins
// table for all of the transactions in the block specified by the given block
// hash.
func updateLastVins(db *sql.DB, blockHash dbtypes.ChainHash, isValid, isMainchain bool) error {
	// retrieve the hash for every transaction in this block. A context with no
	// deadline or cancellation function is used since this UpdateLastVins needs
	// to complete to ensure DB integrity.
	txs, _, trees, timestamps, err := retrieveTxsByBlockHash(context.Background(), db, blockHash)
	if err != nil {
		return err
	}

	for i, txHash := range txs {
		thisValid := isValid || trees[i] == wire.TxTreeStake
		n, err := sqlExec(db, internal.SetIsValidIsMainchainByTxHash,
			"failed to update last vins tx validity: ", thisValid, isMainchain,
			txHash, timestamps[i])
		if err != nil {
			return err
		}

		if n < 1 {
			return fmt.Errorf("failed to update at least 1 row")
		}
	}

	return nil
}

// updateLastAddressesValid sets valid_mainchain as specified by isValid for
// addresses table rows pertaining to regular (non-stake) transactions found in
// the given block.
func updateLastAddressesValid(db *sql.DB, blockHash dbtypes.ChainHash, isValid bool) ([]string, error) {
	// The queries in this function should not timeout or (probably) canceled,
	// so use a background context.
	ctx := context.Background()

	// Get the row ids of all vins and vouts of regular txns in this block.
	onlyRegularTxns := true
	vinDbIDsBlk, voutDbIDsBlk, _, err := retrieveTxnsVinsVoutsByBlock(ctx, db, blockHash, onlyRegularTxns)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve vin data for block %s: %w", blockHash, err)
	}
	// Using vins and vouts row ids, update the valid_mainchain column of the
	// rows of the address table referring to these vins and vouts.
	addresses, numAddrSpending, numAddrFunding, err := updateAddressesMainchainByIDs(db,
		vinDbIDsBlk, voutDbIDsBlk, isValid)
	if err != nil {
		log.Errorf("Failed to set addresses rows in block %s as sidechain: %w", blockHash, err)
	}
	addrsUpdated := numAddrSpending + numAddrFunding
	log.Debugf("Rows of addresses table updated: %d", addrsUpdated)
	return addresses, err
}

// updateBlockNext sets the next block's hash for the specified row of the
// block_chain table specified by DB row ID.
func updateBlockNext(db SqlExecutor, blockDbID uint64, next dbtypes.ChainHash) error {
	res, err := db.Exec(internal.UpdateBlockNext, blockDbID, next)
	if err != nil {
		return err
	}
	numRows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("%s (%d)", notOneRowErrMsg, numRows)
	}
	return nil
}

// updateBlockNextByHash sets the next block's hash for the block in the
// block_chain table specified by hash.
func updateBlockNextByHash(db SqlExecutor, this, next dbtypes.ChainHash) error {
	res, err := db.Exec(internal.UpdateBlockNextByHash, this, next)
	if err != nil {
		return err
	}
	numRows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("%s (%d)", notOneRowErrMsg, numRows)
	}
	return nil
}

// updateBlockNextByNextHash sets the next block's hash for the block in the
// block_chain table with a current next_hash specified by hash.
func updateBlockNextByNextHash(db SqlExecutor, currentNext, newNext dbtypes.ChainHash) error {
	res, err := db.Exec(internal.UpdateBlockNextByNextHash, currentNext, newNext)
	if err != nil {
		return err
	}
	numRows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("%s (%d)", notOneRowErrMsg, numRows)
	}
	return nil
}

// retrievePoolInfo returns ticket pool info for block height ind
func retrievePoolInfo(ctx context.Context, db *sql.DB, ind int64) (*apitypes.TicketPoolInfo, error) {
	tpi := &apitypes.TicketPoolInfo{
		Height: uint32(ind),
	}
	var hash dbtypes.ChainHash // trash?
	var winners dbtypes.ChainHashArray
	var val int64
	err := db.QueryRowContext(ctx, internal.SelectPoolInfoByHeight, ind).Scan(&hash, &tpi.Size,
		&val, &winners)
	tpi.Value = toCoin(val)
	tpi.ValAvg = tpi.Value / float64(tpi.Size)
	tpi.Winners = make([]string, len(winners))
	for i := range winners {
		tpi.Winners[i] = winners[i].String()
	}
	return tpi, err
}

// retrievePoolInfoRange returns an array of apitypes.TicketPoolInfo for block
// range ind0 to ind1 and a non-nil error on success
func retrievePoolInfoRange(ctx context.Context, db *sql.DB, ind0, ind1 int64) ([]apitypes.TicketPoolInfo, []dbtypes.ChainHash, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []apitypes.TicketPoolInfo{}, []dbtypes.ChainHash{}, nil
	}
	if N < 0 {
		return nil, nil, fmt.Errorf("Cannot retrieve pool info range (%d>%d)",
			ind0, ind1)
	}

	tpis := make([]apitypes.TicketPoolInfo, 0, N)
	hashes := make([]dbtypes.ChainHash, 0, N)

	stmt, err := db.PrepareContext(ctx, internal.SelectPoolInfoRange)
	if err != nil {
		return nil, nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(ind0, ind1)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var tpi apitypes.TicketPoolInfo
		var hash dbtypes.ChainHash
		var winners dbtypes.ChainHashArray
		var val int64
		if err = rows.Scan(&tpi.Height, &hash, &tpi.Size, &val, &winners); err != nil {
			log.Errorf("Unable to scan for TicketPoolInfo fields: %v", err)
			return nil, nil, err
		}
		tpi.Value = toCoin(val)
		tpi.ValAvg = tpi.Value / float64(tpi.Size)

		tpi.Winners = make([]string, len(winners))
		for i := range winners {
			tpi.Winners[i] = winners[i].String()
		}
		tpis = append(tpis, tpi)
		hashes = append(hashes, hash)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	return tpis, hashes, nil
}

// retrievePoolValAndSizeRange returns an array each of the pool values and
// sizes for block range ind0 to ind1.
func retrievePoolValAndSizeRange(ctx context.Context, db *sql.DB, ind0, ind1 int64) ([]float64, []uint32, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []float64{}, []uint32{}, nil
	}
	if N < 0 {
		return nil, nil, fmt.Errorf("Cannot retrieve pool val and size range (%d>%d)",
			ind0, ind1)
	}

	poolvals := make([]float64, 0, N)
	poolsizes := make([]uint32, 0, N)

	stmt, err := db.Prepare(internal.SelectPoolValSizeRange)
	if err != nil {
		return nil, nil, err
	}
	defer stmt.Close()

	rows, err := stmt.QueryContext(ctx, ind0, ind1)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var pval int64
		var psize uint32
		if err = rows.Scan(&psize, &pval); err != nil {
			log.Errorf("Unable to scan for TicketPoolInfo fields: %v", err)
			return nil, nil, err
		}
		poolvals = append(poolvals, toCoin(pval))
		poolsizes = append(poolsizes, psize)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	if len(poolsizes) != int(N) {
		log.Warnf("retrievePoolValAndSizeRange: retrieved pool values (%d) not expected number (%d)",
			len(poolsizes), N)
	}

	return poolvals, poolsizes, nil
}

// retrieveBlockSummary fetches basic block data for block ind.
func retrieveBlockSummary(ctx context.Context, db *sql.DB, ind int64) (*apitypes.BlockDataBasic, error) {
	bd := apitypes.NewBlockDataBasic()
	var winners dbtypes.ChainHashArray
	var isValid bool
	var val, sbits int64
	var hash dbtypes.ChainHash
	var timestamp dbtypes.TimeDef
	err := db.QueryRowContext(ctx, internal.SelectBlockDataByHeight, ind).Scan(
		&hash, &bd.Height, &bd.Size, &bd.Difficulty, &sbits, &timestamp,
		&bd.PoolInfo.Size, &val, &winners, &isValid)
	if err != nil {
		return nil, err
	}
	bd.Hash = hash.String()
	bd.PoolInfo.Value = toCoin(val)
	bd.PoolInfo.ValAvg = bd.PoolInfo.Value / float64(bd.Size)
	bd.Time = apitypes.TimeAPI{S: timestamp}
	bd.PoolInfo.Winners = make([]string, len(winners))
	for i := range winners {
		bd.PoolInfo.Winners[i] = winners[i].String()
	}
	bd.StakeDiff = toCoin(sbits)

	return bd, nil
}

// retrieveBlockSummaryByHash fetches basic block data for block hash.
func retrieveBlockSummaryByHash(ctx context.Context, db *sql.DB, hash dbtypes.ChainHash) (*apitypes.BlockDataBasic, error) {
	bd := apitypes.NewBlockDataBasic()
	var winners dbtypes.ChainHashArray
	var isMainchain, isValid bool
	var timestamp dbtypes.TimeDef
	var val, psize sql.NullInt64 // pool value and size are only stored for mainchain blocks
	var sbits int64
	err := db.QueryRowContext(ctx, internal.SelectBlockDataByHash, hash).Scan(
		&bd.Height, &bd.Size, &bd.Difficulty, &sbits, &timestamp,
		&psize, &val, &winners, &isMainchain, &isValid)
	if err != nil {
		return nil, err
	}
	bd.Hash = hash.String()
	bd.PoolInfo.Value = toCoin(val.Int64)
	bd.PoolInfo.Size = uint32(psize.Int64)
	bd.PoolInfo.ValAvg = bd.PoolInfo.Value / float64(bd.Size)
	bd.Time = apitypes.TimeAPI{S: timestamp}
	bd.PoolInfo.Winners = make([]string, len(winners))
	for i := range winners {
		bd.PoolInfo.Winners[i] = winners[i].String()
	}
	bd.StakeDiff = toCoin(sbits)
	return bd, nil
}

// retrieveBlockSummaryRange fetches basic block data for the blocks in range
// (ind0, ind1).
func retrieveBlockSummaryRange(ctx context.Context, db *sql.DB, ind0, ind1 int64) ([]*apitypes.BlockDataBasic, error) {
	var desc bool
	low, high := ind0, ind1
	if low > high {
		low, high = ind1, ind0
		desc = true
	}
	expCount := high - low + 1
	if expCount <= 0 {
		return nil, fmt.Errorf("invalid block range %d-%d", ind0, ind1)
	}
	tmpl := internal.SelectBlockDataRange
	if desc {
		tmpl = internal.SelectBlockDataRangeDesc
	}
	rows, err := db.QueryContext(ctx, tmpl, low, high)
	if err != nil {
		return nil, err
	}
	blocks := make([]*apitypes.BlockDataBasic, 0, expCount)
	defer rows.Close()
	for rows.Next() {
		bd := apitypes.NewBlockDataBasic()
		var winners dbtypes.ChainHashArray
		var isValid bool
		var val, sbits int64
		var timestamp dbtypes.TimeDef
		var hash dbtypes.ChainHash
		err := rows.Scan(
			&hash, &bd.Height, &bd.Size, &bd.Difficulty, &sbits, &timestamp,
			&bd.PoolInfo.Size, &val, &winners, &isValid,
		)
		if err != nil {
			return nil, err
		}
		bd.Hash = hash.String()
		bd.PoolInfo.Value = toCoin(val)
		bd.PoolInfo.ValAvg = bd.PoolInfo.Value / float64(bd.Size)
		bd.Time = apitypes.TimeAPI{S: timestamp}
		bd.PoolInfo.Winners = make([]string, len(winners))
		for i := range winners {
			bd.PoolInfo.Winners[i] = winners[i].String()
		}
		bd.StakeDiff = toCoin(sbits)
		blocks = append(blocks, bd)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// error here if count not correct?
	return blocks, nil
}

// retrieveBlockSummaryRangeStepped fetches basic block data for every step'th
// block in range (ind0, ind1).
func retrieveBlockSummaryRangeStepped(ctx context.Context, db *sql.DB, ind0, ind1, step int64) ([]*apitypes.BlockDataBasic, error) {
	var desc bool
	stepMod := ind0 % step
	low, high := ind0, ind1
	if low > high {
		desc = true
		low, high = ind1, ind0
	}
	expCount := high - low + 1
	if expCount <= 0 {
		return nil, fmt.Errorf("invalid block range %d-%d", ind0, ind1)
	}
	tmpl := internal.SelectBlockDataRangeWithSkip
	if desc {
		tmpl = internal.SelectBlockDataRangeWithSkipDesc
	}
	query := fmt.Sprintf(tmpl, step, stepMod)
	rows, err := db.QueryContext(ctx, query, low, high)
	if err != nil {
		return nil, err
	}
	blocks := make([]*apitypes.BlockDataBasic, 0, expCount)
	defer rows.Close()
	for rows.Next() {
		bd := apitypes.NewBlockDataBasic()
		var winners dbtypes.ChainHashArray
		var isValid bool
		var val, sbits int64
		var timestamp dbtypes.TimeDef
		var hash dbtypes.ChainHash
		err := rows.Scan(
			&hash, &bd.Height, &bd.Size, &bd.Difficulty, &sbits, &timestamp,
			&bd.PoolInfo.Size, &val, &winners, &isValid,
		)
		if err != nil {
			return nil, err
		}
		bd.Hash = hash.String()
		bd.PoolInfo.Value = toCoin(val)
		bd.PoolInfo.ValAvg = bd.PoolInfo.Value / float64(bd.Size)
		bd.Time = apitypes.TimeAPI{S: timestamp}
		bd.PoolInfo.Winners = make([]string, len(winners))
		for i := range winners {
			bd.PoolInfo.Winners[i] = winners[i].String()
		}
		bd.StakeDiff = toCoin(sbits)
		blocks = append(blocks, bd)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// error here if count not correct?
	return blocks, nil
}

// retrieveBlockSize return the size of block at height ind.
func retrieveBlockSize(ctx context.Context, db *sql.DB, ind int64) (int32, error) {
	var blockSize int32
	err := db.QueryRowContext(ctx, internal.SelectBlockSizeByHeight, ind).Scan(&blockSize)
	if err != nil {
		return -1, fmt.Errorf("unable to scan for block size: %w", err)
	}

	return blockSize, nil
}

// retrieveBlockSizeRange returns an array of block sizes for block range ind0 to ind1
func retrieveBlockSizeRange(ctx context.Context, db *sql.DB, ind0, ind1 int64) ([]int32, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []int32{}, nil
	}
	if N < 0 {
		return nil, fmt.Errorf("Cannot retrieve block size range (%d>%d)",
			ind0, ind1)
	}

	blockSizes := make([]int32, 0, N)

	stmt, err := db.Prepare(internal.SelectBlockSizeRange)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.QueryContext(ctx, ind0, ind1)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var blockSize int32
		if err = rows.Scan(&blockSize); err != nil {
			log.Errorf("Unable to scan for block size field: %v", err)
			return nil, err
		}
		blockSizes = append(blockSizes, blockSize)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return blockSizes, nil
}

// retrieveSDiff returns the stake difficulty for block at the specified chain
// height.
func retrieveSDiff(ctx context.Context, db *sql.DB, ind int64) (float64, error) {
	var sbits int64
	err := db.QueryRowContext(ctx, internal.SelectSBitsByHeight, ind).Scan(&sbits)
	return toCoin(sbits), err
}

// retrieveSBitsByHash returns the stake difficulty in atoms for the specified
// block.
func retrieveSBitsByHash(ctx context.Context, db *sql.DB, hash dbtypes.ChainHash) (int64, error) {
	var sbits int64
	err := db.QueryRowContext(ctx, internal.SelectSBitsByHash, hash).Scan(&sbits)
	return sbits, err
}

// retrieveSDiffRange returns an array of stake difficulties for block range
// ind0 to ind1.
func retrieveSDiffRange(ctx context.Context, db *sql.DB, ind0, ind1 int64) ([]float64, error) {
	N := ind1 - ind0 + 1
	if N == 0 {
		return []float64{}, nil
	}
	if N < 0 {
		return nil, fmt.Errorf("Cannot retrieve sdiff range (%d>%d)",
			ind0, ind1)
	}
	sdiffs := make([]float64, 0, N)

	stmt, err := db.Prepare(internal.SelectSBitsRange)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.QueryContext(ctx, ind0, ind1)
	if err != nil {
		log.Errorf("Query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sbits int64
		if err = rows.Scan(&sbits); err != nil {
			log.Errorf("Unable to scan for sdiff fields: %v", err)
			return nil, err
		}
		sdiffs = append(sdiffs, toCoin(sbits))
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return sdiffs, nil
}

// retrieveLatestBlockSummary returns the block summary for the best block.
func retrieveLatestBlockSummary(ctx context.Context, db *sql.DB) (*apitypes.BlockDataBasic, error) {
	bd := apitypes.NewBlockDataBasic()

	var winners dbtypes.ChainHashArray
	var timestamp dbtypes.TimeDef
	var isValid bool
	var val, sbits int64
	var hash dbtypes.ChainHash
	err := db.QueryRowContext(ctx, internal.SelectBlockDataBest).Scan(
		&hash, &bd.Height, &bd.Size, &bd.Difficulty, &sbits, &timestamp,
		&bd.PoolInfo.Size, &val, &winners, &isValid)
	if err != nil {
		return nil, err
	}
	bd.Hash = hash.String()
	bd.PoolInfo.Value = toCoin(val)
	bd.PoolInfo.ValAvg = bd.PoolInfo.Value / float64(bd.PoolInfo.Size)
	bd.Time = apitypes.TimeAPI{S: timestamp}
	bd.PoolInfo.Winners = make([]string, len(winners))
	for i := range winners {
		bd.PoolInfo.Winners = append(bd.PoolInfo.Winners, winners[i].String())
	}
	bd.StakeDiff = toCoin(sbits)
	return bd, nil
}

// retrieveDiff returns the difficulty for the first block mined after the
// provided UNIX timestamp.
func retrieveDiff(ctx context.Context, db *sql.DB, timestamp int64) (float64, error) {
	var diff float64
	tDef := dbtypes.NewTimeDefFromUNIX(timestamp)
	err := db.QueryRowContext(ctx, internal.SelectDiffByTime, tDef).Scan(&diff)
	return diff, err
}
