// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package stakedb

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

type StakeDatabase struct {
	params          *chaincfg.Params
	NodeClient      *dcrrpcclient.Client
	nodeMtx         sync.RWMutex
	StakeDB         database.DB
	BestNode        *stake.Node
	blkMtx          sync.RWMutex
	blockCache      map[int64]*dcrutil.Block
	liveTicketCache map[chainhash.Hash]int64
}

const (
	// dbType is the database backend type to use
	dbType = "ffldb"
	// DefaultStakeDbName is the default database name
	DefaultStakeDbName = "ffldb_stake"
)

func NewStakeDatabase(client *dcrrpcclient.Client, params *chaincfg.Params) (*StakeDatabase, error) {
	sDB := &StakeDatabase{
		params:          params,
		NodeClient:      client,
		blockCache:      make(map[int64]*dcrutil.Block),
		liveTicketCache: make(map[chainhash.Hash]int64),
	}
	if err := sDB.Open(); err != nil {
		return nil, err
	}

	liveTickets, err := sDB.NodeClient.LiveTickets()
	if err != nil {
		return sDB, err
	}

	log.Info("Pre-populating live ticket cache...")
	for _, hash := range liveTickets {
		var txid *dcrutil.Tx
		txid, err = sDB.NodeClient.GetRawTransaction(hash)
		if err != nil {
			log.Errorf("Unable to get transaction %v: %v\n", hash, err)
			continue
		}
		// This isn't quite right for pool tickets where the small
		// pool fees are included in vout[0], but it's close.
		sDB.liveTicketCache[*hash] = txid.MsgTx().TxOut[0].Value
	}

	return sDB, nil
}

func (db *StakeDatabase) Height() uint32 {
	if db == nil || db.BestNode == nil {
		log.Error("Stake database not yet opened")
		return 0
	}
	db.nodeMtx.RLock()
	defer db.nodeMtx.RUnlock()
	return db.BestNode.Height()
}

func (db *StakeDatabase) Header() (*wire.BlockHeader, int64) {
	if db == nil || db.BestNode == nil {
		log.Error("Stake database not yet opened")
		return nil, -1
	}
	db.nodeMtx.RLock()
	height := int64(db.BestNode.Height())
	db.nodeMtx.RUnlock()
	block, _ := db.Block(height)
	header := block.MsgBlock().Header
	return &header, height
}

// Block first tries to find the block at the input height in cache, and if that
// fails it will request it from the node RPC client.
func (db *StakeDatabase) Block(ind int64) (*dcrutil.Block, bool) {
	db.blkMtx.RLock()
	block, ok := db.blockCache[ind]
	db.blkMtx.RUnlock()
	//log.Info(ind, block, ok)
	if !ok {
		var err error
		block, _, err = rpcutils.GetBlock(ind, db.NodeClient)
		if err != nil {
			log.Error(err)
			return nil, false
		}
	}
	return block, ok
}

// ForgetBlock deletes the block with the input height from the block cache.
func (db *StakeDatabase) ForgetBlock(ind int64) {
	db.blkMtx.Lock()
	defer db.blkMtx.Unlock()
	delete(db.blockCache, ind)
}

// ConnectBlockHeight is a wrapper for ConnectBlock.  For the input height, it
// fetches either the cached block with that height or requests it from the node
// RPC client. Use ConnectBlockHash instead when possible.
func (db *StakeDatabase) ConnectBlockHeight(height int64) (*dcrutil.Block, error) {
	block, _ := db.Block(height)
	return block, db.ConnectBlock(block)
}

// ConnectBlockHash is a wrapper for ConnectBlock. For the input block hash, it
// gets the block from the node RPC client and calls ConnectBlock.
func (db *StakeDatabase) ConnectBlockHash(hash *chainhash.Hash) (*dcrutil.Block, error) {
	block, err := db.NodeClient.GetBlock(hash)
	if err != nil {
		return nil, err
	}
	return block, db.ConnectBlock(block)
}

// ConnectBlock connects the input block to the tip of the stake DB and updates
// the best stake node. This exported function gets any revoked and spend
// tickets from the input block, and any maturing tickets from the past block in
// which those tickets would be found, and passes them to connectBlock.
func (db *StakeDatabase) ConnectBlock(block *dcrutil.Block) error {
	height := block.Height()
	maturingHeight := height - int64(db.params.TicketMaturity)

	var maturingTickets []chainhash.Hash
	if maturingHeight >= 0 {
		maturingBlock, wasCached := db.Block(maturingHeight)
		if wasCached {
			db.ForgetBlock(maturingHeight)
		}
		maturingTickets = txhelpers.TicketsInBlock(maturingBlock)
	}

	db.blkMtx.Lock()
	db.blockCache[block.Height()] = block
	db.blkMtx.Unlock()

	revokedTickets := txhelpers.RevokedTicketsInBlock(block)
	spentTickets := txhelpers.TicketsSpentInBlock(block)

	// If the stake db is ahead, it was probably a reorg, unhandled!
	db.nodeMtx.Lock()
	bestNodeHeight := int64(db.BestNode.Height())
	db.nodeMtx.Unlock()
	if height <= bestNodeHeight {
		return fmt.Errorf("cannot connect block height %d at height %d", height, bestNodeHeight)
	}

	return db.connectBlock(block, spentTickets, revokedTickets, maturingTickets)
}

func (db *StakeDatabase) connectBlock(block *dcrutil.Block, spent []chainhash.Hash,
	revoked []chainhash.Hash, maturing []chainhash.Hash) error {
	db.nodeMtx.Lock()
	defer db.nodeMtx.Unlock()

	var err error
	db.BestNode, err = db.BestNode.ConnectNode(block.MsgBlock().Header,
		spent, revoked, maturing)
	if err != nil {
		return err
	}

	// Write the new node to db
	return db.StakeDB.Update(func(dbTx database.Tx) error {
		return stake.WriteConnectedBestNode(dbTx, db.BestNode, *block.Hash())
	})
}

// DisconnectBlock attempts to disconnect the current best block from the stake
// DB and updates the best stake node.
func (db *StakeDatabase) DisconnectBlock() error {
	db.nodeMtx.Lock()
	defer db.nodeMtx.Unlock()

	return db.disconnectBlock()
}

// disconnectBlock is the non-thread-safe version of DisconnectBlock.
func (db *StakeDatabase) disconnectBlock() error {
	prunedTipBlockHeight := db.BestNode.Height()
	parentBlock, _ := db.Block(int64(prunedTipBlockHeight) - 1)
	if parentBlock == nil {
		return fmt.Errorf("Unable to get parent block")
	}
	childUndoData := append(stake.UndoTicketDataSlice(nil), db.BestNode.UndoData()...)

	log.Debugf("Disconnecting block %d.", prunedTipBlockHeight)

	// previous best node
	var parentStakeNode *stake.Node
	err := db.StakeDB.View(func(dbTx database.Tx) error {
		var errLocal error
		parentStakeNode, errLocal = db.BestNode.DisconnectNode(
			parentBlock.MsgBlock().Header, nil, nil, dbTx)
		return errLocal
	})
	if err != nil {
		return err
	}
	db.BestNode = parentStakeNode

	return db.StakeDB.Update(func(dbTx database.Tx) error {
		return stake.WriteDisconnectedBestNode(dbTx, parentStakeNode,
			*parentBlock.Hash(), childUndoData)
	})
}

// DisconnectBlocks disconnects N blocks from the head of the chain.
func (db *StakeDatabase) DisconnectBlocks(count int64) error {
	db.nodeMtx.Lock()
	defer db.nodeMtx.Unlock()

	for i := int64(0); i < count; i++ {
		if err := db.disconnectBlock(); err != nil {
			return err
		}
	}

	return nil
}

func (db *StakeDatabase) Open() error {
	db.nodeMtx.Lock()
	defer db.nodeMtx.Unlock()

	// Create a new database to store the accepted stake node data into.
	dbName := DefaultStakeDbName
	var err error
	db.StakeDB, err = database.Open(dbType, dbName, db.params.Net)
	if err != nil {
		log.Infof("Unable to open stake DB (%v). Removing and creating new.", err)
		_ = os.RemoveAll(dbName)
		db.StakeDB, err = database.Create(dbType, dbName, db.params.Net)
		if err != nil {
			// do not return nil interface, but interface of nil DB
			return fmt.Errorf("error creating db: %v", err)
		}
	}

	// Load the best block from stake db
	var bestNodeHeight = int64(-1)
	err = db.StakeDB.View(func(dbTx database.Tx) error {
		v := dbTx.Metadata().Get([]byte("stakechainstate"))
		if v == nil {
			return fmt.Errorf("missing key for chain state data")
		}

		var stakeDBHash chainhash.Hash
		copy(stakeDBHash[:], v[:chainhash.HashSize])
		offset := chainhash.HashSize
		stakeDBHeight := binary.LittleEndian.Uint32(v[offset : offset+4])
		bestNodeHeight = int64(stakeDBHeight)

		var errLocal error
		block, errLocal := db.NodeClient.GetBlock(&stakeDBHash)
		if errLocal != nil {
			return fmt.Errorf("GetBlock failed (%s): %v", stakeDBHash, errLocal)
		}
		header := block.MsgBlock().Header

		db.BestNode, errLocal = stake.LoadBestNode(dbTx, stakeDBHeight,
			stakeDBHash, header, db.params)
		return errLocal
	})
	if err != nil {
		log.Errorf("Error reading from database (%v).  Reinitializing.", err)
		err = db.StakeDB.Update(func(dbTx database.Tx) error {
			var errLocal error
			db.BestNode, errLocal = stake.InitDatabaseState(dbTx, db.params)
			return errLocal
		})
		log.Debug("Created new stake db.")
	} else {
		log.Debug("Opened existing stake db.")
	}

	return err
}

func (db *StakeDatabase) PoolInfo() apitypes.TicketPoolInfo {
	poolSize := db.BestNode.PoolSize()
	liveTickets := db.BestNode.LiveTickets()

	var poolValue int64
	for _, hash := range liveTickets {
		val, ok := db.liveTicketCache[hash]
		if !ok {
			txid, err := db.NodeClient.GetRawTransaction(&hash)
			if err != nil {
				log.Errorf("Unable to get transaction %v: %v\n", hash, err)
				continue
			}
			// This isn't quite right for pool tickets where the small
			// pool fees are included in vout[0], but it's close.
			db.liveTicketCache[hash] = txid.MsgTx().TxOut[0].Value
		}
		poolValue += val
	}

	header, _ := db.Header()
	if int(header.PoolSize) != len(liveTickets) {
		log.Warnf("Inconsistent pool sizes: %d, %d", header.PoolSize, len(liveTickets))
	}

	poolCoin := dcrutil.Amount(poolValue).ToCoin()
	valAvg := 0.0
	if header.PoolSize > 0 {
		valAvg = poolCoin / float64(poolSize)
	}

	return apitypes.TicketPoolInfo{
		Size:   uint32(poolSize),
		Value:  poolCoin,
		ValAvg: valAvg,
	}
}
