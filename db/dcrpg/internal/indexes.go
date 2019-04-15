package internal

const (
	// blocks table

	IndexOfBlocksTableOnHash   = "uix_block_hash"
	IndexOfBlocksTableOnHeight = "uix_block_height"

	// transactions table

	IndexOfTransactionsTableOnHashes   = "uix_tx_hashes"
	IndexOfTransactionsTableOnBlockInd = "uix_tx_block_in"

	// vins table

	IndexOfVinsTableOnVin     = "uix_vin"
	IndexOfVinsTableOnPrevOut = "uix_vin_prevout"

	// vouts table

	IndexOfVoutsTableOnTxHashInd = "uix_vout_txhash_ind"

	// addresses table

	IndexOfAddressTableOnAddress    = "uix_addresses_address"
	IndexOfAddressTableOnVoutID     = "uix_addresses_vout_id"
	IndexOfAddressTableOnBlockTime  = "block_time_index"
	IndexOfAddressTableOnTx         = "uix_addresses_funding_tx"
	IndexOfAddressTableOnMatchingTx = "matching_tx_hash_index"

	// tickets table

	IndexOfTicketsTableOnHashes     = "uix_ticket_hashes_index"
	IndexOfTicketsTableOnTxRowID    = "uix_ticket_ticket_db_id"
	IndexOfTicketsTableOnPoolStatus = "uix_tickets_pool_status"

	// votes table

	IndexOfVotesTableOnHashes    = "uix_votes_hashes_index"
	IndexOfVotesTableOnBlockHash = "uix_votes_block_hash"
	IndexOfVotesTableOnCandBlock = "uix_votes_candidate_block"
	IndexOfVotesTableOnVersion   = "uix_votes_vote_version"
	IndexOfVotesTableOnHeight    = "uix_votes_height"
	IndexOfVotesTableOnBlockTime = "uix_votes_block_time"

	// misses table

	IndexOfMissesTableOnHashes = "uix_misses_hashes_index"

	// agendas table

	IndexOfAgendasTableOnName = "uix_agendas_name"

	// agenda_votes table

	IndexOfAgendaVotesTableOnRowIDs = "uix_agenda_votes"

	// proposals table

	IndexOfProposalsTableOnToken = "uix_proposals"

	// proposal votes table

	IndexOfProposalVotesTableOnProposalsID = "uix_proposal_votes"
)

// AddressesIndexNames are the names of the indexes on the addresses table.
var AddressesIndexNames = []string{IndexOfAddressTableOnAddress,
	IndexOfAddressTableOnVoutID, IndexOfAddressTableOnBlockTime,
	IndexOfAddressTableOnTx, IndexOfAddressTableOnMatchingTx}

// IndexDescriptions relate table index names to descriptions of the indexes.
var IndexDescriptions = map[string]string{
	IndexOfBlocksTableOnHash:               "blocks on hash",
	IndexOfBlocksTableOnHeight:             "blocks on height",
	IndexOfTransactionsTableOnHashes:       "transactions on block hash and transaction hash",
	IndexOfTransactionsTableOnBlockInd:     "transactions on block hash and block index",
	IndexOfVinsTableOnVin:                  "vins on transaction hash and index",
	IndexOfVinsTableOnPrevOut:              "vins on previous outpoint",
	IndexOfVoutsTableOnTxHashInd:           "vouts on transaction hash and index",
	IndexOfAddressTableOnAddress:           "addresses table on address",
	IndexOfAddressTableOnVoutID:            "addresses table on vout row id, address, and is_funding",
	IndexOfAddressTableOnBlockTime:         "addresses table on block time",
	IndexOfAddressTableOnTx:                "addresses table on transaction hash",
	IndexOfAddressTableOnMatchingTx:        "addresses table on matching tx hash",
	IndexOfTicketsTableOnHashes:            "tickets table on block hash and transaction hash",
	IndexOfTicketsTableOnTxRowID:           "tickets table on transactions table row ID",
	IndexOfTicketsTableOnPoolStatus:        "tickets table on pool status",
	IndexOfVotesTableOnHashes:              "votes table on block hash and transaction hash",
	IndexOfVotesTableOnBlockHash:           "votes table on block hash",
	IndexOfVotesTableOnCandBlock:           "votes table on candidate block",
	IndexOfVotesTableOnVersion:             "votes table on vote version",
	IndexOfVotesTableOnHeight:              "votes table on height",
	IndexOfVotesTableOnBlockTime:           "votes table on block time",
	IndexOfMissesTableOnHashes:             "misses on ticket hash and block hash",
	IndexOfAgendasTableOnName:              "agendas on agenda name",
	IndexOfAgendaVotesTableOnRowIDs:        "agenda_votes on votes table row ID and agendas table row ID",
	IndexOfProposalsTableOnToken:           "proposals on token and time",
	IndexOfProposalVotesTableOnProposalsID: "proposal_votes on proposals row ID",
}
