package dcrsqlite

import (
	"testing"
)

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
