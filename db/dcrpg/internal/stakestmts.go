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
	IndexTicketsTableOnHashes = `CREATE UNIQUE INDEX ` + IndexOfTicketsTableOnHashes +
		` ON tickets(tx_hash, block_hash);`
	DeindexTicketsTableOnHashes = `DROP INDEX ` + IndexOfTicketsTableOnHashes + `;`

	// IndexTicketsTableOnTxDbID creates the unique index that ensures only one
	// row in the tickets table may refer to a certain row of the transactions
	// table. This is not the same as being unique on transaction hash, since
	// the transactions table also has a unique constraint is on (tx_hash,
	// block_hash) that allows a transaction appearing in multiple blocks (e.g.
	// side chains and/or invalidated blocks) to have multiple rows in the
	// transactions table.
	IndexTicketsTableOnTxDbID = `CREATE UNIQUE INDEX ` + IndexOfTicketsTableOnTxRowID +
		` ON tickets(purchase_tx_db_id);`
	DeindexTicketsTableOnTxDbID = `DROP INDEX ` + IndexOfTicketsTableOnTxRowID + `;`

	IndexTicketsTableOnPoolStatus = `CREATE INDEX ` + IndexOfTicketsTableOnPoolStatus +
		` ON tickets(pool_status);`
	DeindexTicketsTableOnPoolStatus = `DROP INDEX ` + IndexOfTicketsTableOnPoolStatus + `;`

	SelectTicketsInBlock        = `SELECT * FROM tickets WHERE block_hash = $1;`
	SelectTicketsTxDbIDsInBlock = `SELECT purchase_tx_db_id FROM tickets WHERE block_hash = $1;`
	SelectTicketsForAddress     = `SELECT * FROM tickets WHERE stakesubmission_address = $1;`

	forTxHashMainchainFirst    = ` WHERE tx_hash = $1 ORDER BY is_mainchain DESC;`
	SelectTicketIDHeightByHash = `SELECT id, block_height FROM tickets` + forTxHashMainchainFirst
	SelectTicketIDByHash       = `SELECT id FROM tickets` + forTxHashMainchainFirst
	SelectTicketStatusByHash   = `SELECT id, spend_type, pool_status FROM tickets` + forTxHashMainchainFirst
	SelectTicketInfoByHash     = `SELECT block_hash, block_height, spend_type, pool_status, spend_tx_db_id FROM tickets` + forTxHashMainchainFirst

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

	selectTicketsByPurchaseDate = `SELECT %s as timestamp,
		SUM(price) as price,
		SUM(CASE WHEN tickets.block_height >= $1 THEN 1 ELSE 0 END) as immature,
		SUM(CASE WHEN tickets.block_height < $1 THEN 1 ELSE 0 END) as live
		FROM tickets JOIN transactions ON purchase_tx_db_id=transactions.id
		WHERE pool_status = 0 AND tickets.is_mainchain = TRUE
		GROUP BY timestamp ORDER BY timestamp;`

	SelectTicketSpendTypeByBlock = `SELECT block_height,
		SUM(CASE WHEN spend_type = 0 THEN 1 ELSE 0 END) as unspent,
		SUM(CASE WHEN spend_type = 1 THEN 1 ELSE 0 END) as revoked
		FROM tickets
		WHERE block_height > $1
		GROUP BY block_height
		ORDER BY block_height;`

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

	// CreateVotesTable creates a new table named votes. block_time field is
	// needed to plot "Cumulative Vote Choices" agendas chart that plots
	// cumulative votes count against time over the voting period.
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
		is_mainchain BOOLEAN,
		block_time TIMESTAMPTZ
	);`

	// insertVoteRow is the basis for several vote insert/upsert statements.
	insertVoteRow = `INSERT INTO votes (
		height, tx_hash,
		block_hash, candidate_block_hash,
		version, vote_bits, block_valid,
		ticket_hash, ticket_tx_db_id, ticket_price, vote_reward,
		is_mainchain, block_time)
	VALUES (
		$1, $2,
		$3, $4,
		$5, $6, $7,
		$8, $9, $10, $11,
		$12, $13) `

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
	IndexVotesTableOnHashes = `CREATE UNIQUE INDEX ` + IndexOfVotesTableOnHashes +
		` ON votes(tx_hash, block_hash);`
	DeindexVotesTableOnHashes = `DROP INDEX ` + IndexOfVotesTableOnHashes + `;`

	IndexVotesTableOnBlockHash = `CREATE INDEX ` + IndexOfVotesTableOnBlockHash +
		` ON votes(block_hash);`
	DeindexVotesTableOnBlockHash = `DROP INDEX ` + IndexOfVotesTableOnBlockHash + `;`

	IndexVotesTableOnCandidate = `CREATE INDEX ` + IndexOfVotesTableOnCandBlock +
		` ON votes(candidate_block_hash);`
	DeindexVotesTableOnCandidate = `DROP INDEX ` + IndexOfVotesTableOnCandBlock + `;`

	IndexVotesTableOnVoteVersion = `CREATE INDEX ` + IndexOfVotesTableOnVersion +
		` ON votes(version);`
	DeindexVotesTableOnVoteVersion = `DROP INDEX ` + IndexOfVotesTableOnVersion + `;`

	IndexVotesTableOnHeight = `CREATE INDEX ` + IndexOfVotesTableOnHeight + ` ON votes(height);`

	DeindexVotesTableOnHeight = `DROP INDEX ` + IndexOfVotesTableOnHeight + `;`

	IndexVotesTableOnBlockTime = `CREATE INDEX ` + IndexOfVotesTableOnBlockTime +
		` ON votes(block_time);`
	DeindexVotesTableOnBlockTime = `DROP INDEX ` + IndexOfVotesTableOnBlockTime + `;`

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
	IndexMissesTableOnHashes = `CREATE UNIQUE INDEX ` + IndexOfMissesTableOnHashes +
		` ON misses(ticket_hash, block_hash);`
	DeindexMissesTableOnHashes = `DROP INDEX ` + IndexOfMissesTableOnHashes + `;`

	SelectMissesInBlock = `SELECT ticket_hash FROM misses WHERE block_hash = $1;`

	SelectMissesForTicket = `SELECT height, block_hash FROM misses WHERE ticket_hash = $1;`

	SelectMissesMainchainForTicket = `SELECT misses.height, misses.block_hash
		FROM misses
		JOIN blocks ON misses.block_hash=blocks.hash
		WHERE ticket_hash = $1
			AND blocks.is_mainchain = TRUE;`

	// agendas table

	CreateAgendasTable = `CREATE TABLE IF NOT EXISTS agendas (
		id SERIAL PRIMARY KEY,
		name TEXT,
		status INT2,
		locked_in INT4,
		activated INT4,
		hard_forked INT4
	);`

	// Insert
	insertAgendaRow = `INSERT INTO agendas (name, status, locked_in, activated,
		hard_forked) VALUES ($1, $2, $3, $4, $5) `

	InsertAgendaRow = insertAgendaRow + `RETURNING id;`

	UpsertAgendaRow = insertAgendaRow + `ON CONFLICT (name) DO UPDATE
		SET status = $2, locked_in = $3, activated = $4, hard_forked = $5 RETURNING id;`

	IndexAgendasTableOnAgendaID = `CREATE UNIQUE INDEX ` + IndexOfAgendasTableOnName +
		` ON agendas(name);`
	DeindexAgendasTableOnAgendaID = `DROP INDEX ` + IndexOfAgendasTableOnName + `;`

	SelectAllAgendas = `SELECT id, name, status, locked_in, activated, hard_forked
		FROM agendas;`

	SelectAgendasLockedIn = `SELECT locked_in FROM agendas WHERE name = $1;`

	SelectAgendasHardForked = `SELECT hard_forked FROM agendas WHERE name = $1;`

	SelectAgendasActivated = `SELECT activated FROM agendas WHERE name = $1;`

	SetVoteMileStoneheights = `UPDATE agendas SET status = $2, locked_in = $3,
		activated = $4, hard_forked = $5 WHERE id = $1;`

	// DeleteAgendasDuplicateRows removes rows that would violate the unique
	// index uix_agendas_name. This should be run prior to creating the index.
	DeleteAgendasDuplicateRows = `DELETE FROM agendas
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY name ORDER BY id) AS rnum
				FROM agendas) t
			WHERE t.rnum > 1);`

	// agendas votes table

	CreateAgendaVotesTable = `CREATE TABLE IF NOT EXISTS agenda_votes (
		id SERIAL PRIMARY KEY,
		votes_row_id INT8,
		agendas_row_id INT8,
		agenda_vote_choice INT2
	);`

	// Insert
	insertAgendaVotesRow = `INSERT INTO agenda_votes (votes_row_id, agendas_row_id,
		agenda_vote_choice) VALUES ($1, $2, $3) `

	InsertAgendaVotesRow = insertAgendaVotesRow + `RETURNING id;`

	UpsertAgendaVotesRow = insertAgendaVotesRow + `ON CONFLICT (agendas_row_id,
		votes_row_id) DO UPDATE SET agenda_vote_choice = $3 RETURNING id;`

	IndexAgendaVotesTableOnAgendaID = `CREATE UNIQUE INDEX ` + IndexOfAgendaVotesTableOnRowIDs +
		` ON agenda_votes(votes_row_id, agendas_row_id);`
	DeindexAgendaVotesTableOnAgendaID = `DROP INDEX ` + IndexOfAgendaVotesTableOnRowIDs + `;`

	// DeleteAgendaVotesDuplicateRows removes rows that would violate the unique
	// index uix_agenda_votes. This should be run prior to creating the index.
	DeleteAgendaVotesDuplicateRows = `DELETE FROM agenda_votes
		WHERE id IN (SELECT id FROM (
				SELECT id, ROW_NUMBER()
				OVER (partition BY votes_row_id, agendas_row_id ORDER BY id) AS rnum
				FROM agenda_votes) t
			WHERE t.rnum > 1);`

	// Select

	SelectAgendasVotesByTime = `SELECT votes.block_time AS timestamp,` +
		selectAgendaVotesQuery + `GROUP BY timestamp ORDER BY timestamp;`

	SelectAgendasVotesByHeight = `SELECT votes.height AS height,` +
		selectAgendaVotesQuery + `GROUP BY height ORDER BY height;`

	SelectAgendaVoteTotals = `SELECT ` + selectAgendaVotesQuery + `;`

	selectAgendaVotesQuery = `
			count(CASE WHEN agenda_votes.agenda_vote_choice = $1 THEN 1 ELSE NULL END) AS yes,
			count(CASE WHEN agenda_votes.agenda_vote_choice = $2 THEN 1 ELSE NULL END) AS abstain,
			count(CASE WHEN agenda_votes.agenda_vote_choice = $3 THEN 1 ELSE NULL END) AS no,
			count(*) AS total
		FROM agenda_votes
		INNER JOIN votes ON agenda_votes.votes_row_id = votes.id
		WHERE agenda_votes.agendas_row_id = (SELECT id from agendas WHERE name = $4)
		AND votes.height >= $5 AND votes.height <= $6 `
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

// MakeVoteInsertStatement returns the appropriate votes insert statement for
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

// MakeMissInsertStatement returns the appropriate misses insert statement for
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

// MakeAgendaInsertStatement returns the appropriate agendas insert statement for
// the desired conflict checking and handling behavior. See the description of
// MakeTicketInsertStatement for details.
func MakeAgendaInsertStatement(checked bool) string {
	if checked {
		return UpsertAgendaRow
	}
	return InsertAgendaRow
}

// MakeAgendaVotesInsertStatement returns the appropriate agenda votes insert
// statement for the desired conflict checking and handling behavior. See the
// description of MakeTicketInsertStatement for details.
func MakeAgendaVotesInsertStatement(checked bool) string {
	if checked {
		return UpsertAgendaVotesRow
	}
	return InsertAgendaVotesRow
}

// MakeSelectTicketsByPurchaseDate returns the selectTicketsByPurchaseDate query
func MakeSelectTicketsByPurchaseDate(group string) string {
	return formatGroupingQuery(selectTicketsByPurchaseDate, group, "transactions.block_time")
}
