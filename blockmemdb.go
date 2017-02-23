package main

// BlockDataToMemdb satisfies BlockDataSaver interface by implementing the
// Store(data *blockData) method.  It saves the data in memory with a map. All
// the data is destroyed on exit.  Something like this is needed that uses a
// durable database (e.g. MySQL).

import (
	"sync"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/dcrjson"
)

type BlockDataToMemdb struct {
	mtx             *sync.Mutex
	Height          int
	blockDataMap    map[int]*blockData
	blockSummaryMap map[int]*apitypes.BlockDataBasic
}

func NewBlockDataToMemdb(m ...*sync.Mutex) *BlockDataToMemdb {
	if len(m) > 1 {
		panic("Too many inputs.")
	}
	saver := &BlockDataToMemdb{
		Height:          -1,
		blockDataMap:    make(map[int]*blockData),
		blockSummaryMap: make(map[int]*apitypes.BlockDataBasic),
	}
	if len(m) > 0 {
		saver.mtx = m[0]
	}
	return saver
}

// Store writes blockData to memdb
func (s *BlockDataToMemdb) Store(data *blockData) error {
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	s.Height = int(data.header.Height)

	// save data to slice in memory
	s.blockDataMap[s.Height] = data

	blockSummary := apitypes.BlockDataBasic{
		Height:         uint32(s.Height),
		Size:           data.header.Size,
		Difficulty:     data.header.Difficulty,
		StakeDiff:      data.header.SBits,
		Time:           data.header.Time,
		TicketPoolInfo: data.poolinfo,
	}
	s.blockSummaryMap[s.Height] = &blockSummary

	return nil
}

func (s *BlockDataToMemdb) Get(idx int) *blockData {
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
	blockData, ok := s.blockDataMap[idx]
	if !ok {
		return nil
	}
	return &blockData.header
}

func (s *BlockDataToMemdb) GetFeeInfo(idx int) *dcrjson.FeeInfoBlock {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	blockData, ok := s.blockDataMap[idx]
	if !ok {
		return nil
	}
	return &blockData.feeinfo
}

func (s *BlockDataToMemdb) GetStakeDiffEstimate(idx int) *dcrjson.EstimateStakeDiffResult {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	blockData, ok := s.blockDataMap[idx]
	if !ok {
		return nil
	}
	return &blockData.eststakediff
}

func (s *BlockDataToMemdb) GetStakeInfoExtended(idx int) *apitypes.StakeInfoExtended {
	if idx < 0 {
		return nil
	}
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}
	blockData, ok := s.blockDataMap[idx]
	if !ok {
		return nil
	}

	stakeinfo := &apitypes.StakeInfoExtended{
		Feeinfo: blockData.feeinfo,
		StakeDiff: apitypes.StakeDiff{dcrjson.GetStakeDifficultyResult{
			blockData.currentstakediff.CurrentStakeDifficulty,
			blockData.currentstakediff.NextStakeDifficulty},
			blockData.eststakediff},
		PriceWindowNum:   blockData.priceWindowNum,
		IdxBlockInWindow: blockData.idxBlockInWindow,
		Poolinfo:         blockData.poolinfo,
	}

	return stakeinfo
}

func (s *BlockDataToMemdb) GetBestBlock() *blockData {
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
