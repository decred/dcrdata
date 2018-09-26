// +build testnetRPC

package rpcutils

import (
	"testing"
)

func TestSideChains(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", "dcrd", "dcrd", "", true)
	if err != nil {
		t.Fatal(err)
	}
	tips, err := SideChains(client)
	if err != nil {
		t.Errorf("SideChains failed: %v", err)
	}
	t.Logf("Tips: %v", tips)
}
