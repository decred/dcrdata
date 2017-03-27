package mempool

import (
	"sync"
	"time"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/dcrjson"
)

type MempoolDataCache struct {
	sync.RWMutex
	height                  uint32
	timestamp               time.Time
	numTickets              uint32
	ticketFeeInfo           dcrjson.FeeInfoMempool
	allFees                 []float64
	allFeeRates             []float64
	lowestMineableByFeeRate float64
	allTicketsDetails       TicketsDetails
}

func (c *MempoolDataCache) StoreMPData(data *MempoolData, timestamp time.Time) error {
	c.Lock()
	defer c.Unlock()

	c.height = data.Height
	c.timestamp = timestamp
	c.numTickets = data.NumTickets
	c.ticketFeeInfo = data.Ticketfees.FeeInfoMempool
	c.allFees = data.MinableFees.allFees
	c.allFeeRates = data.MinableFees.allFeeRates
	c.lowestMineableByFeeRate = data.MinableFees.lowestMineableFee
	c.allTicketsDetails = data.AllTicketsDetails

	return nil
}

func (c *MempoolDataCache) GetHeight() uint32 {
	return c.height
}

func (c *MempoolDataCache) GetNumTickets() (uint32, uint32) {
	c.RLock()
	defer c.RUnlock()
	return c.height, c.numTickets
}

func (c *MempoolDataCache) GetFeeInfo() (uint32, dcrjson.FeeInfoMempool) {
	c.RLock()
	defer c.RUnlock()
	return c.height, c.ticketFeeInfo
}

func (c *MempoolDataCache) GetFeeInfoExtra() (uint32, *apitypes.MempoolTicketFeeInfo) {
	c.RLock()
	defer c.RUnlock()
	feeInfo := apitypes.MempoolTicketFeeInfo{
		Height:         c.height,
		Time:           c.timestamp.Unix(),
		FeeInfoMempool: c.ticketFeeInfo,
		LowestMineable: c.lowestMineableByFeeRate,
	}
	return c.height, &feeInfo
}

func (c *MempoolDataCache) GetFees(N int) (uint32, int, []float64) {
	c.RLock()
	defer c.RUnlock()

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

func (c *MempoolDataCache) GetFeeRates(N int) (uint32, int64, int, []float64) {
	c.RLock()
	defer c.RUnlock()

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

func (c *MempoolDataCache) GetTicketsDetails(N int) (uint32, int64, int, TicketsDetails) {
	c.RLock()
	defer c.RUnlock()

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
