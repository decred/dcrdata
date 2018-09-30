// +build mainnettest

package dcrpg

import (
	"fmt"
	"os"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
)

var (
	db *ChainDB
)

func openDB() (func() error, error) {
	dbi := DBInfo{
		Host:   "localhost",
		Port:   "5432",
		User:   "dcrdata",
		Pass:   "dcrdata",
		DBName: "dcrdata",
	}
	var err error
	db, err = NewChainDB(&dbi, &chaincfg.MainNetParams)
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
	spendingTxns, err := db.SpendingTransactions(testTx)
	if err != nil {
		t.Error("SpendingTransactions", err)
	}
	t.Log(spew.Sdump(spendingTxns))

	if len(spendingTxns) != numSpentOuts {
		t.Fatalf("Incorrect number of spending tx. Got %d, wanted %d.",
			len(spendingTxns), numSpentOuts)
	}

	// Test a certain spending transaction is as expected
	spendingTx, err := db.SpendingTransaction(testTx, voutInd)
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
