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

// insightVinV4 is a breaking modification to api/types/v3.InsightVin, but when
// marshalled, it can be unmarshalled to api/types/v3.InsightVin. This is used
// so we do not have to make numerous major version bumps to the modules used in
// dcrdata v5.0.x. The api/types/v4 module will be used in dcrdata v5.1.
type insightVinV4 struct {
	Txid      string                     `json:"txid,omitempty"`
	Vout      *uint32                    `json:"vout,omitempty"`
	Sequence  *uint32                    `json:"sequence,omitempty"`
	N         int                        `json:"n"`
	ScriptSig *apitypes.InsightScriptSig `json:"scriptSig,omitempty"`
	Addr      string                     `json:"addr,omitempty"`
	ValueSat  int64                      `json:"valueSat"`
	Value     float64                    `json:"value"`
	CoinBase  string                     `json:"coinbase,omitempty"`
}

type insightTxV4 struct {
	Txid          string                  `json:"txid,omitempty"`
	Version       int32                   `json:"version,omitempty"`
	Locktime      uint32                  `json:"locktime"`
	IsCoinBase    bool                    `json:"isCoinBase,omitempty"`
	Vins          []*insightVinV4         `json:"vin,omitempty"`
	Vouts         []*apitypes.InsightVout `json:"vout,omitempty"`
	Blockhash     string                  `json:"blockhash,omitempty"`
	Blockheight   int64                   `json:"blockheight"`
	Confirmations int64                   `json:"confirmations"`
	Time          int64                   `json:"time,omitempty"`
	Blocktime     int64                   `json:"blocktime,omitempty"`
	ValueOut      float64                 `json:"valueOut,omitempty"`
	Size          uint32                  `json:"size,omitempty"`
	ValueIn       float64                 `json:"valueIn,omitempty"`
	Fees          float64                 `json:"fees,omitempty"`
}

// txConverter converts dcrd-tx to insight tx using an internal output type that
// matches api/types/v4.InsightTx.
func (iapi *InsightApi) txConverter(txs []*dcrjson.TxRawResult) ([]insightTxV4, error) {
	return iapi.dcrToInsightTxns(txs, false, false, false)
}

// dcrToInsightTxns converts a dcrjson TxRawResult to a InsightTx. The asm,
// scriptSig, and spending status may be skipped by setting the appropriate
// input arguments.
func (iapi *InsightApi) dcrToInsightTxns(txs []*dcrjson.TxRawResult, noAsm, noScriptSig, noSpent bool) ([]insightTxV4, error) {
	newTxs := make([]insightTxV4, 0, len(txs))
	for _, tx := range txs {
		// Build new InsightTx
		txNew := insightTxV4{
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

			InsightVin := &insightVinV4{
				Txid:     vin.Txid,
				Vout:     newUint32Ptr(vin.Vout),
				Sequence: newUint32Ptr(vin.Sequence),
				N:        vinID,
				Value:    vin.AmountIn,
				ValueSat: int64(amt),
				CoinBase: vin.Coinbase,
			}

			// Identify a vin corresponding to generated coins.
			vinGenerated := vin.IsCoinBase() || vin.IsStakeBase()
			txNew.IsCoinBase = txNew.IsCoinBase || vin.IsCoinBase() // exclude stakebase

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
			addrFull, err := iapi.BlockData.ChainDB.SpendDetailsForFundingTx(txNew.Txid)
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
				Vout:     vin.Vout,
				Sequence: vin.Sequence,
				N:        vinID,
				Value:    vin.AmountIn,
				ValueSat: int64(amt),
				CoinBase: vin.Coinbase,
			}

			// Identify a vin corresponding to generated coins.
			vinGenerated := vin.IsCoinBase() || vin.IsStakeBase()
			txNew.IsCoinBase = txNew.IsCoinBase || vin.IsCoinBase() // exclude stakebase

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
			addrFull, err := iapi.BlockData.ChainDB.SpendDetailsForFundingTx(txNew.Txid)
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
