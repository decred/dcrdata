// Copyright (c) 2018, The Decred developers
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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrdata/v3/db/agendadb"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

// Status page strings
const (
	defaultErrorCode    = "Something went wrong..."
	defaultErrorMessage = "Try refreshing... it usually fixes things."
	fullModeRequired    = "Full-functionality mode is required for this page."
	wrongNetwork        = "Wrong Network"
)

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
			"Database timeout. Please try again later.", ExpStatusDBTimeout)
	}
	return
}

// Home is the page handler for the "/" path.
func (exp *explorerUI) Home(w http.ResponseWriter, r *http.Request) {
	height := exp.blockData.GetHeight()

	blocks := exp.blockData.GetExplorerBlocks(height, height-5)

	// Lock for both MempoolData and pageData.HomeInfo
	exp.MempoolData.RLock()
	exp.pageData.RLock()

	str, err := exp.templates.execTemplateToString("home", struct {
		*CommonPageData
		Info    *HomeInfo
		Mempool *MempoolInfo
		Blocks  []*BlockBasic
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Info:           exp.pageData.HomeInfo,
		Mempool:        exp.MempoolData,
		Blocks:         blocks,
		NetName:        exp.NetName,
	})

	exp.MempoolData.RUnlock()
	exp.pageData.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
		exp.StatusPage(w, defaultErrorCode, "failed to retrieve side chain blocks", ExpStatusError)
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
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// DisapprovedBlocks is the page handler for the "/rejects" path.
func (exp *explorerUI) DisapprovedBlocks(w http.ResponseWriter, r *http.Request) {
	disapprovedBlocks, err := exp.explorerSource.DisapprovedBlocks()
	if exp.timeoutErrorPage(w, err, "DisapprovedBlocks") {
		return
	}
	if err != nil {
		log.Errorf("Unable to get stakeholder disapproved blocks: %v", err)
		exp.StatusPage(w, defaultErrorCode,
			"failed to retrieve stakeholder disapproved blocks", ExpStatusError)
		return
	}

	str, err := exp.templates.execTemplateToString("rejects", struct {
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
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// show only regular tx in block.Transactions, exclude coinbase (reward) transactions
// for use in NextHome handler and websocket response to getmempooltxs event
func filterRegularTx(txs []*TrimmedTxInfo) (transactions []*TrimmedTxInfo) {
	for _, tx := range txs {
		if !tx.Coinbase {
			transactions = append(transactions, tx)
		}
	}
	return transactions
}

func trimMempoolTx(txs []MempoolTx) (trimmedTxs []*TrimmedTxInfo) {
	for _, tx := range txs {
		txBasic := &TxBasic{
			Coinbase: tx.Coinbase,
			TxID:     tx.TxID,
			Total:    tx.TotalOut,
			VoteInfo: tx.VoteInfo,
		}

		var voteValid bool
		if tx.VoteInfo != nil {
			voteValid = tx.VoteInfo.Validation.Validity
		}

		trimmedTx := &TrimmedTxInfo{
			TxBasic:   txBasic,
			Fees:      tx.Fees,
			VoteValid: voteValid,
			VinCount:  tx.VinCount,
			VoutCount: tx.VoutCount,
		}

		trimmedTxs = append(trimmedTxs, trimmedTx)
	}

	return trimmedTxs
}

func filterUniqueLastBlockVotes(txs []*TrimmedTxInfo) (votes []*TrimmedTxInfo) {
	for _, tx := range txs {
		if tx.VoteInfo != nil && tx.VoteInfo.ForLastBlock {
			votes = append(votes, tx)
		}
	}
	return votes
}

// convert the *MempoolInfo in exp.MempoolData to *MempoolData
func (exp *explorerUI) TrimmedMempoolInfo() *TrimmedMempoolInfo {
	exp.MempoolData.RLock()

	mempoolRegularTxs := trimMempoolTx(exp.MempoolData.Transactions)
	mempoolVotes := trimMempoolTx(exp.MempoolData.Votes)

	data := &TrimmedMempoolInfo{
		Transactions: filterRegularTx(mempoolRegularTxs),
		Tickets:      trimMempoolTx(exp.MempoolData.Tickets),
		Votes:        filterUniqueLastBlockVotes(mempoolVotes),
		Revocations:  trimMempoolTx(exp.MempoolData.Revocations),
		Total:        exp.MempoolData.TotalOut,
		Time:         exp.MempoolData.LastBlockTime,
	}

	exp.MempoolData.RUnlock()

	// calculate total fees for mempool block
	getTotalFee := func(txs []*TrimmedTxInfo) (total float64) {
		for _, tx := range txs {
			total += tx.Fees
		}
		return
	}

	data.Fees = getTotalFee(data.Transactions) + getTotalFee(data.Revocations) + getTotalFee(data.Tickets) +
		getTotalFee(data.Votes)

	return data
}

// NextHome is the page handler for the "/nexthome" path.
func (exp *explorerUI) NextHome(w http.ResponseWriter, r *http.Request) {
	// Get top N blocks and trim each block to have just the fields required for this page.
	height := exp.blockData.GetHeight()
	blocks := exp.blockData.GetExplorerFullBlocks(height, height-homePageBlocksMaxCount)

	// trim unwanted data in each block
	trimmedBlocks := make([]*TrimmedBlockInfo, 0, len(blocks))
	for _, block := range blocks {
		trimmedBlock := &TrimmedBlockInfo{
			Time:         block.BlockTime,
			Height:       block.Height,
			Total:        block.TotalSent,
			Fees:         block.MiningFee,
			Subsidy:      block.Subsidy,
			Votes:        block.Votes,
			Tickets:      block.Tickets,
			Revocations:  block.Revs,
			Transactions: filterRegularTx(block.Tx),
		}

		trimmedBlocks = append(trimmedBlocks, trimmedBlock)
	}

	// construct mempool object with properties required in template
	mempoolInfo := exp.TrimmedMempoolInfo()
	// mempool fees appear incorrect, temporarily set to zero for now
	mempoolInfo.Fees = 0

	exp.pageData.RLock()
	mempoolInfo.Subsidy = exp.pageData.HomeInfo.NBlockSubsidy

	str, err := exp.templates.execTemplateToString("nexthome", struct {
		*CommonPageData
		Info    *HomeInfo
		Mempool *TrimmedMempoolInfo
		Blocks  []*TrimmedBlockInfo
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
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
			"Windows page cannot run in lite mode.", ExpStatusNotSupported)
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
		log.Errorf("The specified windows are invalid. offset=%d&rows=%d: error: %v ", offsetWindow, rows, err)
		exp.StatusPage(w, defaultErrorCode, "The specified windows could not found", ExpStatusNotFound)
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
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
			"Time based blocks listing page cannot run in lite mode.", ExpStatusNotSupported)
	}

	grouping := dbtypes.TimeGroupingFromStr(val)
	i, err := dbtypes.TimeBasedGroupingToInterval(grouping)
	if err != nil {
		// default to year grouping if grouping is missing
		i, err = dbtypes.TimeBasedGroupingToInterval(dbtypes.YearGrouping)
		if err != nil {
			exp.StatusPage(w, defaultErrorCode, "Invalid year grouping found.", ExpStatusError)
			log.Errorf("Invalid year grouping found: error: %v ", err)
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
		log.Errorf("The specified /%s intervals are invalid. offset=%d&rows=%d: error: %v ", val, offset, rows, err)
		exp.StatusPage(w, defaultErrorCode, "The specified intervals could not found", ExpStatusNotFound)
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
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Blocks is the page handler for the "/blocks" path.
func (exp *explorerUI) Blocks(w http.ResponseWriter, r *http.Request) {
	bestBlockHeight := exp.blockData.GetHeight()

	height, err := strconv.Atoi(r.URL.Query().Get("height"))
	if err != nil || height > bestBlockHeight {
		height = bestBlockHeight
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
		exp.StatusPage(w, defaultErrorCode, "could not find those blocks", ExpStatusNotFound)
		return
	}

	if !exp.liteMode {
		for _, s := range summaries {
			blockStatus, err := exp.explorerSource.BlockStatus(s.Hash)
			if exp.timeoutErrorPage(w, err, "BlockStatus") {
				return
			}
			if err != nil && err != sql.ErrNoRows {
				log.Warnf("Unable to retrieve chain status for block %s: %v", s.Hash, err)
			}
			s.Valid = blockStatus.IsValid
			s.MainChain = blockStatus.IsMainchain
		}
	}

	str, err := exp.templates.execTemplateToString("explorer", struct {
		*CommonPageData
		Data       []*BlockBasic
		BestBlock  int64
		Rows       int64
		NetName    string
		WindowSize int64
	}{
		CommonPageData: exp.commonData(),
		Data:           summaries,
		BestBlock:      int64(bestBlockHeight),
		Rows:           int64(rows),
		NetName:        exp.NetName,
		WindowSize:     exp.ChainParams.StakeDiffWindowSize,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
		exp.StatusPage(w, defaultErrorCode, "could not find that block", ExpStatusNotFound)
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

	pageData := struct {
		*CommonPageData
		Data    *BlockInfo
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Data:           data,
		NetName:        exp.NetName,
	}
	str, err := exp.templates.execTemplateToString("block", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Mempool is the page handler for the "/mempool" path.
func (exp *explorerUI) Mempool(w http.ResponseWriter, r *http.Request) {
	exp.MempoolData.RLock()
	str, err := exp.templates.execTemplateToString("mempool", struct {
		*CommonPageData
		Mempool *MempoolInfo
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Mempool:        exp.MempoolData,
		NetName:        exp.NetName,
	})
	exp.MempoolData.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
			"Ticketpool page cannot run in lite mode", ExpStatusNotSupported)
		return
	}
	interval := dbtypes.AllGrouping

	barGraphs, donutChart, height, err := exp.explorerSource.TicketPoolVisualization(interval)
	if exp.timeoutErrorPage(w, err, "TicketPoolVisualization") {
		return
	}
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}

	var mp dbtypes.PoolTicketsData
	exp.MempoolData.RLock()
	if len(exp.MempoolData.Tickets) > 0 {
		t := time.Unix(exp.MempoolData.Tickets[0].Time, 0)
		mp.Time = append(mp.Time, dbtypes.TimeDef{T: t})
		mp.Price = append(mp.Price, exp.MempoolData.Tickets[0].TotalOut)
		mp.Mempool = append(mp.Mempool, uint64(len(exp.MempoolData.Tickets)))
	} else {
		log.Debug("No tickets exist in the mempool")
	}
	exp.MempoolData.RUnlock()

	str, err := exp.templates.execTemplateToString("ticketpool", struct {
		*CommonPageData
		NetName      string
		ChartsHeight uint64
		ChartData    []*dbtypes.PoolTicketsData
		GroupedData  *dbtypes.PoolTicketsData
		Mempool      *dbtypes.PoolTicketsData
	}{
		CommonPageData: exp.commonData(),
		NetName:        exp.NetName,
		ChartsHeight:   height,
		ChartData:      barGraphs,
		GroupedData:    donutChart,
		Mempool:        &mp,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
		exp.StatusPage(w, defaultErrorCode, "there was no transaction requested", ExpStatusNotFound)
		return
	}

	inout, _ := r.Context().Value(ctxTxInOut).(string)
	if inout != "in" && inout != "out" && inout != "" {
		exp.StatusPage(w, defaultErrorCode, "there was no transaction requested", ExpStatusNotFound)
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
			exp.StatusPage(w, defaultErrorCode, "could not find that transaction", ExpStatusNotFound)
			return
		}
		// Search for occurrences of the transaction in the database.
		dbTxs, err := exp.explorerSource.Transaction(hash)
		if exp.timeoutErrorPage(w, err, "Transaction") {
			return
		}
		if err != nil {
			log.Errorf("Unable to retrieve transaction details for %s.", hash)
			exp.StatusPage(w, defaultErrorCode, "could not find that transaction", ExpStatusNotFound)
			return
		}
		if dbTxs == nil {
			exp.StatusPage(w, defaultErrorCode, "that transaction has not been recorded", ExpStatusNotFound)
			return
		}

		// Take the first one. The query order should put valid at the top of
		// the list. Regardless of order, the transaction web page will link to
		// all occurrences of the transaction.
		dbTx0 := dbTxs[0]
		fees := dcrutil.Amount(dbTx0.Fees)
		tx = &TxInfo{
			TxBasic: &TxBasic{
				TxID:          hash,
				FormattedSize: humanize.Bytes(uint64(dbTx0.Size)),
				Total:         dcrutil.Amount(dbTx0.Sent).ToCoin(),
				Fee:           fees,
				FeeRate:       dcrutil.Amount((1000 * int64(fees)) / int64(dbTx0.Size)),
				// VoteInfo TODO - check votes table
				Coinbase: dbTx0.BlockIndex == 0,
			},
			SpendingTxns: make([]TxInID, len(dbTx0.VoutDbIds)), // SpendingTxns filled below
			Type:         txhelpers.TxTypeToString(int(dbTx0.TxType)),
			// Vins - looked-up in vins table
			// Vouts - looked-up in vouts table
			BlockHeight:   dbTx0.BlockHeight,
			BlockIndex:    dbTx0.BlockIndex,
			BlockHash:     dbTx0.BlockHash,
			Confirmations: exp.Height() - dbTx0.BlockHeight + 1,
			Time:          dbTx0.Time,
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
			exp.StatusPage(w, defaultErrorCode, "VoutsForTx failed", ExpStatusError)
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
			tx.Vout = append(tx.Vout, Vout{
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
			exp.StatusPage(w, defaultErrorCode, "VinsForTx failed", ExpStatusError)
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
			tx.Vin = append(tx.Vin, Vin{
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
			coinbaseMaturityInHours := exp.ChainParams.TargetTimePerBlock.Hours() * float64(tx.Maturity)
			tx.MaturityTimeTill = coinbaseMaturityInHours * (1 - float64(tx.Confirmations)/float64(tx.Maturity))
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
	unspents := UnspentOutputIndices(tx.Vout)
	if len(unspents) > 0 {
		// Grab the mempool transaction inputs that match this transaction.
		mempoolVins := exp.GetTxMempoolInputs(hash, tx.Type)
		if len(mempoolVins) > 0 {
			// A quick matching function.
			matchingVin := func(vout *Vout) (string, uint32) {
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
				tx.SpendingTxns[vout.Index] = TxInID{
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
			float64(tx.Confirmations)) / float64(exp.ChainParams.TicketMaturity)) * maturityInHours
		ticketExpiryBlocksLeft := int64(exp.ChainParams.TicketExpiry) - blocksLive
		tx.TicketInfo.TicketExpiryDaysLeft = (float64(ticketExpiryBlocksLeft) /
			float64(exp.ChainParams.TicketExpiry)) * expirationInDays
	}

	// In full mode, create list of blocks in which the transaction was mined,
	// and get additional ticket details and pool status.
	var blocks []*dbtypes.BlockStatus
	var blockInds []uint32
	var hasValidMainchain bool
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
				tx.BlockMiningFee = int64(data.MiningFee)
			}
		}

		// Details on all the blocks containing this transaction
		var err error
		blocks, blockInds, err = exp.explorerSource.TransactionBlocks(tx.TxID)
		if exp.timeoutErrorPage(w, err, "TransactionBlocks") {
			return
		}
		if err != nil {
			log.Errorf("Unable to retrieve blocks for transaction %s: %v", hash, err)
			exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
			return
		}

		// See if any of these blocks are mainchain and stakeholder-approved
		// (a.k.a. valid).
		for ib := range blocks {
			if blocks[ib].IsValid && blocks[ib].IsMainchain {
				hasValidMainchain = true
				break
			}
		}

		// For each output of this transaction, look up any spending transactions,
		// and the index of the spending transaction input.
		spendingTxHashes, spendingTxVinInds, voutInds, err := exp.explorerSource.SpendingTransactions(hash)
		if exp.timeoutErrorPage(w, err, "SpendingTransactions") {
			return
		}
		if err != nil {
			log.Errorf("Unable to retrieve spending transactions for %s: %v", hash, err)
			exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
			return
		}
		for i, vout := range voutInds {
			if int(vout) >= len(tx.SpendingTxns) {
				log.Errorf("Invalid spending transaction data (%s:%d)", hash, vout)
				continue
			}
			tx.SpendingTxns[vout] = TxInID{
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
				log.Errorf("Unable to retrieve ticket spend and pool status for %s: %v", hash, err)
				exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
				return
			} else if err != sql.ErrNoRows {
				log.Warnf("Spend and pool status not found for ticket %s: %v", hash, err)
			} else {
				if tx.Mature == "False" {
					tx.TicketInfo.PoolStatus = "immature"
				} else {
					tx.TicketInfo.PoolStatus = poolStatus.String()
				}
				tx.TicketInfo.SpendStatus = spendStatus.String()

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
				if tx.TicketInfo.VoteLuck >= float64(tx.TicketInfo.BestLuck-(1/int64(exp.ChainParams.TicketPoolSize))) {
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
				pVote := float64(exp.ChainParams.TicketsPerBlock) / float64(exp.pageData.HomeInfo.PoolInfo.Size)
				exp.pageData.RUnlock()

				remainingBlocksLive := float64(exp.ChainParams.TicketExpiry) - float64(blocksLive)
				tx.TicketInfo.Probability = 100 * math.Pow(1-pVote, remainingBlocksLive)
			}
		} // tx.IsTicket()
	} // !exp.liteMode

	// Prepare the string to display for previous outpoint.
	for idx, _ := range tx.Vin {
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

	pageData := struct {
		*CommonPageData
		Data              *TxInfo
		Blocks            []*dbtypes.BlockStatus
		BlockInds         []uint32
		HasValidMainchain bool
		NetName           string
		HighlightInOut    string
		HighlightInOutID  int64
	}{
		CommonPageData:    exp.commonData(),
		Data:              tx,
		Blocks:            blocks,
		BlockInds:         blockInds,
		HasValidMainchain: hasValidMainchain,
		NetName:           exp.NetName,
		HighlightInOut:    inout,
		HighlightInOutID:  inoutid,
	}

	str, err := exp.templates.execTemplateToString("tx", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
		Data           *AddressInfo
		TxBlockHeights []int64
		NetName        string
		IsLiteMode     bool
		ChartData      *dbtypes.ChartsData
		Metrics        *dbtypes.AddressMetrics
	}

	// Get the address URL parameter, which should be set in the request context
	// by the addressPathCtx middleware.
	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		log.Trace("address not set")
		exp.StatusPage(w, defaultErrorCode, "there seems to not be an address in this request", ExpStatusNotFound)
		return
	}

	// Number of outputs for the address to query the database for. The URL
	// query parameter "n" is used to specify the limit (e.g. "?n=20").
	limitN, err := strconv.ParseInt(r.URL.Query().Get("n"), 10, 64)
	if err != nil || limitN < 0 {
		limitN = defaultAddressRows
	} else if limitN > MaxAddressRows {
		log.Warnf("addressPage: requested up to %d address rows, "+
			"limiting to %d", limitN, MaxAddressRows)
		limitN = MaxAddressRows
	}

	// Number of outputs to skip (OFFSET in database query). For UX reasons, the
	// "start" URL query parameter is used.
	offsetAddrOuts, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	if err != nil || offsetAddrOuts < 0 {
		offsetAddrOuts = 0
	}

	// Transaction types to show.
	txntype := r.URL.Query().Get("txntype")
	if txntype == "" {
		txntype = "all"
	}
	txnType := dbtypes.AddrTxnTypeFromStr(txntype)
	if txnType == dbtypes.AddrTxnUnknown {
		exp.StatusPage(w, defaultErrorCode, "unknown txntype query value", ExpStatusError)
		return
	}
	log.Debugf("Showing transaction types: %s (%d)", txntype, txnType)

	var addrMetrics *dbtypes.AddressMetrics

	// Retrieve address information from the DB and/or RPC
	var addrData *AddressInfo
	if exp.liteMode {
		addrData, err = exp.blockData.GetExplorerAddress(address, limitN, offsetAddrOuts)
		if err != nil && strings.HasPrefix(err.Error(), "wrong network") {
			exp.StatusPage(w, wrongNetwork, "That address is not valid for "+exp.NetName, ExpStatusNotSupported)
			return
		}
		// AddressInfo should never be nil if err is nil. Catch non-nil error
		// and nil addrData here since they are both unexpected errors.
		if err != nil || addrData == nil {
			log.Errorf("Unable to get data for address %s: %v", address, err)
			exp.StatusPage(w, defaultErrorCode,
				"Unexpected issue locating data for that address.", ExpStatusError)
			return
		}
	} else {
		// Get addresses table rows for the address.
		addrHist, balance, errH := exp.explorerSource.AddressHistory(
			address, limitN, offsetAddrOuts, txnType)
		if exp.timeoutErrorPage(w, errH, "AddressHistory") {
			return
		} else if errH == sql.ErrNoRows {
			// We do not have any confirmed transactions. Prep to display ONLY
			// unconfirmed transactions (or none at all).
			addrData = new(AddressInfo)
			addrData.Address = address
			addrData.Fullmode = true
			addrData.Balance = &AddressBalance{}
			log.Tracef("AddressHistory: No confirmed transactions for address %s.", address)
		} else if errH != nil {
			// Unexpected error
			log.Errorf("AddressHistory: %v", errH)
			exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
			return
		} else /*errH == nil*/ {
			// Generate AddressInfo skeleton from the address table rows.
			addrData = ReduceAddressHistory(addrHist)
			if addrData == nil {
				// Empty history is not expected for credit txnType with any txns.
				if txnType != dbtypes.AddrTxnDebit &&
					(balance.NumSpent+balance.NumUnspent) > 0 {
					log.Debugf("empty address history (%s): n=%d&start=%d",
						address, limitN, offsetAddrOuts)
					exp.StatusPage(w, defaultErrorCode,
						"that address has no history", ExpStatusNotFound)
					return
				}
				// No mined transactions
				addrData = new(AddressInfo)
				addrData.Address = address
			}
			addrData.Fullmode = true

			// Balances and txn counts (partial unless in full mode)
			addrData.Balance = balance
			addrData.KnownTransactions = (balance.NumSpent * 2) + balance.NumUnspent
			addrData.KnownFundingTxns = balance.NumSpent + balance.NumUnspent
			addrData.KnownSpendingTxns = balance.NumSpent
			addrData.KnownMergedSpendingTxns = balance.NumMergedSpent

			// Transactions to fetch with FillAddressTransactions. This should be a
			// noop if ReduceAddressHistory is working right.
			switch txnType {
			case dbtypes.AddrTxnAll, dbtypes.AddrMergedTxnDebit:
			case dbtypes.AddrTxnCredit:
				addrData.Transactions = addrData.TxnsFunding
			case dbtypes.AddrTxnDebit:
				addrData.Transactions = addrData.TxnsSpending
			default:
				log.Warnf("Unknown address transaction type: %v", txnType)
			}

			// Transactions on current page
			addrData.NumTransactions = int64(len(addrData.Transactions))
			if addrData.NumTransactions > limitN {
				addrData.NumTransactions = limitN
			}

			// Query database for transaction details.
			err = exp.explorerSource.FillAddressTransactions(addrData)
			if exp.timeoutErrorPage(w, err, "FillAddressTransactions") {
				return
			}
			if err != nil {
				log.Errorf("Unable to fill address %s transactions: %v", address, err)
				exp.StatusPage(w, defaultErrorCode,
					"transactions for that address not found", ExpStatusNotFound)
				return
			}
		}

		// If there are confirmed transactions, get address metrics: oldest
		// transaction's time, number of entries for the various time
		// groupings/intervals.
		if len(addrData.Transactions) > 0 {
			addrMetrics, err = exp.explorerSource.AddressMetrics(address)
			if exp.timeoutErrorPage(w, err, "AddressMetrics") {
				return
			}
			if err != nil {
				log.Errorf("Unable to fetch address metrics %s: %v", address, err)
				exp.StatusPage(w, defaultErrorCode, "address metrics not found",
					ExpStatusNotFound)
				return
			}
		} else {
			addrMetrics = &dbtypes.AddressMetrics{}
		}

		// Check for unconfirmed transactions.
		addressOuts, numUnconfirmed, err := exp.blockData.UnconfirmedTxnsForAddress(address)
		if err != nil || addressOuts == nil {
			log.Errorf("UnconfirmedTxnsForAddress failed for address %s: %v", address, err)
			exp.StatusPage(w, defaultErrorCode, "transactions for that address not found",
				ExpStatusNotFound)
			return
		}
		addrData.NumUnconfirmed = numUnconfirmed
		if addrData.UnconfirmedTxns == nil {
			addrData.UnconfirmedTxns = new(AddressTransactions)
		}

		// Funding transactions (unconfirmed)
		var received, sent, numReceived, numSent int64
	FUNDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.Outpoints {
			// Mempool transactions stick around for 2 blocks. The first block
			// incorporates the transaction and mines it. The second block
			// validates it by the stake. However, transactions move into our
			// database as soon as they are mined and thus we need to be careful
			// to not include those transactions in our list.
			for _, b := range addrData.Transactions {
				if f.Hash.String() == b.TxID && f.Index == b.InOutID {
					continue FUNDING_TX_DUPLICATE_CHECK
				}
			}
			fundingTx, ok := addressOuts.TxnsStore[f.Hash]
			if !ok {
				log.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if fundingTx.Confirmed() {
				log.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}
			if txnType == dbtypes.AddrTxnAll || txnType == dbtypes.AddrTxnCredit {
				addrTx := &AddressTx{
					TxID:          fundingTx.Hash().String(),
					TxType:        txhelpers.DetermineTxTypeString(fundingTx.Tx),
					InOutID:       f.Index,
					Time:          dbtypes.TimeDef{T: time.Unix(fundingTx.MemPoolTime, 0)},
					FormattedSize: humanize.Bytes(uint64(fundingTx.Tx.SerializeSize())),
					Total:         txhelpers.TotalOutFromMsgTx(fundingTx.Tx).ToCoin(),
					ReceivedTotal: dcrutil.Amount(fundingTx.Tx.TxOut[f.Index].Value).ToCoin(),
				}
				addrData.Transactions = append(addrData.Transactions, addrTx)
			}
			received += fundingTx.Tx.TxOut[f.Index].Value
			numReceived++

		}

		// Spending transactions (unconfirmed)
	SPENDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.PrevOuts {
			// Mempool transactions stick around for 2 blocks. The first block
			// incorporates the transaction and mines it. The second block
			// validates it by the stake. However, transactions move into our
			// database as soon as they are mined and thus we need to be careful
			// to not include those transactions in our list.
			for _, b := range addrData.Transactions {
				if f.TxSpending.String() == b.TxID && f.InputIndex == int(b.InOutID) {
					continue SPENDING_TX_DUPLICATE_CHECK
				}
			}
			spendingTx, ok := addressOuts.TxnsStore[f.TxSpending]
			if !ok {
				log.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if spendingTx.Confirmed() {
				log.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}

			// The total send amount must be looked up from the previous
			// outpoint because vin:i valuein is not reliable from dcrd.
			prevhash := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Hash
			strprevhash := prevhash.String()
			previndex := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Index
			valuein := addressOuts.TxnsStore[prevhash].Tx.TxOut[previndex].Value

			// Look through old transactions and set the spending transactions'
			// matching transaction fields.
			for _, dbTxn := range addrData.Transactions {
				if dbTxn.TxID == strprevhash && dbTxn.InOutID == previndex && dbTxn.IsFunding {
					dbTxn.MatchedTx = spendingTx.Hash().String()
					dbTxn.MatchedTxIndex = uint32(f.InputIndex)
				}
			}

			if txnType == dbtypes.AddrTxnAll || txnType == dbtypes.AddrTxnDebit {
				addrTx := &AddressTx{
					TxID:           spendingTx.Hash().String(),
					TxType:         txhelpers.DetermineTxTypeString(spendingTx.Tx),
					InOutID:        uint32(f.InputIndex),
					Time:           dbtypes.TimeDef{T: time.Unix(spendingTx.MemPoolTime, 0)},
					FormattedSize:  humanize.Bytes(uint64(spendingTx.Tx.SerializeSize())),
					Total:          txhelpers.TotalOutFromMsgTx(spendingTx.Tx).ToCoin(),
					SentTotal:      dcrutil.Amount(valuein).ToCoin(),
					MatchedTx:      strprevhash,
					MatchedTxIndex: previndex,
				}
				addrData.Transactions = append(addrData.Transactions, addrTx)
			}

			sent += valuein
			numSent++
		} // range addressOuts.PrevOuts

		// Totals from funding and spending transactions.
		addrData.Balance.NumSpent += numSent
		addrData.Balance.NumUnspent += (numReceived - numSent)
		addrData.Balance.TotalSpent += sent
		addrData.Balance.TotalUnspent += (received - sent)
	}

	// Set page parameters.
	addrData.Path = r.URL.Path
	addrData.Limit, addrData.Offset = limitN, offsetAddrOuts
	addrData.TxnType = txnType.String()

	sort.Slice(addrData.Transactions, func(i, j int) bool {
		if addrData.Transactions[i].Time == addrData.Transactions[j].Time {
			return addrData.Transactions[i].InOutID > addrData.Transactions[j].InOutID
		}
		return addrData.Transactions[i].Time.T.Unix() > addrData.Transactions[j].Time.T.Unix()
	})

	// Compute block height for each transaction. This must be done *after*
	// sort.Slice of addrData.Transactions.
	txBlockHeights := make([]int64, len(addrData.Transactions))
	bdHeight := exp.Height()
	for i, v := range addrData.Transactions {
		txBlockHeights[i] = bdHeight - int64(v.Confirmations) + 1
	}

	// Execute the HTML template.
	pageData := AddressPageData{
		CommonPageData: exp.commonData(),
		Data:           addrData,
		TxBlockHeights: txBlockHeights,
		IsLiteMode:     exp.liteMode,
		NetName:        exp.NetName,
		Metrics:        addrMetrics,
	}
	str, err := exp.templates.execTemplateToString("address", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
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
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
			"Charts page cannot run in lite mode", ExpStatusNotSupported)
		return
	}
	tickets, err := exp.explorerSource.TicketsPriceByHeight()
	if exp.timeoutErrorPage(w, err, "TicketsPriceByHeight") {
		return
	}
	if err != nil {
		log.Errorf("Loading the Ticket Price By Height chart data failed %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}

	str, err := exp.templates.execTemplateToString("charts", struct {
		*CommonPageData
		NetName string
		Data    *dbtypes.ChartsData
	}{
		CommonPageData: exp.commonData(),
		NetName:        exp.NetName,
		Data:           tickets,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
		exp.StatusPage(w, "search failed", "Empty search string!", ExpStatusNotSupported)
		return
	}

	// Attempt to get a block hash by calling GetBlockHash of wiredDB or
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
		exp.StatusPage(w, "search failed", "Block "+searchStr+" has not yet been mined", ExpStatusNotFound)
		return
	}

	// Check to see if the value is an address, and redirect to the address page
	// if it is. Ignore the error as the passed data is expected to fail
	// validation or have other issues.
	address, _ := exp.blockData.GetExplorerAddress(searchStr, 1, 0)
	if address != nil {
		http.Redirect(w, r, "/address/"+searchStr, http.StatusPermanentRedirect)
		return
	}
	if !exp.liteMode {
		addrHist, _, _ := exp.explorerSource.AddressHistory(searchStr,
			1, 0, dbtypes.AddrTxnAll)
		if len(addrHist) > 0 {
			http.Redirect(w, r, "/address/"+searchStr, http.StatusPermanentRedirect)
			return
		}
	}

	// Remaining possibilities are hashes, so verify the string is a hash.
	if _, err = chainhash.NewHashFromStr(searchStr); err != nil {
		exp.StatusPage(w, "search failed", "Search string is not a valid hash or address: "+searchStr, ExpStatusNotFound)
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
	if !exp.liteMode {
		// Search for occurrences of the transaction in the database.
		dbTxs, err := exp.explorerSource.Transaction(searchStr)
		if err != nil && err != sql.ErrNoRows {
			log.Errorf("Searching for transaction failed: %v", err)
		}
		if dbTxs != nil {
			http.Redirect(w, r, "/tx/"+searchStr, http.StatusPermanentRedirect)
			return
		}
	}

	exp.StatusPage(w, "search failed", "The search string does not match any address, block, or transaction: "+searchStr, ExpStatusNotFound)
}

// StatusPage provides a page for displaying status messages and exception
// handling without redirecting.
func (exp *explorerUI) StatusPage(w http.ResponseWriter, code, message string, sType expStatus) {
	str, err := exp.templates.execTemplateToString("status", struct {
		*CommonPageData
		StatusType expStatus
		Code       string
		Message    string
		NetName    string
	}{
		CommonPageData: exp.commonData(),
		StatusType:     sType,
		Code:           code,
		Message:        message,
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
	case ExpStatusNotSupported:
		w.WriteHeader(http.StatusUnprocessableEntity)
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	io.WriteString(w, str)
}

// NotFound wraps StatusPage to display a 404 page.
func (exp *explorerUI) NotFound(w http.ResponseWriter, r *http.Request) {
	exp.StatusPage(w, "Page not found.", "Cannot find page: "+r.URL.Path, ExpStatusNotFound)
}

// ParametersPage is the page handler for the "/parameters" path.
func (exp *explorerUI) ParametersPage(w http.ResponseWriter, r *http.Request) {
	cp := exp.ChainParams
	addrPrefix := AddressPrefixes(cp)
	actualTicketPoolSize := int64(cp.TicketPoolSize * cp.TicketsPerBlock)
	ecp := ExtendedChainParams{
		MaximumBlockSize:     cp.MaximumBlockSizes[0],
		AddressPrefix:        addrPrefix,
		ActualTicketPoolSize: actualTicketPoolSize,
	}

	str, err := exp.templates.execTemplateToString("parameters", struct {
		*CommonPageData
		Cp      ExtendedChainParams
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Cp:             ecp,
		NetName:        exp.NetName,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AgendaPage is the page handler for the "/agenda" path.
func (exp *explorerUI) AgendaPage(w http.ResponseWriter, r *http.Request) {
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired,
			"Agenda page cannot run in lite mode.", ExpStatusNotSupported)
		return
	}
	errPageInvalidAgenda := func(err error) {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode,
			"the agenda ID given seems to not exist", ExpStatusNotFound)
	}

	// Attempt to get agendaid string from URL path.
	agendaid := getAgendaIDCtx(r)
	agendaInfo, err := GetAgendaInfo(agendaid)
	if err != nil {
		errPageInvalidAgenda(err)
		return
	}

	chartDataByTime, err := exp.explorerSource.AgendaVotes(agendaid, 0)
	if exp.timeoutErrorPage(w, err, "AgendaVotes") {
		return
	}
	if err != nil {
		errPageInvalidAgenda(err)
		return
	}

	chartDataByHeight, err := exp.explorerSource.AgendaVotes(agendaid, 1)
	if exp.timeoutErrorPage(w, err, "AgendaVotes") {
		return
	}
	if err != nil {
		errPageInvalidAgenda(err)
		return
	}

	str, err := exp.templates.execTemplateToString("agenda", struct {
		*CommonPageData
		Ai               *agendadb.AgendaTagged
		NetName          string
		ChartDataByTime  *dbtypes.AgendaVoteChoices
		ChartDataByBlock *dbtypes.AgendaVoteChoices
	}{
		CommonPageData:   exp.commonData(),
		Ai:               agendaInfo,
		NetName:          exp.NetName,
		ChartDataByTime:  chartDataByTime,
		ChartDataByBlock: chartDataByHeight,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AgendasPage is the page handler for the "/agendas" path.
func (exp *explorerUI) AgendasPage(w http.ResponseWriter, r *http.Request) {
	agendas, err := agendadb.GetAllAgendas()
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}

	str, err := exp.templates.execTemplateToString("agendas", struct {
		*CommonPageData
		Agendas []*agendadb.AgendaTagged
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Agendas:        agendas,
		NetName:        exp.NetName,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// HandleApiRequestsOnSync is a handler that handles all API request when the
// sync status pages is running.
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

// StatsPage is the page handler for the "/stats" path
func (exp *explorerUI) StatsPage(w http.ResponseWriter, r *http.Request) {
	// Get current PoW difficulty.
	powDiff, err := exp.blockData.Difficulty()
	if err != nil {
		log.Errorf("Failed to get Difficulty: %v", err)
	}

	// Subsidies
	ultSubsidy := txhelpers.UltimateSubsidy(exp.ChainParams)
	blockSubsidy := exp.blockData.BlockSubsidy(int64(exp.blockData.GetHeight()),
		exp.ChainParams.TicketsPerBlock)

	exp.MempoolData.RLock()
	exp.pageData.RLock()
	stats := StatsInfo{
		TotalSupply:              exp.pageData.HomeInfo.CoinSupply,
		UltimateSupply:           ultSubsidy,
		TotalSupplyPercentage:    float64(exp.pageData.HomeInfo.CoinSupply) / float64(ultSubsidy) * 100,
		ProjectFunds:             exp.pageData.HomeInfo.DevFund,
		ProjectAddress:           exp.pageData.HomeInfo.DevAddress,
		PoWDiff:                  exp.pageData.HomeInfo.Difficulty,
		BlockReward:              blockSubsidy.Total,
		NextBlockReward:          exp.pageData.HomeInfo.NBlockSubsidy.Total,
		PoWReward:                exp.pageData.HomeInfo.NBlockSubsidy.PoW,
		PoSReward:                exp.pageData.HomeInfo.NBlockSubsidy.PoS,
		ProjectFundReward:        exp.pageData.HomeInfo.NBlockSubsidy.Dev,
		VotesInMempool:           exp.MempoolData.NumVotes,
		TicketsInMempool:         exp.MempoolData.NumTickets,
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
		HashRate:                   powDiff * math.Pow(2, 32) / exp.ChainParams.TargetTimePerBlock.Seconds() / math.Pow(10, 15),
	}
	exp.MempoolData.RUnlock()
	exp.pageData.RUnlock()

	str, err := exp.templates.execTemplateToString("statistics", struct {
		*CommonPageData
		Stats   StatsInfo
		NetName string
	}{
		CommonPageData: exp.commonData(),
		Stats:          stats,
		NetName:        exp.NetName,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ExpStatusError)
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
