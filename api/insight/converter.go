// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"math"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	apitypes "github.com/decred/dcrdata/api/types"
)

// TxConverter converts dcrd-tx to insight tx
func (c *insightApiContext) TxConverter(txs []*dcrjson.TxRawResult) ([]apitypes.InsightTx, error) {
	return c.TxConverterWithParams(txs, false, false, false)
}

// TxConverterWithParams takes struct with filter params
func (c *insightApiContext) TxConverterWithParams(txs []*dcrjson.TxRawResult, noAsm bool, noScriptSig bool, noSpent bool) ([]apitypes.InsightTx, error) {
	newTxs := []apitypes.InsightTx{}
	for _, tx := range txs {

		vInSum := float64(0)
		vOutSum := float64(0)

		// Build new model. Based on the old api responses of
		txNew := apitypes.InsightTx{}
		txNew.Txid = tx.Txid
		txNew.Version = tx.Version
		txNew.Locktime = tx.LockTime

		// Vins fill
		for vinID, vin := range tx.Vin {
			vinEmpty := &apitypes.InsightVin{}
			emptySS := &apitypes.InsightScriptSig{}
			txNew.Vins = append(txNew.Vins, vinEmpty)
			txNew.Vins[vinID].Txid = vin.Txid
			txNew.Vins[vinID].Vout = vin.Vout
			txNew.Vins[vinID].Sequence = vin.Sequence

			txNew.Vins[vinID].CoinBase = vin.Coinbase

			// init ScriptPubKey
			if !noScriptSig {
				txNew.Vins[vinID].ScriptSig = emptySS
				if vin.ScriptSig != nil {
					if !noAsm {
						txNew.Vins[vinID].ScriptSig.Asm = vin.ScriptSig.Asm
					}
					txNew.Vins[vinID].ScriptSig.Hex = vin.ScriptSig.Hex
				}
			}

			txNew.Vins[vinID].N = vinID

			txNew.Vins[vinID].Value = vin.AmountIn

			// Lookup addresses OPTION 2
			// Note, this only gathers information from the database which does not include mempool transactions
			_, addresses, value, err := c.BlockData.ChainDB.RetrieveAddressIDsByOutpoint(vin.Txid, vin.Vout)
			if err == nil {
				if len(addresses) > 0 {
					// Update Vin due to DCRD AMOUNTIN - START
					// NOTE THIS IS ONLY USEFUL FOR INPUT AMOUNTS THAT ARE NOT ALSO FROM MEMPOOL
					if tx.Confirmations == 0 {
						txNew.Vins[vinID].Value = dcrutil.Amount(value).ToCoin()
					}
					// Update Vin due to DCRD AMOUNTIN - END
					txNew.Vins[vinID].Addr = addresses[0]
				}
			}
			dcramt, _ := dcrutil.NewAmount(txNew.Vins[vinID].Value)
			txNew.Vins[vinID].ValueSat = int64(dcramt)
			vInSum += txNew.Vins[vinID].Value

		}

		// Vout fill
		for _, v := range tx.Vout {
			voutEmpty := &apitypes.InsightVout{}
			emptyPubKey := apitypes.InsightScriptPubKey{}
			txNew.Vouts = append(txNew.Vouts, voutEmpty)
			txNew.Vouts[v.N].Value = v.Value
			vOutSum += v.Value
			txNew.Vouts[v.N].N = v.N
			// pk block
			txNew.Vouts[v.N].ScriptPubKey = emptyPubKey
			if !noAsm {
				txNew.Vouts[v.N].ScriptPubKey.Asm = v.ScriptPubKey.Asm
			}
			txNew.Vouts[v.N].ScriptPubKey.Hex = v.ScriptPubKey.Hex
			txNew.Vouts[v.N].ScriptPubKey.Type = v.ScriptPubKey.Type
			txNew.Vouts[v.N].ScriptPubKey.Addresses = v.ScriptPubKey.Addresses
		}

		txNew.Blockhash = tx.BlockHash
		txNew.Blockheight = tx.BlockHeight
		txNew.Confirmations = tx.Confirmations
		txNew.Time = tx.Time
		txNew.Blocktime = tx.Blocktime
		txNew.Size = uint32(len(tx.Hex) / 2)

		dcramt, _ := dcrutil.NewAmount(vOutSum)
		txNew.ValueOut = dcramt.ToCoin()

		dcramt, _ = dcrutil.NewAmount(vInSum)
		txNew.ValueIn = dcramt.ToCoin()

		dcramt, _ = dcrutil.NewAmount(txNew.ValueIn - txNew.ValueOut)
		txNew.Fees = dcramt.ToCoin()

		// Return true if coinbase value is not empty, return 0 at some fields
		if txNew.Vins != nil && len(txNew.Vins[0].CoinBase) > 0 {
			txNew.IsCoinBase = true
			txNew.ValueIn = 0
			txNew.Fees = 0
			for _, v := range txNew.Vins {
				v.Value = 0
				v.ValueSat = 0
			}
		}

		if !noSpent {

			// set of unique addresses for db query
			uniqAddrs := make(map[string]string)

			for _, vout := range txNew.Vouts {
				for _, addr := range vout.ScriptPubKey.Addresses {
					uniqAddrs[addr] = txNew.Txid
				}
			}

			addresses := []string{}
			for addr := range uniqAddrs {
				addresses = append(addresses, addr)
			}
			// Note, this only gathers information from the database which does not include mempool transactions
			addrFull := c.BlockData.ChainDB.GetAddressSpendByFunHash(addresses, txNew.Txid)
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

// BlockConverter converts dcrd-block to insight block
func (c *insightApiContext) BlockConverter(inBlocks []*dcrjson.GetBlockVerboseResult) ([]*apitypes.InsightBlockResult, error) {
	params := &chaincfg.MainNetParams

	RewardAtBlock := func(blocknum int64, voters uint16) float64 {
		// Use DCRD package
		// subsidyCache := blockchain.NewSubsidyCache(0, params)
		// work := blockchain.CalcBlockWorkSubsidy(subsidyCache, blocknum,
		// 	voters, params)
		// stake := blockchain.CalcStakeVoteSubsidy(subsidyCache, blocknum,
		// 	params) * voters
		// tax := blockchain.CalcBlockTaxSubsidy(subsidyCache, blocknum,
		// 	voters, params)
		// return work + stake + tax

		// Use Calculation
		epoch := math.Floor(float64(blocknum) / float64(params.SubsidyReductionInterval))
		StakeRewardProportion := (float64(voters) / float64(params.TicketsPerBlock)) * float64(params.StakeRewardProportion)
		TotalRewardProportion := ((float64(params.WorkRewardProportion) +
			float64(params.BlockTaxProportion) + StakeRewardProportion) / 10)
		TotalReward := TotalRewardProportion * dcrutil.Amount(params.BaseSubsidy).ToCoin() *
			math.Pow(float64(params.MulSubsidy)/float64(params.DivSubsidy), epoch)
		return math.Round(TotalReward/0.00000001) * 0.00000001
	}

	outBlocks := make([]*apitypes.InsightBlockResult, 0)
	for _, inBlock := range inBlocks {
		var outBlock apitypes.InsightBlockResult
		outBlock.Hash = inBlock.Hash
		outBlock.Confirmations = inBlock.Confirmations
		outBlock.Size = inBlock.Size
		outBlock.Height = inBlock.Height
		outBlock.Version = inBlock.Version
		outBlock.MerkleRoot = inBlock.MerkleRoot
		outBlock.Tx = append(inBlock.Tx, inBlock.STx...)
		outBlock.Time = inBlock.Time
		outBlock.Nonce = inBlock.Nonce
		outBlock.Bits = inBlock.Bits
		outBlock.Difficulty = inBlock.Difficulty
		outBlock.PreviousHash = inBlock.PreviousHash
		outBlock.Reward = RewardAtBlock(inBlock.Height, inBlock.Voters)
		if inBlock.Height > 0 {
			outBlock.IsMainChain = true
		} else {
			outBlock.IsMainChain = false
		}
		outBlocks = append(outBlocks, &outBlock)

	}
	return outBlocks, nil
}
