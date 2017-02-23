package main

import (
	"sync"
	"time"

	"encoding/hex"
	"errors"
	"fmt"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"strconv"
)

// blockData
// consider if pointers are desirable here
type blockData struct {
	header           dcrjson.GetBlockHeaderVerboseResult
	connections      int32
	feeinfo          dcrjson.FeeInfoBlock
	currentstakediff dcrjson.GetStakeDifficultyResult
	eststakediff     dcrjson.EstimateStakeDiffResult
	poolinfo         apitypes.TicketPoolInfo
	priceWindowNum   int
	idxBlockInWindow int
}

type blockDataCollector struct {
	mtx          sync.Mutex
	dcrdChainSvr *dcrrpcclient.Client
}

// newBlockDataCollector creates a new blockDataCollector.
func newBlockDataCollector(dcrdChainSvr *dcrrpcclient.Client) *blockDataCollector {
	return &blockDataCollector{
		mtx:          sync.Mutex{},
		dcrdChainSvr: dcrdChainSvr,
	}
}

// collect is the main handler for collecting chain data
func (t *blockDataCollector) collect(noTicketPool bool) (*blockData, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Time this function
	defer func(start time.Time) {
		log.Debugf("blockDataCollector.collect() completed in %v", time.Since(start))
	}(time.Now())

	// Run first client call with a timeout
	type bbhRes struct {
		err  error
		hash *chainhash.Hash
	}
	toch := make(chan bbhRes)

	// Pull and store relevant data about the blockchain.
	go func() {
		bestBlockHash, err := t.dcrdChainSvr.GetBestBlockHash()
		toch <- bbhRes{err, bestBlockHash}
		return
	}()

	var bbs bbhRes
	select {
	case bbs = <-toch:
	case <-time.After(time.Second * 10):
		log.Errorf("Timeout waiting for dcrd.")
		return nil, errors.New("Timeout")
	}

	bestBlockHash := bbs.hash

	bestBlock, err := t.dcrdChainSvr.GetBlock(bestBlockHash)
	if err != nil {
		return nil, err
	}

	blockHeader := bestBlock.MsgBlock().Header
	//timestamp := blockHeader.Timestamp
	height := blockHeader.Height

	// In datasaver.go check TicketPoolInfo.PoolValue >= 0
	poolSize := blockHeader.PoolSize
	ticketPoolInfo := apitypes.TicketPoolInfo{poolSize, -1, -1}
	if !noTicketPool {
		poolValue, err := t.dcrdChainSvr.GetTicketPoolValue()
		if err != nil {
			return nil, err
		}
		avgPricePoolAmt := dcrutil.Amount(0)
		if poolSize != 0 {
			avgPricePoolAmt = poolValue / dcrutil.Amount(poolSize)
		}

		ticketPoolInfo = apitypes.TicketPoolInfo{poolSize, poolValue.ToCoin(),
			avgPricePoolAmt.ToCoin()}
	}
	// Fee info
	numFeeBlocks := uint32(1)
	numFeeWindows := uint32(0)

	feeInfo, err := t.dcrdChainSvr.TicketFeeInfo(&numFeeBlocks, &numFeeWindows)
	if err != nil {
		return nil, err
	}

	if len(feeInfo.FeeInfoBlocks) == 0 {
		return nil, fmt.Errorf("Unable to get fee info for block %d", height)
	}
	feeInfoBlock := feeInfo.FeeInfoBlocks[0]

	// Stake difficulty
	stakeDiff, err := t.dcrdChainSvr.GetStakeDifficulty()
	if err != nil {
		return nil, err
	}

	// To get difficulty, use getinfo or getmininginfo
	info, err := t.dcrdChainSvr.GetInfo()
	//t.dcrdChainSvr.GetConnectionCount()

	// blockVerbose, err := t.dcrdChainSvr.GetBlockVerbose(bestBlockHash, false)
	// if err != nil {
	// 	log.Error(err)
	// }

	// We want a GetBlockHeaderVerboseResult
	// Not sure how to manage this:
	//cmd := dcrjson.NewGetBlockHeaderCmd(bestBlockHash.String(), dcrjson.Bool(true))
	// instead:
	blockHeaderResults := dcrjson.GetBlockHeaderVerboseResult{
		Hash:          bestBlockHash.String(),
		Confirmations: uint64(1),
		Version:       blockHeader.Version,
		PreviousHash:  blockHeader.PrevBlock.String(),
		MerkleRoot:    blockHeader.MerkleRoot.String(),
		StakeRoot:     blockHeader.StakeRoot.String(),
		VoteBits:      blockHeader.VoteBits,
		FinalState:    hex.EncodeToString(blockHeader.FinalState[:]),
		Voters:        blockHeader.Voters,
		FreshStake:    blockHeader.FreshStake,
		Revocations:   blockHeader.Revocations,
		PoolSize:      blockHeader.PoolSize,
		Bits:          strconv.FormatInt(int64(blockHeader.Bits), 16),
		SBits:         dcrutil.Amount(blockHeader.SBits).ToCoin(),
		Height:        blockHeader.Height,
		Size:          blockHeader.Size,
		Time:          blockHeader.Timestamp.Unix(),
		Nonce:         blockHeader.Nonce,
		Difficulty:    info.Difficulty,
		NextHash:      "",
	}

	// estimatestakediff
	estStakeDiff, err := t.dcrdChainSvr.EstimateStakeDiff(nil)
	if err != nil {
		return nil, err
	}

	// Output
	winSize := uint32(activeNet.StakeDiffWindowSize)
	blockdata := &blockData{
		header:           blockHeaderResults,
		connections:      info.Connections,
		feeinfo:          feeInfoBlock,
		currentstakediff: *stakeDiff,
		eststakediff:     *estStakeDiff,
		poolinfo:         ticketPoolInfo,
		priceWindowNum:   int(height / winSize),
		idxBlockInWindow: int(height%winSize) + 1,
	}

	return blockdata, err
}
