// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package internal

// The stats table holds additional data beyond basic block data. row are unique
// on height, so this table does not retain information related to orphaned
// blocks.
const (
	CreateStatsTable = `CREATE TABLE IF NOT EXISTS stats (
    blocks_id INT4 REFERENCES blocks(id) ON DELETE CASCADE,
    height INT4,
    pool_size INT8,
    pool_val INT8
  );`

	IndexStatsOnHeight   = `CREATE UNIQUE INDEX ` + IndexOfHeightOnStatsTable + ` ON stats(height);`
	DeindexStatsOnHeight = `DROP INDEX ` + IndexOfHeightOnStatsTable + ` CASCADE;`

	UpsertStats = `INSERT INTO stats (blocks_id, height, pool_size, pool_val)
    VALUES ($1, $2, $3, $4)
    ON CONFLICT (height)
    DO UPDATE SET
    blocks_id = $1,
    height = $2,
    pool_size = $3,
    pool_val = $4
  ;`
)
