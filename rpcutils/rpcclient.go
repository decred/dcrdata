// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package rpcutils

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"strconv"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/v3/api/types"
	"github.com/decred/dcrdata/v3/semver"
	"github.com/decred/dcrdata/v3/txhelpers"
)

// Any of the following dcrd RPC API versions are deemed compatible with
// dcrdata.
var compatibleChainServerAPIs = []semver.Semver{
	semver.NewSemver(3, 0, 0),
	semver.NewSemver(4, 0, 0), // bumped for removal of createrawssgen, not used by dcrdata
}

// ConnectNodeRPC attempts to create a new websocket connection to a dcrd node,
// with the given credentials and optional notification handlers.
func ConnectNodeRPC(host, user, pass, cert string, disableTLS bool,
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
		Host:         host,
		Endpoint:     "ws", // websocket
		User:         user,
		Pass:         pass,
		Certificates: dcrdCerts,
		DisableTLS:   disableTLS,
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
		NextHash:      next,
	}

	return &blockHeaderResult
}

// GetBlockHeaderVerbose creates a *dcrjson.GetBlockHeaderVerboseResult for the
// block at height idx via an RPC connection to a chain server.
func GetBlockHeaderVerbose(client *rpcclient.Client, idx int64) *dcrjson.GetBlockHeaderVerboseResult {
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
func GetBlockHeaderVerboseByString(client *rpcclient.Client, hash string) *dcrjson.GetBlockHeaderVerboseResult {
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
func GetBlock(ind int64, client *rpcclient.Client) (*dcrutil.Block, *chainhash.Hash, error) {
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
func GetBlockByHash(blockhash *chainhash.Hash, client *rpcclient.Client) (*dcrutil.Block, error) {
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
// block hash. The last block is not mainchain, but it's previous block is.
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
func GetTransactionVerboseByID(client *rpcclient.Client, txid string) (*dcrjson.TxRawResult, error) {
	txhash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		log.Errorf("Invalid transaction hash %s", txid)
		return nil, err
	}

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
