package internal

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
)

const (
	// Insert
	insertTxRow0 = `INSERT INTO transactions (
		block_hash, block_height, block_time, time,
		tx_type, version, tree, tx_hash, block_index, 
		lock_time, expiry, size, spent, sent, fees, 
		num_vin, vin_db_ids, num_vout, vout_db_ids)
	VALUES (
		$1, $2, $3, $4, 
		$5, $6, $7, $8, $9,
		$10, $11, $12, $13, $14, $15,
		$16, $17, $18, $19) `
	insertTxRow = insertTxRow0 + `RETURNING id;`
	//insertTxRowChecked = insertTxRow0 + `ON CONFLICT (tx_hash, block_hash) DO NOTHING RETURNING id;`
	upsertTxRow = insertTxRow0 + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
		SET block_height = $2 RETURNING id;`
	insertTxRowReturnId = `WITH ins AS (` +
		insertTxRow0 +
		`ON CONFLICT (tx_hash, block_hash) DO UPDATE
		SET tx_hash = NULL WHERE FALSE
		RETURNING id
		)
	SELECT id FROM ins
	UNION  ALL
	SELECT id FROM transactions
	WHERE  tx_hash = $8 AND block_hash = $1
	LIMIT  1;`

	CreateTransactionTable = `CREATE TABLE IF NOT EXISTS transactions (
		id SERIAL8 PRIMARY KEY,
		/*block_db_id INT4,*/
		block_hash TEXT,
		block_height INT8,
		block_time INT8,
		time INT8,
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
		num_vin INT4,
		vin_db_ids INT8[],
		num_vout INT4,
		vout_db_ids INT8[]
	);`

	SelectTxByHash       = `SELECT id, block_hash, block_index, tree FROM transactions WHERE tx_hash = $1;`
	SelectTxsByBlockHash = `SELECT id, tx_hash, block_index, tree FROM transactions WHERE block_hash = $1;`

	SelectTxIDHeightByHash = `SELECT id, block_height FROM transactions WHERE tx_hash = $1;`

	SelectFullTxByHash = `SELECT id, block_hash, block_height, block_time, 
		time, tx_type, version, tree, tx_hash, block_index, lock_time, expiry, 
		size, spent, sent, fees, num_vin, vin_db_ids, num_vout, vout_db_ids 
		FROM transactions WHERE tx_hash = $1;`

	SelectRegularTxByHash = `SELECT id, block_hash, block_index FROM transactions WHERE tx_hash = $1 and tree=0;`
	SelectStakeTxByHash   = `SELECT id, block_hash, block_index FROM transactions WHERE tx_hash = $1 and tree=1;`

	IndexTransactionTableOnBlockIn = `CREATE UNIQUE INDEX uix_tx_block_in
		ON transactions(block_hash, block_index, tree)
		;` // STORING (tx_hash, block_hash)
	DeindexTransactionTableOnBlockIn = `DROP INDEX uix_tx_block_in;`

	IndexTransactionTableOnHashes = `CREATE UNIQUE INDEX uix_tx_hashes
		 ON transactions(tx_hash, block_hash)
		 ;` // STORING (block_hash, block_index, tree)
	DeindexTransactionTableOnHashes = `DROP INDEX uix_tx_hashes;`

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

	DeleteTxDuplicateRows = `DELETE FROM transactions
	WHERE id IN (SELECT id FROM (
			SELECT id, ROW_NUMBER()
			OVER (partition BY tx_hash, block_hash ORDER BY id) AS rnum
			FROM transactions) t
		WHERE t.rnum > 1);`

	RetrieveVoutDbIDs = `SELECT unnest(vout_db_ids) FROM transactions WHERE id = $1;`
	RetrieveVoutDbID  = `SELECT vout_db_ids[$2] FROM transactions WHERE id = $1;`
)

var (
	SelectAllRevokes = fmt.Sprintf(`SELECT id, tx_hash, block_height, vin_db_ids[0] FROM transactions WHERE tx_type = %d;`, stake.TxTypeSSRtx)
)

// func makeTxInsertStatement(voutDbIDs, vinDbIDs []uint64, vouts []*dbtypes.Vout, checked bool) string {
// 	voutDbIDsBIGINT := makeARRAYOfBIGINTs(voutDbIDs)
// 	vinDbIDsBIGINT := makeARRAYOfBIGINTs(vinDbIDs)
// 	voutCompositeARRAY := makeARRAYOfVouts(vouts)
// 	var insert string
// 	if checked {
// 		insert = insertTxRowChecked
// 	} else {
// 		insert = insertTxRow
// 	}
// 	return fmt.Sprintf(insert, voutDbIDsBIGINT, voutCompositeARRAY, vinDbIDsBIGINT)
// }

func MakeTxInsertStatement(checked bool) string {
	if checked {
		return upsertTxRow
	}
	return insertTxRow
}
