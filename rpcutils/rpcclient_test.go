package rpcutils

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
)

func TestSideChainTips(t *testing.T) {
	_ = `[
		{
		  "height": 35890,
		  "hash": "000000000332ad2682f681a2199e481f03b06998e29e2e72cafa54a156fc1159",
		  "branchlen": 0,
		  "status": "active"
		},
		{
		  "height": 22058,
		  "hash": "00000000017c501c7d78af471e3ae60ea9a5696e9f840af6f1f2b8fa05b35030",
		  "branchlen": 1,
		  "status": "valid-headers"
		},
		{
		  "height": 21752,
		  "hash": "00000000000847caac35991cdd3f9f8117d9b27de9e67e3e3a4d4ec942133b1d",
		  "branchlen": 1,
		  "status": "valid-fork"
		}
	]`

	allTips := []chainjson.GetChainTipsResult{
		{
			Height:    35890,
			Hash:      "000000000332ad2682f681a2199e481f03b06998e29e2e72cafa54a156fc1159",
			BranchLen: 0,
			Status:    "active",
		},
		{
			Height:    22058,
			Hash:      "00000000017c501c7d78af471e3ae60ea9a5696e9f840af6f1f2b8fa05b35030",
			BranchLen: 1,
			Status:    "valid-headers",
		},
		{
			Height:    21752,
			Hash:      "00000000000847caac35991cdd3f9f8117d9b27de9e67e3e3a4d4ec942133b1d",
			BranchLen: 1,
			Status:    "valid-fork",
		},
	}

	sideTips := []chainjson.GetChainTipsResult{
		{
			Height:    22058,
			Hash:      "00000000017c501c7d78af471e3ae60ea9a5696e9f840af6f1f2b8fa05b35030",
			BranchLen: 1,
			Status:    "valid-headers",
		},
		{
			Height:    21752,
			Hash:      "00000000000847caac35991cdd3f9f8117d9b27de9e67e3e3a4d4ec942133b1d",
			BranchLen: 1,
			Status:    "valid-fork",
		},
	}

	tips := sideChainTips(allTips)

	// Check number of side chain tips before deep equality check.
	if len(tips) != len(sideTips) {
		t.Errorf("Found %d side chain tips, expected %d.", len(tips), len(sideTips))
	}

	// Valid side chain tips only have status of "valid-headers" or "valid-fork".
	for it := range tips {
		switch tips[it].Status {
		case "valid-headers", "valid-fork":
		default:
			t.Errorf("Unexpected tip status: %s", tips[it].Status)
		}
	}

	// Result should be the same elements in the same order.
	if !reflect.DeepEqual(sideTips, tips) {
		t.Errorf("Incorrect side chain tips.\nGot:\n\t%v\nExpected:\n\t%v", tips, sideTips)
	}
}

func TestReverseStringSlice(t *testing.T) {
	// Even length slice
	s0 := []string{"a", "b", "c", "d"}
	ref0 := []string{"d", "c", "b", "a"}

	reverseStringSlice(s0)
	if !reflect.DeepEqual(s0, ref0) {
		t.Errorf("reverseStringSlice failed. Got %v, expected %v.", s0, ref0)
	}

	// Odd length slice
	s1 := []string{"a", "b", "c", "d", "e"}
	ref1 := []string{"e", "d", "c", "b", "a"}

	reverseStringSlice(s1)
	if !reflect.DeepEqual(s1, ref1) {
		t.Errorf("reverseStringSlice failed. Got %v, expected %v.", s1, ref1)
	}

	// nil slice
	var s2, ref2 []string

	reverseStringSlice(s2)
	if !reflect.DeepEqual(s2, ref2) {
		t.Errorf("reverseStringSlice failed. Got %v, expected %v.", s2, ref2)
	}
}

var hashMap = map[int64]string{
	0: "0000000000000000000000000000000000000000000000000000000000000000",
	1: "0000000000000000000000000000000000000000000000000000000000000001",
	2: "0000000000000000000000000000000000000000000000000000000000000002",
	3: "0000000000000000000000000000000000000000000000000000000000000003",
	4: "0000000000000000000000000000000000000000000000000000000000000004",
	5: "0000000000000000000000000000000000000000000000000000000000000005",
}

type hashGetterStub struct{}

func (client hashGetterStub) GetBlockHash(_ context.Context, idx int64) (*chainhash.Hash, error) {
	if idx > 5 || idx < 0 {
		return nil, fmt.Errorf("hashGetterStub: index out of range")
	}
	return chainhash.NewHashFromStr(hashMap[idx])
}

func TestOrphanedTipLength(t *testing.T) {
	client := hashGetterStub{}

	hashes := map[int64]string{
		5: "0000000000000000000000000000000000000000000000000000000000000005",
		4: "something else",
	}

	hashFunc := func(idx int64) (string, error) {
		hash, ok := hashes[idx]
		if ok {
			return hash, nil
		}
		return "", fmt.Errorf("hashFunc index not found")
	}

	ctx, shutdown := context.WithCancel(context.Background())

	uncommon, err := OrphanedTipLength(ctx, client, 5, hashFunc)
	if err != nil {
		t.Fatal(err)
	}
	if uncommon != 0 {
		t.Fatal(fmt.Errorf("Unexpected results from OrphanedTipLength test 1"))
	}

	hashes = map[int64]string{
		5: "something else",
		4: "0000000000000000000000000000000000000000000000000000000000000004",
	}

	uncommon, err = OrphanedTipLength(ctx, client, 5, hashFunc)
	if err != nil {
		t.Fatal(err)
	}
	if uncommon != 1 {
		t.Fatal(fmt.Errorf("Unexpected results from OrphanedTipLength test 2"))
	}

	hashes = map[int64]string{
		5: "something else",
		4: "side block",
		3: "0000000000000000000000000000000000000000000000000000000000000003",
	}
	twoOrphans := hashes // Will use this later to test shutdown

	uncommon, err = OrphanedTipLength(ctx, client, 5, hashFunc)
	if err != nil {
		t.Fatal(err)
	}
	if uncommon != 2 {
		t.Fatal(fmt.Errorf("Unexpected results from OrphanedTipLength test 3"))
	}

	hashes = map[int64]string{
		5: "something else",
		4: "side block",
		3: "blah",
		2: "blue",
		1: "flu",
		0: "fly",
	}

	_, err = OrphanedTipLength(ctx, client, 5, hashFunc)
	if err == nil || err.Error() != "Unable to find a common ancestor" {
		t.Fatal(fmt.Errorf("Unexpected results from OrphanedTipLength test 4"))
	}

	hashes = twoOrphans
	shutdown()
	uncommon, err = OrphanedTipLength(ctx, client, 5, hashFunc)
	if err != nil {
		t.Fatal(err)
	}
	if uncommon != 0 {
		t.Fatal(fmt.Errorf("Unexpected results from OrphanedTipLength test 5"))
	}
}
