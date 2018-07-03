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
			if !voteForLastBlock(lastBlockHash, validation.Hash.String()) {
				continue
			}
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
			if idx, ok := exp.MempoolData.TicketIndexes[tx.VoteInfo.TicketSpent]; ok {
				tx.VoteInfo.MempoolTicketIndex = idx
			} else {
				idx := len(exp.MempoolData.TicketIndexes) + 1
				exp.MempoolData.TicketIndexes[tx.VoteInfo.TicketSpent] = idx
				tx.VoteInfo.MempoolTicketIndex = idx
			}
			exp.MempoolData.Votes = append([]MempoolTx{tx}, exp.MempoolData.Votes...)
			exp.MempoolData.NumVotes++
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

	// Categorize the transactions, and bin votes by ticket spent
	txindexes := make(map[string]int)
	for _, tx := range memtxs {
		switch tx.Type {
		case "Ticket":
			tickets = append(tickets, tx)
		case "Vote":
			if !voteForLastBlock(lastBlockHash, tx.VoteInfo.Validation.Hash) {
				continue
			}
			if idx, ok := txindexes[tx.VoteInfo.TicketSpent]; ok {
				tx.VoteInfo.MempoolTicketIndex = idx
			} else {
				idx := len(txindexes) + 1
				txindexes[tx.VoteInfo.TicketSpent] = idx
				tx.VoteInfo.MempoolTicketIndex = idx
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

	// Store the results in MempoolData for the web page
	exp.MempoolData.Lock()
	defer exp.MempoolData.Unlock()

	exp.MempoolData.Transactions = regular
	exp.MempoolData.Tickets = tickets
	exp.MempoolData.Revocations = revs
	exp.MempoolData.Votes = votes

	exp.MempoolData.MempoolShort = MempoolShort{
		LastBlockHeight:    lastBlock,
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
	}
	return
}

func voteForLastBlock(blockHash, validationHash string) bool {
	return blockHash == validationHash && blockHash != ""
}

// getLastBlock returns the last block hash, height and time
func (exp *explorerUI) getLastBlock() (lastBlockHash string, lastBlock int64, lastBlockTime int64) {
	exp.NewBlockDataMtx.RLock()
	lastBlock = exp.NewFullBlockData.Height
	lastBlockTime = exp.NewFullBlockData.BlockTime
	exp.NewBlockDataMtx.RUnlock()
	lastBlockHash, err := exp.blockData.GetBlockHash(lastBlock)
	if err != nil {
		log.Warnf("Could not get block hash for last block")
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
