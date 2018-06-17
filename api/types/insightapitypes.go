// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package types

import (
	"encoding/json"

	"github.com/decred/dcrd/dcrutil"
)

// InsightAddress models an address transactions
//
type InsightAddress struct {
	Address      string      `json:"address,omitempty"`
	From         int         `json:"from"`
	To           int         `json:"to"`
	Transactions []InsightTx `json:"items,omitempty"`
}

// InsightAddressInfo models basic information
// about an address
type InsightAddressInfo struct {
	Address          string         `json:"addrStr,omitempty"`
	Limit            int64          `json:"limit,omitemtpy"`
	Offset           int64          `json:"offset,omitempty"`
	TransactionsID   []string       `json:"transactions,omitempty"`
	NumFundingTxns   int64          `json:"numFundingTxns,omitempty"`
	NumSpendingTxns  int64          `json:"numSpendingTxns,omitempty"`
	KnownFundingTxns int64          `json:"knownFundingTxns,omitempty"`
	NumUnconfirmed   int64          `json:"numUnconfirmed,omitempty"`
	TotalReceived    dcrutil.Amount `json:"totalReceived"`
	TotalSent        dcrutil.Amount `json:"totalSent"`
	Unspent          dcrutil.Amount `json:"balance"`
	Path             string         `json:"path,omitempty"`
}

// InsightRawTx contains the raw transaction string
// of a transaction
type InsightRawTx struct {
	Rawtx string `json:"rawtx"`
}

// InsightMultiAddrsTx models multi address post data structure
type InsightMultiAddrsTx struct {
	Addresses   string      `json:"addrs"`
	From        json.Number `json:"from,Number,omitempty"`
	To          json.Number `json:"to,Number,omitempty"`
	NoAsm       json.Number `json:"noAsm"`
	NoScriptSig json.Number `json:"noScriptSig"`
	NoSpent     json.Number `json:"noSpent"`
}

type InsightMultiAddrsTxOutput struct {
	TotalItems int64       `json:"totalItems"`
	From       int         `json:"from"`
	To         int         `json:"to"`
	Items      []InsightTx `json:"items"`
}

// InsightAddr models the multi address post data structure
type InsightAddr struct {
	Addrs string `json:"addrs"`
}

// InsightPagination models basic pagination output
// for a result
type InsightPagination struct {
	Next    string `json:"next,omitempty"`
	Prev    string `json:"prev,omitempty"`
	IsToday string `json:"isToday,omitempty"`
}

// AddressTxnOutput models an address transaction outputs
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

// AddressSpendByFunHash models a return from
// GetAddressSpendByFunHash
type AddressSpendByFunHash struct {
	FundingTxVoutIndex uint32
	SpendingTxVinIndex interface{}
	SpendingTxHash     interface{}
	BlockHeight        interface{}
}

type InsightTx struct {
	Txid          string         `json:"txid,omitempty"`
	Version       int32          `json:"version,omitempty"`
	Locktime      uint32         `json:"locktime"`
	IsCoinBase    bool           `json:"isCoinBase,omitempty"`
	Vins          []*InsightVin  `json:"vin,omitempty"`
	Vouts         []*InsightVout `json:"vout,omitempty"`
	Blockhash     string         `json:"blockhash,omitempty"`
	Blockheight   int64          `json:"blockheight"`
	Confirmations int64          `json:"confirmations"`
	Time          int64          `json:"time,omitempty"`
	Blocktime     int64          `json:"blocktime,omitempty"`
	ValueOut      float64        `json:"valueOut,omitempty"`
	Size          uint32         `json:"size,omitempty"`
	ValueIn       float64        `json:"valueIn,omitempty"`
	Fees          float64        `json:"fees,omitempty"`
}

type InsightVin struct {
	Txid      string            `json:"txid,omitempty"`
	Vout      uint32            `json:"vout,omitempty"`
	Sequence  uint32            `json:"sequence,omitempty"`
	N         int               `json:"n"`
	ScriptSig *InsightScriptSig `json:"scriptSig,omitempty"`
	Addr      string            `json:"addr,omitempty"`
	ValueSat  int64             `json:"valueSat,omitempty"`
	Value     float64           `json:"value,omitempty"`
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
