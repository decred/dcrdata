// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package rpcutils

import (
	"fmt"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
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
}

type MasterBlockGetter interface {
	BlockGetter
	UpdateToBestBlock() (*dcrutil.Block, error)
	UpdateToNextBlock() (*dcrutil.Block, error)
	UpdateToBlock(height int64) (*dcrutil.Block, error)
}

// BlockGate is an implementation of MasterBlockGetter with cache
type BlockGate struct {
	sync.RWMutex
	client        *rpcclient.Client
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

// ensure BlockGate satisfies BlockGetter
var _ BlockGetter = (*BlockGate)(nil)

func NewBlockGate(client *rpcclient.Client, capacity int) *BlockGate {
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

func (g *BlockGate) SetFetchToHeight(height int64) {
	g.RLock()
	defer g.RUnlock()
	g.fetchToHeight = height
}

func (g *BlockGate) NodeHeight() (int64, error) {
	_, height, err := g.client.GetBestBlock()
	return height, err
}

func (g *BlockGate) BestBlockHeight() int64 {
	g.RLock()
	defer g.RUnlock()
	return g.height
}

func (g *BlockGate) BestBlockHash() (chainhash.Hash, int64, error) {
	g.RLock()
	defer g.RUnlock()
	var err error
	hash, ok := g.hashAtHeight[g.height]
	if !ok {
		err = fmt.Errorf("hash of best block %d not found", g.height)
	}
	return hash, g.height, err
}

func (g *BlockGate) BestBlock() (*dcrutil.Block, error) {
	g.RLock()
	defer g.RUnlock()
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

func (g *BlockGate) Block(hash chainhash.Hash) (*dcrutil.Block, error) {
	g.RLock()
	defer g.RUnlock()
	var err error
	block, ok := g.blockWithHash[hash]
	if !ok {
		err = fmt.Errorf("block %d not found", hash)
	}
	return block, err
}

func (g *BlockGate) UpdateToBestBlock() (*dcrutil.Block, error) {
	_, height, err := g.client.GetBestBlock()
	if err != nil {
		return nil, fmt.Errorf("GetBestBlockHash failed: %v", err)
	}

	return g.UpdateToBlock(height)
}

func (g *BlockGate) UpdateToNextBlock() (*dcrutil.Block, error) {
	g.Lock()
	height := g.height + 1
	g.Unlock()
	return g.UpdateToBlock(height)
}

func (g *BlockGate) UpdateToBlock(height int64) (*dcrutil.Block, error) {
	g.Lock()
	defer g.Unlock()
	return g.updateToBlock(height)
}

func (g *BlockGate) updateToBlock(height int64) (*dcrutil.Block, error) {
	block, hash, err := GetBlock(height, g.client)
	if err != nil {
		return nil, fmt.Errorf("GetBlock (%d) failed: %v", height, err)
	}

	g.height = height
	g.hashAtHeight[height] = *hash
	g.blockWithHash[*hash] = block

	// Push the new block onto the expiration queue, and remove and old ones if
	// above capacity.
	g.rotateIn(height, *hash)

	defer func() {
		go g.signalHeight(height)
		go g.signalHash(*hash)
	}()

	return block, nil
}

func (g *BlockGate) rotateIn(height int64, hash chainhash.Hash) {
	// Push this new height-hash pair onto the queue
	g.expireQueue.q = append(g.expireQueue.q, heightHashPair{height, hash})
	// If above capacity, pop the oldest off
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

func (g *BlockGate) signalHash(blockhash chainhash.Hash) {
	g.Lock()
	defer g.Unlock()

	block, ok := g.blockWithHash[blockhash]
	if !ok {
		panic("g.blockWithHash[hash] not OK in signalHash")
	}
	height := block.Height()

	waitChans := g.hashWaiters[blockhash]
	for _, c := range waitChans {
		select {
		case c <- height:
		default:
			panic(fmt.Sprintf("unable to signal block with hash %s at height %d", blockhash, height))
		}
	}

	delete(g.hashWaiters, blockhash)
}

func (g *BlockGate) signalHeight(height int64) {
	g.Lock()
	defer g.Unlock()

	blockhash, ok := g.hashAtHeight[height]
	if !ok {
		panic("g.hashAtHeight[height] not OK in signalHeight")
	}

	waitChans := g.heightWaiters[height]
	for _, c := range waitChans {
		select {
		case c <- blockhash:
		default:
			panic(fmt.Sprintf("unable to signal block with hash %s at height %d", blockhash, height))
		}
	}

	delete(g.heightWaiters, height)
}

func (g *BlockGate) WaitForHeight(height int64) chan chainhash.Hash {
	g.Lock()
	defer g.Unlock()

	if height < 0 {
		return nil
	}

	waitChain := make(chan chainhash.Hash, 1)
	g.heightWaiters[height] = append(g.heightWaiters[height], waitChain)
	if height <= g.fetchToHeight {
		g.updateToBlock(height)
	}
	return waitChain
}

func (g *BlockGate) WaitForHash(hash chainhash.Hash) chan int64 {
	g.Lock()
	defer g.Unlock()

	waitChain := make(chan int64, 4)
	g.hashWaiters[hash] = append(g.hashWaiters[hash], waitChain)
	if hash == g.hashAtHeight[g.height] {
		go g.signalHash(hash)
	}
	return waitChain
}
