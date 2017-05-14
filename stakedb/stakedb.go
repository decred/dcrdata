// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package stakedb

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

type StakeDatabase struct {
	params     *chaincfg.Params
	NodeClient *dcrrpcclient.Client
	nodeMtx    sync.RWMutex
	StakeDB    database.DB
	BestNode   *stake.Node
	blkMtx     sync.RWMutex
	blockCache map[int64]*dcrutil.Block
}

const (
	// dbType is the database backend type to use
	dbType = "ffldb"
	// DefaultStakeDbName is the default database name
	DefaultStakeDbName = "ffldb_stake"
)

func NewStakeDatabase(client *dcrrpcclient.Client, params *chaincfg.Params) (*StakeDatabase, error) {
	sDB := &StakeDatabase{
		params:     params,
		NodeClient: client,
		blockCache: make(map[int64]*dcrutil.Block),
	}
	if err := sDB.Open(); err != nil {
		return nil, err
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

func (db *StakeDatabase) ForgetBlock(ind int64) {
	db.blkMtx.Lock()
	defer db.blkMtx.Unlock()
	delete(db.blockCache, ind)
}

func (db *StakeDatabase) ConnectBlockHeight(height int64) (*dcrutil.Block, error) {
	block, _ := db.Block(height)
	return block, db.ConnectBlock(block)
}

func (db *StakeDatabase) ConnectBlockHash(hash *chainhash.Hash) (*dcrutil.Block, error) {
	block, err := db.NodeClient.GetBlock(hash)
	if err != nil {
		return nil, err
	}
	return block, db.ConnectBlock(block)
}

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

	// If the stake db is ahead, it was probably a reorg, so rewind before
	// adding the new block
	bestNodeHeight := int64(db.Height())
	for height <= bestNodeHeight {
		log.Infof("Disconnecting block %d.", bestNodeHeight)
		if err := db.DisconnectBlock(); err != nil {
			return err
		}
		bestNodeHeight = int64(db.Height())
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

func (db *StakeDatabase) DisconnectBlock() error {
	db.nodeMtx.Lock()
	defer db.nodeMtx.Unlock()

	formerBestNode := db.BestNode
	parentBlock, _ := db.Block(int64(db.Height()) - 1)
	if parentBlock == nil {
		return fmt.Errorf("Unable to get parent block")
	}

	// previous best node
	err := db.StakeDB.View(func(dbTx database.Tx) error {
		var errLocal error
		db.BestNode, errLocal = db.BestNode.DisconnectNode(
			parentBlock.MsgBlock().Header, nil, nil, dbTx)
		return errLocal
	})
	if err != nil {
		return err
	}

	return db.StakeDB.Update(func(dbTx database.Tx) error {
		return stake.WriteDisconnectedBestNode(dbTx, db.BestNode,
			*parentBlock.Hash(), formerBestNode.UndoData())
	})
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
