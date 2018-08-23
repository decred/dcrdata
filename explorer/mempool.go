// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"sort"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrdata/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

// NumLatestMempoolTxns is the maximum number of mempool transactions that will
// be stored in MempoolData.LatestTransactions.
const NumLatestMempoolTxns = 5

func (exp *explorerUI) mempoolMonitor(txChan chan *NewMempoolTx) {
	// Get the initial best block hash, height, and time.
	lastBlockHash, lastBlockHeight, lastBlockTime := exp.storeMempoolInfo()

	// Process new transactions as they arrive.
	for {
		ntx, ok := <-txChan
		if !ok {
			log.Infof("New Tx channel closed")
			return
		}

		// A nil tx is the signal to stop.
		if ntx == nil {
			return
		}

		// A tx with an empty hex is the new block signal.
		if ntx.Hex == "" {
			lastBlockHash, lastBlockHeight, lastBlockTime = exp.storeMempoolInfo()
			exp.wsHub.HubRelay <- sigMempoolUpdate
			continue
		}

		// Ignore this tx if it was received before the last block.
		if ntx.Time < lastBlockTime {
			continue
		}

		msgTx, err := txhelpers.MsgTxFromHex(ntx.Hex)
		if err != nil {
			log.Debugf("Failed to decode transaction: %v", err)
			continue
		}

		hash := msgTx.TxHash().String()

		// If this is a vote, decode vote bits.
		var voteInfo *VoteInfo
		if ok := stake.IsSSGen(msgTx); ok {
			validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(msgTx, exp.ChainParams)
			if err != nil {
				log.Debugf("Cannot get vote choices for %s", hash)
			} else {
				voteInfo = &VoteInfo{
					Validation: BlockValidation{
						Hash:     validation.Hash.String(),
						Height:   validation.Height,
						Validity: validation.Validity,
					},
					Version:     version,
					Bits:        bits,
					Choices:     choices,
					TicketSpent: msgTx.TxIn[1].PreviousOutPoint.Hash.String(),
				}
				voteInfo.ForLastBlock = voteInfo.VotesOnBlock(lastBlockHash)
			}
		}

		tx := MempoolTx{
			Hash:     hash,
			Time:     ntx.Time,
			Size:     int32(len(ntx.Hex) / 2),
			TotalOut: txhelpers.TotalOutFromMsgTx(msgTx).ToCoin(),
			Type:     txhelpers.DetermineTxTypeString(msgTx),
			VoteInfo: voteInfo,
		}

		// Add the tx to the appropriate tx slice in MempoolData and update the
		// count for the transaction type.
		exp.MempoolData.Lock()
		switch tx.Type {
		case "Ticket":
			if _, found := exp.MempoolData.InvStake[tx.Hash]; found {
				exp.MempoolData.Unlock()
				log.Debugf("Not broadcasting duplicate ticket notification: %s", hash)
				continue // back to waiting for new tx signal
			}
			exp.MempoolData.InvStake[tx.Hash] = struct{}{}
			exp.MempoolData.Tickets = append([]MempoolTx{tx}, exp.MempoolData.Tickets...)
			exp.MempoolData.NumTickets++
		case "Vote":
			// Votes on the next block may be recieve just prior to dcrdata
			// actually processing the new block. Do not broadcast these ahead
			// of the full update with the new block signal as the vote will be
			// included in that update.
			if tx.VoteInfo.Validation.Height > lastBlockHeight {
				exp.MempoolData.Unlock()
				log.Debug("Got a vote for a future block. Waiting to pull it "+
					"out of mempool with new block signal. Vote: ", tx.Hash)
				continue
			}

			// Maintain the list of unique stake txns encountered.
			if _, found := exp.MempoolData.InvStake[tx.Hash]; found {
				exp.MempoolData.Unlock()
				log.Debugf("Not broadcasting duplicate vote notification: %s", hash)
				continue // back to waiting for new tx signal
			}
			exp.MempoolData.InvStake[tx.Hash] = struct{}{}
			exp.MempoolData.Votes = append([]MempoolTx{tx}, exp.MempoolData.Votes...)
			//sort.Sort(byHeight(exp.MempoolData.Votes))
			exp.MempoolData.NumVotes++

			// Multiple transactions can be in mempool from the same ticket.  We
			// need to insure we do not double count these votes.
			tx.VoteInfo.setTicketIndex(exp.MempoolData.TicketIndexes)
			votingInfo := &exp.MempoolData.VotingInfo
			if tx.VoteInfo.ForLastBlock && !votingInfo.votedTickets[tx.VoteInfo.TicketSpent] {
				votingInfo.votedTickets[tx.VoteInfo.TicketSpent] = true
				votingInfo.TicketsVoted++
			}
		case "Regular":
			// Maintain the list of unique regular txns encountered.
			if _, found := exp.MempoolData.InvRegular[tx.Hash]; found {
				exp.MempoolData.Unlock()
				log.Debugf("Not broadcasting duplicate txns notification: %s", hash)
				continue // back to waiting for new tx signal
			}
			exp.MempoolData.InvRegular[tx.Hash] = struct{}{}
			exp.MempoolData.Transactions = append([]MempoolTx{tx}, exp.MempoolData.Transactions...)
			exp.MempoolData.NumRegular++
		case "Revocation":
			// Maintain the list of unique stake txns encountered.
			if _, found := exp.MempoolData.InvStake[tx.Hash]; found {
				exp.MempoolData.Unlock()
				log.Debugf("Not broadcasting duplicate revocation notification: %s", hash)
				continue // back to waiting for new tx signal
			}
			exp.MempoolData.InvStake[tx.Hash] = struct{}{}
			exp.MempoolData.Revocations = append([]MempoolTx{tx}, exp.MempoolData.Revocations...)
			exp.MempoolData.NumRevokes++
		}

		// Update latest transactions, popping the oldest transaction off the
		// back if necessary to limit to NumLatestMempoolTxns.
		numLatest := len(exp.MempoolData.LatestTransactions)
		if numLatest >= NumLatestMempoolTxns {
			exp.MempoolData.LatestTransactions = append([]MempoolTx{tx},
				exp.MempoolData.LatestTransactions[:numLatest-1]...)
		} else {
			exp.MempoolData.LatestTransactions = append([]MempoolTx{tx},
				exp.MempoolData.LatestTransactions...)
		}

		// Store totals
		exp.MempoolData.NumAll++
		exp.MempoolData.TotalOut += tx.TotalOut
		exp.MempoolData.TotalSize += tx.Size
		exp.MempoolData.FormattedTotalSize = humanize.Bytes(uint64(exp.MempoolData.TotalSize))
		exp.MempoolData.Unlock()

		// Broadcast the new transaction
		exp.wsHub.HubRelay <- sigNewTx
		exp.wsHub.NewTxChan <- &tx
	}
}

func (exp *explorerUI) StopMempoolMonitor(txChan chan *NewMempoolTx) {
	log.Infof("Stopping mempool monitor")
	txChan <- nil
}

func (exp *explorerUI) StartMempoolMonitor(newTxChan chan *NewMempoolTx) {
	go exp.mempoolMonitor(newTxChan)
}

func (exp *explorerUI) storeMempoolInfo() (lastBlockHash string, lastBlock int64, lastBlockTime int64) {

	// Store mempool data for template rendering
	exp.MempoolData.Lock()
	defer exp.MempoolData.Unlock()

	defer func(start time.Time) {
		log.Debugf("storeMempoolInfo() completed in %v", time.Since(start))
	}(time.Now())

	// Get mempool transactions and ensure block ID is correct
	var memtxs []MempoolTx
	for {
		lastBlockHash0, _, _ := exp.getLastBlock()
		memtxs = exp.blockData.GetMempool()
		if memtxs == nil {
			log.Error("Could not get mempool transactions")
			return
		}
		lastBlockHash, lastBlock, lastBlockTime = exp.getLastBlock()
		if lastBlockHash == lastBlockHash0 {
			break
		}
		log.Warnf("Best block change while getting mempool. Repeating request.")
	}

	// Get the NumLatestMempoolTxns latest transactions in mempool
	var latest []MempoolTx
	sort.Sort(byTime(memtxs))
	if len(memtxs) > NumLatestMempoolTxns {
		latest = memtxs[:NumLatestMempoolTxns]
	} else {
		latest = memtxs
	}

	tickets := make([]MempoolTx, 0)
	votes := make([]MempoolTx, 0)
	revs := make([]MempoolTx, 0)
	regular := make([]MempoolTx, 0)

	var totalOut float64
	var totalSize int32
	votingInfo := VotingInfo{
		MaxVotesPerBlock: exp.ChainParams.TicketsPerBlock,
		votedTickets:     make(map[string]bool),
	}
	invRegular := make(map[string]struct{})
	invStake := make(map[string]struct{})

	// Initialize the BlockValidatorIndex, a map.
	ticketSpendInds := make(BlockValidatorIndex)
	for _, tx := range memtxs {
		switch tx.Type {
		case "Ticket":
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			invStake[tx.Hash] = struct{}{}
			tickets = append(tickets, tx)
		case "Vote":
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			invStake[tx.Hash] = struct{}{}
			votes = append(votes, tx)

			// Assign an index to this vote that is unique to the spent ticket +
			// validated block.
			tx.VoteInfo.setTicketIndex(ticketSpendInds)
			// Determine if this vote is (in)validating the previous block.
			tx.VoteInfo.ForLastBlock = tx.VoteInfo.VotesOnBlock(lastBlockHash)
			// Update tally if this is for the previous block, the ticket has not
			// yet been spent. Do not attempt to decide block validity.
			if tx.VoteInfo.ForLastBlock && !votingInfo.votedTickets[tx.VoteInfo.TicketSpent] {
				votingInfo.votedTickets[tx.VoteInfo.TicketSpent] = true
				votingInfo.TicketsVoted++
			}
		case "Revocation":
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			invStake[tx.Hash] = struct{}{}
			revs = append(revs, tx)
		default:
			if _, found := invRegular[tx.Hash]; found {
				continue
			}
			invRegular[tx.Hash] = struct{}{}
			regular = append(regular, tx)
		}

		// Update mempool totals
		totalOut += tx.TotalOut
		totalSize += tx.Size
	}

	sort.Sort(byHeight(votes))

	exp.MempoolData.Transactions = regular
	exp.MempoolData.Tickets = tickets
	exp.MempoolData.Revocations = revs
	exp.MempoolData.Votes = votes

	exp.MempoolData.MempoolShort = MempoolShort{
		LastBlockHeight:    lastBlock,
		LastBlockHash:      lastBlockHash,
		LastBlockTime:      lastBlockTime,
		TotalOut:           totalOut,
		TotalSize:          totalSize,
		NumAll:             len(memtxs),
		NumTickets:         len(tickets),
		NumVotes:           len(votes),
		NumRegular:         len(regular),
		NumRevokes:         len(revs),
		LatestTransactions: latest,
		FormattedTotalSize: humanize.Bytes(uint64(totalSize)),
		TicketIndexes:      ticketSpendInds,
		VotingInfo:         votingInfo,
		InvRegular:         invRegular,
		InvStake:           invStake,
	}
	return
}

// setTicketIndex assigns the VoteInfo an index based on the block that the vote
// is (in)validating and the spent ticket hash. The ticketSpendInds tracks
// known combinations of target block and spent ticket hash. This index is used
// for sorting in views and counting total unique votes for a block.
func (v *VoteInfo) setTicketIndex(ticketSpendInds BlockValidatorIndex) {
	// One-based indexing
	startInd := 1
	// Reference the sub-index for the block being (in)validated by this vote.
	if idxs, ok := ticketSpendInds[v.Validation.Hash]; ok {
		// If this ticket has been seen before voting on this block, set the
		// known index. Otherwise, assign the next index in the series.
		if idx, ok := idxs[v.TicketSpent]; ok {
			v.MempoolTicketIndex = idx
		} else {
			idx := len(idxs) + startInd
			idxs[v.TicketSpent] = idx
			v.MempoolTicketIndex = idx
		}
	} else {
		// First vote encountered for this block. Create new ticket sub-index.
		ticketSpendInds[v.Validation.Hash] = TicketIndex{
			v.TicketSpent: startInd,
		}
		v.MempoolTicketIndex = startInd
	}
}

// VotesOnBlock indicates if the vote is voting on the validity of block
// specified by the given hash.
func (v *VoteInfo) VotesOnBlock(blockHash string) bool {
	return v.Validation.ForBlock(blockHash)
}

// ForBlock indicates if the validation choice is for the specified block.
func (v *BlockValidation) ForBlock(blockHash string) bool {
	return blockHash != "" && blockHash == v.Hash
}

// getLastBlock returns the last block hash, height and time
func (exp *explorerUI) getLastBlock() (lastBlockHash string, lastBlock int64, lastBlockTime int64) {
	exp.NewBlockDataMtx.RLock()
	lastBlock = exp.NewBlockData.Height
	lastBlockTime = exp.NewBlockData.BlockTime
	lastBlockHash = exp.NewBlockData.Hash
	exp.NewBlockDataMtx.RUnlock()
	return
}

type byTime []MempoolTx

func (txs byTime) Less(i, j int) bool {
	return txs[i].Time > txs[j].Time
}

func (txs byTime) Len() int {
	return len(txs)
}

func (txs byTime) Swap(i, j int) {
	txs[i], txs[j] = txs[j], txs[i]
}

type byHeight []MempoolTx

func (votes byHeight) Less(i, j int) bool {
	if votes[i].VoteInfo.Validation.Height == votes[j].VoteInfo.Validation.Height {
		return votes[i].VoteInfo.MempoolTicketIndex < votes[j].VoteInfo.MempoolTicketIndex
	}
	return votes[i].VoteInfo.Validation.Height > votes[j].VoteInfo.Validation.Height
}

func (votes byHeight) Len() int {
	return len(votes)
}

func (votes byHeight) Swap(i, j int) {
	votes[i], votes[j] = votes[j], votes[i]
}
