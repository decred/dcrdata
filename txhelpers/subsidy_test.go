// Copyright (c) 2018-2022, The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txhelpers

import (
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
)

func TestUltimateSubsidy(t *testing.T) {
	// Mainnet
	wantMainnetSubsidy := int64(2099999999800912)
	totalSubsidy := UltimateSubsidy(chaincfg.MainNetParams(), -1, -1)

	if totalSubsidy != wantMainnetSubsidy {
		t.Errorf("Bad total subsidy; want %d, got %d",
			wantMainnetSubsidy, totalSubsidy)
	}

	// verify cache
	totalSubsidy2 := UltimateSubsidy(chaincfg.MainNetParams(), -1, -1)
	if totalSubsidy != totalSubsidy2 {
		t.Errorf("Bad total subsidy; want %d, got %d",
			totalSubsidy, totalSubsidy2)
	}

	// Mainnet with dcp0010 activating at 638976
	wantMainnetSubsidy = int64(2100000000015952)
	totalSubsidyDCP0010 := UltimateSubsidy(chaincfg.MainNetParams(), 638976, -1)

	if totalSubsidyDCP0010 != wantMainnetSubsidy {
		t.Fatalf("Bad total subsidy; want %d, got %d",
			wantMainnetSubsidy, totalSubsidyDCP0010)
	}

	// Testnet
	wantTestnetSubsidy := int64(526540305161472)
	totalTNSubsidy := UltimateSubsidy(chaincfg.TestNet3Params(), -1, -1)

	if totalTNSubsidy != wantTestnetSubsidy {
		t.Errorf("Bad total subsidy; want %d, got %d",
			wantTestnetSubsidy, totalTNSubsidy)
	}

	// verify cache
	totalTNSubsidy2 := UltimateSubsidy(chaincfg.TestNet3Params(), -1, -1)
	if totalTNSubsidy != totalTNSubsidy2 {
		t.Errorf("Bad total subsidy; want %d, got %d",
			totalTNSubsidy, totalTNSubsidy2)
	}

	// re-verify mainnet cache
	totalSubsidy3 := UltimateSubsidy(chaincfg.MainNetParams(), -1, -1)
	if totalSubsidy != totalSubsidy3 {
		t.Errorf("Bad total subsidy; want %d, got %d",
			totalSubsidy, totalSubsidy3)
	}

	// re-verify mainnet cache (dcp0010)
	totalSubsidy4 := UltimateSubsidy(chaincfg.MainNetParams(), 638976, -1)
	if totalSubsidyDCP0010 != totalSubsidy4 {
		t.Errorf("Bad total subsidy; want %d, got %d",
			totalSubsidyDCP0010, totalSubsidy4)
	}

	// Mainnet with dcp0010 activating at 657280, and dcp0012 at 794368.
	wantMainnetSubsidy = int64(2099999998394320)
	totalSubsidyDCP0010and12 := UltimateSubsidy(chaincfg.MainNetParams(), 657280, 794368)

	if totalSubsidyDCP0010and12 != wantMainnetSubsidy {
		t.Fatalf("Bad total subsidy; want %d, got %d",
			wantMainnetSubsidy, totalSubsidyDCP0010and12)
	}
}

func BenchmarkUltimateSubsidy(b *testing.B) {
	// warm up
	totalSubsidy := UltimateSubsidy(chaincfg.MainNetParams(), -1, -1)
	// verify cache
	totalSubsidy2 := UltimateSubsidy(chaincfg.MainNetParams(), -1, -1)
	if totalSubsidy != totalSubsidy2 {
		b.Errorf("Bad total subsidy; want %d, got %d",
			totalSubsidy, totalSubsidy2)
	}

	for i := 0; i < b.N; i++ {
		totalSubsidy = UltimateSubsidy(chaincfg.MainNetParams(), -1, -1)
	}

	if totalSubsidy != totalSubsidy2 {
		b.Errorf("Bad total subsidy; want %d, got %d",
			totalSubsidy, totalSubsidy2)
	}
}
