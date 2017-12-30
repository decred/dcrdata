package internal

const (
	// Tickets

	CreateTicketsTable = `CREATE TABLE IF NOT EXISTS tickets (  
		id SERIAL PRIMARY KEY,
		tx_hash TEXT NOT NULL,
		block_hash TEXT NOT NULL,
		block_height INT4,
		purchase_tx_db_id INT8,
		stakesubmission_address TEXT,
		is_multisig BOOLEAN,
		is_split BOOLEAN,
		num_inputs INT2,
		price FLOAT8,
		fee FLOAT8,
		spend_type INT2,
		spend_height INT4,
		spend_tx_db_id INT8
	);`

	// Insert
	insertTicketRow0 = `INSERT INTO tickets (
		tx_hash, block_hash, block_height, purchase_tx_db_id,
		stakesubmission_address, is_multisig, is_split,
		num_inputs, price, fee, spend_type)
	VALUES (
		$1, $2, $3,	$4,
		$5, $6, $7,
		$8, $9, $10, $11) `
	insertTicketRow = insertTicketRow0 + `RETURNING id;`
	// insertTicketRowChecked = insertTicketRow0 + `ON CONFLICT (tx_hash, block_hash) DO NOTHING RETURNING id;`
	upsertTicketRow = insertTicketRow0 + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
		SET tx_hash = $1, block_hash = $2 RETURNING id;`
	insertTicketRowReturnId = `WITH ins AS (` +
		insertTicketRow0 +
		`ON CONFLICT (tx_hash, block_hash) DO UPDATE
		SET tx_hash = NULL WHERE FALSE
		RETURNING id
		)
	SELECT id FROM ins
	UNION  ALL
	SELECT id FROM tickets
	WHERE  tx_hash = $1 AND block_hash = $2
	LIMIT  1;`

	SelectTicketsInBlock         = `SELECT * FROM tickets WHERE block_hash = $1;`
	SelectTicketsTxDbIDsInBlock  = `SELECT purchase_tx_db_id FROM tickets WHERE block_hash = $1;`
	SelectTicketsForAddress      = `SELECT * FROM tickets WHERE stakesubmission_address = $1;`
	SelectTicketsForPriceAtLeast = `SELECT * FROM tickets WHERE price >= $1;`
	SelectTicketsForPriceAtMost  = `SELECT * FROM tickets WHERE price <= $1;`
	SelectTicketIDHeightByHash   = `SELECT id, block_height FROM tickets WHERE tx_hash = $1;`
	SelectTicketIDByHash         = `SELECT id FROM tickets WHERE tx_hash = $1;`

	// Update
	SetTicketSpendingInfoForHash = `UPDATE tickets
		SET spend_type = $5, spend_height = $3, spend_tx_db_id = $4
		WHERE tx_hash = $1 and block_hash = $2;`
	SetTicketSpendingInfoForTicketDbID = `UPDATE tickets
		SET spend_type = $4, spend_height = $2, spend_tx_db_id = $3
		WHERE id = $1;`
	SetTicketSpendingInfoForTxDbID = `UPDATE tickets
		SET spend_type = $4, spend_height = $2, spend_tx_db_id = $3
		WHERE purchase_tx_db_id = $1;`

	// Index
	IndexTicketsTableOnHashes = `CREATE UNIQUE INDEX uix_ticket_hashes_index
		ON tickets(tx_hash, block_hash);`
	DeindexTicketsTableOnHashes = `DROP INDEX uix_ticket_hashes_index;`

	IndexTicketsTableOnTxDbID = `CREATE UNIQUE INDEX uix_ticket_ticket_db_id
		ON tickets(purchase_tx_db_id);`
	DeindexTicketsTableOnTxDbID = `DROP INDEX uix_ticket_ticket_db_id;`

	DeleteTicketsDuplicateRows = `DELETE FROM tickets
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY tx_hash, block_hash ORDER BY id) AS rnum
				FROM tickets) t
			WHERE t.rnum > 1);`

	// Votes

	CreateVotesTable = `CREATE TABLE IF NOT EXISTS votes (
		id SERIAL PRIMARY KEY,
		height INT4,
		tx_hash TEXT NOT NULL,
		block_hash TEXT NOT NULL,
		candidate_block_hash TEXT NOT NULL,
		version INT2,
		vote_bits INT2,
		block_valid BOOLEAN,
		ticket_hash TEXT,
		ticket_price FLOAT8,
		vote_reward FLOAT8
	);`

	// Insert
	insertVoteRow0 = `INSERT INTO votes (
		height, tx_hash,
		block_hash, candidate_block_hash,
		version, vote_bits, block_valid,
		ticket_hash, ticket_price, vote_reward)
	VALUES (
		$1, $2,
		$3, $4,
		$5, $6, $7,
		$8, $9, $10) `
	insertVoteRow = insertVoteRow0 + `RETURNING id;`
	// insertVoteRowChecked = insertVoteRow0 + `ON CONFLICT (tx_hash, block_hash) DO NOTHING RETURNING id;`
	upsertVoteRow = insertVoteRow0 + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
		SET tx_hash = $3, block_hash = $4 RETURNING id;`
	insertVoteRowReturnId = `WITH ins AS (` +
		insertVoteRow0 +
		`ON CONFLICT (tx_hash, block_hash) DO UPDATE
		SET tx_hash = NULL WHERE FALSE
		RETURNING id
		)
	SELECT id FROM ins
	UNION  ALL
	SELECT id FROM votes
	WHERE  tx_hash = $3 AND block_hash = $4
	LIMIT  1;`

	SelectAllVoteDbIDsHeightsTicketHashes = `SELECT id, height, ticket_hash FROM votes;`

	// Index
	IndexVotesTableOnHashes = `CREATE UNIQUE INDEX uix_votes_hashes_index
		ON votes(tx_hash, block_hash);`
	DeindexVotesTableOnHashes = `DROP INDEX uix_votes_hashes_index;`

	IndexVotesTableOnCandidate = `CREATE INDEX uix_votes_candidate_block
		ON votes(candidate_block_hash);`
	DeindexVotesTableOnCandidate = `DROP INDEX uix_votes_candidate_block;`

	IndexVotesTableOnVoteVersion = `CREATE INDEX uix_votes_vote_version
		ON votes(version);`
	DeindexVotesTableOnVoteVersion = `DROP INDEX uix_votes_vote_version;`

	DeleteVotesDuplicateRows = `DELETE FROM votes
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY tx_hash, block_hash ORDER BY id) AS rnum
				FROM votes) t
			WHERE t.rnum > 1);`

	// Misses

	CreateMissesTable = `CREATE TABLE IF NOT EXISTS misses (
		id SERIAL PRIMARY KEY,
		height INT4,
		block_hash TEXT NOT NULL,
		candidate_block_hash TEXT NOT NULL,
		ticket_hash TEXT NOT NULL
	);`

	// Insert
	insertMissRow0 = `INSERT INTO misses (
		height, block_hash, candidate_block_hash, ticket_hash)
	VALUES (
		$1, $2, $3, $4) `
	insertMissRow = insertMissRow0 + `RETURNING id;`
	// insertVoteRowChecked = insertMissRow0 + `ON CONFLICT (ticket_hash, block_hash) DO NOTHING RETURNING id;`
	upsertMissRow = insertMissRow0 + `ON CONFLICT (ticket_hash, block_hash) DO UPDATE 
		SET ticket_hash = $4, block_hash = $2 RETURNING id;`
	insertMissRowReturnId = `WITH ins AS (` +
		insertMissRow0 +
		`ON CONFLICT (ticket_hash, block_hash) DO UPDATE
		SET ticket_hash = NULL WHERE FALSE
		RETURNING id
		)
	SELECT id FROM ins
	UNION  ALL
	SELECT id FROM misses
	WHERE  ticket_hash = $4 AND block_hash = $2
	LIMIT  1;`

	// Index
	IndexMissesTableOnHashes = `CREATE UNIQUE INDEX uix_misses_hashes_index
		ON misses(ticket_hash, block_hash);`
	DeindexMissesTableOnHashes = `DROP INDEX uix_misses_hashes_index;`

	DeleteMissesDuplicateRows = `DELETE FROM misses
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY ticket_hash, block_hash ORDER BY id) AS rnum
				FROM misses) t
			WHERE t.rnum > 1);`

	// Revokes?
)

func MakeTicketInsertStatement(checked bool) string {
	if checked {
		return upsertTicketRow
	}
	return insertTicketRow
}

func MakeVoteInsertStatement(checked bool) string {
	if checked {
		return upsertVoteRow
	}
	return insertVoteRow
}

func MakeMissInsertStatement(checked bool) string {
	if checked {
		return upsertMissRow
	}
	return insertMissRow
}
