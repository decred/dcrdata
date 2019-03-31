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

	var result = new(dbtypes.ChartsData)
	result.Time, result.SizeF, result.ValueF, err = db.RetrievePoolAllValueAndSize(result.Time,
		result.SizeF, result.ValueF)
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

	heightArr, feeArr, err := db.RetrieveBlockFeeInfo(nil, nil)
	if err != nil {
		t.Fatalf("RetrieveBlockFeeInfo() failed: default result expected: %v", err)
	}

	var defaultHeights []uint64
	var defaultFees []float64

	if !cmp.Equal(heightArr, defaultHeights) {
		t.Fatalf("RetrieveBlockFeeInfo() failed: default result for heights array expected:\n%v",
			cmp.Diff(heightArr, defaultHeights))
	}

	if !cmp.Equal(feeArr, defaultFees) {
		t.Fatalf("RetrieveBlockFeeInfo() failed: default result for fees array expected:\n%v",
			cmp.Diff(feeArr, defaultFees))
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
