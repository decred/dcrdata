// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"

	apitypes "github.com/decred/dcrdata/v3/api/types"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/db/dcrpg/internal"
	"github.com/decred/dcrdata/v3/txhelpers"

	"github.com/lib/pq"
)

// outputCountType defines the modes of the output count chart data.
// outputCountByAllBlocks defines count per block i.e. solo and pooled tickets
// count per block. outputCountByTicketPoolWindow defines the output count per
// given ticket price window
type outputCountType int

const (
	outputCountByAllBlocks outputCountType = iota
	outputCountByTicketPoolWindow
)

// Maintenance functions

// closeRows closes the input sql.Rows, logging any error.
func closeRows(rows *sql.Rows) {
	if e := rows.Close(); e != nil {
		log.Errorf("Close of Query failed: %v", e)
	}
}

// sqlExec executes the SQL statement string with any optional arguments, and
// returns the nuber of rows affected.
func sqlExec(db *sql.DB, stmt, execErrPrefix string, args ...interface{}) (int64, error) {
	res, err := db.Exec(stmt, args...)
	if err != nil {
		return 0, fmt.Errorf(execErrPrefix + " " + err.Error())
	}
	if res == nil {
		return 0, nil
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`error in RowsAffected: %v`, err)
	}
	return N, err
}

// sqlExecStmt executes the prepared SQL statement with any optional arguments,
// and returns the nuber of rows affected.
func sqlExecStmt(stmt *sql.Stmt, execErrPrefix string, args ...interface{}) (int64, error) {
	res, err := stmt.Exec(args...)
	if err != nil {
		return 0, fmt.Errorf("%v %v", execErrPrefix, err)
	}
	if res == nil {
		return 0, nil
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`error in RowsAffected: %v`, err)
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

// DeleteDuplicateVins deletes rows in vin with duplicate tx information,
// leaving the one row with the lowest id.
func DeleteDuplicateVins(db *sql.DB) (int64, error) {
	execErrPrefix := "failed to delete duplicate vins: "

	existsIdx, err := ExistsIndex(db, "uix_vin")
	if err != nil {
		return 0, err
	} else if !existsIdx {
		return sqlExec(db, internal.DeleteVinsDuplicateRows, execErrPrefix)
	}

	if isuniq, err := IsUniqueIndex(db, "uix_vin"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}

	return sqlExec(db, internal.DeleteVinsDuplicateRows, execErrPrefix)
}

// DeleteDuplicateVouts deletes rows in vouts with duplicate tx information,
// leaving the one row with the lowest id.
func DeleteDuplicateVouts(db *sql.DB) (int64, error) {
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

// DeleteDuplicateTxns deletes rows in transactions with duplicate tx-block
// hashes, leaving the one row with the lowest id.
func DeleteDuplicateTxns(db *sql.DB) (int64, error) {
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

// DeleteDuplicateTickets deletes rows in tickets with duplicate tx-block
// hashes, leaving the one row with the lowest id.
func DeleteDuplicateTickets(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, "uix_ticket_hashes_index"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate tickets: "
	return sqlExec(db, internal.DeleteTicketsDuplicateRows, execErrPrefix)
}

// DeleteDuplicateVotes deletes rows in votes with duplicate tx-block hashes,
// leaving the one row with the lowest id.
func DeleteDuplicateVotes(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, "uix_votes_hashes_index"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate votes: "
	return sqlExec(db, internal.DeleteVotesDuplicateRows, execErrPrefix)
}

// DeleteDuplicateMisses deletes rows in misses with duplicate tx-block hashes,
// leaving the one row with the lowest id.
func DeleteDuplicateMisses(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, "uix_misses_hashes_index"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate misses: "
	return sqlExec(db, internal.DeleteMissesDuplicateRows, execErrPrefix)
}

// --- stake (votes, tickets, misses) tables ---

// InsertTickets takes a slice of *dbtypes.Tx and corresponding DB row IDs for
// transactions, extracts the tickets, and inserts the tickets into the
// database. Outputs are a slice of DB row IDs of the inserted tickets, and an
// error.
func InsertTickets(db *sql.DB, dbTxns []*dbtypes.Tx, txDbIDs []uint64, checked bool) ([]uint64, []*dbtypes.Tx, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeTicketInsertStatement(checked))
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
		var isMultisig bool
		if len(tx.Vouts) > 0 {
			if len(tx.Vouts[0].ScriptPubKeyData.Addresses) > 0 {
				stakesubmissionAddress = tx.Vouts[0].ScriptPubKeyData.Addresses[0]
			}
			// scriptSubClass, _, _, _ := txscript.ExtractPkScriptAddrs(
			// 	tx.Vouts[0].Version, tx.Vouts[0].ScriptPubKey[1:], chainParams)
			scriptSubClass, _ := txscript.GetStakeOutSubclass(tx.Vouts[0].ScriptPubKey)
			isMultisig = scriptSubClass == txscript.MultiSigTy
		}

		price := dcrutil.Amount(tx.Vouts[0].Value).ToCoin()
		fee := dcrutil.Amount(tx.Fees).ToCoin()
		isSplit := tx.NumVin > 1

		var id uint64
		err := stmt.QueryRow(
			tx.TxID, tx.BlockHash, tx.BlockHeight, ticketDbIDs[i],
			stakesubmissionAddress, isMultisig, isSplit, tx.NumVin,
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

// InsertVotes takes a slice of *dbtypes.Tx, which must contain all the stake
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
// Outputs are slices of DB row IDs for the votes and misses, and an error.
func InsertVotes(db *sql.DB, dbTxns []*dbtypes.Tx, _ /*txDbIDs*/ []uint64,
	fTx *TicketTxnIDGetter, msgBlock *MsgBlockPG, checked bool, params *chaincfg.Params) ([]uint64,
	[]*dbtypes.Tx, []string, []uint64, map[string]uint64, error) {
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
			if tx.TxID != msgTxs[i].TxHash().String() {
				return nil, nil, nil, nil, nil, fmt.Errorf("txid of dbtypes.Tx does not match that of msgTx")
			}
		}
	}

	if len(voteTxs) == 0 {
		return nil, nil, nil, nil, nil, nil
	}

	// Start DB transaction and prepare vote insert statement
	dbtx, err := db.Begin()
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	voteInsert := internal.MakeVoteInsertStatement(checked)
	voteStmt, err := dbtx.Prepare(voteInsert)
	if err != nil {
		log.Errorf("Votes INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, nil, nil, nil, nil, err
	}

	prep, err := dbtx.Prepare(internal.MakeAgendaInsertStatement(checked))
	if err != nil {
		log.Errorf("Agendas INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, nil, nil, nil, nil, err
	}

	// Insert each vote, and build list of missed votes equal to
	// setdiff(Validators, votes).
	candidateBlockHash := msgBlock.Header.PrevBlock.String()
	ids := make([]uint64, 0, len(voteTxs))
	spentTicketHashes := make([]string, 0, len(voteTxs))
	spentTicketDbIDs := make([]uint64, 0, len(voteTxs))
	misses := make([]string, len(msgBlock.Validators))
	copy(misses, msgBlock.Validators)
	for i, tx := range voteTxs {
		msgTx := voteMsgTxs[i]
		voteVersion := stake.SSGenVersion(msgTx)
		validBlock, voteBits, err := txhelpers.SSGenVoteBlockValid(msgTx)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}

		stakeSubmissionAmount := dcrutil.Amount(msgTx.TxIn[1].ValueIn).ToCoin()
		stakeSubmissionTxHash := msgTx.TxIn[1].PreviousOutPoint.Hash.String()
		spentTicketHashes = append(spentTicketHashes, stakeSubmissionTxHash)

		var ticketTxDbID sql.NullInt64
		if fTx != nil {
			t, err := fTx.TxnDbID(stakeSubmissionTxHash, false)
			if err != nil {
				_ = voteStmt.Close() // try, but we want the QueryRow error back
				if errRoll := dbtx.Rollback(); errRoll != nil {
					log.Errorf("Rollback failed: %v", errRoll)
				}
				return nil, nil, nil, nil, nil, err
			}
			ticketTxDbID.Int64 = int64(t)
		}
		spentTicketDbIDs = append(spentTicketDbIDs, uint64(ticketTxDbID.Int64))

		voteReward := dcrutil.Amount(msgTx.TxIn[0].ValueIn).ToCoin()

		// delete spent ticket from missed list
		for im := range misses {
			if misses[im] == stakeSubmissionTxHash {
				misses[im] = misses[len(misses)-1]
				misses = misses[:len(misses)-1]
				break
			}
		}

		_, _, _, choices, err := txhelpers.SSGenVoteChoices(msgTx, params)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}

		// agendas
		var rowID uint64
		for _, val := range choices {
			index, err := dbtypes.ChoiceIndexFromStr(val.Choice.Id)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}

			lockedIn, activated, hardForked := false, false, false

			// THIS IS A TEMPORARY SOLUTION till activated, lockedIn and hardforked
			// height values can be sent via an rpc method.
			progress, ok := VotingMilestones[val.ID]
			if ok {
				lockedIn = (progress.LockedIn == tx.BlockHeight)
				activated = (progress.Activated == tx.BlockHeight)
				hardForked = (progress.HardForked == tx.BlockHeight)
			}

			err = prep.QueryRow(val.ID, index, tx.TxID, tx.BlockHeight,
				tx.BlockTime, lockedIn, activated, hardForked).Scan(&rowID)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
		}

		var id uint64
		err = voteStmt.QueryRow(
			tx.BlockHeight, tx.TxID, tx.BlockHash, candidateBlockHash,
			voteVersion, voteBits, validBlock.Validity,
			stakeSubmissionTxHash, ticketTxDbID, stakeSubmissionAmount,
			voteReward, tx.IsMainchainBlock).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			_ = voteStmt.Close() // try, but we want the QueryRow error back
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return nil, nil, nil, nil, nil, err
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = voteStmt.Close()

	// If the validators are available, miss accounting should be accurate.
	if len(msgBlock.Validators) > 0 && len(ids)+len(misses) != 5 {
		fmt.Println(misses)
		fmt.Println(voteTxs)
		_ = dbtx.Rollback()
		panic(fmt.Sprintf("votes (%d) + misses (%d) != 5", len(ids), len(misses)))
	}

	// Store missed tickets
	missHashMap := make(map[string]uint64)
	if len(misses) > 0 {
		stmtMissed, err := dbtx.Prepare(internal.MakeMissInsertStatement(checked))
		if err != nil {
			log.Errorf("Miss INSERT prepare: %v", err)
			_ = dbtx.Rollback() // try, but we want the Prepare error back
			return nil, nil, nil, nil, nil, err
		}

		blockHash := msgBlock.BlockHash().String()
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

// RetrieveMissedVotesInBlock gets a list of ticket hashes that were called to
// vote in the given block, but missed their vote.
func RetrieveMissedVotesInBlock(db *sql.DB, blockHash string) (ticketHashes []string, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectMissesInBlock, blockHash)
	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var hash string
		err = rows.Scan(&hash)
		if err != nil {
			break
		}

		ticketHashes = append(ticketHashes, hash)
	}
	return
}

// RetrieveAllRevokes gets for all ticket revocations the row IDs (primary
// keys), transaction hashes, block heights. It also gets the row ID in the vins
// table for the first input of the revocation transaction, which should
// correspond to the stakesubmission previous outpoint of the ticket purchase.
func RetrieveAllRevokes(db *sql.DB) (ids []uint64, hashes []string, heights []int64, vinDbIDs []uint64, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectAllRevokes)
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
			break
		}

		ids = append(ids, id)
		heights = append(heights, height)
		hashes = append(hashes, hash)
		vinDbIDs = append(vinDbIDs, vinDbID)
	}
	return
}

// RetrieveAllVotesDbIDsHeightsTicketDbIDs gets for all votes the row IDs
// (primary keys) in the votes table, the block heights, and the row IDs in the
// tickets table of the spent tickets.
func RetrieveAllVotesDbIDsHeightsTicketDbIDs(db *sql.DB) (ids []uint64, heights []int64,
	ticketDbIDs []uint64, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectAllVoteDbIDsHeightsTicketDbIDs)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id, ticketDbID uint64
		var height int64
		err = rows.Scan(&id, &height, &ticketDbID)
		if err != nil {
			break
		}

		ids = append(ids, id)
		heights = append(heights, height)
		ticketDbIDs = append(ticketDbIDs, ticketDbID)
	}
	return
}

// RetrieveUnspentTickets gets all unspent tickets.
func RetrieveUnspentTickets(db *sql.DB) (ids []uint64, hashes []string, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectUnspentTickets)
	if err != nil {
		return ids, hashes, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var hash string
		err = rows.Scan(&id, &hash)
		if err != nil {
			break
		}

		ids = append(ids, id)
		hashes = append(hashes, hash)
	}

	return ids, hashes, err
}

// RetrieveTicketIDByHash gets the db row ID (primary key) in the tickets table
// for the given ticket hash.
func RetrieveTicketIDByHash(db *sql.DB, ticketHash string) (id uint64, err error) {
	err = db.QueryRow(internal.SelectTicketIDByHash, ticketHash).Scan(&id)
	return
}

// RetrieveTicketStatusByHash gets the spend status and ticket pool status for
// the given ticket hash.
func RetrieveTicketStatusByHash(db *sql.DB, ticketHash string) (id uint64, spendStatus dbtypes.TicketSpendType,
	poolStatus dbtypes.TicketPoolStatus, err error) {
	err = db.QueryRow(internal.SelectTicketStatusByHash, ticketHash).Scan(&id, &spendStatus, &poolStatus)
	return
}

// RetrieveTicketIDsByHashes gets the db row IDs (primary keys) in the tickets
// table for the given ticket purchase transaction hashes.
func RetrieveTicketIDsByHashes(db *sql.DB, ticketHashes []string) (ids []uint64, err error) {
	var dbtx *sql.Tx
	dbtx, err = db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.SelectTicketIDByHash)
	if err != nil {
		log.Errorf("Tickets SELECT prepare: %v", err)
		_ = stmt.Close()
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
			return ids, fmt.Errorf("Tickets SELECT exec failed: %v", err)
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return ids, dbtx.Commit()
}

// retrieveTicketsByDate fetches the tickets in the current ticketpool order by the
// purchase date. The maturity block is needed to identify immature tickets.
// The grouping interval size is specified in seconds.
func retrieveTicketsByDate(db *sql.DB, maturityBlock, groupBy int64) (*dbtypes.PoolTicketsData, error) {
	rows, err := db.Query(internal.SelectTicketsByPurchaseDate, groupBy, maturityBlock)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	tickets := new(dbtypes.PoolTicketsData)
	for rows.Next() {
		var immature, live, timestamp uint64
		var price, total float64
		err = rows.Scan(&timestamp, &price, &immature, &live)
		if err != nil {
			return nil, fmt.Errorf("retrieveTicketsByDate %v", err)
		}

		tickets.Time = append(tickets.Time, timestamp)
		tickets.Immature = append(tickets.Immature, immature)
		tickets.Live = append(tickets.Live, live)

		// Returns the average value of a ticket depending on the grouping mode used
		price = price * 100000000
		total = float64(live + immature)
		tickets.Price = append(tickets.Price, dcrutil.Amount(price/total).ToCoin())
	}

	return tickets, nil
}

// retrieveTicketByPrice fetches the tickets in the current ticketpool ordered by the
// purchase price. The maturity block is needed to identify immature tickets.
// The grouping interval size is specified in seconds.
func retrieveTicketByPrice(db *sql.DB, maturityBlock int64) (*dbtypes.PoolTicketsData, error) {
	// Create the query statement and retrieve rows
	rows, err := db.Query(internal.SelectTicketsByPrice, maturityBlock)
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
			return nil, fmt.Errorf("retrieveTicketByPrice %v", err)
		}

		tickets.Immature = append(tickets.Immature, immature)
		tickets.Live = append(tickets.Live, live)
		tickets.Price = append(tickets.Price, price)
	}

	return tickets, nil
}

// retrieveTicketsGroupedByType fetches the count of tickets in the current
// ticketpool grouped by ticket type (inferred by their output counts). The
// grouping used here i.e. solo, pooled and tixsplit is just a guessing based on
// commonly structured ticket purchases.
func retrieveTicketsGroupedByType(db *sql.DB) (*dbtypes.PoolTicketsData, error) {
	var entry dbtypes.PoolTicketsData
	rows, err := db.Query(internal.SelectTicketsByType)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var txType, txTypeCount uint64
		err = rows.Scan(&txType, &txTypeCount)

		if err != nil {
			return nil, fmt.Errorf("retrieveTicketsGroupedByType %v", err)
		}

		switch txType {
		case 1:
			entry.Solo = txTypeCount
		case 2:
			entry.Pooled = txTypeCount
		case 3:
			entry.TxSplit = txTypeCount
		}
	}

	return &entry, nil
}

func retrieveTicketSpendTypePerBlock(db *sql.DB) (*dbtypes.ChartsData, error) {
	var items = new(dbtypes.ChartsData)
	rows, err := db.Query(internal.SelectTicketSpendTypeByBlock)
	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var height, unspent, revoked uint64
		err = rows.Scan(&height, &unspent, &revoked)
		if err != nil {
			return nil, err
		}

		items.Height = append(items.Height, height)
		items.Unspent = append(items.Unspent, unspent)
		items.Revoked = append(items.Revoked, revoked)
	}
	return items, nil
}

// SetPoolStatusForTickets sets the ticket pool status for the tickets specified
// by db row ID.
func SetPoolStatusForTickets(db *sql.DB, ticketDbIDs []uint64, poolStatuses []dbtypes.TicketPoolStatus) (int64, error) {
	if len(ticketDbIDs) == 0 {
		return 0, nil
	}
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	var stmt *sql.Stmt
	stmt, err = dbtx.Prepare(internal.SetTicketPoolStatusForTicketDbID)
	if err != nil {
		// Already up a creek. Just return error from Prepare.
		_ = dbtx.Rollback()
		return 0, fmt.Errorf("tickets SELECT prepare failed: %v", err)
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
func SetPoolStatusForTicketsByHash(db *sql.DB, tickets []string,
	poolStatuses []dbtypes.TicketPoolStatus) (int64, error) {
	if len(tickets) == 0 {
		return 0, nil
	}
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	var stmt *sql.Stmt
	stmt, err = dbtx.Prepare(internal.SetTicketPoolStatusForHash)
	if err != nil {
		// Already up a creek. Just return error from Prepare.
		_ = dbtx.Rollback()
		return 0, fmt.Errorf("tickets SELECT prepare failed: %v", err)
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

// SetSpendingForTickets sets the spend type, spend height, spending transaction
// row IDs (in the table relevant to the spend type), and ticket pool status for
// the given tickets specified by their db row IDs.
func SetSpendingForTickets(db *sql.DB, ticketDbIDs, spendDbIDs []uint64,
	blockHeights []int64, spendTypes []dbtypes.TicketSpendType,
	poolStatuses []dbtypes.TicketPoolStatus) (int64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	var stmt *sql.Stmt
	stmt, err = dbtx.Prepare(internal.SetTicketSpendingInfoForTicketDbID)
	if err != nil {
		// Already up a creek. Just return error from Prepare.
		_ = dbtx.Rollback()
		return 0, fmt.Errorf("tickets SELECT prepare failed: %v", err)
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

// setSpendingForTickets is identical to SetSpendingForTickets except it takes a
// database transaction that was begun and will be committed by the caller.
func setSpendingForTickets(dbtx *sql.Tx, ticketDbIDs, spendDbIDs []uint64,
	blockHeights []int64, spendTypes []dbtypes.TicketSpendType,
	poolStatuses []dbtypes.TicketPoolStatus) error {
	stmt, err := dbtx.Prepare(internal.SetTicketSpendingInfoForTicketDbID)
	if err != nil {
		return fmt.Errorf("tickets SELECT prepare failed: %v", err)
	}

	rowsAffected := make([]int64, len(ticketDbIDs))
	for i, ticketDbID := range ticketDbIDs {
		rowsAffected[i], err = sqlExecStmt(stmt, "failed to set ticket spending info: ",
			ticketDbID, blockHeights[i], spendDbIDs[i], spendTypes[i], poolStatuses[i])
		if err != nil {
			_ = stmt.Close()
			return err
		}
		if rowsAffected[i] != 1 {
			log.Warnf("Updated spending info for %d tickets, expecting just 1!",
				rowsAffected[i])
		}
	}

	return stmt.Close()
}

// --- addresses table ---

// InsertAddressRow inserts an AddressRow (input or output), returning the row
// ID in the addresses table of the inserted data.
func InsertAddressRow(db *sql.DB, dbA *dbtypes.AddressRow, dupCheck bool) (uint64, error) {
	sqlStmt := internal.MakeAddressRowInsertStatement(dupCheck)
	var id uint64
	err := db.QueryRow(sqlStmt, dbA.Address, dbA.MatchingTxHash, dbA.TxHash,
		dbA.TxVinVoutIndex, dbA.VinVoutDbID, dbA.Value, dbA.TxBlockTime,
		dbA.IsFunding, dbA.ValidMainChain, dbA.TxType).Scan(&id)
	return id, err
}

// InsertAddressRows inserts multiple transaction inputs or outputs for certain
// addresses ([]AddressRow). The row IDs of the inserted data are returned.
func InsertAddressRows(db *sql.DB, dbAs []*dbtypes.AddressRow, dupCheck bool) ([]uint64, error) {
	// Create the address table if it does not exist
	tableName := "addresses"
	if haveTable, _ := TableExists(db, tableName); !haveTable {
		if err := CreateTable(db, tableName); err != nil {
			log.Errorf("Failed to create table %s: %v", tableName, err)
		}
	}

	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	sqlStmt := internal.MakeAddressRowInsertStatement(dupCheck)

	stmt, err := dbtx.Prepare(sqlStmt)
	if err != nil {
		log.Errorf("AddressRow INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
	}

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

func RetrieveAddressRecvCount(db *sql.DB, address string) (count int64, err error) {
	err = db.QueryRow(internal.SelectAddressRecvCount, address).Scan(&count)
	return
}

func RetrieveAddressUnspent(db *sql.DB, address string) (count, totalAmount int64, err error) {
	err = db.QueryRow(internal.SelectAddressUnspentCountANDValue, address).
		Scan(&count, &totalAmount)
	return
}

func RetrieveAddressSpent(db *sql.DB, address string) (count, totalAmount int64, err error) {
	err = db.QueryRow(internal.SelectAddressSpentCountANDValue, address).
		Scan(&count, &totalAmount)
	return
}

// RetrieveAddressSpentUnspent gets the numbers of spent and unspent outpoints
// for the given address, the total amounts spent and unspent, and the the
// number of distinct spending transactions.
func RetrieveAddressSpentUnspent(db *sql.DB, address string) (numSpent, numUnspent,
	amtSpent, amtUnspent, numMergedSpent int64, err error) {
	var dbtx *sql.Tx
	dbtx, err = db.Begin()
	if err != nil {
		err = fmt.Errorf("unable to begin database transaction: %v", err)
		return
	}

	// Query for spent and unspent totals.
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectAddressSpentUnspentCountAndValue, address)
	if err != nil && err != sql.ErrNoRows {
		if errRoll := dbtx.Rollback(); errRoll != nil {
			log.Errorf("Rollback failed: %v", errRoll)
		}
		err = fmt.Errorf("unable to Query for spent and unspent amounts: %v", err)
		return
	}
	if err == sql.ErrNoRows {
		return
	}

	for rows.Next() {
		var count, totalValue int64
		var noMatchingTx, isFunding bool
		err = rows.Scan(&count, &totalValue, &isFunding, &noMatchingTx)
		if err != nil {
			break
		}

		// Unspent == funding with no matching transaction
		if isFunding && noMatchingTx {
			numUnspent = count
			amtUnspent = totalValue
		}
		// Spent == spending (but ensure a matching transaction is set)
		if !isFunding {
			if noMatchingTx {
				log.Errorf("Found spending transactions with matching_tx_hash"+
					" unset for %s!", address)
				continue
			}
			numSpent = count
			amtSpent = totalValue
		}
	}
	closeRows(rows)

	// Query for spending transaction count, repeated transaction hashes merged.
	var nms sql.NullInt64
	err = dbtx.QueryRow(internal.SelectAddressesMergedSpentCount, address).
		Scan(&nms)
	if err != nil && err != sql.ErrNoRows {
		if errRoll := dbtx.Rollback(); errRoll != nil {
			log.Errorf("Rollback failed: %v", errRoll)
		}
		err = fmt.Errorf("unable to QueryRow for merged spent count: %v", err)
		return
	}

	numMergedSpent = nms.Int64
	if !nms.Valid {
		log.Debug("Merged debit spent count is not valid")
	}

	err = dbtx.Rollback()
	return
}

// RetrieveAddressUTXOs gets the unspent transaction outputs (UTXOs) paying to
// the specified address.
func RetrieveAddressUTXOs(db *sql.DB, address string, currentBlockHeight int64) ([]apitypes.AddressTxnOutput, error) {
	stmt, err := db.Prepare(internal.SelectAddressUnspentWithTxn)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	rows, err := stmt.Query(address)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer rows.Close()

	var outputs []apitypes.AddressTxnOutput
	for rows.Next() {
		pkScript := []byte{}
		var blockHeight, atoms int64
		blocktime := uint64(0)
		txnOutput := apitypes.AddressTxnOutput{}
		if err = rows.Scan(&txnOutput.Address, &txnOutput.TxnID,
			&atoms, &blockHeight, &blocktime, &txnOutput.Vout, &pkScript); err != nil {
			log.Error(err)
		}
		txnOutput.ScriptPubKey = hex.EncodeToString(pkScript)
		txnOutput.Amount = dcrutil.Amount(atoms).ToCoin()
		txnOutput.Satoshis = atoms
		txnOutput.Height = blockHeight
		txnOutput.Confirmations = currentBlockHeight - blockHeight + 1
		outputs = append(outputs, txnOutput)
	}

	return outputs, nil
}

// RetrieveAddressTxnsOrdered will get all transactions for addresses provided
// and return them sorted by time in descending order. It will also return a
// short list of recently (defined as greater than recentBlockHeight) confirmed
// transactions that can be used to validate mempool status.
func RetrieveAddressTxnsOrdered(db *sql.DB, addresses []string, recentBlockHeight int64) (txs []string, recenttxs []string) {
	var txHash string
	var height int64
	stmt, err := db.Prepare(internal.SelectAddressesAllTxn)
	if err != nil {
		log.Error(err)
		return nil, nil
	}

	rows, err := stmt.Query(pq.Array(addresses))
	if err != nil {
		log.Error(err)
		return nil, nil
	}

	defer closeRows(rows)

	for rows.Next() {
		err = rows.Scan(&txHash, &height)
		if err != nil {
			log.Error(err)
			return
		}
		txs = append(txs, txHash)
		if height > recentBlockHeight {
			recenttxs = append(recenttxs, txHash)
		}
	}
	return
}

func RetrieveAllAddressTxns(db *sql.DB, address string) ([]uint64, []*dbtypes.AddressRow, error) {
	rows, err := db.Query(internal.SelectAddressAllByAddress, address)
	if err != nil {
		return nil, nil, err
	}

	defer closeRows(rows)

	return scanAddressQueryRows(rows)
}

func RetrieveAddressTxns(db *sql.DB, address string, N, offset int64) ([]uint64, []*dbtypes.AddressRow, error) {
	return retrieveAddressTxns(db, address, N, offset,
		internal.SelectAddressLimitNByAddress, false)
}

func RetrieveAddressDebitTxns(db *sql.DB, address string, N, offset int64) ([]uint64, []*dbtypes.AddressRow, error) {
	return retrieveAddressTxns(db, address, N, offset,
		internal.SelectAddressDebitsLimitNByAddress, false)
}

func RetrieveAddressCreditTxns(db *sql.DB, address string, N, offset int64) ([]uint64, []*dbtypes.AddressRow, error) {
	return retrieveAddressTxns(db, address, N, offset,
		internal.SelectAddressCreditsLimitNByAddress, false)
}

func RetrieveAddressMergedDebitTxns(db *sql.DB, address string, N, offset int64) ([]uint64, []*dbtypes.AddressRow, error) {
	return retrieveAddressTxns(db, address, N, offset,
		internal.SelectAddressMergedDebitView, true)
}

func retrieveAddressTxns(db *sql.DB, address string, N, offset int64,
	statement string, isMergedDebitView bool) ([]uint64, []*dbtypes.AddressRow, error) {
	rows, err := db.Query(statement, address, N, offset)
	if err != nil {
		return nil, nil, err
	}

	defer closeRows(rows)

	if isMergedDebitView {
		addr, err := scanPartialAddressQueryRows(rows, address)
		return nil, addr, err
	}
	return scanAddressQueryRows(rows)
}

func scanPartialAddressQueryRows(rows *sql.Rows, addr string) (addressRows []*dbtypes.AddressRow, err error) {
	for rows.Next() {
		var addr = dbtypes.AddressRow{Address: addr}

		err = rows.Scan(&addr.TxHash, &addr.ValidMainChain, &addr.TxBlockTime,
			&addr.Value, &addr.MergedDebitCount)
		if err != nil {
			return
		}
		addressRows = append(addressRows, &addr)
	}
	return
}

func scanAddressQueryRows(rows *sql.Rows) (ids []uint64, addressRows []*dbtypes.AddressRow, err error) {
	for rows.Next() {
		var id uint64
		var addr dbtypes.AddressRow
		var txHash sql.NullString
		var blockTime, txVinIndex, vinDbID sql.NullInt64
		// Scan values in order of columns listed in internal.addrsColumnNames
		err = rows.Scan(&id, &addr.Address, &addr.MatchingTxHash, &txHash,
			&addr.ValidMainChain, &txVinIndex, &blockTime, &vinDbID,
			&addr.Value, &addr.IsFunding)
		if err != nil {
			return
		}

		if blockTime.Valid {
			addr.TxBlockTime = uint64(blockTime.Int64)
		}
		if txHash.Valid {
			addr.TxHash = txHash.String
		}
		if txVinIndex.Valid {
			addr.TxVinVoutIndex = uint32(txVinIndex.Int64)
		}
		if vinDbID.Valid {
			addr.VinVoutDbID = uint64(vinDbID.Int64)
		}

		ids = append(ids, id)
		addressRows = append(addressRows, &addr)
	}
	return
}

// RetrieveAddressIDsByOutpoint fetches all address row IDs for a given outpoint
// (hash:index).
// Update Vin due to DCRD AMOUNTIN - START - DO NOT MERGE CHANGES IF DCRD FIXED
func RetrieveAddressIDsByOutpoint(db *sql.DB, txHash string,
	voutIndex uint32) ([]uint64, []string, int64, error) {
	var ids []uint64
	var addresses []string
	var value int64
	rows, err := db.Query(internal.SelectAddressIDsByFundingOutpoint, txHash, voutIndex)
	if err != nil {
		return ids, addresses, 0, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var addr string
		err = rows.Scan(&id, &addr, &value)
		if err != nil {
			break
		}

		ids = append(ids, id)
		addresses = append(addresses, addr)
	}
	return ids, addresses, value, err
} // Update Vin due to DCRD AMOUNTIN - END

// retrieveOldestTxBlockTime helps choose the most appropriate address page
// graph grouping to load by default depending on when the first transaction to
// the specific address was made.
func retrieveOldestTxBlockTime(db *sql.DB, addr string) (blockTime int64, err error) {
	err = db.QueryRow(internal.SelectAddressOldestTxBlockTime, addr).Scan(&blockTime)
	return
}

// retrieveTxHistoryByType fetches the transaction types count for all the
// transactions associated with a given address for the given time interval.
// The time interval is grouping records by week, month, year, day and all.
// For all time interval, transactions are grouped by the unique
// timestamps (blocks) available.
func retrieveTxHistoryByType(db *sql.DB, addr string,
	timeInterval int64) (*dbtypes.ChartsData, error) {
	var items = new(dbtypes.ChartsData)

	rows, err := db.Query(internal.SelectAddressTxTypesByAddress,
		timeInterval, addr)
	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var blockTime, sentRtx, receivedRtx, tickets, votes, revokeTx uint64
		err = rows.Scan(&blockTime, &sentRtx, &receivedRtx, &tickets, &votes, &revokeTx)
		if err != nil {
			return nil, err
		}

		items.Time = append(items.Time, blockTime)
		items.SentRtx = append(items.SentRtx, sentRtx)
		items.ReceivedRtx = append(items.ReceivedRtx, receivedRtx)
		items.Tickets = append(items.Tickets, tickets)
		items.Votes = append(items.Votes, votes)
		items.RevokeTx = append(items.RevokeTx, revokeTx)
	}
	return items, nil
}

// retrieveTxHistoryByAmount fetches the transaction amount flow i.e. received
// and sent amount for all the transactions associated with a given address and for
// the given time interval. The time interval is grouping records by week,
// month, year, day and all. For all time interval, transactions are grouped by
// the unique timestamps (blocks) available.
func retrieveTxHistoryByAmountFlow(db *sql.DB, addr string,
	timeInterval int64) (*dbtypes.ChartsData, error) {
	var items = new(dbtypes.ChartsData)

	rows, err := db.Query(internal.SelectAddressAmountFlowByAddress,
		timeInterval, addr)
	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var blockTime, received, sent uint64
		err = rows.Scan(&blockTime, &received, &sent)
		if err != nil {
			return nil, err
		}

		items.Time = append(items.Time, blockTime)
		items.Received = append(items.Received, dcrutil.Amount(received).ToCoin())
		items.Sent = append(items.Sent, dcrutil.Amount(sent).ToCoin())
		// Net represents the difference between the received and sent amount for a
		// given block. If the difference is positive then the value is unspent amount
		// otherwise if the value is zero then all amount is spent and if the net amount
		// is negative then for the given block more amount was sent than received.
		items.Net = append(items.Net, dcrutil.Amount(received-sent).ToCoin())
	}
	return items, nil
}

// retrieveTxHistoryByUnspentAmount fetches the unspent amount for all the
// transactions associated with a given address for the given time interval.
// The time interval is grouping records by week, month, year, day and all.
// For all time interval, transactions are grouped by the unique
// timestamps (blocks) available.
func retrieveTxHistoryByUnspentAmount(db *sql.DB, addr string,
	timeInterval int64) (*dbtypes.ChartsData, error) {
	var totalAmount uint64
	var items = new(dbtypes.ChartsData)

	rows, err := db.Query(internal.SelectAddressUnspentAmountByAddress,
		timeInterval, addr)
	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	for rows.Next() {
		var blockTime, amount uint64
		err = rows.Scan(&blockTime, &amount)
		if err != nil {
			return nil, err
		}

		items.Time = append(items.Time, blockTime)

		// Return commmulative amount data for the unspent chart type
		totalAmount += amount
		items.Amount = append(items.Amount, dcrutil.Amount(totalAmount).ToCoin())
	}
	return items, nil
}

// --- vins and vouts tables ---

func InsertVin(db *sql.DB, dbVin dbtypes.VinTxProperty, checked bool) (id uint64, err error) {
	err = db.QueryRow(internal.MakeVinInsertStatement(checked),
		dbVin.TxID, dbVin.TxIndex, dbVin.TxTree,
		dbVin.PrevTxHash, dbVin.PrevTxIndex, dbVin.PrevTxTree,
		dbVin.ValueIn, dbVin.IsValid, dbVin.IsMainchain, dbVin.Time,
		dbVin.TxType).Scan(&id)
	return
}

func InsertVins(db *sql.DB, dbVins dbtypes.VinTxPropertyARRAY, checked bool) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeVinInsertStatement(checked))
	if err != nil {
		log.Errorf("Vin INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
	}

	// TODO/Question: Should we skip inserting coinbase txns, which have same PrevTxHash?

	ids := make([]uint64, 0, len(dbVins))
	for _, vin := range dbVins {
		var id uint64
		err = stmt.QueryRow(vin.TxID, vin.TxIndex, vin.TxTree,
			vin.PrevTxHash, vin.PrevTxIndex, vin.PrevTxTree,
			vin.ValueIn, vin.IsValid, vin.IsMainchain, vin.Time, vin.TxType).Scan(&id)
		if err != nil {
			_ = stmt.Close() // try, but we want the QueryRow error back
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return ids, fmt.Errorf("InsertVins INSERT exec failed: %v", err)
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return ids, dbtx.Commit()
}

func InsertVout(db *sql.DB, dbVout *dbtypes.Vout, checked bool) (uint64, error) {
	insertStatement := internal.MakeVoutInsertStatement(checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbVout.TxHash, dbVout.TxIndex, dbVout.TxTree,
		dbVout.Value, dbVout.Version,
		dbVout.ScriptPubKey, dbVout.ScriptPubKeyData.ReqSigs,
		dbVout.ScriptPubKeyData.Type,
		pq.Array(dbVout.ScriptPubKeyData.Addresses)).Scan(&id)
	return id, err
}

func InsertVouts(db *sql.DB, dbVouts []*dbtypes.Vout, checked bool) ([]uint64, []dbtypes.AddressRow, error) {
	// All inserts in atomic DB transaction
	dbtx, err := db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeVoutInsertStatement(checked))
	if err != nil {
		log.Errorf("Vout INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, nil, err
	}

	addressRows := make([]dbtypes.AddressRow, 0, len(dbVouts)) // may grow with multisig
	ids := make([]uint64, 0, len(dbVouts))
	for _, vout := range dbVouts {
		var id uint64
		err = stmt.QueryRow(
			vout.TxHash, vout.TxIndex, vout.TxTree, vout.Value, vout.Version,
			vout.ScriptPubKey, vout.ScriptPubKeyData.ReqSigs,
			vout.ScriptPubKeyData.Type,
			pq.Array(vout.ScriptPubKeyData.Addresses)).Scan(&id)
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
		for _, addr := range vout.ScriptPubKeyData.Addresses {
			addressRows = append(addressRows, dbtypes.AddressRow{
				Address:        addr,
				TxHash:         vout.TxHash,
				TxVinVoutIndex: vout.TxIndex,
				VinVoutDbID:    id,
				TxType:         vout.TxType,
				Value:          vout.Value,
				// Not set here are: ValidMainchain, MatchingTxHash, IsFunding,
				// and TxBlockTime.
			})
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return ids, addressRows, dbtx.Commit()
}

func RetrievePkScriptByID(db *sql.DB, id uint64) (pkScript []byte, ver uint16, err error) {
	err = db.QueryRow(internal.SelectPkScriptByID, id).Scan(&ver, &pkScript)
	return
}

func RetrievePkScriptByOutpoint(db *sql.DB, txHash string, voutIndex uint32) (pkScript []byte, ver uint16, err error) {
	err = db.QueryRow(internal.SelectPkScriptByOutpoint, txHash, voutIndex).Scan(&ver, &pkScript)
	return
}

func RetrieveVoutIDByOutpoint(db *sql.DB, txHash string, voutIndex uint32) (id uint64, err error) {
	err = db.QueryRow(internal.SelectVoutIDByOutpoint, txHash, voutIndex).Scan(&id)
	return
}

func RetrieveVoutValue(db *sql.DB, txHash string, voutIndex uint32) (value uint64, err error) {
	err = db.QueryRow(internal.RetrieveVoutValue, txHash, voutIndex).Scan(&value)
	return
}

func RetrieveVoutValues(db *sql.DB, txHash string) (values []uint64, txInds []uint32, txTrees []int8, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.RetrieveVoutValues, txHash)
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
			break
		}

		values = append(values, v)
		txInds = append(txInds, ind)
		txTrees = append(txTrees, tree)
	}

	return
}

// RetrieveAllVinDbIDs gets every row ID (the primary keys) for the vins table.
func RetrieveAllVinDbIDs(db *sql.DB) (vinDbIDs []uint64, err error) {
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
			break
		}

		vinDbIDs = append(vinDbIDs, id)
	}

	return
}

// RetrieveFundingOutpointByTxIn gets the previous outpoint for a transaction
// input specified by transaction hash and input index.
func RetrieveFundingOutpointByTxIn(db *sql.DB, txHash string,
	vinIndex uint32) (id uint64, tx string, index uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectFundingOutpointByTxIn, txHash, vinIndex).
		Scan(&id, &tx, &index, &tree)
	return
}

// RetrieveFundingOutpointByVinID gets the previous outpoint for a transaction
// input specified by row ID in the vins table.
func RetrieveFundingOutpointByVinID(db *sql.DB, vinDbID uint64) (tx string, index uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectFundingOutpointByVinID, vinDbID).
		Scan(&tx, &index, &tree)
	return
}

// RetrieveFundingOutpointIndxByVinID gets the transaction output index of the
// previous outpoint for a transaction input specified by row ID in the vins
// table.
func RetrieveFundingOutpointIndxByVinID(db *sql.DB, vinDbID uint64) (idx uint32, err error) {
	err = db.QueryRow(internal.SelectFundingOutpointIndxByVinID, vinDbID).Scan(&idx)
	return
}

// RetrieveFundingTxByTxIn gets the transaction hash of the previous outpoint
// for a transaction input specified by hash and input index.
func RetrieveFundingTxByTxIn(db *sql.DB, txHash string, vinIndex uint32) (id uint64, tx string, err error) {
	err = db.QueryRow(internal.SelectFundingTxByTxIn, txHash, vinIndex).
		Scan(&id, &tx)
	return
}

// RetrieveFundingTxByVinDbID gets the transaction hash of the previous outpoint
// for a transaction input specified by row ID in the vins table.
func RetrieveFundingTxByVinDbID(db *sql.DB, vinDbID uint64) (tx string, err error) {
	err = db.QueryRow(internal.SelectFundingTxByVinID, vinDbID).Scan(&tx)
	return
}

// TODO: this does not appear correct.
func RetrieveFundingTxsByTx(db *sql.DB, txHash string) ([]uint64, []*dbtypes.Tx, error) {
	var ids []uint64
	var txs []*dbtypes.Tx
	rows, err := db.Query(internal.SelectFundingTxsByTx, txHash)
	if err != nil {
		return ids, txs, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var tx dbtypes.Tx
		err = rows.Scan(&id, &tx)
		if err != nil {
			break
		}

		ids = append(ids, id)
		txs = append(txs, &tx)
	}

	return ids, txs, err
}

// RetrieveSpendingTxByVinID gets the spending transaction input (hash, vin
// number, and tx tree) for the transaction input specified by row ID in the
// vins table.
func RetrieveSpendingTxByVinID(db *sql.DB, vinDbID uint64) (tx string,
	vinIndex uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectSpendingTxByVinID, vinDbID).Scan(&tx, &vinIndex, &tree)
	return
}

// RetrieveSpendingTxByTxOut gets any spending transaction input info for a
// previous outpoint specified by funding transaction hash and vout number.
func RetrieveSpendingTxByTxOut(db *sql.DB, txHash string,
	voutIndex uint32) (id uint64, tx string, vin uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectSpendingTxByPrevOut,
		txHash, voutIndex).Scan(&id, &tx, &vin, &tree)
	return
}

// RetrieveSpendingTxsByFundingTx gets info on all spending transaction inputs
// for the given funding transaction specified by DB row ID.
func RetrieveSpendingTxsByFundingTx(db *sql.DB, fundingTxID string) (dbIDs []uint64,
	txns []string, vinInds []uint32, voutInds []uint32, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectSpendingTxsByPrevTx, fundingTxID)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var tx string
		var vin, vout uint32
		err = rows.Scan(&id, &tx, &vin, &vout)
		if err != nil {
			break
		}

		dbIDs = append(dbIDs, id)
		txns = append(txns, tx)
		vinInds = append(vinInds, vin)
		voutInds = append(voutInds, vout)
	}

	return
}

// RetrieveSpendingTxsByFundingTxWithBlockHeight will retrieve all transactions,
// indexes and block heights funded by a specific transaction.
func RetrieveSpendingTxsByFundingTxWithBlockHeight(db *sql.DB,
	fundingTxID string) (aSpendByFunHash []*apitypes.SpendByFundingHash, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectSpendingTxsByPrevTxWithBlockHeight, fundingTxID)
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
	return
}

func RetrieveVinByID(db *sql.DB, vinDbID uint64) (prevOutHash string, prevOutVoutInd uint32,
	prevOutTree int8, txHash string, txVinInd uint32, txTree int8, valueIn int64, err error) {
	var blockTime uint64
	var isValid, isMainchain bool
	var txType uint32
	err = db.QueryRow(internal.SelectAllVinInfoByID, vinDbID).
		Scan(&txHash, &txVinInd, &txTree, &isValid, &isMainchain, &blockTime,
			&prevOutHash, &prevOutVoutInd, &prevOutTree, &valueIn, &txType)
	return
}

func RetrieveVinsByIDs(db *sql.DB, vinDbIDs []uint64) ([]dbtypes.VinTxProperty, error) {
	vins := make([]dbtypes.VinTxProperty, len(vinDbIDs))
	for i, id := range vinDbIDs {
		vin := &vins[i]
		err := db.QueryRow(internal.SelectAllVinInfoByID, id).Scan(&vin.TxID,
			&vin.TxIndex, &vin.TxTree, &vin.IsValid, &vin.IsMainchain,
			&vin.Time, &vin.PrevTxHash, &vin.PrevTxIndex, &vin.PrevTxTree,
			&vin.ValueIn, &vin.TxType)
		if err != nil {
			return nil, err
		}
	}
	return vins, nil
}

func RetrieveVoutsByIDs(db *sql.DB, voutDbIDs []uint64) ([]dbtypes.Vout, error) {
	vouts := make([]dbtypes.Vout, len(voutDbIDs))
	for i, id := range voutDbIDs {
		vout := &vouts[i]
		var id0 uint64
		var reqSigs uint32
		var scriptType, addresses string
		err := db.QueryRow(internal.SelectVoutByID, id).Scan(&id0, &vout.TxHash,
			&vout.TxIndex, &vout.TxTree, &vout.Value, &vout.Version,
			&vout.ScriptPubKey, &reqSigs, &scriptType, &addresses)
		if err != nil {
			return nil, err
		}
		// Parse the addresses array
		replacer := strings.NewReplacer("{", "", "}", "")
		addresses = replacer.Replace(addresses)

		vout.ScriptPubKeyData.ReqSigs = reqSigs
		vout.ScriptPubKeyData.Type = scriptType
		vout.ScriptPubKeyData.Addresses = strings.Split(addresses, ",")
	}
	return vouts, nil
}

// SetSpendingForVinDbIDs updates rows of the addresses table with spending
// information from the rows of the vins table specified by vinDbIDs.
func SetSpendingForVinDbIDs(db *sql.DB, vinDbIDs []uint64) ([]int64, int64, error) {
	// Get funding details for vin and set them in the address table.
	dbtx, err := db.Begin()
	if err != nil {
		return nil, 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	var vinGetStmt *sql.Stmt
	vinGetStmt, err = dbtx.Prepare(internal.SelectAllVinInfoByID)
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
		// Get the funding tx outpoint (vins table) for the vin DB ID
		var prevOutHash, txHash string
		var prevOutVoutInd, txVinInd uint32
		var prevOutTree, txTree int8
		var valueIn, blockTime int64
		var isValid, isMainchain bool
		var txType int16
		err = vinGetStmt.QueryRow(vinDbID).Scan(
			&txHash, &txVinInd, &txTree, &isValid, &isMainchain, &blockTime,
			&prevOutHash, &prevOutVoutInd, &prevOutTree, &valueIn, &txType)
		if err != nil {
			return addressRowsUpdated, 0, fmt.Errorf(`SelectAllVinInfoByID: `+
				`%v + %v (rollback)`, err, bail())
		}

		// skip coinbase inputs
		if bytes.Equal(zeroHashStringBytes, []byte(prevOutHash)) {
			continue
		}

		// Set the spending tx info (addresses table) for the vin DB ID
		addressRowsUpdated[iv], err = insertSpendingTxByPrptStmt(dbtx,
			prevOutHash, prevOutVoutInd, prevOutTree,
			txHash, txVinInd, vinDbID, false, isValid && isMainchain, txType)
		if err != nil {
			return addressRowsUpdated, 0, fmt.Errorf(`insertSpendingTxByPrptStmt: `+
				`%v + %v (rollback)`, err, bail())
		}

		totalUpdated += addressRowsUpdated[iv]
	}

	// Close prepared statements. Ignore errors as we'll Commit regardless.
	_ = vinGetStmt.Close()

	return addressRowsUpdated, totalUpdated, dbtx.Commit()
}

// SetSpendingForVinDbID updates a row of the addresses table with spending
// information from a rows of the vins table specified by vinDbID.
func SetSpendingForVinDbID(db *sql.DB, vinDbID uint64) (int64, error) {
	// get funding details for vin and set them in the address table
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	// Get the funding tx outpoint (vins table) for the vin DB ID
	var prevOutHash, txHash string
	var prevOutVoutInd, txVinInd uint32
	var prevOutTree, txTree int8
	var isValid, isMainchain bool
	var valueIn, blockTime int64
	var txType int16
	err = dbtx.QueryRow(internal.SelectAllVinInfoByID, vinDbID).
		Scan(&txHash, &txVinInd, &txTree, &isValid, &isMainchain, &blockTime,
			&prevOutHash, &prevOutVoutInd, &prevOutTree, &valueIn, &txType)
	if err != nil {
		return 0, fmt.Errorf(`SetSpendingForVinDbID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	// skip coinbase inputs
	if bytes.Equal(zeroHashStringBytes, []byte(prevOutHash)) {
		return 0, dbtx.Rollback()
	}

	// Insert the spending tx info (addresses table) for the vin DB ID
	N, err := insertSpendingTxByPrptStmt(dbtx, prevOutHash, prevOutVoutInd,
		prevOutTree, txHash, txVinInd, vinDbID, false, isValid && isMainchain, txType)
	if err != nil {
		return 0, fmt.Errorf(`RowsAffected: %v + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return N, dbtx.Commit()
}

// SetSpendingForFundingOP inserts a new spending tx row and updates any
// corresponding funding tx row.
func SetSpendingForFundingOP(db *sql.DB, fundingTxHash string,
	fundingTxVoutIndex uint32, fundingTxTree int8, spendingTxHash string,
	spendingTxVinIndex uint32, spendingTXBlockTime, vinDbID uint64,
	checked, isValidMainchain bool, txType int16) (int64, error) {

	// Only allow atomic transactions to happen
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	c, err := insertSpendingTxByPrptStmt(dbtx, fundingTxHash, fundingTxVoutIndex,
		fundingTxTree, spendingTxHash, spendingTxVinIndex, vinDbID, checked,
		isValidMainchain, txType, spendingTXBlockTime)
	if err != nil {
		return 0, fmt.Errorf(`RowsAffected: %v + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return c, dbtx.Commit()
}

// insertSpendingTxByPrptStmt inserts into the addresses table a new spending
// transaction corresponding to a vin specified by the row ID vinDbID, and
// updates the spending information for the corresponding funding row in the
// addresses table.
func insertSpendingTxByPrptStmt(tx *sql.Tx, fundingTxHash string, fundingTxVoutIndex uint32,
	fundingTxTree int8, spendingTxHash string, spendingTxVinIndex uint32,
	vinDbID uint64, checked, validMainchain bool, txType int16, blockT ...uint64) (int64, error) {
	var addr string
	var value, rowID, blockTime uint64

	// Select id, address and value from the matching funding tx.
	// A maximum of one row and a minimum of none are expected.
	err := tx.QueryRow(internal.SelectAddressByTxHash,
		fundingTxHash, fundingTxVoutIndex, fundingTxTree).Scan(&addr, &value)
	switch err {
	case sql.ErrNoRows, nil:
		// If no row found or error is nil, continue
	default:
		return 0, fmt.Errorf("SelectAddressByTxHash: %v", err)
	}

	// Get first address in list.  TODO: actually handle bare multisig
	replacer := strings.NewReplacer("{", "", "}", "")
	addr = replacer.Replace(addr)
	newAddr := strings.Split(addr, ",")[0]

	// Check if the block time was passed
	if len(blockT) > 0 {
		blockTime = blockT[0]
	} else {
		// fetch the block time from the tx table
		err = tx.QueryRow(internal.SelectTxBlockTimeByHash, spendingTxHash).Scan(&blockTime)
		if err != nil {
			return 0, fmt.Errorf("SelectTxBlockTimeByHash: %v", err)
		}
	}

	// Insert the new spending tx
	var isFunding bool
	sqlStmt := internal.MakeAddressRowInsertStatement(checked)
	err = tx.QueryRow(sqlStmt, newAddr, fundingTxHash, spendingTxHash,
		spendingTxVinIndex, vinDbID, value, blockTime, isFunding,
		validMainchain, txType).Scan(&rowID)
	if err != nil {
		return 0, fmt.Errorf("InsertAddressRow: %v", err)
	}

	// Update the matchingTxHash for the funding tx output. matchingTxHash here
	// is the hash of the funding tx.
	res, err := tx.Exec(internal.SetAddressFundingForMatchingTxHash,
		spendingTxHash, fundingTxHash, fundingTxVoutIndex)
	if err != nil || res == nil {
		return 0, fmt.Errorf("SetAddressFundingForMatchingTxHash: %v", err)
	}

	return res.RowsAffected()
}

// SetSpendingByVinID is for when you got a new spending tx (vin entry) and you
// need to get the funding (previous output) tx info, and then update the
// corresponding row in the addresses table with the spending tx info.
func SetSpendingByVinID(db *sql.DB, vinDbID uint64, spendingTxDbID uint64,
	spendingTxHash string, spendingTxVinIndex uint32, checked, isValidMainchain bool,
	txType int16) (int64, error) {
	// get funding details for vin and set them in the address table
	dbtx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf(`unable to begin database transaction: %v`, err)
	}

	// Get the funding tx outpoint (vins table) for the vin DB ID
	var fundingTxHash string
	var fundingTxVoutIndex uint32
	var tree int8
	err = dbtx.QueryRow(internal.SelectFundingOutpointByVinID, vinDbID).
		Scan(&fundingTxHash, &fundingTxVoutIndex, &tree)
	if err != nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	// skip coinbase inputs
	if bytes.Equal(zeroHashStringBytes, []byte(fundingTxHash)) {
		return 0, dbtx.Rollback()
	}

	// Insert the spending tx info (addresses table) for the vin DB ID
	N, err := insertSpendingTxByPrptStmt(dbtx, fundingTxHash, fundingTxVoutIndex,
		tree, spendingTxHash, spendingTxVinIndex, vinDbID, checked, isValidMainchain, txType)
	if err != nil {
		return 0, fmt.Errorf(`RowsAffected: %v + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return N, dbtx.Commit()
}

// retrieveCoinSupply fetches the coin supply data from the vins table.
func retrieveCoinSupply(db *sql.DB) (*dbtypes.ChartsData, error) {
	rows, err := db.Query(internal.SelectCoinSupply)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	var sum float64
	items := new(dbtypes.ChartsData)
	for rows.Next() {
		var value, timestamp int64
		err = rows.Scan(&timestamp, &value)
		if err != nil {
			return nil, err
		}

		if value < 0 {
			value = 0
		}
		sum += dcrutil.Amount(value).ToCoin()
		items.Time = append(items.Time, uint64(timestamp))
		items.ValueF = append(items.ValueF, sum)
	}

	return items, nil
}

// --- agendas table ---

// retrieveAgendaVoteChoices retrieves for the specified agenda the vote counts
// for each choice and the total number of votes. The interval size is either a
// single block or a day, as specified by byType, where a value of 1 indicates a
// block and 0 indicates a day-long interval. For day intervals, the counts
// accumulate over time (cumulative sum), whereas for block intervals the counts
// are just for the block. The total length of time over all intervals always
// spans the locked-in period of the agenda.
func retrieveAgendaVoteChoices(db *sql.DB, agendaID string, byType int) (*dbtypes.AgendaVoteChoices, error) {
	// Query with block or day interval size
	var query = internal.SelectAgendasAgendaVotesByTime
	if byType == 1 {
		query = internal.SelectAgendasAgendaVotesByHeight
	}

	rows, err := db.Query(query, dbtypes.Yes, dbtypes.Abstain, dbtypes.No,
		agendaID)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	// Sum abstain, yes, no, and total votes
	var a, y, n, t uint64
	totalVotes := new(dbtypes.AgendaVoteChoices)
	for rows.Next() {
		// Parse the counts and time/height
		var abstain, yes, no, total, heightOrTime uint64
		err = rows.Scan(&heightOrTime, &yes, &abstain, &no, &total)
		if err != nil {
			return nil, err
		}

		// For day intervals, counts are cumulative
		if byType == 0 {
			a += abstain
			y += yes
			n += no
			t += total
			totalVotes.Time = append(totalVotes.Time, heightOrTime)
		} else {
			a = abstain
			y = yes
			n = no
			t = total
			totalVotes.Height = append(totalVotes.Height, heightOrTime)
		}

		totalVotes.Abstain = append(totalVotes.Abstain, a)
		totalVotes.Yes = append(totalVotes.Yes, y)
		totalVotes.No = append(totalVotes.No, n)
		totalVotes.Total = append(totalVotes.Total, t)
	}

	return totalVotes, nil
}

// --- transactions table ---

func InsertTx(db *sql.DB, dbTx *dbtypes.Tx, checked bool) (uint64, error) {
	insertStatement := internal.MakeTxInsertStatement(checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbTx.BlockHash, dbTx.BlockHeight, dbTx.BlockTime, dbTx.Time,
		dbTx.TxType, dbTx.Version, dbTx.Tree, dbTx.TxID, dbTx.BlockIndex,
		dbTx.Locktime, dbTx.Expiry, dbTx.Size, dbTx.Spent, dbTx.Sent, dbTx.Fees,
		dbTx.NumVin, dbtypes.UInt64Array(dbTx.VinDbIds),
		dbTx.NumVout, dbtypes.UInt64Array(dbTx.VoutDbIds),
		dbTx.IsValidBlock, dbTx.IsMainchainBlock).Scan(&id)
	return id, err
}

func InsertTxns(db *sql.DB, dbTxns []*dbtypes.Tx, checked bool) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeTxInsertStatement(checked))
	if err != nil {
		log.Errorf("Transaction INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
	}

	ids := make([]uint64, 0, len(dbTxns))
	for _, tx := range dbTxns {
		var id uint64
		err := stmt.QueryRow(
			tx.BlockHash, tx.BlockHeight, tx.BlockTime, tx.Time,
			tx.TxType, tx.Version, tx.Tree, tx.TxID, tx.BlockIndex,
			tx.Locktime, tx.Expiry, tx.Size, tx.Spent, tx.Sent, tx.Fees,
			tx.NumVin, dbtypes.UInt64Array(tx.VinDbIds),
			tx.NumVout, dbtypes.UInt64Array(tx.VoutDbIds), tx.IsValidBlock,
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

func RetrieveDbTxByHash(db *sql.DB, txHash string) (id uint64, dbTx *dbtypes.Tx, err error) {
	dbTx = new(dbtypes.Tx)
	vinDbIDs := dbtypes.UInt64Array(dbTx.VinDbIds)
	voutDbIDs := dbtypes.UInt64Array(dbTx.VoutDbIds)
	err = db.QueryRow(internal.SelectFullTxByHash, txHash).Scan(&id,
		&dbTx.BlockHash, &dbTx.BlockHeight, &dbTx.BlockTime, &dbTx.Time,
		&dbTx.TxType, &dbTx.Version, &dbTx.Tree, &dbTx.TxID, &dbTx.BlockIndex,
		&dbTx.Locktime, &dbTx.Expiry, &dbTx.Size, &dbTx.Spent, &dbTx.Sent,
		&dbTx.Fees, &dbTx.NumVin, &vinDbIDs, &dbTx.NumVout, &voutDbIDs,
		&dbTx.IsValidBlock, &dbTx.IsMainchainBlock)
	dbTx.VinDbIds = vinDbIDs
	dbTx.VoutDbIds = voutDbIDs
	return
}

func RetrieveFullTxByHash(db *sql.DB, txHash string) (id uint64,
	blockHash string, blockHeight int64, blockTime int64, time int64,
	txType int16, version int32, tree int8, blockInd uint32,
	lockTime, expiry int32, size uint32, spent, sent, fees int64,
	numVin int32, vinDbIDs []int64, numVout int32, voutDbIDs []int64,
	isValidBlock, isMainchainBlock bool, err error) {
	var hash string
	err = db.QueryRow(internal.SelectFullTxByHash, txHash).Scan(&id, &blockHash,
		&blockHeight, &blockTime, &time, &txType, &version, &tree,
		&hash, &blockInd, &lockTime, &expiry, &size, &spent, &sent, &fees,
		&numVin, &vinDbIDs, &numVout, &voutDbIDs,
		&isValidBlock, &isMainchainBlock)
	return
}

// RetrieveDbTxsByHash retrieves all the rows of the transactions table,
// including the primary keys/ids, for the given transaction hash.
func RetrieveDbTxsByHash(db *sql.DB, txHash string) (ids []uint64, dbTxs []*dbtypes.Tx, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectFullTxsByHash, txHash)
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
			&dbTx.BlockHash, &dbTx.BlockHeight, &dbTx.BlockTime, &dbTx.Time,
			&dbTx.TxType, &dbTx.Version, &dbTx.Tree, &dbTx.TxID, &dbTx.BlockIndex,
			&dbTx.Locktime, &dbTx.Expiry, &dbTx.Size, &dbTx.Spent, &dbTx.Sent,
			&dbTx.Fees, &dbTx.NumVin, &vinids, &dbTx.NumVout, &voutids,
			&dbTx.IsValidBlock, &dbTx.IsMainchainBlock)
		if err != nil {
			break
		}

		dbTx.VinDbIds = vinids
		dbTx.VoutDbIds = voutids

		ids = append(ids, id)
		dbTxs = append(dbTxs, &dbTx)
	}
	return
}

// RetrieveTxnsVinsByBlock retrieves for all the transactions in the specified
// block the vin_db_ids arrays, is_valid, and is_mainchain.
func RetrieveTxnsVinsByBlock(db *sql.DB, blockHash string) (vinDbIDs []dbtypes.UInt64Array,
	areValid []bool, areMainchain []bool, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectTxnsVinsByBlock, blockHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var ids dbtypes.UInt64Array
		var isValid, isMainchain bool
		err = rows.Scan(&ids, &isValid, &isMainchain)
		if err != nil {
			break
		}

		vinDbIDs = append(vinDbIDs, ids)
		areValid = append(areValid, isValid)
		areMainchain = append(areMainchain, isMainchain)
	}
	return
}

// RetrieveTxnsVinsVoutsByBlock retrieves for all the transactions in the
// specified block the vin_db_ids and vout_db_ids arrays.
func RetrieveTxnsVinsVoutsByBlock(db *sql.DB, blockHash string, onlyRegular bool) (vinDbIDs, voutDbIDs []dbtypes.UInt64Array,
	areMainchain []bool, err error) {
	stmt := internal.SelectTxnsVinsVoutsByBlock
	if onlyRegular {
		stmt = internal.SelectRegularTxnsVinsVoutsByBlock
	}

	var rows *sql.Rows
	rows, err = db.Query(stmt, blockHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var vinIDs, voutIDs dbtypes.UInt64Array
		var isMainchain bool
		err = rows.Scan(&vinIDs, &voutIDs, &isMainchain)
		if err != nil {
			break
		}

		vinDbIDs = append(vinDbIDs, vinIDs)
		voutDbIDs = append(voutDbIDs, voutIDs)
		areMainchain = append(areMainchain, isMainchain)
	}
	return
}

func RetrieveTxByHash(db *sql.DB, txHash string) (id uint64, blockHash string,
	blockInd uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectTxByHash, txHash).Scan(&id, &blockHash, &blockInd, &tree)
	return
}

func RetrieveTxBlockTimeByHash(db *sql.DB, txHash string) (blockTime uint64, err error) {
	err = db.QueryRow(internal.SelectTxBlockTimeByHash, txHash).Scan(&blockTime)
	return
}

func RetrieveTxsByBlockHash(db *sql.DB, blockHash string) (ids []uint64, txs []string,
	blockInds []uint32, trees []int8, blockTimes []uint64, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectTxsByBlockHash, blockHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var id, blockTime uint64
		var tx string
		var bind uint32
		var tree int8
		err = rows.Scan(&id, &tx, &bind, &tree, &blockTime)
		if err != nil {
			break
		}

		ids = append(ids, id)
		txs = append(txs, tx)
		blockInds = append(blockInds, bind)
		trees = append(trees, tree)
		blockTimes = append(blockTimes, blockTime)
	}

	return
}

// RetrieveTxnsBlocks retrieves for the specified transaction hash the following
// data for each block containing the transactions: block_hash, block_index,
// is_valid, is_mainchain.
func RetrieveTxnsBlocks(db *sql.DB, txHash string) (blockHashes []string, blockHeights, blockIndexes []uint32, areValid, areMainchain []bool, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectTxsBlocks, txHash)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var hash string
		var height, idx uint32
		var isValid, isMainchain bool
		err = rows.Scan(&height, &hash, &idx, &isValid, &isMainchain)
		if err != nil {
			break
		}

		blockHeights = append(blockHeights, height)
		blockHashes = append(blockHashes, hash)
		blockIndexes = append(blockIndexes, idx)
		areValid = append(areValid, isValid)
		areMainchain = append(areMainchain, isMainchain)
	}
	return
}

func retrieveTxPerDay(db *sql.DB) (*dbtypes.ChartsData, error) {
	rows, err := db.Query(internal.SelectTxsPerDay)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	items := new(dbtypes.ChartsData)
	for rows.Next() {
		var timestr string
		var count uint64
		err = rows.Scan(&timestr, &count)
		if err != nil {
			return nil, err
		}

		items.TimeStr = append(items.TimeStr, timestr)
		items.Count = append(items.Count, count)
	}
	return items, nil
}

func retrieveTicketByOutputCount(db *sql.DB, dataType outputCountType) (*dbtypes.ChartsData, error) {
	var query string
	switch dataType {
	case outputCountByAllBlocks:
		query = internal.SelectTicketsOutputCountByAllBlocks
	case outputCountByTicketPoolWindow:
		query = internal.SelectTicketsOutputCountByTPWindow
	default:
		return nil, fmt.Errorf("unknown output count type '%v'", dataType)
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	items := new(dbtypes.ChartsData)
	for rows.Next() {
		var height, solo, pooled uint64
		err = rows.Scan(&height, &solo, &pooled)
		if err != nil {
			return nil, err
		}

		items.Height = append(items.Height, height)
		items.Solo = append(items.Solo, solo)
		items.Pooled = append(items.Pooled, pooled)
	}
	return items, nil
}

// --- blocks and block_chain tables ---

func InsertBlock(db *sql.DB, dbBlock *dbtypes.Block, isValid, isMainchain, checked bool) (uint64, error) {
	insertStatement := internal.MakeBlockInsertStatement(dbBlock, checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbBlock.Hash, dbBlock.Height, dbBlock.Size, isValid, isMainchain,
		dbBlock.Version, dbBlock.MerkleRoot, dbBlock.StakeRoot,
		dbBlock.NumTx, dbBlock.NumRegTx, dbBlock.NumStakeTx,
		dbBlock.Time, dbBlock.Nonce, dbBlock.VoteBits,
		dbBlock.FinalState, dbBlock.Voters, dbBlock.FreshStake,
		dbBlock.Revocations, dbBlock.PoolSize, dbBlock.Bits,
		dbBlock.SBits, dbBlock.Difficulty, dbBlock.ExtraData,
		dbBlock.StakeVersion, dbBlock.PreviousHash).Scan(&id)
	return id, err
}

// InsertBlockPrevNext inserts a new row of the block_chain table.
func InsertBlockPrevNext(db *sql.DB, blockDbID uint64,
	hash, prev, next string) error {
	rows, err := db.Query(internal.InsertBlockPrevNext, blockDbID, prev, hash, next)
	if err == nil {
		return rows.Close()
	}
	return err
}

// RetrieveBestBlockHeight gets the best block height (main chain only).
func RetrieveBestBlockHeight(db *sql.DB) (height uint64, hash string, id uint64, err error) {
	err = db.QueryRow(internal.RetrieveBestBlockHeight).Scan(&id, &hash, &height)
	return
}

// RetrieveBestBlockHeightAny gets the best block height, including side chains.
func RetrieveBestBlockHeightAny(db *sql.DB) (height uint64, hash string, id uint64, err error) {
	err = db.QueryRow(internal.RetrieveBestBlockHeightAny).Scan(&id, &hash, &height)
	return
}

// RetrieveBlockHash retrieves the hash of the block at the given height, if it
// exists (be sure to check error against sql.ErrNoRows!). WARNING: this returns
// the most recently added block at this height, but there may be others.
func RetrieveBlockHash(db *sql.DB, idx int64) (hash string, err error) {
	err = db.QueryRow(internal.SelectBlockHashByHeight, idx).Scan(&hash)
	return
}

// RetrieveBlockHeight retrieves the height of the block with the given hash, if
// it exists (be sure to check error against sql.ErrNoRows!).
func RetrieveBlockHeight(db *sql.DB, hash string) (height int64, err error) {
	err = db.QueryRow(internal.SelectBlockHeightByHash, hash).Scan(&height)
	return
}

// RetrieveBlockVoteCount gets the number of votes mined in a block.
func RetrieveBlockVoteCount(db *sql.DB, hash string) (numVotes int16, err error) {
	err = db.QueryRow(internal.SelectBlockVoteCount, hash).Scan(&numVotes)
	return
}

// RetrieveBlocksHashesAll retrieve the hash of every block in the blocks table,
// ordered by their row ID.
func RetrieveBlocksHashesAll(db *sql.DB) ([]string, error) {
	var hashes []string
	rows, err := db.Query(internal.SelectBlocksHashes)
	if err != nil {
		return hashes, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var hash string
		err = rows.Scan(&hash)
		if err != nil {
			break
		}

		hashes = append(hashes, hash)
	}
	return hashes, err
}

// RetrieveBlockChainDbID retrieves the row id in the block_chain table of the
// block with the given hash, if it exists (be sure to check error against
// sql.ErrNoRows!).
func RetrieveBlockChainDbID(db *sql.DB, hash string) (dbID uint64, err error) {
	err = db.QueryRow(internal.SelectBlockChainRowIDByHash, hash).Scan(&dbID)
	return
}

// RetrieveSideChainBlocks retrieves the block chain status for all known side
// chain blocks.
func RetrieveSideChainBlocks(db *sql.DB) (blocks []*dbtypes.BlockStatus, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectSideChainBlocks)
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
	return
}

// RetrieveSideChainTips retrieves the block chain status for all known side
// chain tip blocks.
func RetrieveSideChainTips(db *sql.DB) (blocks []*dbtypes.BlockStatus, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectSideChainTips)
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
	return
}

// RetrieveDisapprovedBlocks retrieves the block chain status for all blocks
// that had their regular transactions invalidated by stakeholder disapproval.
func RetrieveDisapprovedBlocks(db *sql.DB) (blocks []*dbtypes.BlockStatus, err error) {
	var rows *sql.Rows
	rows, err = db.Query(internal.SelectDisapprovedBlocks)
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
	return
}

// RetrieveBlockStatus retrieves the block chain status for the block with the
// specified hash.
func RetrieveBlockStatus(db *sql.DB, hash string) (bs dbtypes.BlockStatus, err error) {
	err = db.QueryRow(internal.SelectBlockStatus, hash).Scan(&bs.IsValid,
		&bs.IsMainchain, &bs.Height, &bs.PrevHash, &bs.Hash, &bs.NextHash)
	return
}

func RetrieveBlockSummaryByTimeRange(db *sql.DB, minTime, maxTime int64, limit int) ([]dbtypes.BlockDataBasic, error) {
	var blocks []dbtypes.BlockDataBasic
	var stmt *sql.Stmt
	var rows *sql.Rows
	var err error

	if limit == 0 {
		stmt, err = db.Prepare(internal.SelectBlockByTimeRangeSQLNoLimit)
		if err != nil {
			return nil, err
		}
		rows, err = stmt.Query(minTime, maxTime)
	} else {
		stmt, err = db.Prepare(internal.SelectBlockByTimeRangeSQL)
		if err != nil {
			return nil, err
		}
		rows, err = stmt.Query(minTime, maxTime, limit)
	}

	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var dbBlock dbtypes.BlockDataBasic
		if err = rows.Scan(&dbBlock.Hash, &dbBlock.Height, &dbBlock.Size, &dbBlock.Time, &dbBlock.NumTx); err != nil {
			log.Errorf("Unable to scan for block fields: %v", err)
		}
		blocks = append(blocks, dbBlock)
	}
	if err = rows.Err(); err != nil {
		log.Error(err)
	}
	return blocks, nil
}

// RetrieveTicketsPriceByHeight fetches the ticket price and its timestamp that
// are used to display the ticket price variation on ticket price chart. These
// data are fetched at an interval of chaincfg.Params.StakeDiffWindowSize.
func RetrieveTicketsPriceByHeight(db *sql.DB, val int64) (*dbtypes.ChartsData, error) {
	rows, err := db.Query(internal.SelectBlocksTicketsPrice, val)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	items := new(dbtypes.ChartsData)
	for rows.Next() {
		var timestamp, price uint64
		var difficulty float64
		err = rows.Scan(&price, &timestamp, &difficulty)
		if err != nil {
			return nil, err
		}

		items.Time = append(items.Time, timestamp)
		priceCoin := dcrutil.Amount(price).ToCoin()
		items.ValueF = append(items.ValueF, priceCoin)
		items.Difficulty = append(items.Difficulty, difficulty)
	}

	return items, nil
}

func RetrievePreviousHashByBlockHash(db *sql.DB, hash string) (previousHash string, err error) {
	err = db.QueryRow(internal.SelectBlocksPreviousHash, hash).Scan(&previousHash)
	return
}

func SetMainchainByBlockHash(db *sql.DB, hash string, isMainchain bool) (previousHash string, err error) {
	err = db.QueryRow(internal.UpdateBlockMainchain, hash, isMainchain).Scan(&previousHash)
	return
}

func retrieveBlockTicketsPoolValue(db *sql.DB) (*dbtypes.ChartsData, error) {
	rows, err := db.Query(internal.SelectBlocksBlockSize)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	items := new(dbtypes.ChartsData)
	var oldTimestamp, chainsize uint64
	for rows.Next() {
		var timestamp, blockSize, blocksCount, blockHeight uint64
		err = rows.Scan(&timestamp, &blockSize, &blocksCount, &blockHeight)
		if err != nil {
			return nil, err
		}

		val := int64(oldTimestamp - timestamp)
		if val < 0 {
			val = val * -1
		}
		chainsize += blockSize
		oldTimestamp = timestamp
		items.Time = append(items.Time, timestamp)
		items.Size = append(items.Size, blockSize)
		items.ChainSize = append(items.ChainSize, chainsize)
		items.Count = append(items.Count, blocksCount)
		items.ValueF = append(items.ValueF, float64(val))
		items.Value = append(items.Value, blockHeight)
	}

	return items, nil
}

// -- UPDATE functions for various tables ---

// UpdateTransactionsMainchain sets the is_mainchain column for the transactions
// in the specified block.
func UpdateTransactionsMainchain(db *sql.DB, blockHash string, isMainchain bool) (int64, []uint64, error) {
	rows, err := db.Query(internal.UpdateTxnsMainchainByBlock, isMainchain, blockHash)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to update transactions is_mainchain: %v", err)
	}
	defer closeRows(rows)

	var numRows int64
	var txRowIDs []uint64
	for rows.Next() {
		var id uint64
		err = rows.Scan(&id)
		if err != nil {
			break
		}

		txRowIDs = append(txRowIDs, id)
		numRows++
	}

	return numRows, txRowIDs, nil
}

// UpdateTransactionsValid sets the is_valid column of the transactions table
// for the regular (non-stake) transactions in the specified block.
func UpdateTransactionsValid(db *sql.DB, blockHash string, isValid bool) (int64, []uint64, error) {
	rows, err := db.Query(internal.UpdateRegularTxnsValidByBlock, isValid, blockHash)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to update regular transactions is_valid: %v", err)
	}
	defer closeRows(rows)

	var numRows int64
	var txRowIDs []uint64
	for rows.Next() {
		var id uint64
		err = rows.Scan(&id)
		if err != nil {
			break
		}

		txRowIDs = append(txRowIDs, id)
		numRows++
	}

	return numRows, txRowIDs, nil
}

// UpdateVotesMainchain sets the is_mainchain column for the votes in the
// specified block.
func UpdateVotesMainchain(db *sql.DB, blockHash string, isMainchain bool) (int64, error) {
	numRows, err := sqlExec(db, internal.UpdateVotesMainchainByBlock,
		"failed to update votes is_mainchain: ", isMainchain, blockHash)
	if err != nil {
		return 0, err
	}
	return numRows, nil
}

// UpdateTicketsMainchain sets the is_mainchain column for the tickets in the
// specified block.
func UpdateTicketsMainchain(db *sql.DB, blockHash string, isMainchain bool) (int64, error) {
	numRows, err := sqlExec(db, internal.UpdateTicketsMainchainByBlock,
		"failed to update tickets is_mainchain: ", isMainchain, blockHash)
	if err != nil {
		return 0, err
	}
	return numRows, nil
}

// UpdateAddressesMainchainByIDs sets the valid_mainchain column for the
// addresses specified by their vin (spending) or vout (funding) row IDs.
func UpdateAddressesMainchainByIDs(db *sql.DB, vinsBlk, voutsBlk []dbtypes.UInt64Array, isValidMainchain bool) (numSpendingRows, numFundingRows int64, err error) {
	// Spending/vins: Set valid_mainchain for the is_funding=false addresses
	// table rows using the vins row ids.
	var numUpdated int64
	for iTxn := range vinsBlk {
		for _, vin := range vinsBlk[iTxn] {
			numUpdated, err = sqlExec(db, internal.SetAddressMainchainForVinIDs,
				"failed to update spending addresses is_mainchain: ", isValidMainchain, vin)
			if err != nil {
				return
			}
			numSpendingRows += numUpdated
		}
	}

	// Funding/vouts: Set valid_mainchain for the is_funding=true addresses
	// table rows using the vouts row ids.
	for iTxn := range voutsBlk {
		for _, vout := range voutsBlk[iTxn] {
			numUpdated, err = sqlExec(db, internal.SetAddressMainchainForVoutIDs,
				"failed to update funding addresses is_mainchain: ", isValidMainchain, vout)
			if err != nil {
				return
			}
			numFundingRows += numUpdated
		}
	}
	return
}

// UpdateLastBlockValid updates the is_valid column of the block specified by
// the row id for the blocks table.
func UpdateLastBlockValid(db *sql.DB, blockDbID uint64, isValid bool) error {
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

// UpdateLastVins updates the is_valid and is_mainchain columns in the vins
// table for all of the transactions in the block specified by the given block
// hash.
func UpdateLastVins(db *sql.DB, blockHash string, isValid, isMainchain bool) error {
	_, txs, _, trees, timestamps, err := RetrieveTxsByBlockHash(db, blockHash)
	if err != nil {
		return err
	}

	for i, txHash := range txs {
		n, err := sqlExec(db, internal.SetIsValidIsMainchainByTxHash,
			"failed to update last vins tx validity: ", isValid, isMainchain,
			txHash, timestamps[i], trees[i])
		if err != nil {
			return err
		}

		if n < 1 {
			return fmt.Errorf(" failed to update at least 1 row")
		}
	}

	return nil
}

// UpdateLastAddressesValid sets valid_mainchain as specified by isValid for
// addresses table rows pertaining to regular (non-stake) transactions found in
// the given block.
func UpdateLastAddressesValid(db *sql.DB, blockHash string, isValid bool) error {
	// Get the row ids of all vins and vouts of regular txns in this block.
	onlyRegularTxns := true
	vinDbIDsBlk, voutDbIDsBlk, _, err := RetrieveTxnsVinsVoutsByBlock(db, blockHash, onlyRegularTxns)
	if err != nil {
		return fmt.Errorf("unable to retrieve vin data for block %s: %v", blockHash, err)
	}
	// Using vins and vouts row ids, update the valid_mainchain colume of the
	// rows of the address table referring to these vins and vouts.
	numAddrSpending, numAddrFunding, err := UpdateAddressesMainchainByIDs(db,
		vinDbIDsBlk, voutDbIDsBlk, isValid)
	if err != nil {
		log.Errorf("Failed to set addresses rows in block %s as sidechain: %v", blockHash, err)
	}
	addrsUpdated := numAddrSpending + numAddrFunding
	log.Debugf("Rows of addresses table updated: %d", addrsUpdated)
	return err
}

// UpdateBlockNext sets the next block's hash for the specified row of the
// block_chain table specified by DB row ID.
func UpdateBlockNext(db *sql.DB, blockDbID uint64, next string) error {
	res, err := db.Exec(internal.UpdateBlockNext, blockDbID, next)
	if err != nil {
		return err
	}
	numRows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("UpdateBlockNext failed to update exactly 1 row (%d)", numRows)
	}
	return nil
}

// UpdateBlockNextByHash sets the next block's hash for the block in the
// block_chain table specified by hash.
func UpdateBlockNextByHash(db *sql.DB, this, next string) error {
	res, err := db.Exec(internal.UpdateBlockNextByHash, this, next)
	if err != nil {
		return err
	}
	numRows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("UpdateBlockNextByHash failed to update exactly 1 row (%d)", numRows)
	}
	return nil
}
