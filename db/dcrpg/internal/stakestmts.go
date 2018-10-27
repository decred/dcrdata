package internal

// The folloiwng statements are for the tickets, votes, and misses tables.

const (
	// tickets table

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
		pool_status INT2,
		is_mainchain BOOLEAN,
		spend_height INT4,
		spend_tx_db_id INT8
	);`

	// insertTicketRow is the basis for several ticket insert/upsert statements.
	insertTicketRow = `INSERT INTO tickets (
		tx_hash, block_hash, block_height, purchase_tx_db_id,
		stakesubmission_address, is_multisig, is_split,
		num_inputs, price, fee, spend_type, pool_status,
		is_mainchain)
	VALUES (
		$1, $2, $3,	$4,
		$5, $6, $7,
		$8, $9, $10, $11, $12, 
		$13) `

	// InsertTicketRow inserts a new ticket row without checking for unique
	// index conflicts. This should only be used before the unique indexes are
	// created or there may be constraint violations (errors).
	InsertTicketRow = insertTicketRow + `RETURNING id;`

	// UpsertTicketRow is an upsert (insert or update on conflict), returning
	// the inserted/updated ticket row id. is_mainchain is updated as this might
	// be a reorganization.
	UpsertTicketRow = insertTicketRow + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
		SET is_mainchain = $13 RETURNING id;`

	// InsertTicketRowOnConflictDoNothing allows an INSERT with a DO NOTHING on
	// conflict with tickets' unique tx index, while returning the row id of
	// either the inserted row or the existing row that causes the conflict. The
	// complexity of this statement is necessary to avoid an unnecessary UPSERT,
	// which would have performance consequences. The row is not locked.
	InsertTicketRowOnConflictDoNothing = `WITH ins AS (` +
		insertTicketRow +
		`	ON CONFLICT (tx_hash, block_hash) DO NOTHING -- no lock on row
			RETURNING id
		)
		SELECT id FROM ins
		UNION  ALL
		SELECT id FROM tickets
		WHERE  tx_hash = $1 AND block_hash = $2 -- only executed if no INSERT
		LIMIT  1;`

	// DeleteTicketsDuplicateRows removes rows that would violate the unique
	// index uix_ticket_hashes_index. This should be run prior to creating the
	// index.
	DeleteTicketsDuplicateRows = `DELETE FROM tickets
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY tx_hash, block_hash ORDER BY id) AS rnum
				FROM tickets) t
			WHERE t.rnum > 1);`

	// Indexes

	// IndexTicketsTableOnHashes creates the unique index
	// uix_ticket_hashes_index on (tx_hash, block_hash).
	IndexTicketsTableOnHashes = `CREATE UNIQUE INDEX uix_ticket_hashes_index
		ON tickets(tx_hash, block_hash);`
	DeindexTicketsTableOnHashes = `DROP INDEX uix_ticket_hashes_index;`

	// IndexTicketsTableOnTxDbID creates the unique index that ensures only one
	// row in the tickets table may refer to a certain row of the transactions
	// table. This is not the same as being unique on transaction hash, since
	// the transactions table also has a unique constraint is on (tx_hash,
	// block_hash) that allows a transaction appearing in multiple blocks (e.g.
	// side chains and/or invalidated blocks) to have multiple rows in the
	// transactions table.
	IndexTicketsTableOnTxDbID = `CREATE UNIQUE INDEX uix_ticket_ticket_db_id
		ON tickets(purchase_tx_db_id);`
	DeindexTicketsTableOnTxDbID = `DROP INDEX uix_ticket_ticket_db_id;`

	IndexTicketsTableOnPoolStatus = `CREATE INDEX uix_tickets_pool_status ON 
		tickets(pool_status);`
	DeindexTicketsTableOnPoolStatus = `DROP INDEX uix_tickets_pool_status;`

	SelectTicketsInBlock        = `SELECT * FROM tickets WHERE block_hash = $1;`
	SelectTicketsTxDbIDsInBlock = `SELECT purchase_tx_db_id FROM tickets WHERE block_hash = $1;`
	SelectTicketsForAddress     = `SELECT * FROM tickets WHERE stakesubmission_address = $1;`

	forTxHashMainchainFirst    = ` WHERE tx_hash = $1 ORDER BY is_mainchain DESC;`
	SelectTicketIDHeightByHash = `SELECT id, block_height FROM tickets` + forTxHashMainchainFirst
	SelectTicketIDByHash       = `SELECT id FROM tickets` + forTxHashMainchainFirst
	SelectTicketStatusByHash   = `SELECT id, spend_type, pool_status FROM tickets` + forTxHashMainchainFirst

	SelectUnspentTickets = `SELECT id, tx_hash FROM tickets
		WHERE spend_type = 0 AND is_mainchain = true;`

	SelectTicketsForPriceAtLeast = `SELECT * FROM tickets WHERE price >= $1;`
	SelectTicketsForPriceAtMost  = `SELECT * FROM tickets WHERE price <= $1;`

	SelectTicketsByPrice = `SELECT price,
		SUM(CASE WHEN tickets.block_height >= $1 THEN 1 ELSE 0 END) as immature,
		SUM(CASE WHEN tickets.block_height < $1 THEN 1 ELSE 0 END) as live
		FROM tickets JOIN transactions ON purchase_tx_db_id=transactions.id
		WHERE pool_status = 0 AND tickets.is_mainchain = TRUE
		GROUP BY price ORDER BY price;`

	SelectTicketsByPurchaseDate = `SELECT (transactions.block_time/$1)*$1 as timestamp,
		SUM(price) as price,
		SUM(CASE WHEN tickets.block_height >= $2 THEN 1 ELSE 0 END) as immature,
		SUM(CASE WHEN tickets.block_height < $2 THEN 1 ELSE 0 END) as live
		FROM tickets JOIN transactions ON purchase_tx_db_id=transactions.id
		WHERE pool_status = 0 AND tickets.is_mainchain = TRUE
		GROUP BY timestamp ORDER BY timestamp;`

	SelectTicketSpendTypeByBlock = `SELECT block_height, 
		SUM(CASE WHEN spend_type = 0 THEN 1 ELSE 0 END) as unspent,
		SUM(CASE WHEN spend_type = 1 THEN 1 ELSE 0 END) as revoked
		FROM tickets GROUP BY block_height ORDER BY block_height;`

	// Updates

	SetTicketSpendingInfoForHash = `UPDATE tickets
		SET spend_type = $5, spend_height = $3, spend_tx_db_id = $4, pool_status = $6
		WHERE tx_hash = $1 and block_hash = $2;`
	SetTicketSpendingInfoForTicketDbID = `UPDATE tickets
		SET spend_type = $4, spend_height = $2, spend_tx_db_id = $3, pool_status = $5
		WHERE id = $1;`
	SetTicketSpendingInfoForTxDbID = `UPDATE tickets
		SET spend_type = $4, spend_height = $2, spend_tx_db_id = $3, pool_status = $5
		WHERE purchase_tx_db_id = $1;`
	SetTicketPoolStatusForTicketDbID = `UPDATE tickets SET pool_status = $2 WHERE id = $1;`
	SetTicketPoolStatusForHash       = `UPDATE tickets SET pool_status = $2 WHERE tx_hash = $1;`

	UpdateTicketsMainchainAll = `UPDATE tickets
		SET is_mainchain=b.is_mainchain
		FROM (
			SELECT hash, is_mainchain
			FROM blocks
		) b
		WHERE block_hash = b.hash;`

	UpdateTicketsMainchainByBlock = `UPDATE tickets
		SET is_mainchain=$1 
		WHERE block_hash=$2;`

	// votes table

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
		ticket_tx_db_id INT8,
		ticket_price FLOAT8,
		vote_reward FLOAT8,
		is_mainchain BOOLEAN
	);`

	// insertVoteRow is the basis for several vote insert/upsert statements.
	insertVoteRow = `INSERT INTO votes (
		height, tx_hash,
		block_hash, candidate_block_hash,
		version, vote_bits, block_valid,
		ticket_hash, ticket_tx_db_id, ticket_price, vote_reward,
		is_mainchain)
	VALUES (
		$1, $2,
		$3, $4,
		$5, $6, $7,
		$8, $9, $10, $11,
		$12) `

	// InsertVoteRow inserts a new vote row without checking for unique index
	// conflicts. This should only be used before the unique indexes are created
	// or there may be constraint violations (errors).
	InsertVoteRow = insertVoteRow + `RETURNING id;`

	// UpsertVoteRow is an upsert (insert or update on conflict), returning the
	// inserted/updated vote row id. is_mainchain is updated as this might be a
	// reorganization.
	UpsertVoteRow = insertVoteRow + `ON CONFLICT (tx_hash, block_hash) DO UPDATE 
		SET is_mainchain = $12 RETURNING id;`

	// InsertVoteRowOnConflictDoNothing allows an INSERT with a DO NOTHING on
	// conflict with votes' unique tx index, while returning the row id of
	// either the inserted row or the existing row that causes the conflict. The
	// complexity of this statement is necessary to avoid an unnecessary UPSERT,
	// which would have performance consequences. The row is not locked.
	InsertVoteRowOnConflictDoNothing = `WITH ins AS (` +
		insertVoteRow +
		`	ON CONFLICT (tx_hash, block_hash) DO NOTHING -- no lock on row
			RETURNING id
		)
		SELECT id FROM ins
		UNION  ALL
		SELECT id FROM votes
		WHERE  tx_hash = $2 AND block_hash = $3 -- only executed if no INSERT
		LIMIT  1;`

	// DeleteVotesDuplicateRows removes rows that would violate the unique index
	// uix_votes_hashes_index. This should be run prior to creating the index.
	DeleteVotesDuplicateRows = `DELETE FROM votes
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY tx_hash, block_hash ORDER BY id) AS rnum
				FROM votes) t
			WHERE t.rnum > 1);`

	// Indexes

	// IndexVotesTableOnHashes creates the unique index uix_votes_hashes_index
	// on (tx_hash, block_hash).
	IndexVotesTableOnHashes = `CREATE UNIQUE INDEX uix_votes_hashes_index
		ON votes(tx_hash, block_hash);`
	DeindexVotesTableOnHashes = `DROP INDEX uix_votes_hashes_index;`

	IndexVotesTableOnBlockHash = `CREATE INDEX uix_votes_block_hash
		ON votes(block_hash);`
	DeindexVotesTableOnBlockHash = `DROP INDEX uix_votes_block_hash;`

	IndexVotesTableOnCandidate = `CREATE INDEX uix_votes_candidate_block
		ON votes(candidate_block_hash);`
	DeindexVotesTableOnCandidate = `DROP INDEX uix_votes_candidate_block;`

	IndexVotesTableOnVoteVersion = `CREATE INDEX uix_votes_vote_version
		ON votes(version);`
	DeindexVotesTableOnVoteVersion = `DROP INDEX uix_votes_vote_version;`

	SelectAllVoteDbIDsHeightsTicketHashes = `SELECT id, height, ticket_hash FROM votes;`
	SelectAllVoteDbIDsHeightsTicketDbIDs  = `SELECT id, height, ticket_tx_db_id FROM votes;`

	UpdateVotesMainchainAll = `UPDATE votes
		SET is_mainchain=b.is_mainchain
		FROM (
			SELECT hash, is_mainchain
			FROM blocks
		) b
		WHERE block_hash = b.hash;`

	UpdateVotesMainchainByBlock = `UPDATE votes
		SET is_mainchain=$1 
		WHERE block_hash=$2;`

	// misses table

	CreateMissesTable = `CREATE TABLE IF NOT EXISTS misses (
		id SERIAL PRIMARY KEY,
		height INT4,
		block_hash TEXT NOT NULL,
		candidate_block_hash TEXT NOT NULL,
		ticket_hash TEXT NOT NULL
	);`

	// insertMissRow is the basis for several miss insert/upsert statements.
	insertMissRow = `INSERT INTO misses (
		height, block_hash, candidate_block_hash, ticket_hash)
	VALUES (
		$1, $2, $3, $4) `

	// InsertMissRow inserts a new misss row without checking for unique index
	// conflicts. This should only be used before the unique indexes are created
	// or there may be constraint violations (errors).
	InsertMissRow = insertMissRow + `RETURNING id;`

	// UpsertMissRow is an upsert (insert or update on conflict), returning
	// the inserted/updated miss row id.
	UpsertMissRow = insertMissRow + `ON CONFLICT (ticket_hash, block_hash) DO UPDATE 
		SET ticket_hash = $4, block_hash = $2 RETURNING id;`

	// InsertMissRowOnConflictDoNothing allows an INSERT with a DO NOTHING on
	// conflict with misses' unique tx index, while returning the row id of
	// either the inserted row or the existing row that causes the conflict. The
	// complexity of this statement is necessary to avoid an unnecessary UPSERT,
	// which would have performance consequences. The row is not locked.
	InsertMissRowOnConflictDoNothing = `WITH ins AS (` +
		insertMissRow +
		`	ON CONFLICT (ticket_hash, block_hash) DO NOTHING -- no lock on row
			RETURNING id
		)
		SELECT id FROM ins
		UNION  ALL
		SELECT id FROM misses
		WHERE  block_hash = $2 AND ticket_hash = $4 -- only executed if no INSERT
		LIMIT  1;`

	// DeleteMissesDuplicateRows removes rows that would violate the unique
	// index uix_misses_hashes_index. This should be run prior to creating the
	// index.
	DeleteMissesDuplicateRows = `DELETE FROM misses
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY ticket_hash, block_hash ORDER BY id) AS rnum
				FROM misses) t
			WHERE t.rnum > 1);`

	// IndexMissesTableOnHashes creates the unique index uix_misses_hashes_index
	// on (ticket_hash, block_hash).
	IndexMissesTableOnHashes = `CREATE UNIQUE INDEX uix_misses_hashes_index
		ON misses(ticket_hash, block_hash);`
	DeindexMissesTableOnHashes = `DROP INDEX uix_misses_hashes_index;`

	SelectMissesInBlock = `SELECT ticket_hash FROM misses WHERE block_hash = $1;`

	// agendas table

	CreateAgendasTable = `CREATE TABLE IF NOT EXISTS agendas (
		id SERIAL PRIMARY KEY,
		agenda_id TEXT,
		agenda_vote_choice INT2,
		tx_hash TEXT NOT NULL,
		block_height INT4,
		block_time INT8,
		locked_in BOOLEAN,
		activated BOOLEAN,
		hard_forked BOOLEAN
	);`

	// Insert
	insertAgendaRow = `INSERT INTO agendas (
		agenda_id, agenda_vote_choice,
		tx_hash, block_height, block_time,
		locked_in, activated, hard_forked)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) `

	InsertAgendaRow = insertAgendaRow + `RETURNING id;`

	UpsertAgendaRow = insertAgendaRow + `ON CONFLICT (agenda_id, agenda_vote_choice, tx_hash, block_height) DO UPDATE 
		SET block_time = $5 RETURNING id;`

	// IndexAgendasTableOnAgendaID creates the unique index
	// uix_agendas_agenda_id on (agenda_id, agenda_vote_choice, tx_hash,
	// block_height).
	IndexAgendasTableOnAgendaID = `CREATE UNIQUE INDEX uix_agendas_agenda_id
		ON agendas(agenda_id, agenda_vote_choice, tx_hash, block_height);`
	DeindexAgendasTableOnAgendaID = `DROP INDEX uix_agendas_agenda_id;`

	IndexAgendasTableOnBlockTime = `CREATE INDEX uix_agendas_block_time
		ON agendas(block_time);`
	DeindexAgendasTableOnBlockTime = `DROP INDEX uix_agendas_block_time;`

	agendaLockinBlock              = `SELECT block_height FROM agendas WHERE locked_in = true AND agenda_id = $4 LIMIT 1`
	SelectAgendasAgendaVotesByTime = `SELECT block_time AS timestamp,
			count(CASE WHEN agenda_vote_choice = $1 THEN 1 ELSE NULL END) AS yes,
			count(CASE WHEN agenda_vote_choice = $2 THEN 1 ELSE NULL END) AS abstain,
			count(CASE WHEN agenda_vote_choice = $3 THEN 1 ELSE NULL END) AS no,
			count(*) AS total
		 FROM agendas
		WHERE agenda_id = $4
		  AND block_height <= (` + agendaLockinBlock + `)
		GROUP BY timestamp ORDER BY timestamp;`

	SelectAgendasAgendaVotesByHeight = `SELECT block_height,
			count(CASE WHEN agenda_vote_choice = $1 THEN 1 ELSE NULL END) AS yes,
			count(CASE WHEN agenda_vote_choice = $2 THEN 1 ELSE NULL END) AS abstain,
			count(CASE WHEN agenda_vote_choice = $3 THEN 1 ELSE NULL END) AS no,
			count(*) AS total
		 FROM agendas
		WHERE agenda_id = $4
		  AND block_height <= (` + agendaLockinBlock + `)
		GROUP BY block_height;`

	SelectAgendasLockedIn   = `SELECT block_height FROM agendas WHERE locked_in = true AND agenda_id = $1 LIMIT 1;`
	SelectAgendasHardForked = `SELECT block_height FROM agendas WHERE hard_forked = true AND agenda_id = $1 LIMIT 1;`
	SelectAgendasActivated  = `SELECT block_height FROM agendas WHERE activated = true AND agenda_id = $1 LIMIT 1;`
)

// MakeTicketInsertStatement returns the appropriate tickets insert statement
// for the desired conflict checking and handling behavior. For checked=false,
// no ON CONFLICT checks will be performed, and the value of updateOnConflict is
// ignored. This should only be used prior to creating the unique indexes as
// these constraints will cause an errors if an inserted row violates a
// constraint. For updateOnConflict=true, an upsert statement will be provided
// that UPDATEs the conflicting row. For updateOnConflict=false, the statement
// will either insert or do nothing, and return the inserted (new) or
// conflicting (unmodified) row id.
func MakeTicketInsertStatement(checked, updateOnConflict bool) string {
	if !checked {
		return InsertTicketRow
	}
	if updateOnConflict {
		return UpsertTicketRow
	}
	return InsertTicketRowOnConflictDoNothing
}

// MakeTicketInsertStatement returns the appropriate votes insert statement for
// the desired conflict checking and handling behavior. See the description of
// MakeTicketInsertStatement for details.
func MakeVoteInsertStatement(checked, updateOnConflict bool) string {
	if !checked {
		return InsertVoteRow
	}
	if updateOnConflict {
		return UpsertVoteRow
	}
	return InsertVoteRowOnConflictDoNothing
}

// MakeTicketInsertStatement returns the appropriate misses insert statement for
// the desired conflict checking and handling behavior. See the description of
// MakeTicketInsertStatement for details.
func MakeMissInsertStatement(checked, updateOnConflict bool) string {
	if !checked {
		return InsertMissRow
	}
	if updateOnConflict {
		return UpsertMissRow
	}
	return InsertMissRowOnConflictDoNothing
}

func MakeAgendaInsertStatement(checked bool) string {
	if checked {
		return UpsertAgendaRow
	}
	return InsertAgendaRow
}
