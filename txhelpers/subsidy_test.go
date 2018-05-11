package txhelpers

import (
	"testing"

	"github.com/decred/dcrd/chaincfg"
)

func TestBlockSubsidy(t *testing.T) {
	totalSubsidy := UltimateSubsidy(&chaincfg.MainNetParams)

	if totalSubsidy != 2099999999800912 {
		t.Errorf("Bad total subsidy; want 2099999999800912, got %v", totalSubsidy)
	}
}
