package internal

import (
	"fmt"

	"github.com/dcrdata/dcrdata/db/dbtypes"
)

const (
	// Block insert
	insertBlockRow0 = `INSERT INTO blocks (
		hash, height, size, is_valid, version, merkle_root, stake_root,
		numtx, num_rtx, tx, txDbIDs, num_stx, stx, stxDbIDs,
		time, nonce, vote_bits, final_state, voters,
		fresh_stake, revocations, pool_size, bits, sbits, 
		difficulty, extra_data, stake_version, previous_hash)
	VALUES ($1, $2, $3, $4, $5, $6, $7,
		$8, $9, %s, %s, $10, %s, %s,
		$11, $12, $13, $14, $15, 
		$16, $17, $18, $19, $20,
		$21, $22, $23, $24) `
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

	CreateBlockTable = `CREATE TABLE IF NOT EXISTS blocks (  
		id SERIAL PRIMARY KEY,
		hash TEXT NOT NULL, -- UNIQUE
		height INT4,
		size INT4,
		is_valid BOOLEAN,
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
	RetrieveBestBlockHeight = `SELECT id, hash, height FROM blocks ORDER BY height DESC LIMIT 1;`

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

	UpdateBlockNext = `UPDATE block_chain set next_hash = $2 WHERE block_db_id = $1;`
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
