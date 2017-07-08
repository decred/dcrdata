// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package blockdata

import (
	"errors"
	"sync"
	"time"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/stakedb"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

// BlockData contains all the data collected by a Collector and stored
// by a BlockDataSaver. TODO: consider if pointers are desirable here.
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
		Feeinfo:          b.FeeInfo,
		StakeDiff:        b.CurrentStakeDiff.CurrentStakeDifficulty,
		PriceWindowNum:   b.PriceWindowNum,
		IdxBlockInWindow: b.IdxBlockInWindow,
		PoolInfo:         b.PoolInfo,
	}
}

func (b *BlockData) ToStakeInfoExtendedEstimates() apitypes.StakeInfoExtendedEstimates {
	return apitypes.StakeInfoExtendedEstimates{
		Feeinfo: b.FeeInfo,
		StakeDiff: apitypes.StakeDiff{
			GetStakeDifficultyResult: b.CurrentStakeDiff,
			Estimates:                b.EstStakeDiff,
		},
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

type Collector struct {
	mtx          sync.Mutex
	dcrdChainSvr *dcrrpcclient.Client
	netParams    *chaincfg.Params
	stakeDB      *stakedb.StakeDatabase
}

// NewCollector creates a new Collector.
func NewCollector(dcrdChainSvr *dcrrpcclient.Client, params *chaincfg.Params,
	stakeDB *stakedb.StakeDatabase) *Collector {
	return &Collector{
		mtx:          sync.Mutex{},
		dcrdChainSvr: dcrdChainSvr,
		netParams:    params,
		stakeDB:      stakeDB,
	}
}

// CollectAPITypes uses CollectBlockInfo to collect block data, then organizes
// it into the BlockDataBasic and StakeInfoExtended and dcrdataapi types.
func (t *Collector) CollectAPITypes(hash *chainhash.Hash) (*apitypes.BlockDataBasic, *apitypes.StakeInfoExtended) {
	blockDataBasic, feeInfoBlock, _, err := t.CollectBlockInfo(hash)
	if err != nil {
		return nil, nil
	}

	height := int64(blockDataBasic.Height)
	winSize := t.netParams.StakeDiffWindowSize

	stakeInfoExtended := &apitypes.StakeInfoExtended{
		Feeinfo:          *feeInfoBlock,
		StakeDiff:        blockDataBasic.StakeDiff,
		PriceWindowNum:   int(height / winSize),
		IdxBlockInWindow: int(height%winSize) + 1,
		PoolInfo:         blockDataBasic.PoolInfo,
	}

	return blockDataBasic, stakeInfoExtended
}

// CollectBlockInfo uses the chain server and the stake DB to collect most of
// the block data required by Collect() that is specific to the block with the
// given hash.
func (t *Collector) CollectBlockInfo(hash *chainhash.Hash) (*apitypes.BlockDataBasic,
	*dcrjson.FeeInfoBlock, *dcrjson.GetBlockHeaderVerboseResult, error) {
	block, err := t.dcrdChainSvr.GetBlock(hash)
	if err != nil {
		return nil, nil, nil, err
	}
	height := block.Height()

	// Ticket pool info (value, size, avg)
	ticketPoolInfo, sdbHeight := t.stakeDB.PoolInfo()
	if sdbHeight != uint32(height) {
		log.Warnf("Chain server height %d != stake db height %d. Pool info will not match.", sdbHeight, height)
	}

	// Fee info
	feeInfoBlock := txhelpers.FeeRateInfoBlock(block, t.dcrdChainSvr)
	if feeInfoBlock == nil {
		log.Error("FeeInfoBlock failed")
	}

	// Work/Stake difficulty
	header := block.MsgBlock().Header
	diff := txhelpers.GetDifficultyRatio(header.Bits, t.netParams)
	sdiff := dcrutil.Amount(header.SBits).ToCoin()

	blockHeaderResults, err := t.dcrdChainSvr.GetBlockHeaderVerbose(hash)
	if err != nil {
		return nil, nil, nil, err
	}

	// Output
	blockdata := &apitypes.BlockDataBasic{
		Height:     uint32(height),
		Size:       uint32(block.MsgBlock().SerializeSize()),
		Hash:       hash.String(),
		Difficulty: diff,
		StakeDiff:  sdiff,
		Time:       header.Timestamp.Unix(),
		PoolInfo:   ticketPoolInfo,
	}

	return blockdata, feeInfoBlock, blockHeaderResults, err
}

// Collect is the main handler for collecting chain data at the current best
// block. The input argument specifies if ticket pool value should be omitted.
func (t *Collector) Collect() (*BlockData, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Time this function
	defer func(start time.Time) {
		log.Debugf("Collector.Collect() completed in %v", time.Since(start))
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

	// Stake difficulty
	stakeDiff, err := t.dcrdChainSvr.GetStakeDifficulty()
	if err != nil {
		return nil, err
	}

	// estimatestakediff
	estStakeDiff, err := t.dcrdChainSvr.EstimateStakeDiff(nil)
	if err != nil {
		log.Warn("estimatestakediff is broken: ", err)
		estStakeDiff = &dcrjson.EstimateStakeDiffResult{}
	}

	// Info specific to the block hash
	blockDataBasic, feeInfoBlock, blockHeaderVerbose, err := t.CollectBlockInfo(bbs.hash)
	if err != nil {
		return nil, err
	}

	// Number of peer connection to chain server
	numConn, err := t.dcrdChainSvr.GetConnectionCount()
	if err != nil {
		log.Warn("Unable to get connection count: ", err)
	}

	// Output
	height := int64(blockDataBasic.Height)
	winSize := t.netParams.StakeDiffWindowSize
	blockdata := &BlockData{
		Header:           *blockHeaderVerbose,
		Connections:      int32(numConn),
		FeeInfo:          *feeInfoBlock,
		CurrentStakeDiff: *stakeDiff,
		EstStakeDiff:     *estStakeDiff,
		PoolInfo:         blockDataBasic.PoolInfo,
		PriceWindowNum:   int(height / winSize),
		IdxBlockInWindow: int(height%winSize) + 1,
	}

	return blockdata, err
}
