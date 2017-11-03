// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/db/dcrsqlite"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/dcrdata/dcrdata/stakedb"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
)

const (
	// blockConnChanBuffer is the size of the block connected channel buffers.
	blockConnChanBuffer = 64

	// newTxChanBuffer is the size of the new transaction channel buffer, for
	// ANY transactions are added into mempool.
	newTxChanBuffer = 48

	reorgBuffer = 2

	// relevantMempoolTxChanBuffer is the size of the new transaction channel
	// buffer, for relevant transactions that are added into mempool.
	//relevantMempoolTxChanBuffer = 2048
)

// Channels are package-level variables for simplicity
var ntfnChans struct {
	connectChan                       chan *chainhash.Hash
	reorgChanBlockData                chan *blockdata.ReorgData
	connectChanWiredDB                chan *chainhash.Hash
	reorgChanWiredDB                  chan *dcrsqlite.ReorgData
	connectChanStakeDB                chan *chainhash.Hash
	reorgChanStakeDB                  chan *stakedb.ReorgData
	updateStatusNodeHeight            chan uint32
	updateStatusDBHeight              chan uint32
	spendTxBlockChan, recvTxBlockChan chan *txhelpers.BlockWatchedTx
	relevantTxMempoolChan             chan *dcrutil.Tx
	newTxChan                         chan *mempool.NewTx
}

func makeNtfnChans(cfg *config) {
	// If we're monitoring for blocks OR collecting block data, these channels
	// are necessary to handle new block notifications. Otherwise, leave them
	// as nil so that both a send (below) blocks and a receive (in
	// blockConnectedHandler) block. default case makes non-blocking below.
	// quit channel case manages blockConnectedHandlers.
	ntfnChans.connectChan = make(chan *chainhash.Hash, blockConnChanBuffer)

	// WiredDB channel for connecting new blocks
	ntfnChans.connectChanWiredDB = make(chan *chainhash.Hash, blockConnChanBuffer)

	// Stake DB channel for connecting new blocks - BLOCKING!
	ntfnChans.connectChanStakeDB = make(chan *chainhash.Hash)

	// Reorg data channels
	ntfnChans.reorgChanBlockData = make(chan *blockdata.ReorgData, reorgBuffer)
	ntfnChans.reorgChanWiredDB = make(chan *dcrsqlite.ReorgData, reorgBuffer)
	ntfnChans.reorgChanStakeDB = make(chan *stakedb.ReorgData, reorgBuffer)

	// To update app status
	ntfnChans.updateStatusNodeHeight = make(chan uint32, blockConnChanBuffer)
	ntfnChans.updateStatusDBHeight = make(chan uint32, blockConnChanBuffer)

	// watchaddress
	// if len(cfg.WatchAddresses) > 0 {
	// // recv/spendTxBlockChan come with connected blocks
	// 	ntfnChans.recvTxBlockChan = make(chan *txhelpers.BlockWatchedTx, blockConnChanBuffer)
	// 	ntfnChans.spendTxBlockChan = make(chan *txhelpers.BlockWatchedTx, blockConnChanBuffer)
	// 	ntfnChans.relevantTxMempoolChan = make(chan *dcrutil.Tx, relevantMempoolTxChanBuffer)
	// }

	if cfg.MonitorMempool {
		ntfnChans.newTxChan = make(chan *mempool.NewTx, newTxChanBuffer)
	}
}

func closeNtfnChans() {
	if ntfnChans.connectChan != nil {
		close(ntfnChans.connectChan)
	}
	if ntfnChans.connectChanWiredDB != nil {
		close(ntfnChans.connectChanWiredDB)
	}
	if ntfnChans.connectChanStakeDB != nil {
		close(ntfnChans.connectChanStakeDB)
	}

	if ntfnChans.reorgChanBlockData != nil {
		close(ntfnChans.reorgChanBlockData)
	}
	if ntfnChans.reorgChanWiredDB != nil {
		close(ntfnChans.reorgChanWiredDB)
	}
	if ntfnChans.reorgChanStakeDB != nil {
		close(ntfnChans.reorgChanStakeDB)
	}

	if ntfnChans.updateStatusNodeHeight != nil {
		close(ntfnChans.updateStatusNodeHeight)
	}
	if ntfnChans.updateStatusDBHeight != nil {
		close(ntfnChans.updateStatusDBHeight)
	}

	if ntfnChans.newTxChan != nil {
		close(ntfnChans.newTxChan)
	}
	if ntfnChans.relevantTxMempoolChan != nil {
		close(ntfnChans.relevantTxMempoolChan)
	}

	if ntfnChans.spendTxBlockChan != nil {
		close(ntfnChans.spendTxBlockChan)
	}
	if ntfnChans.recvTxBlockChan != nil {
		close(ntfnChans.recvTxBlockChan)
	}
}
