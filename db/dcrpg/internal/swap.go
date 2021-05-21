package internal

const (
	CreateAtomicSwapTableV0 = `CREATE TABLE IF NOT EXISTS swaps (
		contract_tx TEXT,
		contract_vout INT4,
		spend_tx TEXT,
		spend_vin INT4,
		spend_height INT8,
		p2sh_addr TEXT,
		value INT8,
		secret_hash BYTEA,
		secret BYTEA,        -- NULL for refund
		lock_time INT8,
		CONSTRAINT spend_tx_in PRIMARY KEY (spend_tx, spend_vin)
	);`

	CreateAtomicSwapTable = CreateAtomicSwapTableV0

	InsertContractSpend = `INSERT INTO swaps (contract_tx, contract_vout, spend_tx, spend_vin, spend_height,
		p2sh_addr, value, secret_hash, secret, lock_time)
	VALUES ($1, $2, $3, $4, $5,
		$6, $7, $8, $9, $10) 
	ON CONFLICT (spend_tx, spend_vin)
		DO UPDATE SET spend_height = $5;`

	IndexSwapsOnHeightV0 = `CREATE INDEX idx_swaps_height ON swaps (spend_height);`
	IndexSwapsOnHeight   = IndexSwapsOnHeightV0
	DeindexSwapsOnHeight = `DROP INDEX idx_swaps_height;`
)
