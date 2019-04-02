// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

// Package txhelpers contains helper functions for working with transactions and
// blocks (e.g. checking for a transaction in a block).
package txhelpers

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/decred/base58"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient/v2"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
)

var (
	zeroHash            = chainhash.Hash{}
	zeroHashStringBytes = []byte(chainhash.Hash{}.String())
)

var CoinbaseFlags = "/dcrd/"
var CoinbaseScript = append([]byte{0x00, 0x00}, []byte(CoinbaseFlags)...)

// ReorgData contains details of a chain reorganization, including the full old
// and new chains, and the common ancestor that should not be included in either
// chain. Below is the description of the reorg data with the letter indicating
// the various blocks in the chain:
// 			A  -> B  -> C
//   			\  -> B' -> C' -> D'
// CommonAncestor - Hash of A
// OldChainHead - Hash of C
// OldChainHeight - Height of C
// OldChain - Chain from B to C
// NewChainHead - Hash of D'
// NewChainHeight - Height of D'
// NewChain - Chain from B' to D'
type ReorgData struct {
	CommonAncestor chainhash.Hash
	OldChainHead   chainhash.Hash
	OldChainHeight int32
	OldChain       []chainhash.Hash
	NewChainHead   chainhash.Hash
	NewChainHeight int32
	NewChain       []chainhash.Hash
	WG             *sync.WaitGroup
}

// RawTransactionGetter is an interface satisfied by rpcclient.Client, and
// required by functions that would otherwise require a rpcclient.Client just
// for GetRawTransaction.
type RawTransactionGetter interface {
	GetRawTransaction(txHash *chainhash.Hash) (*dcrutil.Tx, error)
}

// VerboseTransactionGetter is an interface satisfied by rpcclient.Client, and
// required by functions that would otherwise require a rpcclient.Client just
// for GetRawTransactionVerbose.
type VerboseTransactionGetter interface {
	GetRawTransactionVerbose(txHash *chainhash.Hash) (*dcrjson.TxRawResult, error)
	GetRawTransactionVerboseAsync(txHash *chainhash.Hash) rpcclient.FutureGetRawTransactionVerboseResult
}

// BlockWatchedTx contains, for a certain block, the transactions for certain
// watched addresses
type BlockWatchedTx struct {
	BlockHeight   int64
	TxsForAddress map[string][]*dcrutil.Tx
}

// TxAction is what is happening to the transaction (mined or inserted into
// mempool).
type TxAction int32

// Valid values for TxAction
const (
	TxMined TxAction = 1 << iota
	TxInserted
	// removed? invalidated?
)

// HashInSlice determines if a hash exists in a slice of hashes.
func HashInSlice(h chainhash.Hash, list []chainhash.Hash) bool {
	for _, hash := range list {
		if h == hash {
			return true
		}
	}
	return false
}

// TxhashInSlice searches a slice of *dcrutil.Tx for a transaction with the hash
// txHash. If found, it returns the corresponding *Tx, otherwise nil.
func TxhashInSlice(txs []*dcrutil.Tx, txHash *chainhash.Hash) *dcrutil.Tx {
	if len(txs) < 1 {
		return nil
	}

	for _, minedTx := range txs {
		txSha := minedTx.Hash()
		if txHash.IsEqual(txSha) {
			return minedTx
		}
	}
	return nil
}

// IncludesStakeTx checks if a block contains a stake transaction hash
func IncludesStakeTx(txHash *chainhash.Hash, block *dcrutil.Block) (int, int8) {
	blockTxs := block.STransactions()

	if tx := TxhashInSlice(blockTxs, txHash); tx != nil {
		return tx.Index(), tx.Tree()
	}
	return -1, -1
}

// IncludesTx checks if a block contains a transaction hash
func IncludesTx(txHash *chainhash.Hash, block *dcrutil.Block) (int, int8) {
	blockTxs := block.Transactions()

	if tx := TxhashInSlice(blockTxs, txHash); tx != nil {
		return tx.Index(), tx.Tree()
	}
	return -1, -1
}

// FilterHashSlice removes elements from the specified if the doRemove function
// evaluates to true for a given element. For example, given a slice of hashes
// called blackList that should be removed from the slice hashList:
//
// hashList = FilterHashSlice(hashList, func(h chainhash.Hash) bool {
//  return HashInSlice(h, blackList)
// })
func FilterHashSlice(s []chainhash.Hash, doRemove func(h chainhash.Hash) bool) []chainhash.Hash {
	_s := s[:0]
	for _, h := range s {
		if !doRemove(h) {
			_s = append(_s, h)
		}
	}
	return _s
}

// PrevOut contains a transaction input's previous outpoint, the Hash of the
// spending (following) transaction, and input index in the transaction.
type PrevOut struct {
	TxSpending       chainhash.Hash
	InputIndex       int
	PreviousOutpoint *wire.OutPoint
}

// TxWithBlockData contains a MsgTx and the block hash and height in which it
// was mined and Time it entered MemPool.
type TxWithBlockData struct {
	Tx          *wire.MsgTx
	BlockHeight int64
	BlockHash   string
	MemPoolTime int64
}

// Hash returns the chainhash.Hash of the transaction.
func (t *TxWithBlockData) Hash() chainhash.Hash {
	return t.Tx.TxHash()
}

// Confirmed indicates if the transaction is confirmed (mined).
func (t *TxWithBlockData) Confirmed() bool {
	return t.BlockHeight > 0 && len(t.BlockHash) <= chainhash.MaxHashStringSize
}

// AddressOutpoints collects spendable and spent transactions outpoints paying
// to a certain address. The transactions referenced by the outpoints are stored
// for quick access.
type AddressOutpoints struct {
	Address   string
	Outpoints []*wire.OutPoint
	PrevOuts  []PrevOut
	TxnsStore map[chainhash.Hash]*TxWithBlockData
}

// NewAddressOutpoints creates a new AddressOutpoints, initializing the
// transaction store/cache, and setting the address string.
func NewAddressOutpoints(address string) *AddressOutpoints {
	return &AddressOutpoints{
		Address:   address,
		TxnsStore: make(map[chainhash.Hash]*TxWithBlockData),
	}
}

// Update appends the provided outpoints, and merges the transactions.
func (a *AddressOutpoints) Update(txns []*TxWithBlockData,
	outpoints []*wire.OutPoint, prevOutpoint []PrevOut) {
	// Relevant outpoints
	a.Outpoints = append(a.Outpoints, outpoints...)

	// Previous outpoints (inputs)
	a.PrevOuts = append(a.PrevOuts, prevOutpoint...)

	// Referenced transactions
	for _, t := range txns {
		a.TxnsStore[t.Hash()] = t
	}
}

// Merge concatenates the outpoints of two AddressOutpoints, and merges the
// transactions.
func (a *AddressOutpoints) Merge(ao *AddressOutpoints) {
	// Relevant outpoints
	a.Outpoints = append(a.Outpoints, ao.Outpoints...)

	// Previous outpoints (inputs)
	a.PrevOuts = append(a.PrevOuts, ao.PrevOuts...)

	// Referenced transactions
	for h, t := range ao.TxnsStore {
		a.TxnsStore[h] = t
	}
}

// TxInvolvesAddress checks the inputs and outputs of a transaction for
// involvement of the given address.
func TxInvolvesAddress(msgTx *wire.MsgTx, addr string, c VerboseTransactionGetter,
	params *chaincfg.Params) (outpoints []*wire.OutPoint,
	prevOuts []PrevOut, prevTxs []*TxWithBlockData) {
	// The outpoints of this transaction paying to the address
	outpoints = TxPaysToAddress(msgTx, addr, params)
	// The inputs of this transaction funded by outpoints of previous
	// transactions paying to the address.
	prevOuts, prevTxs = TxConsumesOutpointWithAddress(msgTx, addr, c, params)
	return
}

// MempoolAddressStore organizes AddressOutpoints by address.
type MempoolAddressStore map[string]*AddressOutpoints

// TxnsStore allows quick lookup of a TxWithBlockData by transaction Hash.
type TxnsStore map[chainhash.Hash]*TxWithBlockData

// TxOutpointsByAddr sets the Outpoints field for the AddressOutpoints stored in
// the input MempoolAddressStore. For addresses not yet present in the
// MempoolAddressStore, a new AddressOutpoints is added to the store. The
// provided MempoolAddressStore must be initialized. The number of msgTx outputs
// that pay to any address are counted and returned. The addresses paid to by
// the transaction are listed in the output addrs map, where the value of the
// stored bool indicates the address is new to the MempoolAddressStore.
func TxOutpointsByAddr(txAddrOuts MempoolAddressStore, msgTx *wire.MsgTx, params *chaincfg.Params) (newOuts int, addrs map[string]bool) {
	if txAddrOuts == nil {
		panic("TxAddressOutpoints: input map must be initialized: map[string]*AddressOutpoints")
	}

	// Check the addresses associated with the PkScript of each TxOut.
	txTree := TxTree(msgTx)
	hash := msgTx.TxHash()
	addrs = make(map[string]bool)
	for outIndex, txOut := range msgTx.TxOut {
		_, txOutAddrs, _, err := txscript.ExtractPkScriptAddrs(txOut.Version,
			txOut.PkScript, params)
		if err != nil {
			fmt.Printf("ExtractPkScriptAddrs: %v", err.Error())
			continue
		}
		if len(txOutAddrs) == 0 {
			continue
		}
		newOuts++

		// Check if we are watching any address for this TxOut.
		for _, txAddr := range txOutAddrs {
			addr := txAddr.EncodeAddress()

			op := wire.NewOutPoint(&hash, uint32(outIndex), txTree)

			addrOuts := txAddrOuts[addr]
			if addrOuts == nil {
				addrOuts = &AddressOutpoints{
					Address:   addr,
					Outpoints: []*wire.OutPoint{op},
				}
				txAddrOuts[addr] = addrOuts
				addrs[addr] = true // new
				continue
			}
			if _, found := addrs[addr]; !found {
				addrs[addr] = false // not new to the address store
			}
			addrOuts.Outpoints = append(addrOuts.Outpoints, op)
		}
	}
	return
}

// TxPrevOutsByAddr sets the PrevOuts field for the AddressOutpoints stored in
// the MempoolAddressStore. For addresses not yet present in the
// MempoolAddressStore, a new AddressOutpoints is added to the store. The
// provided MempoolAddressStore must be initialized. A VerboseTransactionGetter
// is required to retrieve the pkScripts for the previous outpoints. The number
// of consumed previous outpoints that paid addresses in the provided
// transaction are counted and returned. The addresses in the previous outpoints
// are listed in the output addrs map, where the value of the stored bool
// indicates the address is new to the MempoolAddressStore.
func TxPrevOutsByAddr(txAddrOuts MempoolAddressStore, txnsStore TxnsStore, msgTx *wire.MsgTx, c VerboseTransactionGetter, params *chaincfg.Params) (newPrevOuts int, addrs map[string]bool) {
	if txAddrOuts == nil {
		panic("TxPrevOutAddresses: input map must be initialized: map[string]*AddressOutpoints")
	}
	if txnsStore == nil {
		panic("TxPrevOutAddresses: input map must be initialized: map[string]*AddressOutpoints")
	}

	// Send all the raw transaction requests
	type promiseGetRawTransaction struct {
		result rpcclient.FutureGetRawTransactionVerboseResult
		inIdx  int
	}
	promisesGetRawTransaction := make([]promiseGetRawTransaction, 0, len(msgTx.TxIn))

	for inIdx, txIn := range msgTx.TxIn {
		hash := &txIn.PreviousOutPoint.Hash
		if zeroHash.IsEqual(hash) {
			continue // coinbase or stakebase
		}
		promisesGetRawTransaction = append(promisesGetRawTransaction, promiseGetRawTransaction{
			result: c.GetRawTransactionVerboseAsync(hash),
			inIdx:  inIdx,
		})
	}

	addrs = make(map[string]bool)

	// For each TxIn of this transaction, inspect the previous outpoint.
	for i := range promisesGetRawTransaction {
		// Previous outpoint for this TxIn
		inIdx := promisesGetRawTransaction[i].inIdx
		prevOut := &msgTx.TxIn[inIdx].PreviousOutPoint
		hash := prevOut.Hash

		prevTxRaw, err := promisesGetRawTransaction[i].result.Receive()
		if err != nil {
			fmt.Printf("Unable to get raw transaction for %v: %v\n", hash, err)
			return
		}

		if prevTxRaw.Txid != hash.String() {
			fmt.Printf("TxPrevOutsByAddr error: %v != %v", prevTxRaw.Txid, hash.String())
			continue
		}

		prevTx, err := MsgTxFromHex(prevTxRaw.Hex)
		if err != nil {
			fmt.Printf("TxPrevOutsByAddr: MsgTxFromHex failed: %s\n", err)
			continue
		}

		// prevOut.Index indicates which output.
		txOut := prevTx.TxOut[prevOut.Index]
		// Extract the addresses from this output's PkScript.
		_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
			txOut.Version, txOut.PkScript, params)
		if err != nil {
			fmt.Printf("TxPrevOutsByAddr: ExtractPkScriptAddrs: %v\n", err.Error())
			continue
		}

		if len(txAddrs) == 0 {
			fmt.Printf("pkScript of a previous transaction output "+
				"(%v:%d) unexpectedly encoded no addresses.",
				prevOut.Hash, prevOut.Index)
			continue
		}

		newPrevOuts++

		// Put the previous outpoint's transaction in the txnsStore.
		txnsStore[hash] = &TxWithBlockData{
			Tx:          prevTx,
			BlockHeight: prevTxRaw.BlockHeight,
			BlockHash:   prevTxRaw.BlockHash,
		}

		outpoint := wire.NewOutPoint(&hash,
			prevOut.Index, TxTree(prevTx))
		prevOutExtended := PrevOut{
			TxSpending:       msgTx.TxHash(),
			InputIndex:       inIdx,
			PreviousOutpoint: outpoint,
		}

		// For each address paid to by this previous outpoint, record the
		// previous outpoint and the containing transactions.
		for _, txAddr := range txAddrs {
			addr := txAddr.EncodeAddress()

			// Check if it is already in the address store.
			addrOuts := txAddrOuts[addr]
			if addrOuts == nil {
				// Insert into affected address map.
				addrs[addr] = true // new
				// Insert into the address store.
				txAddrOuts[addr] = &AddressOutpoints{
					Address:  addr,
					PrevOuts: []PrevOut{prevOutExtended},
				}
				continue
			}

			// Address already in the address store, append the prevout.
			addrOuts.PrevOuts = append(addrOuts.PrevOuts, prevOutExtended)

			// See if the address was new before processing this transaction or
			// if it was added by a different prevout consumed by this
			// transaction. Only set new=false if this is the first occurrence
			// of this address in a prevout of this transaction.
			if _, found := addrs[addr]; !found {
				addrs[addr] = false // not new to the address store
			}
		}
	}
	return
}

// TxConsumesOutpointWithAddress checks a transaction for inputs that spend an
// outpoint paying to the given address. Returned are the identified input
// indexes and the corresponding previous outpoints determined.
func TxConsumesOutpointWithAddress(msgTx *wire.MsgTx, addr string,
	c VerboseTransactionGetter, params *chaincfg.Params) (prevOuts []PrevOut, prevTxs []*TxWithBlockData) {
	// Send all the raw transaction requests
	type promiseGetRawTransaction struct {
		result rpcclient.FutureGetRawTransactionVerboseResult
		inIdx  int
	}
	numPrevOut := len(msgTx.TxIn)
	promisesGetRawTransaction := make([]promiseGetRawTransaction, 0, numPrevOut)

	for inIdx, txIn := range msgTx.TxIn {
		hash := &txIn.PreviousOutPoint.Hash
		if zeroHash.IsEqual(hash) {
			continue // coinbase or stakebase
		}
		promisesGetRawTransaction = append(promisesGetRawTransaction, promiseGetRawTransaction{
			result: c.GetRawTransactionVerboseAsync(hash),
			inIdx:  inIdx,
		})
	}

	// For each TxIn of this transaction, inspect the previous outpoint.
	for i := range promisesGetRawTransaction {
		// Previous outpoint for this TxIn
		inIdx := promisesGetRawTransaction[i].inIdx
		prevOut := &msgTx.TxIn[inIdx].PreviousOutPoint
		hash := prevOut.Hash

		prevTxRaw, err := promisesGetRawTransaction[i].result.Receive()
		if err != nil {
			fmt.Printf("Unable to get raw transaction for %v: %v\n", hash, err)
			return nil, nil
		}

		if prevTxRaw.Txid != hash.String() {
			fmt.Printf("%v != %v", prevTxRaw.Txid, hash.String())
			return nil, nil
		}

		prevTx, err := MsgTxFromHex(prevTxRaw.Hex)
		if err != nil {
			fmt.Printf("MsgTxFromHex failed: %s\n", err)
			continue
		}

		// prevOut.Index indicates which output.
		txOut := prevTx.TxOut[prevOut.Index]
		// Extract the addresses from this output's PkScript.
		_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
			txOut.Version, txOut.PkScript, params)
		if err != nil {
			fmt.Printf("ExtractPkScriptAddrs: %v\n", err.Error())
			continue
		}

		// For each address that matches the address of interest, record this
		// previous outpoint and the containing transactions.
		for _, txAddr := range txAddrs {
			addrstr := txAddr.EncodeAddress()
			if addr == addrstr {
				outpoint := wire.NewOutPoint(&hash,
					prevOut.Index, TxTree(prevTx))
				prevOuts = append(prevOuts, PrevOut{
					TxSpending:       msgTx.TxHash(),
					InputIndex:       inIdx,
					PreviousOutpoint: outpoint,
				})
				prevTxs = append(prevTxs, &TxWithBlockData{
					Tx:          prevTx,
					BlockHeight: prevTxRaw.BlockHeight,
					BlockHash:   prevTxRaw.BlockHash,
				})
			}
		}

	}
	return
}

// BlockConsumesOutpointWithAddresses checks the specified block to see if it
// includes transactions that spend from outputs created using any of the
// addresses in addrs. The TxAction for each address is not important, but it
// would logically be TxMined. Both regular and stake transactions are checked.
// The RPC client is used to get the PreviousOutPoint for each TxIn of each
// transaction in the block, from which the address is obtained from the
// PkScript of that output. chaincfg Params is required to decode the script.
func BlockConsumesOutpointWithAddresses(block *dcrutil.Block, addrs map[string]TxAction,
	c RawTransactionGetter, params *chaincfg.Params) map[string][]*dcrutil.Tx {
	addrMap := make(map[string][]*dcrutil.Tx)

	checkForOutpointAddr := func(blockTxs []*dcrutil.Tx) {
		for _, tx := range blockTxs {
			for _, txIn := range tx.MsgTx().TxIn {
				prevOut := &txIn.PreviousOutPoint
				if bytes.Equal(zeroHash[:], prevOut.Hash[:]) {
					continue
				}
				// For each TxIn, check the indicated vout index in the txid of the
				// previous outpoint.
				// txrr, err := c.GetRawTransactionVerbose(&prevOut.Hash)
				prevTx, err := c.GetRawTransaction(&prevOut.Hash)
				if err != nil {
					fmt.Printf("Unable to get raw transaction for %s\n", prevOut.Hash.String())
					continue
				}

				// prevOut.Index should tell us which one, but check all anyway
				for _, txOut := range prevTx.MsgTx().TxOut {
					_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
						txOut.Version, txOut.PkScript, params)
					if err != nil {
						fmt.Printf("ExtractPkScriptAddrs: %v\n", err.Error())
						continue
					}

					for _, txAddr := range txAddrs {
						addrstr := txAddr.EncodeAddress()
						if _, ok := addrs[addrstr]; ok {
							if addrMap[addrstr] == nil {
								addrMap[addrstr] = make([]*dcrutil.Tx, 0)
							}
							addrMap[addrstr] = append(addrMap[addrstr], prevTx)
						}
					}
				}
			}
		}
	}

	checkForOutpointAddr(block.Transactions())
	checkForOutpointAddr(block.STransactions())

	return addrMap
}

// TxPaysToAddress returns a slice of outpoints of a transaction which pay to
// specified address.
func TxPaysToAddress(msgTx *wire.MsgTx, addr string,
	params *chaincfg.Params) (outpoints []*wire.OutPoint) {
	// Check the addresses associated with the PkScript of each TxOut
	txTree := TxTree(msgTx)
	hash := msgTx.TxHash()
	for outIndex, txOut := range msgTx.TxOut {
		_, txOutAddrs, _, err := txscript.ExtractPkScriptAddrs(txOut.Version,
			txOut.PkScript, params)
		if err != nil {
			fmt.Printf("ExtractPkScriptAddrs: %v", err.Error())
			continue
		}

		// Check if we are watching any address for this TxOut
		for _, txAddr := range txOutAddrs {
			addrstr := txAddr.EncodeAddress()
			if addr == addrstr {
				outpoints = append(outpoints, wire.NewOutPoint(&hash,
					uint32(outIndex), txTree))
			}
		}
	}
	return
}

// BlockReceivesToAddresses checks a block for transactions paying to the
// specified addresses, and creates a map of addresses to a slice of dcrutil.Tx
// involving the address.
func BlockReceivesToAddresses(block *dcrutil.Block, addrs map[string]TxAction,
	params *chaincfg.Params) map[string][]*dcrutil.Tx {
	addrMap := make(map[string][]*dcrutil.Tx)

	checkForAddrOut := func(blockTxs []*dcrutil.Tx) {
		for _, tx := range blockTxs {
			// Check the addresses associated with the PkScript of each TxOut
			for _, txOut := range tx.MsgTx().TxOut {
				_, txOutAddrs, _, err := txscript.ExtractPkScriptAddrs(txOut.Version,
					txOut.PkScript, params)
				if err != nil {
					fmt.Printf("ExtractPkScriptAddrs: %v", err.Error())
					continue
				}

				// Check if we are watching any address for this TxOut
				for _, txAddr := range txOutAddrs {
					addrstr := txAddr.EncodeAddress()
					if _, ok := addrs[addrstr]; ok {
						if _, gotSlice := addrMap[addrstr]; !gotSlice {
							addrMap[addrstr] = make([]*dcrutil.Tx, 0) // nil
						}
						addrMap[addrstr] = append(addrMap[addrstr], tx)
					}
				}
			}
		}
	}

	checkForAddrOut(block.Transactions())
	checkForAddrOut(block.STransactions())

	return addrMap
}

// OutPointAddresses gets the addresses paid to by a transaction output.
func OutPointAddresses(outPoint *wire.OutPoint, c RawTransactionGetter,
	params *chaincfg.Params) ([]string, dcrutil.Amount, error) {
	// The addresses are encoded in the pkScript, so we need to get the
	// raw transaction, and the TxOut that contains the pkScript.
	prevTx, err := c.GetRawTransaction(&outPoint.Hash)
	if err != nil {
		return nil, 0, fmt.Errorf("unable to get raw transaction for %s", outPoint.Hash.String())
	}

	txOuts := prevTx.MsgTx().TxOut
	if len(txOuts) <= int(outPoint.Index) {
		return nil, 0, fmt.Errorf("PrevOut index (%d) is beyond the TxOuts slice (length %d)",
			outPoint.Index, len(txOuts))
	}

	// For the TxOut of interest, extract the list of addresses
	txOut := txOuts[outPoint.Index]
	_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
		txOut.Version, txOut.PkScript, params)
	if err != nil {
		return nil, 0, fmt.Errorf("ExtractPkScriptAddrs: %v", err.Error())
	}
	value := dcrutil.Amount(txOut.Value)
	addresses := make([]string, 0, len(txAddrs))
	for _, txAddr := range txAddrs {
		addr := txAddr.EncodeAddress()
		addresses = append(addresses, addr)
	}
	return addresses, value, nil
}

// OutPointAddressesFromString is the same as OutPointAddresses, but it takes
// the outpoint as the tx string, vout index, and tree.
func OutPointAddressesFromString(txid string, index uint32, tree int8,
	c RawTransactionGetter, params *chaincfg.Params) ([]string, error) {
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("Invalid hash %s", txid)
	}

	outPoint := wire.NewOutPoint(hash, index, tree)
	outPointAddress, _, err := OutPointAddresses(outPoint, c, params)
	return outPointAddress, err
}

// MedianAmount gets the median Amount from a slice of Amounts
func MedianAmount(s []dcrutil.Amount) dcrutil.Amount {
	if len(s) == 0 {
		return 0
	}

	sort.Sort(dcrutil.AmountSorter(s))

	middle := len(s) / 2

	if len(s) == 0 {
		return 0
	} else if (len(s) % 2) != 0 {
		return s[middle]
	}
	return (s[middle] + s[middle-1]) / 2
}

// MedianCoin gets the median DCR from a slice of float64s
func MedianCoin(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}

	sort.Float64s(s)

	middle := len(s) / 2

	if len(s) == 0 {
		return 0
	} else if (len(s) % 2) != 0 {
		return s[middle]
	}
	return (s[middle] + s[middle-1]) / 2
}

// GetDifficultyRatio returns the proof-of-work difficulty as a multiple of the
// minimum difficulty using the passed bits field from the header of a block.
func GetDifficultyRatio(bits uint32, params *chaincfg.Params) float64 {
	// The minimum difficulty is the max possible proof-of-work limit bits
	// converted back to a number.  Note this is not the same as the proof of
	// work limit directly because the block difficulty is encoded in a block
	// with the compact form which loses precision.
	max := blockchain.CompactToBig(params.PowLimitBits)
	target := blockchain.CompactToBig(bits)

	difficulty := new(big.Rat).SetFrac(max, target)
	outString := difficulty.FloatString(8)
	diff, err := strconv.ParseFloat(outString, 64)
	if err != nil {
		fmt.Printf("Cannot get difficulty: %v", err)
		return 0
	}
	return diff
}

// SSTXInBlock gets a slice containing all of the SSTX mined in a block
func SSTXInBlock(block *dcrutil.Block) []*dcrutil.Tx {
	_, txns := TicketTxnsInBlock(block)
	return txns
}

// SSGenVoteBlockValid determines if a vote transaction is voting yes or no to a
// block, and returns the votebits in case the caller wants to check agenda
// votes. The error return may be ignored if the input transaction is known to
// be a valid ssgen (vote), otherwise it should be checked.
func SSGenVoteBlockValid(msgTx *wire.MsgTx) (BlockValidation, uint16, error) {
	if !stake.IsSSGen(msgTx) {
		return BlockValidation{}, 0, fmt.Errorf("not a vote transaction")
	}

	ssGenVoteBits := stake.SSGenVoteBits(msgTx)
	blockHash, blockHeight := stake.SSGenBlockVotedOn(msgTx)
	blockValid := BlockValidation{
		Hash:     blockHash,
		Height:   int64(blockHeight),
		Validity: dcrutil.IsFlagSet16(ssGenVoteBits, dcrutil.BlockValid),
	}
	return blockValid, ssGenVoteBits, nil
}

// VoteBitsInBlock returns a list of vote bits for the votes in a block
func VoteBitsInBlock(block *dcrutil.Block) []stake.VoteVersionTuple {
	var voteBits []stake.VoteVersionTuple
	for _, stx := range block.MsgBlock().STransactions {
		if !stake.IsSSGen(stx) {
			continue
		}

		voteBits = append(voteBits, stake.VoteVersionTuple{
			Version: stake.SSGenVersion(stx),
			Bits:    stake.SSGenVoteBits(stx),
		})
	}

	return voteBits
}

// SSGenVoteBits returns the VoteBits of txOut[1] of a ssgen tx
func SSGenVoteBits(tx *wire.MsgTx) (uint16, error) {
	if len(tx.TxOut) < 2 {
		return 0, fmt.Errorf("not a ssgen")
	}

	pkScript := tx.TxOut[1].PkScript
	if len(pkScript) < 8 {
		return 0, fmt.Errorf("vote consensus version abent")
	}

	return binary.LittleEndian.Uint16(pkScript[2:4]), nil
}

// BlockValidation models the block validation indicated by an ssgen (vote)
// transaction.
type BlockValidation struct {
	// Hash is the hash of the block being targeted (in)validated
	Hash chainhash.Hash

	// Height is the height of the block
	Height int64

	// Validity indicates the vote is to validate (true) or invalidate (false)
	// the block.
	Validity bool
}

// VoteChoice represents the choice made by a vote transaction on a single vote
// item in an agenda. The ID, Description, and Mask fields describe the vote
// item for which the choice is being made. Those are the initial fields in
// chaincfg.Params.Deployments[VoteVersion][VoteIndex].
type VoteChoice struct {
	// Single unique word identifying the vote.
	ID string `json:"id"`

	// Longer description of what the vote is about.
	Description string `json:"description"`

	// Usable bits for this vote.
	Mask uint16 `json:"mask"`

	// VoteVersion and VoteIndex specify which vote item is referenced by this
	// VoteChoice (i.e. chaincfg.Params.Deployments[VoteVersion][VoteIndex]).
	VoteVersion uint32 `json:"vote_version"`
	VoteIndex   int    `json:"vote_index"`

	// ChoiceIdx indicates the corresponding element in the vote item's []Choice
	ChoiceIdx int `json:"choice_index"`

	// Choice is the selected choice for the specified vote item
	Choice *chaincfg.Choice `json:"choice"`
}

// VoteVersion extracts the vote version from the input pubkey script.
func VoteVersion(pkScript []byte) uint32 {
	if len(pkScript) < 8 {
		return stake.VoteConsensusVersionAbsent
	}

	return binary.LittleEndian.Uint32(pkScript[4:8])
}

// SSGenVoteChoices gets a ssgen's vote choices (block validity and any
// agendas). The vote's stake version, to which the vote choices correspond, and
// vote bits are also returned. Note that []*VoteChoice may be an empty slice if
// there are no consensus deployments for the transaction's vote version. The
// error value may be non-nil if the tx is not a valid ssgen.
func SSGenVoteChoices(tx *wire.MsgTx, params *chaincfg.Params) (BlockValidation, uint32, uint16, []*VoteChoice, error) {
	validBlock, voteBits, err := SSGenVoteBlockValid(tx)
	if err != nil {
		return validBlock, 0, 0, nil, err
	}

	// Determine the ssgen's vote version and get the relevant consensus
	// deployments containing the vote items targeted.
	voteVersion := stake.SSGenVersion(tx)
	deployments := params.Deployments[voteVersion]

	// Allocate space for each choice
	choices := make([]*VoteChoice, 0, len(deployments))

	// For each vote item (consensus deployment), extract the choice from the
	// vote bits and store the vote item's Id, Description and vote bits Mask.
	for d := range deployments {
		voteAgenda := &deployments[d].Vote
		choiceIndex := voteAgenda.VoteIndex(voteBits)
		voteChoice := VoteChoice{
			ID:          voteAgenda.Id,
			Description: voteAgenda.Description,
			Mask:        voteAgenda.Mask,
			VoteVersion: voteVersion,
			VoteIndex:   d,
			ChoiceIdx:   choiceIndex,
			Choice:      &voteAgenda.Choices[choiceIndex],
		}
		choices = append(choices, &voteChoice)
	}

	return validBlock, voteVersion, voteBits, choices, nil
}

// FeeInfoBlock computes ticket fee statistics for the tickets included in the
// specified block.
func FeeInfoBlock(block *dcrutil.Block) *dcrjson.FeeInfoBlock {
	feeInfo := new(dcrjson.FeeInfoBlock)
	_, sstxMsgTxns := TicketsInBlock(block)

	feeInfo.Height = uint32(block.Height())
	feeInfo.Number = uint32(len(sstxMsgTxns))

	var minFee, maxFee, meanFee float64
	minFee = math.MaxFloat64
	fees := make([]float64, feeInfo.Number)
	for it, msgTx := range sstxMsgTxns {
		var amtIn int64
		for iv := range msgTx.TxIn {
			amtIn += msgTx.TxIn[iv].ValueIn
		}
		var amtOut int64
		for iv := range msgTx.TxOut {
			amtOut += msgTx.TxOut[iv].Value
		}
		fee := dcrutil.Amount(amtIn - amtOut).ToCoin()
		if fee < minFee {
			minFee = fee
		}
		if fee > maxFee {
			maxFee = fee
		}
		meanFee += fee
		fees[it] = fee
	}

	if feeInfo.Number > 0 {
		N := float64(feeInfo.Number)
		meanFee /= N
		feeInfo.Mean = meanFee
		feeInfo.Median = MedianCoin(fees)
		feeInfo.Min = minFee
		feeInfo.Max = maxFee

		if N > 1 {
			var variance float64
			for _, f := range fees {
				variance += (f - meanFee) * (f - meanFee)
			}
			variance /= (N - 1)
			feeInfo.StdDev = math.Sqrt(variance)
		}
	}

	return feeInfo
}

// FeeRateInfoBlock computes ticket fee rate statistics for the tickets included
// in the specified block.
func FeeRateInfoBlock(block *dcrutil.Block) *dcrjson.FeeInfoBlock {
	feeInfo := new(dcrjson.FeeInfoBlock)
	_, sstxMsgTxns := TicketsInBlock(block)

	feeInfo.Height = uint32(block.Height())
	feeInfo.Number = uint32(len(sstxMsgTxns))

	var minFee, maxFee, meanFee float64
	minFee = math.MaxFloat64
	feesRates := make([]float64, feeInfo.Number)
	for it, msgTx := range sstxMsgTxns {
		var amtIn, amtOut int64
		for iv := range msgTx.TxIn {
			amtIn += msgTx.TxIn[iv].ValueIn
		}
		for iv := range msgTx.TxOut {
			amtOut += msgTx.TxOut[iv].Value
		}
		fee := dcrutil.Amount(1000*(amtIn-amtOut)).ToCoin() / float64(msgTx.SerializeSize())
		if fee < minFee {
			minFee = fee
		}
		if fee > maxFee {
			maxFee = fee
		}
		meanFee += fee
		feesRates[it] = fee
	}

	if feeInfo.Number > 0 {
		N := float64(feeInfo.Number)
		feeInfo.Mean = meanFee / N
		feeInfo.Median = MedianCoin(feesRates)
		feeInfo.Min = minFee
		feeInfo.Max = maxFee

		if feeInfo.Number > 1 {
			var variance float64
			for _, f := range feesRates {
				fDev := f - feeInfo.Mean
				variance += fDev * fDev
			}
			variance /= (N - 1)
			feeInfo.StdDev = math.Sqrt(variance)
		}
	}

	return feeInfo
}

// MsgTxFromHex returns a wire.MsgTx struct built from the transaction hex string.
func MsgTxFromHex(txhex string) (*wire.MsgTx, error) {
	msgTx := wire.NewMsgTx()
	if err := msgTx.Deserialize(hex.NewDecoder(strings.NewReader(txhex))); err != nil {
		return nil, err
	}
	return msgTx, nil
}

// DetermineTxTypeString returns a string representing the transaction type given
// a wire.MsgTx struct
func DetermineTxTypeString(msgTx *wire.MsgTx) string {
	switch stake.DetermineTxType(msgTx) {
	case stake.TxTypeSSGen:
		return "Vote"
	case stake.TxTypeSStx:
		return "Ticket"
	case stake.TxTypeSSRtx:
		return "Revocation"
	default:
		return "Regular"
	}
}

// TxTypeToString returns a string representation of the provided transaction
// type, which corresponds to the txn types defined for stake.TxType type.
func TxTypeToString(txType int) string {
	switch stake.TxType(txType) {
	case stake.TxTypeSSGen:
		return "Vote"
	case stake.TxTypeSStx:
		return "Ticket"
	case stake.TxTypeSSRtx:
		return "Revocation"
	default:
		return "Regular"
	}
}

// TxIsTicket indicates if the transaction type is a ticket (sstx).
func TxIsTicket(txType int) bool {
	return stake.TxType(txType) == stake.TxTypeSStx
}

// TxIsVote indicates if the transaction type is a vote (ssgen).
func TxIsVote(txType int) bool {
	return stake.TxType(txType) == stake.TxTypeSSGen
}

// TxIsRevoke indicates if the transaction type is a revocation (ssrtx).
func TxIsRevoke(txType int) bool {
	return stake.TxType(txType) == stake.TxTypeSSRtx
}

// TxIsRegular indicates if the transaction type is a regular (non-stake)
// transaction.
func TxIsRegular(txType int) bool {
	return stake.TxType(txType) == stake.TxTypeRegular
}

// IsStakeTx indicates if the input MsgTx is a stake transaction.
func IsStakeTx(msgTx *wire.MsgTx) bool {
	switch stake.DetermineTxType(msgTx) {
	case stake.TxTypeSSGen:
		fallthrough
	case stake.TxTypeSStx:
		fallthrough
	case stake.TxTypeSSRtx:
		return true
	default:
		return false
	}
}

// TxTree returns for a wire.MsgTx either wire.TxTreeStake or wire.TxTreeRegular
// depending on the type of transaction.
func TxTree(msgTx *wire.MsgTx) int8 {
	if IsStakeTx(msgTx) {
		return wire.TxTreeStake
	}
	return wire.TxTreeRegular
}

// TxFee computes and returns the fee for a given tx
func TxFee(msgTx *wire.MsgTx) dcrutil.Amount {
	var amtIn int64
	for iv := range msgTx.TxIn {
		amtIn += msgTx.TxIn[iv].ValueIn
	}
	var amtOut int64
	for iv := range msgTx.TxOut {
		amtOut += msgTx.TxOut[iv].Value
	}
	return dcrutil.Amount(amtIn - amtOut)
}

// TxFeeRate computes and returns the total fee in atoms and fee rate in
// atoms/kB for a given tx.
func TxFeeRate(msgTx *wire.MsgTx) (dcrutil.Amount, dcrutil.Amount) {
	var amtIn int64
	for iv := range msgTx.TxIn {
		amtIn += msgTx.TxIn[iv].ValueIn
	}
	var amtOut int64
	for iv := range msgTx.TxOut {
		amtOut += msgTx.TxOut[iv].Value
	}
	txSize := int64(msgTx.SerializeSize())
	return dcrutil.Amount(amtIn - amtOut), dcrutil.Amount(FeeRate(amtIn, amtOut, txSize))
}

// FeeRate computes the fee rate in atoms/kB for a transaction provided the
// total amount of the transaction's inputs, the total amount of the
// transaction's outputs, and the size of the transaction in bytes. Note that a
// kB refers to 1000 bytes, not a kiB. If the size is 0, the returned fee is -1.
func FeeRate(amtIn, amtOut, sizeBytes int64) int64 {
	if sizeBytes == 0 {
		return -1
	}
	return 1000 * (amtIn - amtOut) / sizeBytes
}

// TotalOutFromMsgTx computes the total value out of a MsgTx
func TotalOutFromMsgTx(msgTx *wire.MsgTx) dcrutil.Amount {
	var amtOut int64
	for _, v := range msgTx.TxOut {
		amtOut += v.Value
	}
	return dcrutil.Amount(amtOut)
}

// TotalVout computes the total value of a slice of dcrjson.Vout
func TotalVout(vouts []dcrjson.Vout) dcrutil.Amount {
	var total dcrutil.Amount
	for _, v := range vouts {
		a, err := dcrutil.NewAmount(v.Value)
		if err != nil {
			continue
		}
		total += a
	}
	return total
}

// GenesisTxHash returns the hash of the single coinbase transaction in the
// genesis block of the specified network. This transaction is hard coded, and
// the pubkey script for its one output only decodes for simnet.
func GenesisTxHash(params *chaincfg.Params) chainhash.Hash {
	return params.GenesisBlock.Transactions[0].TxHash()
}

// IsZeroHashP2PHKAddress checks if the given address is the dummy (zero pubkey
// hash) address. See https://github.com/decred/dcrdata/issues/358 for details.
func IsZeroHashP2PHKAddress(checkAddressString string, params *chaincfg.Params) bool {
	zeroed := [20]byte{}
	// expecting DsQxuVRvS4eaJ42dhQEsCXauMWjvopWgrVg address for mainnet
	address, err := dcrutil.NewAddressPubKeyHash(zeroed[:], params, 0)
	if err != nil {
		return false
	}
	zeroAddress := address.String()
	return checkAddressString == zeroAddress
}

// IsZeroHash checks if the Hash is the zero hash.
func IsZeroHash(hash chainhash.Hash) bool {
	return hash == zeroHash
}

// IsZeroHashStr checks if the string is the zero hash string.
func IsZeroHashStr(hash string) bool {
	return hash == string(zeroHashStringBytes)
}

// ValidateNetworkAddress checks if the given address is valid on the given
// network.
func ValidateNetworkAddress(address dcrutil.Address, p *chaincfg.Params) bool {
	return address.IsForNet(p)
}

// AddressError is the type of error returned by AddressValidation.
type AddressError error

var (
	AddressErrorNoError      AddressError
	AddressErrorZeroAddress  AddressError = errors.New("null address")
	AddressErrorWrongNet     AddressError = errors.New("wrong network")
	AddressErrorDecodeFailed AddressError = errors.New("decoding failed")
	AddressErrorUnknown      AddressError = errors.New("unknown error")
	AddressErrorUnsupported  AddressError = errors.New("recognized, but unsuported address type")
)

type AddressType int

const (
	AddressTypeP2PK = iota
	AddressTypeP2PKH
	AddressTypeP2SH
	AddressTypeOther
	AddressTypeUnknown
)

// AddressValidation performs several validation checks on the given address
// string. Initially, decoding as a Decred address is attempted. If it fails to
// decode, AddressErrorDecodeFailed is returned with AddressTypeUnknown.
// If the address decoded successfully as a Decred address, it is checked
// against the specified network. If it is the wrong network,
// AddressErrorWrongNet is returned with AddressTypeUnknown. If the address is
// the correct network, the address type is obtained. A final check is performed
// to determine if the address is the zero pubkey hash address, in which case
// AddressErrorZeroAddress is returned with the determined address type. If it
// is another address, AddressErrorNoError is returned with the determined
// address type.
func AddressValidation(address string, params *chaincfg.Params) (dcrutil.Address, AddressType, AddressError) {
	// Decode and validate the address.
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		return nil, AddressTypeUnknown, AddressErrorDecodeFailed
	}

	// Detect when an address belonging to a different Decred network.
	if !ValidateNetworkAddress(addr, params) {
		return addr, AddressTypeUnknown, AddressErrorWrongNet
	}

	// Determine address type for this valid Decred address. Ignore the error
	// since DecodeAddress succeeded.
	_, netID, _ := base58.CheckDecode(address)

	var addrType AddressType
	switch netID {
	case params.PubKeyAddrID:
		addrType = AddressTypeP2PK
	case params.PubKeyHashAddrID:
		addrType = AddressTypeP2PKH
	case params.ScriptHashAddrID:
		addrType = AddressTypeP2SH
	case params.PKHEdwardsAddrID, params.PKHSchnorrAddrID:
		addrType = AddressTypeOther
	default:
		addrType = AddressTypeUnknown
	}

	// Check if the address is the zero pubkey hash address commonly used for
	// zero value sstxchange-tagged outputs. Return a special error value, but
	// the decoded address and address type are valid.
	if IsZeroHashP2PHKAddress(address, params) {
		return addr, addrType, AddressErrorZeroAddress
	}

	return addr, addrType, AddressErrorNoError
}
