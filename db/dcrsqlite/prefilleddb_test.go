package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestGetBestBlockHeight(t *testing.T) {
	testutil.BindCurrentTestSetup(t)

	tag := "synced_up_to_260241"
	tdb := testutil.LoadTestDataHandler(tag)
	db := ObtainDB(tag)
	exh := tdb.GetExpectedBestBlockHeight()
	h := db.GetBestBlockHeight()
	if exh != h {
		testutil.ReportTestFailed(
			"db.GetBestBlockHeight() is %v, expected %v",
			h,
			exh)
	}
}

func TestGetStakeInfoHeight(t *testing.T) {
	testutil.BindCurrentTestSetup(t)

	tag := "synced_up_to_260241"
	tdb := testutil.LoadTestDataHandler(tag)
	db := ObtainDB(tag)
	exh := tdb.GetExpectedStakeInfoHeight()
	h, err := db.GetStakeInfoHeight()
	if err != nil {
		testutil.ReportTestFailed(
			"GetStakeInfoHeight() failed: %v",
			err)
	}
	if exh != h {
		testutil.ReportTestFailed(
			"db.GetStakeInfoHeight() is %v, expected %v",
			h,
			exh)
	}
}

func TestGetBestBlockHash(t *testing.T) {
	testutil.BindCurrentTestSetup(t)

	tag := "synced_up_to_260241"
	tdb := testutil.LoadTestDataHandler(tag)
	db := ObtainDB(tag)
	exh := tdb.GetExpectedBestBlockHash()
	h := db.GetBestBlockHash()
	if exh != h {
		testutil.ReportTestFailed(
			"db.GetBestBlockHash() is %v, expected %v",
			h,
			exh)
	}
}
