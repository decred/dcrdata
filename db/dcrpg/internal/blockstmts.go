package internal

import (
	"fmt"

	"github.com/decred/dcrdata/db/dbtypes"
)

const (
	// Block insert. is_valid refers to blocks that have been validated by
	// stakeholders (voting on the previous block), while is_mainchain
	// distinguishes blocks that are on the main chain from those that are
	// on side chains and/or orphaned.

	insertBlockRow0 = `INSERT INTO blocks (
		hash, height, size, is_valid, is_mainchain, version, merkle_root, stake_root,
		numtx, num_rtx, tx, txDbIDs, num_stx, stx, stxDbIDs,
		time, nonce, vote_bits, final_state, voters,
		fresh_stake, revocations, pool_size, bits, sbits, 
		difficulty, extra_data, stake_version, previous_hash)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		$9, $10, %s, %s, $11, %s, %s,
		$12, $13, $14, $15, $16, 
		$17, $18, $19, $20, $21,
		$22, $23, $24, $25) `
	insertBlockRow = insertBlockRow0 + `RETURNING id;`
	// insertBlockRowChecked  = insertBlockRow0 + `ON CONFLICT (hash) DO NOTHING RETURNING id;`
	upsertBlockRow = insertBlockRow0 + `ON CONFLICT (hash) DO UPDATE 
		SET hash = $1 RETURNING id;`
	insertBlockRowReturnId = `WITH ins AS (` +
		insertBlockRow0 +
		`ON CONFLICT (hash) DO UPDATE
		SET hash = NULL WHERE FALSE
		RETURNING id
		)
	SELECT id FROM ins
	UNION  ALL
	SELECT id FROM blocks
	WHERE  hash = $1
	LIMIT  1;`

	UpdateLastBlockValid = `UPDATE blocks SET is_valid = $2 WHERE id = $1;`

	SelectBlockByTimeRangeSQL = `SELECT hash, height, size, time, numtx
		FROM blocks WHERE time BETWEEN $1 and $2 ORDER BY time DESC LIMIT $3;`
	SelectBlockByTimeRangeSQLNoLimit = `SELECT hash, height, size, time, numtx
		FROM blocks WHERE time BETWEEN $1 and $2 ORDER BY time DESC;`
	SelectBlockHashByHeight = `SELECT hash FROM blocks WHERE height = $1 AND is_mainchain = true;`
	SelectBlockHeightByHash = `SELECT height FROM blocks WHERE hash = $1;`

	CreateBlockTable = `CREATE TABLE IF NOT EXISTS blocks (  
		id SERIAL PRIMARY KEY,
		hash TEXT NOT NULL, -- UNIQUE
		height INT4,
		size INT4,
		is_valid BOOLEAN,
		is_mainchain BOOLEAN,
		version INT4,
		merkle_root TEXT,
		stake_root TEXT,
		numtx INT4,
		num_rtx INT4,
		tx TEXT[],
		txDbIDs INT8[],
		num_stx INT4,
		stx TEXT[],
		stxDbIDs INT8[],
		time INT8,
		nonce INT8,
		vote_bits INT2,
		final_state BYTEA,
		voters INT2,
		fresh_stake INT2,
		revocations INT2,
		pool_size INT4,
		bits INT4,
		sbits INT8,
		difficulty FLOAT8,
		extra_data BYTEA,
		stake_version INT4,
		previous_hash TEXT
	);`

	IndexBlockTableOnHash = `CREATE UNIQUE INDEX uix_block_hash
		ON blocks(hash);`
	DeindexBlockTableOnHash = `DROP INDEX uix_block_hash;`

	RetrieveBestBlock       = `SELECT * FROM blocks ORDER BY height DESC LIMIT 0, 1;`
	RetrieveBestBlockHeight = `SELECT id, hash, height FROM blocks WHERE is_mainchain = true ORDER BY height DESC LIMIT 1;`

	// SelectBlocksTicketsPrice selects the ticket price and difficulty for the first block in a stake difficulty window.
	SelectBlocksTicketsPrice = `SELECT sbits, time, difficulty FROM blocks WHERE height % $1 = 0 ORDER BY time;`

	SelectBlocksBlockSize = `SELECT time, size, numtx, height FROM blocks ORDER BY time;`

	SelectBlocksPreviousHash = `SELECT previous_hash FROM blocks WHERE hash = $1;`

	SelectBlocksHashes = `SELECT hash FROM blocks ORDER BY id;`

	SelectBlockVoteCount = `SELECT voters FROM blocks WHERE hash = $1;`

	UpdateBlockMainchain = `UPDATE blocks SET is_mainchain = $2 WHERE hash = $1 RETURNING previous_hash;`

	IndexBlocksTableOnHeight = `CREATE INDEX uix_block_height ON blocks(height);`

	DeindexBlocksTableOnHeight = `DROP INDEX uix_block_height;`

	// block_chain, with primary key that is not a SERIAL
	CreateBlockPrevNextTable = `CREATE TABLE IF NOT EXISTS block_chain (
		block_db_id INT8 PRIMARY KEY,
		prev_hash TEXT NOT NULL,
		this_hash TEXT UNIQUE NOT NULL, -- UNIQUE
		next_hash TEXT
	);`

	// Insert includes the primary key, which should be from the blocks table
	InsertBlockPrevNext = `INSERT INTO block_chain (
		block_db_id, prev_hash, this_hash, next_hash)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (this_hash) DO NOTHING;`

	SelectBlockChainRowIDByHash = `SELECT block_db_id FROM block_chain WHERE this_hash = $1;`

	UpdateBlockNext       = `UPDATE block_chain SET next_hash = $2 WHERE block_db_id = $1;`
	UpdateBlockNextByHash = `UPDATE block_chain SET next_hash = $2 WHERE this_hash = $1;`
)

func MakeBlockInsertStatement(block *dbtypes.Block, checked bool) string {
	return makeBlockInsertStatement(block.TxDbIDs, block.STxDbIDs,
		block.Tx, block.STx, checked)
}

func makeBlockInsertStatement(txDbIDs, stxDbIDs []uint64, rtxs, stxs []string, checked bool) string {
	rtxDbIDsARRAY := makeARRAYOfBIGINTs(txDbIDs)
	stxDbIDsARRAY := makeARRAYOfBIGINTs(stxDbIDs)
	rtxTEXTARRAY := makeARRAYOfTEXT(rtxs)
	stxTEXTARRAY := makeARRAYOfTEXT(stxs)
	var insert string
	if checked {
		insert = upsertBlockRow
	} else {
		insert = insertBlockRow
	}
	return fmt.Sprintf(insert, rtxTEXTARRAY, rtxDbIDsARRAY,
		stxTEXTARRAY, stxDbIDsARRAY)
}
