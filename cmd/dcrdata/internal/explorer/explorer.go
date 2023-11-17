// Copyright (c) 2018-2022, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

// Package explorer handles the block explorer subsystem for generating the
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

	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/wire"

	"github.com/decred/dcrdata/exchanges/v3"
	"github.com/decred/dcrdata/gov/v6/agendas"
	pitypes "github.com/decred/dcrdata/gov/v6/politeia/types"
	"github.com/decred/dcrdata/v8/blockdata"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/explorer/types"
	"github.com/decred/dcrdata/v8/mempool"
	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
	"github.com/decred/dcrdata/v8/txhelpers"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"
)

const (
	// maxExplorerRows and minExplorerRows are the limits on the number of
	// blocks/time-window rows that may be shown on the explorer pages.
	maxExplorerRows = 400
	minExplorerRows = 10

	// syncStatusInterval is the frequency with startup synchronization progress
	// signals are sent to websocket clients.
	syncStatusInterval = 2 * time.Second

	// defaultAddressRows is the default number of rows to be shown on the
	// address page table.
	defaultAddressRows int64 = 20

	// MaxAddressRows is an upper limit on the number of rows that may be shown
	// on the address page table.
	MaxAddressRows int64 = 160

	MaxTreasuryRows int64 = 200

	testnetNetName = "Testnet"
)

// explorerDataSource implements extra data retrieval functions that require a
// faster solution than RPC, or additional functionality.
type explorerDataSource interface {
	BlockHeight(hash string) (int64, error)
	Height() int64
	HeightDB() (int64, error)
	BlockHash(height int64) (string, error)
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	PoolStatusForTicket(txid string) (dbtypes.TicketSpendType, dbtypes.TicketPoolStatus, error)
	TreasuryBalance() (*dbtypes.TreasuryBalance, error)
	TreasuryTxns(n, offset int64, txType stake.TxType) ([]*dbtypes.TreasuryTx, error)
	AddressHistory(address string, N, offset int64, txnType dbtypes.AddrTxnViewType) ([]*dbtypes.AddressRow, *dbtypes.AddressBalance, error)
	AddressData(address string, N, offset int64, txnType dbtypes.AddrTxnViewType) (*dbtypes.AddressInfo, error)
	DevBalance() (*dbtypes.AddressBalance, error)
	FillAddressTransactions(addrInfo *dbtypes.AddressInfo) error
	BlockMissedVotes(blockHash string) ([]string, error)
	TicketMiss(ticketHash string) (string, int64, error)
	SideChainBlocks() ([]*dbtypes.BlockStatus, error)
	DisapprovedBlocks() ([]*dbtypes.BlockStatus, error)
	BlockStatus(hash string) (dbtypes.BlockStatus, error)
	BlockStatuses(height int64) ([]*dbtypes.BlockStatus, error)
	BlockFlags(hash string) (bool, bool, error)
	TicketPoolVisualization(interval dbtypes.TimeBasedGrouping) (*dbtypes.PoolTicketsData, *dbtypes.PoolTicketsData, *dbtypes.PoolTicketsData, int64, error)
	TransactionBlocks(hash string) ([]*dbtypes.BlockStatus, []uint32, error)
	Transaction(txHash string) ([]*dbtypes.Tx, error)
	VinsForTx(*dbtypes.Tx) (vins []dbtypes.VinTxProperty, prevPkScripts []string, scriptVersions []uint16, err error)
	VoutsForTx(*dbtypes.Tx) ([]dbtypes.Vout, error)
	PosIntervals(limit, offset uint64) ([]*dbtypes.BlocksGroupedInfo, error)
	TimeBasedIntervals(timeGrouping dbtypes.TimeBasedGrouping, limit, offset uint64) ([]*dbtypes.BlocksGroupedInfo, error)
	AgendasVotesSummary(agendaID string) (summary *dbtypes.AgendaSummary, err error)
	BlockTimeByHeight(height int64) (int64, error)
	GetChainParams() *chaincfg.Params
	GetExplorerBlock(hash string) *types.BlockInfo
	GetExplorerBlocks(start int, end int) []*types.BlockBasic
	GetBlockHeight(hash string) (int64, error)
	GetBlockHash(idx int64) (string, error)
	GetExplorerTx(txid string) *types.TxInfo
	GetTip() (*types.WebBasicBlock, error)
	DecodeRawTransaction(txhex string) (*chainjson.TxRawResult, error)
	SendRawTransaction(txhex string) (string, error)
	GetTransactionByHash(txid string) (*wire.MsgTx, error)
	GetHeight() (int64, error)
	TxHeight(txid *chainhash.Hash) (height int64)
	DCP0010ActivationHeight() int64
	DCP0011ActivationHeight() int64
	DCP0012ActivationHeight() int64
	BlockSubsidy(height int64, voters uint16) *chainjson.GetBlockSubsidyResult
	GetExplorerFullBlocks(start int, end int) []*types.BlockInfo
	CurrentDifficulty() (float64, error)
	Difficulty(timestamp int64) float64
}

type PoliteiaBackend interface {
	ProposalsLastSync() int64
	ProposalsSync() error
	ProposalsAll(offset, rowsCount int, filterByVoteStatus ...int) ([]*pitypes.ProposalRecord, int, error)
	ProposalByToken(token string) (*pitypes.ProposalRecord, error)
}

// agendaBackend implements methods that manage agendas db data.
type agendaBackend interface {
	AgendaInfo(agendaID string) (*agendas.AgendaTagged, error)
	AllAgendas() (agendas []*agendas.AgendaTagged, err error)
	UpdateAgendas() error
}

// ChartDataSource provides data from the charts cache.
type ChartDataSource interface {
	AnonymitySet() uint64
}

// links to be passed with common page data.
type links struct {
	CoinbaseComment string
	POSExplanation  string
	APIDocs         string
	InsightAPIDocs  string
	Github          string
	License         string
	NetParams       string
	DownloadLink    string
	// Testnet and below are set via dcrdata config.
	Testnet       string
	Mainnet       string
	TestnetSearch string
	MainnetSearch string
	OnionURL      string
}

var explorerLinks = &links{
	CoinbaseComment: "https://github.com/decred/dcrd/blob/2a18beb4d56fe59d614a7309308d84891a0cba96/chaincfg/genesis.go#L17-L53",
	POSExplanation:  "https://docs.decred.org/proof-of-stake/overview/",
	APIDocs:         "https://github.com/decred/dcrdata#apis",
	InsightAPIDocs:  "https://github.com/decred/dcrdata/blob/master/docs/Insight_API_documentation.md",
	Github:          "https://github.com/decred/dcrdata",
	License:         "https://github.com/decred/dcrdata/blob/master/LICENSE",
	NetParams:       "https://github.com/decred/dcrd/blob/master/chaincfg/params.go",
	DownloadLink:    "https://decred.org/wallets/",
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
	BlockchainInfo *chainjson.GetBlockChainInfoResult
	HomeInfo       *types.HomeInfo
}

type explorerUI struct {
	Mux              *chi.Mux
	dataSource       explorerDataSource
	chartSource      ChartDataSource
	agendasSource    agendaBackend
	voteTracker      *agendas.VoteTracker
	proposals        PoliteiaBackend
	dbsSyncing       atomic.Value
	devPrefetch      bool
	templates        templates
	wsHub            *WebsocketHub
	pageData         *pageData
	ChainParams      *chaincfg.Params
	Version          string
	NetName          string
	MeanVotingBlocks int64
	xcBot            *exchanges.ExchangeBot
	xcDone           chan struct{}
	// displaySyncStatusPage indicates if the sync status page is the only web
	// page that should be accessible during DB synchronization.
	displaySyncStatusPage atomic.Value
	politeiaURL           string

	invsMtx sync.RWMutex
	invs    *types.MempoolInfo
	premine int64
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
	close(exp.xcDone)
}

// ExplorerConfig is the configuration settings for explorerUI.
type ExplorerConfig struct {
	DataSource    explorerDataSource
	ChartSource   ChartDataSource
	UseRealIP     bool
	AppVersion    string
	DevPrefetch   bool
	Viewsfolder   string
	XcBot         *exchanges.ExchangeBot
	AgendasSource agendaBackend
	Tracker       *agendas.VoteTracker
	Proposals     PoliteiaBackend
	PoliteiaURL   string
	MainnetLink   string
	TestnetLink   string
	OnionAddress  string
	ReloadHTML    bool
}

// New returns an initialized instance of explorerUI
func New(cfg *ExplorerConfig) *explorerUI {
	exp := new(explorerUI)
	exp.Mux = chi.NewRouter()
	exp.dataSource = cfg.DataSource
	exp.chartSource = cfg.ChartSource
	// Allocate Mempool fields.
	exp.invs = new(types.MempoolInfo)
	exp.Version = cfg.AppVersion
	exp.devPrefetch = cfg.DevPrefetch
	exp.xcBot = cfg.XcBot
	exp.xcDone = make(chan struct{})
	exp.agendasSource = cfg.AgendasSource
	exp.voteTracker = cfg.Tracker
	exp.proposals = cfg.Proposals
	exp.politeiaURL = cfg.PoliteiaURL
	explorerLinks.Mainnet = cfg.MainnetLink
	explorerLinks.Testnet = cfg.TestnetLink
	explorerLinks.MainnetSearch = cfg.MainnetLink + "search?search="
	explorerLinks.TestnetSearch = cfg.TestnetLink + "search?search="

	if cfg.OnionAddress != "" {
		explorerLinks.OnionURL = fmt.Sprintf("http://%s/", cfg.OnionAddress)
	}

	// explorerDataSource is an interface that could have a value of pointer type.
	if exp.dataSource == nil || reflect.ValueOf(exp.dataSource).IsNil() {
		log.Errorf("An explorerDataSource (PostgreSQL backend) is required.")
		return nil
	}

	if cfg.UseRealIP {
		exp.Mux.Use(middleware.RealIP)
	}

	params := exp.dataSource.GetChainParams()
	exp.ChainParams = params
	exp.NetName = netName(exp.ChainParams)
	exp.MeanVotingBlocks = txhelpers.CalcMeanVotingBlocks(params)
	exp.premine = params.BlockOneSubsidy()

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
				Target: uint32(exp.ChainParams.TicketPoolSize) * uint32(exp.ChainParams.TicketsPerBlock),
			},
		},
	}

	log.Infof("Mean Voting Blocks calculated: %d", exp.pageData.HomeInfo.Params.MeanVotingBlocks)

	commonTemplates := []string{"extras"}
	exp.templates = newTemplates(cfg.Viewsfolder, cfg.ReloadHTML, commonTemplates, makeTemplateFuncMap(exp.ChainParams))

	tmpls := []string{"home", "blocks", "mempool", "block", "tx", "address",
		"rawtx", "status", "parameters", "agenda", "agendas", "charts",
		"sidechains", "disapproved", "ticketpool", "visualblocks", "statistics",
		"windows", "timelisting", "addresstable", "proposals", "proposal",
		"market", "insight_root", "attackcost", "treasury", "treasurytable", "verify_message", "stakingreward"}

	for _, name := range tmpls {
		if err := exp.templates.addTemplate(name); err != nil {
			log.Errorf("Unable to create new html template: %v", err)
			return nil
		}
	}

	exp.addRoutes()

	exp.wsHub = NewWebsocketHub()

	go exp.wsHub.run()

	go exp.watchExchanges()

	return exp
}

// Height returns the height of the current block data.
func (exp *explorerUI) Height() int64 {
	exp.pageData.RLock()
	defer exp.pageData.RUnlock()

	if exp.pageData.BlockInfo.BlockBasic == nil {
		// If exp.pageData.BlockInfo.BlockBasic has not yet been set return:
		return -1
	}

	return exp.pageData.BlockInfo.Height
}

// LastBlock returns the last block hash, height and time.
func (exp *explorerUI) LastBlock() (lastBlockHash string, lastBlock int64, lastBlockTime int64) {
	exp.pageData.RLock()
	defer exp.pageData.RUnlock()

	if exp.pageData.BlockInfo.BlockBasic == nil {
		// If exp.pageData.BlockInfo.BlockBasic has not yet been set return:
		lastBlock, lastBlockTime = -1, -1
		return
	}

	lastBlock = exp.pageData.BlockInfo.Height
	lastBlockTime = exp.pageData.BlockInfo.BlockTime.UNIX()
	lastBlockHash = exp.pageData.BlockInfo.Hash
	return
}

// MempoolInventory safely retrieves the current mempool inventory.
func (exp *explorerUI) MempoolInventory() *types.MempoolInfo {
	exp.invsMtx.RLock()
	defer exp.invsMtx.RUnlock()
	return exp.invs
}

// MempoolID safely fetches the current mempool inventory ID.
func (exp *explorerUI) MempoolID() uint64 {
	exp.invsMtx.RLock()
	defer exp.invsMtx.RUnlock()
	return exp.invs.ID()
}

// MempoolSignal returns the mempool signal channel, which is to be used by the
// mempool package's MempoolMonitor as a send-only channel.
func (exp *explorerUI) MempoolSignal() chan<- pstypes.HubMessage {
	return exp.wsHub.HubRelay
}

// StoreMPData stores mempool data. It is advisable to pass a copy of the
// []types.MempoolTx so that it may be modified (e.g. sorted) without affecting
// other MempoolDataSavers.
func (exp *explorerUI) StoreMPData(_ *mempool.StakeData, _ []types.MempoolTx, inv *types.MempoolInfo) {
	// Get exclusive access to the Mempool field.
	exp.invsMtx.Lock()
	exp.invs = inv
	exp.invsMtx.Unlock()
	log.Debugf("Updated mempool details for the explorerUI.")
}

// Store implements BlockDataSaver.
func (exp *explorerUI) Store(blockData *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	// Retrieve block data for the passed block hash.
	newBlockData := exp.dataSource.GetExplorerBlock(msgBlock.BlockHash().String())

	// Use the latest block's blocktime to get the last 24hr timestamp.
	day := 24 * time.Hour
	targetTimePerBlock := float64(exp.ChainParams.TargetTimePerBlock)

	// Hashrate change over last day
	timestamp := newBlockData.BlockTime.T.Add(-day).Unix()
	last24hrDifficulty := exp.dataSource.Difficulty(timestamp)
	last24HrHashRate := dbtypes.CalculateHashRate(last24hrDifficulty, targetTimePerBlock)

	// Hashrate change over last month
	timestamp = newBlockData.BlockTime.T.Add(-30 * day).Unix()
	lastMonthDifficulty := exp.dataSource.Difficulty(timestamp)
	lastMonthHashRate := dbtypes.CalculateHashRate(lastMonthDifficulty, targetTimePerBlock)

	difficulty := blockData.Header.Difficulty
	hashrate := dbtypes.CalculateHashRate(difficulty, targetTimePerBlock)

	// If BlockData contains non-nil PoolInfo, compute actual percentage of DCR
	// supply staked.
	stakePerc := 45.0
	if blockData.PoolInfo != nil {
		stakePerc = blockData.PoolInfo.Value / dcrutil.Amount(blockData.ExtraInfo.CoinSupply).ToCoin()
	}

	treasuryBalance, err := exp.dataSource.TreasuryBalance()
	if err != nil {
		log.Errorf("Store: TreasuryBalance failed: %v", err)
		treasuryBalance = &dbtypes.TreasuryBalance{}
	}

	// Update pageData with block data and chain (home) info.
	p := exp.pageData
	p.Lock()

	// Store current block and blockchain data.
	p.BlockInfo = newBlockData
	p.BlockchainInfo = blockData.BlockchainInfo

	// Update HomeInfo.
	p.HomeInfo.HashRate = hashrate
	p.HomeInfo.HashRateChangeDay = 100 * (hashrate - last24HrHashRate) / last24HrHashRate
	p.HomeInfo.HashRateChangeMonth = 100 * (hashrate - lastMonthHashRate) / lastMonthHashRate
	p.HomeInfo.CoinSupply = blockData.ExtraInfo.CoinSupply
	p.HomeInfo.StakeDiff = blockData.CurrentStakeDiff.CurrentStakeDifficulty
	p.HomeInfo.NextExpectedStakeDiff = blockData.EstStakeDiff.Expected
	p.HomeInfo.NextExpectedBoundsMin = blockData.EstStakeDiff.Min
	p.HomeInfo.NextExpectedBoundsMax = blockData.EstStakeDiff.Max
	p.HomeInfo.IdxBlockInWindow = blockData.IdxBlockInWindow
	p.HomeInfo.IdxInRewardWindow = int(newBlockData.Height%exp.ChainParams.SubsidyReductionInterval) + 1
	p.HomeInfo.Difficulty = difficulty
	p.HomeInfo.TreasuryBalance = treasuryBalance
	p.HomeInfo.NBlockSubsidy.Dev = blockData.ExtraInfo.NextBlockSubsidy.Developer
	p.HomeInfo.NBlockSubsidy.PoS = blockData.ExtraInfo.NextBlockSubsidy.PoS
	p.HomeInfo.NBlockSubsidy.PoW = blockData.ExtraInfo.NextBlockSubsidy.PoW
	p.HomeInfo.NBlockSubsidy.Total = blockData.ExtraInfo.NextBlockSubsidy.Total

	// If BlockData contains non-nil PoolInfo, copy values.
	p.HomeInfo.PoolInfo = types.TicketPoolInfo{}
	if blockData.PoolInfo != nil {
		tpTarget := uint32(exp.ChainParams.TicketPoolSize) * uint32(exp.ChainParams.TicketsPerBlock)
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

	// If exchange monitoring is enabled, set the exchange rate.
	if exp.xcBot != nil {
		exchangeConversion := exp.xcBot.Conversion(1.0)
		if exchangeConversion != nil {
			conversion := types.Conversion(*exchangeConversion)
			p.HomeInfo.ExchangeRate = &conversion
		} else {
			log.Errorf("No rate conversion available yet.")
		}
	}

	p.Unlock()

	// Signal to the websocket hub that a new block was received, but do not
	// block Store(), and do not hang forever in a goroutine waiting to send.
	go func() {
		select {
		case exp.wsHub.HubRelay <- pstypes.HubMessage{Signal: sigNewBlock}:
		case <-time.After(time.Second * 10):
			log.Errorf("sigNewBlock send failed: Timeout waiting for WebsocketHub.")
		}
	}()

	log.Debugf("Got new block %d for the explorer.", newBlockData.Height)

	// Do not run remaining updates when blockchain sync is running.
	if exp.AreDBsSyncing() {
		return nil
	}

	// Simulate the annual staking rate.
	go func(height int64, sdiff float64, supply int64) {
		ASR, _ := exp.simulateASR(1000, false, stakePerc,
			dcrutil.Amount(supply).ToCoin(),
			float64(height), sdiff)
		p.Lock()
		p.HomeInfo.ASR = ASR
		p.Unlock()
	}(newBlockData.Height, blockData.CurrentStakeDiff.CurrentStakeDifficulty,
		blockData.ExtraInfo.CoinSupply) // eval args now instead of in closure

	// Project fund balance, not useful while syncing.
	if exp.devPrefetch {
		go exp.updateDevFundBalance()
	}

	// Trigger a vote info refresh.
	if exp.voteTracker != nil {
		go exp.voteTracker.Refresh()
	}

	// Update proposals data every 5 blocks
	if (newBlockData.Height%5 == 0) && exp.proposals != nil {
		// Update the proposal DB. This is run asynchronously since it involves
		// a query to Politeia (a remote system) and we do not want to block
		// execution.
		go func() {
			err := exp.proposals.ProposalsSync()
			if err != nil {
				log.Errorf("(PoliteiaBackend).ProposalsSync: %v", err)
			}
		}()
	}

	// Update consensus agendas data every 5 blocks.
	if newBlockData.Height%5 == 0 {
		go func() {
			err := exp.agendasSource.UpdateAgendas()
			if err != nil {
				log.Errorf("(agendaBackend).UpdateAgendas: %v", err)
			}
		}()
	}

	return nil
}

// ChartsUpdated should be called when a chart update completes.
func (exp *explorerUI) ChartsUpdated() {
	anonSet := exp.chartSource.AnonymitySet()
	exp.pageData.Lock()
	if exp.pageData.HomeInfo.CoinSupply > 0 {
		exp.pageData.HomeInfo.MixedPercent = float64(anonSet) / float64(exp.pageData.HomeInfo.CoinSupply) * 100
	}
	exp.pageData.Unlock()
}

func (exp *explorerUI) updateDevFundBalance() {
	// yield processor to other goroutines
	runtime.Gosched()

	devBalance, err := exp.dataSource.DevBalance()
	if err == nil && devBalance != nil {
		exp.pageData.Lock()
		exp.pageData.HomeInfo.DevFund = devBalance.TotalUnspent
		exp.pageData.Unlock()
	} else {
		log.Errorf("explorerUI.updateDevFundBalance failed: %v", err)
	}
}

type loggerFunc func(string, ...interface{})

func (lw loggerFunc) Printf(str string, args ...interface{}) {
	lw(str, args...)
}

func (exp *explorerUI) addRoutes() {
	exp.Mux.Use(middleware.Logger)
	exp.Mux.Use(middleware.Recoverer)
	corsMW := cors.Default()
	corsMW.Log = loggerFunc(log.Tracef)
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

	votesPerBlock := exp.ChainParams.VotesPerBlock()

	StakeRewardAtBlock := func(blocknum float64) float64 {
		Subsidy := exp.dataSource.BlockSubsidy(int64(blocknum), votesPerBlock)
		return dcrutil.Amount(Subsidy.PoS / int64(votesPerBlock)).ToCoin()
	}

	MaxCoinSupplyAtBlock := func(blocknum float64) float64 {
		// 4th order poly best fit curve to Decred mainnet emissions plot.
		// Curve fit was done with 0 Y intercept and Pre-Mine added after.

		return (-9e-19*math.Pow(blocknum, 4) +
			7e-12*math.Pow(blocknum, 3) -
			2e-05*math.Pow(blocknum, 2) +
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

	ReturnTable += "\n\nBLOCKNUM        DCR  TICKETS TKT_PRICE TKT_REWRD  ACTION\n"
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

func (exp *explorerUI) watchExchanges() {
	if exp.xcBot == nil {
		return
	}
	xcChans := exp.xcBot.UpdateChannels()

	sendXcUpdate := func(isFiat bool, token string, updater *exchanges.ExchangeState) {
		xcState := exp.xcBot.State()
		update := &WebsocketExchangeUpdate{
			Updater: WebsocketMiniExchange{
				Token:  token,
				Price:  updater.Price,
				Volume: updater.Volume,
				Change: updater.Change,
			},
			IsFiatIndex: isFiat,
			BtcIndex:    exp.xcBot.BtcIndex,
			Price:       xcState.Price,
			BtcPrice:    xcState.BtcPrice,
			Volume:      xcState.Volume,
		}
		select {
		case exp.wsHub.xcChan <- update:
		default:
			log.Warnf("Failed to send WebsocketExchangeUpdate on WebsocketHub channel")
		}
	}

	for {
		select {
		case update := <-xcChans.Exchange:
			sendXcUpdate(false, update.Token, update.State)
		case update := <-xcChans.Index:
			indexState, found := exp.xcBot.State().FiatIndices[update.Token]
			if !found {
				log.Errorf("Index state not found when preparing websocket update")
				continue
			}
			sendXcUpdate(true, update.Token, indexState)
		case <-xcChans.Quit:
			log.Warnf("ExchangeBot has quit.")
			return
		case <-exp.xcDone:
			return
		}
	}
}

func (exp *explorerUI) getExchangeState() *exchanges.ExchangeBotState {
	if exp.xcBot == nil || exp.xcBot.IsFailed() {
		return nil
	}
	return exp.xcBot.State()
}

// mempoolTime is the TimeDef that the transaction was received in DCRData, or
// else a zero-valued TimeDef if no transaction is found.
func (exp *explorerUI) mempoolTime(txid string) types.TimeDef {
	exp.invsMtx.RLock()
	defer exp.invsMtx.RUnlock()
	tx, found := exp.invs.Tx(txid)
	if !found {
		return types.NewTimeDefFromUNIX(0)
	}
	return types.NewTimeDefFromUNIX(tx.Time)
}
