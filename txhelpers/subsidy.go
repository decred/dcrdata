// Copyright (c) 2015-2018 The Decred developers
// Copyright (c) 2013-2015 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txhelpers

import (
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
)

// UltimateSubsidy computes the total subsidy over the entire subsidy
// distribution period of the network.
func UltimateSubsidy(params *chaincfg.Params) int64 {
	subsidyCache := blockchain.NewSubsidyCache(0, params)

	totalSubsidy := params.BlockOneSubsidy()
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
				params.TicketsPerBlock, params)
			stake := blockchain.CalcStakeVoteSubsidy(subsidyCache, height,
				params) * int64(params.TicketsPerBlock)
			tax := blockchain.CalcBlockTaxSubsidy(subsidyCache, height,
				params.TicketsPerBlock, params)
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
	return totalSubsidy
}
