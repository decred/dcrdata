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
		num_vin, vin_db_ids, num_vout, vout_db_ids,
		is_valid, is_mainchain)
	VALUES (
		$1, $2, $3, $4, 
		$5, $6, $7, $8, $9,
		$10, $11, $12, $13, $14, $15,
		$16, $17, $18, $19,
		$20, $21) `
	insertTxRow = insertTxRow0 + `RETURNING id;`
	//insertTxRowChecked = insertTxRow0 + `ON CONFLICT (tx_hash, block_hash) DO NOTHING RETURNING id;`
	upsertTxRow = insertTxRow0 + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
		SET is_valid = $20, is_mainchain = $21 RETURNING id;`
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
		vout_db_ids INT8[],
		is_valid BOOLEAN,
		is_mainchain BOOLEAN
	);`

	SelectTxByHash = `SELECT id, block_hash, block_index, tree
		FROM transactions WHERE tx_hash = $1;`
	SelectTxsByBlockHash = `SELECT id, tx_hash, block_index, tree, block_time
		FROM transactions WHERE block_hash = $1;`

	SelectTxBlockTimeByHash = `SELECT block_time FROM transactions where tx_hash = $1 
		ORDER BY block_time DESC LIMIT 1;`

	SelectTxIDHeightByHash = `SELECT id, block_height FROM transactions WHERE tx_hash = $1;`

	SelectTxsPerDay = `SELECT to_timestamp(time)::date as date, count(*) FROM transactions
		GROUP BY date ORDER BY date;`

	SelectFullTxByHash = `SELECT id, block_hash, block_height, block_time, 
		time, tx_type, version, tree, tx_hash, block_index, lock_time, expiry, 
		size, spent, sent, fees, num_vin, vin_db_ids, num_vout, vout_db_ids,
		is_valid, is_mainchain
		FROM transactions WHERE tx_hash = $1;`

	SelectTxnsVinsByBlock = `SELECT vin_db_ids, is_valid, is_mainchain
		FROM transactions WHERE block_hash = $1;`

	SelectTxnsVinsVoutsByBlock = `SELECT vin_db_ids, vout_db_ids, is_mainchain
		FROM transactions WHERE block_hash = $1;`

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

	SelectRegularTxByHash = `SELECT id, block_hash, block_index FROM transactions WHERE tx_hash = $1 and tree=0;`
	SelectStakeTxByHash   = `SELECT id, block_hash, block_index FROM transactions WHERE tx_hash = $1 and tree=1;`

	IndexTransactionTableOnBlockIn = `CREATE UNIQUE INDEX uix_tx_block_in
		ON transactions(block_hash, block_index, tree);` // with cockroach: STORING (tx_hash, block_hash)
	DeindexTransactionTableOnBlockIn = `DROP INDEX uix_tx_block_in;`

	IndexTransactionTableOnHashes = `CREATE UNIQUE INDEX uix_tx_hashes
		 ON transactions(tx_hash, block_hash);`
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
	SelectAllRevokes = fmt.Sprintf(`SELECT id, tx_hash, block_height, vin_db_ids[0] `+
		`FROM transactions WHERE tx_type = %d;`, stake.TxTypeSSRtx)

	SelectTicketsOutputCountByAllBlocks = fmt.Sprintf(`SELECT block_height,
		SUM(CASE WHEN num_vout = 3 THEN 1 ELSE 0 END) as solo,
		SUM(CASE WHEN num_vout = 5 THEN 1 ELSE 0 END) as pooled
		FROM transactions WHERE tx_type = %d GROUP BY block_height
		ORDER BY block_height;`, stake.TxTypeSStx)

	SelectTicketsOutputCountByTPWindow = fmt.Sprintf(`SELECT
		floor(block_height/144) as count,
		SUM(CASE WHEN num_vout = 3 THEN 1 ELSE 0 END) as solo,
		SUM(CASE WHEN num_vout = 5 THEN 1 ELSE 0 END) as pooled
		FROM transactions WHERE tx_type = %d
		GROUP BY count ORDER BY count;`, stake.TxTypeSStx)
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
