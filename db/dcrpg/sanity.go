// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"database/sql"

	"github.com/decred/dcrdata/db/dcrpg/v8/internal"
)

// CheckUnmatchedSpending checks the addresses table for spending rows where the
// matching (funding) txhash has not been set. The row IDs and addresses are
// returned. Non-zero length sizes is an indication of database corruption.
func CheckUnmatchedSpending(ctx context.Context, db *sql.DB) (ids []uint64, addresses []string, err error) {
	rows, err := db.QueryContext(ctx, internal.UnmatchedSpending)
	if err != nil {
		return nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var address string
		var id uint64
		err = rows.Scan(&id, &address)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		addresses = append(addresses, address)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	return
}

// CheckExtraMainchainBlocks checks the blocks table for multiple main chain
// blocks at a given height, which is not possible. The row IDs, block heights,
// and block hashes are returned. Non-zero length sizes is an indication of
// database corruption.
func CheckExtraMainchainBlocks(ctx context.Context, db *sql.DB) (ids, heights []uint64, hashes []string, err error) {
	rows, err := db.QueryContext(ctx, internal.ExtraMainchainBlocks)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var hash string
		var id, height uint64
		err = rows.Scan(&id, &height, &hash)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		heights = append(heights, height)
		hashes = append(hashes, hash)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckMislabeledInvalidBlocks checks the blocks table for blocks labeled as
// approved, but for which the following block has specified vote bits that
// invalidate it. This indicates likely database corruption.
func CheckMislabeledInvalidBlocks(ctx context.Context, db *sql.DB) (ids []uint64, hashes []string, err error) {
	rows, err := db.QueryContext(ctx, internal.MislabeledInvalidBlocks)
	if err != nil {
		return nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var hash string
		var id uint64
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

	return
}

// CheckUnspentTicketsWithSpendInfo checks the tickets table for tickets that
// are flagged as unspent, but which have set either a spend height or spending
// transaction row id. This indicates likely database corruption.
func CheckUnspentTicketsWithSpendInfo(ctx context.Context, db *sql.DB) (ids, spendHeights, spendTxDbIDs []uint64, err error) {
	rows, err := db.QueryContext(ctx, internal.UnspentTicketsWithSpendInfo)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id, height, txID uint64
		err = rows.Scan(&id, &height, &txID)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		spendHeights = append(spendHeights, height)
		spendTxDbIDs = append(spendTxDbIDs, txID)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckSpentTicketsWithoutSpendInfo checks the tickets table for tickets that
// are flagged as spent, but which do not have set either a spend height or
// spending transaction row id. This indicates likely database corruption.
func CheckSpentTicketsWithoutSpendInfo(ctx context.Context, db *sql.DB) (ids, spendHeights, spendTxDbIDs []uint64, err error) {
	rows, err := db.QueryContext(ctx, internal.SpentTicketsWithoutSpendInfo)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id, height, txID uint64
		err = rows.Scan(&id, &height, &txID)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		spendHeights = append(spendHeights, height)
		spendTxDbIDs = append(spendTxDbIDs, txID)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckMislabeledTicketTransactions checks the transactions table for ticket
// transactions that also appear in the tickets table, but which do not have the
// proper tx_type (1) set. This indicates likely database corruption.
func CheckMislabeledTicketTransactions(ctx context.Context, db *sql.DB) (ids []uint64, txTypes []int16, txHashes []string, err error) {
	rows, err := db.QueryContext(ctx, internal.MislabeledTicketTransactions)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var txType int16
		var txHash string
		err = rows.Scan(&id, &txType, &txHash)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		txTypes = append(txTypes, txType)
		txHashes = append(txHashes, txHash)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckMissingTickets checks the transactions table for ticket transactions
// (with tx_type=1) that do NOT appear in the tickets table. This indicates
// likely database corruption.
func CheckMissingTickets(ctx context.Context, db *sql.DB) (ids []uint64, txTypes []int16, txHashes []string, err error) {
	rows, err := db.QueryContext(ctx, internal.MissingTickets)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var txType int16
		var txHash string
		err = rows.Scan(&id, &txType, &txHash)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		txTypes = append(txTypes, txType)
		txHashes = append(txHashes, txHash)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckMissingTicketTransactions checks the tickets table for tickets that do
// NOT appear in the transactions table at all. This indicates likely database
// corruption.
func CheckMissingTicketTransactions(ctx context.Context, db *sql.DB) (ids []uint64, txHashes []string, err error) {
	rows, err := db.QueryContext(ctx, internal.MissingTicketTransactions)
	if err != nil {
		return nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var txHash string
		err = rows.Scan(&id, &txHash)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		txHashes = append(txHashes, txHash)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	return
}

// CheckBadSpentLiveTickets checks the tickets table for tickets with live pool
// status but not unspent status. This indicates likely database corruption.
func CheckBadSpentLiveTickets(ctx context.Context, db *sql.DB) (ids []uint64, txHashes []string, spendTypes []int16, err error) {
	rows, err := db.QueryContext(ctx, internal.BadSpentLiveTickets)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var txHash string
		var spendType int16
		err = rows.Scan(&id, &txHash)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		txHashes = append(txHashes, txHash)
		spendTypes = append(spendTypes, spendType)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckBadVotedTickets checks the tickets table for tickets with voted pool
// status but not voted status. This indicates likely database corruption.
func CheckBadVotedTickets(ctx context.Context, db *sql.DB) (ids []uint64, txHashes []string, spendTypes []int16, err error) {
	rows, err := db.QueryContext(ctx, internal.BadVotedTickets)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var txHash string
		var spendType int16
		err = rows.Scan(&id, &txHash)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		txHashes = append(txHashes, txHash)
		spendTypes = append(spendTypes, spendType)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckBadExpiredVotedTickets checks the tickets table for tickets with expired
// pool status but with voted status. This indicates likely database corruption.
func CheckBadExpiredVotedTickets(ctx context.Context, db *sql.DB) (ids []uint64, txHashes []string, spendTypes []int16, err error) {
	rows, err := db.QueryContext(ctx, internal.BadExpiredVotedTickets)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var txHash string
		var spendType int16
		err = rows.Scan(&id, &txHash)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		txHashes = append(txHashes, txHash)
		spendTypes = append(spendTypes, spendType)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckBadMissedVotedTickets checks the tickets table for tickets with missed
// pool status but with voted status. This indicates likely database corruption.
func CheckBadMissedVotedTickets(ctx context.Context, db *sql.DB) (ids []uint64, txHashes []string, spendTypes []int16, err error) {
	rows, err := db.QueryContext(ctx, internal.BadMissedVotedTickets)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var id uint64
		var txHash string
		var spendType int16
		err = rows.Scan(&id, &txHash)
		if err != nil {
			return nil, nil, nil, err
		}
		ids = append(ids, id)
		txHashes = append(txHashes, txHash)
		spendTypes = append(spendTypes, spendType)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	return
}

// CheckBadBlockApproval checks the blocks table for blocks with an incorrect
// approval flag as determined by computing (approvals/total_votes > 0.5).
func CheckBadBlockApproval(ctx context.Context, db *sql.DB) (hashes []string, approvals, disapprovals, totals []int16, approvedActual, approvedSet []bool, err error) {
	rows, err := db.QueryContext(ctx, internal.BadBlockApproval)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	defer closeRows(rows)

	for rows.Next() {
		var hash string
		var appr, disappr, tot int16
		var apprAct, apprSet bool
		err = rows.Scan(&hash, appr, disappr, tot, apprAct, apprSet)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}

		hashes = append(hashes, hash)
		approvals = append(approvals, appr)
		disapprovals = append(disapprovals, disappr)
		totals = append(totals, tot)
		approvedActual = append(approvedActual, apprAct)
		approvedSet = append(approvedSet, apprSet)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	return
}
