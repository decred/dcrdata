//go:build pgonline && fullpgdb

package dcrpg

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/db/dcrpg/v8/internal"
)

func TestGetAddressTransactionsRawWithSkip(t *testing.T) {
	// a := "DsoFFRrsCvWLSgy9wnjWuqrRkUrAYoWSH3s" // split tx and ticket spending it
	a := "Dsnm5MaPtic52uveAP6savT8hkSHttAFpvv" // stakesubmission (ticket) and vote
	// a := "DsSWTHFrsXV77SwAcMe451kJTwWjwPYjWTM" // miner with coinbases
	t0 := time.Now()
	res := db.GetAddressTransactionsRawWithSkip(a, 1000, 0)
	d := time.Since(t0)
	// spew.Dump(res)

	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(b), len(res))
	t.Log(d)
}

func TestMixedUtxosByHeight(t *testing.T) {
	heights, utxoCountReg, utxoValueReg, utxoCountStk, utxoValueStk, err := db.MixedUtxosByHeight()
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	csvfile, err := os.Create("utxos.csv")
	if err != nil {
		t.Fatalf("error creating utxos file: %s", err)
	}
	defer csvfile.Close()

	csvwriter := csv.NewWriter(csvfile)
	defer csvwriter.Flush()

	for i := range heights {
		err = csvwriter.Write([]string{
			fmt.Sprint(heights[i]),
			fmt.Sprint(utxoCountReg[i]),
			fmt.Sprint(utxoValueReg[i] / 1e8),
			fmt.Sprint(utxoCountStk[i]),
			fmt.Sprint(utxoValueStk[i] / 1e8),
		})
		if err != nil {
			t.Fatalf("csvwriter.Write: %s", err)
		}
	}

}

func TestAddressRows(t *testing.T) {
	rows, err := db.AddressRowsMerged("Dsh6khiGjTuyExADXxjtDgz1gRr9C5dEUf6")
	if err != nil {
		t.Fatal(err)
	}
	neededTxns := []string{"23c329675052915d326c8048b16105a25002478c247c815b314d04b349fc8bea",
		"53bb28bce5bde2b75825f5dc9cbe4c310fe04d8447ef48df20815f1079633c11"}
	for _, r := range rows {
		for i := range neededTxns {
			if neededTxns[i] == r.TxHash.String() {
				// remove
				neededTxns[i] = neededTxns[len(neededTxns)-1]
				neededTxns = neededTxns[:len(neededTxns)-1]
				break
			}
		}
	}
	if len(neededTxns) > 0 {
		t.Error("Some expected transactions not found:", neededTxns)
	}
}

func TestMissingIndexes(t *testing.T) {
	missing, descs, err := db.MissingIndexes()
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Errorf("Not all indexes exist in test table! Missing: %v", missing)
	}
	if len(missing) != len(descs) {
		t.Errorf("MissingIndexes returned %d missing indexes but %d descriptions.",
			len(missing), len(descs))
	}
}

func TestExistsIndex(t *testing.T) {
	// negative test
	fakeIndexName := "not_an_index_adsfasdfa"
	exists, err := ExistsIndex(db.db, fakeIndexName)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Errorf(`Index "%s" should not exist!`, fakeIndexName)
	}

	// positive test
	realIndexName := internal.IndexOfBlocksTableOnHash
	exists, err = ExistsIndex(db.db, realIndexName)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Errorf(`Index "%s" should exist!`, realIndexName)
	}
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
