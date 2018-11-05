package internal

const (
	CreateAddressTable = `CREATE TABLE IF NOT EXISTS addresses (
		id SERIAL8 PRIMARY KEY,
		address TEXT,
		tx_hash TEXT,
		valid_mainchain BOOLEAN,
		matching_tx_hash TEXT,
		value INT8,
		block_time INT8 NOT NULL,
		is_funding BOOLEAN,
		tx_vin_vout_index INT4,
		tx_vin_vout_row_id INT8,
		tx_type INT4
	);`

	// insertAddressRow is the basis for several address insert/upsert
	// statements.
	insertAddressRow = `INSERT INTO addresses (address, matching_tx_hash, tx_hash,
		tx_vin_vout_index, tx_vin_vout_row_id, value, block_time, is_funding, valid_mainchain, tx_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) `

	// InsertAddressRow inserts a address block row without checking for unique
	// index conflicts. This should only be used before the unique indexes are
	// created or there may be constraint violations (errors).
	InsertAddressRow = insertAddressRow + `RETURNING id;`

	// UpsertAddressRow is an upsert (insert or update on conflict), returning
	// the inserted/updated address row id.
	UpsertAddressRow = insertAddressRow + `ON CONFLICT (tx_vin_vout_row_id, address, is_funding) DO UPDATE
		SET matching_tx_hash = $2, tx_hash = $3, tx_vin_vout_index = $4,
		block_time = $7, valid_mainchain = $9 RETURNING id;`

	// InsertAddressRowOnConflictDoNothing allows an INSERT with a DO NOTHING on
	// conflict with addresses' unique tx index, while returning the row id of
	// either the inserted row or the existing row that causes the conflict. The
	// complexity of this statement is necessary to avoid an unnecessary UPSERT,
	// which would have performance consequences. The row is not locked.
	InsertAddressRowOnConflictDoNothing = `WITH inserting AS (` +
		insertAddressRow +
		`	ON CONFLICT (tx_vin_vout_row_id, address, is_funding) DO NOTHING -- no lock on row
			RETURNING id
		)
		SELECT id FROM inserting
		UNION  ALL
		SELECT id FROM addresses
		WHERE  address = $1 AND is_funding = $8 AND tx_vin_vout_row_id = $5 -- only executed if no INSERT
		LIMIT  1;`

	// IndexAddressTableOnVoutID creates the unique index uix_addresses_vout_id
	// on (tx_vin_vout_row_id, address, is_funding).
	IndexAddressTableOnVoutID = `CREATE UNIQUE INDEX uix_addresses_vout_id
		ON addresses(tx_vin_vout_row_id, address, is_funding);`
	DeindexAddressTableOnVoutID = `DROP INDEX uix_addresses_vout_id;`

	// IndexBlockTimeOnTableAddress creates a sorted index on block_time, which
	// accelerates queries with ORDER BY block_time LIMIT n OFFSET m.
	IndexBlockTimeOnTableAddress = `CREATE INDEX block_time_index
		ON addresses(block_time DESC NULLS LAST);`
	DeindexBlockTimeOnTableAddress = `DROP INDEX block_time_index;`

	IndexMatchingTxHashOnTableAddress = `CREATE INDEX matching_tx_hash_index
	    ON addresses(matching_tx_hash);`
	DeindexMatchingTxHashOnTableAddress = `DROP INDEX matching_tx_hash_index;`

	IndexAddressTableOnAddress = `CREATE INDEX uix_addresses_address
		ON addresses(address);`
	DeindexAddressTableOnAddress = `DROP INDEX uix_addresses_address;`

	IndexAddressTableOnTxHash = `CREATE INDEX uix_addresses_funding_tx
		ON addresses(tx_hash);`
	DeindexAddressTableOnTxHash = `DROP INDEX uix_addresses_funding_tx;`

	// SelectSpendingTxsByPrevTx = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	// SelectSpendingTxByPrevOut = `SELECT id, tx_hash, tx_index FROM vins WHERE prev_tx_hash=$1 AND prev_tx_index=$2;`
	// SelectFundingTxsByTx      = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	// SelectFundingTxByTxIn     = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`

	addrsColumnNames = `id, address, matching_tx_hash, tx_hash, tx_type, valid_mainchain,
		tx_vin_vout_index, block_time, tx_vin_vout_row_id, value, is_funding`

	SelectAddressAllByAddress = `SELECT ` + addrsColumnNames + ` FROM addresses WHERE address=$1 ORDER BY block_time DESC;`
	SelectAddressRecvCount    = `SELECT COUNT(*) FROM addresses WHERE address=$1 AND valid_mainchain = TRUE;`

	SelectAddressesAllTxn = `SELECT
			transactions.tx_hash,
			block_height
		FROM
			addresses
			INNER JOIN
				transactions
				ON addresses.tx_hash = transactions.tx_hash
				AND is_mainchain = TRUE AND is_valid=TRUE
		WHERE
			address = ANY($1) AND valid_mainchain=true
		ORDER BY
			time DESC,
			transactions.tx_hash ASC;`

	SelectAddressUnspentCountANDValue = `SELECT COUNT(*), SUM(value) FROM addresses
	    WHERE address = $1 AND is_funding = TRUE AND matching_tx_hash = '' AND valid_mainchain = TRUE;`

	SelectAddressSpentCountANDValue = `SELECT COUNT(*), SUM(value) FROM addresses
		WHERE address = $1 AND is_funding = FALSE AND matching_tx_hash != '' AND valid_mainchain = TRUE;`

	SelectAddressesMergedSpentCount = `SELECT COUNT( DISTINCT tx_hash ) FROM addresses
		WHERE address = $1 AND is_funding = FALSE AND valid_mainchain = TRUE;`

	// SelectAddressSpentUnspentCountAndValue gets the number and combined spent
	// and unspent outpoints for the given address. The key is the "GROUP BY
	// is_funding, matching_tx_hash=''" part of the statement that gets the data
	// for the combinations of is_funding (boolean) and matching_tx_hash=''
	// (boolean). There should never be any with is_funding=true where
	// matching_tx_hash is empty, thus there are three rows in the output. For
	// example, the first row is the spending transactions that must have
	// matching_tx_hash set, the second row the the funding transactions for the
	// first row (notice the equal count and sum), and the third row are the
	// unspent outpoints that are is_funding=true but with an empty
	// matching_tx_hash:
	//
	// count  |      sum       | is_funding | all_empty_matching | no_empty_matching
	// --------+----------------+------------+--------------------+--------------------
	//   45150 | 12352318108368 | f          | f                  | t
	//   45150 | 12352318108368 | t          | f                  | t
	//  229145 | 55875634749104 | t          | t                  | f
	// (3 rows)
	//
	// Since part of the grouping is on "matching_tx_hash = ''", what is
	// logically "any" empty matching is actually no_empty_matching.
	SelectAddressSpentUnspentCountAndValue = `SELECT COUNT(*),
			SUM(value),
			is_funding,
			BOOL_AND(matching_tx_hash = '') AS all_empty_matching
			-- NOT BOOL_AND(matching_tx_hash = '') AS no_empty_matching
		FROM addresses
		WHERE address = $1 AND valid_mainchain = TRUE
		GROUP BY is_funding, matching_tx_hash=''  -- separate spent and unspent
		ORDER BY count, is_funding;`

	SelectAddressUnspentWithTxn = `SELECT
			addresses.address,
			addresses.tx_hash,
			addresses.value,
			transactions.block_height,
			addresses.block_time,
			tx_vin_vout_index,
			pkscript
		FROM addresses
		JOIN transactions ON
			addresses.tx_hash = transactions.tx_hash
		JOIN vouts ON addresses.tx_hash = vouts.tx_hash AND addresses.tx_vin_vout_index=vouts.tx_index
		WHERE addresses.address=$1 AND addresses.is_funding = TRUE AND addresses.matching_tx_hash = '' AND valid_mainchain = TRUE
		ORDER BY addresses.block_time DESC;`

	SelectAddressLimitNByAddress = `SELECT ` + addrsColumnNames + ` FROM addresses
		WHERE address=$1 AND valid_mainchain = TRUE
		ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	// SelectAddressLimitNByAddressSubQry was used in certain cases prior to
	// sorting the block_time_index.
	// SelectAddressLimitNByAddressSubQry = `WITH these AS (SELECT ` + addrsColumnNames +
	// 	` FROM addresses WHERE address=$1 AND valid_mainchain = TRUE)
	// 	SELECT * FROM these
	// 	ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressMergedDebitView = `SELECT tx_hash, valid_mainchain, block_time, sum(value), COUNT(*)
		FROM addresses
		WHERE address=$1 AND is_funding = FALSE          -- spending transactions
		GROUP BY (tx_hash, valid_mainchain, block_time)  -- merging common transactions in same valid mainchain block
		ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressDebitsLimitNByAddress = `SELECT ` + addrsColumnNames + `
		FROM addresses WHERE address=$1 AND is_funding = FALSE AND valid_mainchain = TRUE
		ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressCreditsLimitNByAddress = `SELECT ` + addrsColumnNames + `
		FROM addresses WHERE address=$1 AND is_funding = TRUE AND valid_mainchain = TRUE
		ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressIDsByFundingOutpoint = `SELECT id, address, value FROM addresses WHERE tx_hash=$1 AND
		tx_vin_vout_index=$2 AND is_funding = TRUE ORDER BY block_time DESC;`

	SelectAddressIDByVoutIDAddress = `SELECT id FROM addresses WHERE address=$1 AND
	    tx_vin_vout_row_id=$2 AND is_funding = TRUE;`

	SelectAddressOldestTxBlockTime = `SELECT block_time FROM addresses WHERE
		address=$1 ORDER BY block_time LIMIT 1;`

	// SelectAddressTxTypesByAddress gets the transaction type histogram for the
	// given address using block time binning with bin size of block_time.
	// Regular transactions are grouped into (SentRtx and ReceivedRtx), SSTx
	// defines tickets, SSGen defines votes, and SSRtx defines revocations.
	SelectAddressTxTypesByAddress = `SELECT (block_time/$1)*$1 as timestamp,
		COUNT(CASE WHEN tx_type = 0 AND is_funding = false THEN 1 ELSE NULL END) as SentRtx,
		COUNT(CASE WHEN tx_type = 0 AND is_funding = true THEN 1 ELSE NULL END) as ReceivedRtx,
		COUNT(CASE WHEN tx_type = 1 THEN 1 ELSE NULL END) as SSTx,
		COUNT(CASE WHEN tx_type = 2 THEN 1 ELSE NULL END) as SSGen,
		COUNT(CASE WHEN tx_type = 3 THEN 1 ELSE NULL END) as SSRtx
		FROM addresses WHERE address=$2 GROUP BY timestamp ORDER BY timestamp;`

	SelectAddressAmountFlowByAddress = `SELECT (block_time/$1)*$1 as timestamp,
		SUM(CASE WHEN is_funding = TRUE THEN value ELSE 0 END) as received,
		SUM(CASE WHEN is_funding = FALSE THEN value ELSE 0 END) as sent FROM
		addresses WHERE address=$2 GROUP BY timestamp ORDER BY timestamp;`

	SelectAddressUnspentAmountByAddress = `SELECT (block_time/$1)*$1 as timestamp,
		SUM(value) as unspent FROM addresses WHERE address=$2 AND is_funding=TRUE
		AND matching_tx_hash ='' GROUP BY timestamp ORDER BY timestamp;`

	// UPDATEs/SETs

	// SetAddressMatchingTxHashForOutpoint sets the matching tx hash (a spending
	// transaction) for the addresses rows corresponding to the specified
	// outpoint (tx_hash:tx_vin_vout_index), a funding tx row.
	SetAddressMatchingTxHashForOutpoint = `UPDATE addresses SET matching_tx_hash=$1
		WHERE tx_hash=$2 AND is_funding = TRUE AND tx_vin_vout_index=$3`  // not terminated with ;

	// AssignMatchingTxHashForOutpoint is like
	// SetAddressMatchingTxHashForOutpoint except that it only updates rows
	// where matching_tx_hash is not already set.
	AssignMatchingTxHashForOutpoint = SetAddressMatchingTxHashForOutpoint + ` AND matching_tx_hash='';`

	SetAddressMainchainForVoutIDs = `UPDATE addresses SET valid_mainchain=$1
		WHERE is_funding = TRUE AND tx_vin_vout_row_id=$2;`

	SetAddressMainchainForVinIDs = `UPDATE addresses SET valid_mainchain=$1
		WHERE is_funding = FALSE AND tx_vin_vout_row_id=$2;`

	SetTxTypeOnAddressesByVinAndVoutIDs = `UPDATE addresses SET tx_type=$1 WHERE
		tx_vin_vout_row_id=$2 AND is_funding=$3;`

	// Patches/upgrades

	// The SelectAddressesGloballyInvalid and UpdateAddressesGloballyInvalid
	// queries are used to patch a bug in new block handling that neglected to
	// set valid_mainchain=false for the previous block when the new block's
	// vote bits invalidate the previous block. This pertains to dcrpg 3.5.x.

	// SelectAddressesGloballyInvalid selects the row ids of the addresses table
	// corresponding to transactions that should have valid_mainchain set to
	// false according to the transactions table. Should is defined as any
	// occurrence of a given transaction (hash) being flagged as is_valid AND
	// is_mainchain.
	SelectAddressesGloballyInvalid = `SELECT id, valid_mainchain
		FROM addresses
		JOIN
			(  -- globally_invalid transactions with no (is_valid && is_mainchain)=true occurrence
				SELECT tx_hash
				FROM
				(
					SELECT bool_or(is_valid AND is_mainchain) AS any_valid, tx_hash
					FROM transactions
					GROUP BY tx_hash
				) AS foo
				WHERE any_valid=FALSE
			) AS globally_invalid
		ON globally_invalid.tx_hash = addresses.tx_hash `

	// UpdateAddressesGloballyInvalid sets valid_mainchain=false on address rows
	// identified by the SelectAddressesGloballyInvalid query (ids of
	// globally_invalid subquery table) as requiring this flag set, but which do
	// not already have it set (incorrectly_valid).
	UpdateAddressesGloballyInvalid = `UPDATE addresses SET valid_mainchain=false
		FROM (
			SELECT id FROM
			(
				` + SelectAddressesGloballyInvalid + `
			) AS invalid_ids
			WHERE invalid_ids.valid_mainchain=true
		) AS incorrectly_valid
		WHERE incorrectly_valid.id=addresses.id;`

	// UpdateAddressesFundingMatchingHash sets matching_tx_hash as per the vins
	// table. This is needed to fix partially updated addresses table entries
	// that were affected by stake invalidation.
	UpdateAddressesFundingMatchingHash = `UPDATE addresses SET matching_tx_hash=vins.tx_hash -- , matching_tx_index=vins.tx_index
		FROM vins
		WHERE addresses.tx_hash=vins.prev_tx_hash
		AND addresses.tx_vin_vout_index=vins.prev_tx_index
		AND is_funding=TRUE
		AND is_valid=TRUE
		AND matching_tx_hash!=vins.tx_hash;`
	// AND (matching_tx_hash!=vins.tx_hash OR matching_tx_index!=vins.tx_index);`

	// UpdateValidMainchainFromTransactions sets valid_mainchain in all rows of
	// the addresses table according to the transactions table, unlike
	// UpdateAddressesGloballyInvalid that does it selectively for only the
	// incorrectly set addresses table rows.  This is much slower.
	UpdateValidMainchainFromTransactions = `UPDATE addresses
		SET valid_mainchain = (tr.is_mainchain::int * tr.is_valid::int)::boolean
		FROM transactions AS tr
		WHERE addresses.tx_hash = tr.tx_hash;`
)

// MakeAddressRowInsertStatement returns the appropriate addresses insert statement for
// the desired conflict checking and handling behavior. For checked=false, no ON
// CONFLICT checks will be performed, and the value of updateOnConflict is
// ignored. This should only be used prior to creating the unique indexes as
// these constraints will cause an errors if an inserted row violates a
// constraint. For updateOnConflict=true, an upsert statement will be provided
// that UPDATEs the conflicting row. For updateOnConflict=false, the statement
// will either insert or do nothing, and return the inserted (new) or
// conflicting (unmodified) row id.
func MakeAddressRowInsertStatement(checked, updateOnConflict bool) string {
	if !checked {
		return InsertAddressRow
	}
	if updateOnConflict {
		return UpsertAddressRow
	}
	return InsertAddressRowOnConflictDoNothing
}
