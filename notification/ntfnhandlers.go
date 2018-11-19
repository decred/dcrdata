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
	"github.com/decred/dcrdata/v3/explorer"
	"github.com/decred/dcrdata/v3/mempool"
	"github.com/decred/dcrdata/v3/rpcutils"
	"github.com/decred/dcrdata/v3/txhelpers"
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

// ReorgData contains
type ReorgData struct {
	OldChainHead   chainhash.Hash
	OldChainHeight int32
	NewChainHead   chainhash.Hash
	NewChainHeight int32
	WG             *sync.WaitGroup
}

// reorgDataChan relays details of a chain reorganization to reorgSignaler via
// queueReorg.
var reorgDataChan = make(chan ReorgData, 16)

// queueReorg puts the provided ReorgData into the queue for signaling to each
// package's reorg handler.
func queueReorg(reorgData ReorgData) {
	// Do not block, even if the channel buffer is full.
	go func() { reorgDataChan <- reorgData }()
}

// ReorgSignaler processes ReorgData sent on reorgDataChan (use queueReorg to
// send), allowing each package's reorg handler to finish before proceeding to
// the next reorg in the queue. This should be launched as a goroutine.
func ReorgSignaler(dcrdClient *rpcclient.Client) {
	for d := range reorgDataChan {
		// Determine the common ancestor of the two chains, and get the full
		// list of blocks in each chain back to but not including the common
		// ancestor.
		ancestor, newChain, oldChain, err := rpcutils.CommonAncestor(dcrdClient,
			d.NewChainHead, d.OldChainHead)
		if err != nil {
			log.Errorf("Failed to determine common ancestor. Aborting reorg.")
			continue
		}

		fullData := &txhelpers.ReorgData{
			CommonAncestor: *ancestor,
			NewChain:       newChain,
			NewChainHead:   d.NewChainHead,
			NewChainHeight: d.NewChainHeight,
			OldChain:       oldChain,
			OldChainHead:   d.OldChainHead,
			OldChainHeight: d.OldChainHeight,
			WG:             d.WG,
		}

		// Send reorg data to dcrsqlite's monitor
		d.WG.Add(1)
		select {
		case NtfnChans.ReorgChanWiredDB <- fullData:
		default:
			d.WG.Done()
		}

		// Send reorg data to blockdata's monitor (so that it stops collecting)
		d.WG.Add(1)
		select {
		case NtfnChans.ReorgChanBlockData <- fullData:
		default:
			d.WG.Done()
		}

		// Send reorg data to stakedb's monitor
		d.WG.Add(1)
		select {
		case NtfnChans.ReorgChanStakeDB <- fullData:
		default:
			d.WG.Done()
		}
		d.WG.Wait()

		// Send reorg data to ChainDB's monitor
		d.WG.Add(1)
		select {
		case NtfnChans.ReorgChanDcrpgDB <- fullData:
		default:
			d.WG.Done()
		}
		d.WG.Wait()
	}
}

// MakeNodeNtfnHandlers defines the dcrd notification handlers.
func MakeNodeNtfnHandlers() (*rpcclient.NotificationHandlers, *collectionQueue) {
	blockQueue := NewCollectionQueue()
	go blockQueue.ProcessBlocks()
	// ReorgSignaler must be started after creating the RPC client.
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
			log.Tracef("OnReorganization: %d / %s --> %d / %s",
				oldHeight, oldHash, newHeight, newHash)

			// Queue sending the details of this reorganization to each
			// package's reorg handler. This is not blocking.
			queueReorg(ReorgData{
				OldChainHead:   *oldHash,
				OldChainHeight: oldHeight,
				NewChainHead:   *newHash,
				NewChainHeight: newHeight,
				WG:             new(sync.WaitGroup),
			})
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
