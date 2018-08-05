package internal

import (
	"encoding/hex"
	"fmt"

	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/lib/pq"
)

const (
	// vins

	CreateVinTable = `CREATE TABLE IF NOT EXISTS vins (
		id SERIAL8 PRIMARY KEY,
		tx_hash TEXT,
		tx_index INT4,
		tx_tree INT2,
		is_valid BOOLEAN,
		is_mainchain BOOLEAN,
		block_time INT8,
		prev_tx_hash TEXT,
		prev_tx_index INT8,
		prev_tx_tree INT2,
		value_in INT8
	);`

	InsertVinRow0 = `INSERT INTO vins (tx_hash, tx_index, tx_tree, prev_tx_hash, prev_tx_index, prev_tx_tree,
		value_in, is_valid, is_mainchain, block_time) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) `
	InsertVinRow = InsertVinRow0 + `RETURNING id;`
	// InsertVinRowChecked = InsertVinRow0 +
	// 	`ON CONFLICT (tx_hash, tx_index, tx_tree) DO NOTHING RETURNING id;`
	UpsertVinRow = InsertVinRow0 + `ON CONFLICT (tx_hash, tx_index, tx_tree) DO UPDATE 
		SET tx_hash = $1, tx_index = $2, tx_tree = $3 RETURNING id;`

	DeleteVinsDuplicateRows = `DELETE FROM vins
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY tx_hash, tx_index, tx_tree ORDER BY id) AS rnum
				FROM vins) t
			WHERE t.rnum > 1);`

	IndexVinTableOnVins = `CREATE UNIQUE INDEX uix_vin
		ON vins(tx_hash, tx_index, tx_tree)
		;` // STORING (prev_tx_hash, prev_tx_index)
	IndexVinTableOnPrevOuts = `CREATE INDEX uix_vin_prevout
		ON vins(prev_tx_hash, prev_tx_index)
		;` // STORING (tx_hash, tx_index)
	DeindexVinTableOnVins     = `DROP INDEX uix_vin;`
	DeindexVinTableOnPrevOuts = `DROP INDEX uix_vin_prevout;`

	SelectVinIDsALL = `SELECT id FROM vins;`
	CountVinsRows   = `SELECT reltuples::BIGINT AS estimate FROM pg_class WHERE relname='vins';`

	SelectSpendingTxsByPrevTx = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	SelectSpendingTxByPrevOut = `SELECT id, tx_hash, tx_index FROM vins 
		WHERE prev_tx_hash=$1 AND prev_tx_index=$2;`
	SelectFundingTxsByTx        = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	SelectFundingTxByTxIn       = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`
	SelectFundingOutpointByTxIn = `SELECT id, prev_tx_hash, prev_tx_index, prev_tx_tree FROM vins 
		WHERE tx_hash=$1 AND tx_index=$2;`
	SelectFundingOutpointByVinID = `SELECT prev_tx_hash, prev_tx_index, prev_tx_tree FROM vins WHERE id=$1;`
	SelectFundingTxByVinID       = `SELECT prev_tx_hash FROM vins WHERE id=$1;`
	SelectSpendingTxByVinID      = `SELECT tx_hash, tx_index, tx_tree FROM vins WHERE id=$1;`
	SelectAllVinInfoByID         = `SELECT tx_hash, tx_index, tx_tree, is_valid, is_mainchain, block_time,
		prev_tx_hash, prev_tx_index, prev_tx_tree, value_in FROM vins WHERE id = $1;`

	SetIsValidIsMainchainByTxHash = `UPDATE vins SET is_valid = $1, is_mainchain = $2
		WHERE tx_hash = $3 AND block_time = $4 AND tx_tree = $5;`
	SetIsValidIsMainchainByVinID = `UPDATE vins SET is_valid = $2, is_mainchain = $3
		WHERE id = $1;`
	SetIsValidByTxHash = `UPDATE vins SET is_valid = $1
		WHERE tx_hash = $2 AND block_time = $3 AND tx_tree = $4;`
	SetIsValidByVinID = `UPDATE vins SET is_valid = $2
		WHERE id = $1;`
	SetIsMainchainByTxHash = `UPDATE vins SET is_mainchain = $1
		WHERE tx_hash = $2 AND block_time = $3 AND tx_tree = $4;`
	SetIsMainchainByVinID = `UPDATE vins SET is_mainchain = $2
		WHERE id = $1;`

	// SetVinsTableCoinSupplyUpgrade does not set is_mainchain because that upgrade comes after this one
	SetVinsTableCoinSupplyUpgrade = `UPDATE vins SET is_valid = $1, block_time = $3, value_in = $4
		WHERE tx_hash = $5 and tx_index = $6 and tx_tree = $7;`

	// SelectCoinSupply fetches the coin supply as of the latest block and sum
	// represents the generated coins for all stakebase and only
	// not-invalidated coinbase transactions.
	SelectCoinSupply = `SELECT block_time, sum(value_in) FROM vins WHERE
		prev_tx_hash = '0000000000000000000000000000000000000000000000000000000000000000' AND
		NOT (is_valid = false AND tx_tree = 0)
		AND is_mainchain = true GROUP BY block_time ORDER BY block_time;`

	CreateVinType = `CREATE TYPE vin_t AS (
		prev_tx_hash TEXT,
		prev_tx_index INTEGER,
		prev_tx_tree SMALLINT,
		htlc_seq_VAL INTEGER,
		value_in DOUBLE PRECISION,
		script_hex BYTEA
	);`

	// vouts

	CreateVoutTable = `CREATE TABLE IF NOT EXISTS vouts (
		id SERIAL8 PRIMARY KEY,
		tx_hash TEXT,
		tx_index INT4,
		tx_tree INT2,
		value INT8,
		version INT2,
		pkscript BYTEA,
		script_req_sigs INT4,
		script_type TEXT,
		script_addresses TEXT[]
	);`

	insertVoutRow0 = `INSERT INTO vouts (tx_hash, tx_index, tx_tree, value, 
		version, pkscript, script_req_sigs, script_type, script_addresses)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) `
	insertVoutRow = insertVoutRow0 + `RETURNING id;`
	//insertVoutRowChecked  = insertVoutRow0 + `ON CONFLICT (tx_hash, tx_index, tx_tree) DO NOTHING RETURNING id;`
	upsertVoutRow = insertVoutRow0 + `ON CONFLICT (tx_hash, tx_index, tx_tree) DO UPDATE 
		SET tx_hash = $1, tx_index = $2, tx_tree = $3 RETURNING id;`
	insertVoutRowReturnId = `WITH inserting AS (` +
		insertVoutRow0 +
		`ON CONFLICT (tx_hash, tx_index, tx_tree) DO UPDATE
		SET tx_hash = NULL WHERE FALSE
		RETURNING id
		)
	 SELECT id FROM inserting
	 UNION  ALL
	 SELECT id FROM vouts
	 WHERE  tx_hash = $1 AND tx_index = $2 AND tx_tree = $3
	 LIMIT  1;`

	DeleteVoutDuplicateRows = `DELETE FROM vouts
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY tx_hash, tx_index, tx_tree ORDER BY id) AS rnum
				FROM vouts) t
			WHERE t.rnum > 1);`

	SelectAddressByTxHash = `SELECT script_addresses, value FROM vouts
		WHERE tx_hash = $1 AND tx_index = $2 AND tx_tree = $3;`

	SelectPkScriptByID     = `SELECT pkscript FROM vouts WHERE id=$1;`
	SelectVoutIDByOutpoint = `SELECT id FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	SelectVoutByID         = `SELECT * FROM vouts WHERE id=$1;`

	RetrieveVoutValue  = `SELECT value FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	RetrieveVoutValues = `SELECT value, tx_index, tx_tree FROM vouts WHERE tx_hash=$1;`

	IndexVoutTableOnTxHashIdx = `CREATE UNIQUE INDEX uix_vout_txhash_ind
		ON vouts(tx_hash, tx_index, tx_tree);`
	DeindexVoutTableOnTxHashIdx = `DROP INDEX uix_vout_txhash_ind;`

	CreateVoutType = `CREATE TYPE vout_t AS (
		value INT8,
		version INT2,
		pkscript BYTEA,
		script_req_sigs INT4,
		script_type TEXT,
		script_addresses TEXT[]
	);`
)

func MakeVinInsertStatement(checked bool) string {
	if checked {
		return UpsertVinRow
	}
	return InsertVinRow
}

var (
	voutCopyStmt = pq.CopyIn("vouts",
		"tx_hash", "tx_index", "tx_tree", "value", "version",
		"pkscript", "script_req_sigs", " script_type", "script_addresses")
	vinCopyStmt = pq.CopyIn("vins",
		"tx_hash", "tx_index", "prev_tx_hash", "prev_tx_index")
)

func MakeVoutCopyInStatement() string {
	return voutCopyStmt
}

func MakeVinCopyInStatement() string {
	return vinCopyStmt
}

func MakeVoutInsertStatement(checked bool) string {
	if checked {
		return upsertVoutRow
	}
	return insertVoutRow
}

func makeARRAYOfVouts(vouts []*dbtypes.Vout) string {
	var rowSubStmts []string
	for i := range vouts {
		hexPkScript := hex.EncodeToString(vouts[i].ScriptPubKey)
		rowSubStmts = append(rowSubStmts,
			fmt.Sprintf(`ROW(%d, %d, decode('%s','hex'), %d, '%s', %s)`,
				vouts[i].Value, vouts[i].Version, hexPkScript,
				vouts[i].ScriptPubKeyData.ReqSigs, vouts[i].ScriptPubKeyData.Type,
				makeARRAYOfTEXT(vouts[i].ScriptPubKeyData.Addresses)))
	}

	return makeARRAYOfUnquotedTEXT(rowSubStmts) + "::vout_t[]"
}
