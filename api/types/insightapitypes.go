// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package types

import (
	"encoding/json"

	"github.com/decred/dcrdata/v8/db/dbtypes"
)

// SyncResponse contains sync status information.
type SyncResponse struct {
	Status           string  `json:"status"`
	BlockChainHeight int64   `json:"blockChainHeight"`
	SyncPercentage   int64   `json:"syncPercentage"`
	Height           int64   `json:"height"`
	Error            *string `json:"error"`
	Type             string  `json:"type"`
}

// InsightAddress models an address' transactions.
type InsightAddress struct {
	Address      string      `json:"address,omitempty"`
	From         int         `json:"from"`
	To           int         `json:"to"`
	Transactions []InsightTx `json:"items,omitempty"`
}

// InsightAddressInfo models basic information about an address.
type InsightAddressInfo struct {
	Address                  string   `json:"addrStr,omitempty"`
	Balance                  float64  `json:"balance"`
	BalanceSat               int64    `json:"balanceSat"`
	TotalReceived            float64  `json:"totalReceived"`
	TotalReceivedSat         int64    `json:"totalReceivedSat"`
	TotalSent                float64  `json:"totalSent"`
	TotalSentSat             int64    `json:"totalSentSat"`
	UnconfirmedBalance       float64  `json:"unconfirmedBalance"`
	UnconfirmedBalanceSat    int64    `json:"unconfirmedBalanceSat"`
	UnconfirmedTxAppearances int64    `json:"unconfirmedTxApperances"` // [sic]
	TxAppearances            int64    `json:"txApperances"`            // [sic]
	TransactionsID           []string `json:"transactions,omitempty"`
}

// InsightRawTx contains the raw transaction string of a transaction.
type InsightRawTx struct {
	Rawtx string `json:"rawtx"`
}

// InsightMultiAddrsTx models the POST request for the multi-address endpoints.
type InsightMultiAddrsTx struct {
	Addresses   string      `json:"addrs"`
	From        json.Number `json:"from,omitempty"`
	To          json.Number `json:"to,omitempty"`
	NoAsm       json.Number `json:"noAsm"`
	NoScriptSig json.Number `json:"noScriptSig"`
	NoSpent     json.Number `json:"noSpent"`
}

// InsightMultiAddrsTxOutput models the response to the multi-address
// transactions POST request.
type InsightMultiAddrsTxOutput struct {
	TotalItems int64       `json:"totalItems"`
	From       int         `json:"from"`
	To         int         `json:"to"`
	Items      []InsightTx `json:"items"`
}

// InsightAddr models the multi-address post data structure.
type InsightAddr struct {
	Addrs string `json:"addrs"`
}

// InsightPagination models basic pagination output for a result.
type InsightPagination struct {
	Next    string `json:"next,omitempty"`
	Prev    string `json:"prev,omitempty"`
	IsToday string `json:"isToday,omitempty"`
}

// AddressTxnOutput models an address transaction outputs.
type AddressTxnOutput struct {
	Address       string  `json:"address"`
	TxnID         string  `json:"txid"`
	Vout          uint32  `json:"vout"`
	BlockTime     int64   `json:"ts,omitempty"`
	ScriptPubKey  string  `json:"scriptPubKey"`
	Height        int64   `json:"height,omitempty"`
	BlockHash     string  `json:"block_hash,omitempty"`
	Amount        float64 `json:"amount,omitempty"`
	Atoms         int64   `json:"atoms,omitempty"` // Not Required per Insight spec
	Satoshis      int64   `json:"satoshis,omitempty"`
	Confirmations int64   `json:"confirmations"`
}

// TxOutFromDB converts a dbtypes.AddressTxnOutput to a api/types.AddressTxnOutput.
func TxOutFromDB(dbTxOut *dbtypes.AddressTxnOutput, currentHeight int32) *AddressTxnOutput {
	return &AddressTxnOutput{
		Address:      dbTxOut.Address,
		TxnID:        dbTxOut.TxHash.String(),
		Vout:         dbTxOut.Vout,
		BlockTime:    dbTxOut.BlockTime,
		ScriptPubKey: dbTxOut.PkScript,
		Height:       int64(dbTxOut.Height),
		//BlockHash:    dbTxOut.BlockHash.String(),
		Amount: float64(dbTxOut.Atoms) / 1e8, // dcrutil.Amount(dbTxOut.Atoms).ToCoin()
		//Atoms: dbTxOut.Atoms,
		Satoshis:      dbTxOut.Atoms,
		Confirmations: int64(currentHeight - dbTxOut.Height + 1),
	}
}

// SpendByFundingHash models a return from SpendDetailsForFundingTx.
type SpendByFundingHash struct {
	FundingTxVoutIndex uint32
	SpendingTxVinIndex interface{}
	SpendingTxHash     interface{}
	BlockHeight        interface{}
}

type InsightTx struct {
	Txid           string         `json:"txid,omitempty"`
	Version        int32          `json:"version,omitempty"`
	Locktime       uint32         `json:"locktime"`
	IsCoinBase     bool           `json:"isCoinBase,omitempty"`
	IsTreasurybase bool           `json:"isTreasurybase,omitempty"`
	Vins           []*InsightVin  `json:"vin,omitempty"`
	Vouts          []*InsightVout `json:"vout,omitempty"`
	Blockhash      string         `json:"blockhash,omitempty"`
	Blockheight    int64          `json:"blockheight"`
	Confirmations  int64          `json:"confirmations"`
	Time           int64          `json:"time,omitempty"`
	Blocktime      int64          `json:"blocktime,omitempty"`
	ValueOut       float64        `json:"valueOut,omitempty"`
	Size           uint32         `json:"size,omitempty"`
	ValueIn        float64        `json:"valueIn,omitempty"`
	Fees           float64        `json:"fees,omitempty"`
}

type InsightVin struct {
	Txid      string            `json:"txid,omitempty"`
	Vout      *uint32           `json:"vout,omitempty"`
	Sequence  *uint32           `json:"sequence,omitempty"`
	N         int               `json:"n"`
	ScriptSig *InsightScriptSig `json:"scriptSig,omitempty"`
	Addr      string            `json:"addr,omitempty"`
	ValueSat  int64             `json:"valueSat"`
	Value     float64           `json:"value"`
	CoinBase  string            `json:"coinbase,omitempty"`
}

type InsightScriptSig struct {
	Hex string `json:"hex,omitempty"`
	Asm string `json:"asm,omitempty"`
}

type InsightVout struct {
	Value        float64             `json:"value"`
	N            uint32              `json:"n"`
	ScriptPubKey InsightScriptPubKey `json:"scriptPubKey,omitempty"`
	SpentTxID    interface{}         `json:"spentTxId"`   // Insight requires null if unspent and spending TxID if spent
	SpentIndex   interface{}         `json:"spentIndex"`  // Insight requires null if unspent and Index if spent
	SpentHeight  interface{}         `json:"spentHeight"` // Insight requires null if unspent and SpentHeight if spent
}

type InsightScriptPubKey struct {
	Hex       string   `json:"hex,omitempty"`
	Asm       string   `json:"asm,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	Type      string   `json:"type,omitempty"`
}

// InsightBlockResult models the data required by a block json return for
// Insight API
type InsightBlockResult struct {
	Hash          string   `json:"hash"`
	Confirmations int64    `json:"confirmations"`
	Size          int32    `json:"size"`
	Height        int64    `json:"height"`
	Version       int32    `json:"version"`
	MerkleRoot    string   `json:"merkleroot"`
	Tx            []string `json:"tx,omitempty"`
	Time          int64    `json:"time"`
	Nonce         uint32   `json:"nonce"`
	Bits          string   `json:"bits"`
	Difficulty    float64  `json:"difficulty"`
	PreviousHash  string   `json:"previousblockhash"`
	NextHash      string   `json:"nextblockhash,omitempty"`
	Reward        float64  `json:"reward"`
	IsMainChain   bool     `json:"isMainChain"`
}

// InsightBlocksSummaryResult models data required by blocks json return for
// Insight API
type InsightBlocksSummaryResult struct {
	Blocks     []dbtypes.BlockDataBasic `json:"blocks"`
	Length     int                      `json:"length"`
	Pagination struct {
		Next      string `json:"next"`
		Prev      string `json:"prev"`
		CurrentTs int64  `json:"currentTs"`
		Current   string `json:"current"`
		IsToday   bool   `json:"isToday"`
		More      bool   `json:"more"`
		MoreTs    int64  `json:"moreTs,omitempty"`
	} `json:"pagination"`
}

// InsightBlockAddrTxSummary models the data required by addrtx json return for
// Insight API
type InsightBlockAddrTxSummary struct {
	PagesTotal int64       `json:"pagesTotal"`
	Txs        []InsightTx `json:"txs"`
}
