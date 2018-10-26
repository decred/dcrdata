// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package blockdata

import (
	"errors"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/v3/api/types"
	"github.com/decred/dcrdata/v3/stakedb"
	"github.com/decred/dcrdata/v3/txhelpers"
)

// BlockData contains all the data collected by a Collector and stored
// by a BlockDataSaver. TODO: consider if pointers are desirable here.
type BlockData struct {
	Header           dcrjson.GetBlockHeaderVerboseResult
	Connections      int32
	FeeInfo          dcrjson.FeeInfoBlock
	CurrentStakeDiff dcrjson.GetStakeDifficultyResult
	EstStakeDiff     dcrjson.EstimateStakeDiffResult
	PoolInfo         *apitypes.TicketPoolInfo
	ExtraInfo        apitypes.BlockExplorerExtraInfo
	PriceWindowNum   int
	IdxBlockInWindow int
	WinningTickets   []string
}

// ToStakeInfoExtended returns an apitypes.StakeInfoExtended object from the
// blockdata
func (b *BlockData) ToStakeInfoExtended() apitypes.StakeInfoExtended {
	return apitypes.StakeInfoExtended{
		Feeinfo:          b.FeeInfo,
		StakeDiff:        b.CurrentStakeDiff.CurrentStakeDifficulty,
		PriceWindowNum:   b.PriceWindowNum,
		IdxBlockInWindow: b.IdxBlockInWindow,
		PoolInfo:         b.PoolInfo,
	}
}

// ToStakeInfoExtendedEstimates returns an apitypes.StakeInfoExtendedEstimates
// object from the blockdata
func (b *BlockData) ToStakeInfoExtendedEstimates() apitypes.StakeInfoExtendedEstimates {
	return apitypes.StakeInfoExtendedEstimates{
		Feeinfo: b.FeeInfo,
		StakeDiff: apitypes.StakeDiff{
			GetStakeDifficultyResult: b.CurrentStakeDiff,
			Estimates:                b.EstStakeDiff,
			IdxBlockInWindow:         b.IdxBlockInWindow,
			PriceWindowNum:           b.PriceWindowNum,
		},
		// PriceWindowNum and Idx... are repeated here since this is a kludge
		PriceWindowNum:   b.PriceWindowNum,
		IdxBlockInWindow: b.IdxBlockInWindow,
		PoolInfo:         b.PoolInfo,
	}
}

// ToBlockSummary returns an apitypes.BlockDataBasic object from the blockdata
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

// ToBlockExplorerSummary returns a BlockExplorerBasic
func (b *BlockData) ToBlockExplorerSummary() apitypes.BlockExplorerBasic {
	t := time.Unix(b.Header.Time, 0)
	ftime := t.Format("2006-01-02 15:04:05")
	extra := b.ExtraInfo
	extra.FormattedTime = ftime
	return apitypes.BlockExplorerBasic{
		Height:                 b.Header.Height,
		Size:                   b.Header.Size,
		Voters:                 b.Header.Voters,
		Revocations:            b.Header.Revocations,
		FreshStake:             b.Header.FreshStake,
		StakeDiff:              b.Header.SBits,
		BlockExplorerExtraInfo: extra,
		Time:                   b.Header.Time,
	}
}

// Collector models a structure for the source of the blockdata
type Collector struct {
	sync.Mutex
	dcrdChainSvr *rpcclient.Client
	netParams    *chaincfg.Params
	stakeDB      *stakedb.StakeDatabase
}

// NewCollector creates a new Collector.
func NewCollector(dcrdChainSvr *rpcclient.Client, params *chaincfg.Params,
	stakeDB *stakedb.StakeDatabase) *Collector {
	return &Collector{
		dcrdChainSvr: dcrdChainSvr,
		netParams:    params,
		stakeDB:      stakeDB,
	}
}

// CollectAPITypes uses CollectBlockInfo to collect block data, then organizes
// it into the BlockDataBasic and StakeInfoExtended and dcrdataapi types.
func (t *Collector) CollectAPITypes(hash *chainhash.Hash) (*apitypes.BlockDataBasic, *apitypes.StakeInfoExtended) {
	blockDataBasic, feeInfoBlock, _, _, _, err := t.CollectBlockInfo(hash)
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
	*dcrjson.FeeInfoBlock, *dcrjson.GetBlockHeaderVerboseResult,
	*apitypes.BlockExplorerExtraInfo, *wire.MsgBlock, error) {
	// Retrieve block from dcrd.
	msgBlock, err := t.dcrdChainSvr.GetBlock(hash)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	height := msgBlock.Header.Height
	block := dcrutil.NewBlock(msgBlock)
	txLen := len(block.Transactions())

	// Coin supply and block subsidy. If either RPC fails, do not immediately
	// return. Attempt acquisition of other data for this block.
	coinSupply, err := t.dcrdChainSvr.GetCoinSupply()
	if err != nil {
		log.Error("GetCoinSupply failed: ", err)
	}
	nbSubsidy, err := t.dcrdChainSvr.GetBlockSubsidy(int64(msgBlock.Header.Height)+1, 5)
	if err != nil {
		log.Errorf("GetBlockSubsidy for %d failed: %v", msgBlock.Header.Height, err)
	}

	// Block header
	blockHeaderResults, err := t.dcrdChainSvr.GetBlockHeaderVerbose(hash)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	isSideChain := blockHeaderResults.Confirmations == -1

	// Ticket pool info (value, size, avg)
	var ticketPoolInfo *apitypes.TicketPoolInfo
	var found bool
	if ticketPoolInfo, found = t.stakeDB.PoolInfo(*hash); !found {
		// If unable to get ticket pool info for this block, stakedb does
		// not have it. This is expected for side chain blocks, so do not
		// log in that case.
		if !isSideChain {
			log.Infof("Unable to find block (%v) in pool info cache, trying best block.", hash)
		}
		ticketPoolInfo = t.stakeDB.PoolInfoBest()
		if ticketPoolInfo.Height != height {
			if !isSideChain {
				log.Warnf("Ticket pool info not available for block %v.", hash)
			}
			ticketPoolInfo = nil
		}
	}

	// Fee info
	feeInfoBlock := txhelpers.FeeRateInfoBlock(block)
	if feeInfoBlock == nil {
		log.Error("FeeInfoBlock failed")
	}

	// Work/Stake difficulty
	header := msgBlock.Header
	diff := txhelpers.GetDifficultyRatio(header.Bits, t.netParams)
	sdiff := dcrutil.Amount(header.SBits).ToCoin()

	// Output
	blockdata := &apitypes.BlockDataBasic{
		Height:     height,
		Size:       uint32(block.MsgBlock().SerializeSize()),
		Hash:       hash.String(),
		Difficulty: diff,
		StakeDiff:  sdiff,
		Time:       header.Timestamp.Unix(),
		PoolInfo:   ticketPoolInfo,
	}
	extrainfo := &apitypes.BlockExplorerExtraInfo{
		TxLen:            txLen,
		CoinSupply:       int64(coinSupply),
		NextBlockSubsidy: nbSubsidy,
	}
	return blockdata, feeInfoBlock, blockHeaderResults, extrainfo, msgBlock, err
}

// CollectHash collects chain data at the block with the specified hash.
func (t *Collector) CollectHash(hash *chainhash.Hash) (*BlockData, *wire.MsgBlock, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.Lock()
	defer t.Unlock()

	// Time this function
	defer func(start time.Time) {
		log.Debugf("Collector.CollectHash() completed in %v", time.Since(start))
	}(time.Now())

	// Info specific to the block hash
	blockDataBasic, feeInfoBlock, blockHeaderVerbose, extra, msgBlock, err := t.CollectBlockInfo(hash)
	if err != nil {
		return nil, nil, err
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
		CurrentStakeDiff: dcrjson.GetStakeDifficultyResult{CurrentStakeDifficulty: blockDataBasic.StakeDiff},
		EstStakeDiff:     dcrjson.EstimateStakeDiffResult{},
		PoolInfo:         blockDataBasic.PoolInfo,
		ExtraInfo:        *extra,
		PriceWindowNum:   int(height / winSize),
		IdxBlockInWindow: int(height%winSize) + 1,
	}

	return blockdata, msgBlock, err
}

// Collect collects chain data at the current best block.
func (t *Collector) Collect() (*BlockData, *wire.MsgBlock, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.Lock()
	defer t.Unlock()

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
	}()

	var bbs bbhRes
	select {
	case bbs = <-toch:
	case <-time.After(time.Second * 10):
		log.Errorf("Timeout waiting for dcrd.")
		return nil, nil, errors.New("Timeout")
	}

	// Stake difficulty
	stakeDiff, err := t.dcrdChainSvr.GetStakeDifficulty()
	if err != nil {
		return nil, nil, err
	}

	// estimatestakediff
	estStakeDiff, err := t.dcrdChainSvr.EstimateStakeDiff(nil)
	if err != nil {
		log.Warn("estimatestakediff is broken: ", err)
		estStakeDiff = &dcrjson.EstimateStakeDiffResult{}
	}

	// Info specific to the block hash
	blockDataBasic, feeInfoBlock, blockHeaderVerbose, extra, msgBlock, err := t.CollectBlockInfo(bbs.hash)
	if err != nil {
		return nil, nil, err
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
		ExtraInfo:        *extra,
		PoolInfo:         blockDataBasic.PoolInfo,
		PriceWindowNum:   int(height / winSize),
		IdxBlockInWindow: int(height%winSize) + 1,
	}

	return blockdata, msgBlock, err
}
