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
		num_inputs INT2,
		spend_height INT4,
		spend_tx_db_id INT8
	);`

	// Insert
	insertTicketRow0 = `INSERT INTO tickets (
		tx_hash, block_hash, block_height, purchase_tx_db_id,
		stakesubmission_address, is_multisig, num_inputs)
	VALUES (
		$1, $2, $3,	$4,
		$5, $6, $7) `
	insertTicketRow        = insertTicketRow0 + `RETURNING id;`
	insertTicketRowChecked = insertTicketRow0 + `ON CONFLICT (tx_hash, block_hash) DO NOTHING RETURNING id;`
	upsertTicketRow        = insertTicketRow0 + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
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

	// Update
	SetTicketSpendingInfoForHash = `UPDATE tickets
		SET spend_height = $3, spend_tx_db_id = $4
		WHERE tx_hash = $1 and block_hash = $2;`
	SetTicketSpendingInfoForTxDbID = `UPDATE tickets
		SET spend_height = $2, spend_tx_db_id = $3
		WHERE purchase_tx_db_id = $1;`

	// Index
	IndexTicketsTableOnHashes = `CREATE UNIQUE INDEX uix_ticket_hashes_index
		ON tickets(tx_hash, block_hash);`
	DeindexTicketsTableOnHashes = `DROP INDEX uix_ticket_hashes_index;`

	IndexTicketsTableOnTxDbID = `CREATE UNIQUE INDEX uix_ticket_ticket_db_id
		ON tickets(purchase_tx_db_id);`
	DeindexTicketsTableOnTxDbID = `DROP INDEX uix_ticket_ticket_db_id;`

	// Votes

	CreateVotesTable = `CREATE TABLE IF NOT EXISTS votes (
		id SERIAL PRIMARY KEY,
		height INT4,
		missed BOOLEAN,
		tx_hash TEXT NOT NULL,
		block_hash TEXT NOT NULL,
		candidate_block_hash TEXT NOT NULL,
		version INT2,
		vote_bits INT2,
		block_valid BOOLEAN
	);`

	// Insert
	insertVoteRow0 = `INSERT INTO votes (
		height, missed, tx_hash,
		block_hash, candidate_block_hash,
		version, vote_bits, block_valid)
	VALUES (
		$1, $2, $3,
		$4, $5,
		$6, $7, $8) `
	insertVoteRow        = insertVoteRow0 + `RETURNING id;`
	insertVoteRowChecked = insertVoteRow0 + `ON CONFLICT (tx_hash, block_hash) DO NOTHING RETURNING id;`
	upsertVoteRow        = insertVoteRow0 + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
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
)

func MakeTicketInsertStatement(checked bool) string {
	if checked {
		return insertTicketRowChecked
	}
	return insertTicketRow
}

func MakeVoteInsertStatement(checked bool) string {
	if checked {
		return insertVoteRowChecked
	}
	return insertVoteRow
}
