// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package rpcutils

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"strconv"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/semver"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

var requiredChainServerAPI = semver.NewSemver(3, 0, 0)

// ConnectNodeRPC attempts to create a new websocket connection to a dcrd node,
// with the given credentials and optional notification handlers.
func ConnectNodeRPC(host, user, pass, cert string, disableTLS bool,
	ntfnHandlers ...*dcrrpcclient.NotificationHandlers) (*dcrrpcclient.Client, semver.Semver, error) {
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

	connCfgDaemon := &dcrrpcclient.ConnConfig{
		Host:         host,
		Endpoint:     "ws", // websocket
		User:         user,
		Pass:         pass,
		Certificates: dcrdCerts,
		DisableTLS:   disableTLS,
	}

	var ntfnHdlrs *dcrrpcclient.NotificationHandlers
	if len(ntfnHandlers) > 0 {
		if len(ntfnHandlers) > 1 {
			return nil, nodeVer, fmt.Errorf("Invalid notification handler argument.")
		}
		ntfnHdlrs = ntfnHandlers[0]
	}
	dcrdClient, err := dcrrpcclient.New(connCfgDaemon, ntfnHdlrs)
	if err != nil {
		return nil, nodeVer, fmt.Errorf("Failed to start dcrd RPC client: %s\n", err.Error())
	}

	// Ensure the RPC server has a compatible API version.
	ver, err := dcrdClient.Version()
	if err != nil {
		log.Error("Unable to get RPC version: ", err)
		return nil, nodeVer, fmt.Errorf("Unable to get node RPC version")
	}

	dcrdVer := ver["dcrdjsonrpcapi"]
	nodeVer = semver.NewSemver(dcrdVer.Major, dcrdVer.Minor, dcrdVer.Patch)

	if !semver.SemverCompatible(requiredChainServerAPI, nodeVer) {
		return nil, nodeVer, fmt.Errorf("Node JSON-RPC server does not have "+
			"a compatible API version. Advertises %v but require %v",
			nodeVer, requiredChainServerAPI)
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
		Confirmations: uint64(currentHeight - int64(header.Height)),
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
// block index specified by idx via an RPC connection to a chain server.
func GetBlockHeaderVerbose(client *dcrrpcclient.Client, params *chaincfg.Params,
	idx int64) *dcrjson.GetBlockHeaderVerboseResult {
	height, err := client.GetBlockCount()
	if err != nil {
		log.Errorf("GetBlockCount failed: %v", err)
		return nil
	}
	if idx > height {
		log.Errorf("Block %d does not exist.", idx)
		return nil
	}

	blockhash, err := client.GetBlockHash(idx)
	if err != nil {
		log.Errorf("GetBlockHash(%d) failed: %v", idx, err)
		return nil
	}

	block, err := client.GetBlock(blockhash)
	if err != nil {
		log.Errorf("GetBlock failed (%s): %v", blockhash, err)
		return nil
	}

	blockHeader := block.MsgBlock().Header
	blockHeaderVerbose := BuildBlockHeaderVerbose(&blockHeader, params, height)

	return blockHeaderVerbose
}

// GetStakeDiffEstimates combines the results of EstimateStakeDiff and
// GetStakeDifficulty into a *apitypes.StakeDiff.
func GetStakeDiffEstimates(client *dcrrpcclient.Client) *apitypes.StakeDiff {
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
func GetBlock(ind int64, client *dcrrpcclient.Client) (*dcrutil.Block, *chainhash.Hash, error) {
	blockhash, err := client.GetBlockHash(ind)
	if err != nil {
		return nil, nil, fmt.Errorf("GetBlockHash(%d) failed: %v", ind, err)
	}

	block, err := client.GetBlock(blockhash)
	if err != nil {
		return nil, blockhash,
			fmt.Errorf("GetBlock failed (%s): %v", blockhash, err)
	}

	return block, blockhash, nil
}
