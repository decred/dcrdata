package txhelpers

import (
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
)

type networkRewardPeriod struct {
	params                *chaincfg.Params
	MeanVotingBlocks      int64
	MeanLockedBlocks      int64
	MeanLockedNanoseconds int64
}

var networkRewardPeriods = []networkRewardPeriod{
	{
		chaincfg.MainNetParams(),
		7860,
		8372,
		2511600000000000,
	},
	{
		chaincfg.TestNet3Params(),
		1006,
		1038,
		124560000000000,
	},
	{
		chaincfg.SimNetParams(),
		62,
		94,
		94000000000,
	},
}

// TestRewardPeriods verifies calcMeanVotingBlocks works for each network.
func TestRewardPeriods(t *testing.T) {
	rewardPeriod := func(params *chaincfg.Params) networkRewardPeriod {
		MeanVotingBlocks := CalcMeanVotingBlocks(params)
		maturity := int64(params.TicketMaturity) + int64(params.CoinbaseMaturity)
		return networkRewardPeriod{
			params:                params,
			MeanVotingBlocks:      MeanVotingBlocks,
			MeanLockedBlocks:      MeanVotingBlocks + maturity,
			MeanLockedNanoseconds: params.TargetTimePerBlock.Nanoseconds() * (MeanVotingBlocks + maturity),
		}
	}

	for i := range networkRewardPeriods {
		r0 := &networkRewardPeriods[i]
		r := rewardPeriod(r0.params)

		if r.MeanVotingBlocks != r0.MeanVotingBlocks {
			t.Errorf("MeanVotingBlocks: got %d, expected %d", r.MeanVotingBlocks, r0.MeanVotingBlocks)
		}

		if r.MeanLockedBlocks != r0.MeanLockedBlocks {
			t.Errorf("MeanLockedBlocks: got %d, expected %d", r.MeanLockedBlocks, r0.MeanLockedBlocks)
		}

		if r.MeanLockedNanoseconds != r0.MeanLockedNanoseconds {
			t.Errorf("MeanLockedNanoseconds: got %d, expected %d", r.MeanLockedNanoseconds, r0.MeanLockedNanoseconds)
		}

		lockedDuration := time.Duration(r.MeanLockedNanoseconds)
		t.Logf("%s expected locked time: %v (%.08f days)", r.params.Name,
			lockedDuration, lockedDuration.Hours()/24)
	}
}
