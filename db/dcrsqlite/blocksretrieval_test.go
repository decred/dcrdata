package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/google/go-cmp/cmp"
)

func TestEmptyDBRetrieveAllPoolValAndSize(t *testing.T) {
	db, err := ReusableEmptyDB()
	if err != nil {
		t.Fatalf("Failed to obtain test DB: %v", err)
	}

	result, err := db.RetrieveAllPoolValAndSize()
	if err != nil {
		t.Fatalf("RetrieveAllPoolValAndSize() failed: default result expected: %v", err)
	}

	var defaultChartsData dbtypes.ChartsData
	if !cmp.Equal(*result, defaultChartsData) {
		t.Fatalf("RetrieveAllPoolValAndSize() failed: default result expected:\n%v",
			cmp.Diff(*result, defaultChartsData))
	}
}

func TestEmptyDBRetrieveBlockFeeInfo(t *testing.T) {
	db, err := ReusableEmptyDB()
	if err != nil {
		t.Fatalf("Failed to obtain test DB: %v", err)
	}

	result, err := db.RetrieveBlockFeeInfo()
	if err != nil {
		t.Fatalf("RetrieveBlockFeeInfo() failed: default result expected: %v", err)
	}

	var defaultChartsData dbtypes.ChartsData
	if !cmp.Equal(*result, defaultChartsData) {
		t.Fatalf("RetrieveBlockFeeInfo() failed: default result expected:\n%v",
			cmp.Diff(*result, defaultChartsData))
	}
}

func TestEmptyDBGetBestBlockHash(t *testing.T) {
	db, err := ReusableEmptyDB()
	if err != nil {
		t.Fatalf("Failed to obtain test DB: %v", err)
	}

	str := db.GetBestBlockHash()
	if str != "" {
		t.Fatalf("GetBestBlockHash() failed: expected empty string, returned %v", str)
	}
}

func TestEmptyDBGetBestBlockHeight(t *testing.T) {
	db, err := ReusableEmptyDB()
	if err != nil {
		t.Fatalf("Failed to obtain test DB: %v", err)
	}

	h := db.GetBestBlockHeight()
	if h != -1 {
		t.Fatalf("db.GetBestBlockHeight() returned %d, expected -1", h)
	}
}

func TestEmptyDBGetStakeInfoHeight(t *testing.T) {
	db, err := ReusableEmptyDB()
	if err != nil {
		t.Fatalf("Failed to obtain test DB: %v", err)
	}

	endHeight, err := db.GetStakeInfoHeight()
	if err != nil {
		t.Fatalf("GetStakeInfoHeight() failed: %v", err)
	}
	if endHeight != -1 {
		t.Fatalf("GetStakeInfoHeight() failed: returned %d, expected -1", endHeight)
	}
}
