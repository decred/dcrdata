// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"strings"
	"time"

	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/stakedb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/wallet/udb"
)

func registerNodeNtfnHandlers(dcrdClient *dcrrpcclient.Client) *ContextualError {
	var err error
	// Register for block connection and chain reorg notifications.
	if err = dcrdClient.NotifyBlocks(); err != nil {
		return newContextualError("block notification "+
			"registration failed", err)
	}

	// Register for stake difficulty change notifications.
	if err = dcrdClient.NotifyStakeDifficulty(); err != nil {
		return newContextualError("stake difficulty change "+
			"notification registration failed", err)
	}

	// Register for tx accepted into mempool ntfns
	if err = dcrdClient.NotifyNewTransactions(false); err != nil {
		return newContextualError("new transaction "+
			"notification registration failed", err)
	}

	// For OnNewTickets
	//  Commented since there is a bug in dcrrpcclient/notify.go
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

// Define notification handlers
func getNodeNtfnHandlers(cfg *config) *dcrrpcclient.NotificationHandlers {
	return &dcrrpcclient.NotificationHandlers{
		OnBlockConnected: func(blockHeaderSerialized []byte, transactions [][]byte) {
			blockHeader := new(wire.BlockHeader)
			err := blockHeader.FromBytes(blockHeaderSerialized)
			if err != nil {
				log.Error("Failed to serialize blockHeader in new block notification.")
			}
			height := int32(blockHeader.Height)
			hash := blockHeader.BlockHash()

			// Prevent handlers other than the stakedb block connected handler
			// from executing certain stake DB functions, namely PoolInfo().
			ntfnChans.stakeDBLock <- struct{}{}

			// stakedb.(p *ChainMonitor).BlockConnectedHandler will unlock
			// stakeDBLock when it finishes handling the new block.
			select {
			case ntfnChans.connectChanStakeDB <- &hash:
			default:
				<-ntfnChans.stakeDBLock
			}

			select {
			case ntfnChans.connectChan <- &hash:
			// send to nil channel blocks
			default:
			}

			select {
			case ntfnChans.connectChanWiredDB <- &hash:
			default:
			}

			// Also send on stake info channel, if enabled.
			select {
			case ntfnChans.connectChanStkInf <- height:
			default:
			}

			// Web UI status update handler
			select {
			case ntfnChans.updateStatusNodeHeight <- blockHeader.Height:
			default:
			}
		},
		OnReorganization: func(oldHash *chainhash.Hash, oldHeight int32,
			newHash *chainhash.Hash, newHeight int32) {
			// Send reorg data to dcrsqlite's monitor
			select {
			case ntfnChans.reorgChanBlockData <- &blockdata.ReorgData{
				OldChainHead:   *oldHash,
				OldChainHeight: oldHeight,
				NewChainHead:   *newHash,
				NewChainHeight: newHeight,
			}:
			default:
			}
			// Send reorg data to stakedb's monitor
			select {
			case ntfnChans.reorgChanStakeDB <- &stakedb.ReorgData{
				OldChainHead:   *oldHash,
				OldChainHeight: oldHeight,
				NewChainHead:   *newHash,
				NewChainHeight: newHeight,
			}:
			default:
			}
		},
		// Not too useful since this notifies on every block
		// OnStakeDifficulty: func(hash *chainhash.Hash, height int64,
		// 	stakeDiff int64) {
		// 	select {
		// 	case ntfnChans.stakeDiffChan <- stakeDiff:
		// 	default:
		// 	}
		// },
		// TODO
		OnWinningTickets: func(blockHash *chainhash.Hash, blockHeight int64,
			tickets []*chainhash.Hash) {
			var txstr []string
			for _, t := range tickets {
				txstr = append(txstr, t.String())
			}
			log.Debugf("Winning tickets: %v", strings.Join(txstr, ", "))
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
			case ntfnChans.relevantTxMempoolChan <- tx:
				log.Debugf("Detected transaction %v in mempool containing registered address.",
					txHash.String())
			default:
			}
		},
		// OnTxAccepted is invoked when a transaction is accepted into the
		// memory pool.  It will only be invoked if a preceding call to
		// NotifyNewTransactions with the verbose flag set to false has been
		// made to register for the notification and the function is non-nil.
		OnTxAccepted: func(hash *chainhash.Hash, amount dcrutil.Amount) {
			// Just send the tx hash and let the goroutine handle everything.
			select {
			case ntfnChans.newTxChan <- hash:
			default:
			}
			//log.Trace("Transaction accepted to mempool: ", hash, amount)
		},
		// Note: dcrjson.TxRawResult is from getrawtransaction
		//OnTxAcceptedVerbose: func(txDetails *dcrjson.TxRawResult) {
		//txDetails.Hex
		//log.Info("Transaction accepted to mempool: ", txDetails.Txid)
		//},
	}
}
