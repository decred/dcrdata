package internal

import (
	"encoding/hex"
	"fmt"

	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/lib/pq"
)

const (
	// vins

	CreateVinTable = `CREATE TABLE IF NOT EXISTS vins (
		id SERIAL8 PRIMARY KEY,
		tx_hash TEXT,
		tx_index INT4,
		tx_tree INT2,
		prev_tx_hash TEXT,
		prev_tx_index INT8,
		prev_tx_tree INT2
	);`

	InsertVinRow0 = `INSERT INTO vins (tx_hash, tx_index, tx_tree, prev_tx_hash, prev_tx_index, prev_tx_tree)
		VALUES ($1, $2, $3, $4, $5, $6) `
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
	CountRow        = `SELECT reltuples::BIGINT AS estimate FROM pg_class WHERE relname='vins';`

	SelectSpendingTxsByPrevTx = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	SelectSpendingTxByPrevOut = `SELECT id, tx_hash, tx_index FROM vins 
		WHERE prev_tx_hash=$1 AND prev_tx_index=$2;`
	SelectFundingTxsByTx        = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	SelectFundingTxByTxIn       = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`
	SelectFundingOutpointByTxIn = `SELECT id, prev_tx_hash, prev_tx_index, prev_tx_tree FROM vins 
		WHERE tx_hash=$1 AND tx_index=$2;`
	SelectFundingOutpointByVinID = `SELECT prev_tx_hash, prev_tx_index, prev_tx_tree FROM vins WHERE id=$1;`
	SelectSpendingTxByVinID      = `SELECT tx_hash, tx_index, tx_tree FROM vins WHERE id=$1;`
	SelectAllVinInfoByID         = `SELECT * FROM vins WHERE id=$1;`

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

	SelectPkScriptByID     = `SELECT pkscript FROM vouts WHERE id=$1;`
	SelectVoutIDByOutpoint = `SELECT id FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	SelectVoutByID         = `SELECT * FROM vouts WHERE id=$1;`

	RetrieveVoutValue  = `SELECT value FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	RetrieveVoutValues = `SELECT value, tx_index, tx_tree FROM vouts WHERE tx_hash=$1;`

	IndexVoutTableOnTxHashIdx = `CREATE UNIQUE INDEX uix_vout_txhash_ind
		ON vouts(tx_hash, tx_index, tx_tree);`
	DeindexVoutTableOnTxHashIdx = `DROP INDEX uix_vout_txhash_ind;`

	IndexVoutTableOnTxHash = `CREATE INDEX uix_vout_txhash
		ON vouts(tx_hash);`
	DeindexVoutTableOnTxHash = `DROP INDEX uix_vout_txhash;`

	CreateVoutType = `CREATE TYPE vout_t AS (
		value INT8,
		version INT2,
		pkscript BYTEA,
		script_req_sigs INT4,
		script_type TEXT,
		script_addresses TEXT[]
	);`
)

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
