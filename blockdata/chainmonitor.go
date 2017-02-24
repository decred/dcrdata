package blockdata

import (
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

// for getblock, ticketfeeinfo, estimatestakediff, etc.
type chainMonitor struct {
	collector       *blockDataCollector
	dataSavers      []BlockDataSaver
	quit            chan struct{}
	wg              *sync.WaitGroup
	noTicketPool    bool
	watchaddrs      map[string]txhelpers.TxAction
	blockChan       chan *chainhash.Hash
	recvTxBlockChan chan *txhelpers.BlockWatchedTx
}

// NewChainMonitor creates a new chainMonitor
func NewChainMonitor(collector *blockDataCollector,
	savers []BlockDataSaver,
	quit chan struct{}, wg *sync.WaitGroup, noPoolValue bool,
	addrs map[string]txhelpers.TxAction, blockChan chan *chainhash.Hash,
	recvTxBlockChan chan *txhelpers.BlockWatchedTx) *chainMonitor {
	return &chainMonitor{
		collector:       collector,
		dataSavers:      savers,
		quit:            quit,
		wg:              wg,
		noTicketPool:    noPoolValue,
		watchaddrs:      addrs,
		blockChan:       blockChan,
		recvTxBlockChan: recvTxBlockChan,
	}
}

// blockConnectedHandler handles block connected notifications, which trigger
// data collection and storage.
func (p *chainMonitor) BlockConnectedHandler() {
	defer p.wg.Done()
out:
	for {
	keepon:
		select {
		case hash, ok := <-p.blockChan:
			if !ok {
				log.Warnf("Block connected channel closed.")
				break out
			}
			block, _ := p.collector.dcrdChainSvr.GetBlock(hash)
			height := block.Height()
			log.Infof("Block height %v connected", height)

			if len(p.watchaddrs) > 0 {
				// txsForOutpoints := blockConsumesOutpointWithAddresses(block, p.watchaddrs,
				// 	p.collector.dcrdChainSvr)
				// if len(txsForOutpoints) > 0 {
				// 	p.spendTxBlockChan <- &BlockWatchedTx{height, txsForOutpoints}
				// }

				txsForAddrs := txhelpers.BlockReceivesToAddresses(block,
					p.watchaddrs, p.collector.netParams)
				if len(txsForAddrs) > 0 {
					p.recvTxBlockChan <- &txhelpers.BlockWatchedTx{
						BlockHeight:   height,
						TxsForAddress: txsForAddrs}
				}
			}

			// data collection with timeout
			bdataChan := make(chan *BlockData)
			// fire it off and get the BlockData pointer back through the channel
			go func() {
				BlockData, err := p.collector.Collect(p.noTicketPool)
				if err != nil {
					log.Errorf("Block data collection failed: %v", err.Error())
					// BlockData is nil when err != nil
				}
				bdataChan <- BlockData
			}()

			// Wait for X seconds before giving up on Collect()
			var BlockData *BlockData
			select {
			case BlockData = <-bdataChan:
				if BlockData == nil {
					break keepon
				}
			case <-time.After(time.Second * 20):
				log.Errorf("Block data collection TIMEOUT after 20 seconds.")
				break keepon
			}

			// Store block data with each saver
			for _, s := range p.dataSavers {
				if s != nil {
					// save data to wherever the saver wants to put it
					go s.Store(BlockData)
				}
			}

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting block connected handler for BLOCK monitor.")
				break out
			}
		}
	}

}
