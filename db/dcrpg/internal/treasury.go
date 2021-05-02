// Copyright (c) 2021, The Decred developers
// See LICENSE for details.

package internal

// These queries relate primarily to the "treasury" table.
const (
	CreateTreasuryTable = `CREATE TABLE IF NOT EXISTS treasury (
		tx_hash TEXT,
		tx_type INT4
		value INT8,
		block_hash TEXT,
		block_height INT8,
		block_time TIMESTAMPTZ NOT NULL,
		is_mainchain BOOLEAN
	);`

	IndexTreasuryOnTxHash   = `CREATE UNIQUE INDEX ` + IndexOfTreasuryTableOnTxHash + ` ON treasury(tx_hash, block_hash);`
	DeindexTreasuryOnTxHash = `DROP INDEX ` + IndexOfTreasuryTableOnTxHash + ` CASCADE;`

	IndexTreasuryOnBlockHeight   = `CREATE INDEX ` + IndexOfTreasuryTableOnHeight + ` ON treasury(block_height DESC);`
	DeindexTreasuryOnBlockHeight = `DROP INDEX ` + IndexOfTreasuryTableOnHeight + ` CASCADE;`

	UpdateTreasuryMainchainByBlock = `UPDATE treasury
		SET is_mainchain=$1
		WHERE block_hash=$2;`

	// InsertTreasuryRow inserts a new treasury row without checking for unique
	// index conflicts. This should only be used before the unique indexes are
	// created or there may be constraint violations (errors).
	InsertTreasuryRow = `INSERT INTO treasury (
		tx_hash, tx_type, value, block_hash, block_height, block_time, is_mainchain)
	VALUES ($1, $2, $3,	$4, $5, $6, $7) `

	// UpsertTreasuryRow is an upsert (insert or update on conflict), returning
	// the inserted/updated treasury row id. is_mainchain is updated as this
	// might be a reorganization.
	UpsertTreasuryRow = InsertTreasuryRow + `ON CONFLICT (tx_hash, block_hash)
		DO UPDATE SET is_mainchain = $7;`

	// InsertTreasuryRowOnConflictDoNothing allows an INSERT with a DO NOTHING
	// on conflict with a treasury tnx's unique tx index.
	InsertTreasuryRowOnConflictDoNothing = InsertTreasuryRow + `ON CONFLICT (tx_hash, block_hash)
		DO NOTHING;`

	// TODO: CreateTreasuryVotesTable
)

// MakeTreasuryInsertStatement returns the appropriate treasury insert statement
// for the desired conflict checking and handling behavior. For checked=false,
// no ON CONFLICT checks will be performed, and the value of updateOnConflict is
// ignored. This should only be used prior to creating a unique index as these
// constraints will cause an errors if an inserted row violates a constraint.
// For updateOnConflict=true, an upsert statement will be provided that UPDATEs
// the conflicting row. For updateOnConflict=false, the statement will either
// insert or do nothing, and return the inserted (new) or conflicting
// (unmodified) row id.
func MakeTreasuryInsertStatement(checked, updateOnConflict bool) string {
	if !checked {
		return InsertTreasuryRow
	}
	if updateOnConflict {
		return UpsertTreasuryRow
	}
	return InsertTreasuryRowOnConflictDoNothing
}
