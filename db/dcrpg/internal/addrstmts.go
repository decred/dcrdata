package internal

const (
	insertAddressRow0 = `INSERT INTO addresses (address, matching_tx_hash, tx_hash,
		tx_vin_vout_index, tx_vin_vout_row_id, value, block_time, is_funding, valid_mainchain)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) `

	InsertAddressRow = insertAddressRow0 + `RETURNING id;`

	UpsertAddressRow = insertAddressRow0 + `ON CONFLICT (tx_vin_vout_row_id, address, is_funding) DO UPDATE 
		SET address = $1, tx_vin_vout_row_id = $5 RETURNING id;`
	InsertAddressRowReturnID = `WITH inserting AS (` +
		insertAddressRow0 +
		`ON CONFLICT (tx_vin_vout_row_id, address, is_funding) DO UPDATE
		SET address = NULL WHERE FALSE
		RETURNING id
		)
		SELECT id FROM inserting
		UNION  ALL
		SELECT id FROM addresses
		WHERE  address = $1, is_funding = true 
		AND tx_vin_vout_row_id = $5
		LIMIT  1;`

	// SelectSpendingTxsByPrevTx = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	// SelectSpendingTxByPrevOut = `SELECT id, tx_hash, tx_index FROM vins WHERE prev_tx_hash=$1 AND prev_tx_index=$2;`
	// SelectFundingTxsByTx      = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	// SelectFundingTxByTxIn     = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`

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
		tx_vin_vout_row_id INT8
		);`

	SelectAddressAllByAddress = `SELECT * FROM addresses WHERE address=$1 ORDER BY block_time DESC;`
	SelectAddressRecvCount    = `SELECT COUNT(*) FROM addresses WHERE address=$1;`
	SelectAddressesAllTxn     = `SELECT tx_hash, block_time AS tx_time, ftxd.block_height AS height 
		FROM addresses LEFT JOIN transactions AS ftxd ON funding_tx_row_id=ftxd.id 
		WHERE address = $1 ORDER BY tx_time desc;`

	// WTF?
	SelectAddressesTxnByFundingTx = `SELECT tx_vin_vout_index, tx_hash, tx_vin_vout_index, block_height
		FROM addresses
		LEFT JOIN transactions ON transactions.tx_hash=tx_hash AND is_funding = FALSE
		WHERE address = $1 AND tx_hash=$2;`

	SelectAddressUnspentCountAndValue = `SELECT COUNT(*), SUM(value) FROM addresses 
	    WHERE address = $1 AND is_funding = TRUE AND matching_tx_hash = '' AND valid_mainchain = true;`

	SelectAddressSpentCountAndValue = `SELECT COUNT(*), SUM(value) FROM addresses 
	    WHERE address = $1 AND is_funding = FALSE AND matching_tx_hash != '' AND valid_mainchain = true;`

	SelectAddressesMergedSpentCount = `SELECT COUNT( distinct tx_hash ) FROM addresses
		WHERE address = $1 and is_funding = false`

	SelectAddressUnspentWithTxn = `SELECT addresses.address, addresses.tx_hash, addresses.value,
			transactions.block_height, addresses.block_time, tx_vin_vout_index, pkscript
		FROM addresses 
		JOIN transactions ON 
			addresses.tx_hash = transactions.tx_hash
		JOIN vouts on addresses.tx_hash = vouts.tx_hash AND addresses.tx_vin_vout_index=vouts.tx_index
			WHERE addresses.address=$1 AND addresses.is_funding = FALSE AND valid_mainchain = true
			ORDER BY addresses.block_time DESC;`

	addrsColumnNames = `id, address, matching_tx_hash, tx_hash, valid_mainchain, 
		tx_vin_vout_index, block_time, tx_vin_vout_row_id, value, is_funding`

	SelectAddressLimitNByAddress = `SELECT ` + addrsColumnNames + ` FROM addresses
	    WHERE address=$1 ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressLimitNByAddressSubQry = `WITH these as (SELECT ` + addrsColumnNames +
		` FROM addresses WHERE address=$1)
		SELECT * FROM these order by block_time desc limit $2 offset $3;`

	SelectAddressMergedDebitView = `SELECT tx_hash, block_time, sum(value), 
		COUNT(*) FROM addresses WHERE address=$1 AND is_funding = FALSE 
		GROUP BY (tx_hash, block_time) ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressDebitsLimitNByAddress = `SELECT ` + addrsColumnNames + `
		FROM addresses WHERE address=$1 AND is_funding = FALSE
		ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressCreditsLimitNByAddress = `SELECT ` + addrsColumnNames + `
		FROM addresses WHERE address=$1 AND is_funding = TRUE
		ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressIDsByFundingOutpoint = `SELECT id, address FROM addresses WHERE tx_hash=$1 AND 
		tx_vin_vout_index=$2 and is_funding = TRUE ORDER BY block_time DESC;`

	SelectAddressIDByVoutIDAddress = `SELECT id FROM addresses WHERE address=$1 and 
	    tx_vin_vout_row_id=$2 and is_funding = true;`

	SetAddressFundingForMatchingTxHash = `UPDATE addresses SET matching_tx_hash=$1 
		WHERE tx_hash=$2 and is_funding = true and tx_vin_vout_index=$3;`

	SetAddressMainchainForVoutIDs = `UPDATE addresses SET valid_mainchain=$1 
		WHERE is_funding = true and tx_vin_vout_row_id=$2;`

	SetAddressMainchainForVinIDs = `UPDATE addresses SET valid_mainchain=$1 
		WHERE is_funding = false and tx_vin_vout_row_id=$2;`

	UpdateValidMainchainFromTransactions = `UPDATE addresses
		SET valid_mainchain = (tr.is_mainchain::int * tr.is_valid::int)::boolean
		FROM transactions AS tr
		WHERE addresses.tx_hash = tr.tx_hash;`

	IndexBlockTimeOnTableAddress   = `CREATE INDEX block_time_index ON addresses (block_time);`
	DeindexBlockTimeOnTableAddress = `DROP INDEX block_time_index;`

	IndexMatchingTxHashOnTableAddress = `CREATE INDEX matching_tx_hash_index 
	    ON addresses (matching_tx_hash);`
	DeindexMatchingTxHashOnTableAddress = `DROP INDEX matching_tx_hash_index;`

	IndexAddressTableOnAddress = `CREATE INDEX uix_addresses_address
		ON addresses(address);`
	DeindexAddressTableOnAddress = `DROP INDEX uix_addresses_address;`

	IndexAddressTableOnVoutID = `CREATE UNIQUE INDEX uix_addresses_vout_id
		ON addresses(tx_vin_vout_row_id, address, is_funding);`
	DeindexAddressTableOnVoutID = `DROP INDEX uix_addresses_vout_id;`

	IndexAddressTableOnTxHash = `CREATE INDEX uix_addresses_funding_tx
		ON addresses(tx_hash);`
	DeindexAddressTableOnTxHash = `DROP INDEX uix_addresses_funding_tx;`
)

func MakeAddressRowInsertStatement(checked bool) string {
	if checked {
		return UpsertAddressRow
	}
	return InsertAddressRow
}
