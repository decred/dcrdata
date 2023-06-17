// Copyright (c) 2015-2022, The Decred developers
// Copyright (c) 2013-2015 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txhelpers

import (
	"math"
	"sync"

	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
)

type netHeight struct {
	net         uint32
	dcp10height int64
	dcp12height int64
}

type subsidySumCache struct {
	sync.RWMutex
	m map[netHeight]int64
}

func (ssc *subsidySumCache) load(nh netHeight) (int64, bool) {
	ssc.RLock()
	defer ssc.RUnlock()
	total, ok := ssc.m[nh]
	return total, ok
}

func (ssc *subsidySumCache) store(nh netHeight, total int64) {
	ssc.Lock()
	defer ssc.Unlock()
	ssc.m[nh] = total
}

// ultimateSubsidies stores ultimate subsidy values computed by UltimateSubsidy.
var ultimateSubsidies = subsidySumCache{m: map[netHeight]int64{}}

// UltimateSubsidy computes the total subsidy over the entire subsidy
// distribution period of the network, given a known height at which the first
// and second subsidy split change go into effect (DCP0010 and DCP0012). If the
// height is unknown, provide -1 to perform the computation with the original
// subsidy split for all heights.
func UltimateSubsidy(params *chaincfg.Params, dcp0010Height, dcp0012Height int64) int64 {
	// Check previously computed ultimate subsidies.
	nh := netHeight{uint32(params.Net), dcp0010Height, dcp0012Height}
	result, ok := ultimateSubsidies.load(nh)
	if ok {
		return result
	}

	votesPerBlock := params.VotesPerBlock()
	stakeValidationHeight := params.StakeValidationBeginHeight()
	reductionInterval := params.SubsidyReductionIntervalBlocks()

	subsidyCache := networkSubsidyCache(params)
	subsidySum := func(height int64, ssv standalone.SubsidySplitVariant) int64 {
		work := subsidyCache.CalcWorkSubsidyV3(height, votesPerBlock, ssv)
		vote := subsidyCache.CalcStakeVoteSubsidyV3(height, ssv) * int64(votesPerBlock)
		// With voters set to max (votesPerBlock), treasury bool is unimportant.
		treasury := subsidyCache.CalcTreasurySubsidy(height, votesPerBlock, false)
		return work + vote + treasury
	}

	// Define details to account for partial intervals where the subsidy split
	// changes.
	subsidySplitChanges := map[int64]struct {
		activationHeight int64
		splitBefore      standalone.SubsidySplitVariant
		splitAfter       standalone.SubsidySplitVariant
	}{
		dcp0010Height / reductionInterval: {
			activationHeight: dcp0010Height,
			splitBefore:      standalone.SSVOriginal,
			splitAfter:       standalone.SSVDCP0010,
		},
		dcp0012Height / reductionInterval: {
			activationHeight: dcp0012Height,
			splitBefore:      standalone.SSVDCP0010,
			splitAfter:       standalone.SSVDCP0012,
		},
	}

	if dcp0010Height == -1 {
		dcp0010Height = math.MaxInt64
	}
	if dcp0012Height == -1 {
		dcp0012Height = math.MaxInt64
	}

	totalSubsidy := params.BlockOneSubsidy()
	for i := int64(0); ; i++ {
		// The first interval contains a few special cases:
		// 1) Block 0 does not produce any subsidy
		// 2) Block 1 consists of a special initial coin distribution
		// 3) Votes do not produce subsidy until voting begins
		if i == 0 {
			// Account for the block up to the point voting begins ignoring the
			// first two special blocks.
			subsidyCalcHeight := int64(2)
			nonVotingBlocks := stakeValidationHeight - subsidyCalcHeight
			totalSubsidy += subsidySum(subsidyCalcHeight, standalone.SSVOriginal) * nonVotingBlocks

			// Account for the blocks remaining in the interval once voting
			// begins.
			subsidyCalcHeight = stakeValidationHeight
			votingBlocks := reductionInterval - subsidyCalcHeight
			totalSubsidy += subsidySum(subsidyCalcHeight, standalone.SSVOriginal) * votingBlocks
			continue
		}

		// Account for partial intervals with subsidy split changes.
		subsidyCalcHeight := i * reductionInterval
		if change, ok := subsidySplitChanges[i]; ok {
			// Account for the blocks up to the point the subsidy split changed.
			preChangeBlocks := change.activationHeight - subsidyCalcHeight
			totalSubsidy += subsidySum(subsidyCalcHeight, change.splitBefore) *
				preChangeBlocks

			// Account for the blocks remaining in the interval after the
			// subsidy split changed.
			subsidyCalcHeight = change.activationHeight
			remainingBlocks := reductionInterval - preChangeBlocks
			totalSubsidy += subsidySum(subsidyCalcHeight, change.splitAfter) *
				remainingBlocks
			continue
		}

		// Account for the all other reduction intervals until all subsidy has
		// been produced including partial intervals with subsidy split changes.
		splitVariant := standalone.SSVOriginal
		switch {
		case subsidyCalcHeight >= dcp0012Height:
			splitVariant = standalone.SSVDCP0012
		case subsidyCalcHeight >= dcp0010Height:
			splitVariant = standalone.SSVDCP0010
		}
		sum := subsidySum(subsidyCalcHeight, splitVariant)
		if sum == 0 {
			break
		}
		totalSubsidy += sum * reductionInterval
	}

	// Update the ultimate subsidy store.
	ultimateSubsidies.store(nh, totalSubsidy)

	return totalSubsidy
}

var scs = struct {
	sync.Mutex
	m map[wire.CurrencyNet]*standalone.SubsidyCache
}{
	m: map[wire.CurrencyNet]*standalone.SubsidyCache{},
}

func networkSubsidyCache(p *chaincfg.Params) *standalone.SubsidyCache {
	scs.Lock()
	defer scs.Unlock()
	sc, ok := scs.m[p.Net]
	if ok {
		return sc
	}
	sc = standalone.NewSubsidyCache(p)
	scs.m[p.Net] = sc
	return sc
}

// RewardsAtBlock computes the PoW, PoS (per vote), and project fund subsidies
// at for the specified block index, assuming a certain number of votes. The
// stake reward is for a single vote. The total reward for the block is thus
// work + stake * votes + tax.
func RewardsAtBlock(blockIdx int64, votes uint16, p *chaincfg.Params, ssv standalone.SubsidySplitVariant) (work, stake, tax int64) {
	subsidyCache := networkSubsidyCache(p)
	work = subsidyCache.CalcWorkSubsidyV3(blockIdx, votes, ssv)
	stake = subsidyCache.CalcStakeVoteSubsidyV3(blockIdx, ssv)
	treasuryActive := IsTreasuryActive(p.Net, blockIdx)
	tax = subsidyCache.CalcTreasurySubsidy(blockIdx, votes, treasuryActive)
	return
}
