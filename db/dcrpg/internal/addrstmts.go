package internal

const (
	insertAddressRow0 = `INSERT INTO addresses (address, in_out_row_id, tx_hash,
		tx_vin_vout_index, tx_vin_vout_row_id, value, block_time, is_funding)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	InsertAddressRow = insertAddressRow0 + `RETURNING id;`
	// InsertAddressRowChecked = insertAddressRow0 +
	// 	`ON CONFLICT (vout_row_id, address) DO NOTHING RETURNING id;`
	UpsertAddressRow = insertAddressRow0 + `ON CONFLICT (tx_vin_vout_row_id, address) DO UPDATE 
		SET address = $1, tx_vin_vout_row_id = $5 RETURNING id;`
	InsertAddressRowReturnID = `WITH inserting AS (` +
		insertAddressRow0 +
		`ON CONFLICT (tx_vin_vout_row_id, address) DO UPDATE
		SET address = NULL WHERE FALSE
		RETURNING id
		)
	 SELECT id FROM inserting
	 UNION  ALL
	 SELECT id FROM addresses
	 WHERE  address = $1 AND tx_vin_vout_row_id = $5
	 LIMIT  1;`

	// SelectSpendingTxsByPrevTx = `SELECT id, tx_hash, tx_index, prev_tx_index FROM vins WHERE prev_tx_hash=$1;`
	// SelectSpendingTxByPrevOut = `SELECT id, tx_hash, tx_index FROM vins WHERE prev_tx_hash=$1 AND prev_tx_index=$2;`
	// SelectFundingTxsByTx      = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1;`
	// SelectFundingTxByTxIn     = `SELECT id, prev_tx_hash FROM vins WHERE tx_hash=$1 AND tx_index=$2;`

	CreateAddressTable = `CREATE TABLE IF NOT EXISTS addresses (
		id SERIAL8 PRIMARY KEY,
		address TEXT,
		tx_hash TEXT,
		in_out_row_id INT8,
		value INT8,
		block_time INT8 NOT NULL,
		is_funding BOOLEAN,
		tx_vin_vout_index INT8,
		tx_vin_vout_row_id INT8
	);`

	SelectAddressAllByAddress = `SELECT * FROM addresses WHERE address=$1 order by block_time desc;`
	SelectAddressRecvCount    = `SELECT COUNT(*) FROM addresses WHERE address=$1;`
	SelectAddressesAllTxn     = `WITH these as (SELECT funding_tx_hash as tx_hash, ftxd.time as tx_time, ftxd.block_height as height
		from addresses left join transactions as ftxd on funding_tx_row_id=ftxd.id
		where address = ANY($1)
		UNION
		SELECT DISTINCT spending_tx_hash as tx_hash, stxd.time as tx_time, stxd.block_height as height from addresses  
		left join transactions as stxd on spending_tx_hash=stxd.tx_hash  
		where address = ANY($1) and spending_tx_hash IS NOT NULL) select tx_hash, height from these order by tx_time desc;`

	SelectAddressesTxnByFundingTx = `SELECT funding_tx_vout_index, spending_tx_hash, spending_tx_vin_index, 
		block_height FROM addresses LEFT JOIN 
		transactions on transactions.tx_hash=spending_tx_hash WHERE 
		address = ANY ($1) and funding_tx_hash=$2;`

	SelectAddressUnspentCountAndValue = `SELECT COUNT(*), SUM(value) FROM addresses WHERE address=$1 and is_funding = FALSE;`
	SelectAddressSpentCountAndValue   = `SELECT COUNT(*), SUM(value) FROM addresses WHERE address=$1 and is_funding = TRUE;`

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
									JOIN vouts on addresses.tx_hash = vouts.tx_hash 
									and addresses.tx_vin_vout_index=vouts.tx_index
									WHERE 
									addresses.address=$1 
									AND 
									addresses.is_funding = FALSE order by addresses.block_time desc`

	SelectAddressLimitNByAddress = `SELECT * FROM addresses WHERE address=$1 order by block_time desc limit $2 offset $3;`

	SelectAddressLimitNByAddressSubQry = `WITH these as (SELECT * FROM addresses WHERE address=$1)
		SELECT * FROM these order by block_time desc limit $2 offset $3;`

	SelectAddressDebitsLimitNByAddress = `SELECT id, address, in_out_row_id, tx_hash, tx_vin_vout_index, block_time, tx_vin_vout_row_id, value
	FROM addresses
	WHERE address=$1 and is_funding = FALSE
	ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressCreditsLimitNByAddress = `SELECT id, in_out_row_id, tx_hash, tx_vin_vout_index, block_time, tx_vin_vout_row_id, value
	FROM addresses
	WHERE address=$1 and is_funding = TRUE
	ORDER BY block_time DESC LIMIT $2 OFFSET $3;`

	SelectAddressIDsByFundingOutpoint = `SELECT id, address FROM addresses WHERE tx_hash=$1, tx_vin_vout_index=$2 and is_funding = TRUE;`

	SelectAddressIDByVoutIDAddress = `SELECT id FROM addresses WHERE address=$1 and tx_vin_vout_row_id=$2 ORDER BY block_time DESC;`

	SetAddressSpendingForID = `UPDATE addresses SET in_out_row_id = $2, tx_hash = $3, tx_vin_vout_index = $4, tx_vin_vout_row_id = $5 WHERE id=$1;`

	SetAddressSpendingForOutpoint = `UPDATE addresses SET in_out_row_id = $3, 
		tx_hash = $4, tx_vin_vout_index = $5, tx_vin_vout_row_id = $6 
		WHERE is_funding = TRUE, tx_hash=$1 and tx_vin_vout_index=$2;`

	IndexBlockTimeOnTableAddress = `CREATE INDEX block_time_index ON addresses (block_time);`

	IndexAddressTableOnAddress = `CREATE INDEX uix_addresses_address
		ON addresses(address);`
	DeindexAddressTableOnAddress = `DROP INDEX uix_addresses_address;`

	IndexAddressTableOnVoutID = `CREATE UNIQUE INDEX uix_addresses_vout_id
		ON addresses(vout_row_id, address);`
	DeindexAddressTableOnVoutID = `DROP INDEX uix_addresses_vout_id;`

	IndexAddressTableOnFundingTx = `CREATE INDEX uix_addresses_funding_tx
		ON addresses(funding_tx_hash, funding_tx_vout_index);`
	DeindexAddressTableOnFundingTx = `DROP INDEX uix_addresses_funding_tx;`
)
