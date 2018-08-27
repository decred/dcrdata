package dbtypes

import (
	"fmt"

	"github.com/decred/dcrd/dcrutil"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/txhelpers"
)

// MsgBlockToDBBlock creates a dbtypes.Block from a wire.MsgBlock
func MsgBlockToDBBlock(msgBlock *wire.MsgBlock, chainParams *chaincfg.Params) *Block {
	// Create the dbtypes.Block structure
	blockHeader := msgBlock.Header

	// convert each transaction hash to a hex string
	var txHashStrs []string
	txHashes := msgBlock.TxHashes()
	for i := range txHashes {
		txHashStrs = append(txHashStrs, txHashes[i].String())
	}

	var stxHashStrs []string
	stxHashes := msgBlock.STxHashes()
	for i := range stxHashes {
		stxHashStrs = append(stxHashStrs, stxHashes[i].String())
	}

	// Assemble the block
	return &Block{
		Hash:       blockHeader.BlockHash().String(),
		Size:       uint32(msgBlock.SerializeSize()),
		Height:     blockHeader.Height,
		Version:    uint32(blockHeader.Version),
		MerkleRoot: blockHeader.MerkleRoot.String(),
		StakeRoot:  blockHeader.StakeRoot.String(),
		NumTx:      uint32(len(msgBlock.Transactions) + len(msgBlock.STransactions)),
		// nil []int64 for TxDbIDs
		NumRegTx:     uint32(len(msgBlock.Transactions)),
		Tx:           txHashStrs,
		NumStakeTx:   uint32(len(msgBlock.STransactions)),
		STx:          stxHashStrs,
		Time:         uint64(blockHeader.Timestamp.Unix()),
		Nonce:        uint64(blockHeader.Nonce),
		VoteBits:     blockHeader.VoteBits,
		FinalState:   blockHeader.FinalState[:],
		Voters:       blockHeader.Voters,
		FreshStake:   blockHeader.FreshStake,
		Revocations:  blockHeader.Revocations,
		PoolSize:     blockHeader.PoolSize,
		Bits:         blockHeader.Bits,
		SBits:        uint64(blockHeader.SBits),
		Difficulty:   txhelpers.GetDifficultyRatio(blockHeader.Bits, chainParams),
		ExtraData:    blockHeader.ExtraData[:],
		StakeVersion: blockHeader.StakeVersion,
		PreviousHash: blockHeader.PrevBlock.String(),
	}
}

// ChartGroupingToInterval converts the chartGrouping value to an actual time
// interval based on the gregorian calendar. AllChartGrouping returns 1 while
// the unknown chartGrouping returns -1 and an error. All the other time
// interval values is returned in terms of seconds.
func ChartGroupingToInterval(grouping ChartGrouping) (float64, error) {
	var hr = 3600.0
	switch grouping {
	case AllChartGrouping:
		return 1, nil

	case DayChartGrouping:
		return hr * 24, nil

	case WeekChartGrouping:
		return hr * 24 * 7, nil

	case MonthChartGrouping:
		return hr * 24 * 30.436875, nil

	case YearChartGrouping:
		return hr * 24 * 30.436875 * 12, nil

	default:
		return -1, fmt.Errorf(`unknown chart grouping "%d"`, grouping)
	}
}

// MergeTicketPoolPurchases merges the immature and live tickets distribution data
// needed to plot the tickets purchase distribution real time graph. Since
// most of the data in the arrays is ordered only the last 10 entries for the
// immature tickets data and first 10 live tickets data are used to merge the
// overlapping entries.
func MergeTicketPoolPurchases(immatureTickets PoolTicketsData, liveTickets PoolTicketsData) PoolTicketsData {
	var track = make(map[uint64]int)
	// defines the length from where immature ticket data sorting should start from.
	var count = len(immatureTickets.Time) - 10

	var unsortedTime = append(immatureTickets.Time[count:], liveTickets.Time[0:10]...)
	var unsortedImmature = append(immatureTickets.Immature[count:], liveTickets.Immature[0:10]...)
	var unsortedLive = append(immatureTickets.Live[count:], liveTickets.Live[0:10]...)
	var unsortedRawPrice = append(immatureTickets.RawPrice[count:], liveTickets.RawPrice[0:10]...)

	var sortedPrice []float64
	var sortedImmature, sortedLive, sortedTime, sortedRawPrice []uint64

	for i, foundTime := range unsortedTime {
		if val, ok := track[foundTime]; ok {
			totalRawPrice := unsortedRawPrice[i] + unsortedRawPrice[val]

			sortedLive[val] = unsortedLive[i]
			sortedPrice[val] = dcrutil.Amount(totalRawPrice /
				(unsortedLive[i] + unsortedImmature[val])).ToCoin()
		} else {
			track[foundTime] = len(sortedTime)
			sortedTime = append(sortedTime, foundTime)
			sortedImmature = append(sortedImmature, unsortedImmature[i])
			sortedLive = append(sortedLive, unsortedLive[i])
			sortedPrice = append(sortedPrice, dcrutil.Amount(unsortedRawPrice[i]/
				(unsortedLive[i]+unsortedImmature[i])).ToCoin())
			sortedRawPrice = append(sortedRawPrice, unsortedRawPrice[i])
		}
	}

	// Append the immature tickets sorted data at the start of the respective arrays.
	sortedTime = append(immatureTickets.Time[:count], sortedTime...)
	sortedImmature = append(immatureTickets.Immature[:count], sortedImmature...)
	sortedLive = append(immatureTickets.Live[:count], sortedLive...)
	sortedPrice = append(immatureTickets.Price[:count], sortedPrice...)

	// Append the live tickets sorted data at the end of the respective arrays and
	// return the sorted merged tickets purchase distribution data.
	return PoolTicketsData{
		Time:     append(sortedTime, liveTickets.Time[10:]...),
		Immature: append(sortedImmature, liveTickets.Immature[10:]...),
		Live:     append(sortedLive, liveTickets.Live[10:]...),
		Price:    append(sortedPrice, liveTickets.Price[10:]...),
	}
}
