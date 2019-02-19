// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package rpcutils

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient/v2"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/semver"
	"github.com/decred/dcrdata/txhelpers"
)

// Any of the following dcrd RPC API versions are deemed compatible with
// dcrdata.
var compatibleChainServerAPIs = []semver.Semver{
	semver.NewSemver(5, 0, 0), // order of reorg and block connected notifications changed
}

var (
	zeroHash            = chainhash.Hash{}
	zeroHashStringBytes = []byte(chainhash.Hash{}.String())

	maxAncestorChainLength = 8192

	ErrAncestorAtGenesis      = errors.New("no ancestor: at genesis")
	ErrAncestorMaxChainLength = errors.New("no ancestor: max chain length reached")
)

// ConnectNodeRPC attempts to create a new websocket connection to a dcrd node,
// with the given credentials and optional notification handlers.
func ConnectNodeRPC(host, user, pass, cert string, disableTLS, disableReconnect bool,
	ntfnHandlers ...*rpcclient.NotificationHandlers) (*rpcclient.Client, semver.Semver, error) {
	var dcrdCerts []byte
	var err error
	var nodeVer semver.Semver
	if !disableTLS {
		dcrdCerts, err = ioutil.ReadFile(cert)
		if err != nil {
			log.Errorf("Failed to read dcrd cert file at %s: %s\n",
				cert, err.Error())
			return nil, nodeVer, err
		}
		log.Debugf("Attempting to connect to dcrd RPC %s as user %s "+
			"using certificate located in %s",
			host, user, cert)
	} else {
		log.Debugf("Attempting to connect to dcrd RPC %s as user %s (no TLS)",
			host, user)
	}

	connCfgDaemon := &rpcclient.ConnConfig{
		Host:                 host,
		Endpoint:             "ws", // websocket
		User:                 user,
		Pass:                 pass,
		Certificates:         dcrdCerts,
		DisableTLS:           disableTLS,
		DisableAutoReconnect: disableReconnect,
	}

	var ntfnHdlrs *rpcclient.NotificationHandlers
	if len(ntfnHandlers) > 0 {
		if len(ntfnHandlers) > 1 {
			return nil, nodeVer, fmt.Errorf("invalid notification handler argument")
		}
		ntfnHdlrs = ntfnHandlers[0]
	}
	dcrdClient, err := rpcclient.New(connCfgDaemon, ntfnHdlrs)
	if err != nil {
		return nil, nodeVer, fmt.Errorf("Failed to start dcrd RPC client: %s", err.Error())
	}

	// Ensure the RPC server has a compatible API version.
	ver, err := dcrdClient.Version()
	if err != nil {
		log.Error("Unable to get RPC version: ", err)
		return nil, nodeVer, fmt.Errorf("unable to get node RPC version")
	}

	dcrdVer := ver["dcrdjsonrpcapi"]
	nodeVer = semver.NewSemver(dcrdVer.Major, dcrdVer.Minor, dcrdVer.Patch)

	// Check if the dcrd RPC API version is compatible with dcrdata.
	isApiCompat := semver.AnyCompatible(compatibleChainServerAPIs, nodeVer)
	if !isApiCompat {
		return nil, nodeVer, fmt.Errorf("Node JSON-RPC server does not have "+
			"a compatible API version. Advertises %v but requires one of: %v",
			nodeVer, compatibleChainServerAPIs)
	}

	return dcrdClient, nodeVer, nil
}

// BuildBlockHeaderVerbose creates a *dcrjson.GetBlockHeaderVerboseResult from
// an input *wire.BlockHeader and current best block height, which is used to
// compute confirmations.  The next block hash may optionally be provided.
func BuildBlockHeaderVerbose(header *wire.BlockHeader, params *chaincfg.Params,
	currentHeight int64, nextHash ...string) *dcrjson.GetBlockHeaderVerboseResult {
	if header == nil {
		return nil
	}

	diffRatio := txhelpers.GetDifficultyRatio(header.Bits, params)

	var next string
	if len(nextHash) > 0 {
		next = nextHash[0]
	}

	blockHeaderResult := dcrjson.GetBlockHeaderVerboseResult{
		Hash:          header.BlockHash().String(),
		Confirmations: currentHeight - int64(header.Height),
		Version:       header.Version,
		PreviousHash:  header.PrevBlock.String(),
		MerkleRoot:    header.MerkleRoot.String(),
		StakeRoot:     header.StakeRoot.String(),
		VoteBits:      header.VoteBits,
		FinalState:    hex.EncodeToString(header.FinalState[:]),
		Voters:        header.Voters,
		FreshStake:    header.FreshStake,
		Revocations:   header.Revocations,
		PoolSize:      header.PoolSize,
		Bits:          strconv.FormatInt(int64(header.Bits), 16),
		SBits:         dcrutil.Amount(header.SBits).ToCoin(),
		Height:        header.Height,
		Size:          header.Size,
		Time:          header.Timestamp.Unix(),
		Nonce:         header.Nonce,
		Difficulty:    diffRatio,
		// Cannot get ChainWork from the wire.BlockHeader
		NextHash: next,
	}

	return &blockHeaderResult
}

// GetBlockHeaderVerbose creates a *dcrjson.GetBlockHeaderVerboseResult for the
// block at height idx via an RPC connection to a chain server.
func GetBlockHeaderVerbose(client BlockFetcher, idx int64) *dcrjson.GetBlockHeaderVerboseResult {
	blockhash, err := client.GetBlockHash(idx)
	if err != nil {
		log.Errorf("GetBlockHash(%d) failed: %v", idx, err)
		return nil
	}

	blockHeaderVerbose, err := client.GetBlockHeaderVerbose(blockhash)
	if err != nil {
		log.Errorf("GetBlockHeaderVerbose(%v) failed: %v", blockhash, err)
		return nil
	}

	return blockHeaderVerbose
}

// GetBlockHeaderVerboseByString creates a *dcrjson.GetBlockHeaderVerboseResult
// for the block specified by hash via an RPC connection to a chain server.
func GetBlockHeaderVerboseByString(client BlockFetcher, hash string) *dcrjson.GetBlockHeaderVerboseResult {
	blockhash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		log.Errorf("Invalid block hash %s: %v", blockhash, err)
		return nil
	}

	blockHeaderVerbose, err := client.GetBlockHeaderVerbose(blockhash)
	if err != nil {
		log.Errorf("GetBlockHeaderVerbose(%v) failed: %v", blockhash, err)
		return nil
	}

	return blockHeaderVerbose
}

// GetBlockVerbose creates a *dcrjson.GetBlockVerboseResult for the block index
// specified by idx via an RPC connection to a chain server.
func GetBlockVerbose(client *rpcclient.Client, idx int64, verboseTx bool) *dcrjson.GetBlockVerboseResult {
	blockhash, err := client.GetBlockHash(idx)
	if err != nil {
		log.Errorf("GetBlockHash(%d) failed: %v", idx, err)
		return nil
	}

	blockVerbose, err := client.GetBlockVerbose(blockhash, verboseTx)
	if err != nil {
		log.Errorf("GetBlockVerbose(%v) failed: %v", blockhash, err)
		return nil
	}

	return blockVerbose
}

// GetBlockVerboseByHash creates a *dcrjson.GetBlockVerboseResult for the
// specified block hash via an RPC connection to a chain server.
func GetBlockVerboseByHash(client *rpcclient.Client, hash string, verboseTx bool) *dcrjson.GetBlockVerboseResult {
	blockhash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		log.Errorf("Invalid block hash %s", hash)
		return nil
	}

	blockVerbose, err := client.GetBlockVerbose(blockhash, verboseTx)
	if err != nil {
		log.Errorf("GetBlockVerbose(%v) failed: %v", blockhash, err)
		return nil
	}

	return blockVerbose
}

// GetStakeDiffEstimates combines the results of EstimateStakeDiff and
// GetStakeDifficulty into a *apitypes.StakeDiff.
func GetStakeDiffEstimates(client *rpcclient.Client) *apitypes.StakeDiff {
	stakeDiff, err := client.GetStakeDifficulty()
	if err != nil {
		return nil
	}
	estStakeDiff, err := client.EstimateStakeDiff(nil)
	if err != nil {
		return nil
	}
	stakeDiffEstimates := apitypes.StakeDiff{
		GetStakeDifficultyResult: dcrjson.GetStakeDifficultyResult{
			CurrentStakeDifficulty: stakeDiff.CurrentStakeDifficulty,
			NextStakeDifficulty:    stakeDiff.NextStakeDifficulty,
		},
		Estimates: *estStakeDiff,
	}
	return &stakeDiffEstimates
}

// GetBlock gets a block at the given height from a chain server.
func GetBlock(ind int64, client BlockFetcher) (*dcrutil.Block, *chainhash.Hash, error) {
	blockhash, err := client.GetBlockHash(ind)
	if err != nil {
		return nil, nil, fmt.Errorf("GetBlockHash(%d) failed: %v", ind, err)
	}

	msgBlock, err := client.GetBlock(blockhash)
	if err != nil {
		return nil, blockhash,
			fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
	}
	block := dcrutil.NewBlock(msgBlock)

	return block, blockhash, nil
}

// GetBlockByHash gets the block with the given hash from a chain server.
func GetBlockByHash(blockhash *chainhash.Hash, client BlockFetcher) (*dcrutil.Block, error) {
	msgBlock, err := client.GetBlock(blockhash)
	if err != nil {
		return nil, fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
	}
	block := dcrutil.NewBlock(msgBlock)

	return block, nil
}

// SideChains gets a slice of known side chain tips. This corresponds to the
// results of the getchaintips node RPC where the block tip "status" is either
// "valid-headers" or "valid-fork".
func SideChains(client *rpcclient.Client) ([]dcrjson.GetChainTipsResult, error) {
	tips, err := client.GetChainTips()
	if err != nil {
		return nil, err
	}

	return sideChainTips(tips), nil
}

func sideChainTips(allTips []dcrjson.GetChainTipsResult) (sideTips []dcrjson.GetChainTipsResult) {
	for i := range allTips {
		switch allTips[i].Status {
		case "valid-headers", "valid-fork":
			sideTips = append(sideTips, allTips[i])
		}
	}
	return
}

// SideChainFull gets all of the blocks in the side chain with the specified tip
// block hash. The first block in the slice is the lowest height block in the
// side chain, and its previous block is the main/side common ancestor, which is
// not included in the slice since it is main chain. The last block in the slice
// is thus the side chain tip.
func SideChainFull(client *rpcclient.Client, tipHash string) ([]string, error) {
	// Do not assume specified tip hash is even side chain.
	var sideChain []string

	hash := tipHash
	for {
		header := GetBlockHeaderVerboseByString(client, hash)
		if header == nil {
			return nil, fmt.Errorf("GetBlockHeaderVerboseByString failed for block %s", hash)
		}

		// Main chain blocks have Confirmations != -1.
		if header.Confirmations != -1 {
			// The passed block is main chain, not a side chain tip.
			if hash == tipHash {
				return nil, fmt.Errorf("tip block is not on a side chain")
			}
			// This previous block is the main/side common ancestor.
			break
		}

		// This was another side chain block.
		sideChain = append(sideChain, hash)

		// On to previous block
		hash = header.PreviousHash
	}

	// Reverse side chain order so that last element is tip.
	reverseStringSlice(sideChain)

	return sideChain, nil
}

func reverseStringSlice(s []string) {
	N := len(s)
	for i := 0; i <= (N/2)-1; i++ {
		j := N - 1 - i
		s[i], s[j] = s[j], s[i]
	}
}

// GetTransactionVerboseByID get a transaction by transaction id
func GetTransactionVerboseByID(client *rpcclient.Client, txhash *chainhash.Hash) (*dcrjson.TxRawResult, error) {
	txraw, err := client.GetRawTransactionVerbose(txhash)
	if err != nil {
		log.Errorf("GetRawTransactionVerbose failed for: %v", txhash)
		return nil, err
	}
	return txraw, nil
}

// SearchRawTransaction fetch transactions the belong to an
// address
func SearchRawTransaction(client *rpcclient.Client, count int, address string) ([]*dcrjson.SearchRawTransactionsResult, error) {
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		log.Infof("Invalid address %s: %v", address, err)
		return nil, err
	}
	//change the 1000 000 number demo for now
	txs, err := client.SearchRawTransactionsVerbose(addr, 0, count,
		true, true, nil)
	if err != nil {
		log.Warnf("SearchRawTransaction failed for address %s: %v", addr, err)
	}
	return txs, nil
}

// CommonAncestor attempts to determine the common ancestor block for two chains
// specified by the hash of the chain tip block. The full chains from the tips
// back to but not including the common ancestor are also returned. The first
// element in the chain slices is the lowest block following the common
// ancestor, while the last element is the chain tip. The common ancestor will
// never by one of the chain tips. Thus, if one of the chain tips is on the
// other chain, that block will be shared between the two chains, and the common
// ancestor will be the previous block. However, the intended use of this
// function is to find a common ancestor for two chains with no common blocks.
func CommonAncestor(client BlockFetcher, hashA, hashB chainhash.Hash) (*chainhash.Hash, []chainhash.Hash, []chainhash.Hash, error) {
	if client == nil {
		return nil, nil, nil, errors.New("nil RPC client")
	}

	var length int
	var chainA, chainB []chainhash.Hash
	for {
		if length >= maxAncestorChainLength {
			return nil, nil, nil, ErrAncestorMaxChainLength
		}

		// Chain A
		blockA, err := client.GetBlock(&hashA)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("Failed to get block %v: %v", hashA, err)
		}
		heightA := blockA.Header.Height

		// Chain B
		blockB, err := client.GetBlock(&hashB)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("Failed to get block %v: %v", hashB, err)
		}
		heightB := blockB.Header.Height

		// Reach the same height on both chains before checking the loop
		// termination condition. At least one previous block for each chain
		// must be used, so that a chain tip block will not be considered a
		// common ancestor and it will instead be added to a chain slice.
		if heightA > heightB {
			chainA = append([]chainhash.Hash{hashA}, chainA...)
			length++
			hashA = blockA.Header.PrevBlock
			continue
		}
		if heightB > heightA {
			chainB = append([]chainhash.Hash{hashB}, chainB...)
			length++
			hashB = blockB.Header.PrevBlock
			continue
		}

		// Assert heightB == heightA
		if heightB != heightA {
			panic("you broke the code")
		}

		chainA = append([]chainhash.Hash{hashA}, chainA...)
		chainB = append([]chainhash.Hash{hashB}, chainB...)
		length++

		// We are at genesis if the previous block is the zero hash.
		if blockA.Header.PrevBlock == zeroHash {
			return nil, chainA, chainB, ErrAncestorAtGenesis // no common ancestor, but the same block
		}

		hashA = blockA.Header.PrevBlock
		hashB = blockB.Header.PrevBlock

		// break here rather than for condition so inputs with equal hashes get
		// handled properly (with ancestor as previous block and chains
		// including the input blocks.)
		if hashA == hashB {
			break // hashA(==hashB) is the common ancestor.
		}
	}

	// hashA == hashB
	return &hashA, chainA, chainB, nil
}

type BlockHashGetter interface {
	GetBlockHash(int64) (*chainhash.Hash, error)
}

// OrphanedTipLength finds a common ancestor by iterating block heights
// backwards until a common block hash is found. Unlike CommonAncestor, an
// orphaned DB tip whose corresponding block is not known to dcrd will not cause
// an error. The number of blocks that have been orphaned is returned.
// Realistically, this should rarely be anything but 0 or 1, but no limits are
// placed here on the number of blocks checked.
func OrphanedTipLength(ctx context.Context, client BlockHashGetter,
	tipHeight int64, hashFunc func(int64) (string, error)) (int64, error) {
	commonHeight := tipHeight
	var dbHash string
	var err error
	var dcrdHash *chainhash.Hash
	for {
		// Since there are no limits on the number of blocks scanned, allow
		// cancellation for a clean exit.
		select {
		case <-ctx.Done():
			return 0, nil
		default:
		}

		dbHash, err = hashFunc(commonHeight)
		if err != nil {
			return -1, fmt.Errorf("Unable to retrieve block at height %d: %v", commonHeight, err)
		}
		dcrdHash, err = client.GetBlockHash(commonHeight)
		if err != nil {
			return -1, fmt.Errorf("Unable to retrive dcrd block at height %d: %v", commonHeight, err)
		}
		if dcrdHash.String() == dbHash {
			break
		}

		commonHeight--
		if commonHeight < 0 {
			return -1, fmt.Errorf("Unable to find a common ancestor")
		}
		// Reorgs are soft-limited to depth 6 by dcrd. More than six blocks without
		// a match probably indicates an issue.
		if commonHeight-tipHeight == 7 {
			log.Warnf("No common ancestor within 6 blocks. This is abnormal")
		}

	}
	return tipHeight - commonHeight, nil
}

// GetChainWork fetches the dcrjson.BlockHeaderVerbose and returns only the
// ChainWork field as a string.
func GetChainWork(client BlockFetcher, hash *chainhash.Hash) (string, error) {
	header, err := client.GetBlockHeaderVerbose(hash)
	if err != nil {
		return "", err
	}
	return header.ChainWork, nil
}

type MempoolAddressChecker interface {
	UnconfirmedTxnsForAddress(address string) (*txhelpers.AddressOutpoints, int64, error)
}

type mempoolAddressChecker struct {
	client *rpcclient.Client
	params *chaincfg.Params
}

func (m *mempoolAddressChecker) UnconfirmedTxnsForAddress(address string) (*txhelpers.AddressOutpoints, int64, error) {
	return UnconfirmedTxnsForAddress(m.client, address, m.params)
}

func NewMempoolAddressChecker(client *rpcclient.Client, params *chaincfg.Params) MempoolAddressChecker {
	return &mempoolAddressChecker{client, params}
}

// UnconfirmedTxnsForAddress returns the chainhash.Hash of all transactions in
// mempool that (1) pay to the given address, or (2) spend a previous outpoint
// that paid to the address.
func UnconfirmedTxnsForAddress(client *rpcclient.Client, address string, params *chaincfg.Params) (*txhelpers.AddressOutpoints, int64, error) {
	// Mempool transactions
	var numUnconfirmed int64
	mempoolTxns, err := client.GetRawMempoolVerbose(dcrjson.GRMAll)
	if err != nil {
		log.Warnf("GetRawMempool failed for address %s: %v", address, err)
		return nil, numUnconfirmed, err
	}

	// Check each transaction for involvement with provided address.
	addressOutpoints := txhelpers.NewAddressOutpoints(address)
	for hash, tx := range mempoolTxns {
		// Transaction details from dcrd
		txhash, err1 := chainhash.NewHashFromStr(hash)
		if err1 != nil {
			log.Errorf("Invalid transaction hash %s", hash)
			return addressOutpoints, 0, err1
		}

		Tx, err1 := client.GetRawTransaction(txhash)
		if err1 != nil {
			log.Warnf("Unable to GetRawTransaction(%s): %v", hash, err1)
			err = err1
			continue
		}
		// Scan transaction for inputs/outputs involving the address of interest
		outpoints, prevouts, prevTxns := txhelpers.TxInvolvesAddress(Tx.MsgTx(),
			address, client, params)
		if len(outpoints) == 0 && len(prevouts) == 0 {
			continue
		}
		// Update previous outpoint txn slice with mempool time
		for f := range prevTxns {
			prevTxns[f].MemPoolTime = tx.Time
		}

		// Add present transaction to previous outpoint txn slice
		numUnconfirmed++
		thisTxUnconfirmed := &txhelpers.TxWithBlockData{
			Tx:          Tx.MsgTx(),
			MemPoolTime: tx.Time,
		}
		prevTxns = append(prevTxns, thisTxUnconfirmed)
		// Merge the I/Os and the transactions into results
		addressOutpoints.Update(prevTxns, outpoints, prevouts)
	}

	return addressOutpoints, numUnconfirmed, err
}

// APITransaction uses the RPC client to retrieve the specified transaction, and
// convert the data into a *apitypes.Tx.
func APITransaction(client *rpcclient.Client, txid *chainhash.Hash) (tx *apitypes.Tx, hex string, err error) {
	txraw, err := GetTransactionVerboseByID(client, txid)
	if err != nil {
		err = fmt.Errorf("APITransaction failed for %v: %v", txid, err)
		return
	}
	hex = txraw.Hex

	tx = new(apitypes.Tx)
	tx.TxID = txraw.Txid
	tx.Size = int32(len(hex) / 2)
	tx.Version = txraw.Version
	tx.Locktime = txraw.LockTime
	tx.Expiry = txraw.Expiry
	tx.Vin = make([]dcrjson.Vin, len(txraw.Vin))
	copy(tx.Vin, txraw.Vin)
	tx.Vout = make([]apitypes.Vout, len(txraw.Vout))
	for i := range txraw.Vout {
		tx.Vout[i].Value = txraw.Vout[i].Value
		tx.Vout[i].N = txraw.Vout[i].N
		tx.Vout[i].Version = txraw.Vout[i].Version
		spk := &tx.Vout[i].ScriptPubKeyDecoded
		spkRaw := &txraw.Vout[i].ScriptPubKey
		spk.Asm = spkRaw.Asm
		spk.Hex = spkRaw.Hex
		spk.ReqSigs = spkRaw.ReqSigs
		spk.Type = spkRaw.Type
		spk.Addresses = make([]string, len(spkRaw.Addresses))
		for j := range spkRaw.Addresses {
			spk.Addresses[j] = spkRaw.Addresses[j]
		}
		if spkRaw.CommitAmt != nil {
			spk.CommitAmt = new(float64)
			*spk.CommitAmt = *spkRaw.CommitAmt
		}
	}

	tx.Confirmations = txraw.Confirmations

	// BlockID
	tx.Block = new(apitypes.BlockID)
	tx.Block.BlockHash = txraw.BlockHash
	tx.Block.BlockHeight = txraw.BlockHeight
	tx.Block.BlockIndex = txraw.BlockIndex
	tx.Block.Time = txraw.Time
	tx.Block.BlockTime = txraw.Blocktime

	return
}
