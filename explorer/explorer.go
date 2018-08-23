// Copyright (c) 2018, The Decred developers
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
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/blockdata"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/txhelpers"
	humanize "github.com/dustin/go-humanize"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

const (
	maxExplorerRows              = 2000
	minExplorerRows              = 20
	defaultAddressRows     int64 = 20
	MaxAddressRows         int64 = 1000
	MaxUnconfirmedPossible int64 = 1000
)

// explorerDataSourceLite implements an interface for collecting data for the
// explorer pages
type explorerDataSourceLite interface {
	GetExplorerBlock(hash string) *BlockInfo
	GetExplorerBlocks(start int, end int) []*BlockBasic
	GetBlockHeight(hash string) (int64, error)
	GetBlockHash(idx int64) (string, error)
	GetExplorerTx(txid string) *TxInfo
	GetExplorerAddress(address string, count, offset int64) *AddressInfo
	DecodeRawTransaction(txhex string) (*dcrjson.TxRawResult, error)
	SendRawTransaction(txhex string) (string, error)
	GetHeight() int
	GetChainParams() *chaincfg.Params
	UnconfirmedTxnsForAddress(address string) (*txhelpers.AddressOutpoints, int64, error)
	GetMempool() []MempoolTx
	TxHeight(txid string) (height int64)
	BlockSubsidy(height int64, voters uint16) *dcrjson.GetBlockSubsidyResult
	GetSqliteChartsData() (map[string]*dbtypes.ChartsData, error)
}

// explorerDataSource implements extra data retrieval functions that require a
// faster solution than RPC, or additional functionality.
type explorerDataSource interface {
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	PoolStatusForTicket(txid string) (dbtypes.TicketSpendType, dbtypes.TicketPoolStatus, error)
	AddressHistory(address string, N, offset int64, txnType dbtypes.AddrTxnType) ([]*dbtypes.AddressRow, *AddressBalance, error)
	DevBalance() (*AddressBalance, error)
	FillAddressTransactions(addrInfo *AddressInfo) error
	BlockMissedVotes(blockHash string) ([]string, error)
	AgendaVotes(agendaID string, chartType int) (*dbtypes.AgendaVoteChoices, error)
	GetPgChartsData() (map[string]*dbtypes.ChartsData, error)
	GetTicketsPriceByHeight() (*dbtypes.ChartsData, error)
}

// cacheChartsData holds the prepopulated data that is used to draw the charts
var cacheChartsData = new(ChartDataCounter)

// GetChartTypeData is a thread-safe way to access the chart type data.
func GetChartTypeData(chartType string) (data *dbtypes.ChartsData, ok bool) {
	cacheChartsData.RLock()
	defer cacheChartsData.RUnlock()

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
			return "Missed, Reevoked"
		default:
			return "invalid ticket state"
		}
	default:
		return "Immature"
	}
}

type explorerUI struct {
	Mux             *chi.Mux
	blockData       explorerDataSourceLite
	explorerSource  explorerDataSource
	liteMode        bool
	devPrefetch     bool
	templates       templates
	wsHub           *WebsocketHub
	NewBlockDataMtx sync.RWMutex
	NewBlockData    *BlockBasic
	ExtraInfo       *HomeInfo
	MempoolData     *MempoolInfo
	ChainParams     *chaincfg.Params
	Version         string
	NetName         string
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
	useRealIP bool, appVersion string, devPrefetch bool) *explorerUI {
	exp := new(explorerUI)
	exp.Mux = chi.NewRouter()
	exp.blockData = dataSource
	exp.explorerSource = primaryDataSource
	exp.MempoolData = new(MempoolInfo)
	exp.Version = appVersion
	exp.devPrefetch = devPrefetch
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

	// Development subsidy address of the current network
	devSubsidyAddress, err := dbtypes.DevSubsidyAddress(params)
	if err != nil {
		log.Warnf("explorer.New: %v", err)
	}
	log.Debugf("Organization address: %s", devSubsidyAddress)

	// Set default static values for ExtraInfo
	exp.ExtraInfo = &HomeInfo{
		DevAddress: devSubsidyAddress,
		Params: ChainParams{
			WindowSize:       exp.ChainParams.StakeDiffWindowSize,
			RewardWindowSize: exp.ChainParams.SubsidyReductionInterval,
			BlockTime:        exp.ChainParams.TargetTimePerBlock.Nanoseconds(),
			MeanVotingBlocks: txhelpers.CalcMeanVotingBlocks(params),
		},
		PoolInfo: TicketPoolInfo{
			Target: exp.ChainParams.TicketPoolSize * exp.ChainParams.TicketsPerBlock,
		},
	}

	log.Infof("Mean Voting Blocks calculated: %d", exp.ExtraInfo.Params.MeanVotingBlocks)

	noTemplateError := func(err error) *explorerUI {
		log.Errorf("Unable to create new html template: %v", err)
		return nil
	}
	tmpls := []string{"home", "explorer", "mempool", "block", "tx", "address",
		"rawtx", "status", "parameters", "agenda", "agendas", "charts"}

	tempDefaults := []string{"extras"}

	exp.templates = newTemplates("views", tempDefaults, makeTemplateFuncMap(exp.ChainParams))

	for _, name := range tmpls {
		if err := exp.templates.addTemplate(name); err != nil {
			return noTemplateError(err)
		}
	}

	if !exp.liteMode {
		exp.prePopulateChartsData()
	}

	exp.addRoutes()

	exp.wsHub = NewWebsocketHub()

	go exp.wsHub.run()

	return exp
}

// prePopulateChartsData should run in the background the first time the system
// is initialized and when new blocks are added.
func (exp *explorerUI) prePopulateChartsData() {
	if exp.liteMode {
		log.Warnf("Charts are not supported in lite mode!")
		return
	}
	log.Info("Pre-populating the charts data. This may take a minute...")
	var err error

	pgData, err := exp.explorerSource.GetPgChartsData()
	if err != nil {
		log.Errorf("Invalid PG data found: %v", err)
		return
	}

	sqliteData, err := exp.blockData.GetSqliteChartsData()
	if err != nil {
		log.Errorf("Invalid SQLite data found: %v", err)
		return
	}

	for k, v := range sqliteData {
		pgData[k] = v
	}

	cacheChartsData.Lock()
	cacheChartsData.Data = pgData
	cacheChartsData.Unlock()

	log.Info("Done Pre-populating the charts data")
}

func (exp *explorerUI) Store(blockData *blockdata.BlockData, _ *wire.MsgBlock) error {
	exp.NewBlockDataMtx.Lock()
	bData := blockData.ToBlockExplorerSummary()

	// Update the charts data after every five blocks or if no charts data
	// exists yet.
	if !exp.liteMode && bData.Height%5 == 0 || len(cacheChartsData.Data) == 0 {
		go exp.prePopulateChartsData()
	}

	newBlockData := &BlockBasic{
		Height:         int64(bData.Height),
		Hash:           blockData.Header.Hash,
		Voters:         bData.Voters,
		FreshStake:     bData.FreshStake,
		Size:           int32(bData.Size),
		Transactions:   bData.TxLen,
		BlockTime:      bData.Time,
		FormattedTime:  bData.FormattedTime,
		FormattedBytes: humanize.Bytes(uint64(bData.Size)),
		Revocations:    uint32(bData.Revocations),
	}
	exp.NewBlockData = newBlockData
	percentage := func(a float64, b float64) float64 {
		return (a / b) * 100
	}

	stakePerc := blockData.PoolInfo.Value / dcrutil.Amount(blockData.ExtraInfo.CoinSupply).ToCoin()

	// Update all ExtraInfo with latest data
	exp.ExtraInfo.CoinSupply = blockData.ExtraInfo.CoinSupply
	exp.ExtraInfo.StakeDiff = blockData.CurrentStakeDiff.CurrentStakeDifficulty
	exp.ExtraInfo.IdxBlockInWindow = blockData.IdxBlockInWindow
	exp.ExtraInfo.IdxInRewardWindow = int(newBlockData.Height % exp.ChainParams.SubsidyReductionInterval)
	exp.ExtraInfo.Difficulty = blockData.Header.Difficulty
	exp.ExtraInfo.NBlockSubsidy.Dev = blockData.ExtraInfo.NextBlockSubsidy.Developer
	exp.ExtraInfo.NBlockSubsidy.PoS = blockData.ExtraInfo.NextBlockSubsidy.PoS
	exp.ExtraInfo.NBlockSubsidy.PoW = blockData.ExtraInfo.NextBlockSubsidy.PoW
	exp.ExtraInfo.NBlockSubsidy.Total = blockData.ExtraInfo.NextBlockSubsidy.Total
	exp.ExtraInfo.PoolInfo.Size = blockData.PoolInfo.Size
	exp.ExtraInfo.PoolInfo.Value = blockData.PoolInfo.Value
	exp.ExtraInfo.PoolInfo.ValAvg = blockData.PoolInfo.ValAvg
	exp.ExtraInfo.PoolInfo.Percentage = stakePerc * 100

	exp.ExtraInfo.PoolInfo.PercentTarget = func() float64 {
		target := float64(exp.ChainParams.TicketPoolSize * exp.ChainParams.TicketsPerBlock)
		return float64(blockData.PoolInfo.Size) / target * 100
	}()

	exp.ExtraInfo.TicketReward = func() float64 {
		PosSubPerVote := dcrutil.Amount(blockData.ExtraInfo.NextBlockSubsidy.PoS).ToCoin() / float64(exp.ChainParams.TicketsPerBlock)
		return percentage(PosSubPerVote, blockData.CurrentStakeDiff.CurrentStakeDifficulty)
	}()

	// The actual Reward of a ticket needs to also take into consideration the
	// ticket maturity (time from ticket purchase until its eligible to vote)
	// and coinbase maturity (time after vote until funds distributed to ticket
	// holder are available to use).
	exp.ExtraInfo.RewardPeriod = func() string {
		PosAvgTotalBlocks := float64(
			exp.ExtraInfo.Params.MeanVotingBlocks +
				int64(exp.ChainParams.TicketMaturity) +
				int64(exp.ChainParams.CoinbaseMaturity))
		return fmt.Sprintf("%.2f days", exp.ChainParams.TargetTimePerBlock.Seconds()*PosAvgTotalBlocks/86400)
	}()

	asr, _ := exp.simulateASR(1000, false, stakePerc,
		dcrutil.Amount(blockData.ExtraInfo.CoinSupply).ToCoin(),
		float64(exp.NewBlockData.Height),
		blockData.CurrentStakeDiff.CurrentStakeDifficulty)

	exp.ExtraInfo.ASR = asr

	exp.NewBlockDataMtx.Unlock()

	if !exp.liteMode && exp.devPrefetch {
		go exp.updateDevFundBalance()
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
	exp.NewBlockDataMtx.Lock()
	defer exp.NewBlockDataMtx.Unlock()

	devBalance, err := exp.explorerSource.DevBalance()
	if err == nil && devBalance != nil {
		exp.ExtraInfo.DevFund = devBalance.TotalUnspent
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
		TicketPoolSize := (float64(exp.ExtraInfo.Params.MeanVotingBlocks) + float64(exp.ChainParams.TicketMaturity) +
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
		simblock += (float64(exp.ChainParams.TicketMaturity) + float64(exp.ExtraInfo.Params.MeanVotingBlocks))
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
