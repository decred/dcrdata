// Copyright (c) 2015-2021, The Decred developers
// Copyright (c) 2013-2015 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txhelpers

import (
	"sync"

	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/v3"
)

// ultimateSubsidies stores ultimate subsidy values computed by UltimateSubsidy.
var ultimateSubsidies sync.Map

// UltimateSubsidy computes the total subsidy over the entire subsidy
// distribution period of the network.
func UltimateSubsidy(params *chaincfg.Params) int64 {
	// Check previously computed ultimate subsidies.
	result, ok := ultimateSubsidies.Load(params)
	if ok {
		return result.(int64)
	}

	votesPerBlock := params.VotesPerBlock()
	stakeValidationHeight := params.StakeValidationBeginHeight()
	reductionInterval := params.SubsidyReductionIntervalBlocks()

	subsidyCache := standalone.NewSubsidyCache(params)
	subsidySum := func(height int64) int64 {
		work := subsidyCache.CalcWorkSubsidy(height, votesPerBlock)
		vote := subsidyCache.CalcStakeVoteSubsidy(height) * int64(votesPerBlock)
		treasury := subsidyCache.CalcTreasurySubsidy(height, votesPerBlock, false) // !!!!!!!! what
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
	ultimateSubsidies.Store(params, totalSubsidy)

	return totalSubsidy
}

// RewardsAtBlock computes the PoW, PoS (per vote), and project fund subsidies
// at for the specified block index, assuming a certain number of votes.
func RewardsAtBlock(blockIdx int64, votes uint16, p *chaincfg.Params) (work, stake, tax int64) {
	subsidyCache := standalone.NewSubsidyCache(p)
	work = subsidyCache.CalcWorkSubsidy(blockIdx, votes)
	stake = subsidyCache.CalcStakeVoteSubsidy(blockIdx)
	tax = subsidyCache.CalcTreasurySubsidy(blockIdx, votes, false) //!!!! what?
	return
}
