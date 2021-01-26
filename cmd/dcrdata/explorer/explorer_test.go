package explorer

import (
	// Imports for TestThreeSigFigs
	// "fmt"
	// "math"
	// "math/rand"
	// "time"

	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
)

func TestTestNet3Name(t *testing.T) {
	netName := netName(chaincfg.TestNet3Params())
	if netName != testnetNetName {
		t.Errorf(`Net name not "%s": %s`, testnetNetName, netName)
	}
}

func TestMainNetName(t *testing.T) {
	netName := netName(chaincfg.MainNetParams())
	if netName != "Mainnet" {
		t.Errorf(`Net name not "Mainnet": %s`, netName)
	}
}

func TestSimNetName(t *testing.T) {
	netName := netName(chaincfg.SimNetParams())
	if netName != "Simnet" {
		t.Errorf(`Net name not "Simnet": %s`, netName)
	}
}

// func TestThreeSigFigs(t *testing.T) {
// 	source := rand.NewSource(time.Now().Unix())
// 	generator := rand.New(source)
// 	for i := 0; i < 8; i++ {
// 		flt := generator.Float64()
// 		flt = flt * math.Pow10(-i)
// 		fmt.Printf("%.8f -> %s\n", flt, threeSigFigs(flt))
// 	}
// 	for i := 0; i < 13; i++ {
// 		flt := generator.Float64()
// 		flt = flt * math.Pow10(i)
// 		fmt.Printf("%.8f -> %s\n", flt, threeSigFigs(flt))
// 	}
// }
