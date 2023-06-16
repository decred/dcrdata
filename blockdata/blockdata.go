// Copyright (c) 2020-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package blockdata

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/wire"

	apitypes "github.com/decred/dcrdata/v8/api/types"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/stakedb"
	"github.com/decred/dcrdata/v8/txhelpers"
)

// BlockData contains all the data collected by a Collector and stored
// by a BlockDataSaver. TODO: consider if pointers are desirable here.
type BlockData struct {
	Header           chainjson.GetBlockHeaderVerboseResult
	Connections      int32
	FeeInfo          chainjson.FeeInfoBlock
	CurrentStakeDiff chainjson.GetStakeDifficultyResult
	EstStakeDiff     chainjson.EstimateStakeDiffResult
	PoolInfo         *apitypes.TicketPoolInfo
	ExtraInfo        apitypes.BlockExplorerExtraInfo
	BlockchainInfo   *chainjson.GetBlockChainInfoResult
	PriceWindowNum   int
	IdxBlockInWindow int
	WinningTickets   []string
}

// ToStakeInfoExtended returns an apitypes.StakeInfoExtended object from the
// blockdata
func (b *BlockData) ToStakeInfoExtended() apitypes.StakeInfoExtended {
	return apitypes.StakeInfoExtended{
		Hash:             b.Header.Hash,
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
		Hash:    b.Header.Hash,
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
	t := dbtypes.NewTimeDefFromUNIX(b.Header.Time)
	return apitypes.BlockDataBasic{
		Height:     b.Header.Height,
		Size:       b.Header.Size,
		Hash:       b.Header.Hash,
		Difficulty: b.Header.Difficulty,
		StakeDiff:  b.Header.SBits,
		Time:       apitypes.TimeAPI{S: t},
		PoolInfo:   b.PoolInfo,
	}
}

// ToBlockExplorerSummary returns a BlockExplorerBasic
func (b *BlockData) ToBlockExplorerSummary() apitypes.BlockExplorerBasic {
	extra := b.ExtraInfo
	t := dbtypes.NewTimeDefFromUNIX(b.Header.Time)
	return apitypes.BlockExplorerBasic{
		Height:                 b.Header.Height,
		Size:                   b.Header.Size,
		Voters:                 b.Header.Voters,
		Revocations:            b.Header.Revocations,
		FreshStake:             b.Header.FreshStake,
		StakeDiff:              b.Header.SBits,
		BlockExplorerExtraInfo: extra,
		Time:                   t,
	}
}

// NodeClient is the RPC client functionality required by Collector.
type NodeClient interface {
	GetBlockCount(ctx context.Context) (int64, error)
	GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error)
	GetBlockHeaderVerbose(ctx context.Context, hash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error)
	GetCoinSupply(ctx context.Context) (dcrutil.Amount, error)
	GetBlockSubsidy(ctx context.Context, height int64, voters uint16) (*chainjson.GetBlockSubsidyResult, error)
	GetBlockChainInfo(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error)
	GetConnectionCount(ctx context.Context) (int64, error)
	EstimateStakeDiff(ctx context.Context, tickets *uint32) (*chainjson.EstimateStakeDiffResult, error)
	GetStakeDifficulty(ctx context.Context) (*chainjson.GetStakeDifficultyResult, error)
}

// Collector models a structure for the source of the blockdata
type Collector struct {
	mtx          sync.Mutex
	dcrdChainSvr NodeClient
	netParams    *chaincfg.Params
	stakeDB      *stakedb.StakeDatabase
}

// NewCollector creates a new Collector.
func NewCollector(dcrdChainSvr NodeClient, params *chaincfg.Params,
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
		Hash:             blockDataBasic.Hash,
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
	*chainjson.FeeInfoBlock, *chainjson.GetBlockHeaderVerboseResult,
	*apitypes.BlockExplorerExtraInfo, *wire.MsgBlock, error) {
	// 2 minutes for all the RPCs.
	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	defer cancel()

	// Retrieve block from dcrd.
	msgBlock, err := t.dcrdChainSvr.GetBlock(ctx, hash)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	header := msgBlock.Header
	height := header.Height
	txLen := len(msgBlock.Transactions) + len(msgBlock.STransactions)

	// Coin supply and block subsidy. If either RPC fails, do not immediately
	// return. Attempt acquisition of other data for this block.
	coinSupply, err := t.dcrdChainSvr.GetCoinSupply(ctx)
	if err != nil {
		log.Error("GetCoinSupply failed: ", err)
	}
	nbSubsidy, err := t.dcrdChainSvr.GetBlockSubsidy(ctx, int64(height)+1, 5)
	if err != nil {
		log.Errorf("GetBlockSubsidy for %d failed: %v", height, err)
	}

	// Block header
	blockHeaderResults, err := t.dcrdChainSvr.GetBlockHeaderVerbose(ctx, hash)
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
	block := dcrutil.NewBlock(msgBlock)
	feeInfoBlock := txhelpers.FeeRateInfoBlock(block)
	if feeInfoBlock == nil {
		log.Error("FeeInfoBlock failed")
	}

	// Work/Stake difficulty
	diff := txhelpers.GetDifficultyRatio(header.Bits, t.netParams)
	sdiff := dcrutil.Amount(header.SBits).ToCoin()

	// Output
	blockdata := &apitypes.BlockDataBasic{
		Height:     height,
		Size:       uint32(msgBlock.SerializeSize()),
		Hash:       hash.String(),
		Difficulty: diff,
		StakeDiff:  sdiff,
		Time:       apitypes.TimeAPI{S: dbtypes.NewTimeDef(header.Timestamp)},
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
	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Time this function
	defer func(start time.Time) {
		log.Debugf("Collector.CollectHash() completed in %v", time.Since(start))
	}(time.Now())

	// 2 minutes for all the RPCs.
	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	defer cancel()

	// Info specific to the block hash
	blockDataBasic, feeInfoBlock, blockHeaderVerbose, extra, msgBlock, err :=
		t.CollectBlockInfo(hash)
	if err != nil {
		return nil, nil, err
	}

	// Number of peer connection to chain server
	numConn, err := t.dcrdChainSvr.GetConnectionCount(ctx)
	if err != nil {
		log.Warn("Unable to get connection count: ", err)
	}

	// Blockchain info (e.g. syncheight, verificationprogress, chainwork,
	// bestblockhash, initialblockdownload, maxblocksize, deployments, etc.).
	chainInfo, err := t.dcrdChainSvr.GetBlockChainInfo(ctx)
	if err != nil {
		log.Warn("Unable to get blockchain info: ", err)
	}
	// GetBlockChainInfo is only valid for for chain tip.
	if chainInfo.BestBlockHash != hash.String() {
		chainInfo = nil
	}

	// Output
	height := int64(blockDataBasic.Height)
	winSize := t.netParams.StakeDiffWindowSize
	blockdata := &BlockData{
		Header:           *blockHeaderVerbose,
		Connections:      int32(numConn),
		FeeInfo:          *feeInfoBlock,
		CurrentStakeDiff: chainjson.GetStakeDifficultyResult{CurrentStakeDifficulty: blockDataBasic.StakeDiff},
		EstStakeDiff:     chainjson.EstimateStakeDiffResult{},
		PoolInfo:         blockDataBasic.PoolInfo,
		ExtraInfo:        *extra,
		BlockchainInfo:   chainInfo,
		PriceWindowNum:   int(height / winSize),
		IdxBlockInWindow: int(height%winSize) + 1,
	}

	return blockdata, msgBlock, err
}

// Collect collects chain data at the current best block.
func (t *Collector) Collect() (*BlockData, *wire.MsgBlock, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Time this function.
	defer func(start time.Time) {
		log.Debugf("Collector.Collect() completed in %v", time.Since(start))
	}(time.Now())

	// 2 minutes for all the RPCs.
	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	defer cancel()

	// Pull and store relevant data about the blockchain (e.g. syncheight,
	// verificationprogress, chainwork, bestblockhash, initialblockdownload,
	// maxblocksize, deployments, etc.).
	blockchainInfo, err := t.dcrdChainSvr.GetBlockChainInfo(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get blockchain info: %v", err)
	}

	hash, err := chainhash.NewHashFromStr(blockchainInfo.BestBlockHash)
	if err != nil {
		return nil, nil,
			fmt.Errorf("invalid best block hash from getblockchaininfo: %v", err)
	}

	// Stake difficulty
	stakeDiff, err := t.dcrdChainSvr.GetStakeDifficulty(ctx)
	if err != nil {
		return nil, nil, err
	}

	// estimatestakediff
	estStakeDiff, err := t.dcrdChainSvr.EstimateStakeDiff(ctx, nil)
	if err != nil {
		log.Warn("estimatestakediff is broken: ", err)
		estStakeDiff = &chainjson.EstimateStakeDiffResult{}
	}

	// Info specific to the block hash
	blockDataBasic, feeInfoBlock, blockHeaderVerbose, extra, msgBlock, err :=
		t.CollectBlockInfo(hash)
	if err != nil {
		return nil, nil, err
	}

	// Number of peer connection to chain server
	numConn, err := t.dcrdChainSvr.GetConnectionCount(ctx)
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
		BlockchainInfo:   blockchainInfo,
		PoolInfo:         blockDataBasic.PoolInfo,
		PriceWindowNum:   int(height / winSize),
		IdxBlockInWindow: int(height%winSize) + 1,
	}

	return blockdata, msgBlock, err
}
