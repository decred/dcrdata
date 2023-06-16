// Copyright (c) 2019-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package mempool

import (
	"sync"
	"time"

	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"

	apitypes "github.com/decred/dcrdata/v8/api/types"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	exptypes "github.com/decred/dcrdata/v8/explorer/types"
)

// DataCache models the basic data for the mempool cache.
type DataCache struct {
	mtx sync.RWMutex

	// Height and hash of best block at time of data collection
	height uint32
	hash   string

	// Time of mempool data collection
	timestamp time.Time

	// All transactions
	txns []exptypes.MempoolTx

	// Stake-related data
	numTickets              uint32
	ticketFeeInfo           chainjson.FeeInfoMempool
	allFees                 []float64
	allFeeRates             []float64
	lowestMineableByFeeRate float64
	allTicketsDetails       TicketsDetails
	stakeDiff               int64
}

// StoreMPData stores info from data in the mempool cache. It is advisable to
// pass a copy of the []types.MempoolTx so that it may be modified (e.g. sorted)
// without affecting other MempoolDataSavers.
func (c *DataCache) StoreMPData(stakeData *StakeData, txsCopy []exptypes.MempoolTx, _ *exptypes.MempoolInfo) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.height = uint32(stakeData.LatestBlock.Height)
	c.hash = stakeData.LatestBlock.Hash.String()
	c.timestamp = stakeData.Time

	c.txns = txsCopy

	c.numTickets = stakeData.NumTickets
	c.ticketFeeInfo = stakeData.Ticketfees.FeeInfoMempool
	c.allFees = stakeData.MinableFees.allFees
	c.allFeeRates = stakeData.MinableFees.allFeeRates
	c.lowestMineableByFeeRate = stakeData.MinableFees.lowestMineableFee
	c.allTicketsDetails = stakeData.AllTicketsDetails
	sbits, _ := dcrutil.NewAmount(stakeData.StakeDiff) // 0 if err!=nil
	c.stakeDiff = int64(sbits)
}

// GetHeight returns the mempool height
func (c *DataCache) GetHeight() uint32 {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.height
}

// SBits returns the current stake difficulty, and the current best block
// height.
func (c *DataCache) SBits() (int64, uint32) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.stakeDiff, c.height
}

// GetNumTickets returns the mempool height and number of tickets
func (c *DataCache) GetNumTickets() (uint32, uint32) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.height, c.numTickets
}

// GetFeeInfo returns the mempool height and basic fee info
func (c *DataCache) GetFeeInfo() (uint32, chainjson.FeeInfoMempool) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.height, c.ticketFeeInfo
}

// GetFeeInfoExtra returns the mempool height and detailed fee info
func (c *DataCache) GetFeeInfoExtra() (uint32, *apitypes.MempoolTicketFeeInfo) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	feeInfo := apitypes.MempoolTicketFeeInfo{
		Height:         c.height,
		Time:           c.timestamp.Unix(),
		FeeInfoMempool: c.ticketFeeInfo,
		LowestMineable: c.lowestMineableByFeeRate,
	}
	return c.height, &feeInfo
}

// GetFees returns the mempool height number of fees and an array of the fields
func (c *DataCache) GetFees(N int) (uint32, int, []float64) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	numFees := len(c.allFees)

	//var fees []float64
	fees := []float64{} // for consistency
	if N == 0 {
		return c.height, numFees, fees
	}

	if N < 0 || N >= numFees {
		fees = make([]float64, numFees)
		copy(fees, c.allFees)
	} else if N < numFees {
		// fees are in ascending order, take from end of slice
		smallestFeeInd := numFees - N
		fees = make([]float64, N)
		copy(fees, c.allFees[smallestFeeInd:])
	}

	return c.height, numFees, fees
}

// GetFeeRates returns the mempool height, time, number of fees and an array of
// fee rates
func (c *DataCache) GetFeeRates(N int) (uint32, int64, int, []float64) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	numFees := len(c.allFeeRates)

	//var fees []float64
	fees := []float64{}
	if N == 0 {
		return c.height, c.timestamp.Unix(), numFees, fees
	}

	if N < 0 || N >= numFees {
		fees = make([]float64, numFees)
		copy(fees, c.allFeeRates)
	} else if N < numFees {
		// fees are in ascending order, take from end of slice
		smallestFeeInd := numFees - N
		fees = make([]float64, N)
		copy(fees, c.allFeeRates[smallestFeeInd:])
	}

	return c.height, c.timestamp.Unix(), numFees, fees
}

// GetTicketsDetails returns the mempool height, time, number of tickets and the
// ticket details
func (c *DataCache) GetTicketsDetails(N int) (uint32, int64, int, TicketsDetails) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	numSSTx := len(c.allTicketsDetails)

	//var details TicketsDetails
	details := TicketsDetails{}
	if N == 0 {
		return c.height, c.timestamp.Unix(), numSSTx, details
	}
	if N < 0 || N >= numSSTx {
		details = make(TicketsDetails, numSSTx)
		copy(details, c.allTicketsDetails)
	} else if N < numSSTx {
		// fees are in ascending order, take from end of slice
		smallestFeeInd := numSSTx - N
		details = make(TicketsDetails, N)
		copy(details, c.allTicketsDetails[smallestFeeInd:])
	}

	return c.height, c.timestamp.Unix(), numSSTx, details
}

// GetTicketPriceCountTime gathers the nominal info for mempool tickets.
func (c *DataCache) GetTicketPriceCountTime(feeAvgLength int) *apitypes.PriceCountTime {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	numFees := len(c.allFees)
	if numFees < feeAvgLength {
		feeAvgLength = numFees
	}
	var feeAvg float64
	for i := 0; i < feeAvgLength; i++ {
		feeAvg += c.allFees[numFees-i-1]
	}
	if feeAvgLength > 0 {
		feeAvg /= float64(feeAvgLength)
	}

	return &apitypes.PriceCountTime{
		Price: dcrutil.Amount(c.stakeDiff).ToCoin() + feeAvg,
		Count: numFees,
		Time:  dbtypes.NewTimeDef(c.timestamp),
	}
}
