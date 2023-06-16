// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package mempool

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"

	apitypes "github.com/decred/dcrdata/v8/api/types"
	exptypes "github.com/decred/dcrdata/v8/explorer/types"
	"github.com/decred/dcrdata/v8/txhelpers"
)

// NodeClient is similar to a rpcclient.Client, except for the addition of
// GetRawTransactionVerbosePromise. Use rpcutils.NewAsyncTxClient to create one
// from an rpcclient.Client or just implement a wrapper that provides
// txhelpers.VerboseTransactionPromiseGetter.
type NodeClient interface {
	GetRawMempoolVerbose(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) (map[string]chainjson.GetRawMempoolVerboseResult, error)
	GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error)
	txhelpers.RawTransactionGetter
	txhelpers.VerboseTransactionGetter
	txhelpers.VerboseTransactionPromiseGetter
	GetStakeDifficulty(ctx context.Context) (*chainjson.GetStakeDifficultyResult, error)
	GetBlockHeaderVerbose(ctx context.Context, hash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error)
	TicketFeeInfo(ctx context.Context, blocks *uint32, windows *uint32) (*chainjson.TicketFeeInfoResult, error)
}

// DataCollector is used for retrieving and processing data from a chain
// server's mempool.
type DataCollector struct {
	// Mutex is used to prevent multiple concurrent calls to Collect.
	mtx          sync.Mutex
	dcrdChainSvr NodeClient
	activeChain  *chaincfg.Params
}

// NewDataCollector creates a new DataCollector. Use a rpcutils.AsyncTxClient to
// create a NodeClient from an rpcclient.Client or implement a wrapper that
// provides txhelpers.VerboseTransactionPromiseGetter.
func NewDataCollector(dcrdChainSvr NodeClient, params *chaincfg.Params) *DataCollector {
	return &DataCollector{
		dcrdChainSvr: dcrdChainSvr,
		activeChain:  params,
	}
}

// mempoolTxns retrieves all transactions and returns them as a
// []exptypes.MempoolTx. See also ParseTxns, which may process this slice. A
// fresh MempoolAddressStore and TxnsStore are also generated.
func (t *DataCollector) mempoolTxns() ([]exptypes.MempoolTx, txhelpers.MempoolAddressStore, txhelpers.TxnsStore, error) {
	mempooltxs, err := t.dcrdChainSvr.GetRawMempoolVerbose(context.TODO(), chainjson.GRMAll)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetRawMempoolVerbose failed: %v", err)
	}

	blockHash, _, err := t.dcrdChainSvr.GetBestBlock(context.TODO())
	if err != nil {
		return nil, nil, nil, err
	}
	blockhash := blockHash.String()

	txs := make([]exptypes.MempoolTx, 0, len(mempooltxs))
	addrMap := make(txhelpers.MempoolAddressStore)
	txnsStore := make(txhelpers.TxnsStore)

	for hashStr, tx := range mempooltxs {
		hash, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			log.Warn(err)
			continue
		}
		txn, err := t.dcrdChainSvr.GetRawTransaction(context.TODO(), hash)
		if err != nil {
			log.Warn(err)
			continue
		}
		msgTx := txn.MsgTx()

		// Set Outpoints in the addrMap.
		txhelpers.TxOutpointsByAddr(addrMap, msgTx, t.activeChain)

		// Set PrevOuts in the addrMap, and related txns data in txnsStore.
		txhelpers.TxPrevOutsByAddr(addrMap, txnsStore, msgTx, t.dcrdChainSvr, t.activeChain)

		// Store the current mempool transaction with MemPoolTime from GRM, and
		// block info zeroed.
		txnsStore[*hash] = &txhelpers.TxWithBlockData{
			Tx:          msgTx,
			MemPoolTime: tx.Time,
		}

		var totalOut int64
		for _, v := range msgTx.TxOut {
			totalOut += v.Value
		}

		txType := txhelpers.DetermineTxType(msgTx)

		var voteInfo *exptypes.VoteInfo
		if txType == stake.TxTypeSSGen {
			validation, version, bits, choices, tspendVotes, err := txhelpers.SSGenVoteChoices(msgTx, t.activeChain)
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
					TSpends:     exptypes.ConvertTSpendVotes(tspendVotes),
				}
				voteInfo.ForLastBlock = voteInfo.VotesOnBlock(blockhash)
			}
		}

		// Note that the fee computed from msgTx will be a large negative fee
		// for coinbase transactions.
		//fee, _ := txhelpers.TxFeeRate(msgTx)
		// log.Tracef("tx fee: GRM result = %f, msgTx = %f", tx.Fee, fee.ToCoin())

		_, feeRate := txhelpers.TxFeeRate(msgTx)

		txs = append(txs, exptypes.MempoolTx{
			TxID:      hashStr,
			Version:   int32(msgTx.Version),
			Fees:      tx.Fee,
			FeeRate:   feeRate.ToCoin(),
			VinCount:  len(msgTx.TxIn),
			VoutCount: len(msgTx.TxOut),
			Vin:       exptypes.MsgTxMempoolInputs(msgTx),
			// Coinbase:  txhelpers.IsCoinBaseTx(msgTx), // commented because coinbase is not in mempool
			Hash:     hashStr, // dup of TxID!
			Time:     tx.Time,
			Size:     tx.Size,
			TotalOut: dcrutil.Amount(totalOut).ToCoin(),
			Type:     txhelpers.TxTypeToString(int(txType)),
			TypeID:   int(txType),
			VoteInfo: voteInfo,
		})
	}

	return txs, addrMap, txnsStore, nil
}

// Collect is the main handler for collecting mempool data. Data collection is
// focused on stake-related information, including vote and ticket transactions,
// and fee info. Transactions of all types in mempool are returned as a
// []exptypes.MempoolTx, corresponding to the same data provided by the
// unexported mempoolTxns method.
func (t *DataCollector) Collect() (*StakeData, []exptypes.MempoolTx, txhelpers.MempoolAddressStore, txhelpers.TxnsStore, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Time this function
	defer func(start time.Time) {
		log.Debugf("DataCollector.Collect() completed in %v",
			time.Since(start))
	}(time.Now())

	// Get a map of ticket hashes to getrawmempool results
	// mempoolTickets[ticketHashes[0].String()].Fee
	mempoolTickets, err := t.dcrdChainSvr.GetRawMempoolVerbose(context.TODO(), chainjson.GRMTickets)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	mempoolVotes, err := t.dcrdChainSvr.GetRawMempoolVerbose(context.TODO(), chainjson.GRMVotes)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	numVotes := len(mempoolVotes)

	// Grab the current stake difficulty (ticket price).
	stakeDiff, err := t.dcrdChainSvr.GetStakeDifficulty(context.TODO())
	if err != nil {
		return nil, nil, nil, nil, err
	}

	hash, height, err := t.dcrdChainSvr.GetBestBlock(context.TODO())
	if err != nil {
		return nil, nil, nil, nil, err
	}
	header, err := t.dcrdChainSvr.GetBlockHeaderVerbose(context.TODO(), hash)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	blockTime := header.Time

	// Fee info
	var numFeeWindows, numFeeBlocks uint32 = 0, 0
	feeInfo, err := t.dcrdChainSvr.TicketFeeInfo(context.TODO(), &numFeeBlocks, &numFeeWindows)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// All transactions in mempool.
	allTxns, addrMap, txnsStore, err := t.mempoolTxns()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	now := time.Now()

	// Make slice of TicketDetails
	N := len(mempoolTickets)
	allTicketsDetails := make(TicketsDetails, 0, N)
	for hash, ticket := range mempoolTickets {
		//ageSec := time.Since(time.Unix(ticket.Time, 0)).Seconds()
		// Compute fee in DCR / kB
		feeRate := ticket.Fee / float64(ticket.Size) * 1000
		allTicketsDetails = append(allTicketsDetails, &apitypes.TicketDetails{
			Hash:    hash,
			Fee:     ticket.Fee,
			FeeRate: feeRate,
			Size:    ticket.Size,
			Height:  ticket.Height,
		})
	}
	// Verify we get the correct median result
	//medianFee := MedianCoin(allFeeRates)
	//log.Infof("Median fee computed: %v (%v)", medianFee, N)

	sort.Sort(ByAbsoluteFee{allTicketsDetails})
	allFees := make([]float64, 0, N)
	for _, td := range allTicketsDetails {
		allFees = append(allFees, td.Fee)
	}
	sort.Sort(ByFeeRate{allTicketsDetails})
	allFeeRates := make([]float64, 0, N)
	for _, td := range allTicketsDetails {
		allFeeRates = append(allFeeRates, td.FeeRate)
	}

	// 20 tickets purchases may be mined per block
	Nmax := int(t.activeChain.MaxFreshStakePerBlock)
	//sort.Float64s(allFeeRates)
	var lowestMineableFee float64
	// If no tickets, no valid index
	var lowestMineableIdx = -1
	if N >= Nmax {
		lowestMineableIdx = N - Nmax
		lowestMineableFee = allFeeRates[lowestMineableIdx]
	} else if N != 0 {
		lowestMineableIdx = 0
		lowestMineableFee = allFeeRates[0]
	}

	// Extract the fees for a window about the mileability threshold
	var targetFeeWindow []float64
	if N > 0 {
		// Summary output has it's own radius, but here we hard-code
		const feeRad int = 5

		lowEnd := lowestMineableIdx - feeRad
		if lowEnd < 0 {
			lowEnd = 0
		}

		// highEnd is the exclusive end of the half-open range (+1)
		highEnd := lowestMineableIdx + feeRad + 1
		if highEnd > N {
			highEnd = N
		}

		targetFeeWindow = allFeeRates[lowEnd:highEnd]
	}

	mineables := &MinableFeeInfo{
		allFees,
		allFeeRates,
		lowestMineableIdx,
		lowestMineableFee,
		targetFeeWindow,
	}

	mpoolData := &StakeData{
		LatestBlock: BlockID{
			Hash:   *hash,
			Height: height,
			Time:   blockTime,
		},
		Time:       now,
		NumTickets: feeInfo.FeeInfoMempool.Number,
		NumVotes:   uint32(numVotes),
		// NewTickets set by CollectAndStore
		Ticketfees:        feeInfo,
		MinableFees:       mineables,
		AllTicketsDetails: allTicketsDetails,
		StakeDiff:         stakeDiff.CurrentStakeDifficulty,
	}

	return mpoolData, allTxns, addrMap, txnsStore, err
}

// NumLatestMempoolTxns is the maximum number of mempool transactions that will
// be stored in the LatestTransactions field of the MempoolInfo generated by
// ParseTxns.
const NumLatestMempoolTxns = 5

// ParseTxns analyzes the mempool transactions in the txs slice, and generates a
// MempoolInfo summary with categorized transactions.
func ParseTxns(txs []exptypes.MempoolTx, params *chaincfg.Params, lastBlock *BlockID) *exptypes.MempoolInfo {
	// The txs slice needs to be sorted by time, but we do not want to modify
	// the slice outside of this function and we do not want to waste time
	// copying it if it is already sorted. So, make a copy and sort only if it
	// is not already sorted.
	if !sort.SliceIsSorted(txs, func(i, j int) bool {
		return txs[i].Time > txs[j].Time
	}) {
		log.Debug("The transactions slice was not sorted by time. Sorting it now.")
		// Copy the input slice to avoid side effects.
		txs0 := txs
		txs = make([]exptypes.MempoolTx, len(txs0))
		copy(txs, txs0)
		sort.Sort(exptypes.MPTxsByTime(txs))
	}

	// Get the NumLatestMempoolTxns latest transactions in mempool.
	var latest []exptypes.MempoolTx
	if len(txs) > NumLatestMempoolTxns {
		latest = txs[:NumLatestMempoolTxns]
	} else {
		latest = txs
	}

	// Initialize with make to ensure they marshal to JSON as [] if empty.
	tickets := make([]exptypes.MempoolTx, 0)
	votes := make([]exptypes.MempoolTx, 0)
	revs := make([]exptypes.MempoolTx, 0)
	regular := make([]exptypes.MempoolTx, 0)
	tadds := make([]exptypes.MempoolTx, 0)
	tspends := make([]exptypes.MempoolTx, 0)

	// Transaction inventory.
	invRegular := make(map[string]struct{})
	invStake := make(map[string]struct{}) // includes all treasure types, I believe

	blockhash := lastBlock.Hash.String()
	votingInfo := exptypes.NewVotingInfo(params.TicketsPerBlock)

	// Reduction variables.
	var latestTime int64
	var totalOut, regularTotal, ticketTotal, voteTotal, revTotal, taddTotal, tspendTotal dcrutil.Amount
	var likelyMineable bool
	var likelyTotal dcrutil.Amount
	var totalSize, likelySize int32
	var numLikely int

	// Initialize the BlockValidatorIndex, a map.
	ticketSpendInds := make(exptypes.BlockValidatorIndex)

	for _, tx := range txs {
		likelyMineable = true
		out, _ := dcrutil.NewAmount(tx.TotalOut) // 0 for invalid amounts
		switch stake.TxType(tx.TypeID) {
		case stake.TxTypeSStx:
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			ticketTotal += out
			invStake[tx.Hash] = struct{}{}
			tickets = append(tickets, tx)

		case stake.TxTypeSSGen:
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			invStake[tx.Hash] = struct{}{}
			votes = append(votes, tx)

			if tx.VoteInfo == nil {
				log.Errorf("Missing vote information for %v!", tx)
				continue
			}
			// Assign an index to this vote that is unique to the spent ticket +
			// validated block.
			tx.VoteInfo.SetTicketIndex(ticketSpendInds)
			// Determine if this vote is (in)validating the previous block.
			tx.VoteInfo.ForLastBlock = tx.VoteInfo.VotesOnBlock(blockhash)
			// Update tally if this is for the previous block, the ticket has not
			// yet been spent. Do not attempt to decide block validity.
			if tx.VoteInfo.ForLastBlock && !votingInfo.VotedTickets[tx.VoteInfo.TicketSpent] {
				votingInfo.VotedTickets[tx.VoteInfo.TicketSpent] = true
				votingInfo.TicketsVoted++
				voteTotal += out
				votingInfo.Tally(tx.VoteInfo)
			} else {
				likelyMineable = false
			}

		case stake.TxTypeSSRtx:
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			revTotal += out
			invStake[tx.Hash] = struct{}{}
			revs = append(revs, tx)

		case stake.TxTypeTSpend:
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			invStake[tx.Hash] = struct{}{}
			tspendTotal += out
			tspends = append(tspends, tx)
			// mineable depends on vote choices and TreasuryVoteInterval

		case stake.TxTypeTAdd:
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			invStake[tx.Hash] = struct{}{}
			taddTotal += out
			tadds = append(tadds, tx)

		case stake.TxTypeTreasuryBase:
			// treasurybase won't be in mempool, but it certainly should not
			// default to the regular tree txn map.
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			invStake[tx.Hash] = struct{}{}
			log.Warnf("Processed a treasurybase in mempool, which should not happen! %v", tx.TxID)

		default:
			if _, found := invRegular[tx.Hash]; found {
				continue
			}
			regularTotal += out
			invRegular[tx.Hash] = struct{}{}
			regular = append(regular, tx)
		}

		// Update mempool totals
		if likelyMineable {
			likelyTotal += out
			likelySize += tx.Size
			numLikely++
		}
		totalOut += out
		totalSize += tx.Size

		if latestTime < tx.Time {
			latestTime = tx.Time
		}
	}

	sort.Sort(exptypes.MPTxsByHeight(votes))
	formattedSize := exptypes.BytesString(uint64(totalSize))

	// Store mempool data for template rendering
	mpInfo := exptypes.MempoolInfo{
		MempoolShort: exptypes.MempoolShort{
			LastBlockHeight:    lastBlock.Height,
			LastBlockHash:      blockhash,
			LastBlockTime:      lastBlock.Time,
			FormattedBlockTime: (exptypes.TimeDef{T: time.Unix(lastBlock.Time, 0)}).String(),
			Time:               latestTime,
			TotalOut:           totalOut.ToCoin(),
			TotalSize:          totalSize,
			NumTickets:         len(tickets),
			NumVotes:           len(votes),
			NumRegular:         len(regular),
			NumRevokes:         len(revs),
			NumTSpends:         len(tspends),
			NumTAdds:           len(tadds),
			NumAll:             len(txs),
			LikelyMineable: exptypes.LikelyMineable{
				Total:         likelyTotal.ToCoin(),
				Size:          likelySize,
				FormattedSize: exptypes.BytesString(uint64(likelySize)),
				RegularTotal:  regularTotal.ToCoin(),
				TicketTotal:   ticketTotal.ToCoin(),
				VoteTotal:     voteTotal.ToCoin(),
				RevokeTotal:   revTotal.ToCoin(),
				TAddTotal:     taddTotal.ToCoin(),
				// TSpend mineability is complex, based on vote choices
				Count: numLikely,
			},
			LatestTransactions: latest,
			FormattedTotalSize: formattedSize,
			TicketIndexes:      ticketSpendInds,
			VotingInfo:         votingInfo,
			InvRegular:         invRegular,
			InvStake:           invStake,
		},
		Transactions: regular,
		Tickets:      tickets,
		Votes:        votes,
		Revocations:  revs,
		TSpends:      tspends,
		TAdds:        tadds,
	}

	return &mpInfo
}
