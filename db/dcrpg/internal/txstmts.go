package internal

import (
	"fmt"

	"github.com/dcrdata/dcrdata/db/dbtypes"
)

const (
	// Insert
	insertTxRow0 = `INSERT INTO transactions (
		block_hash, block_index, tree, tx_hash, version,
		lock_time, expiry, num_vin, vins, vin_db_ids,
		num_vout, vouts, vout_db_ids)
	VALUES (
		$1, $2, $3, $4, $5,
		$6, $7, $8, $9, %s,
		$10, %s, %s) `
	insertTxRow        = insertTxRow0 + `RETURNING id;`
	insertTxRowChecked = insertTxRow0 + `ON CONFLICT (tx_hash, block_hash) DO NOTHING RETURNING id;`
	upsertTxRow        = insertTxRow0 + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
		SET block_hash = $1, block_index = $2, tree = $3 RETURNING id;`
	insertTxRowReturnId = `WITH ins AS (` +
		insertTxRow0 +
		`ON CONFLICT (tx_hash, block_hash) DO UPDATE
		SET tx_hash = NULL WHERE FALSE
		RETURNING id
		)
	SELECT id FROM ins
	UNION  ALL
	SELECT id FROM blocks
	WHERE  tx_hash = $3 AND block_hash = $1
	LIMIT  1;`

	CreateTransactionTable = `CREATE TABLE IF NOT EXISTS transactions (
		id SERIAL8 PRIMARY KEY,
		/*block_db_id INT4,*/
		block_hash TEXT,
		block_index INT4,
		tree INT2,
		tx_hash TEXT,
		version INT4,
		lock_time INT4,
		expiry INT4,
		num_vin INT4,
		vins JSONB,
		vin_db_ids INT8[],
		num_vout INT4,
		vouts vout_t[],
		vout_db_ids INT8[]
	);`

	SelectTxByHash       = `SELECT id, block_hash, block_index FROM transactions WHERE tx_hash = $1;`
	SelectTxsByBlockHash = `SELECT id, tx_hash, block_index FROM transactions WHERE block_hash = $1;`

	IndexTransactionTableOnBlockIn = `CREATE UNIQUE INDEX uix_tx_block_in
		ON transactions(block_hash, block_index, tree)
		;` // STORING (tx_hash, block_hash)
	DeindexTransactionTableOnBlockIn = `DROP INDEX uix_tx_block_in;`

	IndexTransactionTableOnHashes = `CREATE UNIQUE INDEX uix_tx_hashes
		 ON transactions(tx_hash, block_hash)
		 ;` // STORING (block_hash, block_index, tree)
	DeindexTransactionTableOnHashes = `DROP INDEX uix_tx_hashes;`

	SelectTxByPrevOut = `SELECT * FROM transactions WHERE vins @> json_build_array(json_build_object('prevtxhash',$1)::jsonb)::jsonb;`
	//SelectTxByPrevOut = `SELECT * FROM transactions WHERE vins #>> '{"prevtxhash"}' = '$1';`

	SelectTxsByPrevOutTx = `SELECT * FROM transactions WHERE vins @> json_build_array(json_build_object('prevtxhash',$1::TEXT)::jsonb)::jsonb;`
	// '[{"prevtxhash":$1}]'

	// RetrieveVoutValues = `WITH voutsOnly AS (
	// 		SELECT unnest((vouts)) FROM transactions WHERE id = $1
	// 	) SELECT v.* FROM voutsOnly v;`
	RetrieveVoutValues = `SELECT vo.value
		FROM  transactions txs, unnest(txs.vouts) vo
		WHERE txs.id = $1;`
	RetrieveVoutValue = `SELECT vouts[$2].value FROM transactions WHERE id = $1;`
)

func makeTxInsertStatement(voutDbIDs, vinDbIDs []uint64, vouts []*dbtypes.Vout, checked bool) string {
	voutDbIDsBIGINT := makeARRAYOfBIGINTs(voutDbIDs)
	vinDbIDsBIGINT := makeARRAYOfBIGINTs(vinDbIDs)
	voutCompositeARRAY := makeARRAYOfVouts(vouts)
	var insert string
	if checked {
		insert = insertTxRowChecked
	} else {
		insert = insertTxRow
	}
	return fmt.Sprintf(insert, voutDbIDsBIGINT, voutCompositeARRAY, vinDbIDsBIGINT)
}

func MakeTxInsertStatement(tx *dbtypes.Tx, checked bool) string {
	return makeTxInsertStatement(tx.VoutDbIds, tx.VinDbIds, tx.Vouts, checked)
}
