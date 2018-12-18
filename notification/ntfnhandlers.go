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
	"github.com/decred/dcrdata/v4/api/insight"
	"github.com/decred/dcrdata/v4/explorer"
	"github.com/decred/dcrdata/v4/mempool"
	"github.com/decred/dcrdata/v4/rpcutils"
	"github.com/decred/dcrdata/v4/txhelpers"
	"github.com/decred/dcrwallet/wallet/udb"
)

var nodeClient *rpcclient.Client

// RegisterNodeNtfnHandlers registers with dcrd to receive new block,
// transaction and winning ticket notifications.
func RegisterNodeNtfnHandlers(dcrdClient *rpcclient.Client) *ContextualError {
	// Set the node client for use by signalReorg to determine the common
	// ancestor of two chains.
	nodeClient = dcrdClient

	// Register for block connection and chain reorg notifications.
	var err error
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

// NewCollectionQueue creates a new collectionQueue with an unbuffered channel
// to facilitate synchronous processing. Run (*collectionQueue).ProcessBlocks as
// a goroutine to begin processing blocks sent to the channel, or call
// processBlock for each new block.
func NewCollectionQueue() *collectionQueue {
	return &collectionQueue{
		q: make(chan *blockHashHeight),
	}
}

var blockQueue = NewCollectionQueue()

// SetSynchronousHandlers sets the slice of synchronous new block handler
// functions, which are called in the order they occur in the slice.
func (q *collectionQueue) SetSynchronousHandlers(syncHandlers []func(hash *chainhash.Hash)) {
	q.syncHandlers = syncHandlers
}

// SetPreviousBlock modifies the height and hash of the best block. This data is
// required to avoid connecting new blocks that are not next in the chain. It is
// only necessary to call SetPreviousBlock if blocks are connected or
// disconnected by a mechanism other than (*collectionQueue).processBlock, which
// keeps this data up-to-date. For example, signalReorg will use
// SetPreviousBlock after the reorg is complete.
func (q *collectionQueue) SetPreviousBlock(prevHash chainhash.Hash, prevHeight int64) {
	q.prevHash = prevHash
	q.prevHeight = prevHeight
}

// ProcessBlocks receives *blockHashHeights on the channel collectionQueue.q,
// and calls processBlock synchronously for the received data. This should be
// run as a goroutine, or use processBlock directly.
func (q *collectionQueue) ProcessBlocks() {
	// Process queued blocks one at a time.
	for bh := range q.q {
		q.processBlock(bh)
	}
}

// processBlock calls each synchronous new block handler with the given
// blockHashHeight, and then signals to the monitors that a new block was mined.
func (q *collectionQueue) processBlock(bh *blockHashHeight) {
	hash := bh.hash
	height := bh.height

	// Ensure that the received block (bh.hash, bh.height) connects to the
	// previously connected block (q.prevHash, q.prevHeight).
	if bh.prevHash != q.prevHash {
		log.Infof("Received block at %d (%v) does not connect to %d (%v). "+
			"This is normal before reorganization.",
			height, hash, q.prevHeight, q.prevHash)
		return
	}

	start := time.Now()

	// Run synchronous block connected handlers in order.
	for _, h := range q.syncHandlers {
		h(&hash)
	}

	// Record this block as the best block connected by the collectionQueue.
	q.SetPreviousBlock(hash, height)

	log.Debugf("Synchronous handlers of collectionQueue.processBlock() completed in %v", time.Since(start))

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

// ReorgData describes the old and new chain tips involved in a reorganization.
type ReorgData struct {
	OldChainHead   chainhash.Hash
	OldChainHeight int32
	NewChainHead   chainhash.Hash
	NewChainHeight int32
	WG             *sync.WaitGroup
}

// signalReorg takes the basic reorganization data from dcrd, determines the two
// chains and their common ancestor, and signals the reorg to each packages'
// reorganization handler. Lastly, the collectionQueue's best block data is
// updated so that it will successfully accept new blocks building on the new
// chain tip.
func signalReorg(d ReorgData) {
	if nodeClient == nil {
		log.Errorf("The daemon RPC client for signalReorg is not configured!")
		return
	}

	// Determine the common ancestor of the two chains, and get the full
	// list of blocks in each chain back to but not including the common
	// ancestor.
	ancestor, newChain, oldChain, err := rpcutils.CommonAncestor(nodeClient,
		d.NewChainHead, d.OldChainHead)
	if err != nil {
		log.Errorf("Failed to determine common ancestor. Aborting reorg.")
		return
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

	// Send reorg data to stakedb's monitor.
	d.WG.Add(1)
	select {
	case NtfnChans.ReorgChanStakeDB <- fullData:
	default:
		d.WG.Done()
	}
	d.WG.Wait()

	// Send reorg data to dcrsqlite's monitor.
	d.WG.Add(1)
	select {
	case NtfnChans.ReorgChanWiredDB <- fullData:
	default:
		d.WG.Done()
	}

	// Send reorg data to blockdata's monitor.
	d.WG.Add(1)
	select {
	case NtfnChans.ReorgChanBlockData <- fullData:
	default:
		d.WG.Done()
	}

	// Send reorg data to ChainDB's monitor.
	d.WG.Add(1)
	select {
	case NtfnChans.ReorgChanDcrpgDB <- fullData:
	default:
		d.WG.Done()
	}
	d.WG.Wait()

	// Update prevHash and prevHeight in collectionQueue.
	blockQueue.SetPreviousBlock(d.NewChainHead, int64(d.NewChainHeight))
}

// anyQueue is a buffered channel used queueing the handling of different types
// of event notifications, while keeping the order in which they were received
// from the daemon. See superQueue.
var anyQueue = make(chan interface{}, 1e7)

// superQueue manages the event notification queue, keeping the events in the
// same order they were received by the rpcclient.NotificationHandlers, and
// sending them to the appropriate handlers. This should be run as a goroutine.
func superQueue() {
	for e := range anyQueue {
		// Do not allow new blocks to process while running reorg. Only allow
		// them to be processed after this reorg completes.
		switch et := e.(type) {
		case *blockHashHeight:
			// Process the new block.
			log.Infof("superQueue: Processing new block %v (height %d).", et.hash, et.height)
			blockQueue.processBlock(et)
		case ReorgData:
			// Process the reorg.
			log.Infof("superQueue: Processing reorganization from %s (height %d) to %s (height %d).",
				et.OldChainHead.String(), et.OldChainHeight,
				et.NewChainHead.String(), et.NewChainHeight)
			signalReorg(et)
		default:
			log.Errorf("Unknown event data type: %T", et)
		}
	}
}

// MakeNodeNtfnHandlers defines the dcrd notification handlers.
func MakeNodeNtfnHandlers() (*rpcclient.NotificationHandlers, *collectionQueue) {
	go superQueue()
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
			anyQueue <- &blockHashHeight{
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
			anyQueue <- ReorgData{
				OldChainHead:   *oldHash,
				OldChainHeight: oldHeight,
				NewChainHead:   *newHash,
				NewChainHeight: newHeight,
				WG:             new(sync.WaitGroup),
			}
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
