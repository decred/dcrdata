// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package mempool

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/rpcclient"
	apitypes "github.com/decred/dcrdata/v4/api/types"
	exptypes "github.com/decred/dcrdata/v4/explorer/types"
	"github.com/decred/dcrdata/v4/rpcutils"
	"github.com/decred/dcrdata/v4/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

// MempoolDataCollector is used for retrieving and processing data from a chain
// server's mempool.
type MempoolDataCollector struct {
	// Mutex is used to prevent multiple concurrent calls to Collect.
	sync.Mutex
	dcrdChainSvr *rpcclient.Client
	activeChain  *chaincfg.Params
}

// NewMempoolDataCollector creates a new MempoolDataCollector.
func NewMempoolDataCollector(dcrdChainSvr *rpcclient.Client, params *chaincfg.Params) *MempoolDataCollector {
	return &MempoolDataCollector{
		dcrdChainSvr: dcrdChainSvr,
		activeChain:  params,
	}
}

// mempoolTxns retrieves all transactions and returns them as a
// []exptypes.MempoolTx. See also ParseTxns, which may process this slice.
func (t *MempoolDataCollector) mempoolTxns() ([]exptypes.MempoolTx, error) {
	mempooltxs, err := t.dcrdChainSvr.GetRawMempoolVerbose(dcrjson.GRMAll)
	if err != nil {
		return nil, fmt.Errorf("GetRawMempoolVerbose failed: %v", err)
	}

	blockHash, _, err := t.dcrdChainSvr.GetBestBlock()
	if err != nil {
		return nil, err
	}
	blockhash := blockHash.String()

	txs := make([]exptypes.MempoolTx, 0, len(mempooltxs))

	for hash, tx := range mempooltxs {
		rawtx, err := rpcutils.GetTransactionVerboseByID(t.dcrdChainSvr, hash)
		if err != nil {
			log.Warn(err)
			continue
		}

		if rawtx == nil {
			log.Errorf("Failed to get mempool transaction %s.", hash)
			continue
		}

		msgTx, err := txhelpers.MsgTxFromHex(rawtx.Hex)
		if err != nil {
			log.Errorf("Failed to decode transaction hex: %v", err)
			continue
		}

		totalOut := 0.0
		for _, v := range rawtx.Vout {
			totalOut += v.Value
		}

		var voteInfo *exptypes.VoteInfo
		if ok := stake.IsSSGen(msgTx); ok {
			validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(msgTx, t.activeChain)
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
				voteInfo.ForLastBlock = voteInfo.VotesOnBlock(blockhash)
			}
		}

		// Note that the fee computed from msgTx will be a large negative fee
		// for coinbase transactions.
		//fee, _ := txhelpers.TxFeeRate(msgTx)
		// log.Tracef("tx fee: GRM result = %f, msgTx = %f", tx.Fee, fee.ToCoin())

		txs = append(txs, exptypes.MempoolTx{
			TxID:      msgTx.TxHash().String(),
			Fees:      tx.Fee,
			VinCount:  len(msgTx.TxIn),
			VoutCount: len(msgTx.TxOut),
			Vin:       exptypes.MsgTxMempoolInputs(msgTx),
			Coinbase:  blockchain.IsCoinBaseTx(msgTx),
			Hash:      hash,
			Time:      tx.Time,
			Size:      tx.Size,
			TotalOut:  totalOut,
			Type:      txhelpers.DetermineTxTypeString(msgTx),
			VoteInfo:  voteInfo,
		})
	}

	return txs, nil
}

// Collect is the main handler for collecting mempool data. Data collection is
// focused on stake-related information, including vote and ticket transactions,
// and fee info. Transactions of all types in mempool are returned as a
// []exptypes.MempoolTx, corresponding to the same data provided by the
// unexported mempoolTxns method.
func (t *MempoolDataCollector) Collect() (*StakeData, []exptypes.MempoolTx, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.Lock()
	defer t.Unlock()

	// Time this function
	defer func(start time.Time) {
		log.Debugf("MempoolDataCollector.Collect() completed in %v",
			time.Since(start))
	}(time.Now())

	// client
	c := t.dcrdChainSvr

	// Get a map of ticket hashes to getrawmempool results
	// mempoolTickets[ticketHashes[0].String()].Fee
	mempoolTickets, err := c.GetRawMempoolVerbose(dcrjson.GRMTickets)
	if err != nil {
		return nil, nil, err
	}

	mempoolVotes, err := c.GetRawMempoolVerbose(dcrjson.GRMVotes)
	if err != nil {
		return nil, nil, err
	}
	numVotes := len(mempoolVotes)

	// Grab the current stake difficulty (ticket price).
	stakeDiff, err := c.GetStakeDifficulty()
	if err != nil {
		return nil, nil, err
	}

	hash, height, err := c.GetBestBlock()
	if err != nil {
		return nil, nil, err
	}
	header, err := c.GetBlockHeaderVerbose(hash)
	if err != nil {
		return nil, nil, err
	}
	blockTime := header.Time

	// Fee info
	var numFeeWindows, numFeeBlocks uint32 = 0, 0
	feeInfo, err := c.TicketFeeInfo(&numFeeBlocks, &numFeeWindows)
	if err != nil {
		return nil, nil, err
	}

	// All transactions in mempool.
	allTxns, err := t.mempoolTxns()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()

	// Make slice of TicketDetails
	N := len(mempoolTickets)
	allTicketsDetails := make(TicketsDetails, 0, N)
	for hash, t := range mempoolTickets {
		//ageSec := time.Since(time.Unix(t.Time, 0)).Seconds()
		// Compute fee in DCR / kB
		feeRate := t.Fee / float64(t.Size) * 1000
		allTicketsDetails = append(allTicketsDetails, &apitypes.TicketDetails{
			Hash:    hash,
			Fee:     t.Fee,
			FeeRate: feeRate,
			Size:    t.Size,
			Height:  t.Height,
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

	return mpoolData, allTxns, err
}

// NumLatestMempoolTxns is the maximum number of mempool transactions that will
// be stored in the LatestTransactions field of the MempoolInfo generated by
// ParseTxns.
const NumLatestMempoolTxns = 5

// ParseTxns analyzes the mempool transactions in the txs slice, and generates a
// MempoolInfo summary with categorized transactions.
func ParseTxns(txs []exptypes.MempoolTx, params *chaincfg.Params, lastBlock *BlockID) *exptypes.MempoolInfo {
	// Get the NumLatestMempoolTxns latest transactions in mempool
	var latest []exptypes.MempoolTx
	sort.Sort(exptypes.MPTxsByTime(txs))
	if len(txs) > NumLatestMempoolTxns {
		latest = txs[:NumLatestMempoolTxns]
	} else {
		latest = txs
	}

	tickets := make([]exptypes.MempoolTx, 0)
	votes := make([]exptypes.MempoolTx, 0)
	revs := make([]exptypes.MempoolTx, 0)
	regular := make([]exptypes.MempoolTx, 0)

	var regularTotal, ticketTotal, voteTotal, revTotal float64
	var likelyMineable bool

	var totalOut, likelyTotal float64
	var totalSize int32
	votingInfo := exptypes.NewVotingInfo(params.TicketsPerBlock)
	invRegular := make(map[string]struct{})
	invStake := make(map[string]struct{})

	blockhash := lastBlock.Hash.String()

	// Initialize the BlockValidatorIndex, a map.
	var latestTime int64
	ticketSpendInds := make(exptypes.BlockValidatorIndex)
	for _, tx := range txs {
		likelyMineable = true
		switch tx.Type {
		case "Ticket":
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			ticketTotal += tx.TotalOut
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
			tx.VoteInfo.SetTicketIndex(ticketSpendInds)
			// Determine if this vote is (in)validating the previous block.
			tx.VoteInfo.ForLastBlock = tx.VoteInfo.VotesOnBlock(blockhash)
			// Update tally if this is for the previous block, the ticket has not
			// yet been spent. Do not attempt to decide block validity.
			if tx.VoteInfo.ForLastBlock && !votingInfo.VotedTickets[tx.VoteInfo.TicketSpent] {
				votingInfo.VotedTickets[tx.VoteInfo.TicketSpent] = true
				votingInfo.TicketsVoted++
				voteTotal += tx.TotalOut
				votingInfo.Tally(tx.VoteInfo)
			} else {
				likelyMineable = false
			}
		case "Revocation":
			if _, found := invStake[tx.Hash]; found {
				continue
			}
			revTotal += tx.TotalOut
			invStake[tx.Hash] = struct{}{}
			revs = append(revs, tx)
		default:
			if _, found := invRegular[tx.Hash]; found {
				continue
			}
			regularTotal += tx.TotalOut
			invRegular[tx.Hash] = struct{}{}
			regular = append(regular, tx)
		}

		// Update mempool totals
		if likelyMineable {
			likelyTotal += tx.TotalOut
		}
		totalOut += tx.TotalOut
		totalSize += tx.Size

		if latestTime < tx.Time {
			latestTime = tx.Time
		}
	}

	sort.Sort(exptypes.MPTxsByHeight(votes))
	formattedSize := humanize.Bytes(uint64(totalSize))

	// Store mempool data for template rendering
	mpInfo := exptypes.MempoolInfo{
		MempoolShort: exptypes.MempoolShort{
			LastBlockHeight:    lastBlock.Height,
			LastBlockHash:      blockhash,
			LastBlockTime:      lastBlock.Time,
			Time:               latestTime,
			TotalOut:           totalOut,
			LikelyTotal:        likelyTotal,
			RegularTotal:       regularTotal,
			TicketTotal:        ticketTotal,
			VoteTotal:          voteTotal,
			RevokeTotal:        revTotal,
			TotalSize:          totalSize,
			NumTickets:         len(tickets),
			NumVotes:           len(votes),
			NumRegular:         len(regular),
			NumRevokes:         len(revs),
			NumAll:             len(txs),
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
	}

	return &mpInfo
}
