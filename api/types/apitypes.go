// Copyright (c) 2019-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/txhelpers"
)

// TimeAPI is a fall back dbtypes.TimeDef wrapper that allows API endpoints that
// previously returned a timestamp instead of a formatted string time to continue
// working.
type TimeAPI struct {
	S dbtypes.TimeDef
}

var _ fmt.Stringer = TimeAPI{}

// String formats the time in a human-friendly layout.
func (t TimeAPI) String() string {
	return t.S.String()
}

// UNIX returns the UNIX epoch time stamp.
func (t TimeAPI) UNIX() int64 {
	return t.S.UNIX()
}

var _ json.Marshaler = (*TimeAPI)(nil)
var _ json.Unmarshaler = (*TimeAPI)(nil)

// MarshalJSON is set as the default marshalling function for TimeAPI struct.
func (t *TimeAPI) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.S.UNIX())
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *TimeAPI) UnmarshalJSON(data []byte) error {
	if t == nil {
		return fmt.Errorf("TimeAPI: UnmarshalJSON on nil pointer")
	}
	var ts int64
	if err := json.Unmarshal(data, &ts); err != nil {
		return err
	}
	*t = NewTimeAPIFromUNIX(ts)
	return nil
}

// NewTimeAPI constructs a TimeAPI from the given time.Time. It presets the
// timezone for formatting to UTC.
func NewTimeAPI(t time.Time) TimeAPI {
	return TimeAPI{
		S: dbtypes.NewTimeDef(t),
	}
}

// NewTimeAPIFromUNIX constructs a TimeAPI from the given UNIX epoch time stamp
// in seconds. It presets the timezone for formatting to UTC.
func NewTimeAPIFromUNIX(t int64) TimeAPI {
	return NewTimeAPI(time.Unix(t, 0))
}

// much of the time, dcrdata will be using the types in chainjson, but others
// are defined here

// BlockTransactions models an array of stake and regular transactions for a
// block
type BlockTransactions struct {
	Tx  []string `json:"tx"`
	STx []string `json:"stx"`
}

// Tx models TxShort with the number of confirmations and block info Block
type Tx struct {
	TxShort
	Confirmations int64    `json:"confirmations"`
	Block         *BlockID `json:"block,omitempty"`
}

// Vin is an alias for dcrd's rpc/jsonrpc/types/v4.Vin type.
type Vin = chainjson.Vin

// TxShort models info about transaction TxID
type TxShort struct {
	TxID     string `json:"txid"`
	Size     int32  `json:"size"`
	Version  int32  `json:"version"`
	Locktime uint32 `json:"locktime"`
	Expiry   uint32 `json:"expiry"`
	Vin      []Vin  `json:"vin"`
	Vout     []Vout `json:"vout"`
	Tree     int8   `json:"tree"`
	Type     string `json:"type"`
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
	TxID     string `json:"txid"`
	Version  int32  `json:"version"`
	Locktime uint32 `json:"locktime"`
	Expiry   uint32 `json:"expiry"`
	Vin      []Vin  `json:"vin"`
	Vout     []Vout `json:"vout"`
}

// Txns models the multi transaction post data structure
type Txns struct {
	Transactions []string `json:"transactions"`
}

// TSpendVote describes how a SSGen transaction decided on a tspend.
type TSpendVote struct {
	TSpend string `json:"tspend"`
	Choice uint8  `json:"choice"`
}

// VoteInfo models data about a SSGen transaction (vote)
type VoteInfo struct {
	Validation BlockValidation         `json:"block_validation"`
	Version    uint32                  `json:"vote_version"`
	Bits       uint16                  `json:"vote_bits"`
	Choices    []*txhelpers.VoteChoice `json:"vote_choices"`
	TSpends    []*TSpendVote           `json:"tspend_votes,omitempty"`
}

// ConvertTSpendVotes converts into the api's TSpendVote format.
func ConvertTSpendVotes(tspendChoices []*txhelpers.TSpendVote) []*TSpendVote {
	tspendVotes := make([]*TSpendVote, len(tspendChoices))
	for i := range tspendChoices {
		tspendVotes[i] = &TSpendVote{
			TSpend: tspendChoices[i].TSpend.String(),
			Choice: tspendChoices[i].Choice,
		}
	}
	return tspendVotes
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

// BlockRaw contains the hexadecimal encoded bytes of a serialized block.
type BlockRaw struct {
	Height uint32 `json:"height"`
	Hash   string `json:"hash"`
	Hex    string `json:"hex"`
}

// VoutMined appends a best block hash, number of confirmations and if a
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
	Spend               *TxInputID   `json:"spend,omitempty"` // unused?
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
	ScriptClassStakeSubCommit                     // Pseudo-class, actually nulldata odd outputs of stake submission (tickets)
	ScriptClassTreasuryAdd                        // Treasury Add (e.g. treasury add tx types, or 0th output of treasury base tx)
	ScriptClassTreasuryGen                        // Treasury Generation (e.g. >0th outputs of treasury spend)
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
	ScriptClassStakeSubCommit:  "sstxcommitment",
	ScriptClassTreasuryAdd:     "treasuryadd",
	ScriptClassTreasuryGen:     "treasurygen",
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
	"sstxcommitment":  ScriptClassStakeSubCommit,
	"treasuryadd":     ScriptClassTreasuryAdd,
	"treasurygen":     ScriptClassTreasuryGen,

	// No "invalid" mapping!
}

// NewScriptClass converts a stdscript.ScriptType to the ScriptClass type, which
// is less fine-grained with respect to the stake subtypes.
func NewScriptClass(sc stdscript.ScriptType) ScriptClass {
	switch sc {
	case stdscript.STNonStandard:
		return ScriptClassNonStandard
	case stdscript.STPubKeyEcdsaSecp256k1:
		return ScriptClassPubKey
	case stdscript.STPubKeyHashEcdsaSecp256k1:
		return ScriptClassPubKeyHash
	case stdscript.STScriptHash:
		return ScriptClassScriptHash
	case stdscript.STMultiSig:
		return ScriptClassMultiSig
	case stdscript.STNullData:
		return ScriptClassNullData // maybe ScriptClassStakeSubCommit!
	case stdscript.STStakeSubmissionPubKeyHash, stdscript.STStakeSubmissionScriptHash:
		return ScriptClassStakeSubmission
	case stdscript.STStakeGenPubKeyHash, stdscript.STStakeGenScriptHash:
		return ScriptClassStakeGen
	case stdscript.STStakeRevocationPubKeyHash, stdscript.STStakeRevocationScriptHash:
		return ScriptClassStakeRevocation
	case stdscript.STStakeChangePubKeyHash, stdscript.STStakeChangeScriptHash:
		return ScriptClassStakeSubChange
	case stdscript.STPubKeyEd25519, stdscript.STPubKeySchnorrSecp256k1:
		return ScriptClassPubkeyAlt
	case stdscript.STPubKeyHashEd25519, stdscript.STPubKeyHashSchnorrSecp256k1:
		return ScriptClassPubkeyHashAlt
	case stdscript.STTreasuryGenPubKeyHash, stdscript.STTreasuryGenScriptHash:
		return ScriptClassTreasuryGen
	case stdscript.STTreasuryAdd:
		return ScriptClassTreasuryAdd
	}
	return ScriptClassInvalid
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
	Version   uint16   `json:"version"`
	ReqSigs   int32    `json:"reqSigs,omitempty"`
	Type      string   `json:"type"` // Consider ScriptClass with marshal/unmarshal methods
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

// ScriptSig models the signature script used to redeem a transaction output.
// type ScriptSig struct {
// 	Asm string `json:"asm,omitempty"`
// 	Hex string `json:"hex,omitempty"`
// }

// VinShort describes a transaction input with limited detail, for the address
// txn API endpoints. In particular, there is no ScriptSig or Sequence, and the
// string fields for Coinbase, Stakebase, and TreasurySpend are just booleans.
type VinShort struct {
	Coinbase      bool    `json:"coinbase"`
	Stakebase     bool    `json:"stakebase"`
	Treasurybase  bool    `json:"treasurybase"`
	TreasurySpend bool    `json:"treasuryspend"`
	Txid          string  `json:"txid"`
	Vout          uint32  `json:"vout"`
	Tree          int8    `json:"tree"`
	AmountIn      float64 `json:"amountin"`
	BlockHeight   *uint32 `json:"blockheight,omitempty"`
	BlockIndex    *uint32 `json:"blockindex,omitempty"`
	// No ScriptSig or Sequence
}

// MarshalJSON is used to marshal a Vin to JSON with special handling for when
// the these are generate coins (should be zero txid).
func (v *VinShort) MarshalJSON() ([]byte, error) {
	switch {
	case v.Coinbase:
		generated := struct {
			Coinbase bool    `json:"coinbase"`
			AmountIn float64 `json:"amountin"`
		}{
			Coinbase: true,
			AmountIn: v.AmountIn,
		}
		return json.Marshal(generated)
	case v.Stakebase:
		generated := struct {
			Stakebase bool    `json:"stakebase"`
			AmountIn  float64 `json:"amountin"`
		}{
			Stakebase: true,
			AmountIn:  v.AmountIn,
		}
		return json.Marshal(generated)
	case v.Treasurybase:
		generated := struct {
			Treasurybase bool    `json:"treasurybase"`
			AmountIn     float64 `json:"amountin"`
		}{
			Treasurybase: true,
			AmountIn:     v.AmountIn,
		}
		return json.Marshal(generated)
	case v.TreasurySpend:
		generated := struct {
			TreasurySpend bool    `json:"treasuryspend"`
			AmountIn      float64 `json:"amountin"`
		}{
			TreasurySpend: true,
			AmountIn:      v.AmountIn,
		}
		return json.Marshal(generated)

	}

	return json.Marshal(struct {
		Txid        string  `json:"txid"`
		Vout        uint32  `json:"vout"`
		Tree        int8    `json:"tree"`
		AmountIn    float64 `json:"amountin"`
		BlockHeight *uint32 `json:"blockheight,omitempty"`
		BlockIndex  *uint32 `json:"blockindex,omitempty"`
	}{
		Txid:        v.Txid,
		Vout:        v.Vout,
		Tree:        v.Tree,
		AmountIn:    v.AmountIn,
		BlockHeight: v.BlockHeight,
		BlockIndex:  v.BlockIndex,
	})
}

// AddressTxRaw is modeled from SearchRawTransactionsResult but with size in
// place of hex, and a limited vin structure.
type AddressTxRaw struct {
	Size          int32      `json:"size"`
	TxID          string     `json:"txid"`
	Version       int32      `json:"version"`
	Locktime      uint32     `json:"locktime"`
	Type          int32      `json:"type"`
	Vin           []VinShort `json:"vin"`
	Vout          []Vout     `json:"vout"`
	Confirmations int64      `json:"confirmations"`
	BlockHash     string     `json:"blockhash,omitempty"`
	Time          TimeAPI    `json:"time,omitempty"`      // for mempool txns?
	Blocktime     *TimeAPI   `json:"blocktime,omitempty"` // vs mined?
	// BlockHeight   int64                  `json:"blockheight"`
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
// chainjson.GetBlockVerboseResult to include the stake transaction type
type BlockDataWithTxType struct {
	*chainjson.GetBlockVerboseResult
	Votes   []TxRawWithVoteInfo
	Tickets []chainjson.TxRawResult
	Revs    []chainjson.TxRawResult
}

// TxRawWithVoteInfo adds the vote info to chainjson.TxRawResult
type TxRawWithVoteInfo struct {
	chainjson.TxRawResult
	VoteInfo VoteInfo
}

// TxRawWithTxType adds the stake transaction type to chainjson.TxRawResult
type TxRawWithTxType struct {
	chainjson.TxRawResult
	TxType string
}

// Status indicates the state of the server. All fields are mutex protected and
// and should be set with the getters and setters.
type Status struct {
	sync.RWMutex
	ready           bool
	dbHeight        uint32
	dbLastBlockTime int64
	height          uint32
	nodeConnections int64
	api             APIStatus
}

// APIStatus is for the JSON-formatted response at /status.
type APIStatus struct {
	Ready           bool   `json:"ready"`
	DBHeight        uint32 `json:"db_height"`
	DBLastBlockTime int64  `json:"db_block_time"`
	Height          uint32 `json:"node_height"`
	NodeConnections int64  `json:"node_connections"`
	APIVersion      int    `json:"api_version"`
	DcrdataVersion  string `json:"dcrdata_version"`
	NetworkName     string `json:"network_name"`
}

// NewStatus is the constructor for a new Status.
func NewStatus(nodeHeight uint32, conns int64, apiVersion int, dcrdataVersion, netName string) *Status {
	return &Status{
		height:          nodeHeight,
		nodeConnections: conns,
		api: APIStatus{
			APIVersion:     apiVersion,
			DcrdataVersion: dcrdataVersion,
			NetworkName:    netName,
		},
	}
}

// API is a method for creating an APIStatus from Status.
func (s *Status) API() APIStatus {
	s.RLock()
	defer s.RUnlock()
	return APIStatus{
		Ready:           s.ready,
		DBHeight:        s.dbHeight,
		DBLastBlockTime: s.dbLastBlockTime,
		Height:          s.height,
		NodeConnections: s.nodeConnections,
		APIVersion:      s.api.APIVersion,
		DcrdataVersion:  s.api.DcrdataVersion,
		NetworkName:     s.api.NetworkName,
	}
}

// Happy describes just how happy dcrdata is.
type Happy struct {
	Happy           bool  `json:"happy"`
	APIReady        bool  `json:"api_ready"`
	TipAge          int64 `json:"tip_age"`
	NodeConnections int64 `json:"node_connections"`
}

// Happy indicates how dcrdata is or isn't happy.
func (s *Status) Happy() Happy {
	s.RLock()
	blockAge := time.Since(time.Unix(s.dbLastBlockTime, 0))
	h := Happy{
		APIReady:        s.ready, // The DB is serving data from the network's best block.
		TipAge:          int64(blockAge.Seconds()),
		NodeConnections: s.nodeConnections,
	}
	blocksBehind := s.height - s.dbHeight
	s.RUnlock()

	// If the DB is at node height, the age of the best block may be fairly
	// large, corresponding to an extremely low likelihood block interval.
	blockAgeLimit := 90 * time.Minute
	// If the DB is not at node height, allow the DB to lag if the age of the
	// best block in the DB is very recent, suggesting that the network's best
	// block may still be processing.
	if blocksBehind > 0 {
		blockAgeLimit = 30 * time.Second // processing rarely takes longer than a couple seconds
	}
	h.Happy = blockAge < blockAgeLimit && h.NodeConnections > 0
	return h
}

// Height is the last known node height.
func (s *Status) Height() uint32 {
	s.RLock()
	defer s.RUnlock()
	return s.height
}

// SetHeight stores the node height. Additionally, Status.ready is set to true
// if Status.height is the same as Status.dbHeight.
func (s *Status) SetHeight(height uint32) {
	s.Lock()
	s.ready = height == s.dbHeight && s.nodeConnections > 0
	s.height = height
	s.Unlock()
}

// DBHeight is the block most recently stored in the DB.
func (s *Status) DBHeight() uint32 {
	s.RLock()
	defer s.RUnlock()
	return s.dbHeight
}

// NodeConnections gets the number of node peer connections.
func (s *Status) NodeConnections() int64 {
	s.RLock()
	defer s.RUnlock()
	return s.nodeConnections
}

// SetConnections sets the node connection count.
func (s *Status) SetConnections(conns int64) {
	s.Lock()
	s.nodeConnections = conns
	s.ready = s.ready && s.nodeConnections > 0
	s.Unlock()
}

// SetReady sets the ready state.
func (s *Status) SetReady(ready bool) {
	s.Lock()
	s.ready = ready
	s.Unlock()
}

// Ready checks the ready state.
func (s *Status) Ready() bool {
	s.Lock()
	defer s.Unlock()
	return s.ready
}

// DBUpdate updates both the height and time of the best DB block. Status.ready
// is set to true if Status.height is the same as Status.dbHeight and the node
// has connections.
func (s *Status) DBUpdate(height uint32, blockTime int64) {
	s.Lock()
	s.dbHeight = height
	s.dbLastBlockTime = blockTime
	s.ready = s.dbHeight == s.height
	s.Unlock()
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
	Size        []uint32  `json:"size"`
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
	TxLen            int                              `json:"tx"`
	CoinSupply       int64                            `json:"coin_supply"`
	NextBlockSubsidy *chainjson.GetBlockSubsidyResult `json:"next_block_subsidy"`
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
	chainjson.GetStakeDifficultyResult
	Estimates        chainjson.EstimateStakeDiffResult `json:"estimates"`
	IdxBlockInWindow int                               `json:"window_block_index"`
	PriceWindowNum   int                               `json:"window_number"`
}

// StakeInfoExtended models data about the fee, pool and stake difficulty
type StakeInfoExtended struct {
	Hash             string                 `json:"hash"`
	Feeinfo          chainjson.FeeInfoBlock `json:"feeinfo"`
	StakeDiff        float64                `json:"stakediff"`
	PriceWindowNum   int                    `json:"window_number"`
	IdxBlockInWindow int                    `json:"window_block_index"`
	PoolInfo         *TicketPoolInfo        `json:"ticket_pool"`
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
	Hash             string                 `json:"hash"`
	Feeinfo          chainjson.FeeInfoBlock `json:"feeinfo"`
	StakeDiff        StakeDiff              `json:"stakediff"`
	PriceWindowNum   int                    `json:"window_number"`
	IdxBlockInWindow int                    `json:"window_block_index"`
	PoolInfo         *TicketPoolInfo        `json:"ticket_pool"`
}

// MempoolTicketFeeInfo models statistical ticket fee info at block height
// Height
type MempoolTicketFeeInfo struct {
	Height uint32 `json:"height"`
	Time   int64  `json:"time"`
	chainjson.FeeInfoMempool
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

// TicketInfo combines spend and pool statuses and relevant block and spending
// transaction IDs.
type TicketInfo struct {
	Status           string     `json:"status"`
	PurchaseBlock    *TinyBlock `json:"purchase_block"`
	MaturityHeight   uint32     `json:"maturity_height"`
	ExpirationHeight uint32     `json:"expiration_height"`
	LotteryBlock     *TinyBlock `json:"lottery_block"`
	Vote             *string    `json:"vote"`
	Revocation       *string    `json:"revocation"`
}

// TinyBlock is the hash and height of a block.
type TinyBlock struct {
	Hash   string `json:"hash"`
	Height uint32 `json:"height"`
}

// TicketPoolChartsData is for data used to display ticket pool statistics at
// /ticketpool.
type TicketPoolChartsData struct {
	ChartHeight  uint64                   `json:"height"`
	TimeChart    *dbtypes.PoolTicketsData `json:"time_chart"`
	PriceChart   *dbtypes.PoolTicketsData `json:"price_chart"`
	OutputsChart *dbtypes.PoolTicketsData `json:"outputs_chart"`
	Mempool      *PriceCountTime          `json:"mempool"`
}

// PowerlessTicket is the purchase block height and value of a missed or expired
// ticket.
type PowerlessTicket struct {
	Height uint32  `json:"h"`
	Price  float64 `json:"p"`
}

// PowerlessTickets contains expired and missed tickets sorted into slices of
// unspent and revoked.
type PowerlessTickets struct {
	Unspent []PowerlessTicket `json:"unspent"`
	Revoked []PowerlessTicket `json:"revoked"`
}

// PriceCountTime is a basic set of information about ticket in the mempool.
type PriceCountTime struct {
	Price float64         `json:"price"`
	Count int             `json:"count"`
	Time  dbtypes.TimeDef `json:"time"`
}
