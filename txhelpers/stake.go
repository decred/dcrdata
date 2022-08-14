package txhelpers

import (
	"math"

	"github.com/decred/dcrd/chaincfg/v3"
)

// CalcMeanVotingBlocks computes the average number of blocks a ticket will be
// live before voting. The expected block (aka mean) of the probability
// distribution is given by:
//
//	sum(B * P(B)), B=1 to 40960
//
// Where B is the block number and P(B) is the probability of voting at
// block B.  For more information see:
// https://github.com/decred/dcrdata/issues/471#issuecomment-390063025
func CalcMeanVotingBlocks(params *chaincfg.Params) int64 {
	logPoolSizeM1 := math.Log(float64(params.TicketPoolSize) - 1)
	logPoolSize := math.Log(float64(params.TicketPoolSize))
	var v float64
	for i := float64(1); i <= float64(params.TicketExpiry); i++ {
		v += math.Exp(math.Log(i) + (i-1)*logPoolSizeM1 - i*logPoolSize)
	}
	return int64(v)
}
