// Copyright (c) 2018-2022, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"math"

	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"

	apitypes "github.com/decred/dcrdata/v8/api/types"
	"github.com/decred/dcrdata/v8/txhelpers"
)

// TxConverter converts dcrd-tx to insight tx
func (iapi *InsightApi) TxConverter(txs []*chainjson.TxRawResult) ([]apitypes.InsightTx, error) {
	return iapi.DcrToInsightTxns(txs, false, false, false)
}

// DcrToInsightTxns converts a chainjson TxRawResult to a InsightTx. The asm,
// scriptSig, and spending status may be skipped by setting the appropriate
// input arguments.
func (iapi *InsightApi) DcrToInsightTxns(txs []*chainjson.TxRawResult, noAsm, noScriptSig, noSpent bool) ([]apitypes.InsightTx, error) {
	newTxs := make([]apitypes.InsightTx, 0, len(txs))
	for _, tx := range txs {
		// Build new InsightTx
		txNew := apitypes.InsightTx{
			Txid:          tx.Txid,
			Version:       tx.Version,
			Locktime:      tx.LockTime,
			Blockhash:     tx.BlockHash,
			Blockheight:   tx.BlockHeight,
			Confirmations: tx.Confirmations,
			Time:          tx.Time,
			Blocktime:     tx.Blocktime,
			Size:          uint32(len(tx.Hex) / 2),
		}

		// Vins
		var vInSum dcrutil.Amount
		for vinID, vin := range tx.Vin {
			amt, _ := dcrutil.NewAmount(vin.AmountIn)
			vInSum += amt

			InsightVin := &apitypes.InsightVin{
				Txid:     vin.Txid,
				Vout:     newUint32Ptr(vin.Vout),
				Sequence: newUint32Ptr(vin.Sequence),
				N:        vinID,
				Value:    vin.AmountIn,
				ValueSat: int64(amt),
				CoinBase: vin.Coinbase,
			}

			// Identify a vin corresponding to generated coins. This is kinda
			// dumb because only the 0th input will be generated.
			coinbase, treasurybase := vin.IsCoinBase(), vin.Treasurybase
			vinGenerated := coinbase || vin.IsStakeBase() || treasurybase
			txNew.IsTreasurybase = vin.Treasurybase || treasurybase
			txNew.IsCoinBase = txNew.IsCoinBase || coinbase // exclude stakebase
			// NOTE: coinbase transactions should only have one input, so the
			// above is a bit weird.

			// init ScriptPubKey
			if !noScriptSig {
				InsightVin.ScriptSig = new(apitypes.InsightScriptSig)
				if vin.ScriptSig != nil {
					if !noAsm {
						InsightVin.ScriptSig.Asm = vin.ScriptSig.Asm
					}
					InsightVin.ScriptSig.Hex = vin.ScriptSig.Hex
				}
			}

			// First, attempt to get input addresses from our DB, which should
			// work if the funding transaction is confirmed. Otherwise use RPC
			// to get the funding transaction outpoint addresses.
			if !vinGenerated {
				_, addresses, _, err := iapi.BlockData.AddressIDsByOutpoint(vin.Txid, vin.Vout)
				if err == nil && len(addresses) > 0 {
					InsightVin.Addr = addresses[0]
				} else {
					// If the previous outpoint is from an unconfirmed transaction,
					// fetch the prevout's addresses since dcrd does not include
					// these details in the info for the spending transaction.
					addrs, _, err := txhelpers.OutPointAddressesFromString(
						vin.Txid, vin.Vout, vin.Tree, iapi.nodeClient, iapi.params)
					if err != nil {
						apiLog.Errorf("OutPointAddresses: %v", err)
					} else if len(addrs) > 0 {
						InsightVin.Addr = addrs[0]
					}
				}
			}

			txNew.Vins = append(txNew.Vins, InsightVin)
		}

		// Vouts
		var vOutSum dcrutil.Amount
		for _, v := range tx.Vout {
			InsightVout := &apitypes.InsightVout{
				Value: v.Value,
				N:     v.N,
				ScriptPubKey: apitypes.InsightScriptPubKey{
					Addresses: v.ScriptPubKey.Addresses,
					Type:      v.ScriptPubKey.Type,
					Hex:       v.ScriptPubKey.Hex,
				},
			}

			if !noAsm {
				InsightVout.ScriptPubKey.Asm = v.ScriptPubKey.Asm
			}

			amt, _ := dcrutil.NewAmount(v.Value)
			vOutSum += amt

			txNew.Vouts = append(txNew.Vouts, InsightVout)
		}

		txNew.ValueIn = vInSum.ToCoin()
		txNew.ValueOut = vOutSum.ToCoin()
		// A coinbase transaction, but not necessarily a stakebase/vote, has no
		// fee. Further, a coinbase transaction collects all of the mining fees
		// for the transactions in the block, but there is no input that
		// accounts for these collected fees. As such, never attempt to compute
		// a transaction fee for the block's coinbase transaction.
		if !txNew.IsCoinBase {
			txNew.Fees = (vInSum - vOutSum).ToCoin()
		}

		if !noSpent {
			// Populate the spending status of all vouts. Note: this only
			// gathers information from the database, which does not include
			// mempool transactions.
			addrFull, err := iapi.BlockData.SpendDetailsForFundingTx(txNew.Txid)
			if err != nil {
				return nil, err
			}
			for _, dbAddr := range addrFull {
				txNew.Vouts[dbAddr.FundingTxVoutIndex].SpentIndex = dbAddr.SpendingTxVinIndex
				txNew.Vouts[dbAddr.FundingTxVoutIndex].SpentTxID = dbAddr.SpendingTxHash
				txNew.Vouts[dbAddr.FundingTxVoutIndex].SpentHeight = dbAddr.BlockHeight
			}
		}
		newTxs = append(newTxs, txNew)
	}
	return newTxs, nil
}

// DcrToInsightBlock converts a chainjson.GetBlockVerboseResult to Insight block.
func (iapi *InsightApi) DcrToInsightBlock(inBlocks []*chainjson.GetBlockVerboseResult) ([]*apitypes.InsightBlockResult, error) {
	dcp0010Height := iapi.BlockData.DCP0010ActivationHeight()
	if dcp0010Height == -1 {
		dcp0010Height = math.MaxInt64
	}
	dcp0012Height := iapi.BlockData.DCP0012ActivationHeight()
	if dcp0012Height == -1 {
		dcp0012Height = math.MaxInt64
	}

	RewardAtBlock := func(blocknum int64, voters uint16) float64 {
		ssv := standalone.SSVOriginal
		if blocknum >= dcp0012Height {
			ssv = standalone.SSVDCP0012
		} else if blocknum >= dcp0010Height {
			ssv = standalone.SSVDCP0010
		}
		work, stake, tax := txhelpers.RewardsAtBlock(blocknum, voters, iapi.params, ssv)
		return dcrutil.Amount(work + stake*int64(voters) + tax).ToCoin()
	}

	outBlocks := make([]*apitypes.InsightBlockResult, 0, len(inBlocks))
	for _, inBlock := range inBlocks {
		outBlock := apitypes.InsightBlockResult{
			Hash:          inBlock.Hash,
			Confirmations: inBlock.Confirmations,
			Size:          inBlock.Size,
			Height:        inBlock.Height,
			Version:       inBlock.Version,
			MerkleRoot:    inBlock.MerkleRoot,
			Tx:            append(inBlock.Tx, inBlock.STx...),
			Time:          inBlock.Time,
			Nonce:         inBlock.Nonce,
			Bits:          inBlock.Bits,
			Difficulty:    inBlock.Difficulty,
			PreviousHash:  inBlock.PreviousHash,
			NextHash:      inBlock.NextHash,
			Reward:        RewardAtBlock(inBlock.Height, inBlock.Voters),
			IsMainChain:   inBlock.Height > 0,
		}
		outBlocks = append(outBlocks, &outBlock)
	}
	return outBlocks, nil
}
