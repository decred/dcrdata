package dbtypes

import (
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/api/types"
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

// MsgBlockToDBBlockWithoutParams creates a dbtypes.Block from a wire.MsgBlock helper to parse without chain
// this method doesnt retunr difficulty
func MsgBlockToDBBlockWithoutParams(msgBlock *wire.MsgBlock) *Block {
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

	// Assemble the blockblockDB
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
		Difficulty:   float64(0),
		ExtraData:    blockHeader.ExtraData[:],
		StakeVersion: blockHeader.StakeVersion,
		PreviousHash: blockHeader.PrevBlock.String(),
	}
}

// TxConverter converts dcrd-tx to old-like type tx
func TxConverter(tx *dcrjson.TxRawResult) apitypes.TxRawResultNew {

	vInSum := float64(0)
	vOutSum := float64(0)

	// Build new model. Based on the old api responses of
	txNew := apitypes.TxRawResultNew{}
	txNew.Txid = tx.Txid
	txNew.Version = tx.Version
	txNew.Locktime = tx.LockTime
	txNew.Expiry = tx.Expiry

	// Vins fill
	for vinID, vin := range tx.Vin {
		vinEmpty := &apitypes.Vin{}
		emptySS := &apitypes.ScriptSig{}
		txNew.Vins = append(txNew.Vins, vinEmpty)
		txNew.Vins[vinID].Txid = vin.Txid
		txNew.Vins[vinID].Vout = vin.Vout
		txNew.Vins[vinID].Tree = vin.Tree
		txNew.Vins[vinID].Sequence = vin.Sequence
		txNew.Vins[vinID].Amountin = vin.AmountIn
		vInSum += vin.AmountIn
		txNew.Vins[vinID].Blockheight = vin.BlockHeight
		txNew.Vins[vinID].Blockindex = vin.BlockIndex
		txNew.Vins[vinID].Coinbase = vin.Coinbase
		txNew.Vins[vinID].Stakebase = vin.Stakebase
		// init ScriptPubKey
		txNew.Vins[vinID].ScriptPubKey = emptySS
		if vin.ScriptSig != nil {
			txNew.Vins[vinID].ScriptPubKey.Asm = vin.ScriptSig.Asm
			txNew.Vins[vinID].ScriptPubKey.Hex = vin.ScriptSig.Hex
		}
		txNew.Vins[vinID].N = vinID
		txNew.Vins[vinID].ValueSat = int64(txNew.Vins[vinID].Amountin * 100000000.0)
		txNew.Vins[vinID].Value = txNew.Vins[vinID].Amountin
	}

	// Vout fill
	emptySPKR := apitypes.ScriptPubKeyResult{}
	for _, v := range tx.Vout {
		voutEmpty := &apitypes.VoutNew{}
		txNew.Vout = append(txNew.Vout, voutEmpty)
		txNew.Vout[v.N].Value = v.Value
		vOutSum += v.Value
		txNew.Vout[v.N].N = v.N
		txNew.Vout[v.N].Version = v.Version
		// pk block
		txNew.Vout[v.N].ScriptPubKeyValue = emptySPKR
		txNew.Vout[v.N].ScriptPubKeyValue.Asm = v.ScriptPubKey.Asm
		txNew.Vout[v.N].ScriptPubKeyValue.Hex = v.ScriptPubKey.Hex
		txNew.Vout[v.N].ScriptPubKeyValue.ReqSigs = v.ScriptPubKey.ReqSigs
		txNew.Vout[v.N].ScriptPubKeyValue.Type = v.ScriptPubKey.Type
		txNew.Vout[v.N].ScriptPubKeyValue.Addresses = v.ScriptPubKey.Addresses
	}

	txNew.Blockhash = tx.BlockHash
	txNew.Blockheight = tx.BlockHeight
	txNew.Confirmations = tx.Confirmations
	txNew.Time = tx.Time
	txNew.Blocktime = tx.Blocktime

	txNew.ValueOut = vOutSum // vout value sum plus fees
	txNew.ValueIn = vInSum

	isStakeBase := false // is stakebase return true if value if stakeTree equal 1
	nextVinID := int(0)  // helper
	// Additional check for vouts
	for _, vout := range txNew.Vout {
		// commitment
		if vout.ScriptPubKeyValue.Type == "sstxcommitment" {
			vout.ScriptPubKeyValue.CommitAmt = txNew.Vins[nextVinID].Amountin
			nextVinID++
		}
		// stake base will true if spk contains value stakegen
		if vout.ScriptPubKeyValue.Type == "stakegen" {
			isStakeBase = true
			for _, vin := range txNew.Vins {
				if vin.Txid != "" {
					txNew.Ticketid = vin.Txid
				}
			}
		}

		if vout.ScriptPubKeyValue.Type == "sstxchange" || vout.ScriptPubKeyValue.Type == "stakesubmission" || vout.ScriptPubKeyValue.Type == "commitamt" {
			txNew.IsStakeTx = true
		}

		if vout.ScriptPubKeyValue.Type == "stakerevoke" {
			txNew.IsStakeRtx = true
			for _, vin := range txNew.Vins {
				if vin.Txid != "" {
					txNew.Ticketid = vin.Txid
				}
			}
		}
	}

	// Return true if stakebase value is not empty or is stacke base true
	if (txNew.Vins != nil && len(txNew.Vins[0].Stakebase) > 0) || isStakeBase {
		txNew.IsStakeGen = true
	}

	// Return true if coinbase value is not empty
	if txNew.Vins != nil && len(txNew.Vins[0].Coinbase) > 0 {
		txNew.IsCoinBase = true
	}

	return txNew
}
