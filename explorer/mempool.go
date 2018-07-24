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
	// Get the initial best block hash and time
	lastBlockHash, _, lastBlockTime := exp.storeMempoolInfo()

	// Process new transactions as they arrive
	for {
		ntx, ok := <-txChan
		if !ok {
			log.Infof("New Tx channel closed")
			return
		}

		// A nil tx is the signal to stop
		if ntx == nil {
			return
		}

		// A tx with an empty hex is the new block signal
		if ntx.Hex == "" {
			lastBlockHash, _, lastBlockTime = exp.storeMempoolInfo()
			exp.wsHub.HubRelay <- sigMempoolUpdate
			continue
		}

		// Ignore this tx if it was received before the last block
		if ntx.Time < lastBlockTime {
			continue
		}

		msgTx, err := txhelpers.MsgTxFromHex(ntx.Hex)
		if err != nil {
			log.Debugf("Failed to decode transaction: %v", err)
			continue
		}

		hash := msgTx.TxHash().String()

		// If this is a vote, decode vote bits
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
					Version:      version,
					Bits:         bits,
					Choices:      choices,
					TicketSpent:  msgTx.TxIn[1].PreviousOutPoint.Hash.String(),
					ForLastBlock: voteForLastBlock(lastBlockHash, validation.Hash.String()),
				}
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
			exp.MempoolData.Tickets = append([]MempoolTx{tx}, exp.MempoolData.Tickets...)
			exp.MempoolData.NumTickets++
		case "Vote":
			addVoteIndex(tx.VoteInfo, exp.MempoolData.TicketIndexes)
			exp.MempoolData.Votes = append([]MempoolTx{tx}, exp.MempoolData.Votes...)
			sort.Sort(byHeight(exp.MempoolData.Votes))
			exp.MempoolData.NumVotes++
			// Multiple transactions can be in mempool from the same ticket.  We
			// need to insure we do not double count these votes.
			if tx.VoteInfo.ForLastBlock && !exp.MempoolData.VotingInfo.voted[tx.VoteInfo.TicketSpent] {
				exp.MempoolData.VotingInfo.voted[tx.VoteInfo.TicketSpent] = true
				exp.MempoolData.VotingInfo.TotalCollected++
				if tx.VoteInfo.Validation.Validity {
					exp.MempoolData.VotingInfo.Valids++
				} else {
					exp.MempoolData.VotingInfo.Invalids++
				}
				updateBlockValidity(&exp.MempoolData.VotingInfo)
			}
		case "Regular":
			exp.MempoolData.Transactions = append([]MempoolTx{tx}, exp.MempoolData.Transactions...)
			exp.MempoolData.NumRegular++
		case "Revocation":
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

	defer func(start time.Time) {
		log.Debugf("storeMempoolInfo() completed in %v",
			time.Since(start))
	}(time.Now())

	memtxs := exp.blockData.GetMempool()
	if memtxs == nil {
		log.Error("Could not get mempool transactions")
		return
	}

	lastBlockHash, lastBlock, lastBlockTime = exp.getLastBlock()

	// RPC succeeded, but mempool is empty
	if len(memtxs) == 0 {
		return
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
	var votingInfo VotingInfo

	lastBlockHash, lastBlock, lastBlockTime = exp.getLastBlock()

	votingInfo.TotalNeeded = exp.ChainParams.TicketsPerBlock
	votingInfo.Required = exp.ChainParams.StakeRewardProportion

	votingInfo.voted = make(map[string]bool)

	txindexes := make(map[int64]map[string]int)
	for _, tx := range memtxs {
		switch tx.Type {
		case "Ticket":
			tickets = append(tickets, tx)
		case "Vote":
			addVoteIndex(tx.VoteInfo, txindexes)
			tx.VoteInfo.ForLastBlock = voteForLastBlock(lastBlockHash, tx.VoteInfo.Validation.Hash)
			if tx.VoteInfo.ForLastBlock && !votingInfo.voted[tx.VoteInfo.TicketSpent] {
				votingInfo.voted[tx.VoteInfo.TicketSpent] = true
				votingInfo.TotalCollected++
				if tx.VoteInfo.Validation.Validity {
					votingInfo.Valids++
				} else {
					votingInfo.Invalids++
				}
			}
			votes = append(votes, tx)
		case "Revocation":
			revs = append(revs, tx)
		default:
			regular = append(regular, tx)
		}
		totalOut += tx.TotalOut
		totalSize += tx.Size
	}
	updateBlockValidity(&votingInfo)
	exp.MempoolData.Lock()
	defer exp.MempoolData.Unlock()

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
		TicketIndexes:      txindexes,
		VotingInfo:         votingInfo,
	}
	return
}

// addVoteIndex assigns a unique index for a vote in a block if an index has not
// already been assigned.  This index is used for sorting in views and counting
// total unique votes for a block.
func addVoteIndex(v *VoteInfo, txindexes map[int64]map[string]int) {
	if idxs, ok := txindexes[v.Validation.Height]; ok {
		if idx, ok := idxs[v.TicketSpent]; ok {
			v.MempoolTicketIndex = idx
		} else {
			idx := len(idxs) + 1
			idxs[v.TicketSpent] = idx
			v.MempoolTicketIndex = idx
		}
	} else {
		idxs := make(map[string]int)
		idxs[v.TicketSpent] = 1
		txindexes[v.Validation.Height] = idxs
		v.MempoolTicketIndex = 1
	}
}

func voteForLastBlock(blockHash, validationHash string) bool {
	return blockHash == validationHash && blockHash != ""
}

func updateBlockValidity(votingInfo *VotingInfo) {
	votingInfo.BlockValid = votingInfo.Valids+votingInfo.Invalids >= votingInfo.TotalNeeded && votingInfo.Valids >= votingInfo.Required
}

// getLastBlock returns the last block hash, height and time
func (exp *explorerUI) getLastBlock() (lastBlockHash string, lastBlock int64, lastBlockTime int64) {
	exp.NewBlockDataMtx.RLock()
	lastBlock = exp.NewBlockData.Height
	lastBlockTime = exp.NewBlockData.BlockTime
	exp.NewBlockDataMtx.RUnlock()
	lastBlockHash, err := exp.blockData.GetBlockHash(lastBlock)
	if err != nil {
		log.Warnf("Could not get bloch hash for last block")
	}
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
