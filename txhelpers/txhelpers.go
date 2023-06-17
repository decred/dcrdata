// Copyright (c) 2018-2022, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

// Package txhelpers contains helper functions for working with transactions and
// blocks (e.g. checking for a transaction in a block).
package txhelpers

import (
	"bytes"
	"context"
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
	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

var (
	zeroHash            = chainhash.Hash{}
	zeroHashStringBytes = []byte(chainhash.Hash{}.String())
)

var CoinbaseFlags = "/dcrd/"
var CoinbaseScript = append([]byte{0x00, 0x00}, []byte(CoinbaseFlags)...)

const (
	treasuryHeightMainnet  = 552448 // lockedin at 544384
	treasuryHeightTestnet3 = 560208
)

// IsTreasuryActive indicates if the decentralized treasury is active for the
// given network and block height.
func IsTreasuryActive(net wire.CurrencyNet, height int64) bool {
	switch net {
	case wire.MainNet:
		return height >= treasuryHeightMainnet
	case wire.TestNet3:
		return height >= treasuryHeightTestnet3
	case wire.SimNet:
		return height >= 2
	default:
		fmt.Printf("unrecognized network %v\n", net)
		return false
	}
}

// SubsidySplitStakeVer locates the "changesubsidysplit" agenda item in the
// consensus deployments defined in the provided chain parameters. If found, the
// corresponding stake version is returned.
func SubsidySplitStakeVer(params *chaincfg.Params) (uint32, bool) {
	for stakeVer, deployments := range params.Deployments {
		for i := range deployments {
			if deployments[i].Vote.Id == chaincfg.VoteIDChangeSubsidySplit {
				return stakeVer, true
			}
		}
	}
	return 0, false
}

// Blake3PowStakeVer locates the "blake3pow" agenda item in the consensus
// deployments defined in the provided chain parameters. If found, the
// corresponding stake version is returned.
func Blake3PowStakeVer(params *chaincfg.Params) (uint32, bool) {
	for stakeVer, deployments := range params.Deployments {
		for i := range deployments {
			if deployments[i].Vote.Id == chaincfg.VoteIDBlake3Pow {
				return stakeVer, true
			}
		}
	}
	return 0, false
}

// SubsidySplitR2StakeVer locates the "changesubsidysplitr2" agenda item in the
// consensus deployments defined in the provided chain parameters. If found, the
// corresponding stake version is returned.
func SubsidySplitR2StakeVer(params *chaincfg.Params) (uint32, bool) {
	for stakeVer, deployments := range params.Deployments {
		for i := range deployments {
			if deployments[i].Vote.Id == chaincfg.VoteIDChangeSubsidySplitR2 {
				return stakeVer, true
			}
		}
	}
	return 0, false
}

// DCP0010ActivationHeight indicates the height at which the
// "changesubsidysplit" consensus change activates for the provided
// getblockchaininfo result.
func DCP0010ActivationHeight(params *chaincfg.Params, bci *chainjson.GetBlockChainInfoResult) int64 {
	splitAgendaInfo, found := bci.Deployments[chaincfg.VoteIDChangeSubsidySplit]
	if !found {
		return 0
	}
	switch splitAgendaInfo.Status {
	case "active":
		return splitAgendaInfo.Since
	case "lockedin":
		return splitAgendaInfo.Since + int64(params.RuleChangeActivationInterval)
	default:
		return -1 // not active
	}
}

func IsCoinBaseTx(tx *wire.MsgTx) bool {
	// First see if this is a coinbase transaction in a world where the treasury
	// agenda is not active. This has to be checked first because if treasury is
	// active, regular coinbase transactions are created with version
	// wire.TxVersionTreasury. This will catch pre-treasury coinbase
	// transactions that would not be recognized as coinbase if treasury was
	// assumed to be active.
	if standalone.IsCoinBaseTx(tx, false) {
		return true
	}
	// Now see if it looks like a coinbase transaction with the new transaction
	// versions with decentralized treasury active, and where it could look like
	// a treasury spend.
	return standalone.IsCoinBaseTx(tx, true)
}

// ReorgData contains details of a chain reorganization, including the full old
// and new chains, and the common ancestor that should not be included in either
// chain. Below is the description of the reorg data with the letter indicating
// the various blocks in the chain:
//
//		A  -> B  -> C
//	  		\  -> B' -> C' -> D'
//
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
	GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*dcrutil.Tx, error)
}

// TxReceiver is satisfied by the return type from GetRawTransactionAsync
// (rpcclient.FutureGetRawTransactionResult).
type TxReceiver interface {
	Receive() (*dcrutil.Tx, error)
}

// TransactionPromiseGetter is satisfied by rpcclient.Client, and required by
// functions that would otherwise require a rpcclient.Client just for
// GetRawTransactionVerboseAsync. See VerboseTransactionPromiseGetter and
// rpcutils.NewAsyncTxClient for more on wrapping an rpcclient.Client.
type TransactionPromiseGetter interface {
	GetRawTransactionPromise(ctx context.Context, txHash *chainhash.Hash) TxReceiver
}

// VerboseTransactionGetter is an interface satisfied by rpcclient.Client, and
// required by functions that would otherwise require a rpcclient.Client just
// for GetRawTransactionVerbose.
type VerboseTransactionGetter interface {
	GetRawTransactionVerbose(ctx context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error)
}

// VerboseTxReceiver is satisfied by the return type from
// GetRawTransactionVerboseAsync
// (rpcclient.FutureGetRawTransactionVerboseResult).
type VerboseTxReceiver interface {
	Receive() (*chainjson.TxRawResult, error)
}

// VerboseTransactionPromiseGetter is required by functions that would otherwise
// require a rpcclient.Client just for GetRawTransactionVerboseAsync. You may
// construct a simple wrapper for an rpcclient.Client with
// rpcutils.NewAsyncTxClient like:
//
//	type mempoolClient struct {
//	    *rpcclient.Client
//	}
//	func (cl *mempoolClient) GetRawTransactionVerbosePromise(ctx context.Context, txHash *chainhash.Hash) txhelpers.VerboseTxReceiver {
//	    return cl.Client.GetRawTransactionVerboseAsync(ctx, txHash)
//	}
//
//	var _ VerboseTransactionPromiseGetter = (*mempoolClient)(nil)
type VerboseTransactionPromiseGetter interface {
	GetRawTransactionVerbosePromise(ctx context.Context, txHash *chainhash.Hash) VerboseTxReceiver
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
//	hashList = FilterHashSlice(hashList, func(h chainhash.Hash) bool {
//	 return HashInSlice(h, blackList)
//	})
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
func TxInvolvesAddress(msgTx *wire.MsgTx, addr string, c VerboseTransactionPromiseGetter,
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
	hash := msgTx.CachedTxHash()
	addrs = make(map[string]bool)
	for outIndex, txOut := range msgTx.TxOut {
		_, txOutAddrs := stdscript.ExtractAddrs(txOut.Version, txOut.PkScript, params)
		if len(txOutAddrs) == 0 {
			continue
		}
		newOuts++

		// Check if we are watching any address for this TxOut.
		for _, txAddr := range txOutAddrs {
			addr := txAddr.String()

			op := wire.NewOutPoint(hash, uint32(outIndex), txTree)

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
// provided MempoolAddressStore must be initialized. A
// VerboseTransactionPromiseGetter is required to retrieve the pkScripts for the
// previous outpoints. The number of consumed previous outpoints that paid
// addresses in the provided transaction are counted and returned. The addresses
// in the previous outpoints are listed in the output addrs map, where the value
// of the stored bool indicates the address is new to the MempoolAddressStore.
func TxPrevOutsByAddr(txAddrOuts MempoolAddressStore, txnsStore TxnsStore, msgTx *wire.MsgTx, c VerboseTransactionPromiseGetter,
	params *chaincfg.Params) (newPrevOuts int, addrs map[string]bool, valsIn []int64) {
	if txAddrOuts == nil {
		panic("TxPrevOutAddresses: input map must be initialized: map[string]*AddressOutpoints")
	}
	if txnsStore == nil {
		panic("TxPrevOutAddresses: input map must be initialized: map[string]*AddressOutpoints")
	}

	// Send all the raw transaction requests
	type promiseGetRawTransaction struct {
		result VerboseTxReceiver
		inIdx  int
	}
	promisesGetRawTransaction := make([]promiseGetRawTransaction, 0, len(msgTx.TxIn))

	for inIdx, txIn := range msgTx.TxIn {
		hash := &txIn.PreviousOutPoint.Hash
		if zeroHash.IsEqual(hash) {
			continue // coinbase or stakebase
		}
		promisesGetRawTransaction = append(promisesGetRawTransaction, promiseGetRawTransaction{
			result: c.GetRawTransactionVerbosePromise(context.TODO(), hash),
			inIdx:  inIdx,
		})
	}

	addrs = make(map[string]bool)
	valsIn = make([]int64, len(msgTx.TxIn))

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

		// Get the values.
		valsIn[inIdx] = txOut.Value

		// Extract the addresses from this output's PkScript.
		_, txAddrs := stdscript.ExtractAddrs(txOut.Version, txOut.PkScript, params)
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

		tree := TxTree(prevTx)
		outpoint := wire.NewOutPoint(&hash, prevOut.Index, tree)
		prevOutExtended := PrevOut{
			TxSpending:       *msgTx.CachedTxHash(),
			InputIndex:       inIdx,
			PreviousOutpoint: outpoint,
		}

		// For each address paid to by this previous outpoint, record the
		// previous outpoint and the containing transactions.
		for _, txAddr := range txAddrs {
			addr := txAddr.String()

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
func TxConsumesOutpointWithAddress(msgTx *wire.MsgTx, addr string, c VerboseTransactionPromiseGetter,
	params *chaincfg.Params) (prevOuts []PrevOut, prevTxs []*TxWithBlockData) {
	// Send all the raw transaction requests.
	type promiseGetRawTransaction struct {
		result VerboseTxReceiver
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
			result: c.GetRawTransactionVerbosePromise(context.TODO(), hash),
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
		_, txAddrs := stdscript.ExtractAddrs(txOut.Version, txOut.PkScript, params)

		// For each address that matches the address of interest, record this
		// previous outpoint and the containing transactions.
		for _, txAddr := range txAddrs {
			addrstr := txAddr.String()
			if addr == addrstr {
				tree := TxTree(prevTx)
				outpoint := wire.NewOutPoint(&hash, prevOut.Index, tree)
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
				prevTx, err := c.GetRawTransaction(context.TODO(), &prevOut.Hash)
				if err != nil {
					fmt.Printf("Unable to get raw transaction for %s\n", prevOut.Hash.String())
					continue
				}

				// prevOut.Index should tell us which one, but check all anyway
				for _, txOut := range prevTx.MsgTx().TxOut {
					_, txAddrs := stdscript.ExtractAddrs(txOut.Version, txOut.PkScript, params)
					for _, txAddr := range txAddrs {
						addrstr := txAddr.String()
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
func TxPaysToAddress(msgTx *wire.MsgTx, addr string, params *chaincfg.Params) (outpoints []*wire.OutPoint) {
	// Check the addresses associated with the PkScript of each TxOut
	txTree := TxTree(msgTx)
	hash := msgTx.TxHash()
	for outIndex, txOut := range msgTx.TxOut {
		// Check if we are watching any address for this TxOut
		_, txOutAddrs := stdscript.ExtractAddrs(txOut.Version, txOut.PkScript, params)
		for _, txAddr := range txOutAddrs {
			addrstr := txAddr.String()
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
				// Check if we are watching any address for this TxOut.
				_, txOutAddrs := stdscript.ExtractAddrs(txOut.Version, txOut.PkScript, params)
				for _, txAddr := range txOutAddrs {
					addrstr := txAddr.String()
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
	prevTx, err := c.GetRawTransaction(context.TODO(), &outPoint.Hash)
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
	_, txAddrs := stdscript.ExtractAddrs(txOut.Version, txOut.PkScript, params)
	value := dcrutil.Amount(txOut.Value)
	addresses := make([]string, 0, len(txAddrs))
	for _, txAddr := range txAddrs {
		addr := txAddr.String()
		addresses = append(addresses, addr)
	}
	return addresses, value, nil
}

// OutPointAddressesFromString is the same as OutPointAddresses, but it takes
// the outpoint as the tx string, vout index, and tree.
func OutPointAddressesFromString(txid string, index uint32, tree int8,
	c RawTransactionGetter, params *chaincfg.Params) ([]string, dcrutil.Amount, error) {
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, 0, fmt.Errorf("Invalid hash %s", txid)
	}

	outPoint := wire.NewOutPoint(hash, index, tree)
	return OutPointAddresses(outPoint, c, params)
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
	max := standalone.CompactToBig(params.PowLimitBits)
	target := standalone.CompactToBig(bits)

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
func SSGenVoteBlockValid(msgTx *wire.MsgTx) (BlockValidation, []*TSpendVote, uint16, error) {
	treasuryVotes, err := stake.CheckSSGenVotes(msgTx)
	if err != nil {
		return BlockValidation{}, nil, 0, fmt.Errorf("not a vote transaction")
	}
	var tspendVotes []*TSpendVote
	if len(treasuryVotes) > 0 {
		tspendVotes = make([]*TSpendVote, len(treasuryVotes))
		for i := range treasuryVotes {
			tspendVotes[i] = &TSpendVote{
				TSpend: treasuryVotes[i].Hash,
				Choice: uint8(treasuryVotes[i].Vote),
			}
		}
	}

	ssGenVoteBits := stake.SSGenVoteBits(msgTx)
	blockHash, blockHeight := stake.SSGenBlockVotedOn(msgTx)
	blockValid := BlockValidation{
		Hash:     blockHash,
		Height:   int64(blockHeight),
		Validity: dcrutil.IsFlagSet16(ssGenVoteBits, dcrutil.BlockValid),
	}
	return blockValid, tspendVotes, ssGenVoteBits, nil
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
		return 0, fmt.Errorf("vote consensus version absent")
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

// TSpendVote describes how a SSGen transaction decided on a tspend.
type TSpendVote struct {
	TSpend chainhash.Hash
	Choice uint8
}

// SSGenVoteChoices gets a ssgen's vote choices (block validity and any
// agendas). The vote's stake version, to which the vote choices correspond, and
// vote bits are also returned. Note that []*VoteChoice may be an empty slice if
// there are no consensus deployments for the transaction's vote version. The
// error value may be non-nil if the tx is not a valid ssgen.
func SSGenVoteChoices(tx *wire.MsgTx, params *chaincfg.Params) (BlockValidation, uint32, uint16, []*VoteChoice, []*TSpendVote, error) {
	validBlock, tspendVotes, voteBits, err := SSGenVoteBlockValid(tx)
	if err != nil {
		return validBlock, 0, 0, nil, nil, err
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
		if choiceIndex < 0 || choiceIndex >= len(voteAgenda.Choices) {
			continue // should not happen with a vote from dcrd, but we don't want to panic below
		}
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

	return validBlock, voteVersion, voteBits, choices, tspendVotes, nil
}

// FeeInfoBlock computes ticket fee statistics for the tickets included in the
// specified block.
func FeeInfoBlock(block *dcrutil.Block) *chainjson.FeeInfoBlock {
	feeInfo := new(chainjson.FeeInfoBlock)
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
func FeeRateInfoBlock(block *dcrutil.Block) *chainjson.FeeInfoBlock {
	feeInfo := new(chainjson.FeeInfoBlock)
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

// MsgTxToHex returns a transaction hex string from a wire.MsgTx struct.
func MsgTxToHex(msgTx *wire.MsgTx) (string, error) {
	var hexBuilder strings.Builder
	if err := msgTx.Serialize(hex.NewEncoder(&hexBuilder)); err != nil {
		return "", err
	}
	return hexBuilder.String(), nil
}

// Transaction type constants.
const (
	TxTypeVote          string = "Vote"
	TxTypeTicket        string = "Ticket"
	TxTypeRevocation    string = "Revocation"
	TxTypeRegular       string = "Regular"
	TxTypeTreasurybase  string = "Treasurybase"
	TxTypeTreasurySpend string = "Treasury Spend"
	TxTypeTreasuryAdd   string = "Treasury Add"
)

// DetermineTxTypeString returns a string representing the transaction type
// given a wire.MsgTx struct. If the caller does not know if treasure is active
// for this txn, but the MsgTx is from from dcrd, it can be assumed to be valid
// according to consensus at height of txn and thus true can be safely used,
// although this may be more inefficient than necessary.
func DetermineTxTypeString(msgTx *wire.MsgTx) string {
	txType := DetermineTxType(msgTx)
	return TxTypeToString(int(txType))
}

func DetermineTxType(msgTx *wire.MsgTx) stake.TxType {
	return stake.DetermineTxType(msgTx)
}

func IsSSRtx(tx *wire.MsgTx) bool {
	return stake.IsSSRtx(tx)
}

// TxTypeToString returns a string representation of the provided transaction
// type, which corresponds to the txn types defined for stake.TxType type.
func TxTypeToString(txType int) string {
	switch stake.TxType(txType) {
	case stake.TxTypeSSGen:
		return TxTypeVote
	case stake.TxTypeSStx:
		return TxTypeTicket
	case stake.TxTypeSSRtx:
		return TxTypeRevocation
	case stake.TxTypeTAdd:
		return TxTypeTreasuryAdd
	case stake.TxTypeTSpend:
		return TxTypeTreasurySpend
	case stake.TxTypeTreasuryBase:
		return TxTypeTreasurybase
	default:
		return TxTypeRegular
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

// TxIsTAdd indicates if the transaction type is a treasury add (tadd).
func TxIsTAdd(txType int) bool {
	return stake.TxType(txType) == stake.TxTypeTAdd
}

// TxIsTSpend indicates if the transaction type is a treasury spend (tspend).
func TxIsTSpend(txType int) bool {
	return stake.TxType(txType) == stake.TxTypeTSpend
}

// TxIsTreasuryBase indicates if the transaction type is a treasurybase.
func TxIsTreasuryBase(txType int) bool {
	return stake.TxType(txType) == stake.TxTypeTreasuryBase
}

// TxIsRegular indicates if the transaction type is a regular (non-stake)
// transaction.
func TxIsRegular(txType int) bool {
	return stake.TxType(txType) == stake.TxTypeRegular
}

// IsStakeTx indicates if the input MsgTx is a stake transaction.
func IsStakeTx(msgTx *wire.MsgTx) bool {
	return DetermineTxType(msgTx) != stake.TxTypeRegular
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

// TotalVout computes the total value of a slice of chainjson.Vout
func TotalVout(vouts []chainjson.Vout) dcrutil.Amount {
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
	address, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(zeroed[:], params)
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

// AddressError is the type of error returned by AddressValidation.
type AddressError error

var (
	AddressErrorNoError      AddressError
	AddressErrorZeroAddress  AddressError = errors.New("null address")
	AddressErrorWrongNet     AddressError = errors.New("wrong network")
	AddressErrorDecodeFailed AddressError = errors.New("decoding failed")
	AddressErrorUnknown      AddressError = errors.New("unknown error")
	AddressErrorUnsupported  AddressError = errors.New("recognized, but unsupported address type")
)

// AddressType is used to label type of an address as returned by
// base58.CheckDecode.
type AddressType int

// These are the AddressType values, as returned by AddressValidation.
const (
	AddressTypeP2PK = iota
	AddressTypeP2PKH
	AddressTypeP2SH
	AddressTypeOther // the "alt" pkh addresses with ed25519 and schorr sigs
	AddressTypeUnknown
)

// String describes the AddressType.
func (at AddressType) String() string {
	switch at {
	case AddressTypeP2PK: // includes all sig types
		return "pubkey"
	case AddressTypeP2PKH:
		return "pubkeyhash"
	case AddressTypeOther: // schnorr or edwards pkh, but still pkh
		return "pubkeyhashalt"
	case AddressTypeP2SH:
		return "scripthash"
	case AddressTypeUnknown:
		fallthrough
	default:
		return "unknown"
	}
}

// AddressValidation performs several validation checks on the given address
// string. Initially, decoding as a Decred address is attempted. If it fails to
// decode, AddressErrorDecodeFailed is returned with AddressTypeUnknown. If the
// address decoded successfully as a Decred address, it is checked against the
// specified network. A final check is performed to determine if the address is
// the zero pubkey hash address, in which case AddressErrorZeroAddress is
// returned with the determined address type. If it is another address,
// AddressErrorNoError (nil) is returned with the determined address type.
func AddressValidation(address string, params *chaincfg.Params) (stdaddr.Address, AddressType, AddressError) {
	// Decode and validate the address.
	addr, err := stdaddr.DecodeAddress(address, params)
	if err != nil {
		// if errors.Is(err, dcrutil.ErrUnknownAddressType) {
		// 	return nil, AddressTypeUnknown, AddressErrorWrongNet // possible? ErrUnknownAddressType means many things
		// }
		return nil, AddressTypeUnknown, AddressErrorDecodeFailed
	}

	// var addrType AddressType
	// switch addr.(type) {
	// case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
	// 	addrType = AddressTypeP2PKH
	// case *stdaddr.AddressScriptHashV0:
	// 	addrType = AddressTypeP2SH
	// case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
	// 	addrType = AddressTypeP2PK
	// case *stdaddr.AddressPubKeyEd25519V0, *stdaddr.AddressPubKeySchnorrSecp256k1V0,
	// 	*stdaddr.AddressPubKeyHashEd25519V0, *stdaddr.AddressPubKeyHashSchnorrSecp256k1V0:
	// 	addrType = AddressTypeOther
	// default:
	// 	addrType = AddressTypeUnknown
	// }

	// Determine address type for this valid Decred address.
	_, netID, _ := base58.CheckDecode(address)
	// Or skip the checksum:
	// var netID [2]byte
	// if dec := base58.Decode(address); len(dec) > 1 { // must be since DecodeAddress wored
	// 	netID[0], netID[1] = dec[0], dec[1]
	// }

	var addrType AddressType
	switch netID {
	case params.PubKeyAddrID:
		addrType = AddressTypeP2PK // all sig types including ed25519 and schnorr
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
