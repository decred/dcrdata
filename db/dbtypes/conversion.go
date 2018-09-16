package dbtypes

import (
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v3/txhelpers"
	"github.com/saintfish/chardet"
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

// DetectCharset first extracts data in bytes from the provided hex string.
// From the extracted bytes, it establishes the possible file encoding used,
// Language, applicable charset and confidence value on how the system expects
// the detected values to be true. Its also returns the extracted data in bytes.
func DetectCharset(hexString string) (*CharsetType, error) {
	charSet := new(CharsetType)
	v := []byte(hexString)

	dst := make([]byte, hex.DecodedLen(len(v)))
	_, err := hex.Decode(dst, v)
	if err != nil {
		return nil, fmt.Errorf("detecting contentType failed: %v", err)
	}

	charSet.Data = dst
	charSet.ContentType = http.DetectContentType(dst)

	result, err := chardet.NewTextDetector().DetectBest(dst)
	if err != nil {
		return nil, fmt.Errorf("detecting charset failed: %v", err)
	}

	charSet.Charset = result.Charset
	charSet.Language = result.Language
	charSet.Confidence = uint32(result.Confidence)

	return charSet, nil
}
