// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	apitypes "github.com/decred/dcrdata/api/types/v3"
	"github.com/decred/dcrdata/txhelpers/v2"
)

// TxConverter converts dcrd-tx to insight tx
func (iapi *InsightApi) TxConverter(txs []*dcrjson.TxRawResult) ([]apitypes.InsightTx, error) {
	return iapi.DcrToInsightTxns(txs, false, false, false)
}

// DcrToInsightTxns converts a dcrjson TxRawResult to a InsightTx. The asm,
// scriptSig, and spending status may be skipped by setting the appropriate
// input arguments.
func (iapi *InsightApi) DcrToInsightTxns(txs []*dcrjson.TxRawResult, noAsm, noScriptSig, noSpent bool) ([]apitypes.InsightTx, error) {
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

			vinCoinbase := vin.IsCoinBase() || vin.IsStakeBase()
			txNew.IsCoinBase = txNew.IsCoinBase || vinCoinbase

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
			if !vinCoinbase {
				_, addresses, _, err := iapi.BlockData.ChainDB.AddressIDsByOutpoint(vin.Txid, vin.Vout)
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
		txNew.Fees = (vInSum - vOutSum).ToCoin()

		if !noSpent {
			// Populate the spending status of all vouts. Note: this only
			// gathers information from the database, which does not include
			// mempool transactions.
			addrFull, err := iapi.BlockData.ChainDB.SpendDetailsForFundingTx(txNew.Txid)
			if err != nil {
				return nil, err
			}
			for _, dbaddr := range addrFull {
				txNew.Vouts[dbaddr.FundingTxVoutIndex].SpentIndex = dbaddr.SpendingTxVinIndex
				txNew.Vouts[dbaddr.FundingTxVoutIndex].SpentTxID = dbaddr.SpendingTxHash
				txNew.Vouts[dbaddr.FundingTxVoutIndex].SpentHeight = dbaddr.BlockHeight
			}
		}
		newTxs = append(newTxs, txNew)
	}
	return newTxs, nil
}

// DcrToInsightBlock converts a dcrjson.GetBlockVerboseResult to Insight block.
func (iapi *InsightApi) DcrToInsightBlock(inBlocks []*dcrjson.GetBlockVerboseResult) ([]*apitypes.InsightBlockResult, error) {
	RewardAtBlock := func(blocknum int64, voters uint16) float64 {
		subsidyCache := blockchain.NewSubsidyCache(0, iapi.params)
		work := blockchain.CalcBlockWorkSubsidy(subsidyCache, blocknum, voters, iapi.params)
		stake := blockchain.CalcStakeVoteSubsidy(subsidyCache, blocknum, iapi.params) * int64(voters)
		tax := blockchain.CalcBlockTaxSubsidy(subsidyCache, blocknum, voters, iapi.params)
		return dcrutil.Amount(work + stake + tax).ToCoin()
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
