package dbtypes

import (
	"fmt"
	"math"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"

	"github.com/decred/dcrdata/v8/txhelpers"
)

// MsgBlockToDBBlock creates a dbtypes.Block from a wire.MsgBlock
func MsgBlockToDBBlock(msgBlock *wire.MsgBlock, chainParams *chaincfg.Params, chainWork string, winners []string) *Block {
	// Create the dbtypes.Block structure
	blockHeader := msgBlock.Header

	// convert each transaction hash to a hex string
	txHashStrs := make([]string, 0, len(msgBlock.Transactions))
	for _, tx := range msgBlock.Transactions {
		txHashStrs = append(txHashStrs, tx.CachedTxHash().String())
	}

	stxHashStrs := make([]string, 0, len(msgBlock.STransactions))
	for _, tx := range msgBlock.STransactions {
		stxHashStrs = append(stxHashStrs, tx.CachedTxHash().String())
	}

	// Assemble the block
	return &Block{
		Hash:    blockHeader.BlockHash().String(),
		Size:    uint32(msgBlock.SerializeSize()),
		Height:  blockHeader.Height,
		Version: uint32(blockHeader.Version),
		NumTx:   uint32(len(msgBlock.Transactions) + len(msgBlock.STransactions)),
		// nil []int64 for TxDbIDs
		NumRegTx:     uint32(len(msgBlock.Transactions)),
		Tx:           txHashStrs,
		NumStakeTx:   uint32(len(msgBlock.STransactions)),
		STx:          stxHashStrs,
		Time:         NewTimeDef(blockHeader.Timestamp),
		Nonce:        uint64(blockHeader.Nonce),
		VoteBits:     blockHeader.VoteBits,
		Voters:       blockHeader.Voters,
		FreshStake:   blockHeader.FreshStake,
		Revocations:  blockHeader.Revocations,
		PoolSize:     blockHeader.PoolSize,
		Bits:         blockHeader.Bits,
		SBits:        uint64(blockHeader.SBits),
		Difficulty:   txhelpers.GetDifficultyRatio(blockHeader.Bits, chainParams),
		StakeVersion: blockHeader.StakeVersion,
		PreviousHash: blockHeader.PrevBlock.String(),
		ChainWork:    chainWork,
		Winners:      winners,
	}
}

// TimeBasedGroupingToInterval converts the TimeBasedGrouping value to an actual
// time value in seconds based on the gregorian calendar except AllGrouping that
// returns 1 while the unknownGrouping returns -1 and an error.
func TimeBasedGroupingToInterval(grouping TimeBasedGrouping) (float64, error) {
	var hr = 3600.0
	// days per month value is obtained by 365/12
	var daysPerMonth = 30.416666666666668
	switch grouping {
	case AllGrouping:
		return 1, nil

	case DayGrouping:
		return hr * 24, nil

	case WeekGrouping:
		return hr * 24 * 7, nil

	case MonthGrouping:
		return hr * 24 * daysPerMonth, nil

	case YearGrouping:
		return hr * 24 * daysPerMonth * 12, nil

	default:
		return -1, fmt.Errorf(`unknown grouping "%d"`, grouping)
	}
}

// CalculateHashRate calculates the hashrate from the difficulty value and
// the targetTimePerBlock in seconds. The hashrate returned is in form PetaHash
// per second (PH/s).
func CalculateHashRate(difficulty, targetTimePerBlock float64) float64 {
	return ((difficulty * math.Pow(2, 32)) / targetTimePerBlock) / 1000000
}

// CalculateWindowIndex calculates the window index from the quotient of a block
// height and the chainParams.StakeDiffWindowSize.
func CalculateWindowIndex(height, stakeDiffWindowSize int64) int64 {
	// A window being a group of blocks whose count is defined by
	// chainParams.StakeDiffWindowSize, the first window starts from block 1 to
	// block 144 instead of block 0 to block 143. To obtain the accurate window
	// index value, we should add 1 to the quotient obtained by dividing the block
	// height with the chainParams.StakeDiffWindowSize value; if the float precision
	// is greater than zero. The precision is equal to zero only when the block
	// height value is divisible by the window size.
	windowVal := float64(height) / float64(stakeDiffWindowSize)
	index := int64(windowVal)
	if windowVal != math.Floor(windowVal) || windowVal == 0 {
		index++
	}
	return index
}
