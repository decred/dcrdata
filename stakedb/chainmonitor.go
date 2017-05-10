package stakedb

import (
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

// ChainMonitor connects blocks to the stake DB as they come in.
type ChainMonitor struct {
	db        *StakeDatabase
	quit      chan struct{}
	wg        *sync.WaitGroup
	blockChan chan *chainhash.Hash
}

// NewChainMonitor creates a new ChainMonitor
func (db *StakeDatabase) NewChainMonitor(quit chan struct{}, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash) *ChainMonitor {
	return &ChainMonitor{
		db:        db,
		quit:      quit,
		wg:        wg,
		blockChan: blockChan,
	}
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
			if !ok {
				log.Warnf("Block connected channel closed.")
				break out
			}
			block, err := p.db.ConnectBlockHash(hash)
			if err != nil {
				log.Error(err)
				break keepon
			}

			log.Infof("Connected block %d to stake DB.", block.Height())

		case _, ok := <-p.quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting block connected handler for BLOCK monitor.")
				break out
			}
		}
	}

}
