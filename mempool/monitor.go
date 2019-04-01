// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package mempool

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/rpcclient/v2"
	exptypes "github.com/decred/dcrdata/explorer/types"
	pstypes "github.com/decred/dcrdata/pubsub/types"
	"github.com/decred/dcrdata/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

// MempoolDataSaver is an interface for storing mempool data.
type MempoolDataSaver interface {
	StoreMPData(*StakeData, []exptypes.MempoolTx, *exptypes.MempoolInfo)
}

// MempoolAddressStore wraps txhelpers.MempoolAddressStore with a Mutex.
type MempoolAddressStore struct {
	mtx   sync.Mutex
	store txhelpers.MempoolAddressStore
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
	mtx        sync.RWMutex
	ctx        context.Context
	mpoolInfo  MempoolInfo
	inventory  *exptypes.MempoolInfo
	addrMap    MempoolAddressStore
	txnsStore  txhelpers.TxnsStore
	lastBlock  BlockID
	params     *chaincfg.Params
	collector  *MempoolDataCollector
	dataSavers []MempoolDataSaver

	// Incoming data
	newTxIn <-chan *dcrjson.TxRawResult
	// Outgoing signal and data
	signalOuts []chan<- pstypes.HubSignal
	newTxOuts  []chan<- *exptypes.MempoolTx

	wg *sync.WaitGroup
}

// NewMempoolMonitor creates a new MempoolMonitor. The MempoolMonitor receives
// notifications of new transactions on newTxInChan, and of new blocks on the
// same channel using a nil transaction message. Once TxHandler is started, the
// MempoolMonitor will process incoming transactions, and forward new ones on
// via the newTxOutChan following an appropriate signal on hubRelay.
func NewMempoolMonitor(ctx context.Context, collector *MempoolDataCollector,
	savers []MempoolDataSaver, params *chaincfg.Params, wg *sync.WaitGroup,
	newTxInChan <-chan *dcrjson.TxRawResult, signalOuts []chan<- pstypes.HubSignal,
	newTxOutChans []chan<- *exptypes.MempoolTx, initialStore bool) (*MempoolMonitor, error) {

	// Make the skeleton MempoolMonitor.
	p := &MempoolMonitor{
		ctx:        ctx,
		params:     params,
		collector:  collector,
		dataSavers: savers,
		newTxIn:    newTxInChan,
		signalOuts: signalOuts,
		newTxOuts:  newTxOutChans,
		wg:         wg,
	}

	if initialStore {
		return p, p.CollectAndStore()
	}
	_, _, _, err := p.Refresh()
	return p, err
}

// LastBlockHash returns the hash of the most recently stored block.
func (p *MempoolMonitor) LastBlockHash() chainhash.Hash {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	return p.lastBlock.Hash
}

// LastBlockHeight returns the height of the most recently stored block.
func (p *MempoolMonitor) LastBlockHeight() int64 {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	return p.lastBlock.Height
}

// LastBlockTime returns the time of the most recently stored block.
func (p *MempoolMonitor) LastBlockTime() int64 {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
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
			p.mtx.RLock()      // do not allow p.inventory to be reset
			p.inventory.Lock() // do not allow *p.inventory to be accessed
			var txExists bool
			if txType == "Regular" {
				_, txExists = p.inventory.InvRegular[hash]
			} else {
				_, txExists = p.inventory.InvStake[hash]
			}

			if txExists {
				log.Tracef("Not broadcasting duplicate %s notification: %s", txType, hash)
				p.inventory.Unlock()
				p.mtx.RUnlock()
				continue // back to waiting for new tx signal
			}

			// Set Outpoints in the addrMap.
			p.addrMap.mtx.Lock()
			if p.addrMap.store == nil {
				p.addrMap.store = make(txhelpers.MempoolAddressStore)
			}
			newOuts, addressesOut := txhelpers.TxOutpointsByAddr(p.addrMap.store, msgTx, p.params)
			var newOutAddrs int
			for _, isNew := range addressesOut {
				if isNew {
					newOutAddrs++
				}
			}

			// Set PrevOuts in the addrMap, and related txns data in txnsStore.
			if p.txnsStore == nil {
				p.txnsStore = make(txhelpers.TxnsStore)
			}
			newPrevOuts, addressesIn := txhelpers.TxPrevOutsByAddr(p.addrMap.store, p.txnsStore, msgTx, client, p.params)
			var newInAddrs int
			for _, isNew := range addressesIn {
				if isNew {
					newInAddrs++
				}
			}
			p.addrMap.mtx.Unlock()

			// Store the current mempool transaction, block info zeroed.
			p.txnsStore[msgTx.TxHash()] = &txhelpers.TxWithBlockData{
				Tx:          msgTx,
				MemPoolTime: s.Time,
			}

			log.Tracef("New transaction (%s: %s) added %d new and %d previous outpoints, "+
				"%d out addrs (%d new), %d prev out addrs (%d new).",
				txType, hash, newOuts, newPrevOuts,
				len(addressesOut), newOutAddrs, len(addressesIn), newInAddrs)

			// Iterate the state id.
			p.inventory.Ident++

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
					voteInfo.ForLastBlock = voteInfo.VotesOnBlock(p.lastBlock.Hash.String())
				}
			}

			fee, _ := txhelpers.TxFeeRate(msgTx)

			tx := exptypes.MempoolTx{
				TxID:      hash,
				Fees:      fee.ToCoin(),
				VinCount:  len(msgTx.TxIn),
				VoutCount: len(msgTx.TxOut),
				Vin:       exptypes.MsgTxMempoolInputs(msgTx),
				Coinbase:  blockchain.IsCoinBaseTx(msgTx),
				Hash:      hash,
				Time:      s.Time,
				Size:      int32(len(s.Hex) / 2),
				TotalOut:  txhelpers.TotalOutFromMsgTx(msgTx).ToCoin(),
				Type:      txType,
				VoteInfo:  voteInfo,
			}

			// Maintain a separate total that excludes votes for sidechain
			// blocks and multiple votes that spend the same ticket.
			likelyMineable := true

			// Add the tx to the appropriate tx slice in inventory and update
			// the count for the transaction type.
			switch tx.Type {
			case "Ticket":
				p.inventory.InvStake[tx.Hash] = struct{}{}
				p.inventory.Tickets = append([]exptypes.MempoolTx{tx}, p.inventory.Tickets...)
				p.inventory.NumTickets++
				p.inventory.LikelyMineable.TicketTotal += tx.TotalOut
			case "Vote":
				// Votes on the next block may be received just prior to dcrdata
				// actually processing the new block. Do not broadcast these
				// ahead of the full update with the new block signal as the
				// vote will be included in that update.
				if tx.VoteInfo.Validation.Height > p.lastBlock.Height {
					p.inventory.Unlock()
					p.mtx.RUnlock()
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
					p.inventory.LikelyMineable.VoteTotal += tx.TotalOut
					p.inventory.VotingInfo.Tally(tx.VoteInfo)
				} else {
					likelyMineable = false
				}
			case "Regular":
				p.inventory.InvRegular[tx.Hash] = struct{}{}
				p.inventory.Transactions = append([]exptypes.MempoolTx{tx}, p.inventory.Transactions...)
				p.inventory.NumRegular++
				p.inventory.LikelyMineable.RegularTotal += tx.TotalOut
			case "Revocation":
				p.inventory.InvStake[tx.Hash] = struct{}{}
				p.inventory.Revocations = append([]exptypes.MempoolTx{tx}, p.inventory.Revocations...)
				p.inventory.NumRevokes++
				p.inventory.LikelyMineable.RevokeTotal += tx.TotalOut
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
			if likelyMineable {
				p.inventory.LikelyMineable.Size += tx.Size
				p.inventory.LikelyMineable.FormattedSize = humanize.Bytes(uint64(p.inventory.LikelyMineable.Size))
				p.inventory.LikelyMineable.Total += tx.TotalOut
				p.inventory.LikelyMineable.Count++
			}
			p.inventory.FormattedTotalSize = humanize.Bytes(uint64(p.inventory.TotalSize))
			p.inventory.Unlock()
			p.mtx.RUnlock()

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

// Refresh collects mempool data, resets counters ticket counters and the timer,
// but does not dispatch the MempoolDataSavers.
func (p *MempoolMonitor) Refresh() (*StakeData, []exptypes.MempoolTx, *exptypes.MempoolInfo, error) {
	// Collect mempool data (currently ticket fees)
	log.Trace("Gathering new mempool data.")
	stakeData, txs, addrOuts, txnsStore, err := p.collector.Collect()
	if err != nil {
		log.Errorf("mempool data collection failed: %v", err.Error())
		// stakeData is nil when err != nil
		return nil, nil, nil, err
	}

	log.Debugf("%d addresses in mempool pertaining to %d transactions",
		len(addrOuts), len(txnsStore))

	// Pre-sort the txs so other consumers will not have to do it.
	sort.Sort(exptypes.MPTxsByTime(txs))
	inventory := ParseTxns(txs, p.params, &stakeData.LatestBlock)

	// Reset the counter for tickets since last report.
	p.mtx.Lock()
	newTickets := p.mpoolInfo.NumTicketsSinceStatsReport
	p.mpoolInfo.NumTicketsSinceStatsReport = 0

	// Reset the timer and ticket counter.
	p.mpoolInfo.CurrentHeight = uint32(stakeData.LatestBlock.Height)
	p.mpoolInfo.LastCollectTime = stakeData.Time
	p.mpoolInfo.NumTicketPurchasesInMempool = stakeData.Ticketfees.FeeInfoMempool.Number

	// Store the current best block info.
	p.lastBlock = stakeData.LatestBlock
	if p.inventory != nil {
		inventory.Ident = p.inventory.ID() + 1
	}
	p.inventory = inventory
	p.txnsStore = txnsStore
	p.mtx.Unlock()

	p.addrMap.mtx.Lock()
	p.addrMap.store = addrOuts
	p.addrMap.mtx.Unlock()

	// Insert new ticket counter into stakeData structure.
	stakeData.NewTickets = uint32(newTickets)

	return stakeData, txs, inventory, err
}

// CollectAndStore collects mempool data, resets counters ticket counters and
// the timer, and dispatches the storers.
func (p *MempoolMonitor) CollectAndStore() error {
	log.Trace("Gathering new mempool data.")
	stakeData, txs, inv, err := p.Refresh()
	if err != nil {
		log.Errorf("mempool data collection failed: %v", err.Error())
		// stakeData is nil when err != nil
		return err
	}

	// Store mempool stakeData with each registered saver.
	for _, s := range p.dataSavers {
		if s != nil {
			log.Trace("Saving MP data.")
			// Save data to wherever the saver wants to put it.
			// Deep copy the txs slice so each saver can modify it.
			txsCopy := exptypes.CopyMempoolTxSlice(txs)
			go s.StoreMPData(stakeData, txsCopy, inv)
		}
	}

	return nil
}

// UnconfirmedTxnsForAddress indexes (1) outpoints in mempool that pay to the
// given address, (2) previous outpoint being consumed that paid to the address,
// and (3) all relevant transactions. See txhelpers.AddressOutpoints for more
// information. The number of unconfirmed transactions is also returned. This
// satisfies the rpcutils.MempoolAddressChecker interface for MempoolMonitor.
func (p *MempoolMonitor) UnconfirmedTxnsForAddress(address string) (*txhelpers.AddressOutpoints, int64, error) {
	p.addrMap.mtx.Lock()
	defer p.addrMap.mtx.Unlock()
	addrStore := p.addrMap.store
	if addrStore == nil {
		return nil, 0, fmt.Errorf("uininitialized MempoolAddressStore")
	}

	// Retrieve the AddressOutpoints for this address.
	outs := addrStore[address]
	if outs == nil {
		return txhelpers.NewAddressOutpoints(address), 0, nil
	}

	// TxnsStore for this address may have been filled out previously.
	if outs.TxnsStore != nil {
		return outs, int64(len(outs.TxnsStore)), nil
	}

	outs.TxnsStore = make(txhelpers.TxnsStore)

	// Fill out the TxnsStore and count unconfirmed transactions. Note that the
	// values stored in TxnsStore are pointers, and they are already allocated
	// and stored in MempoolMonitor.txnsStore. This code makes a similar
	// transaction map for just the transactions related to the address.

	// Process the transaction hashes for the new outpoints.
	for op := range outs.Outpoints {
		hash := outs.Outpoints[op].Hash
		// New transaction for this address?
		if _, found := outs.TxnsStore[hash]; found {
			// This is another (prev)out for an already seen transaction, so
			// there is no need to retrieve it from MempoolMonitor.txnsStore.
			continue
		}

		txData := p.txnsStore[hash]
		if txData == nil {
			log.Warnf("Unable to locate in TxnsStore: %v", hash)
			continue
		}
		outs.TxnsStore[hash] = txData
	}

	// Process the transaction hashes for the consumed previous outpoints.
	for ip := range outs.PrevOuts {
		hash := outs.PrevOuts[ip].PreviousOutpoint.Hash
		// New transaction for this address?
		if _, found := outs.TxnsStore[hash]; found {
			// This is another (prev)out for an already seen transaction, so
			// there is no need to retrieve it from MempoolMonitor.txnsStore.
			continue
		}

		txData := p.txnsStore[hash]
		if txData == nil {
			log.Warnf("Unable to locate in TxnsStore: %v", hash)
		}
		outs.TxnsStore[hash] = txData
	}

	return outs, int64(len(outs.TxnsStore)), nil
}
