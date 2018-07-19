// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	apitypes "github.com/decred/dcrdata/api/types"
)

// TxConverter converts dcrd-tx to insight tx
func (c *insightApiContext) TxConverter(txs []*dcrjson.TxRawResult) ([]apitypes.InsightTx, error) {
	return c.DcrToInsightTxns(txs, false, false, false)
}

// DcrToInsightTxns takes struct with filter params
func (c *insightApiContext) DcrToInsightTxns(txs []*dcrjson.TxRawResult,
	noAsm, noScriptSig, noSpent bool) ([]apitypes.InsightTx, error) {
	var newTxs []apitypes.InsightTx
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

		// Vins fill
		var vInSum, vOutSum float64

		for vinID, vin := range tx.Vin {

			InsightVin := &apitypes.InsightVin{
				Txid:     vin.Txid,
				Vout:     vin.Vout,
				Sequence: vin.Sequence,
				N:        vinID,
				Value:    vin.AmountIn,
				CoinBase: vin.Coinbase,
			}

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

			// Note, this only gathers information from the database which does not include mempool transactions
			_, addresses, value, err := c.BlockData.ChainDB.RetrieveAddressIDsByOutpoint(vin.Txid, vin.Vout)
			if err == nil {
				if len(addresses) > 0 {
					// Update Vin due to DCRD AMOUNTIN - START
					// NOTE THIS IS ONLY USEFUL FOR INPUT AMOUNTS THAT ARE NOT ALSO FROM MEMPOOL
					if tx.Confirmations == 0 {
						InsightVin.Value = dcrutil.Amount(value).ToCoin()
					}
					// Update Vin due to DCRD AMOUNTIN - END
					InsightVin.Addr = addresses[0]
				}
			}
			dcramt, _ := dcrutil.NewAmount(InsightVin.Value)
			InsightVin.ValueSat = int64(dcramt)

			vInSum += InsightVin.Value
			txNew.Vins = append(txNew.Vins, InsightVin)

		}

		// Vout fill
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

			txNew.Vouts = append(txNew.Vouts, InsightVout)
			vOutSum += v.Value
		}

		dcramt, _ := dcrutil.NewAmount(vOutSum)
		txNew.ValueOut = dcramt.ToCoin()

		dcramt, _ = dcrutil.NewAmount(vInSum)
		txNew.ValueIn = dcramt.ToCoin()

		dcramt, _ = dcrutil.NewAmount(txNew.ValueIn - txNew.ValueOut)
		txNew.Fees = dcramt.ToCoin()

		// Return true if coinbase value is not empty, return 0 at some fields
		if txNew.Vins != nil && txNew.Vins[0].CoinBase != "" {
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

			var addresses []string
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

// DcrToInsightBlock converts a dcrjson.GetBlockVerboseResult to Insight block.
func (c *insightApiContext) DcrToInsightBlock(inBlocks []*dcrjson.GetBlockVerboseResult) ([]*apitypes.InsightBlockResult, error) {
	RewardAtBlock := func(blocknum int64, voters uint16) float64 {
		subsidyCache := blockchain.NewSubsidyCache(0, c.params)
		work := blockchain.CalcBlockWorkSubsidy(subsidyCache, blocknum, voters, c.params)
		stake := blockchain.CalcStakeVoteSubsidy(subsidyCache, blocknum, c.params) * int64(voters)
		tax := blockchain.CalcBlockTaxSubsidy(subsidyCache, blocknum, voters, c.params)
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
