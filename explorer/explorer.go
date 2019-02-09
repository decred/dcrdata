// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

// package explorer handles the block explorer subsystem for generating the
// explorer pages.
package explorer

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v4/blockdata"
	"github.com/decred/dcrdata/v4/db/dbtypes"
	"github.com/decred/dcrdata/v4/exchanges"
	"github.com/decred/dcrdata/v4/explorer/types"
	"github.com/decred/dcrdata/v4/mempool"
	pstypes "github.com/decred/dcrdata/v4/pubsub/types"
	"github.com/decred/dcrdata/v4/txhelpers"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

const (
	// maxExplorerRows and minExplorerRows are the limits on the number of
	// blocks/time-window rows that may be shown on the explorer pages.
	maxExplorerRows = 400
	minExplorerRows = 20

	// syncStatusInterval is the frequency with startup synchronization progress
	// signals are sent to websocket clients.
	syncStatusInterval = 2 * time.Second

	// defaultAddressRows is the default number of rows to be shown on the
	// address page table.
	defaultAddressRows int64 = 20

	// MaxAddressRows is an upper limit on the number of rows that may be shown
	// on the address page table.
	MaxAddressRows int64 = 1000
)

// explorerDataSourceLite implements an interface for collecting data for the
// explorer pages
type explorerDataSourceLite interface {
	GetExplorerBlock(hash string) *types.BlockInfo
	GetExplorerBlocks(start int, end int) []*types.BlockBasic
	GetBlockHeight(hash string) (int64, error)
	GetBlockHash(idx int64) (string, error)
	GetExplorerTx(txid string) *types.TxInfo
	GetExplorerAddress(address string, count, offset int64) (*dbtypes.AddressInfo, txhelpers.AddressType, txhelpers.AddressError)
	GetTip() (*types.WebBasicBlock, error)
	DecodeRawTransaction(txhex string) (*dcrjson.TxRawResult, error)
	SendRawTransaction(txhex string) (string, error)
	GetHeight() (int64, error)
	GetChainParams() *chaincfg.Params
	UnconfirmedTxnsForAddress(address string) (*txhelpers.AddressOutpoints, int64, error)
	GetMempool() []types.MempoolTx
	TxHeight(txid string) (height int64)
	BlockSubsidy(height int64, voters uint16) *dcrjson.GetBlockSubsidyResult
	GetSqliteChartsData() (map[string]*dbtypes.ChartsData, error)
	GetExplorerFullBlocks(start int, end int) []*types.BlockInfo
	Difficulty() (float64, error)
	RetreiveDifficulty(timestamp int64) float64
}

// explorerDataSource implements extra data retrieval functions that require a
// faster solution than RPC, or additional functionality.
type explorerDataSource interface {
	BlockHeight(hash string) (int64, error)
	HeightDB() (int64, error)
	BlockHash(height int64) (string, error)
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	PoolStatusForTicket(txid string) (dbtypes.TicketSpendType, dbtypes.TicketPoolStatus, error)
	AddressHistory(address string, N, offset int64, txnType dbtypes.AddrTxnType) ([]*dbtypes.AddressRow, *dbtypes.AddressBalance, error)
	AddressData(address string, N, offset int64, txnType dbtypes.AddrTxnType) (*dbtypes.AddressInfo, error)
	DevBalance() (*dbtypes.AddressBalance, error)
	FillAddressTransactions(addrInfo *dbtypes.AddressInfo) error
	BlockMissedVotes(blockHash string) ([]string, error)
	TicketMiss(ticketHash string) (string, int64, error)
	GetPgChartsData() (map[string]*dbtypes.ChartsData, error)
	TicketsPriceByHeight() (*dbtypes.ChartsData, error)
	SideChainBlocks() ([]*dbtypes.BlockStatus, error)
	DisapprovedBlocks() ([]*dbtypes.BlockStatus, error)
	BlockStatus(hash string) (dbtypes.BlockStatus, error)
	BlockFlags(hash string) (bool, bool, error)
	TicketPoolVisualization(interval dbtypes.TimeBasedGrouping) (*dbtypes.PoolTicketsData, *dbtypes.PoolTicketsData, *dbtypes.PoolTicketsData, int64, error)
	TransactionBlocks(hash string) ([]*dbtypes.BlockStatus, []uint32, error)
	Transaction(txHash string) ([]*dbtypes.Tx, error)
	VinsForTx(*dbtypes.Tx) (vins []dbtypes.VinTxProperty, prevPkScripts []string, scriptVersions []uint16, err error)
	VoutsForTx(*dbtypes.Tx) ([]dbtypes.Vout, error)
	PosIntervals(limit, offset uint64) ([]*dbtypes.BlocksGroupedInfo, error)
	TimeBasedIntervals(timeGrouping dbtypes.TimeBasedGrouping, limit, offset uint64) ([]*dbtypes.BlocksGroupedInfo, error)
	AgendaCumulativeVoteChoices(agendaID string) (yes, abstain, no uint32, err error)
}

// chartDataCounter is a data cache for the historical charts.
type chartDataCounter struct {
	sync.RWMutex
	updateHeight int64
	Data         map[string]*dbtypes.ChartsData
}

// cacheChartsData holds the prepopulated data that is used to draw the charts.
var cacheChartsData chartDataCounter

// Height returns the last update height of the charts data cache.
func (c *chartDataCounter) Height() int64 {
	c.RLock()
	defer c.RUnlock()
	return c.height()
}

// Update sets new data for the given height in the the charts data cache.
func (c *chartDataCounter) Update(height int64, newData map[string]*dbtypes.ChartsData) {
	c.Lock()
	defer c.Unlock()
	c.update(height, newData)
}

// height returns the last update height of the charts data cache. Use Height
// instead for thread-safe access.
func (c *chartDataCounter) height() int64 {
	if c.Data == nil {
		return -1
	}
	return c.updateHeight
}

// update sets new data for the given height in the the charts data cache. Use
// Update instead for thread-safe access.
func (c *chartDataCounter) update(height int64, newData map[string]*dbtypes.ChartsData) {
	c.updateHeight = height
	c.Data = newData
}

// ChartTypeData is a thread-safe way to access chart data of the given type.
func ChartTypeData(chartType string) (data *dbtypes.ChartsData, ok bool) {
	cacheChartsData.RLock()
	defer cacheChartsData.RUnlock()

	// Data updates replace the entire map rather than modifying the data to
	// which the pointers refer, so the pointer can safely be returned here.
	data, ok = cacheChartsData.Data[chartType]
	return
}

// TicketStatusText generates the text to display on the explorer's transaction
// page for the "POOL STATUS" field.
func TicketStatusText(s dbtypes.TicketSpendType, p dbtypes.TicketPoolStatus) string {
	switch p {
	case dbtypes.PoolStatusLive:
		return "In Live Ticket Pool"
	case dbtypes.PoolStatusVoted:
		return "Voted"
	case dbtypes.PoolStatusExpired:
		switch s {
		case dbtypes.TicketUnspent:
			return "Expired, Unrevoked"
		case dbtypes.TicketRevoked:
			return "Expired, Revoked"
		default:
			return "invalid ticket state"
		}
	case dbtypes.PoolStatusMissed:
		switch s {
		case dbtypes.TicketUnspent:
			return "Missed, Unrevoked"
		case dbtypes.TicketRevoked:
			return "Missed, Revoked"
		default:
			return "invalid ticket state"
		}
	default:
		return "Immature"
	}
}

type pageData struct {
	sync.RWMutex
	BlockInfo      *types.BlockInfo
	BlockchainInfo *dcrjson.GetBlockChainInfoResult
	HomeInfo       *types.HomeInfo
}

// Mempool represents a snapshot of the mempool.
type Mempool struct {
	sync.RWMutex

	// Inv contains a MempoolShort, which contains a detailed summary of the
	// mempool state, and a []MempoolTx for each of tickets, votes, revocations,
	// and regular transactions. This is essentially a mempool inventory. The
	// struct pointed to may be shared, so it should not be modified.
	Inv *types.MempoolInfo

	// StakeData and Txns are set by StoreMPData, but this data is presently not
	// used by explorerUI. Consider removing these in the future and editing
	// StoreMPData accordingly
	StakeData *mempool.StakeData
	Txns      []types.MempoolTx
}

type explorerUI struct {
	Mux              *chi.Mux
	blockData        explorerDataSourceLite
	explorerSource   explorerDataSource
	dbsSyncing       atomic.Value
	liteMode         bool
	devPrefetch      bool
	templates        templates
	wsHub            *WebsocketHub
	pageData         *pageData
	mempool          Mempool
	ChainParams      *chaincfg.Params
	Version          string
	NetName          string
	MeanVotingBlocks int64
	ChartUpdate      sync.Mutex
	xcBot            *exchanges.ExchangeBot
	// displaySyncStatusPage indicates if the sync status page is the only web
	// page that should be accessible during DB synchronization.
	displaySyncStatusPage atomic.Value
}

// AreDBsSyncing is a thread-safe way to fetch the boolean in dbsSyncing.
func (exp *explorerUI) AreDBsSyncing() bool {
	syncing, ok := exp.dbsSyncing.Load().(bool)
	return ok && syncing
}

// SetDBsSyncing is a thread-safe way to update dbsSyncing.
func (exp *explorerUI) SetDBsSyncing(syncing bool) {
	exp.dbsSyncing.Store(syncing)
	exp.wsHub.SetDBsSyncing(syncing)
}

func (exp *explorerUI) reloadTemplates() error {
	return exp.templates.reloadTemplates()
}

// See reloadsig*.go for an exported method
func (exp *explorerUI) reloadTemplatesSig(sig os.Signal) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, sig)

	go func() {
		for {
			sigr := <-sigChan
			log.Infof("Received %s", sig)
			if sigr == sig {
				if err := exp.reloadTemplates(); err != nil {
					log.Error(err)
					continue
				}
				log.Infof("Explorer UI html templates reparsed.")
			}
		}
	}()
}

// StopWebsocketHub stops the websocket hub
func (exp *explorerUI) StopWebsocketHub() {
	if exp == nil {
		return
	}
	log.Info("Stopping websocket hub.")
	exp.wsHub.Stop()
}

// New returns an initialized instance of explorerUI
func New(dataSource explorerDataSourceLite, primaryDataSource explorerDataSource,
	useRealIP bool, appVersion string, devPrefetch bool, viewsfolder string,
	xcBot *exchanges.ExchangeBot) *explorerUI {
	exp := new(explorerUI)
	exp.Mux = chi.NewRouter()
	exp.blockData = dataSource
	exp.explorerSource = primaryDataSource
	// Allocate Mempool fields.
	exp.mempool.Inv = new(types.MempoolInfo)
	exp.mempool.StakeData = new(mempool.StakeData)
	exp.Version = appVersion
	exp.devPrefetch = devPrefetch
	exp.xcBot = xcBot
	// explorerDataSource is an interface that could have a value of pointer
	// type, and if either is nil this means lite mode.
	if exp.explorerSource == nil || reflect.ValueOf(exp.explorerSource).IsNil() {
		log.Debugf("Primary data source not available. Operating explorer in lite mode.")
		exp.liteMode = true
	}

	if useRealIP {
		exp.Mux.Use(middleware.RealIP)
	}

	params := exp.blockData.GetChainParams()
	exp.ChainParams = params
	exp.NetName = netName(exp.ChainParams)
	exp.MeanVotingBlocks = txhelpers.CalcMeanVotingBlocks(params)

	// Development subsidy address of the current network
	devSubsidyAddress, err := dbtypes.DevSubsidyAddress(params)
	if err != nil {
		log.Warnf("explorer.New: %v", err)
	}
	log.Debugf("Organization address: %s", devSubsidyAddress)

	exp.pageData = &pageData{
		BlockInfo: new(types.BlockInfo),
		HomeInfo: &types.HomeInfo{
			DevAddress: devSubsidyAddress,
			Params: types.ChainParams{
				WindowSize:       exp.ChainParams.StakeDiffWindowSize,
				RewardWindowSize: exp.ChainParams.SubsidyReductionInterval,
				BlockTime:        exp.ChainParams.TargetTimePerBlock.Nanoseconds(),
				MeanVotingBlocks: exp.MeanVotingBlocks,
			},
			PoolInfo: types.TicketPoolInfo{
				Target: exp.ChainParams.TicketPoolSize * exp.ChainParams.TicketsPerBlock,
			},
		},
	}

	log.Infof("Mean Voting Blocks calculated: %d", exp.pageData.HomeInfo.Params.MeanVotingBlocks)

	commonTemplates := []string{"extras"}
	exp.templates = newTemplates(viewsfolder, commonTemplates, makeTemplateFuncMap(exp.ChainParams))

	tmpls := []string{"home", "explorer", "mempool", "block", "tx", "address",
		"rawtx", "status", "parameters", "agenda", "agendas", "charts",
		"sidechains", "disapproved", "ticketpool", "nexthome", "statistics",
		"windows", "timelisting", "addresstable"}

	for _, name := range tmpls {
		if err := exp.templates.addTemplate(name); err != nil {
			log.Errorf("Unable to create new html template: %v", err)
			return nil
		}
	}

	exp.addRoutes()

	exp.wsHub = NewWebsocketHub()

	go exp.wsHub.run()

	return exp
}

// PrepareCharts pre-populates charts data when in full mode.
func (exp *explorerUI) PrepareCharts() {
	if !exp.liteMode {
		exp.prePopulateChartsData()
	}
}

// Height returns the height of the current block data.
func (exp *explorerUI) Height() int64 {
	exp.pageData.RLock()
	defer exp.pageData.RUnlock()
	return exp.pageData.BlockInfo.Height
}

// LastBlock returns the last block hash, height and time.
func (exp *explorerUI) LastBlock() (lastBlockHash string, lastBlock int64, lastBlockTime int64) {
	exp.pageData.RLock()
	defer exp.pageData.RUnlock()
	lastBlock = exp.pageData.BlockInfo.Height
	lastBlockTime = exp.pageData.BlockInfo.BlockTime.UNIX()
	lastBlockHash = exp.pageData.BlockInfo.Hash
	return
}

// MempoolInventory safely retrieves the current mempool inventory.
func (exp *explorerUI) MempoolInventory() *types.MempoolInfo {
	exp.mempool.RLock()
	defer exp.mempool.RUnlock()
	return exp.mempool.Inv
}

// MempoolSignals returns the mempool signal and data channels, which are to be
// used by the mempool package's MempoolMonitor as send only channels.
func (exp *explorerUI) MempoolSignals() (chan<- pstypes.HubSignal, chan<- *types.MempoolTx) {
	return exp.wsHub.HubRelay, exp.wsHub.NewTxChan
}

// prePopulateChartsData should run in the background the first time the system
// is initialized and when new blocks are added.
func (exp *explorerUI) prePopulateChartsData() {
	if exp.liteMode {
		log.Warnf("Charts are not supported in lite mode!")
		return
	}

	// Prevent multiple concurrent updates, but do not lock the cacheChartsData
	// to avoid blocking Store.
	exp.ChartUpdate.Lock()
	defer exp.ChartUpdate.Unlock()

	// Avoid needlessly updating charts data.
	expHeight := exp.Height()
	if expHeight == cacheChartsData.Height() {
		log.Debugf("Not updating charts data again for height %d.", expHeight)
		return
	}

	log.Info("Pre-populating the charts data. This may take a minute...")
	log.Debugf("Retrieving charts data from aux DB.")
	var err error
	pgData, err := exp.explorerSource.GetPgChartsData()
	if dbtypes.IsTimeoutErr(err) {
		log.Warnf("GetPgChartsData DB timeout: %v", err)
		return
	}
	if err != nil {
		log.Errorf("Invalid PG data found: %v", err)
		return
	}

	log.Debugf("Retrieving charts data from base DB.")
	sqliteData, err := exp.blockData.GetSqliteChartsData()
	if err != nil {
		log.Errorf("Invalid SQLite data found: %v", err)
		return
	}

	for k, v := range sqliteData {
		pgData[k] = v
	}

	cacheChartsData.Update(expHeight, pgData)

	log.Info("Done pre-populating the charts data.")
}

// StoreMPData stores mempool data. It is advisable to pass a copy of the
// []types.MempoolTx so that it may be modified (e.g. sorted) without affecting
// other MempoolDataSavers.
func (exp *explorerUI) StoreMPData(stakeData *mempool.StakeData, txs []types.MempoolTx, inv *types.MempoolInfo) {
	// Get exclusive access to the Mempool field.
	exp.mempool.Lock()
	exp.mempool.Inv = inv
	exp.mempool.StakeData = stakeData
	exp.mempool.Txns = txs
	exp.mempool.Unlock()

	// Signal to the websocket hub that a new tx was received, but do not block
	// StoreMPData(), and do not hang forever in a goroutine waiting to send.
	go func() {
		select {
		case exp.wsHub.HubRelay <- sigMempoolUpdate:
		case <-time.After(time.Second * 10):
			log.Errorf("sigMempoolUpdate send failed: Timeout waiting for WebsocketHub.")
		}
	}()

	log.Debugf("Updated mempool details for the explorerUI.")
}

func (exp *explorerUI) Store(blockData *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	// Retrieve block data for the passed block hash.
	newBlockData := exp.blockData.GetExplorerBlock(msgBlock.BlockHash().String())

	// Use the latest block's blocktime to get the last 24hr timestamp.
	timestamp := newBlockData.BlockTime.UNIX() - 86400
	targetTimePerBlock := float64(exp.ChainParams.TargetTimePerBlock)
	// RetreiveDifficulty fetches the difficulty using the last 24hr timestamp,
	// whereby the difficulty can have a timestamp equal to the last 24hrs
	// timestamp or that is immediately greater than the 24hr timestamp.
	last24hrDifficulty := exp.blockData.RetreiveDifficulty(timestamp)
	last24HrHashRate := dbtypes.CalculateHashRate(last24hrDifficulty, targetTimePerBlock)

	difficulty := blockData.Header.Difficulty
	hashrate := dbtypes.CalculateHashRate(difficulty, targetTimePerBlock)

	// If BlockData contains non-nil PoolInfo, compute actual percentage of DCR
	// supply staked.
	stakePerc := 45.0
	if blockData.PoolInfo != nil {
		stakePerc = blockData.PoolInfo.Value / dcrutil.Amount(blockData.ExtraInfo.CoinSupply).ToCoin()
	}
	// Simulate the annual staking rate
	ASR, _ := exp.simulateASR(1000, false, stakePerc,
		dcrutil.Amount(blockData.ExtraInfo.CoinSupply).ToCoin(),
		float64(newBlockData.Height),
		blockData.CurrentStakeDiff.CurrentStakeDifficulty)

	// Update pageData with block data and chain (home) info.
	p := exp.pageData
	p.Lock()

	// Store current block and blockchain data.
	p.BlockInfo = newBlockData
	p.BlockchainInfo = blockData.BlockchainInfo

	// Update HomeInfo.
	p.HomeInfo.HashRate = hashrate
	p.HomeInfo.HashRateChange = 100 * (hashrate - last24HrHashRate) / last24HrHashRate
	p.HomeInfo.CoinSupply = blockData.ExtraInfo.CoinSupply
	p.HomeInfo.StakeDiff = blockData.CurrentStakeDiff.CurrentStakeDifficulty
	p.HomeInfo.NextExpectedStakeDiff = blockData.EstStakeDiff.Expected
	p.HomeInfo.NextExpectedBoundsMin = blockData.EstStakeDiff.Min
	p.HomeInfo.NextExpectedBoundsMax = blockData.EstStakeDiff.Max
	p.HomeInfo.IdxBlockInWindow = blockData.IdxBlockInWindow
	p.HomeInfo.IdxInRewardWindow = int(newBlockData.Height % exp.ChainParams.SubsidyReductionInterval)
	p.HomeInfo.Difficulty = difficulty
	p.HomeInfo.NBlockSubsidy.Dev = blockData.ExtraInfo.NextBlockSubsidy.Developer
	p.HomeInfo.NBlockSubsidy.PoS = blockData.ExtraInfo.NextBlockSubsidy.PoS
	p.HomeInfo.NBlockSubsidy.PoW = blockData.ExtraInfo.NextBlockSubsidy.PoW
	p.HomeInfo.NBlockSubsidy.Total = blockData.ExtraInfo.NextBlockSubsidy.Total

	// If BlockData contains non-nil PoolInfo, copy values.
	p.HomeInfo.PoolInfo = types.TicketPoolInfo{}
	if blockData.PoolInfo != nil {
		tpTarget := exp.ChainParams.TicketPoolSize * exp.ChainParams.TicketsPerBlock
		p.HomeInfo.PoolInfo = types.TicketPoolInfo{
			Size:          blockData.PoolInfo.Size,
			Value:         blockData.PoolInfo.Value,
			ValAvg:        blockData.PoolInfo.ValAvg,
			Percentage:    stakePerc * 100,
			PercentTarget: 100 * float64(blockData.PoolInfo.Size) / float64(tpTarget),
			Target:        tpTarget,
		}
	}

	posSubsPerVote := dcrutil.Amount(blockData.ExtraInfo.NextBlockSubsidy.PoS).ToCoin() /
		float64(exp.ChainParams.TicketsPerBlock)
	p.HomeInfo.TicketReward = 100 * posSubsPerVote /
		blockData.CurrentStakeDiff.CurrentStakeDifficulty

	// The actual reward of a ticket needs to also take into consideration the
	// ticket maturity (time from ticket purchase until its eligible to vote)
	// and coinbase maturity (time after vote until funds distributed to ticket
	// holder are available to use).
	avgSSTxToSSGenMaturity := exp.MeanVotingBlocks +
		int64(exp.ChainParams.TicketMaturity) +
		int64(exp.ChainParams.CoinbaseMaturity)
	p.HomeInfo.RewardPeriod = fmt.Sprintf("%.2f days", float64(avgSSTxToSSGenMaturity)*
		exp.ChainParams.TargetTimePerBlock.Hours()/24)
	p.HomeInfo.ASR = ASR

	p.Unlock()

	if !exp.liteMode && exp.devPrefetch {
		go exp.updateDevFundBalance()
	}

	// Update the charts data after every five blocks or if no charts data
	// exists yet. Do not update the charts data if blockchain sync is running.
	if !exp.AreDBsSyncing() && (newBlockData.Height%5 == 0 || cacheChartsData.Height() == -1) {
		// This must be done after storing BlockInfo since that provides the
		// explorer's best block height, which is used by prePopulateChartsData
		// to decide if an update is needed.
		go exp.prePopulateChartsData()
	}

	// Signal to the websocket hub that a new block was received, but do not
	// block Store(), and do not hang forever in a goroutine waiting to send.
	go func() {
		select {
		case exp.wsHub.HubRelay <- sigNewBlock:
		case <-time.After(time.Second * 10):
			log.Errorf("sigNewBlock send failed: Timeout waiting for WebsocketHub.")
		}
	}()

	log.Debugf("Got new block %d for the explorer.", newBlockData.Height)

	return nil
}

func (exp *explorerUI) updateDevFundBalance() {
	if exp.liteMode {
		log.Warnf("Full balances not supported in lite mode.")
		return
	}

	// yield processor to other goroutines
	runtime.Gosched()

	devBalance, err := exp.explorerSource.DevBalance()
	if err == nil && devBalance != nil {
		exp.pageData.Lock()
		exp.pageData.HomeInfo.DevFund = devBalance.TotalUnspent
		exp.pageData.Unlock()
	} else {
		log.Errorf("explorerUI.updateDevFundBalance failed: %v", err)
	}
}

func (exp *explorerUI) addRoutes() {
	exp.Mux.Use(middleware.Logger)
	exp.Mux.Use(middleware.Recoverer)
	corsMW := cors.Default()
	exp.Mux.Use(corsMW.Handler)

	redirect := func(url string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			x := chi.URLParam(r, "x")
			if x != "" {
				x = "/" + x
			}
			http.Redirect(w, r, "/"+url+x, http.StatusPermanentRedirect)
		}
	}
	exp.Mux.Get("/", redirect("blocks"))

	exp.Mux.Get("/block/{x}", redirect("block"))

	exp.Mux.Get("/tx/{x}", redirect("tx"))

	exp.Mux.Get("/address/{x}", redirect("address"))

	exp.Mux.Get("/decodetx", redirect("decodetx"))

	exp.Mux.Get("/stats", redirect("statistics"))
}

// Simulate ticket purchase and re-investment over a full year for a given
// starting amount of DCR and calculation parameters.  Generate a TEXT table of
// the simulation results that can optionally be used for future expansion of
// dcrdata functionality.
func (exp *explorerUI) simulateASR(StartingDCRBalance float64, IntegerTicketQty bool,
	CurrentStakePercent float64, ActualCoinbase float64, CurrentBlockNum float64,
	ActualTicketPrice float64) (ASR float64, ReturnTable string) {

	// Calculations are only useful on mainnet.  Short circuit calculations if
	// on any other version of chain params.
	if exp.ChainParams.Name != "mainnet" {
		return 0, ""
	}

	BlocksPerDay := 86400 / exp.ChainParams.TargetTimePerBlock.Seconds()
	BlocksPerYear := 365 * BlocksPerDay
	TicketsPurchased := float64(0)

	StakeRewardAtBlock := func(blocknum float64) float64 {
		// Option 1:  RPC Call
		Subsidy := exp.blockData.BlockSubsidy(int64(blocknum), 1)
		return dcrutil.Amount(Subsidy.PoS).ToCoin()

		// Option 2:  Calculation
		// epoch := math.Floor(blocknum / float64(exp.ChainParams.SubsidyReductionInterval))
		// RewardProportionPerVote := float64(exp.ChainParams.StakeRewardProportion) / (10 * float64(exp.ChainParams.TicketsPerBlock))
		// return float64(RewardProportionPerVote) * dcrutil.Amount(exp.ChainParams.BaseSubsidy).ToCoin() *
		// 	math.Pow(float64(exp.ChainParams.MulSubsidy)/float64(exp.ChainParams.DivSubsidy), epoch)
	}

	MaxCoinSupplyAtBlock := func(blocknum float64) float64 {
		// 4th order poly best fit curve to Decred mainnet emissions plot.
		// Curve fit was done with 0 Y intercept and Pre-Mine added after.

		return (-9E-19*math.Pow(blocknum, 4) +
			7E-12*math.Pow(blocknum, 3) -
			2E-05*math.Pow(blocknum, 2) +
			29.757*blocknum + 76963 +
			1680000) // Premine 1.68M

	}

	CoinAdjustmentFactor := ActualCoinbase / MaxCoinSupplyAtBlock(CurrentBlockNum)

	TheoreticalTicketPrice := func(blocknum float64) float64 {
		ProjectedCoinsCirculating := MaxCoinSupplyAtBlock(blocknum) * CoinAdjustmentFactor * CurrentStakePercent
		TicketPoolSize := (float64(exp.MeanVotingBlocks) + float64(exp.ChainParams.TicketMaturity) +
			float64(exp.ChainParams.CoinbaseMaturity)) * float64(exp.ChainParams.TicketsPerBlock)
		return ProjectedCoinsCirculating / TicketPoolSize

	}
	TicketAdjustmentFactor := ActualTicketPrice / TheoreticalTicketPrice(CurrentBlockNum)

	// Prepare for simulation
	simblock := CurrentBlockNum
	TicketPrice := ActualTicketPrice
	DCRBalance := StartingDCRBalance

	ReturnTable += fmt.Sprintf("\n\nBLOCKNUM        DCR  TICKETS TKT_PRICE TKT_REWRD  ACTION\n")
	ReturnTable += fmt.Sprintf("%8d  %9.2f %8.1f %9.2f %9.2f    INIT\n",
		int64(simblock), DCRBalance, TicketsPurchased,
		TicketPrice, StakeRewardAtBlock(simblock))

	for simblock < (BlocksPerYear + CurrentBlockNum) {

		// Simulate a Purchase on simblock
		TicketPrice = TheoreticalTicketPrice(simblock) * TicketAdjustmentFactor

		if IntegerTicketQty {
			// Use this to simulate integer qtys of tickets up to max funds
			TicketsPurchased = math.Floor(DCRBalance / TicketPrice)
		} else {
			// Use this to simulate ALL funds used to buy tickets - even fractional tickets
			// which is actually not possible
			TicketsPurchased = (DCRBalance / TicketPrice)
		}

		DCRBalance -= (TicketPrice * TicketsPurchased)
		ReturnTable += fmt.Sprintf("%8d  %9.2f %8.1f %9.2f %9.2f     BUY\n",
			int64(simblock), DCRBalance, TicketsPurchased,
			TicketPrice, StakeRewardAtBlock(simblock))

		// Move forward to average vote
		simblock += (float64(exp.ChainParams.TicketMaturity) + float64(exp.MeanVotingBlocks))
		ReturnTable += fmt.Sprintf("%8d  %9.2f %8.1f %9.2f %9.2f    VOTE\n",
			int64(simblock), DCRBalance, TicketsPurchased,
			(TheoreticalTicketPrice(simblock) * TicketAdjustmentFactor), StakeRewardAtBlock(simblock))

		// Simulate return of funds
		DCRBalance += (TicketPrice * TicketsPurchased)

		// Simulate reward
		DCRBalance += (StakeRewardAtBlock(simblock) * TicketsPurchased)
		TicketsPurchased = 0

		// Move forward to coinbase maturity
		simblock += float64(exp.ChainParams.CoinbaseMaturity)

		ReturnTable += fmt.Sprintf("%8d  %9.2f %8.1f %9.2f %9.2f  REWARD\n",
			int64(simblock), DCRBalance, TicketsPurchased,
			(TheoreticalTicketPrice(simblock) * TicketAdjustmentFactor), StakeRewardAtBlock(simblock))

		// Need to receive funds before we can use them again so add 1 block
		simblock++
	}

	// Scale down to exactly 365 days
	SimulationReward := ((DCRBalance - StartingDCRBalance) / StartingDCRBalance) * 100
	ASR = (BlocksPerYear / (simblock - CurrentBlockNum)) * SimulationReward
	ReturnTable += fmt.Sprintf("ASR over 365 Days is %.2f.\n", ASR)
	return
}

func (exp *explorerUI) getExchangeState() *exchanges.ExchangeBotState {
	if exp.xcBot == nil || exp.xcBot.IsFailed() {
		return nil
	}
	return exp.xcBot.State()
}
