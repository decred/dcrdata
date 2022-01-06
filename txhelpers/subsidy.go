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
// distribution period of the network, given a known height at which the subsidy
// split change goes into effect (DCP0010). If the height is unknown, provide -1
// to perform the computation with the original subsidy split for all heights.
func UltimateSubsidy(params *chaincfg.Params, subsidySplitChangeHeight int64) int64 {
	// Check previously computed ultimate subsidies.
	nh := netHeight{uint32(params.Net), subsidySplitChangeHeight}
	result, ok := ultimateSubsidies.load(nh)
	if ok {
		return result
	}

	if subsidySplitChangeHeight == -1 {
		subsidySplitChangeHeight = math.MaxInt64
	}

	votesPerBlock := params.VotesPerBlock()
	stakeValidationHeight := params.StakeValidationBeginHeight()
	reductionInterval := params.SubsidyReductionIntervalBlocks()

	subsidyCache := networkSubsidyCache(params)
	subsidySum := func(height int64) int64 {
		useDCP0010 := height >= subsidySplitChangeHeight
		work := subsidyCache.CalcWorkSubsidyV2(height, votesPerBlock, useDCP0010)
		vote := subsidyCache.CalcStakeVoteSubsidyV2(height, useDCP0010) * int64(votesPerBlock)
		// With voters set to max (votesPerBlock), treasury bool is unimportant.
		treasury := subsidyCache.CalcTreasurySubsidy(height, votesPerBlock, false)
		return work + vote + treasury
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
			totalSubsidy += subsidySum(subsidyCalcHeight) * nonVotingBlocks

			// Account for the blocks remaining in the interval once voting
			// begins.
			subsidyCalcHeight = stakeValidationHeight
			votingBlocks := reductionInterval - subsidyCalcHeight
			totalSubsidy += subsidySum(subsidyCalcHeight) * votingBlocks
			continue
		}

		// Account for the all other reduction intervals until all subsidy has
		// been produced.
		subsidyCalcHeight := i * reductionInterval
		sum := subsidySum(subsidyCalcHeight)
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
func RewardsAtBlock(blockIdx int64, votes uint16, p *chaincfg.Params, useDCP0010 bool) (work, stake, tax int64) {
	subsidyCache := networkSubsidyCache(p)
	work = subsidyCache.CalcWorkSubsidyV2(blockIdx, votes, useDCP0010)
	stake = subsidyCache.CalcStakeVoteSubsidyV2(blockIdx, useDCP0010)
	treasuryActive := IsTreasuryActive(p.Net, blockIdx)
	tax = subsidyCache.CalcTreasurySubsidy(blockIdx, votes, treasuryActive)
	return
}
