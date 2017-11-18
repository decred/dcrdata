// Copyright (c) 2017, The Dcrdata developers
// See LICENSE for details.
package explorer

import (
	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
)

// BlockBasic models data for the explorer's explorer page
type BlockBasic struct {
	Height         int64  `json:"height"`
	Size           int32  `json:"size"`
	Valid          bool   `json:"valid"`
	Voters         uint16 `json:"votes"`
	Transactions   int    `json:"tx"`
	FreshStake     uint8  `json:"tickets"`
	Revocations    uint32 `json:"revocations"`
	BlockTime      int64  `json:"time"`
	FormattedTime  string `json:"formatted_time"`
	FormattedBytes string
}

// TxBasic models data for transactions on the block page
type TxBasic struct {
	TxID          string
	FormattedSize string
	Total         float64
	Fee           dcrutil.Amount
	FeeRate       dcrutil.Amount
	VoteInfo      *VoteInfo
	Coinbase      bool
}

//AddressTx models data for transactions on the address page
type AddressTx struct {
	TxID          string
	FormattedSize string
	Total         float64
	Confirmations uint64
	Time          int64
	FormattedTime string
	RecievedTotal float64
	SentTotal     float64
}

// TxInfo models data needed for display on the tx page
type TxInfo struct {
	*TxBasic
	SpendingTxns    []TxInID
	Type            string
	Vin             []Vin
	Vout            []Vout
	BlockHeight     int64
	BlockIndex      uint32
	Confirmations   int64
	Time            int64
	FormattedTime   string
	Mature          string
	VoteFundsLocked string
	TicketMaturity  int64
}

// TxInID models the identity of a spending transaction input
type TxInID struct {
	Hash  string
	Index uint32
}

// VoteInfo models data about a SSGen transaction (vote)
type VoteInfo struct {
	Validation BlockValidation         `json:"block_validation"`
	Version    uint32                  `json:"vote_version"`
	Bits       uint16                  `json:"vote_bits"`
	Choices    []*txhelpers.VoteChoice `json:"vote_choices"`
}

// BlockValidation models data about a vote's decision on a block
type BlockValidation struct {
	Hash     string `json:"hash"`
	Height   int64  `json:"height"`
	Validity bool   `json:"validity"`
}

// Vin models basic data about a tx input for display
type Vin struct {
	*dcrjson.Vin
	Addresses       []string
	FormattedAmount string
}

// Vout models basic data about a tx output for display
type Vout struct {
	Addresses       []string
	Amount          float64
	FormattedAmount string
	Type            string
	Spent           bool
	OP_RETURN       string
}

// BlockInfo models data for display on the block page
type BlockInfo struct {
	*BlockBasic
	Hash                  string
	Version               int32
	Confirmations         int64
	StakeRoot             string
	MerkleRoot            string
	Tx                    []*TxBasic
	Tickets               []*TxBasic
	Revs                  []*TxBasic
	Votes                 []*TxBasic
	Nonce                 uint32
	VoteBits              uint16
	FinalState            string
	PoolSize              uint32
	Bits                  string
	SBits                 float64
	Difficulty            float64
	ExtraData             string
	StakeVersion          uint32
	PreviousHash          string
	NextHash              string
	TotalSent             float64
	MiningFee             dcrutil.Amount
	StakeValidationHeight int64
}

// AddressInfo models data for display on the address page
type AddressInfo struct {
	Address          string
	Limit            int64
	Offset           int64
	Transactions     []*AddressTx
	NumFundingTxns   int64
	NumSpendingTxns  int64
	KnownFundingTxns int64
	NumUnconfirmed   int64
	TotalReceived    dcrutil.Amount
	TotalSent        dcrutil.Amount
	Unspent          dcrutil.Amount
	Balance          *AddressBalance
	Path             string
}

// AddressBalance represents the number and value of spent and unspent outputs
// for an address.
type AddressBalance struct {
	Address      string
	NumSpent     int64
	NumUnspent   int64
	TotalSpent   int64
	TotalUnspent int64
}

// ReduceAddressHistory generates a template AddressInfo from a slice of
// dbtypes.AddressRow. All fields except NumUnconfirmed and Transactions are set
// completely. Transactions is partially set, with each transaction having only
// the TxID and ReceivedTotal set. The rest of the data should be filled in by
// other means, such as RPC calls or database queries.
func ReduceAddressHistory(addrHist []*dbtypes.AddressRow) *AddressInfo {
	if len(addrHist) == 0 {
		return nil
	}

	var received, sent int64
	var numFundingTxns, numSpendingTxns int64
	var transactions []*AddressTx
	for _, addrOut := range addrHist {
		numFundingTxns++
		coin := dcrutil.Amount(addrOut.Value).ToCoin()

		// Funding transaction
		received += int64(addrOut.Value)
		tx := AddressTx{
			TxID:          addrOut.FundingTxHash,
			RecievedTotal: coin,
		}
		transactions = append(transactions, &tx)

		// Is the outpoint spent?
		if addrOut.SpendingTxHash == "" {
			continue
		}

		// Spending transaction
		numSpendingTxns++
		sent += int64(addrOut.Value)

		spendingTx := AddressTx{
			TxID:      addrOut.SpendingTxHash,
			SentTotal: coin,
		}
		transactions = append(transactions, &spendingTx)
	}

	return &AddressInfo{
		Address:         addrHist[0].Address,
		Transactions:    transactions,
		NumFundingTxns:  numFundingTxns,
		NumSpendingTxns: numSpendingTxns,
		TotalReceived:   dcrutil.Amount(received),
		TotalSent:       dcrutil.Amount(sent),
		Unspent:         dcrutil.Amount(received - sent),
	}
}

// WebsocketBlock wraps the new block info for use in the websocket
type WebsocketBlock struct {
	Block BlockBasic `json:"block"`
}

// MempoolBasic models basic data for updating the front page's mempool data
type MempoolBasic struct {
	NumTickets uint32 `json:"num_tickets"`
	NumVotes   uint32 `json:"num_votes"`
	NumTx      uint32 `json:"num_tx"`
	NumRevs    uint32 `json:"num_revs"`
}

//MempoolTx models data for a transaction in mempool
type MempoolTx struct {
	Hash   string  `json:"hash"`
	Size   int32   `json:"size"`
	Fee    float64 `json:"fee"`
	Time   int64   `json:"time"`
	Height int64   `json:"height"`
}

// MempoolInfo models data about the current mempool
type MempoolInfo struct {
	*MempoolBasic
	Height      uint32
	Tickets     []*MempoolTx `json:"tickets"`
	Votes       []*MempoolTx `json:"votes"`
	Revocations []*MempoolTx `json:"revs"`
	Regular     []*MempoolTx `json:"tx"`
}

// ToExplorerMempool returns mempool data for explorer and the front page
func ToExplorerMempool(m *mempool.MempoolData) *MempoolInfo {
	makeExplorerTxs := func(txs map[string]dcrjson.GetRawMempoolVerboseResult) []*MempoolTx {
		tx := make([]*MempoolTx, 0, len(txs))
		for hash, details := range txs {
			tx = append(tx, &MempoolTx{
				Hash:   hash,
				Size:   details.Size,
				Fee:    details.Fee,
				Time:   details.Time,
				Height: details.Height,
			})
		}
		return tx
	}
	tx := makeExplorerTxs(m.Tx)
	votes := makeExplorerTxs(m.Votes)
	revs := makeExplorerTxs(m.Revs)
	tickets := makeExplorerTxs(m.Tickets)
	return &MempoolInfo{
		MempoolBasic: &MempoolBasic{
			NumRevs:    m.NumRevs,
			NumVotes:   m.NumVotes,
			NumTx:      m.NumTx,
			NumTickets: m.NumTickets,
		},
		Height:      m.Height,
		Tickets:     tickets,
		Votes:       votes,
		Revocations: revs,
		Regular:     tx,
	}
}
