// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package internal

// These queries relate primarily to the "meta" table.
const (
	CreateMetaTable = `CREATE TABLE IF NOT EXISTS meta (
		net_name TEXT,
		currency_net INT8 PRIMARY KEY,
		best_block_height INT8,
		best_block_hash BYTEA,
		compatibility_version INT4,
		schema_version INT4,
		maintenance_version INT4,
		ibd_complete BOOLEAN
	);`

	InitMetaRow = `INSERT INTO meta (
		net_name, currency_net, best_block_height, -- best_block_hash,
		compatibility_version, schema_version, maintenance_version,
		ibd_complete)
	VALUES ($1, $2, $3, $4, $5, $6, $7);`

	SelectMetaDBVersions = `SELECT
		compatibility_version,
		schema_version,
		maintenance_version
	FROM meta;`

	SelectMetaDBBestBlock = `SELECT
		best_block_height,
		best_block_hash
	FROM meta;`

	SetMetaDBBestBlock = `UPDATE meta
		SET best_block_height = $1, best_block_hash = $2;`

	SelectMetaDBIbdComplete = `SELECT ibd_complete FROM meta;`

	SetMetaDBIbdComplete = `UPDATE meta
		SET ibd_complete = $1;`

	SetDBSchemaVersion = `UPDATE meta
		SET schema_version = $1;`

	SetDBMaintenanceVersion = `UPDATE meta
		SET maintenance_version = $1;`
)
