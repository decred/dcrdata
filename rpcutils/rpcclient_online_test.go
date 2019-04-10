// +build testnetRPC

// TODO: All these test should run with regnet. They were originally made for
// testnet2, which no longer exists.

package rpcutils

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient/v2"
	"github.com/decred/slog"
)

const (
	nodeUser = "jdcrd"
	nodePass = "jdcrdx"
)

func testCommonAncestorPositive(t *testing.T, client *rpcclient.Client, hashA, hashB, expectedAncestor *chainhash.Hash, chainAHashes, chainBHashes []string) {
	var chainAExpected, chainBExpected []chainhash.Hash
	for i := range chainAHashes {
		h, _ := chainhash.NewHashFromStr(chainAHashes[i])
		chainAExpected = append(chainAExpected, *h)
	}
	for i := range chainBHashes {
		h, _ := chainhash.NewHashFromStr(chainBHashes[i])
		chainBExpected = append(chainBExpected, *h)
	}

	hash, chainA, chainB, err := CommonAncestor(client, *hashA, *hashB)
	if err != nil {
		t.Error(err)
	}
	// Allow expectedAncestor and hash to both be nil pointers, or indicate the
	// same hash.
	if hash != expectedAncestor && hash != nil && *hash != *expectedAncestor {
		t.Errorf("hash should have been %v, got %v", expectedAncestor, hash)
	}
	if !reflect.DeepEqual(chainA, chainAExpected) {
		t.Errorf("chainA should have been %v, got %v", chainAExpected, chainA)
	}
	if !reflect.DeepEqual(chainB, chainBExpected) {
		t.Errorf("chainB should have been %v, got %v", chainBExpected, chainB)
	}
}

func TestCommonAncestorOnlineDifferentHeights(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()

	// actual testnet side chain on a certain node
	hashAncestorExpected, _ := chainhash.NewHashFromStr("000000000002a17a0aeb56856b7b531926a7684e242a39aa5b1acb130a945f9e") // common ancestor at 72906
	hashA, _ := chainhash.NewHashFromStr("0000000004b7497db457e7159ebe9a3775f9c0389ed6558cbd50da6650d5520f")                // side chain @ 72907
	hashB, _ := chainhash.NewHashFromStr("000000000036c32da2a340574904726f463a497b7a16fca727420e5c62cf154b")                // main chain @ 72908

	chainAHashes := []string{"0000000004b7497db457e7159ebe9a3775f9c0389ed6558cbd50da6650d5520f"}
	chainBHashes := []string{"0000000007771c758e95635dc5806441c3926b76f0fb8de3f23cd4ca24b23059",
		"000000000036c32da2a340574904726f463a497b7a16fca727420e5c62cf154b"}

	testCommonAncestorPositive(t, client, hashA, hashB, hashAncestorExpected, chainAHashes, chainBHashes)

	// swap A and B
	testCommonAncestorPositive(t, client, hashB, hashA, hashAncestorExpected, chainBHashes, chainAHashes)
}

func TestCommonAncestorOnlineSameHeights(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()

	// actual testnet side chain on a certain node
	hashAncestorExpected, _ := chainhash.NewHashFromStr("000000000002a17a0aeb56856b7b531926a7684e242a39aa5b1acb130a945f9e") // common ancestor at 72906
	hashA, _ := chainhash.NewHashFromStr("0000000004b7497db457e7159ebe9a3775f9c0389ed6558cbd50da6650d5520f")                // side chain @ 72907
	hashB, _ := chainhash.NewHashFromStr("0000000007771c758e95635dc5806441c3926b76f0fb8de3f23cd4ca24b23059")                // main chain @ 72907

	chainAHashes := []string{"0000000004b7497db457e7159ebe9a3775f9c0389ed6558cbd50da6650d5520f"}
	chainBHashes := []string{"0000000007771c758e95635dc5806441c3926b76f0fb8de3f23cd4ca24b23059"}

	testCommonAncestorPositive(t, client, hashA, hashB, hashAncestorExpected, chainAHashes, chainBHashes)
}

func TestCommonAncestorOnlineSameHashes(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()

	hashAncestorExpected, _ := chainhash.NewHashFromStr("000000000002a17a0aeb56856b7b531926a7684e242a39aa5b1acb130a945f9e") // common ancestor at 72906
	hashA, _ := chainhash.NewHashFromStr("0000000007771c758e95635dc5806441c3926b76f0fb8de3f23cd4ca24b23059")                // main chain @ 72907
	hashB := hashA                                                                                                          // same block

	chainAHashes := []string{"0000000007771c758e95635dc5806441c3926b76f0fb8de3f23cd4ca24b23059"}
	chainBHashes := chainAHashes

	testCommonAncestorPositive(t, client, hashA, hashB, hashAncestorExpected, chainAHashes, chainBHashes)
}

func TestCommonAncestorOnlineSharedBlock(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()

	// actual testnet side chain on a certain node
	hashAncestorExpected, _ := chainhash.NewHashFromStr("00000000078f12fb5cdf166a9fd04951755bbd7355d3b2f18dcd5158110ca573") // common ancestor at 72905
	hashA, _ := chainhash.NewHashFromStr("0000000004b7497db457e7159ebe9a3775f9c0389ed6558cbd50da6650d5520f")                // side chain @ 72907
	hashB, _ := chainhash.NewHashFromStr("000000000002a17a0aeb56856b7b531926a7684e242a39aa5b1acb130a945f9e")                // main chain at 72906

	// Note that hashB is the last hash in both chains. The common ancestor is
	// the previous block since a block is not the ancestor of itself.
	chainAHashes := []string{"000000000002a17a0aeb56856b7b531926a7684e242a39aa5b1acb130a945f9e",
		"0000000004b7497db457e7159ebe9a3775f9c0389ed6558cbd50da6650d5520f"}
	chainBHashes := []string{"000000000002a17a0aeb56856b7b531926a7684e242a39aa5b1acb130a945f9e"}

	testCommonAncestorPositive(t, client, hashA, hashB, hashAncestorExpected, chainAHashes, chainBHashes)

	// swap A and B
	testCommonAncestorPositive(t, client, hashB, hashA, hashAncestorExpected, chainBHashes, chainAHashes)
}

func TestCommonAncestorOnlineGenesis(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()

	// var hashAncestorExpected *chainhash.Hash
	GenesisHash, _ := chainhash.NewHashFromStr("a649dce53918caf422e9c711c858837e08d626ecfcd198969b24f7b634a49bac")
	hashA, hashB := GenesisHash, GenesisHash // same block

	chainAHashes := []string{"a649dce53918caf422e9c711c858837e08d626ecfcd198969b24f7b634a49bac"}
	chainBHashes := chainAHashes

	var chainAExpected, chainBExpected []chainhash.Hash
	for i := range chainAHashes {
		h, _ := chainhash.NewHashFromStr(chainAHashes[i])
		chainAExpected = append(chainAExpected, *h)
	}
	for i := range chainBHashes {
		h, _ := chainhash.NewHashFromStr(chainBHashes[i])
		chainBExpected = append(chainBExpected, *h)
	}

	hash, chainA, chainB, err := CommonAncestor(client, *hashA, *hashB)
	if err != ErrAncestorAtGenesis {
		t.Error(err)
	}
	if hash != nil {
		t.Errorf("hash should have been %v, got %v", nil, hash)
	}
	if !reflect.DeepEqual(chainA, chainAExpected) {
		t.Errorf("chainA should have been %v, got %v", chainAExpected, chainA)
	}
	if !reflect.DeepEqual(chainB, chainBExpected) {
		t.Errorf("chainB should have been %v, got %v", chainBExpected, chainB)
	}
}

func TestCommonAncestorOnlineBadHash(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()

	// actual testnet side chain on a certain node
	hashA, _ := chainhash.NewHashFromStr("0000000004b7497db457e7159ebe9a3775f9c0389ed6558cbd50da6650d5520f") // side chain @ 72907
	hashB, _ := chainhash.NewHashFromStr("1100000007771c758e95635dc5806441c3926b76f0fb8de3f23cd4ca24b23059") // main chain @ 72907

	hash, chainA, chainB, err := CommonAncestor(client, *hashA, *hashB)
	if err == nil {
		t.Errorf("Invalid block hash should cause error.")
	}
	if hash != nil {
		t.Errorf("Invalid block hash should return a nil common ancestor.")
	}
	if chainA != nil || chainB != nil {
		t.Errorf("Invalid block hash should return nil chain slices.")
	}
}

func TestCommonAncestorOnlineTooLong(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()

	// actual testnet side chain on a certain node
	hashA, _ := chainhash.NewHashFromStr("0000000004b7497db457e7159ebe9a3775f9c0389ed6558cbd50da6650d5520f") // side chain @ 72907
	hashB, _ := chainhash.NewHashFromStr("00000000055ff907b535338734fbb1c2322d5fffc19a3d744e896a93800267d4") // main chain @ 60000

	hash, chainA, chainB, err := CommonAncestor(client, *hashA, *hashB)
	if err != ErrAncestorMaxChainLength {
		t.Errorf("Invalid block hash should cause %v.", ErrAncestorMaxChainLength)
	}
	if hash != nil {
		t.Errorf("Invalid block hash should return a nil common ancestor.")
	}
	if chainA != nil || chainB != nil {
		t.Errorf("Invalid block hash should return nil chain slices.")
	}
}

func TestSideChains(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	tips, err := SideChains(client)
	if err != nil {
		t.Errorf("SideChains failed: %v", err)
	}
	t.Logf("Tips: %v", tips)
}

func TestSideChainFull(t *testing.T) {
	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true, false)
	if err != nil {
		t.Fatal(err)
	}

	// Try to get side chain for a main chain block
	_, err = SideChainFull(client, "00000000aab245a5b4c5cd4c3318310c45edcc1aa016305820602e76551daf87")
	if err == nil {
		t.Errorf("SideChainFull should have failed for a mainchain block")
	}
	t.Logf("Main chain block is not on sidechain, as expected: %v", err)

	// Try to get side chain for a block that was recorded on one node as a side chain tip.
	sideChain, err := SideChainFull(client, "00000000cef7d921ccbb78282509c6a693d67c74d1bc046f0154ef89f082c231")
	if err != nil {
		t.Errorf("SideChainFull failed: %v", err)
	}
	t.Logf("Side chain: %v", sideChain)
}

func TestBlockPrefetchClient_GetBlockData(t *testing.T) {
	backendLog := slog.NewBackend(os.Stdout)
	pfLogger := backendLog.Logger("RPC")
	//pfLogger.SetLevel(slog.LevelDebug)
	UseLogger(pfLogger)

	client, _, err := ConnectNodeRPC("127.0.0.1:19109", nodeUser, nodePass, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()

	pf := NewBlockPrefetchClient(client)
	bestHash, bestHeight, err := pf.GetBestBlock()
	if err != nil {
		t.Fatalf("GetBestBlock: %v", err)
	}
	t.Logf("best block: hash=%v, height=%d", bestHash, bestHeight)

	height := bestHeight - 1e3
	bestHash, err = pf.GetBlockHash(height)
	if err != nil {
		t.Fatalf("GetBlockHash: %v", err)
	}

	msgBlock, headerResult, err := pf.GetBlockData(bestHash)
	if err != nil {
		t.Fatalf("GetBlockData: %v", err)
	}
	t.Log(msgBlock.BlockHash())
	t.Log(msgBlock.Header.Height)
	t.Log(headerResult.ChainWork)

	time.Sleep(500 * time.Millisecond)

	// another block
	height++
	bestHash, err = pf.GetBlockHash(height)
	if err != nil {
		t.Fatalf("GetBlockHash: %v", err)
	}

	msgBlock, headerResult, err = pf.GetBlockData(bestHash)
	if err != nil {
		t.Fatalf("GetBlockData: %v", err)
	}
	t.Log(msgBlock.BlockHash())
	t.Log(msgBlock.Header.Height)
	t.Log(headerResult.ChainWork)

	// another block
	height++
	bestHash, err = pf.GetBlockHash(height)
	if err != nil {
		t.Fatalf("GetBlockHash: %v", err)
	}

	msgBlock, headerResult, err = pf.GetBlockData(bestHash)
	if err != nil {
		t.Fatalf("GetBlockData: %v", err)
	}
	t.Log(msgBlock.BlockHash())
	t.Log(msgBlock.Header.Height)
	t.Log(headerResult.ChainWork)

	// Go crazy
	nextHash := headerResult.NextHash
	var hash chainhash.Hash
	for i := height + 1; i < height+1e3; i++ {
		// hash (Hash) <- nextHash (string)
		_ = chainhash.Decode(&hash, nextHash)

		msgBlock, headerResult, err = pf.GetBlockData(&hash)
		if err != nil {
			t.Logf("GetBestBlock: %v / %d", err, i)
			break
		}
		nextHash = headerResult.NextHash

		// worky worky
		time.Sleep(20 * time.Microsecond) // ~0.2 ms to hex convert hash and prefetch
	}

	t.Log(pf.Hits(), pf.Misses())
}
