// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrdata/db/agendadb"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/txhelpers"
)

// statusType defines the various status types supported by the system.
type statusType string

const (
	ErrorStatusType          statusType = "Error"
	NotFoundStatusType       statusType = "Not Found"
	NotSupportedStatusType   statusType = "Not Supported"
	NotImplementedStatusType statusType = "Not Implemented"
	DeprecatedStatusType     statusType = "Deprecated"
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
	FormattedBytes string `json:"formatted_bytes"`
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

// ChartDataCounter is a data cache for the historical charts.
type ChartDataCounter struct {
	sync.RWMutex
	Data map[string]*dbtypes.ChartsData
}

// AddressTx models data for transactions on the address page
type AddressTx struct {
	TxID           string
	InOutID        uint32
	Size           uint32
	FormattedSize  string
	Total          float64
	Confirmations  uint64
	Time           int64
	FormattedTime  string
	ReceivedTotal  float64
	SentTotal      float64
	IsFunding      bool
	MatchedTx      string
	BlockTime      uint64
	MergedTxnCount uint64 `json:",omitempty"`
}

// IOID formats an identification string for the transaction input (or output)
// represented by the AddressTx.
func (a *AddressTx) IOID(txType ...string) string {
	// If transaction is of type merged_debit, return unformatted transaction ID
	if len(txType) > 0 && dbtypes.AddrTxnTypeFromStr(txType[0]) == dbtypes.AddrMergedTxnDebit {
		return a.TxID
	}
	// When AddressTx is used properly, at least one of ReceivedTotal or
	// SentTotal should be zero.
	if a.IsFunding {
		// An outpoint receiving funds
		return fmt.Sprintf("%s:out[%d]", a.TxID, a.InOutID)
	}
	// A transaction input referencing an outpoint being spent
	return fmt.Sprintf("%s:in[%d]", a.TxID, a.InOutID)
}

// TxInfo models data needed for display on the tx page
type TxInfo struct {
	*TxBasic
	SpendingTxns     []TxInID
	Type             string
	Vin              []Vin
	Vout             []Vout
	BlockHeight      int64
	BlockIndex       uint32
	BlockHash        string
	BlockMiningFee   int64
	Confirmations    int64
	Time             int64
	FormattedTime    string
	Mature           string
	VoteFundsLocked  string
	Maturity         int64   // Total number of blocks before mature
	MaturityTimeTill float64 // Time in hours until mature
	TicketInfo
}

func (t *TxInfo) IsTicket() bool {
	return t.Type == "Ticket"
}

func (t *TxInfo) IsVote() bool {
	return t.Type == "Vote"
}

// TicketInfo is used to represent data shown for a sstx transaction.
type TicketInfo struct {
	TicketMaturity       int64
	TimeTillMaturity     float64 // Time before a particular ticket reaches maturity, in hours
	PoolStatus           string
	SpendStatus          string
	TicketPoolSize       int64   // Total number of ticket in the pool
	TicketExpiry         int64   // Total number of blocks before a ticket expires
	TicketExpiryDaysLeft float64 // Approximate days left before the given ticket expires
	TicketLiveBlocks     int64   // Total number of confirms after maturity and up until the point the ticket votes or expires
	BestLuck             int64   // Best possible Luck for voting
	AvgLuck              int64   // Average Luck for voting
	VoteLuck             float64 // Actual Luck for voting on a ticket
	LuckStatus           string  // Short discription based on the VoteLuck
	Probability          float64 // Probability of success before ticket expires
}

// TxInID models the identity of a spending transaction input
type TxInID struct {
	Hash  string
	Index uint32
}

// VoteInfo models data about a SSGen transaction (vote)
type VoteInfo struct {
	Validation         BlockValidation         `json:"block_validation"`
	Version            uint32                  `json:"vote_version"`
	Bits               uint16                  `json:"vote_bits"`
	Choices            []*txhelpers.VoteChoice `json:"vote_choices"`
	TicketSpent        string                  `json:"ticket_spent"`
	MempoolTicketIndex int                     `json:"mempool_ticket_index"`
	ForLastBlock       bool                    `json:"last_block"`
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
	TxAvailable           bool
	Tx                    []*TxBasic
	Tickets               []*TxBasic
	Revs                  []*TxBasic
	Votes                 []*TxBasic
	Misses                []string
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

// AddressTransactions collects the transactions for an address as AddressTx
// slices.
type AddressTransactions struct {
	Transactions []*AddressTx
	TxnsFunding  []*AddressTx
	TxnsSpending []*AddressTx
}

// AddressInfo models data for display on the address page
type AddressInfo struct {
	// Address is the decred address on the current page
	Address string

	// Page parameters
	MaxTxLimit    int64
	Fullmode      bool
	Path          string
	Limit, Offset int64  // ?n=Limit&start=Offset
	TxnType       string // ?txntype=TxnType

	// NumUnconfirmed is the number of unconfirmed txns for the address
	NumUnconfirmed  int64
	UnconfirmedTxns *AddressTransactions

	// Transactions on the current page
	Transactions    []*AddressTx
	TxnsFunding     []*AddressTx
	TxnsSpending    []*AddressTx
	NumTransactions int64 // The number of transactions in the address
	NumFundingTxns  int64 // number paying to the address
	NumSpendingTxns int64 // number spending outpoints associated with the address
	AmountReceived  dcrutil.Amount
	AmountSent      dcrutil.Amount
	AmountUnspent   dcrutil.Amount

	// Balance is used in full mode, describing all known transactions
	Balance *AddressBalance

	// KnownTransactions refers to the total transaction count in the DB when in
	// full mode, the sum of funding (crediting) and spending (debiting) txns.
	KnownTransactions int64
	KnownFundingTxns  int64
	KnownSpendingTxns int64

	// KnownMergedSpendingTxns refers to the total count of unique debit transactions
	// that appear in the merged debit view.
	KnownMergedSpendingTxns int64

	// IsDummyAddress is true when the address is the dummy address typically
	// used for unspendable ticket change outputs. See
	// https://github.com/decred/dcrdata/issues/358 for details.
	IsDummyAddress bool
}

// TxnCount returns the number of transaction "rows" available.
func (a *AddressInfo) TxnCount() int64 {
	if !a.Fullmode {
		return a.KnownTransactions
	}
	switch dbtypes.AddrTxnTypeFromStr(a.TxnType) {
	case dbtypes.AddrTxnAll:
		return a.KnownTransactions
	case dbtypes.AddrTxnCredit:
		return a.KnownFundingTxns
	case dbtypes.AddrTxnDebit:
		return a.KnownSpendingTxns
	case dbtypes.AddrMergedTxnDebit:
		return a.KnownMergedSpendingTxns
	default:
		log.Warnf("Unknown address transaction type: %v", a.TxnType)
		return 0
	}
}

// AddressBalance represents the number and value of spent and unspent outputs
// for an address.
type AddressBalance struct {
	Address        string `json:"address"`
	NumSpent       int64  `json:"num_stxos"`
	NumUnspent     int64  `json:"num_utxos"`
	TotalSpent     int64  `json:"amount_spent"`
	TotalUnspent   int64  `json:"amount_unspent"`
	NumMergedSpent int64  `json:"num_merged_spent,omitempty"`
}

// HomeInfo represents data used for the home page
type HomeInfo struct {
	CoinSupply        int64          `json:"coin_supply"`
	StakeDiff         float64        `json:"sdiff"`
	IdxBlockInWindow  int            `json:"window_idx"`
	IdxInRewardWindow int            `json:"reward_idx"`
	Difficulty        float64        `json:"difficulty"`
	DevFund           int64          `json:"dev_fund"`
	DevAddress        string         `json:"dev_address"`
	TicketReward      float64        `json:"reward"`
	RewardPeriod      string         `json:"reward_period"`
	ASR               float64        `json:"ASR"`
	NBlockSubsidy     BlockSubsidy   `json:"subsidy"`
	Params            ChainParams    `json:"params"`
	PoolInfo          TicketPoolInfo `json:"pool_info"`
}

// BlockSubsidy is an implementation of dcrjson.GetBlockSubsidyResult
type BlockSubsidy struct {
	Total int64 `json:"total"`
	PoW   int64 `json:"pow"`
	PoS   int64 `json:"pos"`
	Dev   int64 `json:"dev"`
}

// MempoolInfo models data to update mempool info on the home page
type MempoolInfo struct {
	sync.RWMutex
	MempoolShort
	Transactions []MempoolTx `json:"tx"`
	Tickets      []MempoolTx `json:"tickets"`
	Votes        []MempoolTx `json:"votes"`
	Revocations  []MempoolTx `json:"revs"`
}

// MempoolShort represents the mempool data sent as the mempool update
type MempoolShort struct {
	LastBlockHeight    int64                    `json:"block_height"`
	LastBlockHash      string                   `json:"block_hash"`
	LastBlockTime      int64                    `json:"block_time"`
	TotalOut           float64                  `json:"total"`
	TotalSize          int32                    `json:"size"`
	NumTickets         int                      `json:"num_tickets"`
	NumVotes           int                      `json:"num_votes"`
	NumRegular         int                      `json:"num_regular"`
	NumRevokes         int                      `json:"num_revokes"`
	NumAll             int                      `json:"num_all"`
	LatestTransactions []MempoolTx              `json:"latest"`
	FormattedTotalSize string                   `json:"formatted_size"`
	TicketIndexes      map[int64]map[string]int `json:"-"`
	VotingInfo         VotingInfo               `json:"voting_info"`
}

// VotingInfo models data about the validity of the next block from mempool
type VotingInfo struct {
	Valids         uint16 `json:"choice_valid"`
	Invalids       uint16 `json:"choice_invalid"`
	TotalCollected uint16 `json:"total_votes_collected"`
	TotalNeeded    uint16 `json:"total_votes_required"`
	Required       uint16 `json:"total_choices_required"`
	BlockValid     bool   `json:"block_valid"`
	voted          map[string]bool
}

// ChainParams models simple data about the chain server's parameters used for some
// info on the front page
type ChainParams struct {
	WindowSize       int64 `json:"window_size"`
	RewardWindowSize int64 `json:"reward_window_size"`
	TargetPoolSize   int64 `json:"target_pool_size"`
	BlockTime        int64 `json:"target_block_time"`
	MeanVotingBlocks int64
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
	var transactions, creditTxns, debitTxns []*AddressTx
	for _, addrOut := range addrHist {
		if !addrOut.ValidMainChain {
			continue
		}
		coin := dcrutil.Amount(addrOut.Value).ToCoin()
		tx := AddressTx{
			BlockTime: addrOut.TxBlockTime,
			InOutID:   addrOut.TxVinVoutIndex,
			TxID:      addrOut.TxHash,
			MatchedTx: addrOut.MatchingTxHash,
			IsFunding: addrOut.IsFunding,
		}

		if addrOut.IsFunding {
			// Funding transaction
			received += int64(addrOut.Value)
			tx.ReceivedTotal = coin
			creditTxns = append(creditTxns, &tx)
		} else {
			// Spending transaction
			sent += int64(addrOut.Value)
			tx.SentTotal = coin
			tx.MergedTxnCount = addrOut.MergedDebitCount

			debitTxns = append(debitTxns, &tx)
		}

		transactions = append(transactions, &tx)
	}

	return &AddressInfo{
		Address:         addrHist[0].Address,
		Transactions:    transactions,
		TxnsFunding:     creditTxns,
		TxnsSpending:    debitTxns,
		NumFundingTxns:  int64(len(creditTxns)),
		NumSpendingTxns: int64(len(debitTxns)),
		AmountReceived:  dcrutil.Amount(received),
		AmountSent:      dcrutil.Amount(sent),
		AmountUnspent:   dcrutil.Amount(received - sent),
	}
}

// WebsocketBlock wraps the new block info for use in the websocket
type WebsocketBlock struct {
	Block *BlockBasic `json:"block"`
	Extra *HomeInfo   `json:"extra"`
}

// TicketPoolInfo describes the live ticket pool
type TicketPoolInfo struct {
	Size          uint32  `json:"size"`
	Value         float64 `json:"value"`
	ValAvg        float64 `json:"valavg"`
	Percentage    float64 `json:"percent"`
	Target        uint16  `json:"target"`
	PercentTarget float64 `json:"percent_target"`
}

// MempoolTx models the tx basic data for the mempool page
type MempoolTx struct {
	Hash     string    `json:"hash"`
	Time     int64     `json:"time"`
	Size     int32     `json:"size"`
	TotalOut float64   `json:"total"`
	Type     string    `json:"Type"`
	VoteInfo *VoteInfo `json:"vote_info"`
}

// NewMempoolTx models data sent from the notification handler
type NewMempoolTx struct {
	Time int64
	Hex  string
}

// ExtendedChainParams represents the data of ChainParams
type ExtendedChainParams struct {
	Params               *chaincfg.Params
	ActualTicketPoolSize int64
	AddressPrefix        []AddrPrefix
}

// AddrPrefix represent the address name it's prefix and description
type AddrPrefix struct {
	Name        string
	Prefix      string
	Description string
}

// AddressPrefixes generates an array AddrPrefix by using chaincfg.Params
func AddressPrefixes(params *chaincfg.Params) []AddrPrefix {
	Descriptions := []string{"P2PK address",
		"P2PKH address prefix",
		"P2PKH address prefix",
		"secp256k1 Schnorr P2PKH address prefix",
		"P2SH address prefix",
		"WIF private key prefix",
		"HD extended private key prefix",
		"HD extended public key prefix",
	}
	Name := []string{"PubKeyAddrID",
		"PubKeyHashAddrID",
		"PKHEdwardsAddrID",
		"PKHSchnorrAddrID",
		"ScriptHashAddrID",
		"PrivateKeyID",
		"HDPrivateKeyID",
		"HDPublicKeyID",
	}

	MainnetPrefixes := []string{"Dk", "Ds", "De", "DS", "Dc", "Pm", "dprv", "dpub"}
	TestnetPrefixes := []string{"Tk", "Ts", "Te", "TS", "Tc", "Pt", "tprv", "tpub"}
	SimnetPrefixes := []string{"Sk", "Ss", "Se", "SS", "Sc", "Ps", "sprv", "spub"}

	name := params.Name
	var netPrefixes []string
	if name == "mainnet" {
		netPrefixes = MainnetPrefixes
	} else if strings.HasPrefix(name, "testnet") {
		netPrefixes = TestnetPrefixes
	} else if name == "simnet" {
		netPrefixes = SimnetPrefixes
	} else {
		return nil
	}

	addrPrefix := make([]AddrPrefix, 0, len(Descriptions))
	for i, desc := range Descriptions {
		addrPrefix = append(addrPrefix, AddrPrefix{
			Name:        Name[i],
			Description: desc,
			Prefix:      netPrefixes[i],
		})
	}
	return addrPrefix
}

// GetAgendaInfo gets the all info for the specified agenda ID.
func GetAgendaInfo(agendaId string) (*agendadb.AgendaTagged, error) {
	return agendadb.GetAgendaInfo(agendaId)
}
