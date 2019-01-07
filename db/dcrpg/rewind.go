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
	"time"

	"github.com/decred/dcrdata/v4/db/dbtypes"
	"github.com/decred/dcrdata/v4/db/dcrpg/internal"
)

func deleteMissesForBlock(db *sql.DB, hash string) (rowsDeleted int64, err error) {
	return sqlExec(db, internal.DeleteMisses, "failed to delete misses", hash)
}

func deleteVotesForBlock(db *sql.DB, hash string) (rowsDeleted int64, err error) {
	return sqlExec(db, internal.DeleteVotes, "failed to delete votes", hash)
}

func deleteTicketsForBlock(db *sql.DB, hash string) (rowsDeleted int64, err error) {
	return sqlExec(db, internal.DeleteTickets, "failed to delete tickets", hash)
}

func deleteTransactionsForBlock(db *sql.DB, hash string) (rowsDeleted int64, err error) {
	return sqlExec(db, internal.DeleteTransactions, "failed to delete transactions", hash)
}

func deleteVoutsForBlock(db *sql.DB, hash string) (rowsDeleted int64, err error) {
	return sqlExec(db, internal.DeleteVouts, "failed to delete vouts", hash)
}

func deleteVinsForBlock(db *sql.DB, hash string) (rowsDeleted int64, err error) {
	return sqlExec(db, internal.DeleteVins, "failed to delete vins", hash)
}

func deleteAddressesForBlock(db *sql.DB, hash string) (rowsDeleted int64, err error) {
	return sqlExec(db, internal.DeleteAddresses, "failed to delete addresses", hash)
}

func deleteBlock(db *sql.DB, hash string) (rowsDeleted int64, err error) {
	return sqlExec(db, internal.DeleteBlock, "failed to delete block", hash)
}

func deleteBlockFromChain(db *sql.DB, hash string) (err error) {
	// Delete the row from block_chain where this_hash is the specified hash,
	// returning the previous block hash in the chain.
	var prev_hash string
	err = db.QueryRow(internal.DeleteBlockFromChain, hash).Scan(&prev_hash)
	if err != nil {
		return
	}

	// For any row where next_hash is the prev_hash of the removed row, set
	// next_hash to and empty string since that block is no longer in the chain.
	return UpdateBlockNextByHash(db, prev_hash, "")
}

// DeleteBlockData removes all data for the specified block from every table.
// Data are removed from tables in the following order: vins, vouts, addresses,
// transactions, tickets, votes, misses, blocks, block_chain.
// TODO: Consider a transaction for atomic operation.
func DeleteBlockData(db *sql.DB, hash string) (res dbtypes.DeletionSummary, err error) {
	res.Timings = new(dbtypes.DeletionSummary)

	start := time.Now()
	if res.Vins, err = deleteVinsForBlock(db, hash); err != nil {
		return
	}
	res.Timings.Vins = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Vouts, err = deleteVoutsForBlock(db, hash); err != nil {
		return
	}
	res.Timings.Vouts = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Addresses, err = deleteAddressesForBlock(db, hash); err != nil {
		return
	}
	res.Timings.Addresses = time.Since(start).Nanoseconds()

	// Deleting transactions rows follow deletion of vins, vouts, and addresses
	// rows since the transactions table is used to identify the vin and vout DB
	// row IDs for a transaction.
	start = time.Now()
	if res.Transactions, err = deleteTransactionsForBlock(db, hash); err != nil {
		return
	}
	res.Timings.Transactions = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Tickets, err = deleteTicketsForBlock(db, hash); err != nil {
		return
	}
	res.Timings.Tickets = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Votes, err = deleteVotesForBlock(db, hash); err != nil {
		return
	}
	res.Timings.Votes = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Misses, err = deleteMissesForBlock(db, hash); err != nil {
		return
	}
	res.Timings.Misses = time.Since(start).Nanoseconds()

	start = time.Now()
	if res.Blocks, err = deleteBlock(db, hash); err != nil {
		return
	}
	res.Timings.Blocks = time.Since(start).Nanoseconds()
	if res.Blocks != 1 {
		log.Errorf("Expected to delete 1 row of blocks table; actually removed %d.",
			res.Blocks)
	}

	err = deleteBlockFromChain(db, hash)

	return
}

// DeleteBestBlock removes all data for the best block in the DB from every
// table via DeleteBlockData.
func DeleteBestBlock(db *sql.DB) (res dbtypes.DeletionSummary, height uint64, hash string, err error) {
	height, hash, _, err = RetrieveBestBlockHeight(context.Background(), db)
	if err != nil {
		return
	}

	res, err = DeleteBlockData(db, hash)
	return
}

// DeleteBlocks removes all data for the N best blocks in the DB from every
// table via repeated calls to DeleteBestBlock.
func DeleteBlocks(N int64, db *sql.DB) (res []dbtypes.DeletionSummary, height uint64, hash string, err error) {
	for i := int64(0); i < N; i++ {
		var resi dbtypes.DeletionSummary
		resi, height, hash, err = DeleteBestBlock(db)
		res = append(res, resi)
	}
	return
}
