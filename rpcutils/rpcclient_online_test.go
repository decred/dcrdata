// +build testnetRPC

package rpcutils

import (
	"testing"
)

const (
	nodeUser = "dcrd"
	nodePass = "dcrd"
)

func TestSideChains(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true)
	if err != nil {
		t.Fatal(err)
	}
	tips, err := SideChains(client)
	if err != nil {
		t.Errorf("SideChains failed: %v", err)
	}
	t.Logf("Tips: %v", tips)
}

func TestSideChainFull(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true)
	if err != nil {
		t.Fatal(err)
	}

	// Try to get side chain for a main chain block
	sideChain, err := SideChainFull(client, "00000000aab245a5b4c5cd4c3318310c45edcc1aa016305820602e76551daf87")
	if err == nil {
		t.Errorf("SideChainFull should have failed for a mainchain block")
	}
	t.Logf("Main chain block is not on sidechain, as expected: %v", err)

	// Try to get side chain for a block that was recorded on one node as a side chain tip.
	sideChain, err = SideChainFull(client, "00000000cef7d921ccbb78282509c6a693d67c74d1bc046f0154ef89f082c231")
	if err != nil {
		t.Errorf("SideChainFull failed: %v", err)
	}
	t.Logf("Side chain: %v", sideChain)
}
