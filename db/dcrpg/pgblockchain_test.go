// +build mainnettest

package dcrpg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v4/db/dbtypes"
)

var (
	db *ChainDB
)

func openDB() (func() error, error) {
	dbi := DBInfo{
		Host:   "localhost",
		Port:   "5432",
		User:   "dcrdata", // postgres for admin operations
		Pass:   "",
		DBName: "dcrdata_mainnet_test",
	}
	var err error
	db, err = NewChainDB(&dbi, &chaincfg.MainNetParams, nil, true)
	cleanUp := func() error { return nil }
	if db != nil {
		cleanUp = db.Close
	}
	return cleanUp, err
}

func TestMain(m *testing.M) {
	// your func
	cleanUp, err := openDB()
	defer cleanUp()
	if err != nil {
		panic(fmt.Sprintln("no db for testing:", err))
	}

	retCode := m.Run()

	// call with result of m.Run()
	os.Exit(retCode)
}

func TestCheckColumnDataType(t *testing.T) {
	dataType, err := CheckColumnDataType(db.db, "blocks", "time")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(dataType)
}

func TestCheckCurrentTimeZone(t *testing.T) {
	currentTZ, err := CheckCurrentTimeZone(db.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Set time zone: %v", currentTZ)
}

func TestCheckDefaultTimeZone(t *testing.T) {
	defaultTZ, currentTZ, err := CheckDefaultTimeZone(db.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Set time zone: %v", currentTZ)
	t.Logf("Default time zone: %v", defaultTZ)
}

func TestDeleteBestBlock(t *testing.T) {
	ctx := context.Background()
	res, height, hash, err := DeleteBestBlock(ctx, db.db)
	t.Logf("Deletion summary for block %d (%s): %v", height, hash, res)
	if err != nil {
		t.Errorf("Failed to delete best block data: %v", err)
	}

	timings := fmt.Sprintf("Blocks: %v\n", time.Duration(res.Timings.Blocks))
	timings = timings + fmt.Sprintf("\tVins: %v\n", time.Duration(res.Timings.Vins))
	timings = timings + fmt.Sprintf("\tVouts: %v\n", time.Duration(res.Timings.Vouts))
	timings = timings + fmt.Sprintf("\tAddresses: %v\n", time.Duration(res.Timings.Addresses))
	timings = timings + fmt.Sprintf("\tTransactions: %v\n", time.Duration(res.Timings.Transactions))
	timings = timings + fmt.Sprintf("\tTickets: %v\n", time.Duration(res.Timings.Tickets))
	timings = timings + fmt.Sprintf("\tVotes: %v\n", time.Duration(res.Timings.Votes))
	timings = timings + fmt.Sprintf("\tMisses: %v\n", time.Duration(res.Timings.Misses))
	t.Logf("Timings:\n%s", timings)
}

func TestDeleteBlocks(t *testing.T) {
	height0, hash0, _, err := RetrieveBestBlockHeight(context.Background(), db.db)
	if err != nil {
		t.Error(err)
	}

	N := int64(222)
	if N > int64(height0) {
		t.Fatalf("Cannot remove %d blocks from block chain of height %d.",
			N, height0)
	}

	t.Logf("Initial best block %d (%s).", height0, hash0)

	start := time.Now()
	ctx := context.Background()

	res, _, _, err := DeleteBlocks(ctx, N, db.db)
	if err != nil {
		t.Error(err)
	}
	if len(res) != int(N) {
		t.Errorf("Expected to delete %d blocks; actually deleted %d.", N, len(res))
	}

	height, hash, _, err := RetrieveBestBlockHeight(ctx, db.db)
	if err != nil {
		t.Error(err)
	}
	t.Logf("Final best block %d (%s).", height, hash)

	summary := dbtypes.DeletionSummarySlice(res).Reduce()
	if summary.Blocks != N {
		t.Errorf("Expected summary of %d deleted blocks, got %d.", N, summary.Blocks)
	}
	t.Logf("Removed %d blocks in %v:\n%v.", N, time.Since(start), summary)
}

func TestStuff(t *testing.T) {
	//testTx := "fa9acf7a4b1e9a52df1795f3e1c295613c9df44f5562de66595acc33b3831118"
	// A fully spent transaction
	testTx := "f4a44e6916f9ee5a2e41558e0662c1d26206780078dc0a426b3607fd43e34145"

	numSpentOuts := 8
	voutInd := uint32(2)
	spendingRef := "ce6a41aa545af4dfc3b6d9c31f15d0be28b890f24f4344be90a55eda96418cad"

	testBlockHash := "000000000000022173bcd0e354bb3b68f33af459cb68b8dd1f2831172c499c0b"
	numBlockTx := 10
	testTxBlockInd := uint32(1)
	testTxBlockTree := wire.TxTreeRegular

	// Test number of spent outputs / spending transactions
	spendingTxns, _, _, err := db.SpendingTransactions(testTx)
	if err != nil {
		t.Error("SpendingTransactions", err)
	}
	t.Log(spew.Sdump(spendingTxns))

	if len(spendingTxns) != numSpentOuts {
		t.Fatalf("Incorrect number of spending tx. Got %d, wanted %d.",
			len(spendingTxns), numSpentOuts)
	}

	// Test a certain spending transaction is as expected
	spendingTx, _, _, err := db.SpendingTransaction(testTx, voutInd)
	if err != nil {
		t.Error("SpendingTransaction", err)
	}
	t.Log(spew.Sdump(spendingTx))

	if spendingTx != spendingRef {
		t.Fatalf("Incorrect spending tx. Got %s, wanted %s.",
			spendingTx, spendingRef)
	}

	// Block containing the transaction
	blockHash, blockInd, txTree, err := db.TransactionBlock(testTx)
	if err != nil {
		t.Fatal("TransactionBlock", err)
	}
	t.Log(blockHash, blockInd, txTree)
	if testBlockHash != blockHash {
		t.Fatalf("Incorrect block hash. Got %s, wanted %s.", blockHash, testBlockHash)
	}
	if testTxBlockInd != blockInd {
		t.Fatalf("Incorrect tx block index. Got %d, wanted %d.", blockInd, testTxBlockInd)
	}
	if testTxBlockTree != txTree {
		t.Fatalf("Incorrect tx tree. Got %d, wanted %d.", txTree, testTxBlockTree)
	}

	// List block transactions
	blockTransactions, blockTreeOutInds, blockTxTrees, err := db.BlockTransactions(blockHash)
	if err != nil {
		t.Error("BlockTransactions", err)
	}
	t.Log(spew.Sdump(blockTransactions))
	if len(blockTransactions) != numBlockTx {
		t.Fatalf("Incorrect number of transactions in block. Got %d, wanted %d.",
			len(blockTransactions), numBlockTx)
	}

	var blockTxListInd int
	t.Log(spew.Sdump(blockTreeOutInds), spew.Sdump(blockTransactions))
	for i, txOutInd := range blockTreeOutInds {
		t.Log(i, txOutInd)
		if txOutInd == testTxBlockInd && blockTxTrees[i] == testTxBlockTree {
			blockTxListInd = i
			t.Log(i, txOutInd, blockTransactions[i])
		}
	}

	if blockTransactions[blockTxListInd] != testTx {
		t.Fatalf("Transaction not found in block at index %d. Got %s, wanted %s.",
			testTxBlockInd, blockTransactions[testTxBlockInd], testTx)
	}

	voutValue, err := db.VoutValue(testTx, voutInd)
	if err != nil {
		t.Fatalf("VoutValue: %v", err)
	}
	t.Log(spew.Sdump(testTx, voutInd, voutValue))

	voutValues, txInds, txTrees, err := db.VoutValues(testTx)
	if err != nil {
		t.Fatalf("VoutValues: %v", err)
	}
	t.Log(spew.Sdump(testTx, voutValues, txInds, txTrees))

	if voutValue != voutValues[int(voutInd)] {
		t.Errorf("%d (voutValue) != %d (voutValues[ind])",
			voutValue, voutValues[int(voutInd)])
	}
}

func TestUpdateChainState(t *testing.T) {
	// r blockChainInfoData is a sample payload format as returned by
	// getBlockChainInfo rpc endpoint.
	var rawData = []byte(`{"chain":"mainnet","blocks":316016,"headers":316016,"syncheight":316016,"bestblockhash":` +
		`"00000000000000001d8cfa54dc13cfb0563421fd017801401cb2bdebe3579355","difficulty":406452686,"verificationprogress":1,` +
		`"chainwork":"0000000000000000000000000000000000000000000209c779c196914f038522","initialblockdownload":false,` +
		`"maxblocksize":393216,"deployments":{"fixlnseqlocks":{	"status":"defined","starttime":1548633600,"expiretime":1580169600},` +
		`"lnfeatures":{"status":"active","since":189568,"starttime":1505260800,"expiretime":1536796800},"sampleagenda1":` +
		`{"status":"lockedin","since":119248,"starttime":1493164800,"expiretime":1508976000},"sampleagenda2":{"status":` +
		`"failed","since":149248,"starttime":1493164800,"expiretime":1524700800},"sampleagenda3":{"status":"started","` +
		`since":149248,"starttime":1493164800,"expiretime":1524700800}}}`)

	var chainInfoData = new(dcrjson.GetBlockChainInfoResult)
	err := json.Unmarshal(rawData, chainInfoData)
	if err != nil {
		t.Fatalf("expected no error to be returned but found: %v", err)
	}

	// Expected payload
	var expectedPayload = dbtypes.BlockChainData{
		Chain:                  "mainnet",
		SyncHeight:             316016,
		BestHeight:             316016,
		BestBlockHash:          "00000000000000001d8cfa54dc13cfb0563421fd017801401cb2bdebe3579355",
		Difficulty:             406452686,
		VerificationProgress:   1.0,
		ChainWork:              "0000000000000000000000000000000000000000000209c779c196914f038522",
		IsInitialBlockDownload: false,
		MaxBlockSize:           393216,
		AgendaMileStones: map[string]dbtypes.MileStone{
			"fixlnseqlocks": {
				Status:     dbtypes.InitialAgendaStatus,
				StartTime:  time.Unix(1548633600, 0),
				ExpireTime: time.Unix(1580169600, 0),
			},
			"lnfeatures": {
				Status:     dbtypes.ActivatedAgendaStatus,
				VotingDone: 181504,
				Activated:  189568,
				StartTime:  time.Unix(1505260800, 0),
				ExpireTime: time.Unix(1536796800, 0),
			},
			"sampleagenda1": {
				Status:     dbtypes.LockedInAgendaStatus,
				VotingDone: 119248,
				Activated:  127312,
				StartTime:  time.Unix(1493164800, 0),
				ExpireTime: time.Unix(1508976000, 0),
			},
			"sampleagenda2": {
				Status:     dbtypes.FailedAgendaStatus,
				VotingDone: 149248,
				StartTime:  time.Unix(1493164800, 0),
				ExpireTime: time.Unix(1524700800, 0),
			},
			"sampleagenda3": {
				Status:     dbtypes.StartedAgendaStatus,
				VotingDone: 316016,
				StartTime:  time.Unix(1493164800, 0),
				ExpireTime: time.Unix(1524700800, 0),
			},
		},
	}

	dbRPC := new(ChainDBRPC)
	dbRPC.ChainDB = &ChainDB{
		chainParams: &chaincfg.Params{
			RuleChangeActivationInterval: 8064,
		},
	}

	dbRPC.UpdateChainState(chainInfoData)

	if reflect.DeepEqual(dbRPC.chainInfo, expectedPayload) {
		t.Fatalf("expected both payloads to match but the did not")
	}
}
