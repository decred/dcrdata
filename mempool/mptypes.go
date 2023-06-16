// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package mempool

import (
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	apitypes "github.com/decred/dcrdata/v8/api/types"
)

// MempoolInfo models basic data about the node's mempool
type MempoolInfo struct {
	CurrentHeight               uint32
	NumTicketPurchasesInMempool uint32
	NumTicketsSinceStatsReport  int32
	LastCollectTime             time.Time
}

// BlockID provides basic identifying information about a block.
type BlockID struct {
	Hash   chainhash.Hash
	Height int64
	Time   int64
}

// MinableFeeInfo describes the ticket fees
type MinableFeeInfo struct {
	// All fees in mempool
	allFees     []float64
	allFeeRates []float64
	// The index of the 20th largest fee, or largest if number in mempool < 20
	lowestMineableIdx int
	// The corresponding fee (i.e. all[lowestMineableIdx])
	lowestMineableFee float64
	// A window of fees about lowestMineableIdx
	targetFeeWindow []float64
}

// TicketsDetails localizes apitypes.TicketsDetails
type TicketsDetails apitypes.TicketsDetails

// Below is the implementation of sort.Interface
// { Len(), Swap(i, j int), Less(i, j int) bool }. This implementation sorts
// the structure in ascending order

// Len returns the length of TicketsDetails
func (tix TicketsDetails) Len() int {
	return len(tix)
}

// Swap swaps TicketsDetails elements at i and j
func (tix TicketsDetails) Swap(i, j int) {
	tix[i], tix[j] = tix[j], tix[i]
}

// ByFeeRate models TicketsDetails sorted by fee rates
type ByFeeRate struct {
	TicketsDetails
}

// Less compares fee rates by rate_i < rate_j
func (tix ByFeeRate) Less(i, j int) bool {
	return tix.TicketsDetails[i].FeeRate < tix.TicketsDetails[j].FeeRate
}

// ByAbsoluteFee models TicketDetails sorted by fee
type ByAbsoluteFee struct {
	TicketsDetails
}

// Less compares fee rates by fee_i < fee_j
func (tix ByAbsoluteFee) Less(i, j int) bool {
	return tix.TicketsDetails[i].Fee < tix.TicketsDetails[j].Fee
}

// StakeData models info about ticket purchases in mempool
type StakeData struct {
	LatestBlock       BlockID
	Time              time.Time
	NumTickets        uint32
	NumVotes          uint32
	NewTickets        uint32
	Ticketfees        *chainjson.TicketFeeInfoResult
	MinableFees       *MinableFeeInfo
	AllTicketsDetails TicketsDetails
	StakeDiff         float64
}

// Height returns the best block height at the time the mempool data was
// gathered.
func (m *StakeData) Height() uint32 {
	return uint32(m.LatestBlock.Height)
}

// Hash returns the best block hash at the time the mempool data was gathered.
func (m *StakeData) Hash() string {
	return m.LatestBlock.Hash.String()
}

// TODO: tx data types

// NewTx models data for a new transaction.
type NewTx struct {
	Hash *chainhash.Hash
	T    time.Time
}

type ScriptSig struct {
	Asm string `json:"asm"`
	Hex string `json:"hex"`
}

type Vin struct {
	Coinbase    string     `json:"coinbase"`
	Stakebase   string     `json:"stakebase"`
	Txid        string     `json:"txid"`
	Vout        uint32     `json:"vout"`
	Tree        int8       `json:"tree"`
	Sequence    uint32     `json:"sequence"`
	AmountIn    float64    `json:"amountin"`
	BlockHeight uint32     `json:"blockheight"`
	BlockIndex  uint32     `json:"blockindex"`
	ScriptSig   *ScriptSig `json:"scriptSig"`
}

type ScriptPubKeyResult struct {
	Asm       string   `json:"asm"`
	Hex       string   `json:"hex,omitempty"`
	ReqSigs   int32    `json:"reqSigs,omitempty"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses,omitempty"`
	CommitAmt *float64 `json:"commitamt,omitempty"`
}

// Vout models parts of the tx data.  It is defined separately since both
// getrawtransaction and decoderawtransaction use the same structure.
type Vout struct {
	Value        float64            `json:"value"`
	N            uint32             `json:"n"`
	Version      uint16             `json:"version"`
	ScriptPubKey ScriptPubKeyResult `json:"scriptPubKey"`
}

type Tx struct {
	Hex      string `json:"hex"`
	Txid     string `json:"txid"`
	Version  int32  `json:"version"`
	LockTime uint32 `json:"locktime"`
	Expiry   uint32 `json:"expiry"`
	Vin      []Vin  `json:"vin"`
	Vout     []Vout `json:"vout"`
}
