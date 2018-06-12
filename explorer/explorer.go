// Package explorer handles the block explorer subsystem for generating the
// explorer pages.
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
package explorer

import (
	"fmt"
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
	AgendaVotes(agendaID string) ([]*dbtypes.AgendaVoteChoices, *dbtypes.AgendaVoteChoices, error)
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
	useRealIP bool, appVersion string) *explorerUI {
	exp := new(explorerUI)
	exp.Mux = chi.NewRouter()
	exp.blockData = dataSource
	exp.explorerSource = primaryDataSource
	exp.MempoolData = new(MempoolInfo)
	exp.Version = appVersion
	// explorerDataSource is an interface that could have a value of pointer
	// type, and if either is nil this means lite mode.
	if exp.explorerSource == nil || reflect.ValueOf(exp.explorerSource).IsNil() {
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
		},
		PoolInfo: TicketPoolInfo{
			Target: exp.ChainParams.TicketPoolSize * exp.ChainParams.TicketsPerBlock,
		},
	}

	noTemplateError := func(err error) *explorerUI {
		log.Errorf("Unable to create new html template: %v", err)
		return nil
	}
	tmpls := []string{"home", "explorer", "mempool", "block", "tx", "address", "rawtx", "error", "parameters", "agenda", "agendas"}

	tempDefaults := []string{"extras"}

	exp.templates = newTemplates("views", tempDefaults, makeTemplateFuncMap(exp.ChainParams))

	for _, name := range tmpls {
		if err := exp.templates.addTemplate(name); err != nil {
			return noTemplateError(err)
		}
	}

	exp.addRoutes()

	exp.wsHub = NewWebsocketHub()

	go exp.wsHub.run()

	return exp
}

func (exp *explorerUI) Store(blockData *blockdata.BlockData, _ *wire.MsgBlock) error {
	exp.NewBlockDataMtx.Lock()
	bData := blockData.ToBlockExplorerSummary()
	newBlockData := &BlockBasic{
		Height:         int64(bData.Height),
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
	exp.ExtraInfo.PoolInfo.Percentage = percentage(blockData.PoolInfo.Value, dcrutil.Amount(blockData.ExtraInfo.CoinSupply).ToCoin())

	exp.ExtraInfo.PoolInfo.PercentTarget = func() float64 {
		target := float64(exp.ChainParams.TicketPoolSize * exp.ChainParams.TicketsPerBlock)
		return float64(blockData.PoolInfo.Size) / target * 100
	}()

	exp.ExtraInfo.TicketROI = func() float64 {
		PosSubPerVote := dcrutil.Amount(blockData.ExtraInfo.NextBlockSubsidy.PoS).ToCoin() / float64(exp.ChainParams.TicketsPerBlock)
		return percentage(PosSubPerVote, blockData.CurrentStakeDiff.CurrentStakeDifficulty)
	}()

	// If there are ticketpoolsize*TicketsPerBlock total tickets and
	// TicketsPerBlock are drawn every block, and assuming random selection
	// of tickets, then any one ticket will, on average, be selected to vote
	// once every ticketpoolsize blocks

	// Small deviations in reality are due to:
	// 1.  Not all blocks have 5 votes.  On average each block in Decred
	// currently has about 4.8 votes per block
	// 2.  Total tickets in the pool varies slightly above and below
	// ticketpoolsize*TicketsPerBlock depending on supply and demand

	// Both minor deviations are not accounted for in the general ROI
	// calculation below because the variance they cause would be would be
	// extremely small.

	// The actual ROI of a ticket needs to also take into consideration the
	// ticket maturity (time from ticket purchase until its eligible to vote)
	// and coinbase maturity (time after vote until funds distributed to
	// ticket holder are avaliable to use)
	exp.ExtraInfo.ROIPeriod = func() string {
		PosAvgTotalBlocks := float64(
			exp.ChainParams.TicketPoolSize +
				exp.ChainParams.TicketMaturity +
				exp.ChainParams.CoinbaseMaturity)
		return fmt.Sprintf("%.2f days", exp.ChainParams.TargetTimePerBlock.Seconds()*PosAvgTotalBlocks/86400)
	}()

	exp.NewBlockDataMtx.Unlock()

	if !exp.liteMode {
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
