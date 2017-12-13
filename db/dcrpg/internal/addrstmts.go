package internal

const (
	insertAddressRow0 = `INSERT INTO addresses (address, funding_tx_row_id,
		funding_tx_hash, funding_tx_vout_index, vout_row_id, value)
		VALUES ($1, $2, $3, $4, $5, $6) `
	InsertAddressRow = insertAddressRow0 + `RETURNING id;`
	// InsertAddressRowChecked = insertAddressRow0 +
	// 	`ON CONFLICT (address, vout_row_id) DO NOTHING RETURNING id;`
	UpsertAddressRow = insertAddressRow0 + `ON CONFLICT (address, vout_row_id) DO UPDATE 
		SET address = $1, vout_row_id = $5 RETURNING id;`
	InsertAddressRowReturnID = `WITH inserting AS (` +
		insertAddressRow0 +
		`ON CONFLICT (address, vout_row_id) DO UPDATE
		SET address = NULL WHERE FALSE
		RETURNING id
		)
	 SELECT id FROM inserting
	 UNION  ALL
	 SELECT id FROM addresses
	 WHERE  address = $1 AND vout_row_id = $5
	 LIMIT  1;`

	insertAddressRowFull = `INSERT INTO addresses (address, funding_tx_row_id, funding_tx_hash,
		funding_tx_vout_index, vout_row_id, value, spending_tx_row_id, 
		spending_tx_hash, spending_tx_vin_index, vin_row_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) `

	// SelectSpendingTxsByPrevTx = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	// SelectSpendingTxByPrevOut = `SELECT id, tx_hash, tx_index FROM vins WHERE prev_tx_hash=$1 AND prev_tx_index=$2;`
	// SelectFundingTxsByTx      = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	// SelectFundingTxByTxIn     = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`

	CreateAddressTable = `CREATE TABLE IF NOT EXISTS addresses (
		id SERIAL8 PRIMARY KEY,
		address TEXT,
		funding_tx_row_id INT8,
		funding_tx_hash TEXT,
		funding_tx_vout_index INT8,
		vout_row_id INT8,
		value INT8,
		spending_tx_row_id INT8,
		spending_tx_hash TEXT,
		spending_tx_vin_index INT4,
		vin_row_id INT8
	);`

	SelectAddressAllByAddress          = `SELECT * FROM addresses WHERE address=$1 order by id desc;`
	SelectAddressRecvCount             = `SELECT COUNT(*) FROM addresses WHERE address=$1;`
	SelectAddressUnspentCountAndValue  = `SELECT COUNT(*), SUM(value) FROM addresses WHERE address=$1 and spending_tx_row_id IS NULL;`
	SelectAddressSpentCountAndValue    = `SELECT COUNT(*), SUM(value) FROM addresses WHERE address=$1 and spending_tx_row_id IS NOT NULL;`
	SelectAddressLimitNByAddress       = `SELECT * FROM addresses WHERE address=$1 order by id desc limit $2 offset $3;`
	SelectAddressLimitNByAddressSubQry = `WITH these as (SELECT * FROM addresses WHERE address=$1)
		SELECT * FROM these order by id desc limit $2 offset $3;`
	SelectAddressIDsByFundingOutpoint = `SELECT id, address FROM addresses
		WHERE funding_tx_hash=$1 and funding_tx_vout_index=$2;`
	SelectAddressIDByVoutIDAddress = `SELECT id FROM addresses
		WHERE address=$1 and vout_row_id=$2;`

	SetAddressSpendingForID = `UPDATE addresses SET spending_tx_row_id = $2, 
		spending_tx_hash = $3, spending_tx_vin_index = $4, vin_row_id = $5 
		WHERE id=$1;`
	SetAddressSpendingForOutpoint = `UPDATE addresses SET spending_tx_row_id = $3, 
		spending_tx_hash = $4, spending_tx_vin_index = $5, vin_row_id = $6 
		WHERE funding_tx_hash=$1 and funding_tx_vout_index=$2;`

	IndexAddressTableOnAddress = `CREATE INDEX uix_addresses_address
		ON addresses(address);`
	DeindexAddressTableOnAddress = `DROP INDEX uix_addresses_address;`

	IndexAddressTableOnVoutID = `CREATE UNIQUE INDEX uix_addresses_vout_id
		ON addresses(vout_row_id);`
	DeindexAddressTableOnVoutID = `DROP INDEX uix_addresses_vout_id;`

	IndexAddressTableOnFundingTx = `CREATE INDEX uix_addresses_funding_tx
		ON addresses(funding_tx_hash, funding_tx_vout_index);`
	DeindexAddressTableOnFundingTx = `DROP INDEX uix_addresses_funding_tx;`
)
