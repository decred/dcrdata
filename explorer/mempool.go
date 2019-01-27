// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/v4/explorer/types"
	"github.com/decred/dcrdata/v4/mempool"
	"github.com/decred/dcrdata/v4/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

// NumLatestMempoolTxns is the maximum number of mempool transactions that will
// be stored in MempoolData.LatestTransactions.
const NumLatestMempoolTxns = 5

func (exp *explorerUI) mempoolMonitor(txChan chan *types.NewMempoolTx) {
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
		txType := txhelpers.DetermineTxTypeString(msgTx)

		// Maintain the list of unique stake and regular txns encountered.
		var txExists bool
		if txType == "Regular" {
			_, txExists = exp.MempoolData.InvRegular[hash]
		} else {
			_, txExists = exp.MempoolData.InvStake[hash]
		}

		if txExists {
			log.Tracef("Not broadcasting duplicate %s notification: %s", txType, hash)
			continue // back to waiting for new tx signal
		}

		// If this is a vote, decode vote bits.
		var voteInfo *types.VoteInfo
		if ok := stake.IsSSGen(msgTx); ok {
			validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(msgTx, exp.ChainParams)
			if err != nil {
				log.Debugf("Cannot get vote choices for %s", hash)
			} else {
				voteInfo = &types.VoteInfo{
					Validation: types.BlockValidation{
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

		fee, _ := txhelpers.TxFeeRate(msgTx)

		tx := types.MempoolTx{
			TxID:      msgTx.TxHash().String(),
			Fees:      fee.ToCoin(),
			VinCount:  len(msgTx.TxIn),
			VoutCount: len(msgTx.TxOut),
			Vin:       types.MsgTxMempoolInputs(msgTx),
			Coinbase:  blockchain.IsCoinBaseTx(msgTx),
			Hash:      hash,
			Time:      ntx.Time,
			Size:      int32(len(ntx.Hex) / 2),
			TotalOut:  txhelpers.TotalOutFromMsgTx(msgTx).ToCoin(),
			Type:      txhelpers.DetermineTxTypeString(msgTx),
			VoteInfo:  voteInfo,
		}

		// Maintain a separate total that excludes votes for sidechain blocks and
		// multiple votes that spend the same ticket.
		likely := true

		// Add the tx to the appropriate tx slice in MempoolData and update the
		// count for the transaction type.
		exp.MempoolData.Lock()
		switch tx.Type {
		case "Ticket":
			exp.MempoolData.InvStake[tx.Hash] = struct{}{}
			exp.MempoolData.Tickets = append([]types.MempoolTx{tx}, exp.MempoolData.Tickets...)
			exp.MempoolData.NumTickets++
			exp.MempoolData.TicketTotal += tx.TotalOut
		case "Vote":
			// Votes on the next block may be received just prior to dcrdata
			// actually processing the new block. Do not broadcast these ahead
			// of the full update with the new block signal as the vote will be
			// included in that update.
			if tx.VoteInfo.Validation.Height > lastBlockHeight {
				exp.MempoolData.Unlock()
				log.Trace("Got a vote for a future block. Waiting to pull it "+
					"out of mempool with new block signal. Vote: ", tx.Hash)
				continue
			}
			exp.MempoolData.InvStake[tx.Hash] = struct{}{}
			exp.MempoolData.Votes = append([]types.MempoolTx{tx}, exp.MempoolData.Votes...)
			//sort.Sort(byHeight(exp.MempoolData.Votes))
			exp.MempoolData.NumVotes++

			// Multiple transactions can be in mempool from the same ticket.  We
			// need to insure we do not double count these votes.
			tx.VoteInfo.SetTicketIndex(exp.MempoolData.TicketIndexes)
			votingInfo := &exp.MempoolData.VotingInfo
			if tx.VoteInfo.ForLastBlock && !votingInfo.VotedTickets[tx.VoteInfo.TicketSpent] {
				votingInfo.VotedTickets[tx.VoteInfo.TicketSpent] = true
				votingInfo.TicketsVoted++
				exp.MempoolData.VoteTotal += tx.TotalOut
				exp.MempoolData.VotingInfo.Tally(tx.VoteInfo)
			} else {
				likely = false
			}
		case "Regular":
			exp.MempoolData.InvRegular[tx.Hash] = struct{}{}
			exp.MempoolData.Transactions = append([]types.MempoolTx{tx}, exp.MempoolData.Transactions...)
			exp.MempoolData.NumRegular++
			exp.MempoolData.RegularTotal += tx.TotalOut
		case "Revocation":
			exp.MempoolData.InvStake[tx.Hash] = struct{}{}
			exp.MempoolData.Revocations = append([]types.MempoolTx{tx}, exp.MempoolData.Revocations...)
			exp.MempoolData.NumRevokes++
			exp.MempoolData.RevokeTotal += tx.TotalOut
		}

		// Update latest transactions, popping the oldest transaction off the
		// back if necessary to limit to NumLatestMempoolTxns.
		numLatest := len(exp.MempoolData.LatestTransactions)
		if numLatest >= NumLatestMempoolTxns {
			exp.MempoolData.LatestTransactions = append([]types.MempoolTx{tx},
				exp.MempoolData.LatestTransactions[:numLatest-1]...)
		} else {
			exp.MempoolData.LatestTransactions = append([]types.MempoolTx{tx},
				exp.MempoolData.LatestTransactions...)
		}

		// Store totals
		exp.MempoolData.NumAll++
		if likely {
			exp.MempoolData.LikelyTotal += tx.TotalOut
		}
		exp.MempoolData.TotalOut += tx.TotalOut
		exp.MempoolData.TotalSize += tx.Size
		exp.MempoolData.FormattedTotalSize = humanize.Bytes(uint64(exp.MempoolData.TotalSize))
		exp.MempoolData.Unlock()

		// Broadcast the new transaction
		exp.wsHub.HubRelay <- sigNewTx
		exp.wsHub.NewTxChan <- &tx
	}
}

func (exp *explorerUI) StopMempoolMonitor(txChan chan *types.NewMempoolTx) {
	log.Infof("Stopping mempool monitor")
	txChan <- nil
}

func (exp *explorerUI) StartMempoolMonitor(newTxChan chan *types.NewMempoolTx) {
	go exp.mempoolMonitor(newTxChan)
}

func (exp *explorerUI) storeMempoolInfo() (lastBlockHash string, lastBlockHeight int64, lastBlockTime int64) {
	defer func(start time.Time) {
		log.Debugf("storeMempoolInfo() completed in %v", time.Since(start))
	}(time.Now())

	// Get mempool transactions and ensure block ID is correct
	var memtxs []types.MempoolTx
	for {
		lastBlockHash0, _, _ := exp.LastBlock()
		memtxs = exp.blockData.GetMempool()
		if memtxs == nil {
			log.Error("Could not get mempool transactions")
			return
		}
		lastBlockHash, lastBlockHeight, lastBlockTime = exp.LastBlock()
		if lastBlockHash == lastBlockHash0 {
			break
		}
		log.Warnf("Best block change while getting mempool. Repeating request.")
	}

	hash, err := chainhash.NewHashFromStr(lastBlockHash)
	if err != nil {
		log.Errorf("storeMempoolInfo: Invalid block hash %s: %v", lastBlockHash, err)
	}
	var h chainhash.Hash
	if hash != nil {
		h = *hash
	}
	lastBlock := &mempool.BlockID{
		Hash:   h,
		Height: lastBlockHeight,
		Time:   lastBlockTime,
	}

	mpInfo := mempool.ParseTxns(memtxs, exp.ChainParams, lastBlock)

	// Store mempool data for template rendering
	exp.MempoolData.Lock()
	defer exp.MempoolData.Unlock()

	exp.MempoolData.Transactions = mpInfo.Transactions
	exp.MempoolData.Tickets = mpInfo.Tickets
	exp.MempoolData.Revocations = mpInfo.Revocations
	exp.MempoolData.Votes = mpInfo.Votes

	exp.MempoolData.MempoolShort = mpInfo.MempoolShort

	return
}

// getLastBlock returns the last block hash, height and time
func (exp *explorerUI) LastBlock() (lastBlockHash string, lastBlock int64, lastBlockTime int64) {
	exp.pageData.RLock()
	defer exp.pageData.RUnlock()
	lastBlock = exp.pageData.BlockInfo.Height
	lastBlockTime = exp.pageData.BlockInfo.BlockTime.T.Unix()
	lastBlockHash = exp.pageData.BlockInfo.Hash
	return
}

// matchMempoolVins filters relevant mempool transaction inputs whose previous
// outpoints match the specified transaction id.
func matchMempoolVins(txid string, txsList []types.MempoolTx) (vins []types.MempoolVin) {
	for idx := range txsList {
		tx := &txsList[idx]
		var inputs []types.MempoolInput
		for vindex := range tx.Vin {
			input := &tx.Vin[vindex]
			if input.TxId != txid {
				continue
			}
			inputs = append(inputs, *input)
		}
		if len(inputs) == 0 {
			continue
		}
		vins = append(vins, types.MempoolVin{
			TxId:   tx.TxID,
			Inputs: inputs,
		})
	}
	return
}

// GetTxMempoolInputs grabs very simple information about mempool transaction
// inputs that spend a particular previous transaction's outputs. The returned
// slice has just enough information to match an unspent transaction output.
func (exp *explorerUI) GetTxMempoolInputs(txid string, txType string) []types.MempoolVin {
	var vins []types.MempoolVin
	exp.MempoolData.RLock()
	defer exp.MempoolData.RUnlock()
	vins = append(vins, matchMempoolVins(txid, exp.MempoolData.Transactions)...)
	vins = append(vins, matchMempoolVins(txid, exp.MempoolData.Tickets)...)
	vins = append(vins, matchMempoolVins(txid, exp.MempoolData.Revocations)...)
	vins = append(vins, matchMempoolVins(txid, exp.MempoolData.Votes)...)
	return vins
}
