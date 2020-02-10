// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package internal

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake/v2"
)

// These queries relate primarily to the "transactions" table.
const (
	CreateTransactionTable = `CREATE TABLE IF NOT EXISTS transactions (
		id SERIAL8 PRIMARY KEY,
		block_hash TEXT,
		block_height INT8,
		block_time TIMESTAMPTZ,
		time TIMESTAMPTZ,
		tx_type INT4,
		version INT4,
		tree INT2,
		tx_hash TEXT,
		block_index INT4,
		lock_time INT4,
		expiry INT4,
		size INT4,
		spent INT8,
		sent INT8,
		fees INT8,
		mix_count INT4,
		mix_denom INT8,
		num_vin INT4,
		vin_db_ids INT8[],
		num_vout INT4,
		vout_db_ids INT8[],
		is_valid BOOLEAN,
		is_mainchain BOOLEAN
	);`

	// insertTxRow is the basis for several tx insert/upsert statements.
	insertTxRow = `INSERT INTO transactions (
		block_hash, block_height, block_time, time,
		tx_type, version, tree, tx_hash, block_index,
		lock_time, expiry, size, spent, sent, fees,
		mix_count, mix_denom,
		num_vin, vin_db_ids, num_vout, vout_db_ids,
		is_valid, is_mainchain)
	VALUES (
		$1, $2, $3, $4,
		$5, $6, $7, $8, $9,
		$10, $11, $12, $13, $14, $15,
		$16, $17,
		$18, $19, $20, $21,
		$22, $23) `

	// InsertTxRow inserts a new transaction row without checking for unique
	// index conflicts. This should only be used before the unique indexes are
	// created or there may be constraint violations (errors).
	InsertTxRow = insertTxRow + `RETURNING id;`

	// UpsertTxRow is an upsert (insert or update on conflict), returning the
	// inserted/updated transaction row id.
	UpsertTxRow = insertTxRow + `ON CONFLICT (tx_hash, block_hash) DO UPDATE
		SET is_valid = $22, is_mainchain = $23 RETURNING id;`

	// InsertTxRowOnConflictDoNothing allows an INSERT with a DO NOTHING on
	// conflict with transactions' unique tx index, while returning the row id
	// of either the inserted row or the existing row that causes the conflict.
	// The complexity of this statement is necessary to avoid an unnecessary
	// UPSERT, which would have performance consequences. The row is not locked.
	InsertTxRowOnConflictDoNothing = `WITH ins AS (` +
		insertTxRow +
		`	ON CONFLICT (tx_hash, block_hash) DO NOTHING -- no lock on row
			RETURNING id
		)
		SELECT id FROM ins
		UNION  ALL
		SELECT id FROM transactions
		WHERE  tx_hash = $8 AND block_hash = $1 -- only executed if no INSERT
		LIMIT  1;`

	// DeleteTxDuplicateRows removes rows that would violate the unique index
	// uix_tx_hashes. This should be run prior to creating the index.
	DeleteTxDuplicateRows = `DELETE FROM transactions
		WHERE id IN (SELECT id FROM (
			SELECT id, ROW_NUMBER()
			OVER (partition BY tx_hash, block_hash ORDER BY id) AS rnum
			FROM transactions) t
		WHERE t.rnum > 1);`

	SelectTxDupIDs = `WITH dups AS (
		SELECT array_agg(id) AS ids
		FROM transactions
		GROUP BY tx_hash, block_hash
		HAVING count(id)>1
	)
	SELECT array_agg(dupids) FROM (
		SELECT unnest(ids) AS dupids
		FROM dups
		ORDER BY dupids DESC
	) AS _;`

	DeleteTxRows = `DELETE FROM transactions
		WHERE id = ANY($1);`

	// IndexTransactionTableOnHashes creates the unique index uix_tx_hashes on
	// (tx_hash, block_hash).
	IndexTransactionTableOnHashes = `CREATE UNIQUE INDEX ` + IndexOfTransactionsTableOnHashes +
		` ON transactions(tx_hash, block_hash);`
	DeindexTransactionTableOnHashes = `DROP INDEX ` + IndexOfTransactionsTableOnHashes + ` CASCADE;`

	// Investigate removing this. block_hash is already indexed. It would be
	// unique with just (block_hash, block_index). And tree is likely not
	// important to index.  NEEDS TESTING BEFORE REMOVAL.
	IndexTransactionTableOnBlockIn = `CREATE UNIQUE INDEX ` + IndexOfTransactionsTableOnBlockInd +
		` ON transactions(block_hash, block_index, tree);`
	DeindexTransactionTableOnBlockIn = `DROP INDEX ` + IndexOfTransactionsTableOnBlockInd + ` CASCADE;`

	SelectTxByHash = `SELECT id, block_hash, block_index, tree
		FROM transactions
		WHERE tx_hash = $1
		ORDER BY is_mainchain DESC, is_valid DESC;`
	SelectTxsByBlockHash = `SELECT id, tx_hash, block_index, tree, block_time
		FROM transactions WHERE block_hash = $1;`

	SelectTxBlockTimeByHash = `SELECT block_time
		FROM transactions
		WHERE tx_hash = $1
		ORDER BY is_mainchain DESC, is_valid DESC, block_time DESC
		LIMIT 1;`

	SelectFullTxByHash = `SELECT id, block_hash, block_height, block_time,
			time, tx_type, version, tree, tx_hash, block_index, lock_time, expiry,
			size, spent, sent, fees, mix_count, mix_denom, num_vin, vin_db_ids,
			num_vout, vout_db_ids, is_valid, is_mainchain
		FROM transactions WHERE tx_hash = $1
		ORDER BY is_mainchain DESC, is_valid DESC, block_time DESC
		LIMIT 1;`

	SelectFullTxsByHash = `SELECT id, block_hash, block_height, block_time,
		time, tx_type, version, tree, tx_hash, block_index, lock_time, expiry,
		size, spent, sent, fees, mix_count, mix_denom, num_vin, vin_db_ids,
		num_vout, vout_db_ids, is_valid, is_mainchain
		FROM transactions WHERE tx_hash = $1
		ORDER BY is_mainchain DESC, is_valid DESC, block_time DESC;`

	SelectTxnsVinsByBlock = `SELECT vin_db_ids, is_valid, is_mainchain
		FROM transactions WHERE block_hash = $1;`

	SelectTxnsVinsVoutsByBlock = `SELECT vin_db_ids, vout_db_ids, is_mainchain
		FROM transactions WHERE block_hash = $1;`

	SelectTxsVinsAndVoutsIDs = `SELECT tx_type, vin_db_ids, vout_db_ids
		FROM transactions
		WHERE block_height BETWEEN $1 AND $2;`

	SelectTxsBlocksAboveHeight = `SELECT DISTINCT ON(block_height)
			block_height, block_hash
		FROM transactions
		WHERE block_height>$1
			AND is_mainchain;`

	SelectTxsBestBlock = `SELECT block_height, block_hash
		FROM transactions
		WHERE is_mainchain
		ORDER BY block_height DESC
		LIMIT 1;`

	SelectRegularTxnsVinsVoutsByBlock = `SELECT vin_db_ids, vout_db_ids, is_mainchain
		FROM transactions WHERE block_hash = $1 AND tree = 0;`

	SelectTxsBlocks = `SELECT block_height, block_hash, block_index, is_valid, is_mainchain
		FROM transactions
		WHERE tx_hash = $1
		ORDER BY is_valid DESC, is_mainchain DESC, block_height DESC;`

	UpdateRegularTxnsValidMainchainByBlock = `UPDATE transactions
		SET is_valid=$1, is_mainchain=$2
		WHERE block_hash=$3 and tree=0;`

	UpdateRegularTxnsValidByBlock = `UPDATE transactions
		SET is_valid=$1
		WHERE block_hash=$2 and tree=0;`

	UpdateTxnsMainchainByBlock = `UPDATE transactions
		SET is_mainchain=$1
		WHERE block_hash=$2
		RETURNING id;`

	UpdateTxnsValidMainchainAll = `UPDATE transactions
		SET is_valid=(b.is_valid::int + tree)::boolean, is_mainchain=b.is_mainchain
		FROM (
			SELECT hash, is_valid, is_mainchain
			FROM blocks
		) b
		WHERE block_hash = b.hash ;`

	UpdateRegularTxnsValidAll = `UPDATE transactions
		SET is_valid=b.is_valid
		FROM (
			SELECT hash, is_valid
			FROM blocks
		) b
		WHERE block_hash = b.hash AND tree = 0;`

	UpdateTxnsMainchainAll = `UPDATE transactions
		SET is_mainchain=b.is_mainchain
		FROM (
			SELECT hash, is_mainchain
			FROM blocks
		) b
		WHERE block_hash = b.hash;`

	SelectTicketsByType = `SELECT DISTINCT num_vout, COUNT(*)
		FROM transactions
		JOIN tickets
		ON transactions.id=purchase_tx_db_id
		WHERE pool_status=0
		AND tickets.is_mainchain = TRUE
		GROUP BY num_vout;`

	SelectTxnByDbID = `SELECT block_hash, block_height, tx_hash FROM transactions WHERE id = $1;`

	//SelectTxByPrevOut = `SELECT * FROM transactions WHERE vins @> json_build_array(json_build_object('prevtxhash',$1)::jsonb)::jsonb;`
	//SelectTxByPrevOut = `SELECT * FROM transactions WHERE vins #>> '{"prevtxhash"}' = '$1';`

	//SelectTxsByPrevOutTx = `SELECT * FROM transactions WHERE vins @> json_build_array(json_build_object('prevtxhash',$1::TEXT)::jsonb)::jsonb;`
	// '[{"prevtxhash":$1}]'

	// RetrieveVoutValues = `WITH voutsOnly AS (
	// 		SELECT unnest((vouts)) FROM transactions WHERE id = $1
	// 	) SELECT v.* FROM voutsOnly v;`
	// RetrieveVoutValues = `SELECT vo.value
	// 	FROM  transactions txs, unnest(txs.vouts) vo
	// 	WHERE txs.id = $1;`
	// RetrieveVoutValue = `SELECT vouts[$2].value FROM transactions WHERE id = $1;`

	// RetrieveVoutDbIDs = `SELECT unnest(vout_db_ids) FROM transactions WHERE id = $1;`
	// RetrieveVoutDbID  = `SELECT vout_db_ids[$2] FROM transactions WHERE id = $1;`
)

var (
	SelectAllRevokes = fmt.Sprintf(`SELECT id, tx_hash, block_height, vin_db_ids[0]
		FROM transactions
		WHERE tx_type = %d;`,
		stake.TxTypeSSRtx)

	SelectTicketsOutputCountByAllBlocks = `SELECT block_height,
		SUM(CASE WHEN num_vout = 3 THEN 1 ELSE 0 END) as solo,
		SUM(CASE WHEN num_vout = 5 THEN 1 ELSE 0 END) as pooled
		FROM transactions
		WHERE tx_type = $1
		AND block_height > $2
		GROUP BY block_height
		ORDER BY block_height;`

	SelectTicketsOutputCountByTPWindow = `SELECT
		floor(block_height/$3) as count,
		SUM(CASE WHEN num_vout = 3 THEN 1 ELSE 0 END) as solo,
		SUM(CASE WHEN num_vout = 5 THEN 1 ELSE 0 END) as pooled
		FROM transactions
		WHERE tx_type = $1
		AND block_height > $2
		GROUP BY count
		ORDER BY count;`

	SelectFeesPerBlockAboveHeight = `
		SELECT block_height, SUM(fees) as fees
		FROM transactions
		WHERE is_mainchain
			AND block_height > $1
		GROUP BY block_height
		ORDER BY block_height;`

	SelectMixedTotalPerBlock = `
		SELECT tx.block_height as block_height, 
		SUM(tx.mix_count * tx.mix_denom) as total_mixed
		FROM transactions tx
		WHERE tx.is_mainchain
			AND tx.block_height > $1
		GROUP BY tx.block_height
		ORDER BY tx.block_height;`

	SelectMixedVouts = `
		SELECT vouts.value, fund_tx.block_height, spend_tx.block_height, vouts.tx_tree
		FROM vouts
		JOIN transactions AS fund_tx ON vouts.tx_hash=fund_tx.tx_hash
		LEFT OUTER JOIN transactions AS spend_tx ON spend_tx_row_id=spend_tx.id
		WHERE (spend_tx.block_height > $1 or spend_tx.block_height is null) and mixed=true and value>0 
		ORDER BY fund_tx.block_height;`
)

// MakeTxInsertStatement returns the appropriate transaction insert statement
// for the desired conflict checking and handling behavior. For checked=false,
// no ON CONFLICT checks will be performed, and the value of updateOnConflict is
// ignored. This should only be used prior to creating the unique indexes as
// these constraints will cause an errors if an inserted row violates a
// constraint. For updateOnConflict=true, an upsert statement will be provided
// that UPDATEs the conflicting row. For updateOnConflict=false, the statement
// will either insert or do nothing, and return the inserted (new) or
// conflicting (unmodified) row id.
func MakeTxInsertStatement(checked, updateOnConflict bool) string {
	if !checked {
		return InsertTxRow
	}
	if updateOnConflict {
		return UpsertTxRow
	}
	return InsertTxRowOnConflictDoNothing
}
