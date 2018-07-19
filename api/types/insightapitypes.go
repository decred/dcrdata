// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package types

import (
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
)

// InsightAddress models an address transactions
//
type InsightAddress struct {
	Address      string                                 `json:"address,omitempty"`
	From         int                                    `json:"from,omitempty"`
	To           int                                    `json:"to,omitempty"`
	Transactions []*dcrjson.SearchRawTransactionsResult `json:"items,omitempty"`
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
	Vout          uint32  `json:"vout,omitempty"`
	ScriptPubKey  string  `json:"scriptPubKey,omitempty"`
	Height        int64   `json:"height,omitempty"`
	BlockHash     string  `json:"block_hash,omitempty"`
	Amount        float64 `json:"amount"`
	Atoms         float64 `json:"atoms"`
	Confirmations int64   `json:"confirmations"`
}
