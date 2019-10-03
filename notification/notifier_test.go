// Copyright (c) 2019, The Decred developers

package notification

import (
	"context"
	"sync"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/txhelpers/v3"
)

type dummyNode struct{}

func (node *dummyNode) NotifyBlocks() error              { return nil }
func (node *dummyNode) NotifyNewTransactions(bool) error { return nil }
func (node *dummyNode) NotifyWinningTickets() error      { return nil }

var counter int64
var hashTails = []string{"00", "01", "02", "03", "04", "05", "06", "07", "08", "09"}

func newHash() *chainhash.Hash {
	counter++
	h, _ := chainhash.NewHash([]byte("000000000000000000000000000000" + hashTails[int(counter)%len(hashTails)]))
	return h
}

func (node *dummyNode) GetBestBlock() (*chainhash.Hash, int64, error) {
	hash := newHash()
	return hash, counter, nil
}

var commonAncestorHash = newHash()
var commonAncestor = &wire.MsgBlock{
	Header: wire.BlockHeader{
		PrevBlock: *commonAncestorHash,
		Height:    uint32(5),
	},
}

// GetBlock will only be called by rpcutils.CommonAncestor, so it should return
// the same block every time.
func (node *dummyNode) GetBlock(blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	return commonAncestor, nil
}
func (node *dummyNode) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	hash := newHash()
	return hash, nil
}
func (node *dummyNode) GetBlockHeaderVerbose(hash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error) {
	return nil, nil
}

var callCounter int

// testTxHandler will be tested async
var mtx sync.RWMutex
var wg = new(sync.WaitGroup)
var notifier *Notifier

func testTxHandler(_ *chainjson.TxRawResult) error {
	mtx.Lock()
	defer mtx.Unlock()
	defer wg.Done()
	callCounter++
	return nil
}

var testTxHandler2 = testTxHandler

func testBlockHandler(_ *wire.BlockHeader) error {
	defer wg.Done()
	callCounter++
	return nil
}
func testBlockHandlerLite(_ uint32, _ string) error {
	defer wg.Done()
	callCounter++
	return nil
}
func testReorgHandler(reorg *txhelpers.ReorgData) error {
	defer wg.Done()
	callCounter++
	notifier.SetPreviousBlock(reorg.NewChainHead, uint32(reorg.NewChainHeight))
	return nil
}

func TestNotifier(t *testing.T) {
	ctx, shutdown := context.WithCancel(context.Background())
	notifier = NewNotifier(ctx)
	signals := notifier.DcrdHandlers()
	notifier.RegisterTxHandlerGroup(testTxHandler, testTxHandler2)
	notifier.RegisterBlockHandlerGroup(testBlockHandler)
	notifier.RegisterBlockHandlerLiteGroup(testBlockHandlerLite)
	notifier.RegisterReorgHandlerGroup(testReorgHandler)
	wg.Add(5)

	notifier.Listen(&dummyNode{})

	prevBlock := newHash()
	header := wire.BlockHeader{
		PrevBlock: *prevBlock,
		Height:    uint32(counter),
	}
	notifier.previous.hash = *prevBlock
	bytes, _ := header.Bytes()
	signals.OnBlockConnected(bytes, nil)

	oldHash := newHash()
	ohdHeight := int32(counter)
	newHash := newHash()
	newHeight := counter
	signals.OnReorganization(oldHash, ohdHeight, newHash, int32(newHeight))

	signals.OnTxAcceptedVerbose(new(chainjson.TxRawResult))

	wg.Wait()

	if notifier.previous.hash.String() != newHash.String() {
		t.Errorf("unexpected previous.hash after reorg. %s != %s",
			notifier.previous.hash.String(), newHash.String())
	}

	if notifier.previous.height != uint32(newHeight) {
		t.Errorf("unexpected previous.height after reorg. %d != %d",
			notifier.previous.height, uint32(newHeight))
	}

	if callCounter != 5 {
		t.Errorf("callCounter = %d. Should be 5.", callCounter)
	}

	shutdown()
}
