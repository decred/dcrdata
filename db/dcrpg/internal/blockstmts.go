// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package internal

// These queries relate primarily to the "blocks" and "block_chain" tables.
const (
	CreateBlockTable = `CREATE TABLE IF NOT EXISTS blocks (
		id SERIAL PRIMARY KEY,
		hash TEXT NOT NULL, -- UNIQUE
		height INT4,
		size INT4,
		is_valid BOOLEAN,
		is_mainchain BOOLEAN,
		version INT4,
		numtx INT4,
		num_rtx INT4,
		tx TEXT[],
		txDbIDs INT8[],
		num_stx INT4,
		stx TEXT[],
		stxDbIDs INT8[],
		time TIMESTAMPTZ,
		nonce INT8,
		vote_bits INT2,
		voters INT2,
		fresh_stake INT2,
		revocations INT2,
		pool_size INT4,
		bits INT4,
		sbits INT8,
		difficulty FLOAT8,
		stake_version INT4,
		previous_hash TEXT,
		chainwork TEXT,
		winners TEXT[]
	);`

	// Block inserts. is_valid refers to blocks that have been validated by
	// stakeholders (voting on the previous block), while is_mainchain
	// distinguishes blocks that are on the main chain from those that are on
	// side chains and/or orphaned.

	// insertBlockRow is the basis for several block insert/upsert statements.
	insertBlockRow = `INSERT INTO blocks (
		hash, height, size, is_valid, is_mainchain, version,
		numtx, num_rtx, tx, txDbIDs, num_stx, stx, stxDbIDs,
		time, nonce, vote_bits, voters,
		fresh_stake, revocations, pool_size, bits, sbits,
		difficulty, stake_version, previous_hash, chainwork, winners)
	VALUES ($1, $2, $3, $4, $5, $6,
		$7, $8, $9, $10, $11, $12, $13,
		$14, $15, $16, $17, $18, $19,
		$20, $21, $22, $23, $24, $25,
		$26, $27) `

	// InsertBlockRow inserts a new block row without checking for unique index
	// conflicts. This should only be used before the unique indexes are created
	// or there may be constraint violations (errors).
	InsertBlockRow = insertBlockRow + `RETURNING id;`

	// UpsertBlockRow is an upsert (insert or update on conflict), returning
	// the inserted/updated block row id.
	UpsertBlockRow = insertBlockRow + `ON CONFLICT (hash) DO UPDATE
		SET is_valid = $4, is_mainchain = $5 RETURNING id;`

	// InsertBlockRowOnConflictDoNothing allows an INSERT with a DO NOTHING on
	// conflict with blocks' unique tx index, while returning the row id of
	// either the inserted row or the existing row that causes the conflict. The
	// complexity of this statement is necessary to avoid an unnecessary UPSERT,
	// which would have performance consequences. The row is not locked.
	InsertBlockRowOnConflictDoNothing = `WITH ins AS (` +
		insertBlockRow +
		`	ON CONFLICT (hash) DO NOTHING -- no lock on row
			RETURNING id
		)
		SELECT id FROM ins
		UNION  ALL
		SELECT id FROM blocks
		WHERE  hash = $1 -- only executed if no INSERT
		LIMIT  1;`

	// IndexBlockTableOnHash creates the unique index uix_block_hash on (hash).
	IndexBlockTableOnHash   = `CREATE UNIQUE INDEX ` + IndexOfBlocksTableOnHash + ` ON blocks(hash);`
	DeindexBlockTableOnHash = `DROP INDEX ` + IndexOfBlocksTableOnHash + ` CASCADE;`

	// IndexBlocksTableOnHeight creates the index uix_block_height on (height).
	// This is not unique because of side chains.
	IndexBlocksTableOnHeight   = `CREATE INDEX ` + IndexOfBlocksTableOnHeight + ` ON blocks(height);`
	DeindexBlocksTableOnHeight = `DROP INDEX ` + IndexOfBlocksTableOnHeight + ` CASCADE;`

	// IndexBlocksTableOnHeight creates the index uix_block_time on (time).
	// This is not unique because of side chains.
	IndexBlocksTableOnTime   = `CREATE INDEX ` + IndexOfBlocksTableOnTime + ` ON blocks("time");`
	DeindexBlocksTableOnTime = `DROP INDEX ` + IndexOfBlocksTableOnTime + ` CASCADE;`

	SelectBlockByTimeRangeSQL = `SELECT hash, height, size, time, numtx
		FROM blocks WHERE time BETWEEN $1 and $2 ORDER BY time DESC LIMIT $3;`
	SelectBlockByTimeRangeSQLNoLimit = `SELECT hash, height, size, time, numtx
		FROM blocks WHERE time BETWEEN $1 and $2 ORDER BY time DESC;`
	SelectBlockHashByHeight = `SELECT hash FROM blocks WHERE height = $1 AND is_mainchain = true;`
	SelectBlockHeightByHash = `SELECT height FROM blocks WHERE hash = $1;`

	SelectBlockTimeByHeight = `SELECT time FROM blocks
		WHERE height = $1 AND is_mainchain = true;`

	RetrieveBestBlockHeightAny = `SELECT id, hash, height FROM blocks
		ORDER BY height DESC LIMIT 1;`
	RetrieveBestBlockHeight = `SELECT id, hash, height FROM blocks
		WHERE is_mainchain = true ORDER BY height DESC LIMIT 1;`

	// SelectBlocksTicketsPrice selects the ticket price and difficulty for the
	// first block in a stake difficulty window.
	SelectBlocksTicketsPrice = `SELECT sbits, time, difficulty, height, fresh_stake
		FROM blocks
		WHERE height > $1
		ORDER BY height;`

	SelectGenesisTime = `SELECT time
		FROM blocks
		WHERE height = 0
		AND is_mainchain`

	SelectWindowsByLimit = `SELECT (height/$1)*$1 AS window_start,
		MAX(difficulty) AS difficulty,
		SUM(num_rtx) AS txs,
		SUM(fresh_stake) AS tickets,
		SUM(voters) AS votes,
		SUM(revocations) AS revocations,
		SUM(size) AS size,
		MAX(sbits) AS sbits,
		MIN(time) AS time,
		COUNT(*) AS blocks_count
		FROM blocks
		WHERE height BETWEEN $2 AND $3
			AND is_mainchain
		GROUP BY window_start
		ORDER BY window_start DESC;`

	SelectBlocksTimeListingByLimit = `SELECT date_trunc($1, time at time zone 'utc') as index_value,
		MAX(height),
		SUM(num_rtx) AS txs,
		SUM(fresh_stake) AS tickets,
		SUM(voters) AS votes,
		SUM(revocations) AS revocations,
		SUM(size) AS size,
		COUNT(*) AS blocks_count,
		MIN(time) AS start_time,
		MAX(time) AS end_time
		FROM blocks
		GROUP BY index_value
		ORDER BY index_value DESC
		LIMIT $2 OFFSET $3;`

	SelectBlocksPreviousHash = `SELECT previous_hash FROM blocks WHERE hash = $1;`

	SelectBlocksHashes = `SELECT hash FROM blocks ORDER BY id;`

	SelectBlockVoteCount = `SELECT voters FROM blocks WHERE hash = $1;`

	SelectSideChainBlocks = `SELECT is_valid, height, previous_hash, hash, block_chain.next_hash
		FROM blocks
		JOIN block_chain ON this_hash=hash
		WHERE is_mainchain = FALSE
		ORDER BY height DESC;`

	SelectSideChainTips = `SELECT is_valid, height, previous_hash, hash
		FROM blocks
		JOIN block_chain ON this_hash=hash
		WHERE is_mainchain = FALSE AND block_chain.next_hash=''
		ORDER BY height DESC;`

	SelectBlockStatus = `SELECT is_valid, is_mainchain, height, previous_hash, hash, block_chain.next_hash
		FROM blocks
		JOIN block_chain ON this_hash=hash
		WHERE hash = $1;`

	SelectBlockStatuses = `SELECT is_valid, is_mainchain, hash
		FROM blocks
		WHERE height = $1;`

	SelectBlockFlags = `SELECT is_valid, is_mainchain
		FROM blocks
		WHERE hash = $1;`

	SelectDisapprovedBlocks = `SELECT is_mainchain, height, previous_hash, hash, block_chain.next_hash
		FROM blocks
		JOIN block_chain ON this_hash=hash
		WHERE is_valid = FALSE
		ORDER BY height DESC;`

	SelectTxsPerDay = `SELECT date_trunc('day',time) AS date, sum(numtx)
		FROM blocks
		WHERE time > $1
		GROUP BY date
		ORDER BY date;`

	// blocks table updates

	UpdateLastBlockValid = `UPDATE blocks SET is_valid = $2 WHERE id = $1;`
	UpdateBlockMainchain = `UPDATE blocks SET is_mainchain = $2 WHERE hash = $1 RETURNING previous_hash;`

	// CreateBlockPrevNextTable creates a new table named block_chain. The
	// primary key is not a SERIAL, but rather the row ID of the block in the
	// blocks table.
	CreateBlockPrevNextTable = `CREATE TABLE IF NOT EXISTS block_chain (
		block_db_id INT8 PRIMARY KEY,
		prev_hash TEXT NOT NULL,
		this_hash TEXT UNIQUE NOT NULL, -- UNIQUE
		next_hash TEXT
	);`

	// InsertBlockPrevNext includes the primary key, which should be the row ID
	// of the corresponding block in the blocks table.
	InsertBlockPrevNext = `INSERT INTO block_chain (block_db_id, prev_hash, this_hash, next_hash)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (this_hash) DO NOTHING;`

	SelectBlockChainRowIDByHash = `SELECT block_db_id FROM block_chain WHERE this_hash = $1;`

	UpdateBlockNext           = `UPDATE block_chain SET next_hash = $2 WHERE block_db_id = $1;`
	UpdateBlockNextByHash     = `UPDATE block_chain SET next_hash = $2 WHERE this_hash = $1;`
	UpdateBlockNextByNextHash = `UPDATE block_chain SET next_hash = $2 WHERE next_hash = $1;`

	SelectBlockStats = `SELECT height, size, time, chainwork, numtx
		FROM blocks
		WHERE is_mainchain
		AND height > $1
		ORDER BY height;`

	// Get the height data. Because stats is unique on height, the inner join will
	// filter for mainchain as well.
	SelectBlockDataByHeight = `
		SELECT blocks.hash, blocks.height, blocks.size,
			blocks.difficulty, blocks.sbits, blocks.time, stats.pool_size,
			stats.pool_val, blocks.winners, blocks.is_valid
		FROM blocks INNER JOIN stats ON blocks.id = stats.blocks_id
		WHERE blocks.height = $1;`

	SelectBlockDataRange = `
		SELECT blocks.hash, blocks.height, blocks.size,
			blocks.difficulty, blocks.sbits, blocks.time, stats.pool_size,
			stats.pool_val, blocks.winners, blocks.is_valid
		FROM blocks INNER JOIN stats ON blocks.id = stats.blocks_id
		WHERE blocks.height BETWEEN $1 AND $2
		ORDER BY blocks.height;`

	SelectBlockDataRangeDesc = `
		SELECT blocks.hash, blocks.height, blocks.size,
			blocks.difficulty, blocks.sbits, blocks.time, stats.pool_size,
			stats.pool_val, blocks.winners, blocks.is_valid
		FROM blocks INNER JOIN stats ON blocks.id = stats.blocks_id
		WHERE blocks.height BETWEEN $1 AND $2
		ORDER BY blocks.height DESC;`

	SelectBlockDataRangeWithSkip = `
		SELECT blocks.hash, blocks.height, blocks.size,
			blocks.difficulty, blocks.sbits, blocks.time, stats.pool_size,
			stats.pool_val, blocks.winners, blocks.is_valid
		FROM blocks INNER JOIN stats ON blocks.id = stats.blocks_id
		WHERE blocks.height BETWEEN $1 AND $2
			AND blocks.height %% %d = %d
		ORDER BY blocks.height;`

	SelectBlockDataRangeWithSkipDesc = `
		SELECT blocks.hash, blocks.height, blocks.size,
			blocks.difficulty, blocks.sbits, blocks.time, stats.pool_size,
			stats.pool_val, blocks.winners, blocks.is_valid
		FROM blocks INNER JOIN stats ON blocks.id = stats.blocks_id
		WHERE blocks.height BETWEEN $1 AND $2
			AND blocks.height %% %d = %d
		ORDER BY blocks.height DESC;`

	SelectBlockDataByHash = `
			SELECT blocks.hash, blocks.height, blocks.size,
				blocks.difficulty, blocks.sbits, blocks.time, stats.pool_size,
				stats.pool_val, blocks.winners, blocks.is_mainchain, blocks.is_valid
			FROM blocks INNER JOIN stats ON blocks.id = stats.blocks_id
			WHERE blocks.hash = $1;`

	SelectBlockDataBest = `
		SELECT blocks.hash, blocks.height, blocks.size,
			blocks.difficulty, blocks.sbits, blocks.time, stats.pool_size,
			stats.pool_val, blocks.winners, blocks.is_valid
		FROM blocks INNER JOIN stats ON blocks.id = stats.blocks_id
		WHERE blocks.is_mainchain
		ORDER BY height DESC LIMIT 1;`

	SelectBlockSizeByHeight = `SELECT size
		FROM blocks
		WHERE is_mainchain AND height = $1;`

	SelectBlockSizeRange = `SELECT size
		FROM blocks
		WHERE is_mainchain
			AND height BETWEEN $1 AND $2;`

	SelectSBitsByHeight = `SELECT sbits
		FROM blocks
		WHERE height = $1 AND is_mainchain;`

	SelectSBitsByHash = `SELECT sbits
		FROM blocks
		WHERE hash = $1;`

	SelectSBitsRange = `SELECT sbits
		FROM blocks
		WHERE height BETWEEN $1 AND $2;`

	SelectDiffByTime = `SELECT difficulty
		FROM blocks
		WHERE time >= $1
		ORDER BY time
		LIMIT 1;`
)

func BlockInsertStatement(checked bool) string {
	if checked {
		return UpsertBlockRow
	}
	return InsertBlockRow
}
