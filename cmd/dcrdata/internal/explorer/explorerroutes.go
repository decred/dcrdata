// Copyright (c) 2018-2022, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"

	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"

	"github.com/decred/dcrdata/exchanges/v3"
	"github.com/decred/dcrdata/gov/v6/agendas"
	pitypes "github.com/decred/dcrdata/gov/v6/politeia/types"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/explorer/types"
	"github.com/decred/dcrdata/v8/txhelpers"
	ticketvotev1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"

	humanize "github.com/dustin/go-humanize"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	titler       = cases.Title(language.AmericanEnglish)
	dummyRequest = new(http.Request)
)

func init() {
	// URL should be set because commonData call a method on it.
	dummyRequest.URL, _ = url.Parse("/")
}

// Cookies contains information from the request cookies.
type Cookies struct {
	DarkMode bool
}

// CommonPageData is the basis for data structs used for HTML templates.
// explorerUI.commonData returns an initialized instance or CommonPageData,
// which itself should be used to initialize page data template structs.
type CommonPageData struct {
	Tip           *types.WebBasicBlock
	Version       string
	ChainParams   *chaincfg.Params
	BlockTimeUnix int64
	DevAddress    string
	Links         *links
	NetName       string
	Cookies       Cookies
	Host          string
	BaseURL       string // scheme + "://" + "host"
	Path          string
	RequestURI    string // path?query
}

// FullURL constructs the page's complete URL.
func (cp *CommonPageData) FullURL() string {
	return cp.BaseURL + cp.RequestURI
}

// CanonicalURL constructs the page's canonical URL. According to Facebook:
// "This should be the undecorated URL, without session variables, user
// identifying parameters, or counters. Likes and Shares for this URL will
// aggregate at this URL."
// https://developers.facebook.com/docs/sharing/webmasters/
func (cp *CommonPageData) CanonicalURL() string {
	return cp.BaseURL + cp.Path
}

// Status page strings
const (
	defaultErrorCode    = "Something went wrong..."
	defaultErrorMessage = "Try refreshing... it usually fixes things."
	pageDisabledCode    = "%s has been disabled for now."
	wrongNetwork        = "Wrong Network"
)

// expStatus defines the various status types supported by the system.
type expStatus string

// These are the explorer status messages used by the status page.
const (
	ExpStatusError          expStatus = "Error"
	ExpStatusNotFound       expStatus = "Not Found"
	ExpStatusFutureBlock    expStatus = "Future Block"
	ExpStatusNotSupported   expStatus = "Not Supported"
	ExpStatusBadRequest     expStatus = "Bad Request"
	ExpStatusNotImplemented expStatus = "Not Implemented"
	ExpStatusPageDisabled   expStatus = "Page Disabled"
	ExpStatusWrongNetwork   expStatus = "Wrong Network"
	ExpStatusDeprecated     expStatus = "Deprecated"
	ExpStatusSyncing        expStatus = "Blocks Syncing"
	ExpStatusDBTimeout      expStatus = "Database Timeout"
	ExpStatusP2PKAddress    expStatus = "P2PK Address Type"
)

func (e expStatus) IsNotFound() bool {
	return e == ExpStatusNotFound
}

func (e expStatus) IsWrongNet() bool {
	return e == ExpStatusWrongNetwork
}

func (e expStatus) IsP2PKAddress() bool {
	return e == ExpStatusP2PKAddress
}

func (e expStatus) IsFutureBlock() bool {
	return e == ExpStatusFutureBlock
}

func (e expStatus) IsSyncing() bool {
	return e == ExpStatusSyncing
}

// number of blocks displayed on /visualblocks
const homePageBlocksMaxCount = 30

// netName returns the name used when referring to a decred network.
func netName(chainParams *chaincfg.Params) string {
	if chainParams == nil {
		return "invalid"
	}
	if strings.HasPrefix(strings.ToLower(chainParams.Name), "testnet") {
		return testnetNetName
	}
	return titler.String(chainParams.Name)
}

func (exp *explorerUI) timeoutErrorPage(w http.ResponseWriter, err error, debugStr string) (wasTimeout bool) {
	wasTimeout = dbtypes.IsTimeoutErr(err)
	if wasTimeout {
		log.Debugf("%s: %v", debugStr, err)
		exp.StatusPage(w, defaultErrorCode,
			"Database timeout. Please try again later.", "", ExpStatusDBTimeout)
	}
	return
}

// For the exchange rates on the homepage
type homeConversions struct {
	ExchangeRate    *exchanges.Conversion
	StakeDiff       *exchanges.Conversion
	CoinSupply      *exchanges.Conversion
	PowSplit        *exchanges.Conversion
	TreasurySplit   *exchanges.Conversion
	TreasuryBalance *exchanges.Conversion
}

// Home is the page handler for the "/" path.
func (exp *explorerUI) Home(w http.ResponseWriter, r *http.Request) {
	height, err := exp.dataSource.GetHeight()
	if err != nil {
		log.Errorf("GetHeight failed: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "",
			ExpStatusError)
		return
	}

	blocks := exp.dataSource.GetExplorerBlocks(int(height), int(height)-8)
	var bestBlock *types.BlockBasic
	if blocks == nil {
		bestBlock = new(types.BlockBasic)
	} else {
		bestBlock = blocks[0]
	}

	// Safely retrieve the current inventory pointer.
	inv := exp.MempoolInventory()

	// Lock the shared inventory struct from change (e.g. in MempoolMonitor).
	inv.RLock()
	exp.pageData.RLock()

	tallys, consensus := inv.VotingInfo.BlockStatus(bestBlock.Hash)

	// Get fiat conversions if available
	homeInfo := exp.pageData.HomeInfo
	var conversions *homeConversions
	xcBot := exp.xcBot
	if xcBot != nil {
		conversions = &homeConversions{
			ExchangeRate:    xcBot.Conversion(1.0),
			StakeDiff:       xcBot.Conversion(homeInfo.StakeDiff),
			CoinSupply:      xcBot.Conversion(dcrutil.Amount(homeInfo.CoinSupply).ToCoin()),
			PowSplit:        xcBot.Conversion(dcrutil.Amount(homeInfo.NBlockSubsidy.PoW).ToCoin()),
			TreasurySplit:   xcBot.Conversion(dcrutil.Amount(homeInfo.NBlockSubsidy.Dev).ToCoin()),
			TreasuryBalance: xcBot.Conversion(dcrutil.Amount(homeInfo.DevFund + homeInfo.TreasuryBalance.Balance).ToCoin()),
		}
	}

	str, err := exp.templates.exec("home", struct {
		*CommonPageData
		Info          *types.HomeInfo
		Mempool       *types.MempoolInfo
		BestBlock     *types.BlockBasic
		BlockTally    []int
		Consensus     int
		Blocks        []*types.BlockBasic
		Conversions   *homeConversions
		PercentChange float64
	}{
		CommonPageData: exp.commonData(r),
		Info:           homeInfo,
		Mempool:        inv,
		BestBlock:      bestBlock,
		BlockTally:     tallys,
		Consensus:      consensus,
		Blocks:         blocks,
		Conversions:    conversions,
		PercentChange:  homeInfo.PoolInfo.PercentTarget - 100,
	})

	inv.RUnlock()
	exp.pageData.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// SideChains is the page handler for the "/side" path.
func (exp *explorerUI) SideChains(w http.ResponseWriter, r *http.Request) {
	sideBlocks, err := exp.dataSource.SideChainBlocks()
	if exp.timeoutErrorPage(w, err, "SideChainBlocks") {
		return
	}
	if err != nil {
		log.Errorf("Unable to get side chain blocks: %v", err)
		exp.StatusPage(w, defaultErrorCode,
			"failed to retrieve side chain blocks", "", ExpStatusError)
		return
	}

	str, err := exp.templates.exec("sidechains", struct {
		*CommonPageData
		Data []*dbtypes.BlockStatus
	}{
		CommonPageData: exp.commonData(r),
		Data:           sideBlocks,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// InsightRootPage is the page for the "/insight" path.
func (exp *explorerUI) InsightRootPage(w http.ResponseWriter, r *http.Request) {
	str, err := exp.templates.exec("insight_root", struct {
		*CommonPageData
	}{
		CommonPageData: exp.commonData(r),
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// DisapprovedBlocks is the page handler for the "/disapproved" path.
func (exp *explorerUI) DisapprovedBlocks(w http.ResponseWriter, r *http.Request) {
	disapprovedBlocks, err := exp.dataSource.DisapprovedBlocks()
	if exp.timeoutErrorPage(w, err, "DisapprovedBlocks") {
		return
	}
	if err != nil {
		log.Errorf("Unable to get stakeholder disapproved blocks: %v", err)
		exp.StatusPage(w, defaultErrorCode,
			"failed to retrieve stakeholder disapproved blocks", "", ExpStatusError)
		return
	}

	str, err := exp.templates.exec("disapproved", struct {
		*CommonPageData
		Data []*dbtypes.BlockStatus
	}{
		CommonPageData: exp.commonData(r),
		Data:           disapprovedBlocks,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// VisualBlocks is the page handler for the "/visualblocks" path.
func (exp *explorerUI) VisualBlocks(w http.ResponseWriter, r *http.Request) {
	// Get top N blocks and trim each block to have just the fields required for
	// this page.
	height, err := exp.dataSource.GetHeight()
	if err != nil {
		log.Errorf("GetHeight failed: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "",
			ExpStatusError)
		return
	}
	blocks := exp.dataSource.GetExplorerFullBlocks(int(height),
		int(height)-homePageBlocksMaxCount)

	// trim unwanted data in each block
	trimmedBlocks := make([]*types.TrimmedBlockInfo, 0, len(blocks))
	for _, block := range blocks {
		trimmedBlock := &types.TrimmedBlockInfo{
			Time:         block.BlockTime,
			Height:       block.Height,
			Total:        block.TotalSent,
			Fees:         block.MiningFee,
			Subsidy:      block.Subsidy,
			Votes:        block.Votes,
			Tickets:      block.Tickets,
			Revocations:  block.Revs,
			Transactions: types.FilterRegularTx(block.Tx),
		}

		trimmedBlocks = append(trimmedBlocks, trimmedBlock)
	}

	// Construct the required TrimmedMempoolInfo from the shared inventory.
	inv := exp.MempoolInventory()
	mempoolInfo := inv.Trim() // Trim internally locks the MempoolInfo.

	exp.pageData.RLock()
	mempoolInfo.Subsidy = exp.pageData.HomeInfo.NBlockSubsidy

	str, err := exp.templates.exec("visualblocks", struct {
		*CommonPageData
		Info    *types.HomeInfo
		Mempool *types.TrimmedMempoolInfo
		Blocks  []*types.TrimmedBlockInfo
	}{
		CommonPageData: exp.commonData(r),
		Info:           exp.pageData.HomeInfo,
		Mempool:        mempoolInfo,
		Blocks:         trimmedBlocks,
	})

	exp.pageData.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// StakeDiffWindows is the page handler for the "/ticketpricewindows" path.
func (exp *explorerUI) StakeDiffWindows(w http.ResponseWriter, r *http.Request) {
	var offsetWindow uint64
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		o, err := strconv.ParseUint(offsetStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		offsetWindow = o
	}

	var rows uint64
	if rowsStr := r.URL.Query().Get("rows"); rowsStr != "" {
		o, err := strconv.ParseUint(rowsStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		rows = o
	}

	bestWindow := uint64(exp.Height() / exp.ChainParams.StakeDiffWindowSize)
	if offsetWindow > bestWindow {
		offsetWindow = bestWindow
	}
	if rows == 0 {
		rows = minExplorerRows
	} else if rows > maxExplorerRows {
		rows = maxExplorerRows
	}

	windows, err := exp.dataSource.PosIntervals(rows, offsetWindow)
	if exp.timeoutErrorPage(w, err, "PosIntervals") {
		return
	}
	if err != nil {
		log.Errorf("The specified windows are invalid. offset=%d&rows=%d: "+
			"error: %v ", offsetWindow, rows, err)
		exp.StatusPage(w, defaultErrorCode,
			"The specified ticket price windows could not be found", "", ExpStatusNotFound)
		return
	}

	linkTemplate := "/ticketpricewindows?offset=%d&rows=" + strconv.FormatUint(rows, 10)

	str, err := exp.templates.exec("windows", struct {
		*CommonPageData
		Data         []*dbtypes.BlocksGroupedInfo
		WindowSize   int64
		BestWindow   int64
		OffsetWindow int64
		Limit        int64
		TimeGrouping string
		Pages        pageNumbers
	}{
		CommonPageData: exp.commonData(r),
		Data:           windows,
		WindowSize:     exp.ChainParams.StakeDiffWindowSize,
		BestWindow:     int64(bestWindow),
		OffsetWindow:   int64(offsetWindow),
		Limit:          int64(rows),
		TimeGrouping:   "Windows",
		Pages:          calcPages(int(bestWindow), int(rows), int(offsetWindow), linkTemplate),
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// DayBlocksListing handles "/day" page.
func (exp *explorerUI) DayBlocksListing(w http.ResponseWriter, r *http.Request) {
	exp.timeBasedBlocksListing("Days", w, r)
}

// WeekBlocksListing handles "/week" page.
func (exp *explorerUI) WeekBlocksListing(w http.ResponseWriter, r *http.Request) {
	exp.timeBasedBlocksListing("Weeks", w, r)
}

// MonthBlocksListing handles "/month" page.
func (exp *explorerUI) MonthBlocksListing(w http.ResponseWriter, r *http.Request) {
	exp.timeBasedBlocksListing("Months", w, r)
}

// YearBlocksListing handles "/year" page.
func (exp *explorerUI) YearBlocksListing(w http.ResponseWriter, r *http.Request) {
	exp.timeBasedBlocksListing("Years", w, r)
}

// TimeBasedBlocksListing is the main handler for "/day", "/week", "/month" and
// "/year".
func (exp *explorerUI) timeBasedBlocksListing(val string, w http.ResponseWriter, r *http.Request) {
	var offset uint64
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		o, err := strconv.ParseUint(offsetStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		offset = o
	}
	var rows uint64
	if rowsStr := r.URL.Query().Get("rows"); rowsStr != "" {
		o, err := strconv.ParseUint(rowsStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		rows = o
	}
	grouping := dbtypes.TimeGroupingFromStr(val)
	i, err := dbtypes.TimeBasedGroupingToInterval(grouping)
	if err != nil {
		// default to year grouping if grouping is missing
		i, err = dbtypes.TimeBasedGroupingToInterval(dbtypes.YearGrouping)
		if err != nil {
			exp.StatusPage(w, defaultErrorCode, "Invalid year grouping found.", "",
				ExpStatusError)
			log.Errorf("Invalid year grouping found: error: %v ", err)
			return
		}
		grouping = dbtypes.YearGrouping
	}

	oldestBlockTime := exp.ChainParams.GenesisBlock.Header.Timestamp.Unix()
	maxOffset := (time.Now().Unix() - oldestBlockTime) / int64(i)
	m := uint64(maxOffset)
	if offset > m {
		offset = m
	}

	oldestBlockTimestamp := exp.ChainParams.GenesisBlock.Header.Timestamp
	oldestBlockMonth := oldestBlockTimestamp.Month()
	oldestBlockDay := oldestBlockTimestamp.Day()

	now := time.Now()

	if (grouping == dbtypes.YearGrouping && now.Month() < oldestBlockMonth) ||
		grouping == dbtypes.MonthGrouping && now.Day() < oldestBlockDay ||
		grouping == dbtypes.YearGrouping && now.Month() == oldestBlockMonth && now.Day() < oldestBlockDay {
		maxOffset = maxOffset + 1
	}

	if rows == 0 {
		rows = minExplorerRows
	} else if rows > maxExplorerRows {
		rows = maxExplorerRows
	}

	data, err := exp.dataSource.TimeBasedIntervals(grouping, rows, offset)
	if exp.timeoutErrorPage(w, err, "TimeBasedIntervals") {
		return
	}
	if err != nil {
		log.Errorf("The specified /%s intervals are invalid. offset=%d&rows=%d: "+
			"error: %v ", val, offset, rows, err)
		exp.StatusPage(w, defaultErrorCode,
			"The specified block intervals could be not found", "", ExpStatusNotFound)
		return
	}

	lastOffsetRows := uint64(maxOffset) % rows
	var lastOffset uint64

	if lastOffsetRows == 0 && uint64(maxOffset) > rows {
		lastOffset = uint64(maxOffset) - rows
	} else if lastOffsetRows > 0 && uint64(maxOffset) > rows {
		lastOffset = uint64(maxOffset) - lastOffsetRows
	}

	// If the view is "years" and the top row is this year, modify the formatted
	// time string to indicate its a partial result.
	if val == "Years" && len(data) > 0 && data[0].EndTime.T.Year() == time.Now().Year() {
		data[0].FormattedStartTime = fmt.Sprintf("%s YTD", time.Now().Format("2006"))
	}

	linkTemplate := "/" + strings.ToLower(val) + "?offset=%d&rows=" + strconv.FormatUint(rows, 10)

	str, err := exp.templates.exec("timelisting", struct {
		*CommonPageData
		Data         []*dbtypes.BlocksGroupedInfo
		TimeGrouping string
		Offset       int64
		Limit        int64
		BestGrouping int64
		LastOffset   int64
		Pages        pageNumbers
	}{
		CommonPageData: exp.commonData(r),
		Data:           data,
		TimeGrouping:   val,
		Offset:         int64(offset),
		Limit:          int64(rows),
		BestGrouping:   maxOffset,
		LastOffset:     int64(lastOffset),
		Pages:          calcPages(int(maxOffset), int(rows), int(offset), linkTemplate),
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Blocks is the page handler for the "/blocks" path.
func (exp *explorerUI) Blocks(w http.ResponseWriter, r *http.Request) {
	bestBlockHeight, err := exp.dataSource.GetHeight()
	if err != nil {
		log.Errorf("GetHeight failed: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "",
			ExpStatusError)
		return
	}

	var height int64
	if heightStr := r.URL.Query().Get("height"); heightStr != "" {
		h, err := strconv.ParseUint(heightStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		height = int64(h)
	} else {
		height = bestBlockHeight
	}

	var rows int64
	if rowsStr := r.URL.Query().Get("rows"); rowsStr != "" {
		h, err := strconv.ParseUint(rowsStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		rows = int64(h)
	}

	if height > bestBlockHeight {
		height = bestBlockHeight
	}
	if rows == 0 {
		rows = minExplorerRows
	} else if rows > maxExplorerRows {
		rows = maxExplorerRows
	}
	var end int
	oldestBlock := height - rows + 1
	if oldestBlock < 0 {
		end = -1
	} else {
		end = int(height - rows)
	}

	summaries := exp.dataSource.GetExplorerBlocks(int(height), end)
	if summaries == nil {
		log.Errorf("Unable to get blocks: height=%d&rows=%d", height, rows)
		exp.StatusPage(w, defaultErrorCode, "could not find those blocks", "",
			ExpStatusNotFound)
		return
	}

	for _, s := range summaries {
		blockStatus, err := exp.dataSource.BlockStatus(s.Hash)
		if exp.timeoutErrorPage(w, err, "BlockStatus") {
			return
		}
		if err != nil && !errors.Is(err, dbtypes.ErrNoResult) {
			log.Warnf("Unable to retrieve chain status for block %s: %v",
				s.Hash, err)
		}
		s.Valid = blockStatus.IsValid
		s.MainChain = blockStatus.IsMainchain
	}

	linkTemplate := "/blocks?height=%d&rows=" + strconv.FormatInt(rows, 10)

	oldestHeight := bestBlockHeight % rows

	str, err := exp.templates.exec("blocks", struct {
		*CommonPageData
		Data         []*types.BlockBasic
		BestBlock    int64
		OldestHeight int64
		Rows         int64
		RowsCount    int64
		WindowSize   int64
		TimeGrouping string
		Pages        pageNumbers
	}{
		CommonPageData: exp.commonData(r),
		Data:           summaries,
		BestBlock:      bestBlockHeight,
		OldestHeight:   oldestHeight,
		Rows:           rows,
		RowsCount:      int64(len(summaries)),
		WindowSize:     exp.ChainParams.StakeDiffWindowSize,
		TimeGrouping:   "Blocks",
		Pages:          calcPagesDesc(int(bestBlockHeight), int(rows), int(height), linkTemplate),
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Block is the page handler for the "/block" path.
func (exp *explorerUI) Block(w http.ResponseWriter, r *http.Request) {
	// Retrieve the block specified on the path.
	hash := getBlockHashCtx(r)
	data := exp.dataSource.GetExplorerBlock(hash)
	if data == nil {
		log.Errorf("Unable to get block %s", hash)
		exp.StatusPage(w, defaultErrorCode, "could not find that block", "",
			ExpStatusNotFound)
		return
	}

	// Check if there are any regular non-coinbase transactions in the block.
	data.TxAvailable = len(data.Tx) > 1

	// Retrieve missed votes, main/side chain status, and stakeholder approval.
	var err error
	data.Misses, err = exp.dataSource.BlockMissedVotes(hash)
	if exp.timeoutErrorPage(w, err, "BlockMissedVotes") {
		return
	}
	if err != nil && !errors.Is(err, dbtypes.ErrNoResult) {
		log.Warnf("Unable to retrieve missed votes for block %s: %v", hash, err)
	}

	var altBlocks []*dbtypes.BlockStatus
	altBlocks, err = exp.dataSource.BlockStatuses(data.Height)
	if exp.timeoutErrorPage(w, err, "BlockStatuses") {
		return
	}
	if err != nil && !errors.Is(err, dbtypes.ErrNoResult) {
		log.Warnf("Unable to retrieve chain status for block %s: %v", hash, err)
	}
	for i, block := range altBlocks {
		if block.Hash == hash {
			data.Valid = block.IsValid
			data.MainChain = block.IsMainchain
			altBlocks = append(altBlocks[:i], altBlocks[i+1:]...)
			break
		}
	}

	pageData := struct {
		*CommonPageData
		Data           *types.BlockInfo
		AltBlocks      []*dbtypes.BlockStatus
		FiatConversion *exchanges.Conversion
	}{
		CommonPageData: exp.commonData(r),
		Data:           data,
		AltBlocks:      altBlocks,
	}

	if exp.xcBot != nil && time.Since(data.BlockTime.T) < time.Hour {
		pageData.FiatConversion = exp.xcBot.Conversion(data.TotalSent)
	}

	str, err := exp.templates.exec("block", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Mempool is the page handler for the "/mempool" path.
func (exp *explorerUI) Mempool(w http.ResponseWriter, r *http.Request) {
	// Safely retrieve the inventory pointer, which can be reset in StoreMPData.
	inv := exp.MempoolInventory()

	// Prevent modifications to the shared inventory struct (e.g. in the
	// MempoolMonitor) while marshaling the inventory.
	inv.RLock()
	str, err := exp.templates.exec("mempool", struct {
		*CommonPageData
		Mempool *types.MempoolInfo
	}{
		CommonPageData: exp.commonData(r),
		Mempool:        inv,
	})
	inv.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Ticketpool is the page handler for the "/ticketpool" path.
func (exp *explorerUI) Ticketpool(w http.ResponseWriter, r *http.Request) {
	str, err := exp.templates.exec("ticketpool", exp.commonData(r))

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// TxPage is the page handler for the "/tx" path.
func (exp *explorerUI) TxPage(w http.ResponseWriter, r *http.Request) {
	// attempt to get tx hash string from URL path
	hash, ok := r.Context().Value(ctxTxHash).(string)
	if !ok {
		log.Trace("txid not set")
		exp.StatusPage(w, defaultErrorCode, "there was no transaction requested",
			"", ExpStatusNotFound)
		return
	}

	// Weed out bad hashes before we try any queries or RPCs.
	_, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		exp.StatusPage(w, defaultErrorCode, "Invalid transaction ID", "", ExpStatusError)
		return
	}

	inout, _ := r.Context().Value(ctxTxInOut).(string)
	if inout != "in" && inout != "out" && inout != "" {
		exp.StatusPage(w, defaultErrorCode, "there was no transaction requested",
			"", ExpStatusNotFound)
		return
	}
	ioid, _ := r.Context().Value(ctxTxInOutId).(string)
	inoutid, _ := strconv.ParseInt(ioid, 10, 0)

	tx := exp.dataSource.GetExplorerTx(hash)
	// If dcrd has no information about the transaction, pull the transaction
	// details from the auxiliary DB database.
	if tx == nil {
		log.Warnf("No transaction information for %v. Trying tables in case this is an orphaned txn.", hash)
		// Search for occurrences of the transaction in the database.
		dbTxs, err := exp.dataSource.Transaction(hash)
		if exp.timeoutErrorPage(w, err, "Transaction") {
			return
		}
		if err != nil {
			log.Errorf("Unable to retrieve transaction details for %s.", hash)
			exp.StatusPage(w, defaultErrorCode, "could not find that transaction",
				"", ExpStatusNotFound)
			return
		}
		if dbTxs == nil {
			exp.StatusPage(w, defaultErrorCode, "that transaction has not been recorded",
				"", ExpStatusNotFound)
			return
		}

		// Take the first one. The query order should put valid at the top of
		// the list. Regardless of order, the transaction web page will link to
		// all occurrences of the transaction.
		dbTx0 := dbTxs[0]
		fees := dcrutil.Amount(dbTx0.Fees)
		txType := int(dbTx0.TxType)
		txTypeStr := txhelpers.TxTypeToString(txType)
		tx = &types.TxInfo{
			TxBasic: &types.TxBasic{
				TxID:          hash,
				Type:          txTypeStr,
				Version:       int32(dbTx0.Version),
				FormattedSize: humanize.Bytes(uint64(dbTx0.Size)),
				Total:         dcrutil.Amount(dbTx0.Sent).ToCoin(),
				Fee:           fees,
				FeeRate:       dcrutil.Amount((1000 * int64(fees)) / int64(dbTx0.Size)),
				// VoteInfo TODO - check votes table
				Coinbase:     dbTx0.BlockIndex == 0 && dbTx0.Tree == wire.TxTreeRegular,
				Treasurybase: stake.TxType(txType) == stake.TxTypeTreasuryBase,
			},
			SpendingTxns: make([]types.TxInID, len(dbTx0.VoutDbIds)), // SpendingTxns filled below
			// Vins - looked-up in vins table
			// Vouts - looked-up in vouts table
			BlockHeight:   dbTx0.BlockHeight,
			BlockIndex:    dbTx0.BlockIndex,
			BlockHash:     dbTx0.BlockHash,
			Confirmations: exp.Height() - dbTx0.BlockHeight + 1,
			Time:          types.TimeDef(dbTx0.Time),
		}

		// Coinbase transactions are regular, but call them coinbase for the page.
		if tx.Coinbase {
			tx.Type = types.CoinbaseTypeStr
		}

		// TODO: tx.TSpendTally?

		// Retrieve vouts from DB.
		vouts, err := exp.dataSource.VoutsForTx(dbTx0)
		if exp.timeoutErrorPage(w, err, "VoutsForTx") {
			return
		}
		if err != nil {
			log.Errorf("Failed to retrieve all vout details for transaction %s: %v",
				dbTx0.TxID, err)
			exp.StatusPage(w, defaultErrorCode, "VoutsForTx failed", "", ExpStatusError)
			return
		}

		// Convert to explorer.Vout, getting spending information from DB.
		for iv := range vouts {
			// Check pkScript for OP_RETURN and OP_TADD.
			pkScript := vouts[iv].ScriptPubKey
			opTAdd := len(pkScript) > 0 && pkScript[0] == txscript.OP_TADD
			var opReturn string
			if !opTAdd {
				asm, _ := txscript.DisasmString(pkScript)
				if strings.HasPrefix(asm, "OP_RETURN") {
					opReturn = asm
				}
			}
			// Determine if the outpoint is spent
			spendingTx, _, _, err := exp.dataSource.SpendingTransaction(hash, vouts[iv].TxIndex)
			if exp.timeoutErrorPage(w, err, "SpendingTransaction") {
				return
			}
			if err != nil && !errors.Is(err, dbtypes.ErrNoResult) {
				log.Warnf("SpendingTransaction failed for outpoint %s:%d: %v",
					hash, vouts[iv].TxIndex, err)
			}
			amount := dcrutil.Amount(int64(vouts[iv].Value)).ToCoin()
			tx.Vout = append(tx.Vout, types.Vout{
				Addresses:       vouts[iv].ScriptPubKeyData.Addresses,
				Amount:          amount,
				FormattedAmount: humanize.Commaf(amount),
				Type:            vouts[iv].ScriptPubKeyData.Type.String(),
				Spent:           spendingTx != "",
				OP_RETURN:       opReturn,
				OP_TADD:         opTAdd,
				Index:           vouts[iv].TxIndex,
				Version:         vouts[iv].Version,
			})
		}

		// Retrieve vins from DB.
		vins, prevPkScripts, scriptVersions, err := exp.dataSource.VinsForTx(dbTx0)
		if exp.timeoutErrorPage(w, err, "VinsForTx") {
			return
		}
		if err != nil {
			log.Errorf("Failed to retrieve all vin details for transaction %s: %v",
				dbTx0.TxID, err)
			exp.StatusPage(w, defaultErrorCode, "VinsForTx failed", "", ExpStatusError)
			return
		}

		// Convert to explorer.Vin from dbtypes.VinTxProperty.
		for iv := range vins {
			// Decode all addresses from previous outpoint's pkScript.
			var addresses []string
			pkScriptsStr, err := hex.DecodeString(prevPkScripts[iv])
			if err != nil {
				log.Errorf("Failed to decode pkScript: %v", err)
			}
			_, scrAddrs := stdscript.ExtractAddrs(scriptVersions[iv], pkScriptsStr, exp.ChainParams)
			for ia := range scrAddrs {
				addresses = append(addresses, scrAddrs[ia].String())
			}

			// If the scriptsig does not decode or disassemble, oh well.
			asm, _ := txscript.DisasmString(vins[iv].ScriptSig)

			txIndex := vins[iv].TxIndex
			amount := dcrutil.Amount(vins[iv].ValueIn).ToCoin()
			var coinbase, stakebase, tspend string
			var treasuryBase bool
			if txIndex == 0 {
				if tx.Coinbase {
					coinbase = hex.EncodeToString(txhelpers.CoinbaseScript)
				} else if tx.IsVote() {
					stakebase = hex.EncodeToString(txhelpers.CoinbaseScript)
				} else if stake.TxType(dbTx0.TxType) == stake.TxTypeTreasuryBase {
					treasuryBase = true
				} else if stake.TxType(dbTx0.TxType) == stake.TxTypeTSpend {
					tspend = "<unknown>" // todo: maybe store the tspend sigscript in db so we can retrieve it without dcrd, maybe not
				}
			}
			tx.Vin = append(tx.Vin, types.Vin{
				Vin: &chainjson.Vin{
					Coinbase:      coinbase,
					Stakebase:     stakebase,
					Treasurybase:  treasuryBase,
					TreasurySpend: tspend,
					Txid:          hash,
					Vout:          vins[iv].PrevTxIndex,
					Tree:          dbTx0.Tree,
					Sequence:      vins[iv].Sequence,
					AmountIn:      amount,
					BlockHeight:   uint32(tx.BlockHeight),
					BlockIndex:    tx.BlockIndex,
					ScriptSig: &chainjson.ScriptSig{
						Asm: asm,
						Hex: hex.EncodeToString(vins[iv].ScriptSig),
					},
				},
				Addresses:       addresses,
				FormattedAmount: humanize.Commaf(amount),
				Index:           txIndex,
			})
		}

		// For coinbase, stakebase, and treasury txns, get maturity status.
		if tx.Coinbase || tx.IsVote() || tx.IsRevocation() || tx.IsTreasuryAdd() ||
			tx.IsTreasurySpend() || tx.IsTreasurybase() {
			tx.Maturity = int64(exp.ChainParams.CoinbaseMaturity)
			if tx.IsVote() {
				tx.Maturity++ // TODO why as elsewhere for votes?
			}
			if tx.Confirmations >= int64(exp.ChainParams.CoinbaseMaturity) {
				tx.Mature = "True"
			} else if tx.IsVote() {
				tx.VoteFundsLocked = "True"
			}
			coinbaseMaturityInHours :=
				exp.ChainParams.TargetTimePerBlock.Hours() * float64(tx.Maturity)
			tx.MaturityTimeTill = coinbaseMaturityInHours *
				(1 - float64(tx.Confirmations)/float64(tx.Maturity))
		}

		// For ticket purchase, get status and maturity blocks, but compute
		// details in normal code branch below.
		if tx.IsTicket() {
			tx.TicketInfo.TicketMaturity = int64(exp.ChainParams.TicketMaturity)
			if tx.Confirmations >= tx.TicketInfo.TicketMaturity {
				tx.Mature = "True"
			}
		}
	} // tx == nil (not found by dcrd)

	// Check for any transaction outputs that appear unspent.
	unspents := types.UnspentOutputIndices(tx.Vout)
	if len(unspents) > 0 {
		// Grab the mempool transaction inputs that match this transaction.
		mempoolVins := exp.GetTxMempoolInputs(hash, tx.Type)
		if len(mempoolVins) > 0 {
			// A quick matching function.
			matchingVin := func(vout *types.Vout) (string, uint32) {
				for vindex := range mempoolVins {
					vin := mempoolVins[vindex]
					for inIdx := range vin.Inputs {
						input := vin.Inputs[inIdx]
						if input.Outdex == vout.Index {
							return vin.TxId, input.Index
						}
					}
				}
				return "", 0
			}
			for _, outdex := range unspents {
				vout := &tx.Vout[outdex]
				txid, vindex := matchingVin(vout)
				if txid == "" {
					continue
				}
				vout.Spent = true
				tx.SpendingTxns[vout.Index] = types.TxInID{
					Hash:  txid,
					Index: vindex,
				}
			}
		}
	}

	// For any coinbase transactions look up the total block fees to include
	// as part of the inputs.
	if tx.Type == types.CoinbaseTypeStr {
		data := exp.dataSource.GetExplorerBlock(tx.BlockHash)
		if data == nil {
			log.Errorf("Unable to get block %s", tx.BlockHash)
		} else {
			// BlockInfo.MiningFee is coin (float64), while
			// TxInfo.BlockMiningFee is int64 (atoms), so convert. If the
			// float64 is somehow invalid, use the default zero value.
			feeAmt, _ := dcrutil.NewAmount(data.MiningFee)
			tx.BlockMiningFee = int64(feeAmt)
		}
	}

	// Details on all the blocks containing this transaction
	blocks, blockInds, err := exp.dataSource.TransactionBlocks(tx.TxID)
	if exp.timeoutErrorPage(w, err, "TransactionBlocks") {
		return
	}
	if err != nil {
		log.Errorf("Unable to retrieve blocks for transaction %s: %v",
			hash, err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, tx.TxID, ExpStatusError)
		return
	}

	// See if any of these blocks are mainchain and stakeholder-approved
	// (a.k.a. valid).
	var isConfirmedMainchain bool
	for ib := range blocks {
		if blocks[ib].IsValid && blocks[ib].IsMainchain {
			isConfirmedMainchain = true
			break
		}
	}

	// For each output of this transaction, look up any spending transactions,
	// and the index of the spending transaction input.
	spendingTxHashes, spendingTxVinInds, voutInds, err :=
		exp.dataSource.SpendingTransactions(hash)
	if exp.timeoutErrorPage(w, err, "SpendingTransactions") {
		return
	}
	if err != nil {
		log.Errorf("Unable to retrieve spending transactions for %s: %v", hash, err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, hash, ExpStatusError)
		return
	}
	for i, vout := range voutInds {
		if int(vout) >= len(tx.SpendingTxns) {
			log.Errorf("Invalid spending transaction data (%s:%d)", hash, vout)
			continue
		}
		tx.SpendingTxns[vout] = types.TxInID{
			Hash:  spendingTxHashes[i],
			Index: spendingTxVinInds[i],
		}
	}

	if tx.IsTicket() {
		blocksLive := tx.Confirmations - int64(exp.ChainParams.TicketMaturity)
		tx.TicketInfo.TicketPoolSize = int64(exp.ChainParams.TicketPoolSize) *
			int64(exp.ChainParams.TicketsPerBlock)
		tx.TicketInfo.TicketExpiry = int64(exp.ChainParams.TicketExpiry)
		expirationInDays := (exp.ChainParams.TargetTimePerBlock.Hours() *
			float64(exp.ChainParams.TicketExpiry)) / 24
		maturityInHours := (exp.ChainParams.TargetTimePerBlock.Hours() *
			float64(tx.TicketInfo.TicketMaturity))
		tx.TicketInfo.TimeTillMaturity = ((float64(exp.ChainParams.TicketMaturity) -
			float64(tx.Confirmations)) / float64(exp.ChainParams.TicketMaturity)) *
			maturityInHours
		ticketExpiryBlocksLeft := int64(exp.ChainParams.TicketExpiry) - blocksLive
		tx.TicketInfo.TicketExpiryDaysLeft = (float64(ticketExpiryBlocksLeft) /
			float64(exp.ChainParams.TicketExpiry)) * expirationInDays

		spendStatus, poolStatus, err := exp.dataSource.PoolStatusForTicket(hash)
		if exp.timeoutErrorPage(w, err, "PoolStatusForTicket") {
			return
		}
		if errors.Is(err, dbtypes.ErrNoResult) {
			if tx.Confirmations != 0 {
				log.Warnf("Spend and pool status not found for ticket %s: %v", hash, err)
			}
		} else if err != nil {
			log.Errorf("Unable to retrieve ticket spend and pool status for %s: %v",
				hash, err)
			exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
			return
		} else {
			if tx.Mature == "False" {
				tx.TicketInfo.PoolStatus = "immature"
			} else {
				tx.TicketInfo.PoolStatus = poolStatus.String()
			}
			tx.TicketInfo.SpendStatus = spendStatus.String()

			// For missed tickets, get the block in which it should have voted.
			if poolStatus == dbtypes.PoolStatusMissed {
				tx.TicketInfo.LotteryBlock, _, err = exp.dataSource.TicketMiss(hash)
				if errors.Is(err, dbtypes.ErrNoResult) {
					log.Warnf("No mainchain miss data for ticket %s: %v", hash, err)
				} else if err != nil {
					log.Errorf("Unable to retrieve miss information for ticket %s: %v",
						hash, err)
					exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
					return
				}
			}

			// Ticket luck and probability of voting.
			// blockLive < 0 for immature tickets
			blocksLive := tx.Confirmations - int64(exp.ChainParams.TicketMaturity)
			if tx.TicketInfo.SpendStatus == "Voted" {
				// Blocks from eligible until voted (actual luck)
				txhash, err := chainhash.NewHashFromStr(tx.SpendingTxns[0].Hash)
				if err != nil {
					exp.StatusPage(w, defaultErrorCode, err.Error(), "", ExpStatusError)
					return
				}
				tx.TicketInfo.TicketLiveBlocks = exp.dataSource.TxHeight(txhash) -
					tx.BlockHeight - int64(exp.ChainParams.TicketMaturity) - 1
			} else if tx.Confirmations >= int64(exp.ChainParams.TicketExpiry+
				uint32(exp.ChainParams.TicketMaturity)) { // Expired
				// Blocks ticket was active before expiring (actual no luck)
				tx.TicketInfo.TicketLiveBlocks = int64(exp.ChainParams.TicketExpiry)
			} else { // Active
				// Blocks ticket has been active and eligible to vote
				tx.TicketInfo.TicketLiveBlocks = blocksLive
			}
			tx.TicketInfo.BestLuck = tx.TicketInfo.TicketExpiry / int64(exp.ChainParams.TicketPoolSize)
			tx.TicketInfo.AvgLuck = tx.TicketInfo.BestLuck - 1
			if tx.TicketInfo.TicketLiveBlocks == int64(exp.ChainParams.TicketExpiry) {
				tx.TicketInfo.VoteLuck = 0
			} else {
				tx.TicketInfo.VoteLuck = float64(tx.TicketInfo.BestLuck) -
					(float64(tx.TicketInfo.TicketLiveBlocks) / float64(exp.ChainParams.TicketPoolSize))
			}
			if tx.TicketInfo.VoteLuck >= float64(tx.TicketInfo.BestLuck-
				(1/int64(exp.ChainParams.TicketPoolSize))) {
				tx.TicketInfo.LuckStatus = "Perfection"
			} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 0.25) {
				tx.TicketInfo.LuckStatus = "Very Lucky!"
			} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 0.75) {
				tx.TicketInfo.LuckStatus = "Good Luck"
			} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 1.25) {
				tx.TicketInfo.LuckStatus = "Normal"
			} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) * 0.50) {
				tx.TicketInfo.LuckStatus = "Bad Luck"
			} else if tx.TicketInfo.VoteLuck > 0 {
				tx.TicketInfo.LuckStatus = "Horrible Luck!"
			} else if tx.TicketInfo.VoteLuck == 0 {
				tx.TicketInfo.LuckStatus = "No Luck"
			}

			// Chance for a ticket to NOT be voted in a given time frame:
			// C = (1 - P)^N
			// Where: P is the probability of a vote in one block. (votes
			// per block / current ticket pool size)
			// N is the number of blocks before ticket expiry. (ticket
			// expiry in blocks - (number of blocks since ticket purchase -
			// ticket maturity))
			// C is the probability (chance)
			exp.pageData.RLock()
			pVote := float64(exp.ChainParams.TicketsPerBlock) /
				float64(exp.pageData.HomeInfo.PoolInfo.Size)
			exp.pageData.RUnlock()

			remainingBlocksLive := float64(exp.ChainParams.TicketExpiry) -
				float64(blocksLive)
			tx.TicketInfo.Probability = 100 * math.Pow(1-pVote, remainingBlocksLive)
		}
	} // tx.IsTicket()

	// Find atomic swaps related to this tx.
	swapsInfo, err := exp.txAtomicSwapsInfo(tx)
	if err != nil {
		log.Errorf("Unable to get atomic swap info for transaction %v: %v", tx.TxID, err)
	}
	if swapsInfo == nil {
		swapsInfo = new(txhelpers.TxSwapResults)
	}

	// Prepare the string to display for previous outpoint.
	for idx := range tx.Vin {
		vin := &tx.Vin[idx]
		if vin.Coinbase != "" {
			vin.DisplayText = types.CoinbaseTypeStr
		} else if vin.Stakebase != "" {
			vin.DisplayText = "Stakebase"
		} else if vin.Treasurybase {
			vin.DisplayText = "TreasuryBase"
		} else if vin.TreasurySpend != "" {
			vin.DisplayText = "TreasurySpend"
		} else {
			voutStr := strconv.Itoa(int(vin.Vout))
			vin.TextIsHash = true
			vin.Link = "/tx/" + vin.Txid + "/out/" + voutStr
			if swapsInfo.Redemptions[vin.Index] != nil || swapsInfo.Refunds[vin.Index] != nil {
				vin.DisplayText = "Swap"
			} else {
				vin.DisplayText = vin.Txid + ":" + voutStr
			}
		}
	}

	// Set Type for swap-related outputs.
	for idx := range tx.Vout {
		vout := &tx.Vout[idx]
		if swapsInfo.Contracts[vout.Index] != nil {
			vout.Type = "swap"
		}
	}

	// For an unconfirmed tx, get the time it was received in explorer's mempool.
	if tx.BlockHeight == 0 {
		tx.Time = exp.mempoolTime(tx.TxID)
	}

	pageData := struct {
		*CommonPageData
		Data                 *types.TxInfo
		Blocks               []*dbtypes.BlockStatus
		BlockInds            []uint32
		IsConfirmedMainchain bool
		HighlightInOut       string
		HighlightInOutID     int64
		SwapsFound           string
		Conversions          struct {
			Total *exchanges.Conversion
			Fees  *exchanges.Conversion
		}
	}{
		CommonPageData:       exp.commonData(r),
		Data:                 tx,
		Blocks:               blocks,
		BlockInds:            blockInds,
		IsConfirmedMainchain: isConfirmedMainchain,
		HighlightInOut:       inout,
		HighlightInOutID:     inoutid,
		SwapsFound:           swapsInfo.Found,
	}

	// Get a fiat-converted value for the total and the fees.
	if exp.xcBot != nil {
		pageData.Conversions.Total = exp.xcBot.Conversion(tx.Total)
		pageData.Conversions.Fees = exp.xcBot.Conversion(tx.Fee.ToCoin())
	}

	str, err := exp.templates.exec("tx", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (exp *explorerUI) txAtomicSwapsInfo(tx *types.TxInfo) (*txhelpers.TxSwapResults, error) {
	// Check if tx is a stake tree tx or coinbase tx and return empty swap info.
	if tx.Type != txhelpers.TxTypeRegular || tx.Coinbase {
		return new(txhelpers.TxSwapResults), nil
	}

	// Getting the msgTx for MsgTxAtomicSwapsInfo. Rethink TxInfo as an input!
	msgTx, err := exp.dataSource.GetTransactionByHash(tx.TxID)
	if err != nil {
		return nil, fmt.Errorf("GetRawTransaction failed for %s: %v", tx.TxID, err)
	}

	// Spending information for P2SH outputs are required to determine
	// contract outputs in this tx.
	outputSpenders := make(map[uint32]*txhelpers.OutputSpenderTxOut)
	for _, vout := range tx.Vout {
		txOut := msgTx.TxOut[vout.Index]
		if !vout.Spent || !stdscript.IsScriptHashScript(txOut.Version, txOut.PkScript) {
			// only retrieve spending tx for spent p2sh outputs
			continue
		}
		spender := tx.SpendingTxns[vout.Index]
		spendingMsgTx, err := exp.dataSource.GetTransactionByHash(spender.Hash)
		if err != nil {
			return nil, fmt.Errorf("GetRawTransaction failed for %s: %v", spender.Hash, err)
		}
		outputSpenders[vout.Index] = &txhelpers.OutputSpenderTxOut{
			Tx:  spendingMsgTx,
			Vin: spender.Index,
		}
	}

	return txhelpers.MsgTxAtomicSwapsInfo(msgTx, outputSpenders, exp.ChainParams)
}

type TreasuryInfo struct {
	Net string

	// Page parameters
	MaxTxLimit    int64
	Path          string
	Limit, Offset int64  // ?n=Limit&start=Offset
	TxnType       string // ?txntype=TxnType

	// TODO: tadd and tspend can be unconfirmed. tspend for a very long time.
	// NumUnconfirmed is the number of unconfirmed txns
	// NumUnconfirmed  int64
	// UnconfirmedTxns []*dbtypes.TreasuryTx

	// Transactions on the current page
	Transactions    []*dbtypes.TreasuryTx
	NumTransactions int64 // len(Transactions) but int64 for dumb template

	Balance          *dbtypes.TreasuryBalance
	ConvertedBalance *exchanges.Conversion
	TypeCount        int64
}

// TreasuryPage is the page handler for the "/treasury" path
func (exp *explorerUI) TreasuryPage(w http.ResponseWriter, r *http.Request) {
	ctx := context.WithValue(r.Context(), ctxAddress, exp.pageData.HomeInfo.DevAddress)
	r = r.WithContext(ctx)
	if queryVals := r.URL.Query(); queryVals.Get("txntype") == "" {
		queryVals.Set("txntype", "tspend")
		r.URL.RawQuery = queryVals.Encode()
	}

	limitN := defaultAddressRows
	if nParam := r.URL.Query().Get("n"); nParam != "" {
		val, err := strconv.ParseUint(nParam, 10, 64)
		if err != nil {
			exp.StatusPage(w, defaultErrorCode, "invalid n value", "", ExpStatusError)
			return
		}
		if int64(val) > MaxTreasuryRows {
			log.Warnf("TreasuryPage: requested up to %d address rows, "+
				"limiting to %d", limitN, MaxTreasuryRows)
			limitN = MaxTreasuryRows
		} else {
			limitN = int64(val)
		}
	}

	// Number of txns to skip (OFFSET in database query). For UX reasons, the
	// "start" URL query parameter is used.
	var offset int64
	if startParam := r.URL.Query().Get("start"); startParam != "" {
		val, err := strconv.ParseUint(startParam, 10, 64)
		if err != nil {
			exp.StatusPage(w, defaultErrorCode, "invalid start value", "", ExpStatusError)
			return
		}
		offset = int64(val)
	}

	// Transaction types to show.
	txTypeStr := r.URL.Query().Get("txntype")
	txType := parseTreasuryTransactionType(txTypeStr)

	txns, err := exp.dataSource.TreasuryTxns(limitN, offset, txType)
	if exp.timeoutErrorPage(w, err, "TreasuryTxns") {
		return
	} else if err != nil {
		exp.StatusPage(w, defaultErrorCode, err.Error(), "", ExpStatusError)
		return
	}

	exp.pageData.RLock()
	treasuryBalance := exp.pageData.HomeInfo.TreasuryBalance
	exp.pageData.RUnlock()

	typeCount := treasuryTypeCount(treasuryBalance, txType)

	treasuryData := &TreasuryInfo{
		Net:             exp.ChainParams.Net.String(),
		MaxTxLimit:      MaxTreasuryRows,
		Path:            r.URL.Path,
		Limit:           limitN,
		Offset:          offset,
		TxnType:         txTypeStr,
		NumTransactions: int64(len(txns)),
		Transactions:    txns,
		Balance:         treasuryBalance,
		TypeCount:       typeCount,
	}

	xcBot := exp.xcBot
	if xcBot != nil {
		treasuryData.ConvertedBalance = xcBot.Conversion(math.Round(float64(treasuryBalance.Balance) / 1e8))
	}

	// Execute the HTML template.
	linkTemplate := fmt.Sprintf("/treasury?start=%%d&n=%d&txntype=%v", limitN, txType)
	pageData := struct {
		*CommonPageData
		Data        *TreasuryInfo
		FiatBalance *exchanges.Conversion
		Pages       []pageNumber
	}{
		CommonPageData: exp.commonData(r),
		Data:           treasuryData,
		FiatBalance:    exp.xcBot.Conversion(dcrutil.Amount(treasuryBalance.Balance).ToCoin()),
		Pages:          calcPages(int(typeCount), int(limitN), int(offset), linkTemplate),
	}
	str, err := exp.templates.exec("treasury", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AddressPage is the page handler for the "/address" path.
func (exp *explorerUI) AddressPage(w http.ResponseWriter, r *http.Request) {
	// AddressPageData is the data structure passed to the HTML template
	type AddressPageData struct {
		*CommonPageData
		Data         *dbtypes.AddressInfo
		Type         txhelpers.AddressType
		CRLFDownload bool
		FiatBalance  *exchanges.Conversion
		Pages        []pageNumber
	}

	// Grab the URL query parameters
	address, txnType, limitN, offsetAddrOuts, err := parseAddressParams(r)
	if err != nil {
		exp.StatusPage(w, defaultErrorCode, err.Error(), address, ExpStatusError)
		return
	}

	// Validate the address.
	addr, addrType, addrErr := txhelpers.AddressValidation(address, exp.ChainParams)
	isZeroAddress := addrErr == txhelpers.AddressErrorZeroAddress
	if addrErr != nil && !isZeroAddress {
		var status expStatus
		var message string
		code := defaultErrorCode
		switch addrErr {
		case txhelpers.AddressErrorDecodeFailed, txhelpers.AddressErrorUnknown:
			status = ExpStatusBadRequest
			message = "Unexpected issue validating this address."
		case txhelpers.AddressErrorWrongNet:
			status = ExpStatusWrongNetwork
			message = fmt.Sprintf("The address %v is valid on %s, not %s.",
				addr, exp.ChainParams.Net.String(), exp.NetName)
			code = wrongNetwork
		default:
			status = ExpStatusError
			message = "Unknown error."
		}

		exp.StatusPage(w, code, message, address, status)
		return
	}

	// Handle valid but unsupported address types.
	switch addrType {
	case txhelpers.AddressTypeP2PKH, txhelpers.AddressTypeP2SH, txhelpers.AddressTypeP2PK:
		// All good. Used to go to StatusPage with ExpStatusP2PKAddress for
		// txhelpers.AddressTypeP2PK.
	default:
		message := "Unsupported address type."
		exp.StatusPage(w, defaultErrorCode, message, address, ExpStatusNotSupported)
		return
	}

	// Retrieve address information from the DB and/or RPC.
	var addrData *dbtypes.AddressInfo
	if isZeroAddress {
		// For the zero address (e.g. DsQxuVRvS4eaJ42dhQEsCXauMWjvopWgrVg),
		// short-circuit any queries.
		addrData = &dbtypes.AddressInfo{
			Address:         address,
			Type:            addrType,
			Net:             exp.ChainParams.Net.String(),
			IsDummyAddress:  true,
			Balance:         new(dbtypes.AddressBalance),
			UnconfirmedTxns: new(dbtypes.AddressTransactions),
		}
	} else {
		addrData, err = exp.AddressListData(address, txnType, limitN, offsetAddrOuts)
		if exp.timeoutErrorPage(w, err, "TicketsPriceByHeight") {
			return
		} else if err != nil {
			exp.StatusPage(w, defaultErrorCode, err.Error(), address, ExpStatusError)
			return
		}
	}

	// Set page parameters.
	addrData.Path = r.URL.Path

	// If exchange monitoring is active, prepare a fiat balance conversion
	conversion := exp.xcBot.Conversion(dcrutil.Amount(addrData.Balance.TotalUnspent).ToCoin())

	// For Windows clients only, link to downloads with CRLF (\r\n) line
	// endings.
	UseCRLF := strings.Contains(r.UserAgent(), "Windows")

	if limitN == 0 {
		limitN = 20
	}

	linkTemplate := fmt.Sprintf("/address/%s?start=%%d&n=%d&txntype=%v", addrData.Address, limitN, txnType)

	// Execute the HTML template.
	pageData := AddressPageData{
		CommonPageData: exp.commonData(r),
		Data:           addrData,
		CRLFDownload:   UseCRLF,
		FiatBalance:    conversion,
		Pages:          calcPages(int(addrData.TxnCount), int(limitN), int(offsetAddrOuts), linkTemplate),
	}
	str, err := exp.templates.exec("address", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	log.Tracef(`"address" template HTML size: %.2f kiB (%s, %v, %d)`,
		float64(len(str))/1024.0, address, txnType, addrData.NumTransactions)

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AddressTable is the page handler for the "/addresstable" path.
func (exp *explorerUI) AddressTable(w http.ResponseWriter, r *http.Request) {
	// Grab the URL query parameters
	address, txnType, limitN, offsetAddrOuts, err := parseAddressParams(r)
	if err != nil {
		log.Errorf("AddressTable request error: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	addrData, err := exp.AddressListData(address, txnType, limitN, offsetAddrOuts)
	if err != nil {
		log.Errorf("AddressListData error: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}

	linkTemplate := "/address/" + addrData.Address + "?start=%d&n=" + strconv.FormatInt(limitN, 10) + "&txntype=" + fmt.Sprintf("%v", txnType)

	response := struct {
		TxnCount int64        `json:"tx_count"`
		HTML     string       `json:"html"`
		Pages    []pageNumber `json:"pages"`
	}{
		TxnCount: addrData.TxnCount + addrData.NumUnconfirmed,
		Pages:    calcPages(int(addrData.TxnCount), int(limitN), int(offsetAddrOuts), linkTemplate),
	}

	response.HTML, err = exp.templates.exec("addresstable", struct {
		Data *dbtypes.AddressInfo
	}{
		Data: addrData,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}

	log.Tracef(`"addresstable" template HTML size: %.2f kiB (%s, %v, %d)`,
		float64(len(response.HTML))/1024.0, address, txnType, addrData.NumTransactions)

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	//enc.SetEscapeHTML(false)
	err = enc.Encode(response)
	if err != nil {
		log.Debug(err)
	}
}

// TreasuryTable is the handler for the "/treasurytable" path.
func (exp *explorerUI) TreasuryTable(w http.ResponseWriter, r *http.Request) {
	// Grab the URL query parameters
	txType, limitN, offset, err := parseTreasuryParams(r)
	if err != nil {
		log.Errorf("TreasuryTable request error: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	txns, err := exp.dataSource.TreasuryTxns(limitN, offset, txType)
	if exp.timeoutErrorPage(w, err, "TreasuryTxns") {
		return
	} else if err != nil {
		exp.StatusPage(w, defaultErrorCode, err.Error(), "", ExpStatusError)
		return
	}

	exp.pageData.RLock()
	bal := exp.pageData.HomeInfo.TreasuryBalance
	exp.pageData.RUnlock()

	linkTemplate := "/treasury" + "?start=%d&n=" + strconv.FormatInt(limitN, 10) + "&txntype=" + fmt.Sprintf("%v", txType)

	response := struct {
		TxnCount int64        `json:"tx_count"`
		HTML     string       `json:"html"`
		Pages    []pageNumber `json:"pages"`
	}{
		TxnCount: treasuryTypeCount(bal, txType),
		Pages:    calcPages(int(treasuryTypeCount(bal, txType)), int(limitN), int(offset), linkTemplate),
	}

	type txData struct {
		Transactions []*dbtypes.TreasuryTx
	}

	response.HTML, err = exp.templates.exec("treasurytable", struct {
		Data txData
	}{
		Data: txData{
			Transactions: txns,
		},
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}

	log.Tracef(`"treasurytable" template HTML size: %.2f kiB (%v, %d)`,
		float64(len(response.HTML))/1024.0, txType, len(txns))

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	//enc.SetEscapeHTML(false)
	err = enc.Encode(response)
	if err != nil {
		log.Debug(err)
	}
}

// parseAddressParams parses tx filter parameters. Used by both /address and
// /addresstable.
func parseAddressParams(r *http.Request) (address string, txnType dbtypes.AddrTxnViewType, limitN, offsetAddrOuts int64, err error) {
	// Get the address URL parameter, which should be set in the request context
	// by the addressPathCtx middleware.
	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		log.Trace("address not set")
		err = fmt.Errorf("there seems to not be an address in this request")
		return
	}

	tType, limitN, offsetAddrOuts, err := parsePaginationParams(r)
	txnType = dbtypes.AddrTxnViewTypeFromStr(tType)
	if txnType == dbtypes.AddrTxnUnknown {
		err = fmt.Errorf("unknown txntype query value")
	}
	return
}

// treasuryTypeCount returns the tx count for the type treasury tx type. The
// special value txType = -1 specifies all types combined.
func treasuryTypeCount(treasuryBalance *dbtypes.TreasuryBalance, txType stake.TxType) int64 {
	typedCount := treasuryBalance.TxCount
	switch txType {
	case stake.TxTypeTSpend:
		typedCount = treasuryBalance.SpendCount
	case stake.TxTypeTAdd:
		typedCount = treasuryBalance.AddCount
	case stake.TxTypeTreasuryBase:
		typedCount = treasuryBalance.TBaseCount
	}
	return typedCount
}

// parseTreasuryTransactionType parses a treasury transaction type from a
// string. If the provided string is not recognized as a treasury type, the
// special value -1, representing "all", will be returned.
func parseTreasuryTransactionType(txnTypeStr string) (txType stake.TxType) {
	switch strings.ToLower(txnTypeStr) {
	case "tspend":
		return stake.TxTypeTSpend
	case "tadd":
		return stake.TxTypeTAdd
	case "treasurybase":
		return stake.TxTypeTreasuryBase
	}
	return stake.TxType(-1)
}

// parseTreasuryParams parses the tx filters for the treasury page. Used by both
// TreasuryPage and TreasuryTable.
func parseTreasuryParams(r *http.Request) (txType stake.TxType, limitN, offsetAddrOuts int64, err error) {
	tType, limitN, offsetAddrOuts, err := parsePaginationParams(r)
	txType = parseTreasuryTransactionType(tType)
	return
}

// parsePaginationParams parses the pagination parameters from the query. The
// txnType string is returned as-is. The caller must decipher the string.
func parsePaginationParams(r *http.Request) (txnType string, limitN, offset int64, err error) {
	// Number of outputs for the address to query the database for. The URL
	// query parameter "n" is used to specify the limit (e.g. "?n=20").
	limitN = defaultAddressRows

	if nParam := r.URL.Query().Get("n"); nParam != "" {

		var val uint64
		val, err = strconv.ParseUint(nParam, 10, 64)
		if err != nil {
			err = fmt.Errorf("invalid n value")
			return
		}
		if int64(val) > MaxAddressRows {
			log.Warnf("addressPage: requested up to %d address rows, "+
				"limiting to %d", limitN, MaxAddressRows)
			limitN = MaxAddressRows
		} else {
			limitN = int64(val)
		}
	}

	// Number of outputs to skip (OFFSET in database query). For UX reasons, the
	// "start" URL query parameter is used.
	if startParam := r.URL.Query().Get("start"); startParam != "" {
		var val uint64
		val, err = strconv.ParseUint(startParam, 10, 64)
		if err != nil {
			err = fmt.Errorf("invalid start value")
			return
		}
		offset = int64(val)
	}

	// Transaction types to show.
	txnType = r.URL.Query().Get("txntype")
	if txnType == "" {
		txnType = "all"
	}

	return
}

// AddressListData grabs a size-limited and type-filtered set of inputs/outputs
// for a given address.
func (exp *explorerUI) AddressListData(address string, txnType dbtypes.AddrTxnViewType, limitN,
	offsetAddrOuts int64) (addrData *dbtypes.AddressInfo, err error) {

	// Get addresses table rows for the address.
	addrData, err = exp.dataSource.AddressData(address, limitN,
		offsetAddrOuts, txnType)
	if dbtypes.IsTimeoutErr(err) { //exp.timeoutErrorPage(w, err, "TicketsPriceByHeight") {
		return nil, err
	} else if err != nil {
		log.Errorf("AddressData error encountered: %v", err)
		err = fmt.Errorf(defaultErrorMessage)
		return nil, err
	}
	return
}

// DecodeTxPage handles the "decode/broadcast transaction" page. The actual
// decoding or broadcasting is handled by the websocket hub.
func (exp *explorerUI) DecodeTxPage(w http.ResponseWriter, r *http.Request) {
	str, err := exp.templates.exec("rawtx", struct {
		*CommonPageData
	}{
		CommonPageData: exp.commonData(r),
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Charts handles the charts displays showing the various charts plotted.
func (exp *explorerUI) Charts(w http.ResponseWriter, r *http.Request) {
	exp.pageData.RLock()
	tpSize := exp.pageData.HomeInfo.PoolInfo.Target
	exp.pageData.RUnlock()

	str, err := exp.templates.exec("charts", struct {
		*CommonPageData
		Premine        int64
		TargetPoolSize uint32
	}{
		CommonPageData: exp.commonData(r),
		Premine:        exp.premine,
		TargetPoolSize: tpSize,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Search implements a primitive search algorithm by checking if the value in
// question is a block index, block hash, address hash or transaction hash and
// redirects to the appropriate page or displays an error.
func (exp *explorerUI) Search(w http.ResponseWriter, r *http.Request) {
	// The ?search= query.
	searchStr := r.URL.Query().Get("search")

	// Strip leading and tailing whitespace.
	searchStr = strings.TrimSpace(searchStr)

	if searchStr == "" {
		exp.StatusPage(w, "search failed", "The search term was empty.",
			searchStr, ExpStatusBadRequest)
		return
	}

	// Attempt to get a block hash by calling GetBlockHash of WiredDB or
	// BlockHash of ChainDB to see if the URL query value is a block index. Then
	// redirect to the block page if it is.
	idx, err := strconv.ParseInt(searchStr, 10, 0)
	if err == nil {
		_, err = exp.dataSource.GetBlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
		_, err = exp.dataSource.BlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
		exp.StatusPage(w, "search failed", "Block "+searchStr+
			" has not yet been mined", searchStr, ExpStatusNotFound)
		return
	}

	_, err = stdaddr.DecodeAddress(searchStr, exp.ChainParams)
	if err == nil {
		http.Redirect(w, r, "/address/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Split searchStr to the first part corresponding to a transaction hash and
	// to the second part corresponding to a transaction output index.
	searchStrSplit := strings.Split(searchStr, ":")
	searchStrRewritten := searchStrSplit[0]
	var utxoLike bool
	switch {
	case len(searchStrSplit) > 2:
		exp.StatusPage(w, "search failed", "Transaction outpoint does not have a valid format: "+searchStr,
			"", ExpStatusNotFound)
		return
	case len(searchStrSplit) > 1:
		if _, err := strconv.ParseUint(searchStrSplit[1], 10, 32); err == nil {
			searchStrRewritten = searchStrRewritten + "/out/" + searchStrSplit[1]
			utxoLike = true
		} else {
			exp.StatusPage(w, "search failed", "Transaction output index is not a valid non-negative integer: "+searchStrSplit[1],
				"", ExpStatusNotFound)
			return
		}
	}

	tryProp := func() bool {
		// Try proposal token.
		proposalInfo, err := exp.proposals.ProposalByToken(searchStr)
		if err == nil {
			http.Redirect(w, r, "/proposal/"+proposalInfo.Token, http.StatusPermanentRedirect)
			return true
		}
		return false
	}

	// If it is not a valid hash, try proposals and give up.
	if _, err = chainhash.NewHashFromStr(searchStrSplit[0]); err != nil {
		if tryProp() {
			return
		}
		exp.StatusPage(w, "search failed",
			"Search string is not a valid block, tx hash, address, or proposal: "+searchStr,
			"", ExpStatusNotFound)
		return
	}

	// A valid hash could be block, txid, or prop. First try blocks, then tx via
	// getrawtransaction, then props, then tx via DB query.

	if !utxoLike {
		// Attempt to get a block index by calling GetBlockHeight to see if the
		// value is a block hash and then redirect to the block page if it is.
		_, err = exp.dataSource.GetBlockHeight(searchStrSplit[0])
		if err == nil {
			http.Redirect(w, r, "/block/"+searchStrSplit[0], http.StatusPermanentRedirect)
			return
		}
	}

	// It's unlikely to be a tx id with many leading/trailing zeros.
	trimmedZeros := 2*chainhash.HashSize - len(strings.Trim(searchStrSplit[0], "0"))

	// Call GetExplorerTx to see if the value is a transaction hash and then
	// redirect to the tx page if it is.
	if trimmedZeros < 10 {
		tx := exp.dataSource.GetExplorerTx(searchStrSplit[0])
		if tx != nil {
			http.Redirect(w, r, "/tx/"+searchStrRewritten, http.StatusPermanentRedirect)
			return
		}
	}

	// Before checking the DB for txns in orphaned blocks, try props.
	if tryProp() {
		return
	}

	// Also check the DB as it may have transactions from orphaned blocks.
	if trimmedZeros < 10 {
		dbTxs, err := exp.dataSource.Transaction(searchStrSplit[0])
		if err != nil && !errors.Is(err, dbtypes.ErrNoResult) {
			log.Errorf("Searching for transaction failed: %v", err)
		}
		if dbTxs != nil {
			http.Redirect(w, r, "/tx/"+searchStrRewritten, http.StatusPermanentRedirect)
			return
		}
	}

	message := "The search did not find any matching address, block, transaction or proposal token: " + searchStr
	exp.StatusPage(w, "search failed", message, "", ExpStatusNotFound)
}

// StatusPage provides a page for displaying status messages and exception
// handling without redirecting. Be sure to return after calling StatusPage if
// this completes the processing of the calling http handler.
func (exp *explorerUI) StatusPage(w http.ResponseWriter, code, message, additionalInfo string, sType expStatus) {
	commonPageData := exp.commonData(dummyRequest)
	if commonPageData == nil {
		// exp.blockData.GetTip likely failed due to empty DB.
		http.Error(w, "The database is initializing. Try again later.",
			http.StatusServiceUnavailable)
		return
	}
	str, err := exp.templates.exec("status", struct {
		*CommonPageData
		StatusType     expStatus
		Code           string
		Message        string
		AdditionalInfo string
	}{
		CommonPageData: commonPageData,
		StatusType:     sType,
		Code:           code,
		Message:        message,
		AdditionalInfo: additionalInfo,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		str = "Something went very wrong if you can see this, try refreshing"
	}

	w.Header().Set("Content-Type", "text/html")
	switch sType {
	case ExpStatusDBTimeout:
		w.WriteHeader(http.StatusServiceUnavailable)
	case ExpStatusNotFound:
		w.WriteHeader(http.StatusNotFound)
	case ExpStatusFutureBlock:
		w.WriteHeader(http.StatusOK)
	case ExpStatusError:
		w.WriteHeader(http.StatusInternalServerError)
	// When blockchain sync is running, status 202 is used to imply that the
	// other requests apart from serving the status sync page have been received
	// and accepted but cannot be processed now till the sync is complete.
	case ExpStatusSyncing:
		w.WriteHeader(http.StatusAccepted)
	case ExpStatusNotSupported:
		w.WriteHeader(http.StatusUnprocessableEntity)
	case ExpStatusBadRequest:
		w.WriteHeader(http.StatusBadRequest)
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	io.WriteString(w, str)
}

// NotFound wraps StatusPage to display a 404 page.
func (exp *explorerUI) NotFound(w http.ResponseWriter, r *http.Request) {
	exp.StatusPage(w, "Page not found.", "Cannot find page: "+r.URL.Path, "", ExpStatusNotFound)
}

// ParametersPage is the page handler for the "/parameters" path.
func (exp *explorerUI) ParametersPage(w http.ResponseWriter, r *http.Request) {
	params := exp.ChainParams
	addrPrefix := types.AddressPrefixes(params)
	actualTicketPoolSize := int64(params.TicketPoolSize * params.TicketsPerBlock)

	exp.pageData.RLock()
	var maxBlockSize int64
	if exp.pageData.BlockchainInfo != nil {
		maxBlockSize = exp.pageData.BlockchainInfo.MaxBlockSize
	} else {
		maxBlockSize = int64(params.MaximumBlockSizes[0])
	}
	exp.pageData.RUnlock()

	type ExtendedParams struct {
		MaximumBlockSize     int64
		ActualTicketPoolSize int64
		AddressPrefix        []types.AddrPrefix
	}

	str, err := exp.templates.exec("parameters", struct {
		*CommonPageData
		ExtendedParams
	}{
		CommonPageData: exp.commonData(r),
		ExtendedParams: ExtendedParams{
			MaximumBlockSize:     maxBlockSize,
			AddressPrefix:        addrPrefix,
			ActualTicketPoolSize: actualTicketPoolSize,
		},
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AgendaPage is the page handler for the "/agenda" path.
func (exp *explorerUI) AgendaPage(w http.ResponseWriter, r *http.Request) {
	errPageInvalidAgenda := func(err error) {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, "the agenda ID given seems to not exist",
			"", ExpStatusNotFound)
	}

	// Attempt to get agendaid string from URL path.
	agendaId := getAgendaIDCtx(r)
	agendaInfo, err := exp.agendasSource.AgendaInfo(agendaId)
	if err != nil {
		errPageInvalidAgenda(err)
		return
	}

	summary, err := exp.dataSource.AgendasVotesSummary(agendaId)
	if err != nil {
		log.Errorf("fetching Cumulative votes choices count failed: %v", err)
	}

	// Overrides the default count value with the actual vote choices count
	// matching data displayed on "Cumulative Vote Choices" and "Vote Choices By
	// Block" charts.
	var totalVotes uint32
	for index := range agendaInfo.Choices {
		switch strings.ToLower(agendaInfo.Choices[index].ID) {
		case "abstain":
			agendaInfo.Choices[index].Count = summary.Abstain
		case "yes":
			agendaInfo.Choices[index].Count = summary.Yes
		case "no":
			agendaInfo.Choices[index].Count = summary.No
		}
		totalVotes += agendaInfo.Choices[index].Count
	}

	ruleChangeQ := exp.ChainParams.RuleChangeActivationQuorum
	qVotes := uint32(float64(ruleChangeQ) * agendaInfo.QuorumProgress)

	var timeLeft string
	blocksLeft := summary.LockedIn - exp.Height()

	if blocksLeft > 0 {
		// Approximately 1 block per 5 minutes.
		var minPerblock = 5 * time.Minute

		hoursLeft := int((time.Duration(blocksLeft) * minPerblock).Hours())
		if hoursLeft > 0 {
			timeLeft = fmt.Sprintf("%v days %v hours", hoursLeft/24, hoursLeft%24)
		}
	} else {
		blocksLeft = 0
	}

	str, err := exp.templates.exec("agenda", struct {
		*CommonPageData
		Ai            *agendas.AgendaTagged
		QuorumVotes   uint32
		RuleChangeQ   uint32
		VotingStarted int64
		LockedIn      int64
		BlocksLeft    int64
		TimeRemaining string
		TotalVotes    uint32
	}{
		CommonPageData: exp.commonData(r),
		Ai:             agendaInfo,
		QuorumVotes:    qVotes,
		RuleChangeQ:    ruleChangeQ,
		VotingStarted:  summary.VotingStarted,
		LockedIn:       summary.LockedIn,
		BlocksLeft:     blocksLeft,
		TimeRemaining:  timeLeft,
		TotalVotes:     totalVotes,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AgendasPage is the page handler for the "/agendas" path.
func (exp *explorerUI) AgendasPage(w http.ResponseWriter, r *http.Request) {
	if exp.voteTracker == nil {
		log.Warnf("Agendas requested with nil voteTracker")
		exp.StatusPage(w, "", "agendas disabled on simnet", "", ExpStatusPageDisabled)
		return
	}

	agenda, err := exp.agendasSource.AllAgendas()
	if err != nil {
		log.Errorf("Error fetching agendas: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	str, err := exp.templates.exec("agendas", struct {
		*CommonPageData
		Agendas       []*agendas.AgendaTagged
		VotingSummary *agendas.VoteSummary
	}{
		CommonPageData: exp.commonData(r),
		Agendas:        agenda,
		VotingSummary:  exp.voteTracker.Summary(),
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// ProposalPage is the page handler for the "/proposal" path.
func (exp *explorerUI) ProposalPage(w http.ResponseWriter, r *http.Request) {
	if exp.proposals == nil {
		log.Errorf("Proposal DB instance is not available")
		exp.StatusPage(w, defaultErrorCode, "the proposals DB was not instantiated correctly",
			"", ExpStatusNotFound)
		return
	}

	token := getProposalTokenCtx(r)

	prop, err := exp.proposals.ProposalByToken(token)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, "the proposal token does not exist",
			"", ExpStatusNotFound)
		return
	}

	commonData := exp.commonData(r)
	str, err := exp.templates.exec("proposal", struct {
		*CommonPageData
		Data        *pitypes.ProposalRecord
		PoliteiaURL string
		ShortToken  string
		Metadata    *pitypes.ProposalMetadata
	}{
		CommonPageData: commonData,
		Data:           prop,
		PoliteiaURL:    exp.politeiaURL,
		ShortToken:     prop.Token[0:7],
		Metadata:       prop.Metadata(int64(commonData.Tip.Height), int64(exp.ChainParams.TargetTimePerBlock/time.Second)),
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// ProposalsPage is the page handler for the "/proposals" path.
func (exp *explorerUI) ProposalsPage(w http.ResponseWriter, r *http.Request) {
	if exp.proposals == nil {
		errMsg := "Proposal DB instance is not available"
		log.Errorf("proposals page error: %s", errMsg)
		exp.StatusPage(w, errMsg, fmt.Sprintf(pageDisabledCode, "/proposals"), "", ExpStatusPageDisabled)
		return
	}

	rowsCount := uint64(20)
	if rowsStr := r.URL.Query().Get("rows"); rowsStr != "" {
		val, err := strconv.ParseUint(rowsStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if val > 0 {
			rowsCount = val
		}
	}
	var offset uint64
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		val, err := strconv.ParseUint(offsetStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		offset = val
	}
	var filterBy uint64
	if filterByStr := r.URL.Query().Get("byvotestatus"); filterByStr != "" {
		val, err := strconv.ParseUint(filterByStr, 10, 64)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		filterBy = val
	}

	var err error
	var count int
	var proposals []*pitypes.ProposalRecord

	// Check if filter by votes status query parameter was passed.
	if filterBy > 0 {
		proposals, count, err = exp.proposals.ProposalsAll(int(offset),
			int(rowsCount), int(filterBy))
	} else {
		proposals, count, err = exp.proposals.ProposalsAll(int(offset),
			int(rowsCount))
	}

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	lastOffsetRows := uint64(count) % rowsCount
	var lastOffset uint64

	if lastOffsetRows == 0 && uint64(count) > rowsCount {
		lastOffset = uint64(count) - rowsCount
	} else if lastOffsetRows > 0 && uint64(count) > rowsCount {
		lastOffset = uint64(count) - lastOffsetRows
	}

	// Parse vote statuses map with only used status by the UI. Also
	// capitalizes first letter of the string status format.
	votesStatus := map[ticketvotev1.VoteStatusT]string{
		ticketvotev1.VoteStatusUnauthorized: "Unauthorized",
		ticketvotev1.VoteStatusAuthorized:   "Authorized",
		ticketvotev1.VoteStatusStarted:      "Started",
		ticketvotev1.VoteStatusFinished:     "Finished",
		ticketvotev1.VoteStatusApproved:     "Approved",
		ticketvotev1.VoteStatusRejected:     "Rejected",
		ticketvotev1.VoteStatusIneligible:   "Ineligible",
	}

	str, err := exp.templates.exec("proposals", struct {
		*CommonPageData
		Proposals     []*pitypes.ProposalRecord
		VotesStatus   map[ticketvotev1.VoteStatusT]string
		VStatusFilter int
		Offset        int64
		Limit         int64
		TotalCount    int64
		LastOffset    int64
		PoliteiaURL   string
		LastPropSync  int64
		TimePerBlock  int64
	}{
		CommonPageData: exp.commonData(r),
		Proposals:      proposals,
		VotesStatus:    votesStatus,
		Offset:         int64(offset),
		Limit:          int64(rowsCount),
		VStatusFilter:  int(filterBy),
		TotalCount:     int64(count),
		LastOffset:     int64(lastOffset),
		PoliteiaURL:    exp.politeiaURL,
		LastPropSync:   exp.proposals.ProposalsLastSync(),
		TimePerBlock:   int64(exp.ChainParams.TargetTimePerBlock.Seconds()),
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// HandleApiRequestsOnSync handles all API request when the sync status pages is
// running.
func (exp *explorerUI) HandleApiRequestsOnSync(w http.ResponseWriter, r *http.Request) {
	var complete int
	dataFetched := SyncStatus()

	syncStatus := "in progress"
	if len(dataFetched) == complete {
		syncStatus = "complete"
	}

	for _, v := range dataFetched {
		if v.PercentComplete == 100 {
			complete++
		}
	}
	stageRunning := complete + 1
	if stageRunning > len(dataFetched) {
		stageRunning = len(dataFetched)
	}

	data, err := json.Marshal(struct {
		Message string           `json:"message"`
		Stage   int              `json:"stage"`
		Stages  []SyncStatusInfo `json:"stages"`
	}{
		fmt.Sprintf("blockchain sync is %s.", syncStatus),
		stageRunning,
		dataFetched,
	})

	str := string(data)
	if err != nil {
		str = fmt.Sprintf("error occurred while processing the API response: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	io.WriteString(w, str)
}

// StatsPage is the page handler for the "/stats" path.
func (exp *explorerUI) StatsPage(w http.ResponseWriter, r *http.Request) {
	// Get current PoW difficulty.
	powDiff, err := exp.dataSource.CurrentDifficulty()
	if err != nil {
		log.Errorf("Failed to get Difficulty: %v", err)
	}

	// Subsidies
	dcp0010Height := exp.dataSource.DCP0010ActivationHeight()
	dcp0012Height := exp.dataSource.DCP0012ActivationHeight()
	ultSubsidy := txhelpers.UltimateSubsidy(exp.ChainParams, dcp0010Height, dcp0012Height)
	bestBlockHeight, err := exp.dataSource.GetHeight()
	if err != nil {
		log.Errorf("GetHeight failed: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "",
			ExpStatusError)
		return
	}
	blockSubsidy := exp.dataSource.BlockSubsidy(bestBlockHeight,
		exp.ChainParams.TicketsPerBlock)

	// Safely retrieve the inventory pointer, which can be reset in StoreMPData.
	inv := exp.MempoolInventory()

	// Prevent modifications to the shared inventory struct (e.g. in the
	// MempoolMonitor) while we retrieve the number of votes and tickets.
	inv.RLock()
	numVotes := inv.NumVotes
	numTickets := inv.NumTickets
	inv.RUnlock()

	exp.pageData.RLock()
	stats := types.StatsInfo{
		TotalSupply:    exp.pageData.HomeInfo.CoinSupply,
		UltimateSupply: ultSubsidy,
		TotalSupplyPercentage: float64(exp.pageData.HomeInfo.CoinSupply) /
			float64(ultSubsidy) * 100,
		ProjectFunds:             exp.pageData.HomeInfo.DevFund,
		ProjectAddress:           exp.pageData.HomeInfo.DevAddress,
		PoWDiff:                  exp.pageData.HomeInfo.Difficulty,
		BlockReward:              blockSubsidy.Total,
		NextBlockReward:          exp.pageData.HomeInfo.NBlockSubsidy.Total,
		PoWReward:                exp.pageData.HomeInfo.NBlockSubsidy.PoW,
		PoSReward:                exp.pageData.HomeInfo.NBlockSubsidy.PoS,
		ProjectFundReward:        exp.pageData.HomeInfo.NBlockSubsidy.Dev,
		VotesInMempool:           numVotes,
		TicketsInMempool:         numTickets,
		TicketPrice:              exp.pageData.HomeInfo.StakeDiff,
		NextEstimatedTicketPrice: exp.pageData.HomeInfo.NextExpectedStakeDiff,
		TicketPoolSize:           exp.pageData.HomeInfo.PoolInfo.Size,
		TicketPoolSizePerToTarget: float64(exp.pageData.HomeInfo.PoolInfo.Size) /
			float64(exp.ChainParams.TicketPoolSize*exp.ChainParams.TicketsPerBlock) * 100,
		TicketPoolValue:            exp.pageData.HomeInfo.PoolInfo.Value,
		TPVOfTotalSupplyPeecentage: exp.pageData.HomeInfo.PoolInfo.Percentage,
		TicketsROI:                 exp.pageData.HomeInfo.TicketReward,
		RewardPeriod:               exp.pageData.HomeInfo.RewardPeriod,
		ASR:                        exp.pageData.HomeInfo.ASR,
		APR:                        exp.pageData.HomeInfo.ASR,
		IdxBlockInWindow:           exp.pageData.HomeInfo.IdxBlockInWindow,
		WindowSize:                 exp.pageData.HomeInfo.Params.WindowSize,
		BlockTime:                  exp.pageData.HomeInfo.Params.BlockTime,
		IdxInRewardWindow:          exp.pageData.HomeInfo.IdxInRewardWindow,
		RewardWindowSize:           exp.pageData.HomeInfo.Params.RewardWindowSize,
		HashRate: powDiff * math.Pow(2, 32) /
			exp.ChainParams.TargetTimePerBlock.Seconds() / math.Pow(10, 15),
	}
	exp.pageData.RUnlock()

	str, err := exp.templates.exec("statistics", struct {
		*CommonPageData
		Stats types.StatsInfo
	}{
		CommonPageData: exp.commonData(r),
		Stats:          stats,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// MarketPage is the page handler for the "/market" path.
func (exp *explorerUI) MarketPage(w http.ResponseWriter, r *http.Request) {
	str, err := exp.templates.exec("market", struct {
		*CommonPageData
		DepthMarkets []string
		StickMarkets map[string]string
		XcState      *exchanges.ExchangeBotState
	}{
		CommonPageData: exp.commonData(r),
		XcState:        exp.getExchangeState(),
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// commonData grabs the common page data that is available to every page.
// This is particularly useful for extras.tmpl, parts of which
// are used on every page
func (exp *explorerUI) commonData(r *http.Request) *CommonPageData {
	tip, err := exp.dataSource.GetTip()
	if err != nil {
		log.Errorf("Failed to get the chain tip from the database.: %v", err)
		return nil
	}
	darkMode, err := r.Cookie(darkModeCoookie)
	if err != nil && err != http.ErrNoCookie {
		log.Errorf("Cookie dcrdataDarkBG retrieval error: %v", err)
	}

	scheme := r.URL.Scheme
	if scheme == "" {
		if r.TLS == nil {
			scheme = "http"
		} else {
			scheme = "https"
		}
	}
	baseURL := scheme + "://" + r.Host // assumes not opaque url

	return &CommonPageData{
		Tip:           tip,
		Version:       exp.Version,
		ChainParams:   exp.ChainParams,
		BlockTimeUnix: int64(exp.ChainParams.TargetTimePerBlock.Seconds()),
		DevAddress:    exp.pageData.HomeInfo.DevAddress,
		NetName:       exp.NetName,
		Links:         explorerLinks,
		Cookies: Cookies{
			DarkMode: darkMode != nil && darkMode.Value == "1",
		},
		Host:       r.Host,
		BaseURL:    baseURL,
		Path:       r.URL.Path,
		RequestURI: r.URL.RequestURI(),
	}
}

// A page number has the information necessary to create numbered pagination
// links.
type pageNumber struct {
	Active bool   `json:"active"`
	Link   string `json:"link"`
	Str    string `json:"str"`
}

func makePageNumber(active bool, link, str string) pageNumber {
	return pageNumber{
		Active: active,
		Link:   link,
		Str:    str,
	}
}

type pageNumbers []pageNumber

const ellipsisHTML = "…"

// Get a set of pagination numbers, based on a set number of rows that are
// assumed to start from page 1 at the highest row and descend from there.
// For example, if there are 20 pages of 10 rows, 0 - 199, page 1 would start at
// row 199 and go down to row 190. If the offset is between 190 and 199, the
// pagination would return the pageNumbers  necessary to create a pagination
// That looks like 1 2 3 4 5 6 7 8 ... 20. The pageNumber includes a link with
// the offset inserted using Sprintf.
func calcPagesDesc(rows, pageSize, offset int, link string) pageNumbers {
	nums := make(pageNumbers, 0, 11)
	endIdx := rows / pageSize
	if endIdx == 0 {
		return nums
	}
	pages := endIdx + 1
	currentPageIdx := (rows - offset) / pageSize
	if pages > 10 {
		nums = append(nums, makePageNumber(currentPageIdx == 0, fmt.Sprintf(link, rows), "1"))
		start := currentPageIdx - 3
		endMiddle := start + 6
		if start <= 1 {
			start = 1
			endMiddle = 7
		} else if endMiddle >= endIdx-1 {
			endMiddle = endIdx - 1
			start = endMiddle - 6
		}
		if start > 1 {
			nums = append(nums, makePageNumber(false, "", ellipsisHTML))
		}
		for i := start; i <= endMiddle; i++ {
			nums = append(nums, makePageNumber(i == currentPageIdx, fmt.Sprintf(link, rows-i*pageSize), strconv.Itoa(i+1)))
		}
		if endMiddle < endIdx-1 {
			nums = append(nums, makePageNumber(false, "", ellipsisHTML))
		}
		if pages > 1 {
			nums = append(nums, makePageNumber(currentPageIdx == endIdx, fmt.Sprintf(link, rows-endIdx*pageSize), strconv.Itoa(pages)))
		}
	} else {
		for i := 0; i < pages; i++ {
			nums = append(nums, makePageNumber(i == currentPageIdx, fmt.Sprintf(link, rows-i*pageSize), strconv.Itoa(i+1)))
		}
	}

	return nums
}

// Get a set of pagination numbers, based on a set number of rows that are
// assumed to start from page 1 at the lowest row and ascend from there.
// For example, if there are 20 pages of 10 rows, 0 - 199, page 1 would start at
// row 0 and go up to row 9. If the offset is between 0 and 9, the
// pagination would return the pageNumbers  necessary to create a pagination
// That looks like 1 2 3 4 5 6 7 8 ... 20. The pageNumber includes a link with
// the offset inserted using Sprintf.
func calcPages(rows, pageSize, offset int, link string) pageNumbers {
	if pageSize == 0 {
		return pageNumbers{}
	}
	nums := make(pageNumbers, 0, 11)
	endIdx := rows / pageSize
	if endIdx == 0 {
		return nums
	}
	pages := endIdx + 1
	currentPageIdx := offset / pageSize

	if pages > 10 {
		nums = append(nums, makePageNumber(currentPageIdx == 0, fmt.Sprintf(link, 0), "1"))
		start := currentPageIdx - 3
		endMiddle := start + 6
		if start <= 1 {
			start = 1
			endMiddle = 7
		} else if endMiddle >= endIdx-1 {
			endMiddle = endIdx - 1
			start = endMiddle - 6
		}
		if start > 1 {
			nums = append(nums, makePageNumber(false, "", ellipsisHTML))
		}

		for i := start; i <= endMiddle; i++ {
			nums = append(nums, makePageNumber(i == currentPageIdx, fmt.Sprintf(link, i*pageSize), strconv.Itoa(i+1)))
		}
		if endMiddle < endIdx-1 {
			nums = append(nums, makePageNumber(false, "", ellipsisHTML))
		}
		if pages > 1 {
			nums = append(nums, makePageNumber(currentPageIdx == endIdx, fmt.Sprintf(link, endIdx*pageSize), strconv.Itoa(pages)))
		}
	} else {
		for i := 0; i < pages; i++ {
			nums = append(nums, makePageNumber(i == currentPageIdx, fmt.Sprintf(link, i*pageSize), strconv.Itoa(i+1)))
		}
	}

	return nums
}

// AttackCost is the page handler for the "/attack-cost" path.
func (exp *explorerUI) AttackCost(w http.ResponseWriter, r *http.Request) {
	price := 24.42
	if exp.xcBot != nil {
		if rate := exp.xcBot.Conversion(1.0); rate != nil {
			price = rate.Value
		}
	}

	exp.pageData.RLock()

	height := exp.pageData.BlockInfo.Height
	ticketPoolValue := exp.pageData.HomeInfo.PoolInfo.Value
	ticketPoolSize := exp.pageData.HomeInfo.PoolInfo.Size
	ticketPrice := exp.pageData.HomeInfo.StakeDiff
	HashRate := exp.pageData.HomeInfo.HashRate
	coinSupply := exp.pageData.HomeInfo.CoinSupply

	exp.pageData.RUnlock()

	str, err := exp.templates.execTemplateToString("attackcost", struct {
		*CommonPageData
		HashRate        float64
		Height          int64
		DCRPrice        float64
		TicketPrice     float64
		TicketPoolSize  int64
		TicketPoolValue float64
		CoinSupply      int64
	}{
		CommonPageData:  exp.commonData(r),
		HashRate:        HashRate,
		Height:          height,
		DCRPrice:        price,
		TicketPrice:     ticketPrice,
		TicketPoolSize:  int64(ticketPoolSize),
		TicketPoolValue: ticketPoolValue,
		CoinSupply:      coinSupply,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// StakeRewardCalcPage is the page handler for the "/stakingcalc" path.
func (exp *explorerUI) StakeRewardCalcPage(w http.ResponseWriter, r *http.Request) {
	price := 24.42
	if exp.xcBot != nil {
		if rate := exp.xcBot.Conversion(1.0); rate != nil {
			price = rate.Value
		}
	}

	exp.pageData.RLock()

	ticketReward := exp.pageData.HomeInfo.TicketReward
	rewardPeriod := exp.pageData.HomeInfo.RewardPeriod

	exp.pageData.RUnlock()

	str, err := exp.templates.execTemplateToString("stakingreward", struct {
		*CommonPageData
		RewardPeriod string
		TicketReward float64
		DCRPrice     float64
	}{
		CommonPageData: exp.commonData(r),
		RewardPeriod:   rewardPeriod,
		TicketReward:   ticketReward,
		DCRPrice:       price,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

type verifyMessageResult struct {
	Address   string
	Signature string
	Message   string
	Valid     bool
	Error     string
}

// VerifyMessagePage is the page handler for "GET /verify-message" path.
func (exp *explorerUI) VerifyMessagePage(w http.ResponseWriter, r *http.Request) {
	str, err := exp.templates.exec("verify_message", struct {
		*CommonPageData
		VerifyMessageResult *verifyMessageResult
	}{
		CommonPageData: exp.commonData(r),
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// VerifyMessageHandler is the handler for "POST /verify-message" path.
func (exp *explorerUI) VerifyMessageHandler(w http.ResponseWriter, r *http.Request) {
	address := r.PostFormValue("address")
	signature := r.PostFormValue("signature")
	message := r.PostFormValue("message")

	displayPage := func(msg string, result bool) {
		str, err := exp.templates.exec("verify_message", struct {
			*CommonPageData
			VerifyMessageResult *verifyMessageResult
		}{
			CommonPageData: exp.commonData(r),
			VerifyMessageResult: &verifyMessageResult{
				Address:   address,
				Signature: signature,
				Message:   message,
				Valid:     result,
				Error:     msg,
			},
		})

		if err != nil {
			log.Errorf("Template execute failure: %v", err)
			exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, str)
	}

	if address == "" || signature == "" || message == "" {
		displayPage("Form values cannot be empty", false)
		return
	}

	if err := dcrutil.VerifyMessage(address, signature, message, exp.ChainParams); err != nil {
		if strings.Contains(err.Error(), "message not signed by address") {
			displayPage("", false)
		} else if strings.Contains(err.Error(), "malformed base64 encoding") {
			displayPage("invalid signature encoding", false)
		} else {
			displayPage(err.Error(), false)
		}
		return
	}
	displayPage("", true)
}
