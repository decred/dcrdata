package explorer

import (
	"testing"

	"github.com/decred/dcrd/chaincfg"
)

func TestTestNet3Name(t *testing.T) {
	netName := netName(&chaincfg.TestNet3Params)
	if netName != "Testnet" {
		t.Errorf(`Net name not "Testnet": %s`, netName)
	}
}

func TestMainNetName(t *testing.T) {
	netName := netName(&chaincfg.MainNetParams)
	if netName != "Mainnet" {
		t.Errorf(`Net name not "Mainnet": %s`, netName)
	}
}

func TestSimNetName(t *testing.T) {
	netName := netName(&chaincfg.SimNetParams)
	if netName != "Simnet" {
		t.Errorf(`Net name not "Simnet": %s`, netName)
	}
}
