// Copyright (c) 2017-2019, The dcrdata developers
// See LICENSE for details.

package rpcutils

import (
	"fmt"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
)

// BlockGetter is an interface for requesting blocks
type BlockGetter interface {
	NodeHeight() (int64, error)
	BestBlockHeight() int64
	BestBlockHash() (chainhash.Hash, int64, error)
	BestBlock() (*dcrutil.Block, error)
	Block(chainhash.Hash) (*dcrutil.Block, error)
	WaitForHeight(int64) chan chainhash.Hash
	WaitForHash(chainhash.Hash) chan int64
	GetChainWork(*chainhash.Hash) (string, error)
}

// MasterBlockGetter builds on BlockGetter, adding functions that fetch blocks
// directly from dcrd via RPC and subsequently update the internal block cache
// with the retrieved block.
type MasterBlockGetter interface {
	BlockGetter
	UpdateToBestBlock() (*dcrutil.Block, error)
	UpdateToNextBlock() (*dcrutil.Block, error)
	UpdateToBlock(height int64) (*dcrutil.Block, error)
}

// BlockGate is an implementation of MasterBlockGetter with cache
type BlockGate struct {
	mtx           sync.RWMutex
	client        BlockFetcher
	height        int64
	fetchToHeight int64
	hashAtHeight  map[int64]chainhash.Hash
	blockWithHash map[chainhash.Hash]*dcrutil.Block
	heightWaiters map[int64][]chan chainhash.Hash
	hashWaiters   map[chainhash.Hash][]chan int64
	expireQueue   heightHashQueue
}

type heightHashQueue struct {
	q   []heightHashPair
	cap int
}

type heightHashPair struct {
	height int64
	hash   chainhash.Hash
}

func hashInQueue(q heightHashQueue, hash chainhash.Hash) bool {
	for i := range q.q {
		if q.q[i].hash == hash {
			return true
		}
	}
	return false
}

func heightInQueue(q heightHashQueue, height int64) bool {
	for i := range q.q {
		if q.q[i].height == height {
			return true
		}
	}
	return false
}

// Ensure BlockGate satisfies BlockGetter and MasterBlockGetter.
var _ BlockGetter = (*BlockGate)(nil)
var _ MasterBlockGetter = (*BlockGate)(nil)

// NewBlockGate constructs a new BlockGate, wrapping an RPC client, with a
// specified block cache capacity.
func NewBlockGate(client BlockFetcher, capacity int) *BlockGate {
	return &BlockGate{
		client:        client,
		height:        -1,
		fetchToHeight: -1,
		hashAtHeight:  make(map[int64]chainhash.Hash),
		blockWithHash: make(map[chainhash.Hash]*dcrutil.Block),
		heightWaiters: make(map[int64][]chan chainhash.Hash),
		hashWaiters:   make(map[chainhash.Hash][]chan int64),
		expireQueue: heightHashQueue{
			cap: capacity,
		},
	}
}

// SetFetchToHeight sets the height up to which WaitForHeight will trigger an
// RPC to retrieve the block immediately. For the given height and up,
// WaitForHeight will only return a notification channel.
func (g *BlockGate) SetFetchToHeight(height int64) {
	g.mtx.RLock()
	defer g.mtx.RUnlock()
	g.fetchToHeight = height
}

// NodeHeight gets the chain height from dcrd.
func (g *BlockGate) NodeHeight() (int64, error) {
	_, height, err := g.client.GetBestBlock()
	return height, err
}

// BestBlockHeight gets the best block height in the block cache.
func (g *BlockGate) BestBlockHeight() int64 {
	g.mtx.RLock()
	defer g.mtx.RUnlock()
	return g.height
}

// BestBlockHash gets the hash and height of the best block in cache.
func (g *BlockGate) BestBlockHash() (chainhash.Hash, int64, error) {
	g.mtx.RLock()
	defer g.mtx.RUnlock()
	var err error
	hash, ok := g.hashAtHeight[g.height]
	if !ok {
		err = fmt.Errorf("hash of best block %d not found", g.height)
	}
	return hash, g.height, err
}

// BestBlock gets the best block in cache.
func (g *BlockGate) BestBlock() (*dcrutil.Block, error) {
	g.mtx.RLock()
	defer g.mtx.RUnlock()
	var err error
	hash, ok := g.hashAtHeight[g.height]
	if !ok {
		err = fmt.Errorf("hash of best block %d not found", g.height)
	}
	block, ok := g.blockWithHash[hash]
	if !ok {
		err = fmt.Errorf("block %d at height %d not found", hash, g.height)
	}
	return block, err
}

// CachedBlock attempts to get the block with the specified hash from cache.
func (g *BlockGate) CachedBlock(hash chainhash.Hash) (*dcrutil.Block, error) {
	g.mtx.RLock()
	defer g.mtx.RUnlock()
	block, ok := g.blockWithHash[hash]
	if !ok {
		return nil, fmt.Errorf("block %d not found", hash)
	}
	return block, nil
}

// Block first attempts to get the block with the specified hash from cache. In
// the event of a cache miss, the block is retrieved from dcrd via RPC.
func (g *BlockGate) Block(hash chainhash.Hash) (*dcrutil.Block, error) {
	// Try block cache first.
	block, err := g.CachedBlock(hash)
	if err == nil {
		return block, nil
	}

	// Cache miss. Retrieve from dcrd RPC.
	block, err = GetBlockByHash(&hash, g.client)
	if err != nil {
		return nil, fmt.Errorf("GetBlock (%v) failed: %v", hash, err)
	}

	g.mtx.RLock()
	fmt.Printf("Block cache miss: requested %d, cache capacity %d, tip %d.",
		block.Height(), g.expireQueue.cap, g.height)
	g.mtx.RUnlock()
	return block, nil
}

// UpdateToBestBlock gets the best block via RPC and updates the cache.
func (g *BlockGate) UpdateToBestBlock() (*dcrutil.Block, error) {
	_, height, err := g.client.GetBestBlock()
	if err != nil {
		return nil, fmt.Errorf("GetBestBlockHash failed: %v", err)
	}

	return g.UpdateToBlock(height)
}

// UpdateToNextBlock gets the next block following the best in cache via RPC and
// updates the cache.
func (g *BlockGate) UpdateToNextBlock() (*dcrutil.Block, error) {
	g.mtx.Lock()
	height := g.height + 1
	g.mtx.Unlock()
	return g.UpdateToBlock(height)
}

// UpdateToBlock gets the block at the specified height on the main chain from
// dcrd, stores it in cache, and signals any waiters. This is the thread-safe
// version of updateToBlock.
func (g *BlockGate) UpdateToBlock(height int64) (*dcrutil.Block, error) {
	g.mtx.Lock()
	defer g.mtx.Unlock()
	return g.updateToBlock(height)
}

// updateToBlock gets the block at the specified height on the main chain from
// dcrd. It is not thread-safe. It wrapped by UpdateToBlock for thread-safety,
// and used directly by WaitForHeight which locks the BlockGate.
func (g *BlockGate) updateToBlock(height int64) (*dcrutil.Block, error) {
	block, hash, err := GetBlock(height, g.client)
	if err != nil {
		return nil, fmt.Errorf("GetBlock (%d) failed: %v", height, err)
	}

	g.height = height
	g.hashAtHeight[height] = *hash
	g.blockWithHash[*hash] = block

	// Push the new block onto the expiration queue, and remove any old ones if
	// above capacity.
	g.rotateIn(height, *hash)

	// Launch goroutines to signal to height and hash waiters.
	g.signalHeight(height, *hash)
	g.signalHash(*hash, height)

	return block, nil
}

// rotateIn puts the input height-hash pair into an expiration queue. If the
// queue is at capacity, the oldest entry in the queue is popped off, and the
// corresponding items in the blockWithHash and hashAtHeight maps are deleted.
// TODO: possibly check for hash and height waiters before deleting items.
// However, since signalHeight and signalHeight are run synchrounously after
// rotateIn in UpdateToBlock and WaitForHeight (both lock the BlockGate), there
// should be no such issues. At worst, their may be a cache miss when a client
// calls Block or CachedBlock.
func (g *BlockGate) rotateIn(height int64, hash chainhash.Hash) {
	// Push this new height-hash pair onto the queue.
	g.expireQueue.q = append(g.expireQueue.q, heightHashPair{height, hash})

	// If above capacity, pop the oldest off.
	if len(g.expireQueue.q) > g.expireQueue.cap {
		// Pop
		oldest := g.expireQueue.q[0]
		g.expireQueue.q = g.expireQueue.q[1:]

		// Remove the dropped height-hash pair from cache maps only if we don't
		// see it items in the queue with the same height or hash.
		if !hashInQueue(g.expireQueue, oldest.hash) {
			delete(g.blockWithHash, oldest.hash)
		}
		if !heightInQueue(g.expireQueue, oldest.height) {
			delete(g.hashAtHeight, oldest.height)
		}
	}
}

func (g *BlockGate) signalHash(hash chainhash.Hash, height int64) {
	// Get the hash waiter channels, and delete them from list.
	waitChans, ok := g.hashWaiters[hash]
	if !ok {
		return
	}
	delete(g.hashWaiters, hash)

	// Empty slice or nil slice may have been stored.
	if len(waitChans) == 0 {
		return
	}

	// Send the height to each of the hash waiters.
	go func() {
		for _, c := range waitChans {
			select {
			case c <- height:
			default:
				panic(fmt.Sprintf("unable to signal block with hash %s at height %d",
					hash, height))
			}
		}
	}()
}

func (g *BlockGate) signalHeight(height int64, hash chainhash.Hash) {
	waitChans, ok := g.heightWaiters[height]
	if !ok {
		return
	}
	delete(g.heightWaiters, height)

	// Empty slice or nil slice may have been stored.
	if len(waitChans) == 0 {
		return
	}

	// Send the hash to each of the height waiters.
	go func() {
		for _, c := range waitChans {
			select {
			case c <- hash:
			default:
				panic(fmt.Sprintf("unable to signal block with hash %s at height %d",
					hash, height))
			}
		}
	}()
}

// WaitForHeight provides a notification channel for signaling to the caller
// when the block at the specified height is available.
func (g *BlockGate) WaitForHeight(height int64) chan chainhash.Hash {
	g.mtx.Lock()
	defer g.mtx.Unlock()

	if height < 0 {
		return nil
	}

	waitChan := make(chan chainhash.Hash, 1)
	// Queue for future send.
	g.heightWaiters[height] = append(g.heightWaiters[height], waitChan)

	// If the block is already cached, send now.
	if hash, ok := g.hashAtHeight[height]; ok {
		g.signalHeight(height, hash)
	} else if height <= g.fetchToHeight {
		if _, err := g.updateToBlock(height); err != nil {
			fmt.Printf("Failed to updateToBlock: %v", err)
			return nil
		}
	} else if height < g.height {
		fmt.Printf("WARNING: WaitForHeight(%d), but the best block is at %d. "+
			"You may wait forever for this block.",
			height, g.height)
	}

	return waitChan
}

// WaitForHash provides a notification channel for signaling to the caller
// when the block with the specified hash is available.
func (g *BlockGate) WaitForHash(hash chainhash.Hash) chan int64 {
	g.mtx.Lock()
	defer g.mtx.Unlock()

	waitChan := make(chan int64, 1)

	// Queue for future send.
	g.hashWaiters[hash] = append(g.hashWaiters[hash], waitChan)

	// If the block is already cached, send now.
	if block, ok := g.blockWithHash[hash]; ok {
		g.signalHash(hash, block.Height())
	}

	return waitChan
}

// GetChainWork fetches the dcrjson.BlockHeaderVerbose and returns only the
// ChainWork attribute as a string.
func (g *BlockGate) GetChainWork(hash *chainhash.Hash) (string, error) {
	return GetChainWork(g.client, hash)
}

// Client is just an access function to get the BlockGate's RPC client.
func (g *BlockGate) Client() BlockFetcher {
	return g.client
}
