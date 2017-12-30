// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"database/sql"
	"fmt"

	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/dcrdata/dcrdata/db/dcrpg/internal"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/lib/pq"
)

type TicketSpendType int16

const (
	TicketExpired TicketSpendType = iota - 1
	TicketLive
	TicketVoted
	TicketRevoked
)

func ExistsIndex(db *sql.DB, indexName string) (exists bool, err error) {
	err = db.QueryRow(internal.IndexExists, indexName, "public").Scan(&exists)
	return
}

func IsUniqueIndex(db *sql.DB, indexName string) (isUnique bool, err error) {
	err = db.QueryRow(internal.IndexIsUnique, indexName, "public").Scan(&isUnique)
	return
}

func RetrievePkScriptByID(db *sql.DB, id uint64) (pkScript []byte, err error) {
	err = db.QueryRow(internal.SelectPkScriptByID, id).Scan(&pkScript)
	return
}

func RetrieveVoutIDByOutpoint(db *sql.DB, txHash string, voutIndex uint32) (id uint64, err error) {
	err = db.QueryRow(internal.SelectVoutIDByOutpoint, txHash, voutIndex).Scan(&id)
	return
}

func RetrieveAllVotesDbIDsHeightsTicketHashes(db *sql.DB) (ids []uint64, heights []int64,
	ticketHashes []string, err error) {
	rows, err := db.Query(internal.SelectAllVoteDbIDsHeightsTicketHashes)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var id uint64
		var height int64
		var ticketHash string
		err = rows.Scan(&id, &height, &ticketHash)
		if err != nil {
			break
		}

		ids = append(ids, id)
		heights = append(heights, height)
		ticketHashes = append(ticketHashes, ticketHash)
	}
	return
}

func RetrieveTicketIDHeightByHash(db *sql.DB, ticketHash string) (id uint64, blockHeight int64, err error) {
	err = db.QueryRow(internal.SelectTicketIDHeightByHash, ticketHash).Scan(&id, &blockHeight)
	return
}

func RetrieveTicketIDByHash(db *sql.DB, ticketHash string) (id uint64, err error) {
	err = db.QueryRow(internal.SelectTicketIDByHash, ticketHash).Scan(&id)
	return
}

func RetrieveTicketIDsByHashes(db *sql.DB, ticketHashes []string) (ids []uint64, err error) {
	dbtx, err := db.Begin()
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

func SetSpendingForTickets(db *sql.DB, ticketDbIDs, spendDbIDs []uint64,
	blockHeights []int64, spendTypes []TicketSpendType) (int64, error) {
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
			ticketDbID, blockHeights[i], spendDbIDs[i], spendTypes[i])
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

func setSpendingForTickets(dbtx *sql.Tx, ticketDbIDs, spendDbIDs []uint64,
	blockHeights []int64, spendTypes []TicketSpendType) error {
	stmt, err := dbtx.Prepare(internal.SetTicketSpendingInfoForTicketDbID)
	if err != nil {
		return fmt.Errorf("tickets SELECT prepare failed: %v", err)
	}

	rowsAffected := make([]int64, len(ticketDbIDs))
	for i, ticketDbID := range ticketDbIDs {
		rowsAffected[i], err = sqlExecStmt(stmt, "failed to set ticket spending info: ",
			ticketDbID, blockHeights[i], spendDbIDs[i], spendTypes[i])
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

func SetSpendingForVinDbIDs(db *sql.DB, vinDbIDs []uint64) ([]int64, int64, error) {
	// get funding details for vin and set them in the address table
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

	var addrSetStmt *sql.Stmt
	addrSetStmt, err = dbtx.Prepare(internal.SetAddressSpendingForOutpoint)
	if err != nil {
		log.Errorf("address row UPDATE prepare failed: %v", err)
		// Already up a creek. Just return error from Prepare.
		_ = vinGetStmt.Close()
		_ = dbtx.Rollback()
		return nil, 0, err
	}

	bail := func() error {
		// Already up a creek. Just return error from Prepare.
		_ = vinGetStmt.Close()
		_ = addrSetStmt.Close()
		return dbtx.Rollback()
	}

	addressRowsUpdated := make([]int64, len(vinDbIDs))

	for iv, vinDbID := range vinDbIDs {
		// Get the funding tx outpoint (vins table) for the vin DB ID
		var prevOutHash, txHash string
		var prevOutVoutInd, txVinInd uint32
		var prevOutTree, txTree int8
		var id uint64
		err = vinGetStmt.QueryRow(vinDbID).Scan(&id,
			&txHash, &txVinInd, &txTree,
			&prevOutHash, &prevOutVoutInd, &prevOutTree)
		if err != nil {
			return addressRowsUpdated, 0, fmt.Errorf(`SetSpendingForVinDbIDs: `+
				`%v + %v (rollback)`, err, bail())
		}

		// skip coinbase inputs
		if bytes.Equal(zeroHashStringBytes, []byte(prevOutHash)) {
			continue
		}

		// Set the spending tx info (addresses table) for the vin DB ID
		var res sql.Result
		res, err = addrSetStmt.Exec(prevOutHash, prevOutVoutInd,
			0, txHash, txVinInd, vinDbID)
		if err != nil || res == nil {
			return addressRowsUpdated, 0, fmt.Errorf(`SetSpendingForVinDbIDs: `+
				`%v + %v (rollback)`, err, bail())
		}

		addressRowsUpdated[iv], err = res.RowsAffected()
		if err != nil {
			return addressRowsUpdated, 0, fmt.Errorf(`RowsAffected: `+
				`%v + %v (rollback)`, err, bail())
		}
	}

	// Close prepared statements. Ignore errors as we'll Commit regardless.
	_ = vinGetStmt.Close()
	_ = addrSetStmt.Close()

	var totalUpdated int64
	for _, n := range addressRowsUpdated {
		totalUpdated += n
	}

	return addressRowsUpdated, totalUpdated, dbtx.Commit()
}

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
	var id uint64
	err = dbtx.QueryRow(internal.SelectAllVinInfoByID, vinDbID).
		Scan(&id, &txHash, &txVinInd, &txTree,
			&prevOutHash, &prevOutVoutInd, &prevOutTree)
	if err != nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	// skip coinbase inputs
	if bytes.Equal(zeroHashStringBytes, []byte(prevOutHash)) {
		return 0, dbtx.Rollback()
	}

	// Set the spending tx info (addresses table) for the vin DB ID
	var res sql.Result
	res, err = dbtx.Exec(internal.SetAddressSpendingForOutpoint,
		prevOutHash, prevOutVoutInd,
		0, txHash, txVinInd, vinDbID)
	if err != nil || res == nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`RowsAffected: %v + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return N, dbtx.Commit()
}

func SetSpendingForFundingOP(db *sql.DB,
	fundingTxHash string, fundingTxVoutIndex uint32,
	spendingTxDbID uint64, spendingTxHash string, spendingTxVinIndex uint32,
	vinDbID uint64) (int64, error) {
	res, err := db.Exec(internal.SetAddressSpendingForOutpoint,
		fundingTxHash, fundingTxVoutIndex,
		spendingTxDbID, spendingTxHash, spendingTxVinIndex, vinDbID)
	if err != nil || res == nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetSpendingByVinID is for when you got a new spending tx (vin entry) and you
// need to get the funding (previous output) tx info, and then update the
// corresponding row in the addresses table with the spending tx info.
func SetSpendingByVinID(db *sql.DB, vinDbID uint64, spendingTxDbID uint64,
	spendingTxHash string, spendingTxVinIndex uint32) (int64, error) {
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

	// Set the spending tx info (addresses table) for the vin DB ID
	var res sql.Result
	res, err = dbtx.Exec(internal.SetAddressSpendingForOutpoint,
		fundingTxHash, fundingTxVoutIndex,
		spendingTxDbID, spendingTxHash, spendingTxVinIndex, vinDbID)
	if err != nil || res == nil {
		return 0, fmt.Errorf(`SetSpendingByVinID: %v + %v `+
			`(rollback)`, err, dbtx.Rollback())
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`RowsAffected: %v + %v (rollback)`,
			err, dbtx.Rollback())
	}

	return N, dbtx.Commit()
}

// DeleteDuplicateVins deletes rows in vin with duplicate tx information,
// leaving the one row with the lowest id.
func DeleteDuplicateVins(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, "uix_vin"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate vins: "
	return sqlExec(db, internal.DeleteVinsDuplicateRows, execErrPrefix)
}

// DeleteDuplicateVouts deletes rows in vouts with duplicate tx information,
// leaving the one row with the lowest id.
func DeleteDuplicateVouts(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, "uix_vout_txhash_ind"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate vouts: "
	return sqlExec(db, internal.DeleteVoutDuplicateRows, execErrPrefix)
}

// DeleteDuplicateTxns deletes rows in transactions with duplicate tx-block
// hashes, leaving the one row with the lowest id.
func DeleteDuplicateTxns(db *sql.DB) (int64, error) {
	if isuniq, err := IsUniqueIndex(db, "uix_tx_hashes"); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if isuniq {
		return 0, nil
	}
	execErrPrefix := "failed to delete duplicate transactions: "
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

func sqlExec(db *sql.DB, stmt, execErrPrefix string, args ...interface{}) (int64, error) {
	res, err := db.Exec(stmt, args...)
	if err != nil {
		return 0, fmt.Errorf(execErrPrefix + err.Error())
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

func sqlExecStmt(stmt *sql.Stmt, execErrPrefix string, args ...interface{}) (int64, error) {
	res, err := stmt.Exec(args...)
	if err != nil {
		return 0, fmt.Errorf(execErrPrefix + err.Error())
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

func SetSpendingForAddressDbID(db *sql.DB, addrDbID uint64, spendingTxDbID uint64,
	spendingTxHash string, spendingTxVinIndex uint32, vinDbID uint64) error {
	_, err := db.Exec(internal.SetAddressSpendingForID, addrDbID, spendingTxDbID,
		spendingTxHash, spendingTxVinIndex, vinDbID)
	return err
}

func RetrieveAddressRecvCount(db *sql.DB, address string) (count int64, err error) {
	err = db.QueryRow(internal.SelectAddressRecvCount, address).Scan(&count)
	return
}

func RetrieveAddressUnspent(db *sql.DB, address string) (count, totalAmount int64, err error) {
	err = db.QueryRow(internal.SelectAddressUnspentCountAndValue, address).
		Scan(&count, &totalAmount)
	return
}

func RetrieveAddressSpent(db *sql.DB, address string) (count, totalAmount int64, err error) {
	err = db.QueryRow(internal.SelectAddressSpentCountAndValue, address).
		Scan(&count, &totalAmount)
	return
}

func RetrieveAddressSpentUnspent(db *sql.DB, address string) (numSpent, numUnspent,
	totalSpent, totalUnspent int64, err error) {
	dbtx, err := db.Begin()
	if err != nil {
		err = fmt.Errorf("unable to begin database transaction: %v", err)
		return
	}

	var nu, tu sql.NullInt64
	err = dbtx.QueryRow(internal.SelectAddressUnspentCountAndValue, address).
		Scan(&nu, &tu)
	if err != nil && err != sql.ErrNoRows {
		if errRoll := dbtx.Rollback(); errRoll != nil {
			log.Errorf("Rollback failed: %v", errRoll)
		}
		err = fmt.Errorf("unable to QueryRow for unspent amount: %v", err)
		return
	}
	numUnspent, totalUnspent = nu.Int64, tu.Int64

	var ns, ts sql.NullInt64
	err = dbtx.QueryRow(internal.SelectAddressSpentCountAndValue, address).
		Scan(&ns, &ts)
	if err != nil && err != sql.ErrNoRows {
		if errRoll := dbtx.Rollback(); errRoll != nil {
			log.Errorf("Rollback failed: %v", errRoll)
		}
		err = fmt.Errorf("unable to QueryRow for spent amount: %v", err)
		return
	}
	numSpent, totalSpent = ns.Int64, ts.Int64

	err = dbtx.Rollback()
	return
}

func RetrieveAllAddressTxns(db *sql.DB, address string) ([]uint64, []*dbtypes.AddressRow, error) {
	rows, err := db.Query(internal.SelectAddressAllByAddress, address)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	return scanAddressQueryRows(rows)
}

func RetrieveAddressTxns(db *sql.DB, address string, N, offset int64) ([]uint64, []*dbtypes.AddressRow, error) {
	return retrieveAddressTxns(db, address, N, offset,
		internal.SelectAddressLimitNByAddressSubQry)
}

func RetrieveAddressTxnsAlt(db *sql.DB, address string, N, offset int64) ([]uint64, []*dbtypes.AddressRow, error) {
	return retrieveAddressTxns(db, address, N, offset,
		internal.SelectAddressLimitNByAddress)
}

func retrieveAddressTxns(db *sql.DB, address string, N, offset int64,
	statement string) ([]uint64, []*dbtypes.AddressRow, error) {
	rows, err := db.Query(statement, address, N, offset)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	return scanAddressQueryRows(rows)
}

func scanAddressQueryRows(rows *sql.Rows) (ids []uint64, addressRows []*dbtypes.AddressRow, err error) {
	for rows.Next() {
		var id uint64
		var addr dbtypes.AddressRow
		var spendingTxHash sql.NullString
		var spendingTxDbID, spendingTxVinIndex, vinDbID sql.NullInt64
		err = rows.Scan(&id, &addr.Address, &addr.FundingTxDbID, &addr.FundingTxHash,
			&addr.FundingTxVoutIndex, &addr.VoutDbID, &addr.Value,
			&spendingTxDbID, &spendingTxHash, &spendingTxVinIndex, &vinDbID)
		if err != nil {
			return
		}

		if spendingTxDbID.Valid {
			addr.SpendingTxDbID = uint64(spendingTxDbID.Int64)
		}
		if spendingTxHash.Valid {
			addr.SpendingTxHash = spendingTxHash.String
		}
		if spendingTxVinIndex.Valid {
			addr.SpendingTxVinIndex = uint32(spendingTxVinIndex.Int64)
		}
		if vinDbID.Valid {
			addr.VinDbID = uint64(vinDbID.Int64)
		}

		ids = append(ids, id)
		addressRows = append(addressRows, &addr)
	}
	return
}

func RetrieveAddressIDsByOutpoint(db *sql.DB, txHash string,
	voutIndex uint32) ([]uint64, []string, error) {
	var ids []uint64
	var addresses []string
	rows, err := db.Query(internal.SelectAddressIDsByFundingOutpoint, txHash, voutIndex)
	if err != nil {
		return ids, addresses, err
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var id uint64
		var addr string
		err = rows.Scan(&id, &addr)
		if err != nil {
			break
		}

		ids = append(ids, id)
		addresses = append(addresses, addr)
	}

	return ids, addresses, err
}

func RetrieveAllVinDbIDs(db *sql.DB) (vinDbIDs []uint64, err error) {
	rows, err := db.Query(internal.SelectVinIDsALL)
	if err != nil {
		return
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

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

func RetrieveSpendingTxByVinID(db *sql.DB, vinDbID uint64) (tx string,
	voutIndex uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectSpendingTxByVinID, vinDbID).Scan(&tx, &voutIndex, &tree)
	return
}

func RetrieveFundingOutpointByTxIn(db *sql.DB, txHash string,
	vinIndex uint32) (id uint64, tx string, index uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectFundingOutpointByTxIn, txHash, vinIndex).
		Scan(&id, &tx, &index, &tree)
	return
}

func RetrieveFundingOutpointByVinID(db *sql.DB, vinDbID uint64) (tx string, index uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectFundingOutpointByVinID, vinDbID).
		Scan(&tx, &index, &tree)
	return
}

func RetrieveVinByID(db *sql.DB, vinDbID uint64) (prevOutHash string, prevOutVoutInd uint32,
	prevOutTree int8, txHash string, txVinInd uint32, txTree int8, err error) {
	var id uint64
	err = db.QueryRow(internal.SelectAllVinInfoByID, vinDbID).
		Scan(&id, &txHash, &txVinInd, &txTree,
			&prevOutHash, &prevOutVoutInd, &prevOutTree)
	return
}

func RetrieveFundingTxByTxIn(db *sql.DB, txHash string, vinIndex uint32) (id uint64, tx string, err error) {
	err = db.QueryRow(internal.SelectFundingTxByTxIn, txHash, vinIndex).
		Scan(&id, &tx)
	return
}

func RetrieveFundingTxsByTx(db *sql.DB, txHash string) ([]uint64, []*dbtypes.Tx, error) {
	var ids []uint64
	var txs []*dbtypes.Tx
	rows, err := db.Query(internal.SelectFundingTxsByTx, txHash)
	if err != nil {
		return ids, txs, err
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

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

func RetrieveSpendingTxByTxOut(db *sql.DB, txHash string,
	voutIndex uint32) (id uint64, tx string, vin uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectSpendingTxByPrevOut,
		txHash, voutIndex).Scan(&id, &tx, &vin, &tree)
	return
}

func RetrieveSpendingTxsByFundingTx(db *sql.DB, fundingTxID string) (dbIDs []uint64,
	txns []string, vinInds []uint32, voutInds []uint32, err error) {
	rows, err := db.Query(internal.SelectSpendingTxsByPrevTx, fundingTxID)
	if err != nil {
		return
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

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

func RetrieveDbTxByHash(db *sql.DB, txHash string) (id uint64, dbTx *dbtypes.Tx, err error) {
	dbTx = new(dbtypes.Tx)
	vinDbIDs := dbtypes.UInt64Array(dbTx.VinDbIds)
	voutDbIDs := dbtypes.UInt64Array(dbTx.VoutDbIds)
	err = db.QueryRow(internal.SelectFullTxByHash, txHash).Scan(&id,
		&dbTx.BlockHash, &dbTx.BlockHeight, &dbTx.BlockTime, &dbTx.Time,
		&dbTx.TxType, &dbTx.Version, &dbTx.Tree, &dbTx.TxID, &dbTx.BlockIndex,
		&dbTx.Locktime, &dbTx.Expiry, &dbTx.Size, &dbTx.Spent, &dbTx.Sent,
		&dbTx.Fees, &dbTx.NumVin, &vinDbIDs, &dbTx.NumVout, &voutDbIDs)
	return
}

func RetrieveFullTxByHash(db *sql.DB, txHash string) (id uint64,
	blockHash string, blockHeight int64, blockTime int64, time int64,
	txType int16, version int32, tree int8, blockInd uint32,
	lockTime, expiry int32, size uint32, spent, sent, fees int64,
	numVin int32, vinDbIDs []int64, numVout int32, voutDbIDs []int64,
	err error) {
	var hash string
	err = db.QueryRow(internal.SelectFullTxByHash, txHash).Scan(&id, &blockHash,
		&blockHeight, &blockTime, &time, &txType, &version, &tree,
		&hash, &blockInd, &lockTime, &expiry, &size, &spent, &sent, &fees,
		&numVin, &vinDbIDs, &numVout, &voutDbIDs)
	return
}

func RetrieveTxByHash(db *sql.DB, txHash string) (id uint64, blockHash string, blockInd uint32, tree int8, err error) {
	err = db.QueryRow(internal.SelectTxByHash, txHash).Scan(&id, &blockHash, &blockInd, &tree)
	return
}

func RetrieveTxIDHeightByHash(db *sql.DB, txHash string) (id uint64, blockHeight int64, err error) {
	err = db.QueryRow(internal.SelectTxIDHeightByHash, txHash).Scan(&id, &blockHeight)
	return
}

func RetrieveRegularTxByHash(db *sql.DB, txHash string) (id uint64, blockHash string, blockInd uint32, err error) {
	err = db.QueryRow(internal.SelectRegularTxByHash, txHash).Scan(&id, &blockHash, &blockInd)
	return
}

func RetrieveStakeTxByHash(db *sql.DB, txHash string) (id uint64, blockHash string, blockInd uint32, err error) {
	err = db.QueryRow(internal.SelectStakeTxByHash, txHash).Scan(&id, &blockHash, &blockInd)
	return
}

func RetrieveTxsByBlockHash(db *sql.DB, blockHash string) (ids []uint64, txs []string, blockInds []uint32, trees []int8, err error) {
	rows, err := db.Query(internal.SelectTxsByBlockHash, blockHash)
	if err != nil {
		return
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

	for rows.Next() {
		var id uint64
		var tx string
		var bind uint32
		var tree int8
		err = rows.Scan(&id, &tx, &bind, &tree)
		if err != nil {
			break
		}

		ids = append(ids, id)
		txs = append(txs, tx)
		blockInds = append(blockInds, bind)
		trees = append(trees, tree)
	}

	return
}

func InsertBlock(db *sql.DB, dbBlock *dbtypes.Block, isValid, checked bool) (uint64, error) {
	insertStatement := internal.MakeBlockInsertStatement(dbBlock, checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbBlock.Hash, dbBlock.Height, dbBlock.Size, isValid, dbBlock.Version,
		dbBlock.MerkleRoot, dbBlock.StakeRoot,
		dbBlock.NumTx, dbBlock.NumRegTx, dbBlock.NumStakeTx,
		dbBlock.Time, dbBlock.Nonce, dbBlock.VoteBits,
		dbBlock.FinalState, dbBlock.Voters, dbBlock.FreshStake,
		dbBlock.Revocations, dbBlock.PoolSize, dbBlock.Bits,
		dbBlock.SBits, dbBlock.Difficulty, dbBlock.ExtraData,
		dbBlock.StakeVersion, dbBlock.PreviousHash).Scan(&id)
	return id, err
}

// UpdateLastBlock updates the is_valid column of the block specified by the row
// id for the blocks table.
func UpdateLastBlock(db *sql.DB, blockDbID uint64, isValid bool) error {
	numRows, err := sqlExec(db, internal.UpdateLastBlockValid,
		"failed to update last block validity: ", blockDbID, isValid)
	if err != nil {
		return err
	}
	if numRows != 1 {
		return fmt.Errorf("UpdateLastBlock failed to update exactly 1 row (%d)", numRows)
	}
	return nil
}

func RetrieveBestBlockHeight(db *sql.DB) (height uint64, hash string, id uint64, err error) {
	err = db.QueryRow(internal.RetrieveBestBlockHeight).Scan(&id, &hash, &height)
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
	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()

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

func InsertBlockPrevNext(db *sql.DB, blockDbID uint64,
	hash, prev, next string) error {
	rows, err := db.Query(internal.InsertBlockPrevNext, blockDbID, prev, hash, next)
	if err == nil {
		return rows.Close()
	}
	return err
}

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

func InsertVin(db *sql.DB, dbVin dbtypes.VinTxProperty) (id uint64, err error) {
	err = db.QueryRow(internal.InsertVinRow,
		dbVin.TxID, dbVin.TxIndex, dbVin.TxTree,
		dbVin.PrevTxHash, dbVin.PrevTxIndex, dbVin.PrevTxTree).Scan(&id)
	return
}

func InsertVins(db *sql.DB, dbVins dbtypes.VinTxPropertyARRAY) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.InsertVinRow)
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
			vin.PrevTxHash, vin.PrevTxIndex, vin.PrevTxTree).Scan(&id)
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
	addressRows := make([]dbtypes.AddressRow, 0, len(dbVouts)*2)
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

	ids := make([]uint64, 0, len(dbVouts))
	for _, vout := range dbVouts {
		var id uint64
		err := stmt.QueryRow(
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
				Address:            addr,
				FundingTxHash:      vout.TxHash,
				FundingTxVoutIndex: vout.TxIndex,
				VoutDbID:           id,
				Value:              vout.Value,
			})
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	return ids, addressRows, dbtx.Commit()
}

func InsertAddressOut(db *sql.DB, dbA *dbtypes.AddressRow, dupCheck bool) (uint64, error) {
	sqlStmt := internal.InsertAddressRow
	if dupCheck {
		sqlStmt = internal.UpsertAddressRow
	}
	var id uint64
	err := db.QueryRow(sqlStmt, dbA.Address, dbA.FundingTxDbID,
		dbA.FundingTxHash, dbA.FundingTxVoutIndex, dbA.VoutDbID,
		dbA.Value).Scan(&id)
	return id, err
}

func InsertAddressOuts(db *sql.DB, dbAs []*dbtypes.AddressRow, dupCheck bool) ([]uint64, error) {
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

	sqlStmt := internal.InsertAddressRow
	if dupCheck {
		sqlStmt = internal.UpsertAddressRow
	}

	stmt, err := dbtx.Prepare(sqlStmt)
	if err != nil {
		log.Errorf("AddressRow INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
	}

	ids := make([]uint64, 0, len(dbAs))
	for _, dbA := range dbAs {
		var id uint64
		err := stmt.QueryRow(dbA.Address, dbA.FundingTxDbID, dbA.FundingTxHash,
			dbA.FundingTxVoutIndex, dbA.VoutDbID, dbA.Value).Scan(&id)
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

// InsertTickets takes a slice of *dbtypes.Tx and corresponding DB row IDs for
// transactions, extracts the tickets, and inserts the tickets into the
// database. Outputs are a slice of DB row IDs of the inserted tickets, and an
// error.
func InsertTickets(db *sql.DB, dbTxns []*dbtypes.Tx, txDbIDs []uint64, checked bool) ([]uint64, error) {
	dbtx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeTicketInsertStatement(checked))
	if err != nil {
		log.Errorf("Ticket INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, err
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
		isSplit := len(tx.Vins) > 1

		var id uint64
		err := stmt.QueryRow(
			tx.TxID, tx.BlockHash, tx.BlockHeight, ticketDbIDs[i],
			stakesubmissionAddress, isMultisig, isSplit, tx.NumVin,
			price, fee, TicketLive).Scan(&id)
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
// Outputs are slices of DB row IDs for the votes and misses, and an error.
func InsertVotes(db *sql.DB, dbTxns []*dbtypes.Tx,
	msgBlock *MsgBlockPG, checked bool) ([]uint64, []*dbtypes.Tx, []string, []uint64, error) {
	// Choose only SSGen txns
	msgTxs := msgBlock.STransactions
	var voteTxs []*dbtypes.Tx
	var voteMsgTxs []*wire.MsgTx
	for i, tx := range dbTxns {
		if tx.TxType == int16(stake.TxTypeSSGen) {
			voteTxs = append(voteTxs, tx)
			voteMsgTxs = append(voteMsgTxs, msgTxs[i])
			if tx.TxID != msgTxs[i].TxHash().String() {
				return nil, nil, nil, nil, fmt.Errorf("txid of dbtypes.Tx does not match that of msgTx")
			}
		}
	}

	if len(voteTxs) == 0 {
		return nil, nil, nil, nil, nil
	}

	// Start DB transaction and prepare vote insert statement
	dbtx, err := db.Begin()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("unable to begin database transaction: %v", err)
	}

	stmt, err := dbtx.Prepare(internal.MakeVoteInsertStatement(checked))
	if err != nil {
		log.Errorf("Votes INSERT prepare: %v", err)
		_ = dbtx.Rollback() // try, but we want the Prepare error back
		return nil, nil, nil, nil, err
	}

	// Insert each vote, and build list of missed votes equal to
	// setdiff(Validators, votes).
	candidateBlockHash := msgBlock.Header.PrevBlock.String()
	ids := make([]uint64, 0, len(voteTxs))
	spentTicketHashes := make([]string, 0, len(voteTxs))
	misses := make([]string, len(msgBlock.Validators))
	copy(misses, msgBlock.Validators)
	for i, tx := range voteTxs {
		msgTx := voteMsgTxs[i]
		voteVersion := stake.SSGenVersion(msgTx)
		validBlock, voteBits, err := txhelpers.SSGenVoteBlockValid(msgTx)
		if err != nil {
			return nil, nil, nil, nil, err
		}

		stakeSubmissionAmount := dcrutil.Amount(msgTx.TxIn[1].ValueIn).ToCoin()
		stakeSubmissionTxHash := msgTx.TxIn[1].PreviousOutPoint.Hash.String()
		spentTicketHashes = append(spentTicketHashes, stakeSubmissionTxHash)

		voteReward := dcrutil.Amount(msgTx.TxIn[0].ValueIn).ToCoin()

		// delete spent ticket from missed list
		for im := range misses {
			if misses[im] == stakeSubmissionTxHash {
				misses[im] = misses[len(misses)-1]
				misses = misses[:len(misses)-1]
				break
			}
		}

		var id uint64
		err = stmt.QueryRow(
			tx.BlockHeight, tx.TxID, tx.BlockHash, candidateBlockHash,
			voteVersion, voteBits, validBlock.Validity,
			stakeSubmissionTxHash, stakeSubmissionAmount, voteReward).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			_ = stmt.Close() // try, but we want the QueryRow error back
			if errRoll := dbtx.Rollback(); errRoll != nil {
				log.Errorf("Rollback failed: %v", errRoll)
			}
			return nil, nil, nil, nil, err
		}
		ids = append(ids, id)
	}

	// Close prepared statement. Ignore errors as we'll Commit regardless.
	_ = stmt.Close()

	if len(ids)+len(misses) != 5 {
		fmt.Println(misses)
		fmt.Println(voteTxs)
		_ = dbtx.Rollback()
		panic(fmt.Sprintf("votes (%d) + misses (%d) != 5", len(ids), len(misses)))
	}

	// Store missed tickets
	var idsMisses []uint64
	if len(misses) > 0 {
		stmtMissed, err := dbtx.Prepare(internal.MakeMissInsertStatement(checked))
		if err != nil {
			log.Errorf("Miss INSERT prepare: %v", err)
			_ = dbtx.Rollback() // try, but we want the Prepare error back
			return nil, nil, nil, nil, err
		}

		blockHash := msgBlock.BlockHash().String()
		idsMisses = make([]uint64, 0, len(voteTxs))
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
				return nil, nil, nil, nil, err
			}
			idsMisses = append(idsMisses, id)
		}
		_ = stmtMissed.Close()
	}

	return ids, voteTxs, spentTicketHashes, idsMisses, dbtx.Commit()
}

func InsertTx(db *sql.DB, dbTx *dbtypes.Tx, checked bool) (uint64, error) {
	insertStatement := internal.MakeTxInsertStatement(checked)
	var id uint64
	err := db.QueryRow(insertStatement,
		dbTx.BlockHash, dbTx.BlockHeight, dbTx.BlockTime, dbTx.Time,
		dbTx.TxType, dbTx.Version, dbTx.Tree, dbTx.TxID, dbTx.BlockIndex,
		dbTx.Locktime, dbTx.Expiry, dbTx.Size, dbTx.Spent, dbTx.Sent, dbTx.Fees,
		dbTx.NumVin, dbTx.Vins, dbtypes.UInt64Array(dbTx.VinDbIds),
		dbTx.NumVout, pq.Array(dbTx.Vouts), dbtypes.UInt64Array(dbTx.VoutDbIds)).Scan(&id)
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
			tx.NumVin, tx.Vins, dbtypes.UInt64Array(tx.VinDbIds),
			tx.NumVout, pq.Array([]*dbtypes.Vout{}), dbtypes.UInt64Array(tx.VoutDbIds)).Scan(&id)
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
