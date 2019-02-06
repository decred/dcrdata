// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package types

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrdata/v4/db/dbtypes"
	"github.com/decred/dcrdata/v4/txhelpers"
)

// TimeAPI is a fall back dbtypes.TimeDef wrapper that allows API endpoints that
// previously returned a timestamp instead of a formatted string time to continue
// working.
type TimeAPI struct {
	S dbtypes.TimeDef
}

func (t TimeAPI) String() string {
	return t.S.String()
}

// MarshalJSON is set as the default marshalling function for TimeAPI struct.
func (t *TimeAPI) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.S.UNIX())
}

// much of the time, dcrdata will be using the types in dcrjson, but others are
// defined here

// BlockTransactions models an array of stake and regular transactions for a
// block
type BlockTransactions struct {
	Tx  []string `json:"tx"`
	STx []string `json:"stx"`
}

// tx raw
// tx short (tx raw - extra context)
// txout
// scriptPubKey (hex -> decodescript -> result)
// vout
// vin

// Tx models TxShort with the number of confirmations and block info Block
type Tx struct {
	TxShort
	Confirmations int64    `json:"confirmations"`
	Block         *BlockID `json:"block,omitempty"`
}

// TxShort models info about transaction TxID
type TxShort struct {
	TxID     string        `json:"txid"`
	Size     int32         `json:"size"`
	Version  int32         `json:"version"`
	Locktime uint32        `json:"locktime"`
	Expiry   uint32        `json:"expiry"`
	Vin      []dcrjson.Vin `json:"vin"`
	Vout     []Vout        `json:"vout"`
}

// AgendasInfo holds the high level details about an agenda.
type AgendasInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	*dbtypes.MileStone
	VoteVersion uint32 `json:"voteversion"`
	Mask        uint16 `json:"mask"`
}

// AgendaAPIResponse holds two sets of AgendaVoteChoices charts data.
type AgendaAPIResponse struct {
	ByHeight *dbtypes.AgendaVoteChoices `json:"by_height"`
	ByTime   *dbtypes.AgendaVoteChoices `json:"by_time"`
}

// TrimmedTx models data to resemble to result of the decoderawtransaction RPC.
type TrimmedTx struct {
	TxID     string        `json:"txid"`
	Version  int32         `json:"version"`
	Locktime uint32        `json:"locktime"`
	Expiry   uint32        `json:"expiry"`
	Vin      []dcrjson.Vin `json:"vin"`
	Vout     []Vout        `json:"vout"`
}

// Txns models the multi transaction post data structure
type Txns struct {
	Transactions []string `json:"transactions"`
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

// BlockID models very basic info about a block
type BlockID struct {
	BlockHash   string `json:"blockhash"`
	BlockHeight int64  `json:"blockheight"`
	BlockIndex  uint32 `json:"blockindex"`
	Time        int64  `json:"time"`
	BlockTime   int64  `json:"blocktime"`
}

// VoutMined appends a best block hash, number of confimations and if a
// transaction is a coinbase to a transaction output
type VoutMined struct {
	Vout
	BestBlock     string `json:"bestblock"`
	Confirmations int64  `json:"confirmations"`
	Coinbase      bool   `json:"coinbase"`
}

// Vout defines a transaction output
type Vout struct {
	Value               float64      `json:"value"`
	N                   uint32       `json:"n"`
	Version             uint16       `json:"version"`
	ScriptPubKeyDecoded ScriptPubKey `json:"scriptPubKey"`
	Spend               *TxInputID   `json:"spend,omitempty"`
}

// TxInputID specifies a transaction input as hash:vin_index.
type TxInputID struct {
	Hash  string `json:"hash"`
	Index uint32 `json:"vin_index"`
}

// ScriptClass represent the type of a transaction output's pkscript. The values
// of this type are NOT compatible with dcrd's txscript.ScriptClass values! Use
// ScriptClassFromName to get a text representation of a ScriptClass.
type ScriptClass uint8

// Classes of script payment known about in the blockchain.
const (
	ScriptClassNonStandard     ScriptClass = iota // None of the recognized forms.
	ScriptClassPubKey                             // Pay pubkey.
	ScriptClassPubkeyAlt                          // Alternative signature pubkey.
	ScriptClassPubKeyHash                         // Pay pubkey hash.
	ScriptClassPubkeyHashAlt                      // Alternative signature pubkey hash.
	ScriptClassScriptHash                         // Pay to script hash.
	ScriptClassMultiSig                           // Multi signature.
	ScriptClassNullData                           // Empty data-only (provably prunable).
	ScriptClassStakeSubmission                    // Stake submission.
	ScriptClassStakeGen                           // Stake generation
	ScriptClassStakeRevocation                    // Stake revocation.
	ScriptClassStakeSubChange                     // Change for stake submission tx.
	ScriptClassInvalid
)

var scriptClassToName = map[ScriptClass]string{
	ScriptClassNonStandard:     "nonstandard",
	ScriptClassPubKey:          "pubkey",
	ScriptClassPubkeyAlt:       "pubkeyalt",
	ScriptClassPubKeyHash:      "pubkeyhash",
	ScriptClassPubkeyHashAlt:   "pubkeyhashalt",
	ScriptClassScriptHash:      "scripthash",
	ScriptClassMultiSig:        "multisig",
	ScriptClassNullData:        "nulldata",
	ScriptClassStakeSubmission: "stakesubmission",
	ScriptClassStakeGen:        "stakegen",
	ScriptClassStakeRevocation: "stakerevoke",
	ScriptClassStakeSubChange:  "sstxchange",
	ScriptClassInvalid:         "invalid",
}

var scriptNameToClass = map[string]ScriptClass{
	"nonstandard":     ScriptClassNonStandard,
	"pubkey":          ScriptClassPubKey,
	"pubkeyalt":       ScriptClassPubkeyAlt,
	"pubkeyhash":      ScriptClassPubKeyHash,
	"pubkeyhashalt":   ScriptClassPubkeyHashAlt,
	"scripthash":      ScriptClassScriptHash,
	"multisig":        ScriptClassMultiSig,
	"nulldata":        ScriptClassNullData,
	"stakesubmission": ScriptClassStakeSubmission,
	"stakegen":        ScriptClassStakeGen,
	"stakerevoke":     ScriptClassStakeRevocation,
	"sstxchange":      ScriptClassStakeSubChange,
	// No "invalid" mapping!
}

// ScriptClassFromName attempts to identify the ScriptClass for the given script
// class/type name. An unknown script name will return ScriptClassInvalid. This
// may be used to map the Type field of the ScriptPubKey data type to a known
// class. If dcrd's txscript package changes its strings, this function may be
// unable to identify the types from dcrd.
func ScriptClassFromName(name string) ScriptClass {
	class, found := scriptNameToClass[strings.ToLower(name)]
	if !found {
		return ScriptClassInvalid // not even non-standard
	}
	return class
}

// IsValidScriptClass indicates the the provided string corresponds to a known
// ScriptClass (including "nonstandard"). Note that "invalid" is not valid,
// although a ScriptClassInvalid value mapping to "invalid" exists.
func IsValidScriptClass(name string) (isValid bool) {
	_, isValid = scriptNameToClass[strings.ToLower(name)]
	return
}

// String returns the name of the ScriptClass. If the ScriptClass is
// unrecognized it is treated as ScriptClassInvalid.
func (sc ScriptClass) String() string {
	name, found := scriptClassToName[sc]
	if !found {
		return ScriptClassInvalid.String() // better be in scriptClassToName!
	}
	return name
}

// IsNullDataScript indicates if the script class name is a nulldata class.
func IsNullDataScript(name string) bool {
	return name == ScriptClassNullData.String()
}

// ScriptPubKey is the result of decodescript(ScriptPubKeyHex)
type ScriptPubKey struct {
	Asm       string   `json:"asm"`
	Hex       string   `json:"hex"`
	ReqSigs   int32    `json:"reqSigs,omitempty"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses,omitempty"`
	CommitAmt *float64 `json:"commitamt,omitempty"`
}

// TxOut defines a decred transaction output.
type TxOut struct {
	Value               float64      `json:"value"`
	Version             uint16       `json:"version"`
	ScriptPubKeyDecoded ScriptPubKey `json:"scriptPubKey"`
}

// TxIn defines a decred transaction input.
type TxIn struct {
	// Non-witness
	PreviousOutPoint OutPoint `json:"prevout"`
	Sequence         uint32   `json:"sequence"`

	// Witness
	ValueIn         float64 `json:"value"`
	BlockHeight     uint32  `json:"blockheight"`
	BlockIndex      uint32  `json:"blockindex"`
	SignatureScript string  `json:"sigscript"`
}

// OutPoint is used to track previous transaction outputs.
type OutPoint struct {
	Hash  string `json:"hash"`
	Index uint32 `json:"index"`
	Tree  int8   `json:"tree"`
}

// Address models the address string with the transactions as AddressTxShort
type Address struct {
	Address      string            `json:"address"`
	Transactions []*AddressTxShort `json:"address_transactions"`
}

// AddressTxRaw is modeled from SearchRawTransactionsResult but with size in
// place of hex
type AddressTxRaw struct {
	Size          int32                `json:"size"`
	TxID          string               `json:"txid"`
	Version       int32                `json:"version"`
	Locktime      uint32               `json:"locktime"`
	Vin           []dcrjson.VinPrevOut `json:"vin"`
	Vout          []Vout               `json:"vout"`
	Confirmations int64                `json:"confirmations"`
	BlockHash     string               `json:"blockhash"`
	Time          TimeAPI              `json:"time,omitempty"`
	Blocktime     TimeAPI              `json:"blocktime,omitempty"`
}

// AddressTxShort is a subset of AddressTxRaw with just the basic tx details
// pertaining the particular address
type AddressTxShort struct {
	TxID          string  `json:"txid"`
	Size          int32   `json:"size"`
	Time          TimeAPI `json:"time"`
	Value         float64 `json:"value"`
	Confirmations int64   `json:"confirmations"`
}

// AddressTotals represents the number and value of spent and unspent outputs
// for an address.
type AddressTotals struct {
	Address      string  `json:"address"`
	BlockHash    string  `json:"blockhash"`
	BlockHeight  uint64  `json:"blockheight"`
	NumSpent     int64   `json:"num_stxos"`
	NumUnspent   int64   `json:"num_utxos"`
	CoinsSpent   float64 `json:"dcr_spent"`
	CoinsUnspent float64 `json:"dcr_unspent"`
}

// BlockDataWithTxType adds an array of TxRawWithTxType to
// dcrjson.GetBlockVerboseResult to include the stake transaction type
type BlockDataWithTxType struct {
	*dcrjson.GetBlockVerboseResult
	Votes   []TxRawWithVoteInfo
	Tickets []dcrjson.TxRawResult
	Revs    []dcrjson.TxRawResult
}

// TxRawWithVoteInfo adds the vote info to dcrjson.TxRawResult
type TxRawWithVoteInfo struct {
	dcrjson.TxRawResult
	VoteInfo VoteInfo
}

// TxRawWithTxType adds the stake transaction type to dcrjson.TxRawResult
type TxRawWithTxType struct {
	dcrjson.TxRawResult
	TxType string
}

// ScriptSig models the signature script used to redeem the origin transaction
// as a JSON object (non-coinbase txns only)
type ScriptSig struct {
	Asm string `json:"asm,omitempty"`
	Hex string `json:"hex,omitempty"`
}

// PrevOut represents previous output for an input Vin.
type PrevOut struct {
	Addresses []string `json:"addresses,omitempty"`
	Value     float64  `json:"value"`
}

// VinPrevOut is like Vin except it includes PrevOut.  It is used by
// searchrawtransaction
type VinPrevOut struct {
	Coinbase    string     `json:"coinbase"`
	Stakebase   string     `json:"stakebase"`
	Txid        string     `json:"txid"`
	Vout        uint32     `json:"vout"`
	Tree        int8       `json:"tree"`
	AmountIn    *float64   `json:"amountin,omitempty"`
	BlockHeight *uint32    `json:"blockheight,omitempty"`
	BlockIndex  *uint32    `json:"blockindex,omitempty"`
	ScriptSig   *ScriptSig `json:"scriptSig"`
	PrevOut     *PrevOut   `json:"prevOut"`
	Sequence    uint32     `json:"sequence"`
}

// end copy-paste from dcrjson

// Status indicates the state of the server, including the API version and the
// software version.
type Status struct {
	sync.RWMutex    `json:"-"`
	Ready           bool   `json:"ready"`
	DBHeight        uint32 `json:"db_height"`
	DBLastBlockTime int64  `json:"db_block_time"`
	Height          uint32 `json:"node_height"`
	NodeConnections int64  `json:"node_connections"`
	APIVersion      int    `json:"api_version"`
	DcrdataVersion  string `json:"dcrdata_version"`
	NetworkName     string `json:"network_name"`
}

func (s *Status) GetHeight() uint32 {
	s.RLock()
	defer s.RUnlock()
	return s.Height
}

// CoinSupply models the coin supply at a certain best block.
type CoinSupply struct {
	Height   int64  `json:"block_height"`
	Hash     string `json:"block_hash"`
	Mined    int64  `json:"supply_mined"`
	Ultimate int64  `json:"supply_ultimate"`
}

// TicketPoolInfo models data about ticket pool
type TicketPoolInfo struct {
	Height  uint32   `json:"height"`
	Size    uint32   `json:"size"`
	Value   float64  `json:"value"`
	ValAvg  float64  `json:"valavg"`
	Winners []string `json:"winners"`
}

// TicketPoolValsAndSizes models two arrays, one each for ticket values and
// sizes for blocks StartHeight to EndHeight
type TicketPoolValsAndSizes struct {
	StartHeight uint32    `json:"start_height"`
	EndHeight   uint32    `json:"end_height"`
	Value       []float64 `json:"value"`
	Size        []float64 `json:"size"`
}

// BlockDataBasic models primary information about a block.
type BlockDataBasic struct {
	Height     uint32  `json:"height"`
	Size       uint32  `json:"size"`
	Hash       string  `json:"hash"`
	Difficulty float64 `json:"diff"`
	StakeDiff  float64 `json:"sdiff"`
	Time       TimeAPI `json:"time"`
	NumTx      uint32  `json:"txlength"`
	MiningFee  *int64  `json:"fees,omitempty"`
	TotalSent  *int64  `json:"total_sent,omitempty"`
	// TicketPoolInfo may be nil for side chain blocks.
	PoolInfo *TicketPoolInfo `json:"ticket_pool,omitempty"`
}

// NewBlockDataBasic constructs a *BlockDataBasic with pointer fields allocated.
func NewBlockDataBasic() *BlockDataBasic {
	return &BlockDataBasic{
		PoolInfo: new(TicketPoolInfo),
	}
}

// BlockExplorerBasic models primary information about block at height Height
// for the block explorer.
type BlockExplorerBasic struct {
	Height      uint32          `json:"height"`
	Size        uint32          `json:"size"`
	Voters      uint16          `json:"votes"`
	FreshStake  uint8           `json:"tickets"`
	Revocations uint8           `json:"revocations"`
	StakeDiff   float64         `json:"sdiff"`
	Time        dbtypes.TimeDef `json:"time"`
	BlockExplorerExtraInfo
}

// BlockExplorerExtraInfo contains supplemental block metadata used by the
// explorer.
type BlockExplorerExtraInfo struct {
	TxLen            int                            `json:"tx"`
	CoinSupply       int64                          `json:"coin_supply"`
	NextBlockSubsidy *dcrjson.GetBlockSubsidyResult `json:"next_block_subsidy"`
}

// BlockTransactionCounts contains the regular and stake transaction counts for
// a block.
type BlockTransactionCounts struct {
	Tx  int `json:"tx"`
	STx int `json:"stx"`
}

// BlockSubsidies contains the block reward proportions for a certain block
// height. The stake_reward is per vote, while total is for a certain number of
// votes.
type BlockSubsidies struct {
	BlockNum   int64  `json:"height"`
	BlockHash  string `json:"hash,omitempty"`
	Work       int64  `json:"work_reward"`
	Stake      int64  `json:"stake_reward"`
	NumVotes   int16  `json:"num_votes,omitempty"`
	TotalStake int64  `json:"stake_reward_total,omitempty"`
	Tax        int64  `json:"project_subsidy"`
	Total      int64  `json:"total,omitempty"`
}

// StakeDiff represents data about the evaluated stake difficulty and estimates
type StakeDiff struct {
	dcrjson.GetStakeDifficultyResult
	Estimates        dcrjson.EstimateStakeDiffResult `json:"estimates"`
	IdxBlockInWindow int                             `json:"window_block_index"`
	PriceWindowNum   int                             `json:"window_number"`
}

// StakeInfoExtended models data about the fee, pool and stake difficulty
type StakeInfoExtended struct {
	Hash             string               `json:"hash"`
	Feeinfo          dcrjson.FeeInfoBlock `json:"feeinfo"`
	StakeDiff        float64              `json:"stakediff"`
	PriceWindowNum   int                  `json:"window_number"`
	IdxBlockInWindow int                  `json:"window_block_index"`
	PoolInfo         *TicketPoolInfo      `json:"ticket_pool"`
}

// NewStakeInfoExtended constructs a *StakeInfoExtended with pointer fields
// allocated.
func NewStakeInfoExtended() *StakeInfoExtended {
	return &StakeInfoExtended{
		PoolInfo: new(TicketPoolInfo),
	}
}

// StakeInfoExtendedEstimates is similar to StakeInfoExtended but includes stake
// difficulty estimates with the stake difficulty
type StakeInfoExtendedEstimates struct {
	Hash             string               `json:"hash"`
	Feeinfo          dcrjson.FeeInfoBlock `json:"feeinfo"`
	StakeDiff        StakeDiff            `json:"stakediff"`
	PriceWindowNum   int                  `json:"window_number"`
	IdxBlockInWindow int                  `json:"window_block_index"`
	PoolInfo         *TicketPoolInfo      `json:"ticket_pool"`
}

// MempoolTicketFeeInfo models statistical ticket fee info at block height
// Height
type MempoolTicketFeeInfo struct {
	Height uint32 `json:"height"`
	Time   int64  `json:"time"`
	dcrjson.FeeInfoMempool
	LowestMineable float64 `json:"lowest_mineable"`
}

// MempoolTicketFees models info about ticket fees at block height Height
type MempoolTicketFees struct {
	Height   uint32    `json:"height"`
	Time     int64     `json:"time"`
	Length   uint32    `json:"length"`
	Total    uint32    `json:"total"`
	FeeRates []float64 `json:"top_fees"`
}

// TicketDetails models details about ticket Hash received at height Height
type TicketDetails struct {
	Hash    string  `json:"hash"`
	Fee     float64 `json:"abs_fee"`
	FeeRate float64 `json:"fee"`
	Size    int32   `json:"size"`
	Height  int64   `json:"height_received"`
}

// MempoolTicketDetails models basic mempool info with ticket details Tickets
type MempoolTicketDetails struct {
	Height  uint32         `json:"height"`
	Time    int64          `json:"time"`
	Length  uint32         `json:"length"`
	Total   uint32         `json:"total"`
	Tickets TicketsDetails `json:"tickets"`
}

// TicketsDetails is an array of pointers of TicketDetails used in
// MempoolTicketDetails
type TicketsDetails []*TicketDetails

// TicketPoolChartsData is for data used to display ticket pool statistics at
// /ticketpool.
type TicketPoolChartsData struct {
	ChartHeight uint64                   `json:"height"`
	TimeChart   *dbtypes.PoolTicketsData `json:"time_chart"`
	PriceChart  *dbtypes.PoolTicketsData `json:"price_chart"`
	DonutChart  *dbtypes.PoolTicketsData `json:"donut_chart"`
	Mempool     *PriceCountTime          `json:"mempool"`
}

// PriceCountTime is a basic set of information about ticket in the mempool.
type PriceCountTime struct {
	Price float64         `json:"price"`
	Count int             `json:"count"`
	Time  dbtypes.TimeDef `json:"time"`
}
