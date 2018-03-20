package main

// BlockDataToMemdb satisfies BlockDataSaver interface by implementing the
// Store(data *blockdata.BlockData) method.  It saves the data in memory with a map. All
// the data is destroyed on exit.  Something like this is needed that uses a
// durable database (e.g. MySQL).

import (
	"sync"

	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/blockdata"
)

// BlockDataToMemdb models the block data and block data basic as maps
type BlockDataToMemdb struct {
	mtx             *sync.Mutex
	Height          int
	blockDataMap    map[int]*blockdata.BlockData
	blockSummaryMap map[int]*apitypes.BlockDataBasic
}

// NewBlockDataToMemdb returns a new BlockDataToMemdb
func NewBlockDataToMemdb(m ...*sync.Mutex) *BlockDataToMemdb {
	if len(m) > 1 {
		panic("Too many inputs.")
	}
	saver := &BlockDataToMemdb{
		Height:          -1,
		blockDataMap:    make(map[int]*blockdata.BlockData),
		blockSummaryMap: make(map[int]*apitypes.BlockDataBasic),
	}
	if len(m) > 0 {
		saver.mtx = m[0]
	}
	return saver
}

// Store writes blockdata.BlockData to memdb
func (s *BlockDataToMemdb) Store(data *blockdata.BlockData, _ *wire.MsgBlock) error {
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	s.Height = int(data.Header.Height)

	// save data to slice in memory
	s.blockDataMap[s.Height] = data

	blockSummary := data.ToBlockSummary()
	s.blockSummaryMap[s.Height] = &blockSummary

	return nil
}

// GetHeight returns the blockdata height
func (s *BlockDataToMemdb) GetHeight() int {
	return s.Height
}

// Get returns blockdata for block idx
func (s *BlockDataToMemdb) Get(idx int) *blockdata.BlockData {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	return s.blockDataMap[idx]
}

// GetHeader returns the block header for block idx
func (s *BlockDataToMemdb) GetHeader(idx int) *dcrjson.GetBlockHeaderVerboseResult {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	blockdata, ok := s.blockDataMap[idx]
	if !ok {
		return nil
	}
	return &blockdata.Header
}

// GetFeeInfo returns the fee info for block idx
func (s *BlockDataToMemdb) GetFeeInfo(idx int) *dcrjson.FeeInfoBlock {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	blockdata, ok := s.blockDataMap[idx]
	if !ok {
		return nil
	}
	return &blockdata.FeeInfo
}

// GetStakeDiffEstimate returns the stake difficulty estimates for block idx
func (s *BlockDataToMemdb) GetStakeDiffEstimate(idx int) *dcrjson.EstimateStakeDiffResult {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	blockdata, ok := s.blockDataMap[idx]
	if !ok {
		return nil
	}
	return &blockdata.EstStakeDiff
}

// GetStakeInfoExtended returns the stake info for block idx
func (s *BlockDataToMemdb) GetStakeInfoExtended(idx int) *apitypes.StakeInfoExtended {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	blockdata, ok := s.blockDataMap[idx]
	if !ok {
		return nil
	}

	stakeinfo := blockdata.ToStakeInfoExtended()
	return &stakeinfo
}

// GetBestBlock returns the best block
func (s *BlockDataToMemdb) GetBestBlock() *blockdata.BlockData {
	return s.Get(s.Height)
}

// GetSummary returns the block data summary for block idx
func (s *BlockDataToMemdb) GetSummary(idx int) *apitypes.BlockDataBasic {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	return s.blockSummaryMap[idx]
}

// GetBestBlockSummary returns the block data summary for the best block
func (s *BlockDataToMemdb) GetBestBlockSummary() *apitypes.BlockDataBasic {
	return s.GetSummary(s.Height)
}
