package main

// BlockDataToMemdb satisfies BlockDataSaver interface by implementing the
// Store(data *blockdata.BlockData) method.  It saves the data in memory with a map. All
// the data is destroyed on exit.  Something like this is needed that uses a
// durable database (e.g. MySQL).

import (
	"sync"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/dcrjson"
)

type BlockDataToMemdb struct {
	mtx             *sync.Mutex
	Height          int
	blockDataMap    map[int]*blockdata.BlockData
	blockSummaryMap map[int]*apitypes.BlockDataBasic
}

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
func (s *BlockDataToMemdb) Store(data *blockdata.BlockData) error {
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

func (s *BlockDataToMemdb) GetBestBlock() *blockdata.BlockData {
	return s.Get(s.Height)
}

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

func (s *BlockDataToMemdb) GetBestBlockSummary() *apitypes.BlockDataBasic {
	return s.GetSummary(s.Height)
}
