// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package notification

import (
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v3/api/insight"
	"github.com/decred/dcrdata/v3/blockdata"
	"github.com/decred/dcrdata/v3/db/dcrpg"
	"github.com/decred/dcrdata/v3/db/dcrsqlite"
	"github.com/decred/dcrdata/v3/explorer"
	"github.com/decred/dcrdata/v3/mempool"
	"github.com/decred/dcrdata/v3/stakedb"
	"github.com/decred/dcrwallet/wallet/udb"
)

// RegisterNodeNtfnHandlers registers with dcrd to receive new block,
// transaction and winning ticket notifications.
func RegisterNodeNtfnHandlers(dcrdClient *rpcclient.Client) *ContextualError {
	var err error
	// Register for block connection and chain reorg notifications.
	if err = dcrdClient.NotifyBlocks(); err != nil {
		return newContextualError("block notification "+
			"registration failed", err)
	}

	// Register for tx accepted into mempool ntfns
	if err = dcrdClient.NotifyNewTransactions(true); err != nil {
		return newContextualError("new transaction verbose notification registration failed", err)
	}

	// For OnNewTickets
	//  Commented since there is a bug in rpcclient/notify.go
	// dcrdClient.NotifyNewTickets()

	if err = dcrdClient.NotifyWinningTickets(); err != nil {
		return newContextualError("winning ticket "+
			"notification registration failed", err)
	}

	// Register a Tx filter for addresses (receiving).  The filter applies to
	// OnRelevantTxAccepted.
	// TODO: register outpoints (third argument).
	// if len(addresses) > 0 {
	// 	if err = dcrdClient.LoadTxFilter(true, addresses, nil); err != nil {
	// 		return newContextualError("load tx filter failed", err)
	// 	}
	// }

	return nil
}

type blockHashHeight struct {
	hash     chainhash.Hash
	height   int64
	prevHash chainhash.Hash
}

type collectionQueue struct {
	q            chan *blockHashHeight
	syncHandlers []func(hash *chainhash.Hash)
	prevHash     chainhash.Hash
	prevHeight   int64
}

// NewCollectionQueue creates a new collectionQueue with a queue channel large
// enough for 10 million block pointers.
func NewCollectionQueue() *collectionQueue {
	return &collectionQueue{
		q: make(chan *blockHashHeight, 1e7),
	}
}

// SetSynchronousHandlers sets the slice of synchronous new block handler
// functions, which are called in the order they occur in the slice.
func (q *collectionQueue) SetSynchronousHandlers(syncHandlers []func(hash *chainhash.Hash)) {
	q.syncHandlers = syncHandlers
}

func (q *collectionQueue) SetPreviousBlock(prevHash chainhash.Hash, prevHeight int64) {
	q.prevHash = prevHash
	q.prevHeight = prevHeight
}

// ProcessBlocks receives new *blockHashHeights, calls the synchronous handlers,
// then signals to the monitors that a new block was mined.
func (q *collectionQueue) ProcessBlocks() {
	// Process queued blocks one at a time.
	for bh := range q.q {
		hash := bh.hash
		height := bh.height

		// Ensure that the received block (bh.hash, bh.height) connects to the
		// previously connected block (q.prevHash, q.prevHeight).
		if bh.prevHash != q.prevHash {
			log.Infof("Received block at %d (%v) does not connect to %d (%v)."+
				"This is normal before reorganization.",
				height, hash, q.prevHeight, q.prevHash)
			continue
		}

		start := time.Now()

		// Run synchronous block connected handlers in order.
		for _, h := range q.syncHandlers {
			h(&hash)
		}

		q.prevHash = hash
		q.prevHeight = height

		log.Debugf("Synchronous handlers of collectionQueue.ProcessBlocks() completed in %v", time.Since(start))

		// Signal to mempool monitors that a block was mined.
		select {
		case NtfnChans.NewTxChan <- &mempool.NewTx{
			Hash: nil,
			T:    time.Now(),
		}:
		default:
		}

		select {
		case NtfnChans.ExpNewTxChan <- &explorer.NewMempoolTx{
			Hex: "",
		}:
		default:
		}

		// API status update handler
		select {
		case NtfnChans.UpdateStatusNodeHeight <- uint32(height):
		default:
		}
	}
}

// MakeNodeNtfnHandlers defines the dcrd notification handlers
func MakeNodeNtfnHandlers() (*rpcclient.NotificationHandlers, *collectionQueue) {
	blockQueue := NewCollectionQueue()
	go blockQueue.ProcessBlocks()
	return &rpcclient.NotificationHandlers{
		OnBlockConnected: func(blockHeaderSerialized []byte, transactions [][]byte) {
			blockHeader := new(wire.BlockHeader)
			err := blockHeader.FromBytes(blockHeaderSerialized)
			if err != nil {
				log.Error("Failed to deserialize blockHeader in new block notification: "+
					"%v", err)
				return
			}
			height := int32(blockHeader.Height)
			hash := blockHeader.BlockHash()
			prevHash := blockHeader.PrevBlock // to ensure this is the next block

			log.Tracef("OnBlockConnected: %d / %v (previous: %v)", height, hash, prevHash)

			// Queue this block.
			blockQueue.q <- &blockHashHeight{
				hash:     hash,
				height:   int64(height),
				prevHash: prevHash,
			}
		},
		OnBlockDisconnected: func(blockHeaderSerialized []byte) {
			blockHeader := new(wire.BlockHeader)
			err := blockHeader.FromBytes(blockHeaderSerialized)
			if err != nil {
				log.Error("Failed to deserialize blockHeader in block disconnect notification: "+
					"%v", err)
				return
			}
			height := int32(blockHeader.Height)
			hash := blockHeader.BlockHash()

			log.Tracef("OnBlockDisconnected: %d / %v", height, hash)
		},
		OnReorganization: func(oldHash *chainhash.Hash, oldHeight int32,
			newHash *chainhash.Hash, newHeight int32) {
			wg := new(sync.WaitGroup)
			log.Tracef("OnReorganization: %d / %s --> %d / %s",
				oldHeight, oldHash, newHeight, newHash)

			// Send reorg data to dcrsqlite's monitor
			wg.Add(1)
			select {
			case NtfnChans.ReorgChanWiredDB <- &dcrsqlite.ReorgData{
				OldChainHead:   *oldHash,
				OldChainHeight: oldHeight,
				NewChainHead:   *newHash,
				NewChainHeight: newHeight,
				WG:             wg,
			}:
			default:
				wg.Done()
			}

			// Send reorg data to blockdata's monitor (so that it stops collecting)
			wg.Add(1)
			select {
			case NtfnChans.ReorgChanBlockData <- &blockdata.ReorgData{
				OldChainHead:   *oldHash,
				OldChainHeight: oldHeight,
				NewChainHead:   *newHash,
				NewChainHeight: newHeight,
				WG:             wg,
			}:
			default:
				wg.Done()
			}

			// Send reorg data to stakedb's monitor
			wg.Add(1)
			select {
			case NtfnChans.ReorgChanStakeDB <- &stakedb.ReorgData{
				OldChainHead:   *oldHash,
				OldChainHeight: oldHeight,
				NewChainHead:   *newHash,
				NewChainHeight: newHeight,
				WG:             wg,
			}:
			default:
				wg.Done()
			}
			wg.Wait()

			// Send reorg data to ChainDB's monitor
			wg.Add(1)
			select {
			case NtfnChans.ReorgChanDcrpgDB <- &dcrpg.ReorgData{
				OldChainHead:   *oldHash,
				OldChainHeight: oldHeight,
				NewChainHead:   *newHash,
				NewChainHeight: newHeight,
				WG:             wg,
			}:
			default:
				wg.Done()
			}
			wg.Wait()
		},

		OnWinningTickets: func(blockHash *chainhash.Hash, blockHeight int64,
			tickets []*chainhash.Hash) {
			var txstr []string
			for _, t := range tickets {
				txstr = append(txstr, t.String())
			}
			log.Tracef("Winning tickets: %v", strings.Join(txstr, ", "))
		},
		// maturing tickets. Thanks for fixing the tickets type bug, jolan!
		OnNewTickets: func(hash *chainhash.Hash, height int64, stakeDiff int64,
			tickets []*chainhash.Hash) {
			for _, tick := range tickets {
				log.Tracef("Mined new ticket: %v", tick.String())
			}
		},
		// OnRelevantTxAccepted is invoked when a transaction containing a
		// registered address is inserted into mempool.
		OnRelevantTxAccepted: func(transaction []byte) {
			rec, err := udb.NewTxRecord(transaction, time.Now())
			if err != nil {
				return
			}
			tx := dcrutil.NewTx(&rec.MsgTx)
			txHash := rec.Hash
			select {
			case NtfnChans.RelevantTxMempoolChan <- tx:
				log.Debugf("Detected transaction %v in mempool containing registered address.",
					txHash.String())
			default:
			}
		},

		// OnTxAcceptedVerbose is invoked same as OnTxAccepted but is used here
		// for the mempool monitors to avoid an extra call to dcrd for
		// the tx details
		OnTxAcceptedVerbose: func(txDetails *dcrjson.TxRawResult) {

			select {
			case NtfnChans.ExpNewTxChan <- &explorer.NewMempoolTx{
				Time: time.Now().Unix(),
				Hex:  txDetails.Hex,
			}:
			default:
				log.Warn("expNewTxChan buffer full!")
			}

			select {
			case NtfnChans.InsightNewTxChan <- &insight.NewTx{
				Hex:   txDetails.Hex,
				Vouts: txDetails.Vout,
			}:
			default:
				if NtfnChans.InsightNewTxChan != nil {
					log.Warn("InsightNewTxChan buffer full!")
				}
			}

			hash, _ := chainhash.NewHashFromStr(txDetails.Txid)
			select {
			case NtfnChans.NewTxChan <- &mempool.NewTx{
				Hash: hash,
				T:    time.Now(),
			}:
			default:
				log.Warn("NewTxChan buffer full!")
			}
		},
	}, blockQueue
}
