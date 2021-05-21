// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package dcrpg

// Deletion of all data for a certain block (identified by hash), is performed
// using the following relationships:
//
// blocks -> hash identifies: votes, misses, tickets, transactions
//        -> txdbids & stxdbids identifies: transactions
//        -> use previous_hash to continue to parent block
//
// transactions -> vin_db_ids identifies: vins, addresses where is_funding=false
//              -> vout_db_ids identifies vouts, addresses where is_funding=true
//
// tickets -> purchase_tx_db_id identifies the corresponding txn (but rows may
//            be removed directly by block hash)
//
// addresses -> tx_vin_vout_row_id where is_funding=true corresponds to transactions.vout_db_ids
//           -> tx_vin_vout_row_id where is_funding=false corresponds to transactions.vin_db_ids
//
// For example, REMOVAL of a block's data could be performed in the following
// manner, where [] indicates primary key/row ID lookup:
//	1. vin_DB_IDs = transactions[blocks.txdbids].vin_db_ids
//	2. Remove vins[vin_DB_IDs]
//	3. vout_DB_IDs = transactions[blocks.txdbids].vout_db_ids
//	4. Remove vouts[vout_DB_IDs]
//	5. Remove addresses WHERE tx_vin_vout_row_id=vout_DB_IDs AND is_funding=true
//	6. Remove addresses WHERE tx_vin_vout_row_id=vin_DB_IDs AND is_funding=false
//	7. Repeat 1-6 for blocks.stxdbids (instead of blocks.txdbids)
//	8. Remove tickets where purchase_tx_db_id = blocks.stxdbids
//	   OR Remove tickets by block_hash
//	9. Remove votes by block_hash
//	10. Remove misses by block_hash
//	11. Remove transactions[txdbids] and transactions[stxdbids]
//
// Use DeleteBlockData to delete all data across these tables for a certain block.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/decred/dcrdata/db/dcrpg/v6/internal"
	"github.com/decred/dcrdata/v6/db/dbtypes"
)

func deleteMissesForBlock(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteMisses, "failed to delete misses", hash)
}

func deleteVotesForBlock(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteVotes, "failed to delete votes", hash)
}

func deleteTicketsForBlock(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteTicketsSimple, "failed to delete tickets", hash)
}

func deleteTreasuryTxnsForBlock(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteTreasuryTxns, "failed to delete treasury txns", hash)
}

func deleteSwapsForBlockHeight(dbTx SqlExecutor, height int64) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteSwaps, "failed to delete swaps", height)
}

func deleteTransactionsForBlock(dbTx *sql.Tx, hash string) (txRowIds []int64, err error) {
	var rows *sql.Rows
	rows, err = dbTx.Query(internal.DeleteTransactionsSimple, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		txRowIds = append(txRowIds, id)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return
}

func deleteVoutsForBlock(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteVouts, "failed to delete vouts", hash)
}

func deleteVoutsForBlockSubQry(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteVoutsSubQry, "failed to delete vouts", hash)
}

func deleteVinsForBlock(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteVins, "failed to delete vins", hash)
}

func deleteVinsForBlockSubQry(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteVinsSubQry, "failed to delete vins", hash)
}

func deleteAddressesForBlock(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteAddresses, "failed to delete addresses", hash)
}

func deleteAddressesForBlockSubQry(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteAddressesSubQry, "failed to delete addresses", hash)
}

func deleteBlock(dbTx SqlExecutor, hash string) (rowsDeleted int64, err error) {
	return sqlExec(dbTx, internal.DeleteBlock, "failed to delete block", hash)
}

func deleteBlockFromChain(dbTx *sql.Tx, hash string) (err error) {
	// Delete the row from block_chain where this_hash is the specified hash,
	// returning the previous block hash in the chain.
	var prevHash string
	err = dbTx.QueryRow(internal.DeleteBlockFromChain, hash).Scan(&prevHash)
	if err != nil {
		// If a row with this_hash was not found, and thus prev_hash is not set,
		// attempt to locate a row with next_hash set to the hash of this block,
		// and set it to the empty string.
		if err == sql.ErrNoRows {
			err = UpdateBlockNextByNextHash(dbTx, hash, "")
		}
		return
	}

	// For any row where next_hash is the prev_hash of the removed row, set
	// next_hash to and empty string since that block is no longer in the chain.
	return UpdateBlockNextByHash(dbTx, prevHash, "")
}

// RetrieveTxsBlocksAboveHeight returns all distinct mainchain block heights and
// hashes referenced in the transactions table above the given height.
func RetrieveTxsBlocksAboveHeight(ctx context.Context, db *sql.DB, height int64) (heights []int64, hashes []string, err error) {
	var rows *sql.Rows
	rows, err = db.QueryContext(ctx, internal.SelectTxsBlocksAboveHeight, height)
	if err != nil {
		return
	}

	for rows.Next() {
		var height int64
		var hash string
		if err = rows.Scan(&height, &hash); err != nil {
			return nil, nil, err
		}
		heights = append(heights, height)
		hashes = append(hashes, hash)
	}
	return
}

// RetrieveTxsBestBlockMainchain returns the best mainchain block's height from
// the transactions table. If the table is empty, a height of -1, an empty hash
// string, and a nil error are returned
func RetrieveTxsBestBlockMainchain(ctx context.Context, db *sql.DB) (height int64, hash string, err error) {
	err = db.QueryRowContext(ctx, internal.SelectTxsBestBlock).Scan(&height, &hash)
	if err == sql.ErrNoRows {
		err = nil
		height = -1
	}
	return
}

// DeleteBlockData removes all data for the specified block from every table.
// Data are removed from tables in the following order: vins, vouts, addresses,
// transactions, tickets, votes, misses, blocks, block_chain.
// WARNING: When no indexes are present, these queries are VERY SLOW.
func DeleteBlockData(ctx context.Context, db *sql.DB, hash string, height int64) (res dbtypes.DeletionSummary, err error) {
	// The data purge is an all or nothing operation (no partial removal of
	// data), so use a common sql.Tx for all deletions, and Commit in this
	// function rather after each deletion.
	var dbTx *sql.Tx
	dbTx, err = db.BeginTx(ctx, nil)
	if err != nil {
		err = fmt.Errorf("failed to start new DB transaction: %v", err)
		return
	}

	res.Timings = new(dbtypes.DeletionSummary)

	start := time.Now()
	if res.Vins, err = deleteVinsForBlockSubQry(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteVinsForBlockSubQry failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Vins = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Vouts, err = deleteVoutsForBlockSubQry(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteVoutsForBlockSubQry failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Vouts = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Addresses, err = deleteAddressesForBlockSubQry(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteAddressesForBlockSubQry failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Addresses = time.Since(start).Nanoseconds()

	// Deleting transactions rows follow deletion of vins, vouts, and addresses
	// rows since the transactions table is used to identify the vin and vout DB
	// row IDs for a transaction.
	start = time.Now()
	var txIDsRemoved []int64
	if txIDsRemoved, err = deleteTransactionsForBlock(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteTransactionsForBlock failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	var voutsReset int64
	voutsReset, err = resetSpendingForVoutsByTxRowID(dbTx, txIDsRemoved)
	if err != nil {
		err = fmt.Errorf(`resetSpendingForVoutsByTxRowID failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	if voutsReset != int64(len(txIDsRemoved)) {
		log.Warnf(`resetSpendingForVoutsByTxRowID reset %d rows, expected %d`,
			voutsReset, len(txIDsRemoved))
	}
	log.Tracef("Reset spend_tx_row_id for %d vouts.", voutsReset)
	res.Transactions = int64(len(txIDsRemoved))
	res.Timings.Transactions = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Tickets, err = deleteTicketsForBlock(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteTicketsForBlock failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Tickets = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Votes, err = deleteVotesForBlock(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteVotesForBlock failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Votes = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Misses, err = deleteMissesForBlock(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteMissesForBlock failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Misses = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Treasury, err = deleteTreasuryTxnsForBlock(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteTreasuryTxnsForBlock failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Treasury = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Swaps, err = deleteSwapsForBlockHeight(dbTx, height); err != nil {
		err = fmt.Errorf(`deleteSwapsForBlockHeight failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Swaps = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Blocks, err = deleteBlock(dbTx, hash); err != nil {
		err = fmt.Errorf(`deleteBlock failed with "%v". Rollback: %v`,
			err, dbTx.Rollback())
		return
	}
	res.Timings.Blocks = time.Since(start).Nanoseconds()
	if res.Blocks != 1 {
		log.Errorf("Expected to delete 1 row of blocks table; actually removed %d.",
			res.Blocks)
	}

	err = deleteBlockFromChain(dbTx, hash)
	switch err {
	case sql.ErrNoRows:
		// Just warn but do not return the error.
		err = nil
		log.Warnf("Block with hash %s not found in block_chain table.", hash)
	case nil:
		// Great. Go on to Commit.
	default: // err != nil && err != sql.ErrNoRows
		// Do not return an error if deleteBlockFromChain just did not delete
		// exactly 1 row. Commit and be done.
		if strings.HasPrefix(err.Error(), notOneRowErrMsg) {
			log.Warnf("deleteBlockFromChain: %v", err)
			err = dbTx.Commit()
		} else {
			err = fmt.Errorf(`deleteBlockFromChain failed with "%v". Rollback: %v`,
				err, dbTx.Rollback())
		}
		return
	}

	err = dbTx.Commit()

	return
}

// DeleteBestBlock removes all data for the best block in the DB from every
// table via DeleteBlockData. The returned height and hash are for the best
// block after successful data removal, or the initial best block if removal
// fails as indicated by a non-nil error value.
func DeleteBestBlock(ctx context.Context, db *sql.DB) (res dbtypes.DeletionSummary, height int64, hash string, err error) {
	height, hash, err = RetrieveBestBlock(ctx, db)
	if err != nil {
		return
	}

	res, err = DeleteBlockData(ctx, db, hash, height)
	if err != nil {
		return
	}

	height, hash, err = RetrieveBestBlock(ctx, db)
	if err != nil {
		return
	}

	err = SetDBBestBlock(db, hash, height)
	return
}

// DeleteBlocks removes all data for the N best blocks in the DB from every
// table via repeated calls to DeleteBestBlock.
func DeleteBlocks(ctx context.Context, N int64, db *sql.DB) (res []dbtypes.DeletionSummary, height int64, hash string, err error) {
	// If N is less than 1, get the current best block height and hash, then
	// return.
	if N < 1 {
		height, hash, err = RetrieveBestBlock(ctx, db)
		return
	}

	for i := int64(0); i < N; i++ {
		var resi dbtypes.DeletionSummary
		resi, height, hash, err = DeleteBestBlock(ctx, db)
		if err != nil {
			return
		}
		res = append(res, resi)
		if hash == "" {
			break
		}
		if (i%100 == 0 && i > 0) || i == N-1 {
			log.Debugf("Removed data for %d blocks.", i+1)
		}
	}

	return
}
