// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrsqlite

import (
	"database/sql"
	"sync"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/stakedb"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
)

// wiredDB is intended to satisfy APIDataSource interface. The block header is
// not stored in the DB, so the RPC client is used to get it on demand.
type wiredDB struct {
	*DBDataSaver
	MPC    *mempool.MempoolDataCache
	client *dcrrpcclient.Client
	params *chaincfg.Params
	sDB    *stakedb.StakeDatabase
}

func newWiredDB(DB *DB, statusC chan uint32, cl *dcrrpcclient.Client, p *chaincfg.Params) (wiredDB, func() error) {
	wDB := wiredDB{
		DBDataSaver: &DBDataSaver{DB, statusC},
		MPC:         new(mempool.MempoolDataCache),
		client:      cl,
		params:      p,
	}

	//err := wDB.openStakeDB()
	var err error
	wDB.sDB, err = stakedb.NewStakeDatabase(cl, p)
	if err != nil {
		log.Error("Unable to create stake DB: ", err)
		return wDB, func() error { return nil }
	}
	return wDB, wDB.sDB.StakeDB.Close
}

func NewWiredDB(db *sql.DB, statusC chan uint32, cl *dcrrpcclient.Client, p *chaincfg.Params) (wiredDB, func() error) {
	return newWiredDB(NewDB(db), statusC, cl, p)
}

func InitWiredDB(dbInfo *DBInfo, statusC chan uint32, cl *dcrrpcclient.Client, p *chaincfg.Params) (wiredDB, func() error, error) {
	db, err := InitDB(dbInfo)
	if err != nil {
		return wiredDB{}, func() error { return nil }, err
	}

	wDB, cleanup := newWiredDB(db, statusC, cl, p)
	return wDB, cleanup, nil
}

func (db *wiredDB) NewStakeDBChainMonitor(quit chan struct{}, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash, reorgChan chan *stakedb.ReorgData) *stakedb.ChainMonitor {
	return db.sDB.NewChainMonitor(quit, wg, blockChan, reorgChan)
}

func (db *wiredDB) SyncDB(wg *sync.WaitGroup, quit chan struct{}) error {
	defer wg.Done()
	var err error
	if err = db.Ping(); err != nil {
		return err
	}
	if err = db.client.Ping(); err != nil {
		return err
	}
	// Do not allow Store() while doing sync
	db.mtx.Lock()
	defer db.mtx.Unlock()
	return db.resyncDB(quit)
}

func (db *wiredDB) SyncDBWithPoolValue(wg *sync.WaitGroup, quit chan struct{}) error {
	defer wg.Done()
	var err error
	if err = db.Ping(); err != nil {
		return err
	}
	if err = db.client.Ping(); err != nil {
		return err
	}
	// Do not allow Store() while doing sync
	db.mtx.Lock()
	defer db.mtx.Unlock()
	return db.resyncDBWithPoolValue(quit)
}

func (db *wiredDB) GetStakeDB() *stakedb.StakeDatabase {
	return db.sDB
}

// TODO: Update this to use RetrieveBlockHeight
func (db *wiredDB) GetHeight() int {
	return int(db.GetBlockSummaryHeight())
}

func (db *wiredDB) GetHash(idx int64) (string, error) {
	hash, err := db.RetrieveBlockHash(idx)
	if err != nil {
		log.Errorf("Unable to block hash for index %d: %v", idx, err)
		return "", err
	}
	return hash, nil
}

func (db *wiredDB) GetHeader(idx int) *dcrjson.GetBlockHeaderVerboseResult {
	return rpcutils.GetBlockHeaderVerbose(db.client, db.params, int64(idx))
}

func (db *wiredDB) GetBlockVerbose(idx int, verboseTx bool) *dcrjson.GetBlockVerboseResult {
	return rpcutils.GetBlockVerbose(db.client, db.params, int64(idx), verboseTx)
}

func (db *wiredDB) GetStakeDiffEstimates() *apitypes.StakeDiff {
	sd := rpcutils.GetStakeDiffEstimates(db.client)

	height := db.MPC.GetHeight()
	winSize := uint32(db.params.StakeDiffWindowSize)
	sd.IdxBlockInWindow = int(height%winSize) + 1
	sd.PriceWindowNum = int(height / winSize)

	return sd
}

func (db *wiredDB) GetFeeInfo(idx int) *dcrjson.FeeInfoBlock {
	stakeInfo, err := db.RetrieveStakeInfoExtended(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve stake info: %v", err)
		return nil
	}

	return &stakeInfo.Feeinfo
}

func (db *wiredDB) GetStakeInfoExtended(idx int) *apitypes.StakeInfoExtended {
	stakeInfo, err := db.RetrieveStakeInfoExtended(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve stake info: %v", err)
		return nil
	}

	return stakeInfo
}

func (db *wiredDB) GetSummary(idx int) *apitypes.BlockDataBasic {
	blockSummary, err := db.RetrieveBlockSummary(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve block summary: %v", err)
		return nil
	}

	return blockSummary
}

func (db *wiredDB) GetBestBlockSummary() *apitypes.BlockDataBasic {
	dbBlkHeight := db.GetBlockSummaryHeight()
	blockSummary, err := db.RetrieveBlockSummary(dbBlkHeight)
	if err != nil {
		log.Errorf("Unable to retrieve block %d summary: %v", dbBlkHeight, err)
		return nil
	}

	return blockSummary
}

func (db *wiredDB) GetPoolInfo(idx int) *apitypes.TicketPoolInfo {
	ticketPoolInfo, err := db.RetrievePoolInfo(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve ticket pool info: %v", err)
		return nil
	}
	return ticketPoolInfo
}

func (db *wiredDB) GetPoolInfoRange(idx0, idx1 int) []apitypes.TicketPoolInfo {
	ticketPoolInfos, err := db.RetrievePoolInfoRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve ticket pool info range: %v", err)
		return nil
	}
	return ticketPoolInfos
}

func (db *wiredDB) GetPoolValAndSizeRange(idx0, idx1 int) ([]float64, []float64) {
	poolvals, poolsizes, err := db.RetrievePoolValAndSizeRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve ticket value and size range: %v", err)
		return nil, nil
	}
	return poolvals, poolsizes
}

func (db *wiredDB) GetSDiff(idx int) float64 {
	sdiff, err := db.RetrieveSDiff(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve stake difficulty: %v", err)
		return -1
	}
	return sdiff
}

func (db *wiredDB) GetSDiffRange(idx0, idx1 int) []float64 {
	sdiffs, err := db.RetrieveSDiffRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve stake difficulty range: %v", err)
		return nil
	}
	return sdiffs
}

func (db *wiredDB) GetMempoolSSTxSummary() *apitypes.MempoolTicketFeeInfo {
	_, feeInfo := db.MPC.GetFeeInfoExtra()
	return feeInfo
}

func (db *wiredDB) GetMempoolSSTxFeeRates(N int) *apitypes.MempoolTicketFees {
	height, timestamp, totalFees, fees := db.MPC.GetFeeRates(N)
	mpTicketFees := apitypes.MempoolTicketFees{
		Height:   height,
		Time:     timestamp,
		Length:   uint32(len(fees)),
		Total:    uint32(totalFees),
		FeeRates: fees,
	}
	return &mpTicketFees
}

func (db *wiredDB) GetMempoolSSTxDetails(N int) *apitypes.MempoolTicketDetails {
	height, timestamp, totalSSTx, details := db.MPC.GetTicketsDetails(N)
	mpTicketDetails := apitypes.MempoolTicketDetails{
		Height:  height,
		Time:    timestamp,
		Length:  uint32(len(details)),
		Total:   uint32(totalSSTx),
		Tickets: []*apitypes.TicketDetails(details),
	}
	return &mpTicketDetails
}
