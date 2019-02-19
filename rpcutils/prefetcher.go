// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package rpcutils

import (
	"strings"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/rpcclient/v2"
	"github.com/decred/dcrd/wire"
)

const (
	outOfRangeErr = "-1: Block number out of range"
)

// BlockFetcher implements a few basic block data retrieval functions.
type BlockFetcher interface {
	GetBestBlock() (*chainhash.Hash, int64, error)
	GetBlock(blockHash *chainhash.Hash) (*wire.MsgBlock, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
	GetBlockHeaderVerbose(hash *chainhash.Hash) (*dcrjson.GetBlockHeaderVerboseResult, error)
}

// Ensure that rpcclient.Client is a BlockFetcher.
var _ BlockFetcher = (*rpcclient.Client)(nil)

type blockData struct {
	height       uint32
	hash         chainhash.Hash
	msgBlock     *wire.MsgBlock
	headerResult *dcrjson.GetBlockHeaderVerboseResult
}

// BlockPrefetchClient uses a BlockFetcher to prefetch the next block after a
// block request. It implements the BlockFetcher for retrieving block data.
type BlockPrefetchClient struct {
	// The BlockFetcher can be a *rpcclient.Client.
	f BlockFetcher

	// The fetcher goroutine uses retargetChan to receive new block orders. The
	// retarget method sends on this channel.
	retargetChan chan *chainhash.Hash

	// The next block data is protected by a Mutex so the fetcher goroutine and
	// the exported methods may safely access the data concurrently.
	sync.Mutex
	current *blockData
	next    *blockData
	hits    uint64
	misses  uint64
}

// Ensure that BlockPrefetchClient is also a BlockFetcher.
var _ BlockFetcher = (*BlockPrefetchClient)(nil)

// NewBlockPrefetchClient constructs a new BlockPrefetchClient configured to
// prefetch the next block after a request using the given BlockFetcher (e.g. an
// *rpcclient.Client).
func NewBlockPrefetchClient(f BlockFetcher) *BlockPrefetchClient {
	p := &BlockPrefetchClient{
		f:            f,
		retargetChan: make(chan *chainhash.Hash, 10),
		current:      new(blockData),
		next:         new(blockData),
	}
	// Start the block fetcher goroutine, waiting for a signal on retargetChan.
	go p.fetcher()
	return p
}

// Stop shutsdown the fetcher goroutine. The BlockPrefetchClient may not be used
// after this.
func (p *BlockPrefetchClient) Stop() {
	p.Lock()
	close(p.retargetChan)
	p.Unlock()
}

// Hits safely returns the number of prefetch hits.
func (p *BlockPrefetchClient) Hits() uint64 {
	p.Lock()
	defer p.Unlock()
	return p.hits
}

// Hits safely returns the number of prefetch misses.
func (p *BlockPrefetchClient) Misses() uint64 {
	p.Lock()
	defer p.Unlock()
	return p.misses
}

// GetBestBlock is a passthrough to the client. It does not retarget the
// prefetch range since it does not request the actual block, just the hash and
// height of the best block.
func (p *BlockPrefetchClient) GetBestBlock() (*chainhash.Hash, int64, error) {
	return p.f.GetBestBlock()
}

// GetBlockData attempts to get the specified block and retargets the prefetcher
// with the next block's hash. If the block was not already fetched, it is
// retrieved immediately and stored following retargeting.
func (p *BlockPrefetchClient) GetBlockData(hash *chainhash.Hash) (*wire.MsgBlock, *dcrjson.GetBlockHeaderVerboseResult, error) {
	p.Lock()

	retargetAndUnlock := func(nextHash string) {
		p.retarget(nextHash)
		p.Unlock()
	}

	// If the block is already fetched, and the current block, return it. Do not
	// retarget.
	if p.current.hash == *hash /*p.haveBlockHash(*hash)*/ {
		p.hits++
		// Return the requested block.
		defer p.Unlock()
		return p.current.msgBlock, p.current.headerResult, nil
	}
	// If the block is already fetched, and next, return it and retarget with a
	// new range starting at this block's height.
	if p.next.hash == *hash /*p.haveBlockHash(*hash)*/ {
		go retargetAndUnlock(p.next.headerResult.NextHash)
		p.hits++
		// Return the requested block.
		return p.next.msgBlock, p.next.headerResult, nil
	}

	p.misses++

	// Immediately retrieve msgBlock and header verbose result while fetcher is
	// blocked by the Mutex.
	msgBlock, headerResult, _, err := p.retrieveBlockAndHeaderResult(hash)
	if err != nil {
		p.Unlock()
		return nil, nil, err
	}

	// Leaving the mutex locked, fire off a goroutine to retarget to the next
	// block. The fetcher goroutine and other locking methods will stay blocked
	// until the mutex unlocks, but the msgBlock can be returned right away.
	go retargetAndUnlock(headerResult.NextHash)

	return msgBlock, headerResult, err
}

// GetBlock retrieve the wire.MsgBlock for the block with the specified hash.
// See GetBlockData for details on how this interacts with the prefetcher.
func (p *BlockPrefetchClient) GetBlock(hash *chainhash.Hash) (*wire.MsgBlock, error) {
	msgBlock, _, err := p.GetBlockData(hash)
	return msgBlock, err
}

// GetBlock retrieve the dcrjson.GetBlockHeaderVerboseResult for the block with
// the specified hash. See GetBlockData for details on how this interacts with
// the prefetcher
func (p *BlockPrefetchClient) GetBlockHeaderVerbose(hash *chainhash.Hash) (*dcrjson.GetBlockHeaderVerboseResult, error) {
	_, headerResult, err := p.GetBlockData(hash)
	return headerResult, err
}

// CheckNext verifies that if the next block has this height, then it also has
// the returned hash. If not, then it signals to the fetcher to refresh the
// block since there was probably a reorganization.
// func (p *BlockPrefetchClient) CheckNext(height uint32, hash *chainhash.Hash) {
// 	// Verify that the hash returned by RPC for the block at this
// 	// height matches th prefetched block at this height.
// 	p.Lock()
// 	defer p.Unlock()
// 	if p.next.height == height && p.next.hash != *hash {
// 		// A block at this height was prefetched, but it has a different hash.
// 		// Flush all prefetched blocks and retarget the fetcher
// 		log.Warnf("Different block at height %d. Re-fetching.", height)
// 		p.retargetHash(hash)
// 	}
// }

// GetBlockHash is a passthrough to the client.
func (p *BlockPrefetchClient) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	return p.f.GetBlockHash(blockHeight)
}

// storeNext stores the input data as the new "next" block. The existing "next"
// becomes "current".
func (p *BlockPrefetchClient) storeNext(msgBlock *wire.MsgBlock, headerResult *dcrjson.GetBlockHeaderVerboseResult) {
	p.current = p.next
	p.next = &blockData{
		height:       msgBlock.Header.Height,
		hash:         msgBlock.BlockHash(),
		msgBlock:     msgBlock,
		headerResult: headerResult,
	}
}

func (p *BlockPrefetchClient) retrieveBlockAndHeaderResult(hash *chainhash.Hash) (*wire.MsgBlock, *dcrjson.GetBlockHeaderVerboseResult, uint32, error) {
	msgBlock, err := p.f.GetBlock(hash)
	if err != nil {
		return nil, nil, 0, err
	}
	height := msgBlock.Header.Height

	headerResult, err := p.f.GetBlockHeaderVerbose(hash)
	if err != nil {
		return nil, nil, 0, err
	}

	return msgBlock, headerResult, height, nil
}

// RetrieveAndStoreNext retrieves the next block specified by the Hash, if it is
// not already the stored next block, and stores the block data. The existing
// "next" becomes "current".
func (p *BlockPrefetchClient) RetrieveAndStoreNext(nextHash *chainhash.Hash) {
	p.Lock()
	defer p.Unlock()

	// Fetch the next block, if needed.
	if p.haveBlockHash(*nextHash) {
		return
	}

	msgBlock, headerResult, _, err := p.retrieveBlockAndHeaderResult(nextHash)
	if err != nil {
		if strings.HasPrefix(err.Error(), outOfRangeErr) {
			log.Errorf("retrieveBlockAndHeaderResult(%v): %v",
				nextHash, err)
		}
		return
	}

	// Store this block's data.
	p.storeNext(msgBlock, headerResult)
}

func (p *BlockPrefetchClient) fetcher() {
	// Wait for signals from retarget(). The loop terminates when retargetChan
	// is closed.
	for nextHash := range p.retargetChan {
		p.RetrieveAndStoreNext(nextHash)
	}
}

func (p *BlockPrefetchClient) haveBlockHeight(height uint32) bool {
	return p.next.height == height || p.current.height == height
}

// HaveBlockHeight checks if the current or prefetched next block is for a block
// with the specified height. Use HaveBlockHash to be sure it is the desired
// block.
func (p *BlockPrefetchClient) HaveBlockHeight(height uint32) bool {
	p.Lock()
	defer p.Unlock()
	return p.haveBlockHeight(height)
}

func (p *BlockPrefetchClient) haveBlockHash(hash chainhash.Hash) bool {
	return p.next.hash == hash || p.current.hash == hash
}

// HaveBlockHash checks if the current or prefetched next block is for a block
// with the specified hash.
func (p *BlockPrefetchClient) HaveBlockHash(hash chainhash.Hash) bool {
	p.Lock()
	defer p.Unlock()
	return p.haveBlockHash(hash)
}

// func (p *BlockPrefetchClient) retargetHash(hash *chainhash.Hash) {
// 	// Signal to the fetcher that there is a new range.
// 	if !p.haveBlockHash(*hash) {
// 		p.retargetChan <- hash
// 	}
// }

func (p *BlockPrefetchClient) retarget(hash string) {
	if hash == "" {
		return
	}

	if p.next.headerResult != nil && hash == p.next.headerResult.Hash {
		return
	}
	// Do not go backwards
	if p.current.headerResult != nil && hash == p.current.headerResult.Hash {
		return
	}

	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		log.Errorf("Invalid hash %s: %v", hash, err)
		return
	}

	p.retargetChan <- h
}
