// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package internal

// The stats table holds additional data beyond basic block data. row are unique
// on height, so this table does not retain information related to orphaned
// blocks.
const (
	CreateStatsTable = `
		CREATE TABLE IF NOT EXISTS stats (
		blocks_id INT4 REFERENCES blocks(id) ON DELETE CASCADE,
		height INT4 UNIQUE,
		pool_size INT8,
		pool_val INT8
	);`

	IndexStatsOnHeight   = `CREATE UNIQUE INDEX ` + IndexOfHeightOnStatsTable + ` ON stats(height);` // DO NOT USE
	DeindexStatsOnHeight = `DROP INDEX IF EXISTS ` + IndexOfHeightOnStatsTable + ` CASCADE;`

	UpsertStats = `
		INSERT INTO stats (blocks_id, height, pool_size, pool_val)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (height)
		DO UPDATE SET
		blocks_id = $1,
		height = $2,
		pool_size = $3,
		pool_val = $4
	;`

	// SelectPoolInfo selects information about the ticket pool when a block with
	// given hash was mined. The inner join serves to select only mainchain
	// blocks.
	SelectPoolInfo = `
		SELECT blocks.winners, stats.pool_val, stats.pool_size
		FROM stats INNER JOIN blocks ON stats.blocks_id = blocks.id
		WHERE blocks.hash = $1
	;`

	SelectPoolStatsAboveHeight = `
		SELECT pool_size, pool_val
		FROM stats
		WHERE height > $1
		ORDER BY height
	;`

	SelectPoolInfoByHeight = `
		SELECT blocks.hash, stats.pool_size, stats.pool_val, blocks.winners
		FROM stats JOIN blocks ON stats.blocks_id = blocks.id
		WHERE stats.height = $1
			AND is_mainchain
	;`
	SelectPoolInfoByHash = `
		SELECT stats.height, stats.pool_size, stats.pool_val,	blocks.winners
		FROM stats JOIN blocks ON stats.blocks_id = blocks.id
		WHERE blocks.hash = $1
	;`
	SelectPoolInfoRange = `
		SELECT stats.height, blocks.hash, stats.pool_size,
			stats.pool_val, blocks.winners
		FROM stats JOIN blocks ON stats.blocks_id = blocks.id
		WHERE stats.height BETWEEN $1 AND $2
			AND is_mainchain
	;`

	SelectPoolValSizeRange = `
		SELECT poolsize, poolval
		FROM stats
		WHERE height BETWEEN $1 AND $2
			AND is_mainchain
	;`
)
