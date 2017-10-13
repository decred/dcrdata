package dbtypes

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// SyncResult is the result of a database sync operation, containing the height
// of the last block and an arror value.
type SyncResult struct {
	Height int64
	Error  error
}

// JSONB is used to implement the sql.Scanner and driver.Valuer interfaces
// requried for the type to make a postgresql compatible JSONB type.
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
		return fmt.Errorf("Scan type assertion .([]byte) failed.")
	}

	var i interface{}
	err := json.Unmarshal(source, &i)
	if err != nil {
		return err
	}

	// Set this JSONB
	is, ok := i.([]interface{})
	if !ok {
		return fmt.Errorf("Type assertion .([]interface{}) failed.")
	}
	numVin := len(is)
	ba := make(VinTxPropertyARRAY, numVin)
	for ii := range is {
		VinTxPropertyMapIface, ok := is[ii].(map[string]interface{})
		if !ok {
			return fmt.Errorf("Type assertion .(map[string]interface) failed.")
		}
		b, _ := json.Marshal(VinTxPropertyMapIface)
		json.Unmarshal(b, &ba[ii])
	}
	*p = ba

	return nil
}

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

// ScriptPubKeyData is part of the result of decodescript(ScriptPubKeyHex)
type ScriptPubKeyData struct {
	ReqSigs   uint32   `json:"reqSigs"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses"`
}

type VinTxProperty struct {
	PrevOut     string `json:"prevout"`
	PrevTxHash  string `json:"prevtxhash"`
	PrevTxIndex uint32 `json:"prevvoutidx"`
	PrevTxTree  uint16 `json:"tree"`
	Sequence    uint32 `json:"sequence"`
	ValueIn     uint64 `json:"amountin"`
	TxID        string `json:"tx_hash"`
	TxIndex     uint32 `json:"tx_index"`
	BlockHeight uint32 `json:"blockheight"`
	BlockIndex  uint32 `json:"blockindex"`
	ScriptHex   []byte `json:"scripthex"`
}

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

type Tx struct {
	//blockDbID  int64
	BlockHash  string             `json:"block_hash"`
	BlockIndex uint32             `json:"block_index"`
	Tree       int8               `json:"tree"`
	TxID       string             `json:"txid"`
	Version    uint16             `json:"version"`
	Locktime   uint32             `json:"locktime"`
	Expiry     uint32             `json:"expiry"`
	NumVin     uint32             `json:"numvin"`
	Vins       VinTxPropertyARRAY `json:"vins"`
	VinDbIds   []uint64           `json:"vindbids"`
	NumVout    uint32             `json:"numvout"`
	Vouts      []*Vout            `json:"vouts"`
	VoutDbIds  []uint64           `json:"voutdbids"`
	// NOTE: VoutDbIds may not be needed if there is a vout table since each
	// vout will have a tx_dbid
}

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
	//NextHash     string   `json:"nextblockhash"`
}
