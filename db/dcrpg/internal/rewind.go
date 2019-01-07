package internal

const (
	// address row deletion by block hash

	// DeleteAddresses deletes rows of the addresses table (funding and
	// spending) corresponding to all of the transactions (regular and stake)
	// for a given block.
	DeleteAddresses = `DELETE FROM addresses
		USING transactions, blocks
		WHERE (
				(addresses.tx_vin_vout_row_id=ANY(transactions.vin_db_ids) AND addresses.is_funding=false)
				OR
				(addresses.tx_vin_vout_row_id=ANY(transactions.vout_DB_IDs) AND addresses.is_funding=true)
			)
			AND transactions.id = ANY(array_cat(blocks.txdbids, blocks.stxdbids))
			AND blocks.hash=$1;`

	DeleteStakeAddressesFunding = `DELETE FROM addresses
		USING transactions, blocks
		WHERE addresses.tx_vin_vout_row_id=ANY(transactions.vin_db_ids)
			AND addresses.is_funding=false
			AND transactions.id = ANY(blocks.stxdbids)
			AND blocks.hash=$1;`

	DeleteStakeAddressesSpending = `DELETE FROM addresses
		USING transactions, blocks
		WHERE addresses.tx_vin_vout_row_id=ANY(transactions.vout_DB_IDs)
			AND addresses.is_funding=true
			AND transactions.id = ANY(blocks.stxdbids)
			AND blocks.hash=$1;`

	// vin row deletion by block hash

	DeleteVins = `DELETE FROM vins
		USING transactions, blocks
		WHERE vins.id=ANY(transactions.vin_db_ids)
			AND transactions.id = ANY(array_cat(blocks.txdbids,blocks.stxdbids))
			AND blocks.hash=$1;`

	// DeleteStakeVins deletes rows of the vins table corresponding to inputs of
	// the stake transactions (transactions.vin_db_ids) for a block
	// (blocks.stxdbids) specified by its hash (blocks.hash).
	DeleteStakeVins = `DELETE FROM vins
		USING transactions, blocks
		WHERE vins.id=ANY(transactions.vin_db_ids)
			AND transactions.id = ANY(blocks.stxdbids)
			AND blocks.hash=$1;`
	// DeleteStakeVinsSubSelect is like DeleteStakeVins except it is implemented
	// using sub-queries rather than a join.
	DeleteStakeVinsSubSelect = `DELETE FROM vins
		WHERE id IN (
			SELECT UNNEST(vin_db_ids)
			FROM transactions
			WHERE id IN (
				SELECT UNNEST(stxdbids)
				FROM blocks
				WHERE hash=$1
				)
			);`

	// DeleteRegularVins deletes rows of the vins table corresponding to inputs
	// of the regular/non-stake transactions (transactions.vin_db_ids) for a
	// block (blocks.txdbids) specified by its hash (blocks.hash).
	DeleteRegularVins = `DELETE FROM vins
		USING transactions, blocks
		WHERE vins.id=ANY(transactions.vin_db_ids)
			AND transactions.id = ANY(blocks.txdbids)
			AND blocks.hash=$1;`
	// DeleteRegularVinsSubSelect is like DeleteRegularVins except it is
	// implemented using sub-queries rather than a join.
	DeleteRegularVinsSubSelect = `DELETE FROM vins
		WHERE id IN (
			SELECT UNNEST(vin_db_ids)
			FROM transactions
			WHERE id IN (
				SELECT UNNEST(txdbids)
				FROM blocks
				WHERE hash=$1
				)
			);`

	// vout row deletion by block hash

	DeleteVouts = `DELETE FROM vouts
		USING transactions, blocks
		WHERE vouts.id=ANY(transactions.vout_db_ids)
			AND transactions.id = ANY(array_cat(blocks.txdbids,blocks.stxdbids))
			AND blocks.hash=$1;`

	// DeleteStakeVouts deletes rows of the vouts table corresponding to inputs
	// of the stake transactions (transactions.vout_db_ids) for a block
	// (blocks.stxdbids) specified by its hash (blocks.hash).
	DeleteStakeVouts = `DELETE FROM vouts
		USING transactions, blocks
		WHERE vouts.id=ANY(transactions.vout_db_ids)
			AND transactions.id = ANY(blocks.stxdbids)
			AND blocks.hash=$1;`
	// DeleteStakeVoutsSubSelect is like DeleteStakeVouts except it is
	// implemented using sub-queries rather than a join.
	DeleteStakeVoutsSubSelect = `DELETE FROM vouts
		WHERE id IN (
			SELECT UNNEST(vout_db_ids)
			FROM transactions
			WHERE id IN (
				SELECT UNNEST(stxdbids)
				FROM blocks
				WHERE hash=$1
				)
			);`

	// DeleteRegularVouts deletes rows of the vouts table corresponding to
	// inputs of the regular/non-stake transactions (transactions.vout_db_ids)
	// for a block (blocks.txdbids) specified by its hash (blocks.hash).
	DeleteRegularVouts = `DELETE FROM vouts
		USING transactions, blocks
		WHERE vouts.id=ANY(transactions.vout_db_ids)
			AND transactions.id = ANY(blocks.txdbids)
			AND blocks.hash=$1;`
	// DeleteRegularVoutsSubSelect is like DeleteRegularVouts except it is
	// implemented using sub-queries rather than a join.
	DeleteRegularVoutsSubSelect = `DELETE FROM vouts
		WHERE id IN (
			SELECT UNNEST(vout_db_ids)
			FROM transactions
			WHERE id IN (
				SELECT UNNEST(txdbids)
				FROM blocks
				WHERE hash=$1
				)
			);`

	DeleteMisses = `DELETE FROM misses
		WHERE block_hash=$1;`

	DeleteVotes = `DELETE FROM votes
		WHERE block_hash=$1;`

	DeleteTickets = `DELETE FROM tickets
		USING blocks
		WHERE purchase_tx_db_id = ANY(blocks.stxdbids)
			AND blocks.hash=$1;`
	DeleteTicketsSimple = `DELETE FROM tickets
		WHERE block_hash=$1;`

	DeleteTransactions = `DELETE FROM transactions
		USING blocks
		WHERE transactions.id = ANY(array_cat(blocks.txdbids, blocks.stxdbids))
		AND blocks.hash=$1;`

	DeleteBlock = `DELETE FROM blocks
		WHERE hash=$1;`

	DeleteBlockFromChain = `DELETE FROM block_chain
		WHERE this_hash=$1
		RETURNING prev_hash;`

	ClearBlockChainNextHash = `UPDATE block_chain
		SET next_hash=''
		WHERE next_hash=$1;`
)
