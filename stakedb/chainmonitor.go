// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package stakedb

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrdata/v4/txhelpers"
)

// ChainMonitor connects blocks to the stake DB as they come in.
type ChainMonitor struct {
	mtx            sync.Mutex // coordinate reorg handling
	ctx            context.Context
	db             *StakeDatabase
	wg             *sync.WaitGroup
	blockChan      chan *chainhash.Hash
	reorgChan      chan *txhelpers.ReorgData
	syncConnect    sync.Mutex
	ConnectingLock chan struct{}
	DoneConnecting chan struct{}
}

// NewChainMonitor creates a new ChainMonitor
func (db *StakeDatabase) NewChainMonitor(ctx context.Context, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash, reorgChan chan *txhelpers.ReorgData) *ChainMonitor {
	return &ChainMonitor{
		ctx:            ctx,
		db:             db,
		wg:             wg,
		blockChan:      blockChan,
		reorgChan:      reorgChan,
		ConnectingLock: make(chan struct{}, 1),
		DoneConnecting: make(chan struct{}),
	}
}

// BlockConnectedSync is the synchronous (blocking call) handler for the newly
// connected block given by the hash.
func (p *ChainMonitor) BlockConnectedSync(hash *chainhash.Hash) (err error) {
	// Connections go one at a time so signals cannot be mixed
	p.syncConnect.Lock()
	defer p.syncConnect.Unlock()
	// lock with buffered channel, accepting handoff in BlockConnectedHandler
	p.ConnectingLock <- struct{}{}
	t := time.NewTimer(10 * time.Second)
	select {
	case <-t.C:
		err = fmt.Errorf("block send timeout")
	case p.blockChan <- hash:
		// wait
		<-p.DoneConnecting
	}

	return
}

// BlockConnectedHandler handles block connected notifications, which trigger
// data collection and storage.
func (p *ChainMonitor) BlockConnectedHandler() {
	defer p.wg.Done()
out:
	for {
	keepon:
		select {
		case hash, ok := <-p.blockChan:
			p.mtx.Lock()
			release := func() { p.mtx.Unlock() }
			select {
			case <-p.ConnectingLock:
				// send on unbuffered channel
				release = func() { p.mtx.Unlock(); p.DoneConnecting <- struct{}{} }
			default:
			}

			if !ok {
				log.Warnf("Block connected channel closed.")
				release()
				break out
			}

			// Extend main chain
			block, err := p.db.ConnectBlockHash(hash)
			if err != nil {
				release()
				log.Error(err)
				break keepon
			}

			log.Infof("Connected block %d to stake DB.", block.Height())

			release()

		case <-p.ctx.Done():
			log.Debugf("Got quit signal. Exiting block connected handler.")
			break out
		}
	}

	// Drain the block connected channel.
	// for range <-p.blockChan {
	// }

}

// switchToSideChain attempts to switch to a new side chain by: determining a
// common ancestor block, disconnecting blocks from the main chain back to this
// block, and connecting the side chain blocks onto the mainchain.
func (p *ChainMonitor) switchToSideChain(reorgData *txhelpers.ReorgData) (int32, *chainhash.Hash, error) {
	if reorgData == nil || len(reorgData.NewChain) == 0 {
		return 0, nil, fmt.Errorf("no side chain")
	}

	newChain := reorgData.NewChain
	// newChain does not include the common ancestor.
	commonAncestorHeight := int64(reorgData.NewChainHeight) - int64(len(newChain))

	mainTip := int64(p.db.Height())
	if mainTip != int64(reorgData.OldChainHeight) {
		log.Warnf("StakeDatabase height is %d, expected %d. Rewinding as "+
			"needed to complete reorg from ancestor at %d", mainTip,
			reorgData.OldChainHeight, commonAncestorHeight)
	}

	// Disconnect blocks back to common ancestor.
	log.Debugf("Disconnecting %d blocks", mainTip-commonAncestorHeight)
	err := p.db.DisconnectBlocks(mainTip - commonAncestorHeight)
	if err != nil {
		return 0, nil, err
	}

	mainTip = int64(p.db.Height())
	if mainTip != commonAncestorHeight {
		panic(fmt.Sprintf("disconnect blocks failed: tip height %d, expected %d",
			mainTip, commonAncestorHeight))
	}

	// Connect blocks in side/new chain onto main chain.
	log.Debugf("Connecting %d blocks", len(newChain))
	var tipHeight int64
	var tipHash *chainhash.Hash
	for i := range newChain {
		hash := &newChain[i]
		var block *dcrutil.Block
		if block, err = p.db.ConnectBlockHash(hash); err != nil {
			mainTip = int64(p.db.Height())
			currentBlockHdr, _ := p.db.DBTipBlockHeader()
			currentBlockHash := currentBlockHdr.BlockHash()
			return int32(mainTip), &currentBlockHash,
				fmt.Errorf("error connecting block %v", hash)
		}
		tipHeight = block.Height()
		tipHash = block.Hash()
		log.Infof("Connected block %v (height %d) from side chain.",
			tipHash, tipHeight)
	}

	mainTip = int64(p.db.Height())
	if mainTip != tipHeight {
		panic("connected block height not db tip height")
	}

	return int32(mainTip), tipHash, nil
}

// ReorgHandler receives notification of a chain reorganization and initiates a
// corresponding reorganization of the stakedb.StakeDatabase.
func (p *ChainMonitor) ReorgHandler() {
	defer p.wg.Done()
out:
	for {
		//keepon:
		select {
		case reorgData, ok := <-p.reorgChan:
			p.mtx.Lock()
			if !ok {
				p.mtx.Unlock()
				log.Warnf("Reorg channel closed.")
				break out
			}

			newHeight, oldHeight := reorgData.NewChainHeight, reorgData.OldChainHeight
			newHash, oldHash := reorgData.NewChainHead, reorgData.OldChainHead

			log.Infof("Reorganize started. NEW head block %v at height %d.",
				newHash, newHeight)
			log.Infof("Reorganize started. OLD head block %v at height %d.",
				oldHash, oldHeight)

			// Switch to the side chain.
			stakeDBTipHeight, stakeDBTipHash, err := p.switchToSideChain(reorgData)
			if err != nil {
				log.Errorf("switchToSideChain failed: %v", err)
			}
			if stakeDBTipHeight != newHeight {
				log.Errorf("stakeDBTipHeight is %d, expected %d",
					stakeDBTipHeight, newHeight)
			}
			if *stakeDBTipHash != newHash {
				log.Errorf("stakeDBTipHash is %d, expected %d",
					stakeDBTipHash, newHash)
			}

			p.mtx.Unlock()

			reorgData.WG.Done()

		case <-p.ctx.Done():
			log.Debugf("Got quit signal. Exiting stakedb reorg notification handler.")
			break out
		}
	}
}
