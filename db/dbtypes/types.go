package dbtypes

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/decred/dcrdata/db/dbtypes/internal"
)

// Tickets have 6 states, 5 possible fates:
// Live -...---> Voted
//           \-> Missed (unspent) [--> Revoked]
//            \--...--> Expired (unspent) [--> Revoked]

type TicketSpendType int16

const (
	TicketUnspent TicketSpendType = iota
	TicketRevoked
	TicketVoted
)

func (p TicketSpendType) String() string {
	switch p {
	case TicketUnspent:
		return "unspent"
	case TicketRevoked:
		return "revoked"
	case TicketVoted:
		return "Voted"
	default:
		return "unknown"
	}
}

type TicketPoolStatus int16

const (
	PoolStatusLive TicketPoolStatus = iota
	PoolStatusVoted
	PoolStatusExpired
	PoolStatusMissed
)

func (p TicketPoolStatus) String() string {
	switch p {
	case PoolStatusLive:
		return "live"
	case PoolStatusVoted:
		return "Voted"
	case PoolStatusExpired:
		return "expired"
	case PoolStatusMissed:
		return "missed"
	default:
		return "unknown"
	}
}

// SyncResult is the result of a database sync operation, containing the height
// of the last block and an arror value.
type SyncResult struct {
	Height int64
	Error  error
}

// JSONB is used to implement the sql.Scanner and driver.Valuer interfaces
// required for the type to make a postgresql compatible JSONB type.
type JSONB map[string]interface{}

// Value satisfies driver.Valuer
func (p VinTxPropertyARRAY) Value() (driver.Value, error) {
	j, err := json.Marshal(p)
	return j, err
}

// Scan satisfies sql.Scanner
func (p *VinTxPropertyARRAY) Scan(src interface{}) error {
	source, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("scan type assertion .([]byte) failed")
	}

	var i interface{}
	err := json.Unmarshal(source, &i)
	if err != nil {
		return err
	}

	// Set this JSONB
	is, ok := i.([]interface{})
	if !ok {
		return fmt.Errorf("type assertion .([]interface{}) failed")
	}
	numVin := len(is)
	ba := make(VinTxPropertyARRAY, numVin)
	for ii := range is {
		VinTxPropertyMapIface, ok := is[ii].(map[string]interface{})
		if !ok {
			return fmt.Errorf("type assertion .(map[string]interface) failed")
		}
		b, _ := json.Marshal(VinTxPropertyMapIface)
		json.Unmarshal(b, &ba[ii])
	}
	*p = ba

	return nil
}

// VinTxPropertyARRAY is a slice of VinTxProperty sturcts that implements
// sql.Scanner and driver.Valuer.
type VinTxPropertyARRAY []VinTxProperty

// func VinTxPropertyToJSONB(vin *VinTxProperty) (JSONB, error) {
// 	var vinJSONB map[string]interface{}
// 	vinJSON, err := json.Marshal(vin)
// 	if err != nil {
// 		return vinJSONB, err
// 	}
// 	var vinInterface interface{}
// 	err = json.Unmarshal(vinJSON, &vinInterface)
// 	if err != nil {
// 		return vinJSONB, err
// 	}
// 	vinJSONB = vinInterface.(map[string]interface{})
// 	return vinJSONB, nil
// }

// UInt64Array represents a one-dimensional array of PostgreSQL integer types
type UInt64Array []uint64

// Scan implements the sql.Scanner interface.
func (a *UInt64Array) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		return a.scanBytes(src)
	case string:
		return a.scanBytes([]byte(src))
	case nil:
		*a = nil
		return nil
	}

	return fmt.Errorf("pq: cannot convert %T to UInt64Array", src)
}

func (a *UInt64Array) scanBytes(src []byte) error {
	elems, err := internal.ScanLinearArray(src, []byte{','}, "UInt64Array")
	if err != nil {
		return err
	}
	if *a != nil && len(elems) == 0 {
		*a = (*a)[:0]
	} else {
		b := make(UInt64Array, len(elems))
		for i, v := range elems {
			if b[i], err = strconv.ParseUint(string(v), 10, 64); err != nil {
				return fmt.Errorf("pq: parsing array element index %d: %v", i, err)
			}
		}
		*a = b
	}
	return nil
}

// Value implements the driver.Valuer interface.
func (a UInt64Array) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}

	if n := len(a); n > 0 {
		// There will be at least two curly brackets, N bytes of values,
		// and N-1 bytes of delimiters.
		b := make([]byte, 1, 1+2*n)
		b[0] = '{'

		b = strconv.AppendUint(b, a[0], 10)
		for i := 1; i < n; i++ {
			b = append(b, ',')
			b = strconv.AppendUint(b, a[i], 10)
		}

		return string(append(b, '}')), nil
	}

	return "{}", nil
}

// Vout defines a transaction output
type Vout struct {
	// txDbID           int64
	TxHash           string           `json:"tx_hash"`
	TxIndex          uint32           `json:"tx_index"`
	TxTree           int8             `json:"tx_tree"`
	Value            uint64           `json:"value"`
	Version          uint16           `json:"version"`
	ScriptPubKey     []byte           `json:"pkScriptHex"`
	ScriptPubKeyData ScriptPubKeyData `json:"pkScript"`
}

// AddressRow represents a row in the addresses table
type AddressRow struct {
	// id int64
	Address            string
	FundingTxDbID      uint64
	FundingTxHash      string
	FundingTxVoutIndex uint32
	VoutDbID           uint64
	Value              uint64
	SpendingTxDbID     uint64
	SpendingTxHash     string
	SpendingTxVinIndex uint32
	VinDbID            uint64
}

// ScriptPubKeyData is part of the result of decodescript(ScriptPubKeyHex)
type ScriptPubKeyData struct {
	ReqSigs   uint32   `json:"reqSigs"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses"`
}

// VinTxProperty models a transaction input with previous outpoint information.
type VinTxProperty struct {
	PrevOut     string `json:"prevout"`
	PrevTxHash  string `json:"prevtxhash"`
	PrevTxIndex uint32 `json:"prevvoutidx"`
	PrevTxTree  uint16 `json:"tree"`
	Sequence    uint32 `json:"sequence"`
	ValueIn     uint64 `json:"amountin"`
	TxID        string `json:"tx_hash"`
	TxIndex     uint32 `json:"tx_index"`
	TxTree      uint16 `json:"tx_tree"`
	BlockHeight uint32 `json:"blockheight"`
	BlockIndex  uint32 `json:"blockindex"`
	ScriptHex   []byte `json:"scripthex"`
}

// Vin models a transaction input.
type Vin struct {
	//txDbID      int64
	Coinbase    string  `json:"coinbase"`
	TxHash      string  `json:"txhash"`
	VoutIdx     uint32  `json:"voutidx"`
	Tree        int8    `json:"tree"`
	Sequence    uint32  `json:"sequence"`
	AmountIn    float64 `json:"amountin"`
	BlockHeight uint32  `json:"blockheight"`
	BlockIndex  uint32  `json:"blockindex"`
	ScriptHex   string  `json:"scripthex"`
}

// ScriptSig models the signature script used to redeem the origin transaction
// as a JSON object (non-coinbase txns only)
type ScriptSig struct {
	Asm string `json:"asm"`
	Hex string `json:"hex"`
}

// Tx models a Decred transaction. It is stored in a Block.
type Tx struct {
	//blockDbID  int64
	BlockHash   string `json:"block_hash"`
	BlockHeight int64  `json:"block_height"`
	BlockTime   int64  `json:"block_time"`
	Time        int64  `json:"time"`
	TxType      int16  `json:"tx_type"`
	Version     uint16 `json:"version"`
	Tree        int8   `json:"tree"`
	TxID        string `json:"txid"`
	BlockIndex  uint32 `json:"block_index"`
	Locktime    uint32 `json:"locktime"`
	Expiry      uint32 `json:"expiry"`
	Size        uint32 `json:"size"`
	Spent       int64  `json:"spent"`
	Sent        int64  `json:"sent"`
	Fees        int64  `json:"fees"`
	NumVin      uint32 `json:"numvin"`
	//Vins        VinTxPropertyARRAY `json:"vins"`
	VinDbIds  []uint64 `json:"vindbids"`
	NumVout   uint32   `json:"numvout"`
	Vouts     []*Vout  `json:"vouts"`
	VoutDbIds []uint64 `json:"voutdbids"`
	// NOTE: VoutDbIds may not be needed if there is a vout table since each
	// vout will have a tx_dbid
}

// Block models a Decred block.
type Block struct {
	Hash         string `json:"hash"`
	Size         uint32 `json:"size"`
	Height       uint32 `json:"height"`
	Version      uint32 `json:"version"`
	MerkleRoot   string `json:"merkleroot"`
	StakeRoot    string `json:"stakeroot"`
	NumTx        uint32
	NumRegTx     uint32
	Tx           []string `json:"tx"`
	TxDbIDs      []uint64
	NumStakeTx   uint32
	STx          []string `json:"stx"`
	STxDbIDs     []uint64
	Time         uint64  `json:"time"`
	Nonce        uint64  `json:"nonce"`
	VoteBits     uint16  `json:"votebits"`
	FinalState   []byte  `json:"finalstate"`
	Voters       uint16  `json:"voters"`
	FreshStake   uint8   `json:"freshstake"`
	Revocations  uint8   `json:"revocations"`
	PoolSize     uint32  `json:"poolsize"`
	Bits         uint32  `json:"bits"`
	SBits        uint64  `json:"sbits"`
	Difficulty   float64 `json:"difficulty"`
	ExtraData    []byte  `json:"extradata"`
	StakeVersion uint32  `json:"stakeversion"`
	PreviousHash string  `json:"previousblockhash"`
}

type BlockDataBasic struct {
	Height     uint32  `json:"height,omitemtpy"`
	Size       uint32  `json:"size,omitemtpy"`
	Hash       string  `json:"hash,omitemtpy"`
	Difficulty float64 `json:"diff,omitemtpy"`
	StakeDiff  float64 `json:"sdiff,omitemtpy"`
	Time       int64   `json:"time,omitemtpy"`
	NumTx      uint32  `json:"txlength,omitempty"`
}
