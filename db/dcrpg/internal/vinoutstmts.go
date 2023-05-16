// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package internal

// These queries relate primarily to the "vins" and "vouts" tables.
const (
	// vins
	// TODO: add sequence num?

	CreateVinTable = `CREATE TABLE IF NOT EXISTS vins (
		id SERIAL8 PRIMARY KEY,
		tx_hash TEXT,
		tx_index INT4,
		tx_tree INT2,
		is_valid BOOLEAN,
		is_mainchain BOOLEAN,
		block_time TIMESTAMPTZ,
		prev_tx_hash TEXT,
		prev_tx_index INT8,
		prev_tx_tree INT2,
		value_in INT8,
		tx_type INT4
	);`

	// insertVinRow is the basis for several vins insert/upsert statements.
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
				SELECT id,
					row_number() OVER (PARTITION BY tx_hash, tx_index, tx_tree ORDER BY id DESC) AS rnum
				FROM vins) t
			WHERE t.rnum > 1);`

	ShowCreateVinsTable     = `WITH a AS (SHOW CREATE vins) SELECT create_statement FROM a;`
	DistinctVinsToTempTable = `INSERT INTO vins_temp
		SELECT DISTINCT ON (tx_hash, tx_index) *
		FROM vins;`
	RenameVinsTemp = `ALTER TABLE vins_temp RENAME TO vins;`

	SelectVinDupIDs = `WITH dups AS (
		SELECT array_agg(id) AS ids
		FROM vins
		GROUP BY tx_hash, tx_index 
		HAVING count(id)>1
	)
	SELECT array_agg(dupids) FROM (
		SELECT unnest(ids) AS dupids
		FROM dups
		ORDER BY dupids DESC
	) AS _;`

	DeleteVinRows = `DELETE FROM vins
		WHERE id = ANY($1);`

	IndexVinTableOnVins = `CREATE UNIQUE INDEX ` + IndexOfVinsTableOnVin +
		` ON vins(tx_hash, tx_index, tx_tree);`
	DeindexVinTableOnVins = `DROP INDEX ` + IndexOfVinsTableOnVin + ` CASCADE;`

	IndexVinTableOnPrevOuts = `CREATE INDEX ` + IndexOfVinsTableOnPrevOut +
		` ON vins(prev_tx_hash, prev_tx_index);`
	DeindexVinTableOnPrevOuts = `DROP INDEX ` + IndexOfVinsTableOnPrevOut + ` CASCADE;`

	SelectVinIDsALL = `SELECT id FROM vins;`
	CountVinsRows   = `SELECT reltuples::BIGINT AS estimate FROM pg_class WHERE relname='vins';`

	SelectSpendingTxsByPrevTx                = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	SelectSpendingTxsByPrevTxWithBlockHeight = `SELECT prev_tx_index, vins.tx_hash, vins.tx_index, block_height
		FROM vins LEFT JOIN transactions ON
			transactions.tx_hash=vins.tx_hash AND
			transactions.is_valid AND
			transactions.is_mainchain
		WHERE prev_tx_hash=$1 AND vins.is_valid AND vins.is_mainchain;`
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

	SelectUTXOsViaVinsMatch = `SELECT vouts.id, vouts.tx_hash, vouts.tx_index,   -- row ID and outpoint
			vouts.script_addresses, vouts.value, vouts.mixed         -- value, addresses, and mixed flag of output
		FROM vouts
		LEFT OUTER JOIN vins                   -- LEFT JOIN to identify when there is no matching input (spend)
		ON vouts.tx_hash=vins.prev_tx_hash
			AND vouts.tx_index=vins.prev_tx_index
		JOIN transactions ON transactions.tx_hash=vouts.tx_hash
		WHERE vins.prev_tx_hash IS NULL                   -- unspent, condition applied after join, which will put NULL when no vin matches the vout
			AND array_length(script_addresses, 1)>0
			AND transactions.is_mainchain AND transactions.is_valid;`

	SelectUTXOs = `SELECT vouts.id, vouts.tx_hash, vouts.tx_index, vouts.script_addresses, vouts.value, vouts.mixed
		FROM vouts
		JOIN transactions ON transactions.tx_hash=vouts.tx_hash
		WHERE vouts.spend_tx_row_id IS NULL AND vouts.value>0
			AND transactions.is_mainchain AND transactions.is_valid;`

	SetIsValidIsMainchainByTxHash = `UPDATE vins SET is_valid = $1, is_mainchain = $2
		WHERE tx_hash = $3 AND block_time = $4;`
	SetIsValidIsMainchainByVinID = `UPDATE vins SET is_valid = $2, is_mainchain = $3
		WHERE id = $1;`
	SetIsValidByTxHash = `UPDATE vins SET is_valid = $1
		WHERE tx_hash = $2 AND block_time = $3;`
	SetIsValidByVinID = `UPDATE vins SET is_valid = $2
		WHERE id = $1;`
	SetIsMainchainByTxHash = `UPDATE vins SET is_mainchain = $1
		WHERE tx_hash = $2 AND block_time = $3;`
	SetIsMainchainByVinID = `UPDATE vins SET is_mainchain = $2
		WHERE id = $1;`

	// SetVinsTableCoinSupplyUpgrade does not set is_mainchain because that upgrade comes after this one
	SetVinsTableCoinSupplyUpgrade = `UPDATE vins SET is_valid = $1, block_time = $3, value_in = $4
		WHERE tx_hash = $5 AND tx_index = $6 AND tx_tree = $7;`

	// SelectCoinSupply fetches the newly minted atoms per block by filtering
	// for stakebase, treasurybase, and stake-validated coinbase transactions.
	SelectCoinSupply = `SELECT vins.block_time, sum(vins.value_in)
		FROM vins JOIN transactions
		ON vins.tx_hash = transactions.tx_hash
		WHERE vins.prev_tx_hash = '0000000000000000000000000000000000000000000000000000000000000000'
		AND transactions.block_height > $1
		AND vins.is_mainchain AND (vins.is_valid OR vins.tx_tree != 0)
		AND vins.tx_type = ANY(ARRAY[0,2,6])   --- coinbase(regular),ssgen,treasurybase, but NOT tspend, same as =ANY('{0,2,6}') or IN(0,2,6)
		GROUP BY vins.block_time, transactions.block_height
		ORDER BY transactions.block_height;`

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
		script_addresses TEXT[],
		mixed BOOLEAN DEFAULT FALSE,
		spend_tx_row_id INT8
	);`

	// insertVinRow is the basis for several vout insert/upsert statements.
	insertVoutRow = `INSERT INTO vouts (tx_hash, tx_index, tx_tree, value,
		version, pkscript, script_req_sigs, script_type, script_addresses, mixed)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ` // not with spend_tx_row_id

	// InsertVoutRow inserts a new vout row without checking for unique index
	// conflicts. This should only be used before the unique indexes are created
	// or there may be constraint violations (errors).
	InsertVoutRow = insertVoutRow + `RETURNING id;`

	// UpsertVoutRow is an upsert (insert or update on conflict), returning the
	// inserted/updated vout row id.
	UpsertVoutRow = insertVoutRow + `ON CONFLICT (tx_hash, tx_index, tx_tree) DO UPDATE
		SET version = $5 RETURNING id;`

	UpdateVoutSpendTxRowID  = `UPDATE vouts SET spend_tx_row_id = $1 WHERE id = $2;`
	UpdateVoutsSpendTxRowID = `UPDATE vouts SET spend_tx_row_id = $1 WHERE id = ANY($2);`

	// ResetVoutSpendTxRowIDs resets spend_tx_row_id for vouts given transaction
	// row ids. e.g. For rolled-back/purged transactions that no longer spend
	// the targeted vouts (previous outputs).
	ResetVoutSpendTxRowIDs = `UPDATE vouts SET spend_tx_row_id = NULL
		WHERE spend_tx_row_id = ANY($1);`

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
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
					row_number() OVER (PARTITION BY tx_hash, tx_index, tx_tree ORDER BY id DESC) AS rnum
				FROM vouts
			) t
			WHERE t.rnum > 1
		);`

	ShowCreateVoutsTable     = `WITH a AS (SHOW CREATE vouts) SELECT create_statement FROM a;`
	DistinctVoutsToTempTable = `INSERT INTO vouts_temp
		SELECT DISTINCT ON (tx_hash, tx_index) *
		FROM vouts;`
	RenameVoutsTemp = `ALTER TABLE vouts_temp RENAME TO vouts;`

	SelectVoutDupIDs = `WITH dups AS (
		SELECT array_agg(id) AS ids
		FROM vouts
		GROUP BY tx_hash, tx_index 
		HAVING count(id)>1
	)
	SELECT array_agg(dupids) FROM (
		SELECT unnest(ids) AS dupids
		FROM dups
		ORDER BY dupids DESC
	) AS _;`

	DeleteVoutRows = `DELETE FROM vins
		WHERE id = ANY($1);`

	// IndexVoutTableOnTxHashIdx creates the unique index uix_vout_txhash_ind on
	// (tx_hash, tx_index, tx_tree).
	IndexVoutTableOnTxHashIdx = `CREATE UNIQUE INDEX IF NOT EXISTS ` + IndexOfVoutsTableOnTxHashInd +
		` ON vouts(tx_hash, tx_index, tx_tree) INCLUDE (value);`
	DeindexVoutTableOnTxHashIdx = `DROP INDEX IF EXISTS ` + IndexOfVoutsTableOnTxHashInd + ` CASCADE;`

	IndexVoutTableOnSpendTxID = `CREATE INDEX IF NOT EXISTS ` + IndexOfVoutsTableOnSpendTxID +
		` ON vouts(spend_tx_row_id);`
	DeindexVoutTableOnSpendTxID = `DROP INDEX IF EXISTS ` + IndexOfVoutsTableOnSpendTxID + ` CASCADE;`

	SelectVoutAddressesByTxOut = `SELECT id, script_addresses, value, mixed FROM vouts
		WHERE tx_hash = $1 AND tx_index = $2 AND tx_tree = $3;`

	SelectPkScriptByID       = `SELECT version, pkscript FROM vouts WHERE id=$1;`
	SelectPkScriptByOutpoint = `SELECT version, pkscript FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	SelectPkScriptByVinID    = `SELECT version, pkscript FROM vouts
		JOIN vins ON vouts.tx_hash=vins.prev_tx_hash and vouts.tx_index=vins.prev_tx_index
		WHERE vins.id=$1;`

	SelectVoutIDByOutpoint = `SELECT id FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	SelectVoutByID         = `SELECT * FROM vouts WHERE id=$1;`

	RetrieveVoutValue  = `SELECT value FROM vouts WHERE tx_hash=$1 and tx_index=$2;`
	RetrieveVoutValues = `SELECT value, tx_index, tx_tree FROM vouts WHERE tx_hash=$1;`
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
