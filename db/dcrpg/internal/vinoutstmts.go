package internal

import (
	"encoding/hex"
	"fmt"

	"github.com/decred/dcrdata/v3/db/dbtypes"
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
		value_in INT8,
		tx_type INT4
	);`

	// insertVinRow is the basis for several vinvs insert/upsert statements.
	insertVinRow = `INSERT INTO vins (tx_hash, tx_index, tx_tree, prev_tx_hash, prev_tx_index, prev_tx_tree,
		value_in, is_valid, is_mainchain, block_time, tx_type) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) `

	// InsertVinRow inserts a new vin row without checking for unique index
	// conflicts. This should only be used before the unique indexes are created
	// or there may be constraint violations (errors).
	InsertVinRow = insertVinRow + `RETURNING id;`

	// UpsertVinRow is an upsert (insert or update on conflict), returning the
	// inserted/updated vin row id.
	UpsertVinRow = insertVinRow + `ON CONFLICT (tx_hash, tx_index, tx_tree) DO UPDATE
		SET is_valid = $8, is_mainchain = $9, block_time = $10,
			prev_tx_hash = $4, prev_tx_index = $5, prev_tx_tree = $6
		RETURNING id;`

	// InsertVinRowOnConflictDoNothing allows an INSERT with a DO NOTHING on
	// conflict with vins' unique tx index, while returning the row id of either
	// the inserted row or the existing row that causes the conflict. The
	// complexity of this statement is necessary to avoid an unnecessary UPSERT,
	// which would have performance consequences. The row is not locked.
	InsertVinRowOnConflictDoNothing = `WITH inserting AS (` +
		insertVinRow +
		`	ON CONFLICT (tx_hash, tx_index, tx_tree) DO NOTHING -- no lock on row
			RETURNING id
		)
		SELECT id FROM inserting
		UNION  ALL
		SELECT id FROM vins
		WHERE  tx_hash = $1 AND tx_index = $2 AND tx_tree = $3 -- only executed if no INSERT
		LIMIT  1;`

	// DeleteVinsDuplicateRows removes rows that would violate the unique index
	// uix_vin. This should be run prior to creating the index.
	DeleteVinsDuplicateRows = `DELETE FROM vins
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY tx_hash, tx_index, tx_tree ORDER BY id) AS rnum
				FROM vins) t
			WHERE t.rnum > 1);`

	IndexVinTableOnVins = `CREATE UNIQUE INDEX uix_vin
		ON vins(tx_hash, tx_index, tx_tree);`
	DeindexVinTableOnVins = `DROP INDEX uix_vin;`

	IndexVinTableOnPrevOuts = `CREATE INDEX uix_vin_prevout
		ON vins(prev_tx_hash, prev_tx_index);`
	DeindexVinTableOnPrevOuts = `DROP INDEX uix_vin_prevout;`

	SelectVinIDsALL = `SELECT id FROM vins;`
	CountVinsRows   = `SELECT reltuples::BIGINT AS estimate FROM pg_class WHERE relname='vins';`

	SetTxTypeOnVinsByVinIDs = `UPDATE vins SET tx_type=$1 WHERE id=$2;`

	SelectSpendingTxsByPrevTx                = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	SelectSpendingTxsByPrevTxWithBlockHeight = `SELECT prev_tx_index, vins.tx_hash, vins.tx_index, block_height
		FROM vins LEFT JOIN transactions ON
			transactions.tx_hash=vins.tx_hash AND
			transactions.is_valid=TRUE AND
			transactions.is_mainchain=TRUE
		WHERE prev_tx_hash=$1 AND vins.is_valid=TRUE AND vins.is_mainchain=TRUE;`
	SelectSpendingTxByPrevOut = `SELECT id, tx_hash, tx_index, tx_tree FROM vins
		WHERE prev_tx_hash=$1 AND prev_tx_index=$2 ORDER BY is_valid DESC, is_mainchain DESC, block_time DESC;`
	SelectFundingTxsByTx        = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	SelectFundingTxByTxIn       = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`
	SelectFundingOutpointByTxIn = `SELECT id, prev_tx_hash, prev_tx_index, prev_tx_tree FROM vins
		WHERE tx_hash=$1 AND tx_index=$2;`

	SelectFundingOutpointByVinID     = `SELECT prev_tx_hash, prev_tx_index, prev_tx_tree FROM vins WHERE id=$1;`
	SelectFundingOutpointIndxByVinID = `SELECT prev_tx_index FROM vins WHERE id=$1;`
	SelectFundingTxByVinID           = `SELECT prev_tx_hash FROM vins WHERE id=$1;`
	SelectSpendingTxByVinID          = `SELECT tx_hash, tx_index, tx_tree FROM vins WHERE id=$1;`
	SelectAllVinInfoByID             = `SELECT tx_hash, tx_index, tx_tree, is_valid, is_mainchain, block_time,
		prev_tx_hash, prev_tx_index, prev_tx_tree, value_in, tx_type FROM vins WHERE id = $1;`
	SelectVinVoutPairByID = `SELECT tx_hash, tx_index, prev_tx_hash, prev_tx_index FROM vins WHERE id = $1;`

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
		WHERE tx_hash = $5 AND tx_index = $6 AND tx_tree = $7;`

	// SelectCoinSupply fetches the coin supply as of the latest block, where
	// sum represents the generated coins for all stakebase and only
	// stake-validated coinbase transactions.
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

	// insertVinRow is the basis for several vout insert/upsert statements.
	insertVoutRow = `INSERT INTO vouts (tx_hash, tx_index, tx_tree, value,
		version, pkscript, script_req_sigs, script_type, script_addresses)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) `

	// InsertVoutRow inserts a new vout row without checking for unique index
	// conflicts. This should only be used before the unique indexes are created
	// or there may be constraint violations (errors).
	InsertVoutRow = insertVoutRow + `RETURNING id;`

	// UpsertVoutRow is an upsert (insert or update on conflict), returning the
	// inserted/updated vout row id.
	UpsertVoutRow = insertVoutRow + `ON CONFLICT (tx_hash, tx_index, tx_tree) DO UPDATE
		SET version = $5 RETURNING id;`

	// InsertVoutRowOnConflictDoNothing allows an INSERT with a DO NOTHING on
	// conflict with vouts' unique tx index, while returning the row id of
	// either the inserted row or the existing row that causes the conflict. The
	// complexity of this statement is necessary to avoid an unnecessary UPSERT,
	// which would have performance consequences. The row is not locked.
	InsertVoutRowOnConflictDoNothing = `WITH inserting AS (` +
		insertVoutRow +
		`	ON CONFLICT (tx_hash, tx_index, tx_tree) DO NOTHING -- no lock on row
			RETURNING id
		)
		SELECT id FROM inserting
		UNION  ALL
		SELECT id FROM vouts
		WHERE  tx_hash = $1 AND tx_index = $2 AND tx_tree = $3 -- only executed if no INSERT
		LIMIT  1;`

	// DeleteVoutDuplicateRows removes rows that would violate the unique index
	// uix_vout_txhash_ind. This should be run prior to creating the index.
	DeleteVoutDuplicateRows = `DELETE FROM vouts
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY tx_hash, tx_index, tx_tree ORDER BY id) AS rnum
				FROM vouts) t
			WHERE t.rnum > 1);`

	// IndexVoutTableOnTxHashIdx creates the unique index uix_vout_txhash_ind on
	// (tx_hash, tx_index, tx_tree).
	IndexVoutTableOnTxHashIdx = `CREATE UNIQUE INDEX uix_vout_txhash_ind
		ON vouts(tx_hash, tx_index, tx_tree);`
	DeindexVoutTableOnTxHashIdx = `DROP INDEX uix_vout_txhash_ind;`

	SelectAddressByTxHash = `SELECT script_addresses, value FROM vouts
		WHERE tx_hash = $1 AND tx_index = $2 AND tx_tree = $3;`

	SelectPkScriptByID       = `SELECT version, pkscript FROM vouts WHERE id=$1;`
	SelectPkScriptByOutpoint = `SELECT version, pkscript FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	SelectVoutIDByOutpoint   = `SELECT id FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	SelectVoutByID           = `SELECT * FROM vouts WHERE id=$1;`

	RetrieveVoutValue  = `SELECT value FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	RetrieveVoutValues = `SELECT value, tx_index, tx_tree FROM vouts WHERE tx_hash=$1;`

	CreateVoutType = `CREATE TYPE vout_t AS (
		value INT8,
		version INT2,
		pkscript BYTEA,
		script_req_sigs INT4,
		script_type TEXT,
		script_addresses TEXT[]
	);`
)

// MakeVinInsertStatement returns the appropriate vins insert statement for the
// desired conflict checking and handling behavior. For checked=false, no ON
// CONFLICT checks will be performed, and the value of updateOnConflict is
// ignored. This should only be used prior to creating the unique indexes as
// these constraints will cause an errors if an inserted row violates a
// constraint. For updateOnConflict=true, an upsert statement will be provided
// that UPDATEs the conflicting row. For updateOnConflict=false, the statement
// will either insert or do nothing, and return the inserted (new) or
// conflicting (unmodified) row id.
func MakeVinInsertStatement(checked, updateOnConflict bool) string {
	if !checked {
		return InsertVinRow
	}
	if updateOnConflict {
		return UpsertVinRow
	}
	return InsertVinRowOnConflictDoNothing
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

// MakeVoutInsertStatement returns the appropriate vouts insert statement for
// the desired conflict checking and handling behavior. For checked=false, no ON
// CONFLICT checks will be performed, and the value of updateOnConflict is
// ignored. This should only be used prior to creating the unique indexes as
// these constraints will cause an errors if an inserted row violates a
// constraint. For updateOnConflict=true, an upsert statement will be provided
// that UPDATEs the conflicting row. For updateOnConflict=false, the statement
// will either insert or do nothing, and return the inserted (new) or
// conflicting (unmodified) row id.
func MakeVoutInsertStatement(checked, updateOnConflict bool) string {
	if !checked {
		return InsertVoutRow
	}
	if updateOnConflict {
		return UpsertVoutRow
	}
	return InsertVoutRowOnConflictDoNothing
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
