package blockdata

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"sync"
	"time"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

// BlockData
// consider if pointers are desirable here
type BlockData struct {
	Header           dcrjson.GetBlockHeaderVerboseResult
	Connections      int32
	FeeInfo          dcrjson.FeeInfoBlock
	CurrentStakeDiff dcrjson.GetStakeDifficultyResult
	EstStakeDiff     dcrjson.EstimateStakeDiffResult
	PoolInfo         apitypes.TicketPoolInfo
	PriceWindowNum   int
	IdxBlockInWindow int
}

func (b *BlockData) ToStakeInfoExtended() apitypes.StakeInfoExtended {
	return apitypes.StakeInfoExtended{
		Feeinfo: b.FeeInfo,
		StakeDiff: apitypes.StakeDiff{dcrjson.GetStakeDifficultyResult{
			b.CurrentStakeDiff.CurrentStakeDifficulty,
			b.CurrentStakeDiff.NextStakeDifficulty},
			b.EstStakeDiff},
		PriceWindowNum:   b.PriceWindowNum,
		IdxBlockInWindow: b.IdxBlockInWindow,
		PoolInfo:         b.PoolInfo,
	}
}

func (b *BlockData) ToBlockSummary() apitypes.BlockDataBasic {
	return apitypes.BlockDataBasic{
		Height:     b.Header.Height,
		Size:       b.Header.Size,
		Hash:       b.Header.Hash,
		Difficulty: b.Header.Difficulty,
		StakeDiff:  b.Header.SBits,
		Time:       b.Header.Time,
		PoolInfo:   b.PoolInfo,
	}
}

type blockDataCollector struct {
	mtx          sync.Mutex
	dcrdChainSvr *dcrrpcclient.Client
	netParams    *chaincfg.Params
}

// NewBlockDataCollector creates a new blockDataCollector.
func NewBlockDataCollector(dcrdChainSvr *dcrrpcclient.Client, params *chaincfg.Params) *blockDataCollector {
	return &blockDataCollector{
		mtx:          sync.Mutex{},
		dcrdChainSvr: dcrdChainSvr,
		netParams:    params,
	}
}

// Collect is the main handler for collecting chain data at the current best
// block. The input argument specifies if ticket pool value should be omitted.
func (t *blockDataCollector) Collect(noTicketPool bool) (*BlockData, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Time this function
	defer func(start time.Time) {
		log.Debugf("blockDataCollector.Collect() completed in %v", time.Since(start))
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
	winSize := uint32(t.netParams.StakeDiffWindowSize)
	blockdata := &BlockData{
		Header:           blockHeaderResults,
		Connections:      info.Connections,
		FeeInfo:          feeInfoBlock,
		CurrentStakeDiff: *stakeDiff,
		EstStakeDiff:     *estStakeDiff,
		PoolInfo:         ticketPoolInfo,
		PriceWindowNum:   int(height / winSize),
		IdxBlockInWindow: int(height%winSize) + 1,
	}

	return blockdata, err
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
		log.Errorf("Cannot get difficulty: %v", err)
		return 0
	}
	return diff
}
