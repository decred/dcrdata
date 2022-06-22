// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package blockdata

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"

	"github.com/decred/dcrdata/v8/txhelpers"
)

// for getblock, ticketfeeinfo, estimatestakediff, etc.
type chainMonitor struct {
	ctx             context.Context
	collector       *Collector
	dataSavers      []BlockDataSaver
	reorgDataSavers []BlockDataSaver
	reorgLock       sync.Mutex
}

// NewChainMonitor creates a new chainMonitor.
func NewChainMonitor(ctx context.Context, collector *Collector, savers []BlockDataSaver,
	reorgSavers []BlockDataSaver) *chainMonitor {

	return &chainMonitor{
		ctx:             ctx,
		collector:       collector,
		dataSavers:      savers,
		reorgDataSavers: reorgSavers,
	}
}

func (p *chainMonitor) collect(hash *chainhash.Hash) (*wire.MsgBlock, *BlockData, error) {
	// getblock RPC
	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	defer cancel()
	msgBlock, err := p.collector.dcrdChainSvr.GetBlock(ctx, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get block %v: %v", hash, err)
	}
	height := int64(msgBlock.Header.Height)
	log.Infof("Block height %v connected. Collecting data...", height)

	// Get node's best block height to see if the block for which we are
	// collecting data is the best block.
	chainHeight, err := p.collector.dcrdChainSvr.GetBlockCount(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get chain height: %v", err)
	}

	// If new block height not equal to chain height, then we are behind
	// on data collection, so specify the hash of the notified, skipping
	// stake diff estimates and other stuff for web ui that is only
	// relevant for the best block.
	var blockData *BlockData
	if chainHeight != height {
		log.Debugf("Collecting data for block %v (%d), behind tip %d.",
			hash, height, chainHeight)
		blockData, _, err = p.collector.CollectHash(hash)
		if err != nil {
			return nil, nil, fmt.Errorf("blockdata.CollectHash(hash) failed: %v", err.Error())
		}
	} else {
		blockData, _, err = p.collector.Collect()
		if err != nil {
			return nil, nil, fmt.Errorf("blockdata.Collect() failed: %v", err.Error())
		}
	}

	return msgBlock, blockData, nil
}

// ConnectBlock is a synchronous version of BlockConnectedHandler that collects
// and stores data for a block. ConnectBlock satisfies
// notification.BlockHandler, and is registered as a handler in main.go.
func (p *chainMonitor) ConnectBlock(header *wire.BlockHeader) error {
	// Do not handle reorg and block connects simultaneously.
	hash := header.BlockHash()
	p.reorgLock.Lock()
	defer p.reorgLock.Unlock()

	// Collect block data.
	msgBlock, blockData, err := p.collect(&hash)
	if err != nil {
		return err
	}

	// Store block data with each saver.
	for _, s := range p.dataSavers {
		if s != nil {
			tStart := time.Now()
			// Save data to wherever the saver wants to put it.
			if err0 := s.Store(blockData, msgBlock); err0 != nil {
				log.Errorf("(%v).Store failed: %v", reflect.TypeOf(s), err0)
				err = err0
			}
			log.Tracef("(*chainMonitor).ConnectBlock: Completed %s.Store in %v.",
				reflect.TypeOf(s), time.Since(tStart))
		}
	}
	return err
}

// ReorgHandler processes a chain reorg. A reorg is handled in blockdata by
// simply collecting data for the new best block, and storing it in the
// *reorgDataSavers*.
func (p *chainMonitor) ReorgHandler(reorg *txhelpers.ReorgData) error {
	if reorg == nil {
		return fmt.Errorf("nil reorg data received")
	}

	newHeight := reorg.NewChainHeight
	newHash := reorg.NewChainHead

	// Do not handle reorg and block connects simultaneously.
	p.reorgLock.Lock()
	defer p.reorgLock.Unlock()
	log.Infof("Reorganize signaled to blockdata. "+
		"Collecting data for NEW head block %v at height %d.",
		newHash, newHeight)

	// Collect data for the new best block.
	msgBlock, blockData, err := p.collect(&newHash)
	if err != nil {
		reorg.WG.Done()
		return fmt.Errorf("ReorgHandler: Failed to collect data for block %v: %v", newHash, err)
	}

	// Store block data with each REORG saver.
	for _, s := range p.reorgDataSavers {
		if s != nil {
			// Save data to wherever the saver wants to put it.
			if err := s.Store(blockData, msgBlock); err != nil {
				return fmt.Errorf("(%v).Store failed: %v", reflect.TypeOf(s), err)
			}
		}
	}
	return nil
}
