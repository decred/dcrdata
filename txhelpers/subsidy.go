// Copyright (c) 2015-2019 The Decred developers
// Copyright (c) 2013-2015 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txhelpers

import (
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg/v2"
)

// ultimateSubsidies stores ultimate subsidy values computed by UltimateSubsidy.
var ultimateSubsidies = map[*chaincfg.Params]int64{}

// UltimateSubsidy computes the total subsidy over the entire subsidy
// distribution period of the network.
func UltimateSubsidy(params *chaincfg.Params) int64 {
	// Check previously computed ultimate subsidies.
	totalSubsidy, ok := ultimateSubsidies[params]
	if ok {
		return totalSubsidy
	}

	paramsV1 := ParamsV2ToV1(params)
	if paramsV1 == nil {
		return 0
	}

	subsidyCache := blockchain.NewSubsidyCache(0, paramsV1)

	totalSubsidy = params.BlockOneSubsidy()
	for i := int64(0); ; i++ {
		// Genesis block or first block.
		if i <= 1 {
			continue
		}

		if i%params.SubsidyReductionInterval == 0 {
			numBlocks := params.SubsidyReductionInterval
			// First reduction internal, which is reduction interval - 2 to skip
			// the genesis block and block one.
			if i == params.SubsidyReductionInterval {
				numBlocks -= 2
			}
			height := i - numBlocks

			work := blockchain.CalcBlockWorkSubsidy(subsidyCache, height,
				params.TicketsPerBlock, paramsV1)
			stake := blockchain.CalcStakeVoteSubsidy(subsidyCache, height,
				paramsV1) * int64(params.TicketsPerBlock)
			tax := blockchain.CalcBlockTaxSubsidy(subsidyCache, height,
				params.TicketsPerBlock, paramsV1)
			if (work + stake + tax) == 0 {
				break // all done
			}
			totalSubsidy += ((work + stake + tax) * numBlocks)

			// First reduction internal -- subtract the stake subsidy for blocks
			// before the staking system is enabled.
			if i == params.SubsidyReductionInterval {
				totalSubsidy -= stake * (params.StakeValidationHeight - 2)
			}
		}
	}

	// Update the ultimate subsidy store.
	ultimateSubsidies[params] = totalSubsidy

	return totalSubsidy
}

// RewardsAtBlock computes the PoW, PoS (per vote), and project fund subsidies
// at for the specified block index, assuming a certain number of votes.
func RewardsAtBlock(blockIdx int64, votes uint16, p *chaincfg.Params) (work, stake, tax int64) {
	pV1 := ParamsV2ToV1(p)
	if pV1 == nil {
		return
	}
	subsidyCache := blockchain.NewSubsidyCache(0, pV1)
	work = blockchain.CalcBlockWorkSubsidy(subsidyCache, blockIdx, votes, pV1)
	stake = blockchain.CalcStakeVoteSubsidy(subsidyCache, blockIdx, pV1)
	tax = blockchain.CalcBlockTaxSubsidy(subsidyCache, blockIdx, votes, pV1)
	return
}
