// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package mempool

import (
	"context"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/rpcclient"
	exptypes "github.com/decred/dcrdata/v4/explorer/types"
	pstypes "github.com/decred/dcrdata/v4/pubsub/types"
	"github.com/decred/dcrdata/v4/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

// MempoolDataSaver is an interface for storing mempool data.
type MempoolDataSaver interface {
	StoreMPData(*StakeData, []exptypes.MempoolTx)
}

// MempoolMonitor processes new transactions as they are added to mempool, and
// forwards the processed data on channels assigned during construction. An
// inventory of transactions in the current mempool is maintained to prevent
// repetative data processing and signaling. Periodically, such as after a new
// block is mined, the mempool info and the transaction inventory are rebuilt
// fresh via the CollectAndStore method. A MempoolDataCollector is required to
// perform the collection and parsing, and an optional []MempoolDataSaver is
// used to to forward the data to arbitrary destinations. The last block's
// height, hash, and time are kept in memory in order to properly process votes
// in mempool.
type MempoolMonitor struct {
	sync.RWMutex
	ctx        context.Context
	mpoolInfo  MempoolInfo
	inventory  *exptypes.MempoolInfo
	lastBlock  BlockID
	params     *chaincfg.Params
	collector  *MempoolDataCollector
	dataSavers []MempoolDataSaver

	// Incoming data
	newTxIn chan *dcrjson.TxRawResult
	// Outgoing signal and data
	signalOuts []chan pstypes.HubSignal
	newTxOuts  []chan *exptypes.MempoolTx

	wg *sync.WaitGroup
}

// NewMempoolMonitor creates a new MempoolMonitor. The MempoolMonitor receives
// notifications of new transactions on newTxInChan, and of new blocks on the
// same channel using a nil transaction message. Once TxHandler is started, the
// MempoolMonitor will process incoming transactions, and forward new ones on
// via the newTxOutChan following an appropriate signal on hubRelay.
func NewMempoolMonitor(ctx context.Context, collector *MempoolDataCollector,
	savers []MempoolDataSaver, params *chaincfg.Params, wg *sync.WaitGroup,
	newTxInChan chan *dcrjson.TxRawResult,
	signalOuts []chan pstypes.HubSignal, newTxOutChans []chan *exptypes.MempoolTx) *MempoolMonitor {
	// Collect initial mempool data.
	stakeData, txs, err := collector.Collect()
	if err != nil {
		log.Errorf("mempool data collection failed: %v", err.Error())
		return nil
	}

	inventory := ParseTxns(txs, params, &stakeData.LatestBlock)

	p := &MempoolMonitor{
		ctx: ctx,
		mpoolInfo: MempoolInfo{
			CurrentHeight:               uint32(stakeData.LatestBlock.Height),
			NumTicketPurchasesInMempool: stakeData.Ticketfees.FeeInfoMempool.Number,
			LastCollectTime:             stakeData.Time,
		},
		inventory:  inventory,
		lastBlock:  stakeData.LatestBlock,
		params:     params,
		collector:  collector,
		dataSavers: savers,
		newTxIn:    newTxInChan,
		signalOuts: signalOuts,
		newTxOuts:  newTxOutChans,
		wg:         wg,
	}

	return p
}

func (p *MempoolMonitor) LastBlockHash() chainhash.Hash {
	p.RLock()
	defer p.RUnlock()
	return p.lastBlock.Hash
}

func (p *MempoolMonitor) LastBlockHeight() int64 {
	p.RLock()
	defer p.RUnlock()
	return p.lastBlock.Height
}

func (p *MempoolMonitor) LastBlockTime() int64 {
	p.RLock()
	defer p.RUnlock()
	return p.lastBlock.Time
}

// TxHandler receives signals from OnTxAccepted via the newTxIn, indicating that
// a new transaction has entered mempool. This function should be launched as a
// goroutine, and stopped by closing the quit channel, the broadcasting
// mechanism used by main. The newTxIn contains a chain hash for the transaction
// from the notificiation, or a zero value hash indicating it was from a Ticker
// or manually triggered.
func (p *MempoolMonitor) TxHandler(client *rpcclient.Client) {
	defer p.wg.Done()
	for {
		select {
		case s, ok := <-p.newTxIn:
			if !ok {
				log.Infof("New Tx channel closed")
				return
			}

			// A nil value signals a new block.
			if s == nil {
				log.Debugf("New block - starting CollectAndStore...")
				_ = p.CollectAndStore()
				log.Debugf("New Block - sending SigMempoolUpdate to hub relay...")
				// p.signalOuts <- pstypes.SigMempoolUpdate
				p.hubSend(pstypes.SigMempoolUpdate, time.Second*10)
				continue
			}

			log.Tracef("TxHandler: new transaction: %v.", s.Txid)

			// Ignore this tx if it was received before the last block.
			if s.Time < p.LastBlockTime() {
				log.Debugf("Old: %d < %d", s.Time, p.LastBlockTime())
				continue
			}

			msgTx, err := txhelpers.MsgTxFromHex(s.Hex)
			if err != nil {
				log.Errorf("Failed to decode transaction: %v", err)
				continue
			}

			hash := msgTx.TxHash().String()
			txType := txhelpers.DetermineTxTypeString(msgTx)

			// Maintain the list of unique stake and regular txns encountered.
			p.inventory.RLock()
			var txExists bool
			if txType == "Regular" {
				_, txExists = p.inventory.InvRegular[hash]
			} else {
				_, txExists = p.inventory.InvStake[hash]
			}
			p.inventory.RUnlock()

			if txExists {
				log.Tracef("Not broadcasting duplicate %s notification: %s", txType, hash)
				continue // back to waiting for new tx signal
			}

			// If this is a vote, decode vote bits.
			var voteInfo *exptypes.VoteInfo
			if ok := stake.IsSSGen(msgTx); ok {
				validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(msgTx, p.params)
				if err != nil {
					log.Debugf("Cannot get vote choices for %s", hash)
				} else {
					voteInfo = &exptypes.VoteInfo{
						Validation: exptypes.BlockValidation{
							Hash:     validation.Hash.String(),
							Height:   validation.Height,
							Validity: validation.Validity,
						},
						Version:     version,
						Bits:        bits,
						Choices:     choices,
						TicketSpent: msgTx.TxIn[1].PreviousOutPoint.Hash.String(),
					}
					voteInfo.ForLastBlock = voteInfo.VotesOnBlock(p.LastBlockHash().String())
				}
			}

			fee, _ := txhelpers.TxFeeRate(msgTx)

			tx := exptypes.MempoolTx{
				TxID:      msgTx.TxHash().String(),
				Fees:      fee.ToCoin(),
				VinCount:  len(msgTx.TxIn),
				VoutCount: len(msgTx.TxOut),
				Vin:       exptypes.MsgTxMempoolInputs(msgTx),
				Coinbase:  blockchain.IsCoinBaseTx(msgTx),
				Hash:      hash,
				Time:      s.Time,
				Size:      int32(len(s.Hex) / 2),
				TotalOut:  txhelpers.TotalOutFromMsgTx(msgTx).ToCoin(),
				Type:      txhelpers.DetermineTxTypeString(msgTx),
				VoteInfo:  voteInfo,
			}

			// Add the tx to the appropriate tx slice in inventory and update
			// the count for the transaction type.
			p.inventory.Lock()
			switch tx.Type {
			case "Ticket":
				p.inventory.InvStake[tx.Hash] = struct{}{}
				p.inventory.Tickets = append([]exptypes.MempoolTx{tx}, p.inventory.Tickets...)
				p.inventory.NumTickets++
			case "Vote":
				// Votes on the next block may be received just prior to dcrdata
				// actually processing the new block. Do not broadcast these
				// ahead of the full update with the new block signal as the
				// vote will be included in that update.
				if tx.VoteInfo.Validation.Height > p.LastBlockHeight() {
					p.inventory.Unlock()
					log.Trace("Got a vote for a future block. Waiting to pull it "+
						"out of mempool with new block signal. Vote: ", tx.Hash)
					continue
				}

				p.inventory.InvStake[tx.Hash] = struct{}{}
				p.inventory.Votes = append([]exptypes.MempoolTx{tx}, p.inventory.Votes...)
				//sort.Sort(byHeight(p.inventory.Votes))
				p.inventory.NumVotes++

				// Multiple transactions can be in mempool from the same ticket.
				// We need to insure we do not double count these votes.
				tx.VoteInfo.SetTicketIndex(p.inventory.TicketIndexes)
				votingInfo := &p.inventory.VotingInfo
				if tx.VoteInfo.ForLastBlock && !votingInfo.VotedTickets[tx.VoteInfo.TicketSpent] {
					votingInfo.VotedTickets[tx.VoteInfo.TicketSpent] = true
					votingInfo.TicketsVoted++
				}
			case "Regular":
				p.inventory.InvRegular[tx.Hash] = struct{}{}
				p.inventory.Transactions = append([]exptypes.MempoolTx{tx}, p.inventory.Transactions...)
				p.inventory.NumRegular++
			case "Revocation":
				p.inventory.InvStake[tx.Hash] = struct{}{}
				p.inventory.Revocations = append([]exptypes.MempoolTx{tx}, p.inventory.Revocations...)
				p.inventory.NumRevokes++
			}

			// Update latest transactions, popping the oldest transaction off
			// the back if necessary to limit to NumLatestMempoolTxns.
			numLatest := len(p.inventory.LatestTransactions)
			if numLatest >= NumLatestMempoolTxns {
				p.inventory.LatestTransactions = append([]exptypes.MempoolTx{tx},
					p.inventory.LatestTransactions[:numLatest-1]...)
			} else {
				p.inventory.LatestTransactions = append([]exptypes.MempoolTx{tx},
					p.inventory.LatestTransactions...)
			}

			// Store totals.
			p.inventory.NumAll++
			p.inventory.TotalOut += tx.TotalOut
			p.inventory.TotalSize += tx.Size
			p.inventory.FormattedTotalSize = humanize.Bytes(uint64(p.inventory.TotalSize))
			p.inventory.Unlock()

			// Broadcast the new transaction.
			log.Tracef("Signaling mempool event to hub relays...")
			p.hubSend(pstypes.SigNewTx, time.Second*10)

			log.Tracef("Sending tx data on tx out channels...")
			p.sendTx(&tx, time.Second*10)

		case <-p.ctx.Done():
			log.Debugf("Quitting TxHandler (new tx in mempool) handler.")
			return
		}
	}
}

func (p *MempoolMonitor) hubSend(sig pstypes.HubSignal, timeout time.Duration) {
	for _, sigout := range p.signalOuts {
		select {
		case sigout <- sig:
		case <-time.After(timeout):
			log.Errorf("send to signalOuts (%v) failed: Timeout waiting for WebsocketHub.", sig)
		}
	}
}

func (p *MempoolMonitor) sendTx(tx *exptypes.MempoolTx, timeout time.Duration) {
	for _, newTxOut := range p.newTxOuts {
		select {
		case newTxOut <- tx:
		case <-time.After(timeout):
			log.Errorf("send to newTxOut failed: Timeout.")
		}
	}
}

// CollectAndStore collects mempool data, resets counters ticket counters and
// the timer, and dispatches the storers.
func (p *MempoolMonitor) CollectAndStore() error {
	// Collect mempool data (currently ticket fees)
	log.Trace("Gathering new mempool data.")
	stakeData, txs, err := p.collector.Collect()
	if err != nil {
		log.Errorf("mempool data collection failed: %v", err.Error())
		// stakeData is nil when err != nil
		return err
	}

	inventory := ParseTxns(txs, p.params, &stakeData.LatestBlock)

	// Reset the counter for tickets since last report.
	p.Lock()
	newTickets := p.mpoolInfo.NumTicketsSinceStatsReport
	p.mpoolInfo.NumTicketsSinceStatsReport = 0

	// Reset the timer and ticket counter.
	p.mpoolInfo.LastCollectTime = stakeData.Time
	p.mpoolInfo.NumTicketPurchasesInMempool = stakeData.Ticketfees.FeeInfoMempool.Number

	// Store the current best block info.
	p.lastBlock = stakeData.LatestBlock
	p.inventory = inventory
	p.Unlock()

	// Insert new ticket counter into stakeData structure.
	stakeData.NewTickets = uint32(newTickets)

	// Store mempool stakeData with each registered saver.
	for _, s := range p.dataSavers {
		if s != nil {
			log.Trace("Saving MP data.")
			// Save data to wherever the saver wants to put it.
			go s.StoreMPData(stakeData, txs)
		}
	}

	return nil
}
