// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package internal

const (
	// UnmatchedSpending lists addresses with no matching_tx_hash set but with
	// is_funding=FALSE. Spending address rows should always have a matching
	// transaction hash.
	UnmatchedSpending = `SELECT id, address
		FROM addresses
		WHERE char_length(matching_tx_hash)=0
			AND NOT is_funding;`

	// ExtraMainchainBlocks lists mainchain blocks at heights where there are
	// more than one mainchain block (impossible).
	ExtraMainchainBlocks = `WITH mainchain_at_height AS
			(SELECT height, count(*) as num
			FROM blocks
			WHERE is_mainchain
			GROUP BY height)
		SELECT id, blocks.height, blocks.hash
		FROM mainchain_at_height
		JOIN blocks ON blocks.height = mainchain_at_height.height
		WHERE mainchain_at_height.num>1
			AND is_mainchain
		ORDER BY height;`

	// MislabeledInvalidBlocks lists blocks labeled as approved, but for which
	// the following block has specified vote bits that invalidate it.
	MislabeledInvalidBlocks = `WITH disapproved AS
			(SELECT previous_hash as hash
			FROM blocks
			WHERE vote_bits = 0 AND is_mainchain)
		SELECT id, blocks.hash
		FROM blocks
		JOIN disapproved ON blocks.hash = disapproved.hash
		WHERE is_valid;`

	// MainchainBlocksWithSidechainParent lists side chain blocks that are the
	// parent of a main chain block (impossible).
	MainchainBlocksWithSidechainParent = `WITH mainchain_parents AS
			(SELECT hash as child, previous_hash as parent
			FROM blocks
			WHERE is_mainchain)
		SELECT id, mainchain_parents.parent, mainchain_parents.child
		FROM blocks
		JOIN mainchain_parents ON blocks.hash = mainchain_parents.parent
		WHERE NOT blocks.is_mainchain;`

	// UnspentTicketsWithSpendInfo lists tickets that are flagged as unspent,
	// but which have set either spend_height or spend_tx_db_id.
	UnspentTicketsWithSpendInfo = `SELECT id, spend_height, spend_tx_db_id
		FROM tickets
		WHERE spend_type=0
			AND (spend_height IS NOT NULL
				OR spend_tx_db_id IS NOT NULL);`

	// SpentTicketsWithoutSpendInfo lists tickets that are flagged as spent, but
	// which do not have set either spend_height or spend_tx_db_id.
	SpentTicketsWithoutSpendInfo = `SELECT id, spend_height, spend_tx_db_id
		FROM tickets
		WHERE spend_type!=0
			AND (spend_height IS NULL
				OR spend_tx_db_id IS NULL);`

	// MislabeledTicketTransactions lists transactions in the transactions table
	// that also appear in the tickets table, but which do not have the proper
	// tx_type (1) set.
	MislabeledTicketTransactions = `SELECT transactions.id, tx_type, transactions.tx_hash
		FROM transactions
		INNER JOIN tickets ON tickets.tx_hash = transactions.tx_hash
		WHERE tx_type != 1;`

	// MissingTickets lists ticket transactions in the transactions table (with
	// tx_type=1) that do NOT appear in the tickets table.
	MissingTickets = `SELECT transactions.id, tx_type, transactions.tx_hash
		FROM transactions
		LEFT OUTER JOIN tickets ON tickets.tx_hash = transactions.tx_hash
		WHERE tx_type = 1 AND tickets.id IS NULL;`

	// MissingTicketTransactions lists tickets in the tickets table that do not
	// appear in the transactions table at all.
	MissingTicketTransactions = `SELECT tickets.id, tickets.tx_hash
		FROM transactions
		RIGHT OUTER JOIN tickets ON tickets.tx_hash = transactions.tx_hash
		WHERE transactions.id IS NULL;`

	// TicketPoolStatuses lists all combinations of pool_status (live, voted,
	// expired, missed) and spend_type (unspent, voted, revoked).
	//
	//  pool_status | spend_type |  count
	// -------------+------------+---------
	//            0 |          0 |   42388 <- live
	//            1 |          2 | 1675471 <- voted
	//            2 |          0 |      57 <- expired, unspent
	//            2 |          1 |   11346 <- expired, revoked
	//            3 |          0 |    1137 <- missed, unspent
	//            3 |          1 |   25837 <- missed, revoked
	// (6 rows)
	TicketPoolStatuses = `SELECT pool_status, spend_type, count(*)
		FROM tickets
		GROUP BY pool_status, spend_type
		ORDER BY pool_status, spend_type;`
	// alternatively, without the counts...
	// TicketPoolStatuses = `SELECT DISTINCT ON (pool_status, spend_type) pool_status, spend_type
	// 	FROM tickets
	// 	ORDER BY pool_status, spend_type;`

	// BadSpentLiveTickets lists tickets with live pool_status, but not unspent
	// spend_type. There should be none.
	BadSpentLiveTickets = `SELECT id, tx_hash, spend_type
		FROM tickets
		WHERE pool_status=0 AND spend_type!=0;`

	// BadVotedTickets lists tickets with voted pool_status, but not voted
	// spend_type. Voted tickets should not be live, expired, or missed.
	BadVotedTickets = `SELECT id, tx_hash, spend_type
		FROM tickets
		WHERE pool_status=1 AND spend_type!=2;`

	// BadExpiredVotedTickets lists tickets with expired pool_status, but with
	// voted spend_type. Expired tickets should only be unspent or revoked.
	BadExpiredVotedTickets = `SELECT id, tx_hash, spend_type
		FROM tickets
		WHERE pool_status=2 AND spend_type=2;`

	// BadMissedVotedTickets lists tickets with missed pool_status, but with
	// voted spend_type. Missed tickets should only be unspent or revoked.
	BadMissedVotedTickets = `SELECT id, tx_hash, spend_type
		FROM tickets
		WHERE pool_status=3 AND spend_type=2;`

	// DisapprovedBlockVotes lists the number of votes approving and
	// disapproving the blocks that are flagged as not valid (disapproved).
	//
	//                           candidate_hash                          | approvals | disapprovals | total
	// ------------------------------------------------------------------+-----------+--------------+-------
	//  000000000000031e720866f4bd3d4135c5473adc78d3bed22b816dafd55c6dc4 |         2 |            2 |     4
	//  00000000000003f1df8d2ec247864ecc250e53fa4a84e5e9d04b868aae15d4bd |         2 |            2 |     4
	//  00000000000003ae4fa13a6dcd53bf2fddacfac12e86e5b5f98a08a71d3e6caa |         2 |            2 |     4
	//  00000000000008ba8f3d37d27cd45e31906c1cbf45e0de3999fac06ef29b429b |         2 |            3 |     5
	//  0000000000000b5355c8bfb606cd40350e11739d53fb2fc191562c1d6d05b153 |         2 |            2 |     4
	//  0000000000000d7410d10b15b6ec0741cee77682c5e1e9263ca13fd749c47cad |         2 |            3 |     5
	//  000000000000124769cb7f199bcd5543b897374b6e1f6f8866a22ab425e15009 |         2 |            2 |     4
	//  0000000000000eaaa27b96df1228ffc96bcb7e0e476f9180d7d40f886c446e82 |         2 |            2 |     4
	//  000000000000075097acfdf8a3da309919712ebcff28a9e12dd3d9df787565a1 |         2 |            3 |     5
	// (9 rows)
	DisapprovedBlockVotes = `WITH block_votes AS
			(SELECT candidate_block_hash,
					sum(block_valid::INT) AS approvals,
					count(*) AS total
			FROM votes
			GROUP BY candidate_block_hash
			ORDER BY approvals)
		SELECT blocks.hash AS candidate_hash,
			block_votes.approvals,
			block_votes.total-block_votes.approvals as disapprovals,
			block_votes.total
		FROM blocks
		JOIN block_votes ON block_votes.candidate_block_hash = blocks.hash
		WHERE NOT is_valid;`

	// BadBlockApproval lists blocks with an incorrect is_valid flag as
	// determined by computing (approvals/total_votes > 0.5).
	BadBlockApproval = `WITH block_votes AS
			(SELECT candidate_block_hash,
					sum(block_valid::INT) AS approvals,
					count(*) AS total
			FROM votes
			GROUP BY candidate_block_hash
			ORDER BY approvals)
		SELECT blocks.hash AS candidate_hash,
			approvals,
			total-approvals as disapprovals,
			total,
			approvals::FLOAT8/total::FLOAT8 > 0.5 as approved_actual,
			is_valid as approved_set
		FROM blocks
		JOIN block_votes ON block_votes.candidate_block_hash = blocks.hash
		WHERE (NOT is_valid AND approvals::FLOAT8/total::FLOAT8 > 0.5)
		   OR (is_valid AND NOT approvals::FLOAT8/total::FLOAT8 > 0.5);`
)
