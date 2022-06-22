// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package cache

import (
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/v8/db/dbtypes"
)

func TestCacheLock_TryLock(t *testing.T) {
	cl := NewCacheLock()

	addr := "blah"
	busy, wait, done := cl.TryLock(addr)
	if busy {
		t.Fatal("should not be busy")
	}
	if wait != nil {
		t.Fatal("wait should be a nil channel")
	}

	busy2, wait2, _ := cl.TryLock(addr)
	if !busy2 {
		t.Fatal("should be busy")
	}
	if wait2 == nil {
		t.Fatal("wait2 should not be nil")
	}

	go func() {
		time.Sleep(2 * time.Second)
		done()
	}()

	t0 := time.Now()
	t.Log("waiting")
	<-wait2
	t.Log("waited for", time.Since(t0))
}

func TestAddressCacheItem_Transactions(t *testing.T) {
	hash, _ := chainhash.NewHashFromStr("000000000000000013a7c09f195ee4b28cd68599173c918037d67ec5b65c8c7d")
	aci := AddressCacheItem{
		height: 329985,
		hash:   *hash,
	}

	// rows cache misses

	nonMergedViews := []dbtypes.AddrTxnViewType{dbtypes.AddrTxnAll,
		dbtypes.AddrTxnCredit, dbtypes.AddrTxnDebit, dbtypes.AddrUnspentTxn}

	for _, v := range nonMergedViews {
		rows, blockID, err := aci.Transactions(1, 0, v)
		if err != nil {
			t.Fatal(err)
		}

		if blockID != nil {
			t.Errorf("Should have been cache miss.")
		}

		switch rows.(type) {
		case []*dbtypes.AddressRowCompact:
		default:
			t.Error("rows type should have been []dbtypes.AddressRowCompact")
		}
	}

	mergedViews := []dbtypes.AddrTxnViewType{dbtypes.AddrMergedTxn,
		dbtypes.AddrMergedTxnCredit, dbtypes.AddrMergedTxnDebit}

	for _, v := range mergedViews {
		rows, blockID, err := aci.Transactions(1, 0, v)
		if err != nil {
			t.Fatal(err)
		}

		if blockID != nil {
			t.Errorf("Should have been cache miss.")
		}

		switch rows.(type) {
		case []*dbtypes.AddressRowMerged:
		default:
			t.Error("rows type should have been []dbtypes.AddressRowMerged")
		}
	}

	// rows cache hit

	txHash, _ := chainhash.NewHashFromStr("05e7195ce139c62a46cb77e0002018a14ebe7e6442cd6c2e39274902a44a2a66")
	aci.rows = []*dbtypes.AddressRowCompact{
		{
			Address: "Dsnieug5H7Zn3SjUWwbcZ17ox9d3F2TEvZV",
			TxHash:  *txHash,
			Value:   121,
		},
	}

	allTypeViews := []struct {
		merged bool
		view   dbtypes.AddrTxnViewType
	}{
		{false, dbtypes.AddrTxnAll},
		{true, dbtypes.AddrMergedTxn},
	}

	for _, v := range allTypeViews {
		rows, blockID, err := aci.Transactions(100, 0, v.view)
		if err != nil {
			t.Fatal(err)
		}

		if blockID == nil {
			t.Errorf("Should have been cache hit.")
		}

		if v.merged {
			switch r := rows.(type) {
			case []*dbtypes.AddressRowMerged:
				if len(r) != len(aci.rows) {
					t.Fatalf("number of rows incorrect. Got %d, want %d",
						len(r), len(aci.rows))
				}
			default:
				t.Error("rows type should have been []dbtypes.AddressRowMerged")
			}
		} else {
			switch r := rows.(type) {
			case []*dbtypes.AddressRowCompact:
				if len(r) != len(aci.rows) {
					t.Fatalf("number of rows incorrect. Got %d, want %d",
						len(r), len(aci.rows))
				}
			default:
				t.Error("rows type should have been []dbtypes.AddressRowCompact")
			}
		}
	}
}
