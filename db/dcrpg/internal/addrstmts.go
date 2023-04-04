package internal

import "fmt"

// These queries relate primarily to the "addresses" table.
const (
	CreateAddressTable = `CREATE TABLE IF NOT EXISTS addresses (
		id SERIAL8 PRIMARY KEY,
		address TEXT,
		tx_hash TEXT,
		valid_mainchain BOOLEAN,
		matching_tx_hash TEXT,
		value INT8,
		block_time TIMESTAMPTZ NOT NULL,
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
	IndexAddressTableOnVoutID = `CREATE UNIQUE INDEX IF NOT EXISTS ` + IndexOfAddressTableOnVoutID +
		` ON addresses(tx_vin_vout_row_id, address, is_funding);`
	DeindexAddressTableOnVoutID = `DROP INDEX IF EXISTS ` + IndexOfAddressTableOnVoutID + ` CASCADE;`

	// IndexBlockTimeOnTableAddress creates a sorted index on block_time, which
	// accelerates queries with ORDER BY block_time LIMIT n OFFSET m.
	IndexBlockTimeOnTableAddress = `CREATE INDEX IF NOT EXISTS ` + IndexOfAddressTableOnBlockTime +
		` ON addresses(block_time DESC NULLS LAST);`
	DeindexBlockTimeOnTableAddress = `DROP INDEX IF EXISTS ` + IndexOfAddressTableOnBlockTime + ` CASCADE;`

	IndexAddressTableOnMatchingTxHash = `CREATE INDEX IF NOT EXISTS ` + IndexOfAddressTableOnMatchingTx +
		` ON addresses(matching_tx_hash);`
	DeindexAddressTableOnMatchingTxHash = `DROP INDEX IF EXISTS ` + IndexOfAddressTableOnMatchingTx + ` CASCADE;`

	// IndexAddressTableOnAddress exists so address can be the first column in
	// an index; it is second in tx_vin_vout_row_id.
	IndexAddressTableOnAddress = `CREATE INDEX IF NOT EXISTS ` + IndexOfAddressTableOnAddress +
		` ON addresses(address);`
	DeindexAddressTableOnAddress = `DROP INDEX IF EXISTS ` + IndexOfAddressTableOnAddress + ` CASCADE;`

	IndexAddressTableOnTxHash = `CREATE INDEX IF NOT EXISTS ` + IndexOfAddressTableOnTx +
		` ON addresses(tx_hash, tx_vin_vout_index, is_funding);` // INCLUDE (valid_mainchain)? it's mutable tho
	DeindexAddressTableOnTxHash = `DROP INDEX IF EXISTS ` + IndexOfAddressTableOnTx + ` CASCADE;`

	// SelectSpendingTxsByPrevTx = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	// SelectSpendingTxByPrevOut = `SELECT id, tx_hash, tx_index FROM vins WHERE prev_tx_hash=$1 AND prev_tx_index=$2;`
	// SelectFundingTxsByTx      = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	// SelectFundingTxByTxIn     = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`

	addressTxnsSubQuery = `SELECT tx_hash
		FROM addresses
		WHERE address = $1
			AND valid_mainchain
		GROUP BY tx_hash
		ORDER BY MAX(block_time) DESC
		LIMIT $2 OFFSET $3`

	// need random table name? does lib/pq share sessions?
	CreateTempAddrTxnsTable = `CREATE TEMPORARY TABLE address_transactions
		ON COMMIT DROP -- do in a txn!
		AS (` + addressTxnsSubQuery + `);`

	SelectVinsForAddress0 = `SELECT vins.tx_hash, vins.tx_index, vins.prev_tx_hash, vins.prev_tx_index,
			vins.prev_tx_tree, vins.value_in -- no block height or block index
		FROM (` + addressTxnsSubQuery + `) atxs
		-- JOIN transactions txs ON txs.tx_hash=atxs.tx_hash
		-- JOIN vins ON vins.id = any(txs.vin_db_ids)
		JOIN vins ON vins.tx_hash = atxs.tx_hash;`

	SelectVinsForAddress = `SELECT vins.tx_hash, vins.tx_index, vins.prev_tx_hash, vins.prev_tx_index,
			vins.prev_tx_tree, vins.value_in, prevtxs.block_height, prevtxs.block_index
		FROM (` + addressTxnsSubQuery + `) atxs
		JOIN vins ON vins.tx_hash = atxs.tx_hash   -- JOIN vins on vins.id = any(txs.vin_db_ids)
		LEFT JOIN transactions prevtxs ON vins.prev_tx_hash=prevtxs.tx_hash;` // LEFT JOIN because prev_tx_hash may be coinbase

	SelectVoutsForAddress = `SELECT vouts.value, vouts.tx_hash, vouts.tx_index, vouts.version, vouts.pkscript
		FROM (` + addressTxnsSubQuery + `) atxs
		JOIN vouts ON vouts.tx_hash = atxs.tx_hash;` //    -- vouts.id = any(transactions.vout_db_ids)

	// select distinct tx_hash, block_time
	// from addresses
	// where address = 'DsSWTHFrsXV77SwAcMe451kJTwWjwPYjWTM' and valid_mainchain
	// order by block_time desc
	// limit 10 offset 0;

	SelectAddressTxns = `SELECT txs.tx_hash, txs.block_hash, txs.block_height, txs.block_time,
			txs.version, txs.lock_time, txs.size, txs.tx_type, cardinality(txs.vin_db_ids), cardinality(txs.vout_db_ids)
		FROM (` + addressTxnsSubQuery + `) atxs
		JOIN transactions txs ON txs.tx_hash = atxs.tx_hash
		WHERE is_valid AND is_mainchain -- needed?
		ORDER BY txs.block_time DESC;`

	// SelectAddressTxnsAlt is very slow with a join on the full tables
	SelectAddressTxnsAlt = `SELECT txs.tx_hash, txs.vin_db_ids, txs.vout_db_ids,
			txs.block_hash, txs.block_height, txs.block_time,
			txs.version, txs.lock_time, txs.size
		FROM addresses
		JOIN transactions txs ON addresses.tx_hash=txs.tx_hash AND valid_mainchain
		WHERE address = $1 AND is_valid AND is_mainchain
		ORDER BY block_height --  NOTE: height vs time
		LIMIT $2 OFFSET $3;`

	addrsColumnNames = `id, address, matching_tx_hash, tx_hash, tx_type, valid_mainchain,
		tx_vin_vout_index, block_time, tx_vin_vout_row_id, value, is_funding`

	SelectAddressAllByAddress = `SELECT ` + addrsColumnNames + ` FROM addresses
		WHERE address=$1
		ORDER BY block_time DESC, tx_hash ASC;`
	SelectAddressAllMainchainByAddress = `SELECT ` + addrsColumnNames + ` FROM addresses
		WHERE address=$1 AND valid_mainchain
		ORDER BY block_time DESC, tx_hash ASC;`

	SelectAddressesAllTxnWithHeight = `SELECT
			addresses.tx_hash,
			transactions.block_height
		FROM addresses
		INNER JOIN transactions
			ON addresses.tx_hash = transactions.tx_hash
				AND is_mainchain AND is_valid
		WHERE
			address = ANY($1) AND valid_mainchain
		ORDER BY
			transactions.time DESC,
			addresses.tx_hash ASC;`

	SelectAddressesAllTxn = `SELECT	tx_hash, block_time
		FROM addresses
		WHERE address = ANY($1) AND valid_mainchain
		ORDER BY block_time DESC, tx_hash ASC;`

	// selectAddressTimeGroupingCount return the count of record groups,
	// where grouping is done by a specified time interval, for an addresses.
	selectAddressTimeGroupingCount = `SELECT COUNT(DISTINCT %s) FROM addresses WHERE address=$1;`

	SelectAddressUnspentCountANDValue = `SELECT COUNT(*), SUM(value) FROM addresses
	    WHERE address = $1 AND is_funding = TRUE AND matching_tx_hash = '' AND valid_mainchain;`

	SelectAddressSpentCountANDValue = `SELECT COUNT(*), SUM(value) FROM addresses
		WHERE address = $1 AND is_funding = FALSE AND matching_tx_hash != '' AND valid_mainchain;`

	SelectAddressesMergedSpentCount = `SELECT COUNT( DISTINCT tx_hash ) FROM addresses
		WHERE address = $1 AND is_funding = FALSE AND valid_mainchain;`

	SelectAddressesMergedFundingCount = `SELECT COUNT( DISTINCT tx_hash ) FROM addresses
		WHERE address = $1 AND is_funding = TRUE AND valid_mainchain;`

	SelectAddressesMergedCount = `SELECT COUNT( DISTINCT tx_hash ) FROM addresses
		WHERE address = $1 AND valid_mainchain;`

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
	SelectAddressSpentUnspentCountAndValue = `SELECT
			(tx_type = 0) AS is_regular,
			COUNT(*),
			SUM(value),
			is_funding,
			(matching_tx_hash = '') AS all_empty_matching
			-- NOT BOOL_AND(matching_tx_hash = '') AS no_empty_matching
		FROM addresses
		WHERE address = $1 AND valid_mainchain
		GROUP BY tx_type=0, is_funding, 
			matching_tx_hash=''  -- separate spent and unspent
		ORDER BY count, is_funding;`

	SelectAddressUnspentWithTxn = `SELECT
			addresses.address,
			addresses.tx_hash,
			addresses.value,
			transactions.block_height,
			addresses.block_time,
			addresses.tx_vin_vout_index,
			vouts.pkscript
		FROM addresses
		JOIN transactions ON
			addresses.tx_hash = transactions.tx_hash
		JOIN vouts ON addresses.tx_vin_vout_row_id = vouts.id
		WHERE addresses.address=$1 AND addresses.is_funding AND addresses.matching_tx_hash = '' AND valid_mainchain
		ORDER BY addresses.block_time DESC;`
	// Since tx_vin_vout_row_id is the vouts table primary key (id) when
	// is_funding=true, there is no need to join vouts on tx_hash and tx_index.

	SelectAddressLimitNByAddress = `SELECT ` + addrsColumnNames + ` FROM addresses
		WHERE address=$1 AND valid_mainchain
		ORDER BY block_time DESC, tx_hash ASC
		LIMIT $2 OFFSET $3;`

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

	SelectAddressMergedCreditView = `SELECT tx_hash, valid_mainchain, block_time, sum(value), COUNT(*)
		FROM addresses
		WHERE address=$1 AND is_funding = TRUE           -- funding transactions
		GROUP BY (tx_hash, valid_mainchain, block_time)  -- merging common transactions in same valid mainchain block
		ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressMergedViewAll = `SELECT tx_hash, valid_mainchain, block_time, sum(CASE WHEN is_funding = TRUE THEN value ELSE 0 END),
		sum(CASE WHEN is_funding = FALSE THEN value ELSE 0 END), COUNT(*)
		FROM addresses
		WHERE address=$1                                 -- spending and funding transactions
		GROUP BY (tx_hash, valid_mainchain, block_time)  -- merging common transactions in same valid mainchain block
		ORDER BY block_time DESC`

	SelectAddressMergedView = SelectAddressMergedViewAll + ` LIMIT $2 OFFSET $3;`

	SelectAddressCsvView = "SELECT tx_hash, valid_mainchain, matching_tx_hash, value, block_time, is_funding, " +
		"tx_vin_vout_index, tx_type FROM addresses WHERE address=$1 ORDER BY block_time DESC"

	SelectAddressDebitsLimitNByAddress = `SELECT ` + addrsColumnNames + `
		FROM addresses WHERE address=$1 AND is_funding = FALSE AND valid_mainchain
		ORDER BY block_time DESC, tx_hash ASC
		LIMIT $2 OFFSET $3;`

	SelectAddressCreditsLimitNByAddress = `SELECT ` + addrsColumnNames + `
		FROM addresses WHERE address=$1 AND is_funding AND valid_mainchain
		ORDER BY block_time DESC, tx_hash ASC
		LIMIT $2 OFFSET $3;`

	SelectAddressIDsByFundingOutpoint = `SELECT id, address, value
		FROM addresses
		WHERE tx_hash=$1 AND tx_vin_vout_index=$2 AND is_funding
		ORDER BY block_time DESC;`

	SelectAddressOldestTxBlockTime = `SELECT block_time FROM addresses WHERE
		address=$1 ORDER BY block_time LIMIT 1;`

	// selectAddressTxTypesByAddress gets the transaction type histogram for the
	// given address using block time binning with bin size of block_time.
	// Regular transactions are grouped into (SentRtx and ReceivedRtx), SSTx
	// defines tickets, SSGen defines votes, and SSRtx defines revocations.
	selectAddressTxTypesByAddress = `SELECT %s as timestamp,
		COUNT(CASE WHEN tx_type = 0 AND is_funding = false THEN 1 ELSE NULL END) as SentRtx,
		COUNT(CASE WHEN tx_type = 0 AND is_funding = true THEN 1 ELSE NULL END) as ReceivedRtx,
		COUNT(CASE WHEN tx_type = 1 THEN 1 ELSE NULL END) as SSTx,
		COUNT(CASE WHEN tx_type = 2 THEN 1 ELSE NULL END) as SSGen,
		COUNT(CASE WHEN tx_type = 3 THEN 1 ELSE NULL END) as SSRtx
		FROM addresses
		WHERE address=$1 AND valid_mainchain
		GROUP BY timestamp
		ORDER BY timestamp;`

	selectAddressAmountFlowByAddress = `SELECT %s as timestamp,
		SUM(CASE WHEN is_funding = TRUE THEN value ELSE 0 END) as received,
		SUM(CASE WHEN is_funding = FALSE THEN value ELSE 0 END) as sent
		FROM addresses
		WHERE address=$1 AND valid_mainchain
		GROUP BY timestamp
		ORDER BY timestamp;`

	// UPDATEs/SETs

	UpdateAllAddressesMatchingTxHashRange = `UPDATE addresses SET matching_tx_hash=transactions.tx_hash
		FROM vouts, transactions
		WHERE block_height >= $1 AND block_height < $2 AND vouts.value>0 AND addresses.is_funding
			AND vouts.tx_hash=addresses.tx_hash
			AND vouts.tx_index=addresses.tx_vin_vout_index
			AND transactions.id=vouts.spend_tx_row_id;`

	UpdateAllAddressesMatchingTxHash = `UPDATE addresses SET matching_tx_hash=transactions.tx_hash
		FROM vouts, transactions
		WHERE vouts.value>0 AND addresses.is_funding
			AND vouts.tx_hash=addresses.tx_hash
			AND vouts.tx_index=addresses.tx_vin_vout_index
			AND transactions.id=vouts.spend_tx_row_id;`

	UpdateAllAddressesMatchingTxHash1 = `UPDATE addresses SET matching_tx_hash=stuff.matching
		FROM (SELECT transactions.tx_hash AS matching, vouts.tx_hash, vouts.tx_index
			FROM transactions
			JOIN vouts ON vouts.value>0
				AND transactions.id=vouts.spend_tx_row_id)
			AS stuff
		WHERE addresses.is_funding
			AND stuff.tx_hash=addresses.tx_hash
			AND stuff.tx_index=addresses.tx_vin_vout_index;`

	UpdateAllAddressesMatchingTxHash2 = `UPDATE addresses SET matching_tx_hash=transactions.tx_hash
		FROM transactions, (SELECT addresses.id AS addr_id, spend_tx_row_id
			FROM vouts
			JOIN addresses ON vouts.value>0
				AND addresses.is_funding
				AND vouts.tx_hash=addresses.tx_hash
				AND vouts.tx_index=addresses.tx_vin_vout_index)
			AS stuff
		WHERE addresses.id=stuff.addr_id
			AND transactions.id=stuff.spend_tx_row_id;`

	// SetAddressMatchingTxHashForOutpoint sets the matching tx hash (a spending
	// transaction) for the addresses rows corresponding to the specified
	// outpoint (tx_hash:tx_vin_vout_index), a funding tx row.
	SetAddressMatchingTxHashForOutpoint = `UPDATE addresses SET matching_tx_hash=$1
		WHERE tx_hash=$2 AND is_funding AND tx_vin_vout_index=$3 AND valid_mainchain = $4 ` // not terminated with ;

	// AssignMatchingTxHashForOutpoint is like
	// SetAddressMatchingTxHashForOutpoint except that it only updates rows
	// where matching_tx_hash is not already set.
	AssignMatchingTxHashForOutpoint = SetAddressMatchingTxHashForOutpoint + ` AND matching_tx_hash='';`

	SetAddressMainchainForVoutIDs = `UPDATE addresses SET valid_mainchain=$1
		WHERE is_funding = TRUE AND tx_vin_vout_row_id=$2
		RETURNING address;`

	SetAddressMainchainForVinIDs = `UPDATE addresses SET valid_mainchain=$1
		WHERE is_funding = FALSE AND tx_vin_vout_row_id=$2
		RETURNING address;`

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

// MakeSelectAddressTxTypesByAddress returns the selectAddressTxTypesByAddress query
func MakeSelectAddressTxTypesByAddress(group string) string {
	return formatGroupingQuery(selectAddressTxTypesByAddress, group, "block_time")
}

// MakeSelectAddressAmountFlowByAddress returns the selectAddressAmountFlowByAddress query
func MakeSelectAddressAmountFlowByAddress(group string) string {
	return formatGroupingQuery(selectAddressAmountFlowByAddress, group, "block_time")
}

func MakeSelectAddressTimeGroupingCount(group string) string {
	return formatGroupingQuery(selectAddressTimeGroupingCount, group, "block_time")
}

// Since date_trunc function doesn't have an option to group by "all" grouping,
// formatGroupingQuery removes the date_trunc from the sql query as its not applicable.
func formatGroupingQuery(mainQuery, group, column string) string {
	if group == "all" {
		return fmt.Sprintf(mainQuery, column)
	}
	subQuery := fmt.Sprintf("date_trunc('%s', %s)", group, column)
	return fmt.Sprintf(mainQuery, subQuery)
}
