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
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v3/db/agendadb"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/txhelpers"
)

// expStatus defines the various status types supported by the system.
type expStatus string

const (
	ExpStatusError          expStatus = "Error"
	ExpStatusNotFound       expStatus = "Not Found"
	ExpStatusFutureBlock    expStatus = "Future Block"
	ExpStatusNotSupported   expStatus = "Not Supported"
	ExpStatusNotImplemented expStatus = "Not Implemented"
	ExpStatusWrongNetwork   expStatus = "Wrong Network"
	ExpStatusDeprecated     expStatus = "Deprecated"
	ExpStatusSyncing        expStatus = "Blocks Syncing"
	ExpStatusDBTimeout      expStatus = "Database Timeout"
)

// blockchainSyncStatus defines the status update displayed on the syncing status page
// when new blocks are being appended into the db.
var blockchainSyncStatus = new(syncStatus)

// BlockBasic models data for the explorer's explorer page
type BlockBasic struct {
	Height         int64           `json:"height"`
	Hash           string          `json:"hash"`
	Size           int32           `json:"size"`
	Valid          bool            `json:"valid"`
	MainChain      bool            `json:"mainchain"`
	Voters         uint16          `json:"votes"`
	Transactions   int             `json:"tx"`
	IndexVal       int64           `json:"windowIndex"`
	FreshStake     uint8           `json:"tickets"`
	Revocations    uint32          `json:"revocations"`
	BlockTime      dbtypes.TimeDef `json:"time"`
	FormattedBytes string          `json:"formatted_bytes"`
}

// WebBasicBlock is used for quick DB data without rpc calls
type WebBasicBlock struct {
	Height      uint32   `json:"height"`
	Size        uint32   `json:"size"`
	Hash        string   `json:"hash"`
	Difficulty  float64  `json:"diff"`
	StakeDiff   float64  `json:"sdiff"`
	Time        int64    `json:"time"`
	NumTx       uint32   `json:"txlength"`
	PoolSize    uint32   `json:"poolsize"`
	PoolValue   float64  `json:"poolvalue"`
	PoolValAvg  float64  `json:"poolvalavg"`
	PoolWinners []string `json:"winners"`
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

// AddressTx models data for transactions on the address page
type AddressTx struct {
	TxID           string
	TxType         string
	InOutID        uint32
	Size           uint32
	FormattedSize  string
	Total          float64
	Confirmations  uint64
	Time           dbtypes.TimeDef
	ReceivedTotal  float64
	SentTotal      float64
	IsFunding      bool
	MatchedTx      string
	MatchedTxIndex uint32
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

// TrimmedTxInfo for use with /nexthome
type TrimmedTxInfo struct {
	*TxBasic
	Fees      float64
	VinCount  int
	VoutCount int
	VoteValid bool
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
	Time             dbtypes.TimeDef
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
	Index           uint32
	DisplayText     string
	TextIsHash      bool
	Link            string
}

// Vout models basic data about a tx output for display
type Vout struct {
	Addresses       []string
	Amount          float64
	FormattedAmount string
	Type            string
	Spent           bool
	OP_RETURN       string
	Index           uint32
}

// TrimmedBlockInfo models data needed to display block info on the new home page
type TrimmedBlockInfo struct {
	Time         dbtypes.TimeDef
	Height       int64
	Total        float64
	Fees         float64
	Subsidy      *dcrjson.GetBlockSubsidyResult
	Votes        []*TrimmedTxInfo
	Tickets      []*TrimmedTxInfo
	Revocations  []*TrimmedTxInfo
	Transactions []*TrimmedTxInfo
}

// BlockInfo models data for display on the block page
type BlockInfo struct {
	*BlockBasic
	Version               int32
	Confirmations         int64
	StakeRoot             string
	MerkleRoot            string
	TxAvailable           bool
	Tx                    []*TrimmedTxInfo
	Tickets               []*TrimmedTxInfo
	Revs                  []*TrimmedTxInfo
	Votes                 []*TrimmedTxInfo
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
	MiningFee             float64
	StakeValidationHeight int64
	AllTxs                uint32
	Subsidy               *dcrjson.GetBlockSubsidyResult
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
	// https://github.com/decred/dcrdata/v3/issues/358 for details.
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
	CoinSupply            int64          `json:"coin_supply"`
	StakeDiff             float64        `json:"sdiff"`
	NextExpectedStakeDiff float64        `json:"next_expected_sdiff"`
	NextExpectedBoundsMin float64        `json:"next_expected_min"`
	NextExpectedBoundsMax float64        `json:"next_expected_max"`
	IdxBlockInWindow      int            `json:"window_idx"`
	IdxInRewardWindow     int            `json:"reward_idx"`
	Difficulty            float64        `json:"difficulty"`
	DevFund               int64          `json:"dev_fund"`
	DevAddress            string         `json:"dev_address"`
	TicketReward          float64        `json:"reward"`
	RewardPeriod          string         `json:"reward_period"`
	ASR                   float64        `json:"ASR"`
	NBlockSubsidy         BlockSubsidy   `json:"subsidy"`
	Params                ChainParams    `json:"params"`
	PoolInfo              TicketPoolInfo `json:"pool_info"`
	TotalLockedDCR        float64        `json:"total_locked_dcr"`
	HashRate              float64        `json:"hash_rate"`
	// HashRateChange defines the hashrate change in 24hrs
	HashRateChange float64 `json:"hash_rate_change"`
}

// BlockSubsidy is an implementation of dcrjson.GetBlockSubsidyResult
type BlockSubsidy struct {
	Total int64 `json:"total"`
	PoW   int64 `json:"pow"`
	PoS   int64 `json:"pos"`
	Dev   int64 `json:"dev"`
}

// TrimmedMempoolInfo models data needed to display mempool info on the new home page
type TrimmedMempoolInfo struct {
	Transactions []*TrimmedTxInfo
	Tickets      []*TrimmedTxInfo
	Votes        []*TrimmedTxInfo
	Revocations  []*TrimmedTxInfo
	Subsidy      BlockSubsidy
	Total        float64
	Time         int64
	Fees         float64
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

// TicketIndex is used to assign an index to a ticket hash.
type TicketIndex map[string]int

// BlockValidatorIndex keeps a list of arbitrary indexes for unique combinations
// of block hash and the ticket being spent to validate the block, i.e.
// map[validatedBlockHash]map[ticketHash]index.
type BlockValidatorIndex map[string]TicketIndex

// MempoolShort represents the mempool data sent as the mempool update
type MempoolShort struct {
	LastBlockHeight    int64               `json:"block_height"`
	LastBlockHash      string              `json:"block_hash"`
	LastBlockTime      int64               `json:"block_time"`
	TotalOut           float64             `json:"total"`
	TotalSize          int32               `json:"size"`
	NumTickets         int                 `json:"num_tickets"`
	NumVotes           int                 `json:"num_votes"`
	NumRegular         int                 `json:"num_regular"`
	NumRevokes         int                 `json:"num_revokes"`
	NumAll             int                 `json:"num_all"`
	LatestTransactions []MempoolTx         `json:"latest"`
	FormattedTotalSize string              `json:"formatted_size"`
	TicketIndexes      BlockValidatorIndex `json:"-"`
	VotingInfo         VotingInfo          `json:"voting_info"`
	InvRegular         map[string]struct{} `json:"-"`
	InvStake           map[string]struct{} `json:"-"`
}

// VotingInfo models data about the validity of the next block from mempool.
type VotingInfo struct {
	TicketsVoted     uint16 `json:"tickets_voted"`
	MaxVotesPerBlock uint16 `json:"max_votes_per_block"`
	votedTickets     map[string]bool
}

// ChainParams models simple data about the chain server's parameters used for
// some info on the front page.
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
			Time:      addrOut.TxBlockTime,
			InOutID:   addrOut.TxVinVoutIndex,
			TxID:      addrOut.TxHash,
			TxType:    txhelpers.TxTypeToString(int(addrOut.TxType)),
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
	Block *BlockInfo `json:"block"`
	Extra *HomeInfo  `json:"extra"`
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
	TxID      string         `json:"txid"`
	Fees      float64        `json:"fees"`
	VinCount  int            `json:"vin_count"`
	VoutCount int            `json:"vout_count"`
	Vin       []MempoolInput `json:"vin,omitempty"`
	Coinbase  bool           `json:"coinbase"`
	Hash      string         `json:"hash"`
	Time      int64          `json:"time"`
	Size      int32          `json:"size"`
	TotalOut  float64        `json:"total"`
	Type      string         `json:"Type"`
	VoteInfo  *VoteInfo      `json:"vote_info,omitempty"`
}

// NewMempoolTx models data sent from the notification handler
type NewMempoolTx struct {
	Time int64
	Hex  string
}

// MempoolVin is minimal information about the inputs of a mempool transaction.
type MempoolVin struct {
	TxId   string
	Inputs []MempoolInput
}

// MempoolInput is basic information about a transaction input.
type MempoolInput struct {
	TxId   string `json:"txid"`
	Index  uint32 `json:"index"`
	Outdex uint32 `json:"vout"`
}

// ExtendedChainParams represents the data of ChainParams
type ExtendedChainParams struct {
	Params               *chaincfg.Params
	MaximumBlockSize     int
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
		"P2PKH address prefix. Standard wallet address. 1 public key -> 1 private key",
		"Ed25519 P2PKH address prefix",
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

// StatsInfo represents all of the data for the stats page.
type StatsInfo struct {
	UltimateSupply             int64
	TotalSupply                int64
	TotalSupplyPercentage      float64
	ProjectFunds               int64
	ProjectAddress             string
	PoWDiff                    float64
	HashRate                   float64
	BlockReward                int64
	NextBlockReward            int64
	PoWReward                  int64
	PoSReward                  int64
	ProjectFundReward          int64
	VotesInMempool             int
	TicketsInMempool           int
	TicketPrice                float64
	NextEstimatedTicketPrice   float64
	TicketPoolSize             uint32
	TicketPoolSizePerToTarget  float64
	TicketPoolValue            float64
	TPVOfTotalSupplyPeecentage float64
	TicketsROI                 float64
	RewardPeriod               string
	ASR                        float64
	APR                        float64
	IdxBlockInWindow           int
	WindowSize                 int64
	BlockTime                  int64
	IdxInRewardWindow          int
	RewardWindowSize           int64
}

// CommonPageData is the basis for data structs used for HTML templates.
// explorerUI.commonData returns an initialized instance or CommonPageData,
// which itself should be used to initialize page data template structs.
type CommonPageData struct {
	Tip           *WebBasicBlock
	Version       string
	ChainParams   *chaincfg.Params
	BlockTimeUnix int64
	DevAddress    string
}

// isSyncExplorerUpdate helps determine when the explorer should be updated
// when the blockchain sync is running in the background and no explorer page
// view restriction on the running webserver is activated.
// explore.DisplaySyncStatusPage must be false for this to used.
var isSyncExplorerUpdate = new(syncUpdateExplorer)

type syncUpdateExplorer struct {
	sync.RWMutex
	DoStatusUpdate bool
}

// SetSyncExplorerUpdateStatus is a thread-safe way to set when the explorer
// should be updated with the latest blocks synced.
func SetSyncExplorerUpdateStatus(status bool) {
	isSyncExplorerUpdate.Lock()
	defer isSyncExplorerUpdate.Unlock()

	isSyncExplorerUpdate.DoStatusUpdate = status
}

// SyncExplorerUpdateStatus is thread-safe to check the current set explorer update status.
func SyncExplorerUpdateStatus() bool {
	isSyncExplorerUpdate.RLock()
	defer isSyncExplorerUpdate.RUnlock()

	return isSyncExplorerUpdate.DoStatusUpdate
}

// syncStatus makes it possible to update the user on the progress of the
// blockchain db syncing that is running after new blocks were detected on
// system startup. ProgressBars is an array whose every entry is one of the
// progress bars data that will be displayed on the sync status page.
type syncStatus struct {
	sync.RWMutex
	ProgressBars []SyncStatusInfo
}

// SyncStatusInfo defines information for a single progress bar.
type SyncStatusInfo struct {
	// PercentComplete is the percentage of sync complete for a given progress bar.
	PercentComplete float64 `json:"percentage_complete"`
	// BarMsg holds the main bar message about the currect sync.
	BarMsg string `json:"bar_msg"`
	// BarSubtitle holds any other information about the current main sync. This
	// value may include but not limited to; db indexing, deleting duplicates etc.
	BarSubtitle string `json:"subtitle"`
	// Time is the estimated time in seconds to the sync should be complete.
	Time int64 `json:"seconds_to_complete"`
	// ProgressBarID is the given entry progress bar id needed on the UI page.
	ProgressBarID string `json:"progress_bar_id"`
}

// SyncStatus defines a thread-safe way to read the sync status updates
func SyncStatus() []SyncStatusInfo {
	blockchainSyncStatus.RLock()
	defer blockchainSyncStatus.RUnlock()

	return blockchainSyncStatus.ProgressBars
}

// UnspentOutputIndices finds the indices of the transaction outputs that
// appear unspent. The indices returned are the index within the passed slice,
// not within the transaction.
func UnspentOutputIndices(vouts []Vout) (unspents []int) {
	for idx := range vouts {
		vout := vouts[idx]
		if vout.Amount == 0.0 || vout.Spent {
			continue
		}
		unspents = append(unspents, idx)
	}
	return
}

// MsgTxMempoolInputs parses a MsgTx and creates a list of MempoolInput.
func MsgTxMempoolInputs(msgTx *wire.MsgTx) (inputs []MempoolInput) {
	for vindex := range msgTx.TxIn {
		outpoint := msgTx.TxIn[vindex].PreviousOutPoint
		outId := outpoint.Hash.String()
		inputs = append(inputs, MempoolInput{
			TxId:   outId,
			Index:  uint32(vindex),
			Outdex: outpoint.Index,
		})
	}
	return
}
