package internal

import (
	"encoding/hex"
	"fmt"

	"github.com/dcrdata/dcrdata/db/dbtypes"
)

const (
	insertVoutRow0 = `INSERT INTO vouts (tx_hash, tx_index, tx_tree, value, 
		version, pkscript, script_req_sigs, script_type, script_addresses)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, %s) `
	insertVoutRow        = insertVoutRow0 + `RETURNING id;`
	insertVoutRowChecked = insertVoutRow
	//insertVoutRowChecked  = insertVoutRow0 + `ON CONFLICT (tx_hash, tx_index, tx_tree) DO NOTHING RETURNING id;`
	insertVoutRowReturnId = `WITH ins AS (` +
		insertVoutRow0 +
		`ON CONFLICT (tx_hash, tx_index) DO UPDATE
		SET tx_hash = NULL WHERE FALSE
		RETURNING id
		)
	 SELECT id FROM ins
	 UNION  ALL
	 SELECT id FROM vouts
	 WHERE  tx_hash = $1 AND tx_index = $2
	 LIMIT  1;`

	CreateVinType = `CREATE TYPE vin_t AS (
		prev_tx_hash TEXT,
		prev_tx_index INTEGER,
		prev_tx_tree SMALLINT,
		htlc_seq_VAL INTEGER,
		value_in DOUBLE PRECISION,
		script_hex BYTEA
	);`

	CreateVinTable = `CREATE TABLE IF NOT EXISTS vins (
		id SERIAL8 PRIMARY KEY,
		tx_hash TEXT,
		tx_index INT4,
		prev_tx_hash TEXT,
		prev_tx_index INT8
	);`

	InsertVinRow0 = `INSERT INTO vins (tx_hash, tx_index, prev_tx_hash, prev_tx_index)
		VALUES ($1, $2, $3, $4) `
	InsertVinRow        = InsertVinRow0 + `RETURNING id;`
	InsertVinRowChecked = InsertVinRow0 +
		`ON CONFLICT (tx_hash, tx_index) DO NOTHING RETURNING id;`

	IndexVinTableOnVins = `CREATE INDEX uix_vin
		ON vins(tx_hash, tx_index)
		;` // STORING (prev_tx_hash, prev_tx_index)
	IndexVinTableOnPrevOuts = `CREATE INDEX uix_vin_prevout
		ON vins(prev_tx_hash, prev_tx_index)
		;` // STORING (tx_hash, tx_index)
	DeindexVinTableOnVins     = `DROP INDEX uix_vin;`
	DeindexVinTableOnPrevOuts = `DROP INDEX uix_vin_prevout;`

	CreateVoutType = `CREATE TYPE vout_t AS (
		value INT8,
		version INT2,
		pkscript BYTEA,
		script_req_sigs INT4,
		script_type TEXT,
		script_addresses TEXT[]
	);`

	SelectSpendingTxsByPrevTx = `SELECT id, tx_hash FROM vins WHERE prev_tx_hash=$1;`
	SelectSpendingTxByPrevOut = `SELECT id, tx_hash FROM vins WHERE prev_tx_hash=$1 AND prev_tx_index=$2;`
	SelectFundingTxsByTx      = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	SelectFundingTxByTxIn     = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`

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

	SelectVoutByID = `SELECT * FROM vouts WHERE id=$1;`

	// TODO: Why can't this be unique?
	IndexVoutTableOnTxHashIdx = `CREATE INDEX uix_vout_txhash_ind
		ON vouts(tx_hash, tx_index);`
	DeindexVoutTableOnTxHashIdx = `DROP INDEX uix_vout_txhash_ind;`

	IndexVoutTableOnTxHash = `CREATE INDEX uix_vout_txhash
		ON vouts(tx_hash);`
	DeindexVoutTableOnTxHash = `DROP INDEX uix_vout_txhash;`
)

func makeVoutInsertStatement(scriptAddresses []string, checked bool) string {
	addrs := makeARRAYOfTEXT(scriptAddresses)
	var insert string
	if checked {
		insert = insertVoutRowChecked
	} else {
		insert = insertVoutRow
	}
	return fmt.Sprintf(insert, addrs)
}

func MakeVoutInsertStatement(vout *dbtypes.Vout, checked bool) string {
	return makeVoutInsertStatement(vout.ScriptPubKeyData.Addresses, checked)
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
