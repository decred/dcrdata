// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrdata/v4/db/dbtypes"
	"github.com/decred/dcrdata/v4/exchanges"
	"github.com/decred/dcrdata/v4/explorer/types"
	"github.com/decred/dcrdata/v4/gov/agendas"
	pitypes "github.com/decred/dcrdata/v4/gov/politeia/types"
	"github.com/decred/dcrdata/v4/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

// CommonPageData is the basis for data structs used for HTML templates.
// explorerUI.commonData returns an initialized instance or CommonPageData,
// which itself should be used to initialize page data template structs.
type CommonPageData struct {
	Tip           *types.WebBasicBlock
	Version       string
	ChainParams   *chaincfg.Params
	BlockTimeUnix int64
	DevAddress    string
}

// Status page strings
const (
	defaultErrorCode    = "Something went wrong..."
	defaultErrorMessage = "Try refreshing... it usually fixes things."
	fullModeRequired    = "Full-functionality mode is required for this page."
	wrongNetwork        = "Wrong Network"
)

// expStatus defines the various status types supported by the system.
type expStatus string

const (
	ExpStatusError          expStatus = "Error"
	ExpStatusNotFound       expStatus = "Not Found"
	ExpStatusFutureBlock    expStatus = "Future Block"
	ExpStatusNotSupported   expStatus = "Not Supported"
	ExpStatusNotImplemented expStatus = "Not Implemented"
	ExpStatusWrongNetwork   expStatus = "Wrong Network"
	ExpStatusDeprecated     expStatus = "Deprecated"
	ExpStatusSyncing        expStatus = "Blocks Syncing"
	ExpStatusDBTimeout      expStatus = "Database Timeout"
	ExpStatusBitcoin        expStatus = "Bitcoin Address"
	ExpStatusP2PKAddress    expStatus = "P2PK Address Type"
)

// A fiat converted DCR value.
type fiatConversion struct {
	ConvertedValue float64
	BtcIndex       string
}

func (e expStatus) IsNotFound() bool {
	return e == ExpStatusNotFound
}

func (e expStatus) IsWrongNet() bool {
	return e == ExpStatusWrongNetwork
}

func (e expStatus) IsBitcoinAddress() bool {
	return e == ExpStatusBitcoin
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

// number of blocks displayed on /nexthome
const homePageBlocksMaxCount = 30

// netName returns the name used when referring to a decred network.
func netName(chainParams *chaincfg.Params) string {
	if chainParams == nil {
		return "invalid"
	}
	if strings.HasPrefix(strings.ToLower(chainParams.Name), "testnet") {
		return "Testnet"
	}
	return strings.Title(chainParams.Name)
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

// Home is the page handler for the "/" path.
func (exp *explorerUI) Home(w http.ResponseWriter, r *http.Request) {
	height, err := exp.blockData.GetHeight()
	if err != nil {
		log.Errorf("GetHeight failed: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "",
			ExpStatusError)
		return
	}

	blocks := exp.blockData.GetExplorerBlocks(int(height), int(height)-5)
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

	str, err := exp.templates.execTemplateToString("home", struct {
		*CommonPageData
		Info          *types.HomeInfo
		Mempool       *types.MempoolInfo
		BestBlock     *types.BlockBasic
		BlockTally    []int
		Consensus     int
		Blocks        []*types.BlockBasic
		NetName       string
		ExchangeState *exchanges.ExchangeBotState
	}{
		CommonPageData: exp.commonData(),
		Info:           exp.pageData.HomeInfo,
		Mempool:        inv,
		BestBlock:      bestBlock,
		BlockTally:     tallys,
		Consensus:      consensus,
		Blocks:         blocks,
		NetName:        exp.NetName,
		ExchangeState:  exp.getExchangeState(),
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
	sideBlocks, err := exp.explorerSource.SideChainBlocks()
	if exp.timeoutErrorPage(w, err, "SideChainBlocks") {
		return
	}
	if err != nil {
		log.Errorf("Unable to get side chain blocks: %v", err)
		exp.StatusPage(w, defaultErrorCode,
			"failed to retrieve side chain blocks", "", ExpStatusError)
		return
	}

	str, err := exp.templates.execTemplateToString("sidechains", struct {
		*CommonPageData
		Data    []*dbtypes.BlockStatus
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Data:           sideBlocks,
		NetName:        exp.NetName,
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
	disapprovedBlocks, err := exp.explorerSource.DisapprovedBlocks()
	if exp.timeoutErrorPage(w, err, "DisapprovedBlocks") {
		return
	}
	if err != nil {
		log.Errorf("Unable to get stakeholder disapproved blocks: %v", err)
		exp.StatusPage(w, defaultErrorCode,
			"failed to retrieve stakeholder disapproved blocks", "", ExpStatusError)
		return
	}

	str, err := exp.templates.execTemplateToString("disapproved", struct {
		*CommonPageData
		Data    []*dbtypes.BlockStatus
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Data:           disapprovedBlocks,
		NetName:        exp.NetName,
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

// NextHome is the page handler for the "/nexthome" path.
func (exp *explorerUI) NextHome(w http.ResponseWriter, r *http.Request) {
	// Get top N blocks and trim each block to have just the fields required for
	// this page.
	height, err := exp.blockData.GetHeight()
	if err != nil {
		log.Errorf("GetHeight failed: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "",
			ExpStatusError)
		return
	}
	blocks := exp.blockData.GetExplorerFullBlocks(int(height),
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

	str, err := exp.templates.execTemplateToString("nexthome", struct {
		*CommonPageData
		Info    *types.HomeInfo
		Mempool *types.TrimmedMempoolInfo
		Blocks  []*types.TrimmedBlockInfo
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Info:           exp.pageData.HomeInfo,
		Mempool:        mempoolInfo,
		Blocks:         trimmedBlocks,
		NetName:        exp.NetName,
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
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired,
			"Windows page cannot run in lite mode.", "", ExpStatusNotSupported)
		return
	}

	offsetWindow, err := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)
	if err != nil {
		offsetWindow = 0
	}

	bestWindow := uint64(exp.Height() / exp.ChainParams.StakeDiffWindowSize)
	if offsetWindow > bestWindow {
		offsetWindow = bestWindow
	}

	rows, err := strconv.ParseUint(r.URL.Query().Get("rows"), 10, 64)
	if err != nil || (rows < minExplorerRows && rows == 0) {
		rows = minExplorerRows
	}

	if rows > maxExplorerRows {
		rows = maxExplorerRows
	}

	windows, err := exp.explorerSource.PosIntervals(rows, offsetWindow)
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

	str, err := exp.templates.execTemplateToString("windows", struct {
		*CommonPageData
		Data         []*dbtypes.BlocksGroupedInfo
		WindowSize   int64
		BestWindow   int64
		OffsetWindow int64
		Limit        int64
		NetName      string
	}{
		CommonPageData: exp.commonData(),
		Data:           windows,
		WindowSize:     exp.ChainParams.StakeDiffWindowSize,
		BestWindow:     int64(bestWindow),
		OffsetWindow:   int64(offsetWindow),
		Limit:          int64(rows),
		NetName:        exp.NetName,
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
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired,
			"Time based blocks listing page cannot run in lite mode.", "",
			ExpStatusNotSupported)
		return
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

	offset, err := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)
	if err != nil {
		offset = 0
	}

	oldestBlockTime := exp.ChainParams.GenesisBlock.Header.Timestamp.Unix()
	maxOffset := (time.Now().Unix() - oldestBlockTime) / int64(i)
	m := uint64(maxOffset)
	if offset > m {
		offset = m
	}

	rows, err := strconv.ParseUint(r.URL.Query().Get("rows"), 10, 64)
	if err != nil || (rows < minExplorerRows && rows == 0) {
		rows = minExplorerRows
	}

	if rows > maxExplorerRows {
		rows = maxExplorerRows
	}

	data, err := exp.explorerSource.TimeBasedIntervals(grouping, rows, offset)
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

	str, err := exp.templates.execTemplateToString("timelisting", struct {
		*CommonPageData
		Data         []*dbtypes.BlocksGroupedInfo
		TimeGrouping string
		Offset       int64
		Limit        int64
		NetName      string
		BestGrouping int64
	}{
		CommonPageData: exp.commonData(),
		Data:           data,
		TimeGrouping:   val,
		Offset:         int64(offset),
		Limit:          int64(rows),
		NetName:        exp.NetName,
		BestGrouping:   maxOffset,
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
	bestBlockHeight, err := exp.blockData.GetHeight()
	if err != nil {
		log.Errorf("GetHeight failed: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "",
			ExpStatusError)
		return
	}

	height, err := strconv.Atoi(r.URL.Query().Get("height"))
	if err != nil || height > int(bestBlockHeight) {
		height = int(bestBlockHeight)
	}

	rows, err := strconv.Atoi(r.URL.Query().Get("rows"))
	if err != nil || (rows < minExplorerRows && rows == 0) {
		rows = minExplorerRows
	}

	if rows > maxExplorerRows {
		rows = maxExplorerRows
	}

	oldestBlock := height - rows + 1
	if oldestBlock < 0 {
		height = rows - 1
	}

	summaries := exp.blockData.GetExplorerBlocks(height, height-rows)
	if summaries == nil {
		log.Errorf("Unable to get blocks: height=%d&rows=%d", height, rows)
		exp.StatusPage(w, defaultErrorCode, "could not find those blocks", "",
			ExpStatusNotFound)
		return
	}

	if !exp.liteMode {
		for _, s := range summaries {
			blockStatus, err := exp.explorerSource.BlockStatus(s.Hash)
			if exp.timeoutErrorPage(w, err, "BlockStatus") {
				return
			}
			if err != nil && err != sql.ErrNoRows {
				log.Warnf("Unable to retrieve chain status for block %s: %v",
					s.Hash, err)
			}
			s.Valid = blockStatus.IsValid
			s.MainChain = blockStatus.IsMainchain
		}
	}

	str, err := exp.templates.execTemplateToString("explorer", struct {
		*CommonPageData
		Data       []*types.BlockBasic
		BestBlock  int64
		Rows       int64
		NetName    string
		WindowSize int64
	}{
		CommonPageData: exp.commonData(),
		Data:           summaries,
		BestBlock:      bestBlockHeight,
		Rows:           int64(rows),
		NetName:        exp.NetName,
		WindowSize:     exp.ChainParams.StakeDiffWindowSize,
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
	data := exp.blockData.GetExplorerBlock(hash)
	if data == nil {
		log.Errorf("Unable to get block %s", hash)
		exp.StatusPage(w, defaultErrorCode, "could not find that block", "",
			ExpStatusNotFound)
		return
	}

	// Check if there are any regular non-coinbase transactions in the block.
	var count int
	data.TxAvailable = true
	for _, i := range data.Tx {
		if i.Coinbase {
			count++
		}
	}
	if count == len(data.Tx) {
		data.TxAvailable = false
	}

	// In full mode, retrieve missed votes, main/side chain status, and
	// stakeholder approval.
	if !exp.liteMode {
		var err error
		data.Misses, err = exp.explorerSource.BlockMissedVotes(hash)
		if exp.timeoutErrorPage(w, err, "BlockMissedVotes") {
			return
		}
		if err != nil && err != sql.ErrNoRows {
			log.Warnf("Unable to retrieve missed votes for block %s: %v", hash, err)
		}

		var blockStatus dbtypes.BlockStatus
		blockStatus, err = exp.explorerSource.BlockStatus(hash)
		if exp.timeoutErrorPage(w, err, "BlockStatus") {
			return
		}
		if err != nil && err != sql.ErrNoRows {
			log.Warnf("Unable to retrieve chain status for block %s: %v", hash, err)
		}
		data.Valid = blockStatus.IsValid
		data.MainChain = blockStatus.IsMainchain
	}

	var conversion *fiatConversion
	if time.Since(data.BlockTime.T) < time.Hour {
		conversion = exp.getConversion(data.TotalSent)
	}

	pageData := struct {
		*CommonPageData
		Data           *types.BlockInfo
		NetName        string
		FiatConversion *fiatConversion
	}{
		CommonPageData: exp.commonData(),
		Data:           data,
		NetName:        exp.NetName,
		FiatConversion: conversion,
	}
	str, err := exp.templates.execTemplateToString("block", pageData)
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
	str, err := exp.templates.execTemplateToString("mempool", struct {
		*CommonPageData
		Mempool *types.MempoolInfo
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Mempool:        inv,
		NetName:        exp.NetName,
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
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired,
			"Ticketpool page cannot run in lite mode", "", ExpStatusNotSupported)
		return
	}

	str, err := exp.templates.execTemplateToString("ticketpool", struct {
		*CommonPageData
		NetName string
	}{
		CommonPageData: exp.commonData(),
		NetName:        exp.NetName,
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

	inout, _ := r.Context().Value(ctxTxInOut).(string)
	if inout != "in" && inout != "out" && inout != "" {
		exp.StatusPage(w, defaultErrorCode, "there was no transaction requested",
			"", ExpStatusNotFound)
		return
	}
	ioid, _ := r.Context().Value(ctxTxInOutId).(string)
	inoutid, _ := strconv.ParseInt(ioid, 10, 0)

	tx := exp.blockData.GetExplorerTx(hash)
	// If dcrd has no information about the transaction, pull the transaction
	// details from the full mode database.
	if tx == nil {
		if exp.liteMode {
			log.Errorf("Unable to get transaction %s", hash)
			exp.StatusPage(w, defaultErrorCode, "could not find that transaction",
				"", ExpStatusNotFound)
			return
		}
		// Search for occurrences of the transaction in the database.
		dbTxs, err := exp.explorerSource.Transaction(hash)
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
		tx = &types.TxInfo{
			TxBasic: &types.TxBasic{
				TxID:          hash,
				FormattedSize: humanize.Bytes(uint64(dbTx0.Size)),
				Total:         dcrutil.Amount(dbTx0.Sent).ToCoin(),
				Fee:           fees,
				FeeRate:       dcrutil.Amount((1000 * int64(fees)) / int64(dbTx0.Size)),
				// VoteInfo TODO - check votes table
				Coinbase: dbTx0.BlockIndex == 0,
			},
			SpendingTxns: make([]types.TxInID, len(dbTx0.VoutDbIds)), // SpendingTxns filled below
			Type:         txhelpers.TxTypeToString(int(dbTx0.TxType)),
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
			tx.Type = "Coinbase"
		}

		// Retrieve vouts from DB.
		vouts, err := exp.explorerSource.VoutsForTx(dbTx0)
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
			// Check pkScript for OP_RETURN
			var opReturn string
			asm, _ := txscript.DisasmString(vouts[iv].ScriptPubKey)
			if strings.Contains(asm, "OP_RETURN") {
				opReturn = asm
			}
			// Determine if the outpoint is spent
			spendingTx, _, _, err := exp.explorerSource.SpendingTransaction(hash, vouts[iv].TxIndex)
			if exp.timeoutErrorPage(w, err, "SpendingTransaction") {
				return
			}
			if err != nil && err != sql.ErrNoRows {
				log.Warnf("SpendingTransaction failed for outpoint %s:%d: %v",
					hash, vouts[iv].TxIndex, err)
			}
			amount := dcrutil.Amount(int64(vouts[iv].Value)).ToCoin()
			tx.Vout = append(tx.Vout, types.Vout{
				Addresses:       vouts[iv].ScriptPubKeyData.Addresses,
				Amount:          amount,
				FormattedAmount: humanize.Commaf(amount),
				Type:            txhelpers.TxTypeToString(int(vouts[iv].TxType)),
				Spent:           spendingTx != "",
				OP_RETURN:       opReturn,
				Index:           vouts[iv].TxIndex,
			})
		}

		// Retrieve vins from DB.
		vins, prevPkScripts, scriptVersions, err := exp.explorerSource.VinsForTx(dbTx0)
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
				log.Errorf("Failed to decode pkgScript: %v", err)
			}
			_, scrAddrs, _, err := txscript.ExtractPkScriptAddrs(scriptVersions[iv],
				pkScriptsStr, exp.ChainParams)
			if err != nil {
				log.Errorf("Failed to decode pkScript: %v", err)
			} else {
				for ia := range scrAddrs {
					addresses = append(addresses, scrAddrs[ia].EncodeAddress())
				}
			}

			// If the scriptsig does not decode or disassemble, oh well.
			asm, _ := txscript.DisasmString(vins[iv].ScriptHex)

			txIndex := vins[iv].TxIndex
			amount := dcrutil.Amount(vins[iv].ValueIn).ToCoin()
			var coinbase, stakebase string
			if txIndex == 0 {
				if tx.Coinbase {
					coinbase = hex.EncodeToString(txhelpers.CoinbaseScript)
				} else if tx.IsVote() {
					stakebase = hex.EncodeToString(txhelpers.CoinbaseScript)
				}
			}
			tx.Vin = append(tx.Vin, types.Vin{
				Vin: &dcrjson.Vin{
					Coinbase:    coinbase,
					Stakebase:   stakebase,
					Txid:        hash,
					Vout:        vins[iv].PrevTxIndex,
					Tree:        dbTx0.Tree,
					Sequence:    vins[iv].Sequence,
					AmountIn:    amount,
					BlockHeight: uint32(tx.BlockHeight),
					BlockIndex:  tx.BlockIndex,
					ScriptSig: &dcrjson.ScriptSig{
						Asm: asm,
						Hex: hex.EncodeToString(vins[iv].ScriptHex),
					},
				},
				Addresses:       addresses,
				FormattedAmount: humanize.Commaf(amount),
				Index:           txIndex,
			})
		}

		// For coinbase and stakebase, get maturity status.
		if tx.Coinbase || tx.IsVote() {
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

	// Set ticket-related parameters for both full and lite mode.
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
	}

	// In full mode, create list of blocks in which the transaction was mined,
	// and get additional ticket details and pool status.
	var blocks []*dbtypes.BlockStatus
	var blockInds []uint32
	var isConfirmedMainchain bool
	if exp.liteMode {
		blocks = append(blocks, &dbtypes.BlockStatus{
			Hash:        tx.BlockHash,
			Height:      uint32(tx.BlockHeight),
			IsMainchain: true,
			IsValid:     true,
		})
		blockInds = []uint32{tx.BlockIndex}
	} else {
		// For any coinbase transactions look up the total block fees to include
		// as part of the inputs.
		if tx.Type == "Coinbase" {
			data := exp.blockData.GetExplorerBlock(tx.BlockHash)
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
		var err error
		blocks, blockInds, err = exp.explorerSource.TransactionBlocks(tx.TxID)
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
		for ib := range blocks {
			if blocks[ib].IsValid && blocks[ib].IsMainchain {
				isConfirmedMainchain = true
				break
			}
		}

		// For each output of this transaction, look up any spending transactions,
		// and the index of the spending transaction input.
		spendingTxHashes, spendingTxVinInds, voutInds, err :=
			exp.explorerSource.SpendingTransactions(hash)
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
			spendStatus, poolStatus, err := exp.explorerSource.PoolStatusForTicket(hash)
			if exp.timeoutErrorPage(w, err, "PoolStatusForTicket") {
				return
			}
			if err != nil && err != sql.ErrNoRows {
				log.Errorf("Unable to retrieve ticket spend and pool status for %s: %v",
					hash, err)
				exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
				return
			} else if err == sql.ErrNoRows {
				log.Warnf("Spend and pool status not found for ticket %s: %v", hash, err)
			} else {
				if tx.Mature == "False" {
					tx.TicketInfo.PoolStatus = "immature"
				} else {
					tx.TicketInfo.PoolStatus = poolStatus.String()
				}
				tx.TicketInfo.SpendStatus = spendStatus.String()

				// For missed tickets, get the block in which it should have voted.
				if poolStatus == dbtypes.PoolStatusMissed {
					tx.TicketInfo.LotteryBlock, _, err = exp.explorerSource.TicketMiss(hash)
					if err != nil && err != sql.ErrNoRows {
						log.Errorf("Unable to retrieve miss information for ticket %s: %v",
							hash, err)
						exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
						return
					} else if err == sql.ErrNoRows {
						log.Warnf("No mainchain miss data for ticket %s: %v",
							hash, err)
					}
				}

				// Ticket luck and probability of voting.
				// blockLive < 0 for immature tickets
				blocksLive := tx.Confirmations - int64(exp.ChainParams.TicketMaturity)
				if tx.TicketInfo.SpendStatus == "Voted" {
					// Blocks from eligible until voted (actual luck)
					tx.TicketInfo.TicketLiveBlocks = exp.blockData.TxHeight(tx.SpendingTxns[0].Hash) -
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
	} // !exp.liteMode

	// Prepare the string to display for previous outpoint.
	for idx := range tx.Vin {
		vin := &tx.Vin[idx]
		if vin.Coinbase != "" {
			vin.DisplayText = "Coinbase"
		} else if vin.Stakebase != "" {
			vin.DisplayText = "Stakebase"
		} else {
			voutStr := strconv.Itoa(int(vin.Vout))
			vin.DisplayText = vin.Txid + ":" + voutStr
			vin.TextIsHash = true
			vin.Link = "/tx/" + vin.Txid + "/out/" + voutStr
		}
	}

	// Get a fiat-converted value for the total and the fees.
	convertedTotal := exp.getConversion(tx.Total)
	convertedFees := exp.getConversion(tx.Fee.ToCoin())

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
		NetName              string
		HighlightInOut       string
		HighlightInOutID     int64
		ConvertedTotal       *fiatConversion
		ConvertedFees        *fiatConversion
	}{
		CommonPageData:       exp.commonData(),
		Data:                 tx,
		Blocks:               blocks,
		BlockInds:            blockInds,
		IsConfirmedMainchain: isConfirmedMainchain,
		NetName:              exp.NetName,
		HighlightInOut:       inout,
		HighlightInOutID:     inoutid,
		ConvertedTotal:       convertedTotal,
		ConvertedFees:        convertedFees,
	}

	str, err := exp.templates.execTemplateToString("tx", pageData)
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
		NetName      string
		IsLiteMode   bool
		CRLFDownload bool
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
		case txhelpers.AddressErrorBitcoin:
			status = ExpStatusBitcoin
			message = "Looks like you are searching for a bitcoin address."
		case txhelpers.AddressErrorDecodeFailed, txhelpers.AddressErrorUnknown:
			status = ExpStatusError
			message = "Unexpected issue validating this address."
		case txhelpers.AddressErrorWrongNet:
			status = ExpStatusWrongNetwork
			message = fmt.Sprintf("The address %v is valid on %s, not %s.",
				addr, addr.Net().Name, exp.NetName)
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
	case txhelpers.AddressTypeP2PKH, txhelpers.AddressTypeP2SH:
		// All good.
	case txhelpers.AddressTypeP2PK:
		message := "Looks like you are searching for an address of type P2PK."
		exp.StatusPage(w, defaultErrorCode, message, address, ExpStatusP2PKAddress)
		return
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
			Net:             addr.Net().Name,
			IsDummyAddress:  true,
			Balance:         new(dbtypes.AddressBalance),
			UnconfirmedTxns: new(dbtypes.AddressTransactions),
			Fullmode:        true,
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
	addrData.IsDummyAddress = isZeroAddress // may be redundant
	addrData.Path = r.URL.Path

	// For Windows clients only, link to downloads with CRLF (\r\n) line
	// endings.
	UseCRLF := strings.Contains(r.UserAgent(), "Windows")

	// Execute the HTML template.
	pageData := AddressPageData{
		CommonPageData: exp.commonData(),
		Data:           addrData,
		IsLiteMode:     exp.liteMode,
		NetName:        exp.NetName,
		CRLFDownload:   UseCRLF,
	}
	str, err := exp.templates.execTemplateToString("address", pageData)
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

	response := struct {
		TxnCount int64  `json:"tx_count"`
		HTML     string `json:"html"`
	}{
		TxnCount: addrData.TxnCount + addrData.NumUnconfirmed,
	}

	response.HTML, err = exp.templates.execTemplateToString("addresstable", struct {
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

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		jsonBytes = []byte("JSON error")
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
}

// parseAddressParams is used by both /address and /addresstable.
func parseAddressParams(r *http.Request) (address string, txnType dbtypes.AddrTxnType, limitN, offsetAddrOuts int64, err error) {
	// Get the address URL parameter, which should be set in the request context
	// by the addressPathCtx middleware.
	var parseErr error

	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		log.Trace("address not set")
		err = fmt.Errorf("there seems to not be an address in this request")
		return
	}

	// Number of outputs for the address to query the database for. The URL
	// query parameter "n" is used to specify the limit (e.g. "?n=20").
	limitN, parseErr = strconv.ParseInt(r.URL.Query().Get("n"), 10, 64)
	if parseErr != nil || limitN < 0 {
		limitN = defaultAddressRows
	} else if limitN > MaxAddressRows {
		log.Warnf("addressPage: requested up to %d address rows, "+
			"limiting to %d", limitN, MaxAddressRows)
		limitN = MaxAddressRows
	}

	// Number of outputs to skip (OFFSET in database query). For UX reasons, the
	// "start" URL query parameter is used.
	offsetAddrOuts, parseErr = strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	if parseErr != nil || offsetAddrOuts < 0 {
		offsetAddrOuts = 0
	}

	// Transaction types to show.
	txntype := r.URL.Query().Get("txntype")
	if txntype == "" {
		txntype = "all"
	}
	txnType = dbtypes.AddrTxnTypeFromStr(txntype)
	if txnType == dbtypes.AddrTxnUnknown {
		err = fmt.Errorf("unknown txntype query value")
	}

	// log.Debugf("Showing transaction types: %s (%d)", txntype, txnType)

	return
}

// AddressListData grabs a size-limited and type-filtered set of inputs/outputs
// for a given address.
func (exp *explorerUI) AddressListData(address string, txnType dbtypes.AddrTxnType, limitN, offsetAddrOuts int64) (addrData *dbtypes.AddressInfo, err error) {
	if exp.liteMode {
		addrData, _, addrErr := exp.blockData.GetExplorerAddress(address,
			limitN, offsetAddrOuts)
		// The specific AddressError values from ValidateAddress were already
		// handled, but there may be other errors from GetExplorerAddress (e.g.
		// from searchrawtransactions).
		if addrErr != txhelpers.AddressErrorNoError {
			err = fmt.Errorf("Unknown error retrieving data for that address.")
			return nil, err
		}

		if addrData == nil {
			log.Errorf("Unable to get address %s", address)
			err = fmt.Errorf("could not find that address")
			return nil, err
		}
		addrData.TxnType = txnType.String()
	} else {
		// Get addresses table rows for the address.
		addrData, err = exp.explorerSource.AddressData(address, limitN,
			offsetAddrOuts, txnType)
		if dbtypes.IsTimeoutErr(err) { //exp.timeoutErrorPage(w, err, "TicketsPriceByHeight") {
			return nil, err
		} else if err != nil {
			log.Errorf("AddressData error encountered: %v", err)
			err = fmt.Errorf(defaultErrorMessage)
			return nil, err
		}
	}
	return
}

// DecodeTxPage handles the "decode/broadcast transaction" page. The actual
// decoding or broadcasting is handled by the websocket hub.
func (exp *explorerUI) DecodeTxPage(w http.ResponseWriter, r *http.Request) {
	str, err := exp.templates.execTemplateToString("rawtx", struct {
		*CommonPageData
		NetName string
	}{
		CommonPageData: exp.commonData(),
		NetName:        exp.NetName,
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
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired,
			"Charts page cannot run in lite mode", "", ExpStatusNotSupported)
		return
	}

	str, err := exp.templates.execTemplateToString("charts", struct {
		*CommonPageData
		NetName string
	}{
		CommonPageData: exp.commonData(),
		NetName:        exp.NetName,
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
	searchStr := r.URL.Query().Get("search")
	if searchStr == "" {
		exp.StatusPage(w, "search failed", "Nothing was searched for",
			searchStr, ExpStatusNotSupported)
		return
	}

	// Attempt to get a block hash by calling GetBlockHash of WiredDB or
	// BlockHash of ChainDB (if full mode) to see if the URL query value is a
	// block index. Then redirect to the block page if it is.
	idx, err := strconv.ParseInt(searchStr, 10, 0)
	if err == nil {
		_, err = exp.blockData.GetBlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
		if !exp.liteMode {
			_, err = exp.explorerSource.BlockHash(idx)
			if err == nil {
				http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
				return
			}
		}
		exp.StatusPage(w, "search failed", "Block "+searchStr+
			" has not yet been mined", searchStr, ExpStatusNotFound)
		return
	}

	// Call GetExplorerAddress to see if the value is an address hash and
	// then redirect to the address page if it is.
	address, _, addrErr := exp.blockData.GetExplorerAddress(searchStr, 1, 0)
	switch addrErr {
	case txhelpers.AddressErrorNoError, txhelpers.AddressErrorZeroAddress:
		http.Redirect(w, r, "/address/"+searchStr, http.StatusPermanentRedirect)
		return
	case txhelpers.AddressErrorWrongNet:
		// Status page will provide a link, but the address page can too.
		message := fmt.Sprintf("The address %v is valid on %s, not %s",
			searchStr, address.Net, exp.NetName)
		exp.StatusPage(w, wrongNetwork, message, searchStr, ExpStatusWrongNetwork)
		return
	case txhelpers.AddressErrorBitcoin:
		message := "Looks like you are searching for a bitcoin address."
		exp.StatusPage(w, wrongNetwork, message, searchStr, ExpStatusBitcoin)
		return
	}

	// Try aux DB if in full mode.
	if !exp.liteMode {
		// This is be unnecessarily duplicative and possible very
		// slow for a very active addresss.
		addrHist, _, _ := exp.explorerSource.AddressHistory(searchStr,
			1, 0, dbtypes.AddrTxnAll)
		if len(addrHist) > 0 {
			http.Redirect(w, r, "/address/"+searchStr, http.StatusPermanentRedirect)
			return
		}
	}

	// Remaining possibilities are hashes, so verify the string is a hash.
	if _, err = chainhash.NewHashFromStr(searchStr); err != nil {
		exp.StatusPage(w, "search failed",
			"Search string is not a valid hash or address: "+searchStr,
			"", ExpStatusNotFound)
		return
	}

	// Attempt to get a block index by calling GetBlockHeight to see if the
	// value is a block hash and then redirect to the block page if it is.
	_, err = exp.blockData.GetBlockHeight(searchStr)
	// If block search failed, and dcrdata is in full mode, check the aux DB,
	// which has data for side chain and orphaned blocks.
	if err != nil && !exp.liteMode {
		_, err = exp.explorerSource.BlockHeight(searchStr)
	}
	if err == nil {
		http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Call GetExplorerTx to see if the value is a transaction hash and then
	// redirect to the tx page if it is.
	tx := exp.blockData.GetExplorerTx(searchStr)
	if tx != nil {
		http.Redirect(w, r, "/tx/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Also check the aux DB as it may have transactions from orphaned blocks.
	if !exp.liteMode {
		dbTxs, err := exp.explorerSource.Transaction(searchStr)
		if err != nil && err != sql.ErrNoRows {
			log.Errorf("Searching for transaction failed: %v", err)
		}
		if dbTxs != nil {
			http.Redirect(w, r, "/tx/"+searchStr, http.StatusPermanentRedirect)
			return
		}
	}

	message := "The search did not find any matching address, block, or transaction: " + searchStr
	exp.StatusPage(w, "search failed", message, "", ExpStatusNotFound)
}

// StatusPage provides a page for displaying status messages and exception
// handling without redirecting. Be sure to return after calling StatusPage if
// this completes the processing of the calling http handler.
func (exp *explorerUI) StatusPage(w http.ResponseWriter, code, message, additionalInfo string, sType expStatus) {
	str, err := exp.templates.execTemplateToString("status", struct {
		*CommonPageData
		StatusType     expStatus
		Code           string
		Message        string
		AdditionalInfo string
		NetName        string
	}{
		CommonPageData: exp.commonData(),
		StatusType:     sType,
		Code:           code,
		Message:        message,
		AdditionalInfo: additionalInfo,
		NetName:        exp.NetName,
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
	case ExpStatusNotSupported, ExpStatusBitcoin:
		w.WriteHeader(http.StatusUnprocessableEntity)
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

	str, err := exp.templates.execTemplateToString("parameters", struct {
		*CommonPageData
		ExtendedParams
		NetName string
	}{
		CommonPageData: exp.commonData(),
		ExtendedParams: ExtendedParams{
			MaximumBlockSize:     maxBlockSize,
			AddressPrefix:        addrPrefix,
			ActualTicketPoolSize: actualTicketPoolSize,
		},
		NetName: exp.NetName,
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
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired, "Agenda page not available in lite mode.",
			"", ExpStatusNotSupported)
		return
	}
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

	yes, abstain, no, err := exp.explorerSource.AgendaCumulativeVoteChoices(agendaId)
	if err != nil {
		log.Errorf("fetching Cumulative votes choices count failed: %v", err)
	}

	// Overrides the default count value with the actual vote choices count
	// matching data displayed on "Cumulative Vote Choices" and "Vote Choices By
	// Block" charts.
	for index := range agendaInfo.Choices {
		switch strings.ToLower(agendaInfo.Choices[index].ID) {
		case "abstain":
			agendaInfo.Choices[index].Count = abstain
		case "yes":
			agendaInfo.Choices[index].Count = yes
		case "no":
			agendaInfo.Choices[index].Count = no
		}
	}

	str, err := exp.templates.execTemplateToString("agenda", struct {
		*CommonPageData
		Ai      *agendas.AgendaTagged
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Ai:             agendaInfo,
		NetName:        exp.NetName,
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
	agenda, err := exp.agendasSource.AllAgendas()
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	str, err := exp.templates.execTemplateToString("agendas", struct {
		*CommonPageData
		Agendas []*agendas.AgendaTagged
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Agendas:        agenda,
		NetName:        exp.NetName,
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
	// Attempts to retrieve a proposal token from URL path.
	proposalID, err := strconv.Atoi(getProposalTokenCtx(r))
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, "invalid proposal ID used ",
			"", ExpStatusNotFound)
		return
	}

	proposalInfo, err := exp.proposalsSource.ProposalByID(proposalID)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, "the proposal token does not exist",
			"", ExpStatusNotFound)
		return
	}

	str, err := exp.templates.execTemplateToString("proposal", struct {
		*CommonPageData
		Data        *pitypes.ProposalInfo
		NetName     string
		PoliteiaURL string
	}{
		CommonPageData: exp.commonData(),
		Data:           proposalInfo,
		NetName:        exp.NetName,
		PoliteiaURL:    exp.politeiaAPIURL,
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
	rowsCount, err := strconv.ParseUint(r.URL.Query().Get("rows"), 10, 64)
	if err != nil || rowsCount == 0 {
		// Number of rows displayed to the by default should be 10.
		rowsCount = 10
	}

	// Ignore the error if it ever happens.
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)

	var count int
	var proposals []*pitypes.ProposalInfo

	// Check if filter by votes status query parameter was passed. Ignore the
	// error message if it occurs.
	filterBy, _ := strconv.Atoi(r.URL.Query().Get("byvotestatus"))
	if filterBy > 0 {
		proposals, count, err = exp.proposalsSource.AllProposals(int(offset),
			int(rowsCount), filterBy)
	} else {
		proposals, count, err = exp.proposalsSource.AllProposals(int(offset),
			int(rowsCount))
	}

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	str, err := exp.templates.execTemplateToString("proposals", struct {
		*CommonPageData
		Proposals     []*pitypes.ProposalInfo
		VotesStatus   map[pitypes.VoteStatusType]string
		VStatusFilter int
		Offset        int64
		Limit         int64
		NetName       string
		TotalCount    int64
		PoliteiaURL   string
	}{
		CommonPageData: exp.commonData(),
		Proposals:      proposals,
		VotesStatus:    pitypes.VotesStatuses(),
		Offset:         int64(offset),
		Limit:          int64(rowsCount),
		VStatusFilter:  filterBy,
		TotalCount:     int64(count),
		NetName:        exp.NetName,
		PoliteiaURL:    exp.politeiaAPIURL,
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
	powDiff, err := exp.blockData.Difficulty()
	if err != nil {
		log.Errorf("Failed to get Difficulty: %v", err)
	}

	// Subsidies
	ultSubsidy := txhelpers.UltimateSubsidy(exp.ChainParams)
	bestBlockHeight, err := exp.blockData.GetHeight()
	if err != nil {
		log.Errorf("GetHeight failed: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "",
			ExpStatusError)
		return
	}
	blockSubsidy := exp.blockData.BlockSubsidy(bestBlockHeight,
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

	str, err := exp.templates.execTemplateToString("statistics", struct {
		*CommonPageData
		Stats   types.StatsInfo
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Stats:          stats,
		NetName:        exp.NetName,
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
func (exp *explorerUI) commonData() *CommonPageData {
	var cd CommonPageData
	cd.Version = exp.Version
	cd.ChainParams = exp.ChainParams
	cd.BlockTimeUnix = int64(exp.ChainParams.TargetTimePerBlock.Seconds())
	cd.DevAddress = exp.pageData.HomeInfo.DevAddress
	var err error
	cd.Tip, err = exp.blockData.GetTip()
	if err != nil {
		log.Errorf("Failed to get the chain tip from the database.: %v", err)
	}
	return &cd
}

// getConversion converts the float DCR value to the fiat value in the default
// index.
func (exp *explorerUI) getConversion(dcrVal float64) (conversion *fiatConversion) {
	xcState := exp.getExchangeState()
	if xcState != nil {
		conversion = new(fiatConversion)
		conversion.ConvertedValue = xcState.Price * dcrVal
		conversion.BtcIndex = xcState.BtcIndex
	}
	return
}
