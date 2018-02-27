package dbtypes

import (
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
