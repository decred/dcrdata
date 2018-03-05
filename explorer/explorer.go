// Package explorer handles the block explorer subsystem for generating the
// explorer pages.
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
package explorer

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/blockdata"
	"github.com/decred/dcrdata/db/dbtypes"
	humanize "github.com/dustin/go-humanize"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

const (
	homeTemplateIndex int = iota
	rootTemplateIndex
	mempoolTemplateIndex
	blockTemplateIndex
	txTemplateIndex
	addressTemplateIndex
	decodeTxTemplateIndex
	errorTemplateIndex
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
	CountUnconfirmedTransactions(address string, maxUnconfirmedPossible int64) (int64, error)
	GetMempool() []MempoolTx
}

// explorerDataSource implements extra data retrieval functions that require a
// faster solution than RPC.
type explorerDataSource interface {
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	PoolStatusForTicket(txid string) (dbtypes.TicketSpendType, dbtypes.TicketPoolStatus, error)
	AddressHistory(address string, N, offset int64) ([]*dbtypes.AddressRow, *AddressBalance, error)
	FillAddressTransactions(addrInfo *AddressInfo) error
	BlockMissedVotes(blockHash string) ([]string, error)
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
	templates       []*template.Template
	templateFiles   map[string]string
	templateHelpers template.FuncMap
	wsHub           *WebsocketHub
	NewBlockDataMtx sync.RWMutex
	NewBlockData    *BlockBasic
	ExtraInfo       *HomeInfo
	MempoolData     *MempoolInfo
	ChainParams     *chaincfg.Params
	Version         string
}

func (exp *explorerUI) reloadTemplates() error {
	homeTemplate, err := template.New("home").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["home"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	explorerTemplate, err := template.New("explorer").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["explorer"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	mempoolTemplate, err := template.New("mempool").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["mempool"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	blockTemplate, err := template.New("block").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["block"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	txTemplate, err := template.New("tx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["tx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	addressTemplate, err := template.New("address").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["address"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	decodeTxTemplate, err := template.New("rawtx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["rawtx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	errorTemplate, err := template.New("error").ParseFiles(
		exp.templateFiles["error"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	exp.templates[homeTemplateIndex] = homeTemplate
	exp.templates[rootTemplateIndex] = explorerTemplate
	exp.templates[mempoolTemplateIndex] = mempoolTemplate
	exp.templates[blockTemplateIndex] = blockTemplate
	exp.templates[txTemplateIndex] = txTemplate
	exp.templates[addressTemplateIndex] = addressTemplate
	exp.templates[decodeTxTemplateIndex] = decodeTxTemplate
	exp.templates[errorTemplateIndex] = errorTemplate

	return nil
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

	// Development subsidy address of the current network
	devSubsidyAddress, err := dbtypes.DevSubsidyAddress(params)
	if err != nil {
		log.Warnf("explorer.New: %v", err)
	}
	log.Debugf("Organization address: %s", devSubsidyAddress)
	exp.ExtraInfo = &HomeInfo{
		DevAddress: devSubsidyAddress,
	}

	exp.templateFiles = make(map[string]string)
	exp.templateFiles["home"] = filepath.Join("views", "home.tmpl")
	exp.templateFiles["explorer"] = filepath.Join("views", "explorer.tmpl")
	exp.templateFiles["mempool"] = filepath.Join("views", "mempool.tmpl")
	exp.templateFiles["block"] = filepath.Join("views", "block.tmpl")
	exp.templateFiles["tx"] = filepath.Join("views", "tx.tmpl")
	exp.templateFiles["extras"] = filepath.Join("views", "extras.tmpl")
	exp.templateFiles["address"] = filepath.Join("views", "address.tmpl")
	exp.templateFiles["rawtx"] = filepath.Join("views", "rawtx.tmpl")
	exp.templateFiles["error"] = filepath.Join("views", "error.tmpl")

	toInt64 := func(v interface{}) int64 {
		switch vt := v.(type) {
		case int64:
			return vt
		case int32:
			return int64(vt)
		case uint32:
			return int64(vt)
		case uint64:
			return int64(vt)
		case int:
			return int64(vt)
		case int16:
			return int64(vt)
		case uint16:
			return int64(vt)
		default:
			return math.MinInt64
		}
	}

	exp.templateHelpers = template.FuncMap{
		"add": func(a int64, b int64) int64 {
			return a + b
		},
		"subtract": func(a int64, b int64) int64 {
			return a - b
		},
		"divide": func(n int64, d int64) int64 {
			return n / d
		},
		"multiply": func(a int64, b int64) int64 {
			return a * b
		},
		"timezone": func() string {
			t, _ := time.Now().Zone()
			return t
		},
		"percentage": func(a int64, b int64) float64 {
			return (float64(a) / float64(b)) * 100
		},
		"int64": toInt64,
		"intComma": func(v interface{}) string {
			return humanize.Comma(toInt64(v))
		},
		"int64Comma": func(v int64) string {
			return humanize.Comma(v)
		},
		"ticketWindowProgress": func(i int) float64 {
			p := (float64(i) / float64(exp.ChainParams.StakeDiffWindowSize)) * 100
			return p
		},
		"rewardAdjustmentProgress": func(i int) float64 {
			p := (float64(i) / float64(exp.ChainParams.SubsidyReductionInterval)) * 100
			return p
		},
		"float64AsDecimalParts": func(v float64, useCommas bool) []string {
			clipped := fmt.Sprintf("%.8f", v)
			oldLength := len(clipped)
			clipped = strings.TrimRight(clipped, "0")
			trailingZeros := strings.Repeat("0", oldLength-len(clipped))
			valueChunks := strings.Split(clipped, ".")
			integer := valueChunks[0]
			var dec string
			if len(valueChunks) == 2 {
				dec = valueChunks[1]
			} else {
				dec = ""
				log.Errorf("float64AsDecimalParts has no decimal value. Input: %v", v)
			}
			if useCommas {
				integerAsInt64, err := strconv.ParseInt(integer, 10, 64)
				if err != nil {
					log.Errorf("float64AsDecimalParts comma formatting failed. Input: %v Error: %v", v, err.Error())
					integer = "ERROR"
					dec = "VALUE"
					zeros := ""
					return []string{integer, dec, zeros}
				}
				integer = humanize.Comma(integerAsInt64)
			}
			return []string{integer, dec, trailingZeros}
		},
		"amountAsDecimalParts": func(v int64, useCommas bool) []string {
			amt := strconv.FormatInt(v, 10)
			if len(amt) <= 8 {
				dec := strings.TrimRight(amt, "0")
				trailingZeros := strings.Repeat("0", len(amt)-len(dec))
				leadingZeros := strings.Repeat("0", 8-len(amt))
				return []string{"0", leadingZeros + dec, trailingZeros}
			}
			integer := amt[:len(amt)-8]
			if useCommas {
				integerAsInt64, err := strconv.ParseInt(integer, 10, 64)
				if err != nil {
					log.Errorf("amountAsDecimalParts comma formatting failed. Input: %v Error: %v", v, err.Error())
					integer = "ERROR"
					dec := "VALUE"
					zeros := ""
					return []string{integer, dec, zeros}
				}
				integer = humanize.Comma(integerAsInt64)
			}
			dec := strings.TrimRight(amt[len(amt)-8:], "0")
			zeros := strings.Repeat("0", 8-len(dec))
			return []string{integer, dec, zeros}
		},
		"remaining": func(idx int, max int64, t int64) string {
			x := (max - int64(idx)) * t
			allsecs := int(time.Duration(x).Seconds())
			str := ""
			if allsecs > 604799 {
				weeks := allsecs / 604800
				allsecs %= 604800
				str += fmt.Sprintf("%dw ", weeks)
			}
			if allsecs > 86399 {
				days := allsecs / 86400
				allsecs %= 86400
				str += fmt.Sprintf("%dd ", days)
			}
			if allsecs > 3599 {
				hours := allsecs / 3600
				allsecs %= 3600
				str += fmt.Sprintf("%dh ", hours)
			}
			if allsecs > 59 {
				mins := allsecs / 60
				allsecs %= 60
				str += fmt.Sprintf("%dm ", mins)
			}
			if allsecs > 0 {
				str += fmt.Sprintf("%ds ", allsecs)
			}
			return str + "remaining"
		},
	}

	noTemplateError := func(err error) *explorerUI {
		log.Errorf("Unable to create new html template: %v", err)
		return nil
	}

	exp.templates = make([]*template.Template, 0, 4)

	homeTemplate, err := template.New("home").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["home"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return noTemplateError(err)
	}
	exp.templates = append(exp.templates, homeTemplate)

	explorerTemplate, err := template.New("explorer").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["explorer"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return noTemplateError(err)
	}
	exp.templates = append(exp.templates, explorerTemplate)

	mempoolTemplate, err := template.New("mempool").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["mempool"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return noTemplateError(err)
	}
	exp.templates = append(exp.templates, mempoolTemplate)

	blockTemplate, err := template.New("block").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["block"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return noTemplateError(err)
	}
	exp.templates = append(exp.templates, blockTemplate)

	txTemplate, err := template.New("tx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["tx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return noTemplateError(err)
	}
	exp.templates = append(exp.templates, txTemplate)

	addrTemplate, err := template.New("address").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["address"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return noTemplateError(err)
	}
	exp.templates = append(exp.templates, addrTemplate)

	decodeTxTemplate, err := template.New("rawtx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["rawtx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return noTemplateError(err)
	}
	exp.templates = append(exp.templates, decodeTxTemplate)

	errorTemplate, err := template.New("error").ParseFiles(
		exp.templateFiles["error"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return noTemplateError(err)
	}
	exp.templates = append(exp.templates, errorTemplate)

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

	exp.ExtraInfo = &HomeInfo{
		CoinSupply:        blockData.ExtraInfo.CoinSupply,
		StakeDiff:         blockData.CurrentStakeDiff.CurrentStakeDifficulty,
		IdxBlockInWindow:  blockData.IdxBlockInWindow,
		IdxInRewardWindow: int(newBlockData.Height % exp.ChainParams.SubsidyReductionInterval),
		DevAddress:        exp.ExtraInfo.DevAddress,
		Difficulty:        blockData.Header.Difficulty,
		NBlockSubsidy: BlockSubsidy{
			Dev:   blockData.ExtraInfo.NextBlockSubsidy.Developer,
			PoS:   blockData.ExtraInfo.NextBlockSubsidy.PoS,
			PoW:   blockData.ExtraInfo.NextBlockSubsidy.PoW,
			Total: blockData.ExtraInfo.NextBlockSubsidy.Total,
		},
		Params: ChainParams{
			WindowSize:       exp.ChainParams.StakeDiffWindowSize,
			RewardWindowSize: exp.ChainParams.SubsidyReductionInterval,
			BlockTime:        exp.ChainParams.TargetTimePerBlock.Nanoseconds(),
		},
		PoolInfo: TicketPoolInfo{
			Size:       blockData.PoolInfo.Size,
			Value:      blockData.PoolInfo.Value,
			ValAvg:     blockData.PoolInfo.ValAvg,
			Percentage: percentage(blockData.PoolInfo.Value, dcrutil.Amount(blockData.ExtraInfo.CoinSupply).ToCoin()),
			Target:     exp.ChainParams.TicketPoolSize * exp.ChainParams.TicketsPerBlock,
			PercentTarget: func() float64 {
				target := float64(exp.ChainParams.TicketPoolSize * exp.ChainParams.TicketsPerBlock)
				return float64(blockData.PoolInfo.Size) / target * 100
			}(),
		},
		TicketROI: percentage(dcrutil.Amount(blockData.ExtraInfo.NextBlockSubsidy.PoS).ToCoin()/5, blockData.CurrentStakeDiff.CurrentStakeDifficulty),
		ROIPeriod: fmt.Sprintf("%.2f days", exp.ChainParams.TargetTimePerBlock.Seconds()*float64(exp.ChainParams.TicketPoolSize)/86400),
	}

	if !exp.liteMode {
		exp.ExtraInfo.DevFund = 0
		go exp.updateDevFundBalance()
	}

	exp.NewBlockDataMtx.Unlock()

	exp.wsHub.HubRelay <- sigNewBlock

	log.Debugf("Got new block %d for the explorer.", newBlockData.Height)

	return nil
}

func (exp *explorerUI) updateDevFundBalance() {
	// yield processor to other goroutines
	runtime.Gosched()
	exp.NewBlockDataMtx.Lock()
	defer exp.NewBlockDataMtx.Unlock()

	_, devBalance, err := exp.explorerSource.AddressHistory(exp.ExtraInfo.DevAddress, 1, 0)
	if err == nil && devBalance != nil {
		exp.ExtraInfo.DevFund = devBalance.TotalUnspent
	} else {
		log.Warnf("explorerUI.updateDevFundBalance failed: %v", err)
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
