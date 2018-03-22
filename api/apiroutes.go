// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"

	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/rpcclient"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/explorer"
	m "github.com/decred/dcrdata/middleware"
	notify "github.com/decred/dcrdata/notification"
)

// APIDataSource implements an interface for collecting data for the api
type APIDataSource interface {
	GetHeight() int
	GetBestBlockHash() (string, error)
	GetBlockHash(idx int64) (string, error)
	GetBlockHeight(hash string) (int64, error)
	//Get(idx int) *blockdata.BlockData
	GetHeader(idx int) *dcrjson.GetBlockHeaderVerboseResult
	GetBlockVerbose(idx int, verboseTx bool) *dcrjson.GetBlockVerboseResult
	GetBlockVerboseByHash(hash string, verboseTx bool) *dcrjson.GetBlockVerboseResult
	GetRawTransaction(txid string) *apitypes.Tx
	GetTransactionHex(txid string) string
	GetTrimmedTransaction(txid string) *apitypes.TrimmedTx
	GetRawTransactionWithPrevOutAddresses(txid string) (*apitypes.Tx, [][]string)
	GetVoteInfo(txid string) (*apitypes.VoteInfo, error)
	GetVoteVersionInfo(ver uint32) (*dcrjson.GetVoteInfoResult, error)
	GetStakeVersions(txHash string, count int32) (*dcrjson.GetStakeVersionsResult, error)
	GetStakeVersionsLatest() (*dcrjson.StakeVersions, error)
	GetAllTxIn(txid string) []*apitypes.TxIn
	GetAllTxOut(txid string) []*apitypes.TxOut
	GetTransactionsForBlock(idx int64) *apitypes.BlockTransactions
	GetTransactionsForBlockByHash(hash string) *apitypes.BlockTransactions
	GetFeeInfo(idx int) *dcrjson.FeeInfoBlock
	//GetStakeDiffEstimate(idx int) *dcrjson.EstimateStakeDiffResult
	GetStakeInfoExtended(idx int) *apitypes.StakeInfoExtended
	//needs db update: GetStakeInfoExtendedByHash(hash string) *apitypes.StakeInfoExtended
	GetStakeDiffEstimates() *apitypes.StakeDiff
	//GetBestBlock() *blockdata.BlockData
	GetSummary(idx int) *apitypes.BlockDataBasic
	GetSummaryByHash(hash string) *apitypes.BlockDataBasic
	GetBestBlockSummary() *apitypes.BlockDataBasic
	GetBlockSize(idx int) (int32, error)
	GetBlockSizeRange(idx0, idx1 int) ([]int32, error)
	GetPoolInfo(idx int) *apitypes.TicketPoolInfo
	GetPoolInfoByHash(hash string) *apitypes.TicketPoolInfo
	GetPoolInfoRange(idx0, idx1 int) []apitypes.TicketPoolInfo
	GetPool(idx int64) ([]string, error)
	GetPoolByHash(hash string) ([]string, error)
	GetPoolValAndSizeRange(idx0, idx1 int) ([]float64, []float64)
	GetSDiff(idx int) float64
	GetSDiffRange(idx0, idx1 int) []float64
	GetMempoolSSTxSummary() *apitypes.MempoolTicketFeeInfo
	GetMempoolSSTxFeeRates(N int) *apitypes.MempoolTicketFees
	GetMempoolSSTxDetails(N int) *apitypes.MempoolTicketDetails
	GetAddressTransactions(addr string, count int) *apitypes.Address
	GetAddressTransactionsRaw(addr string, count int) []*apitypes.AddressTxRaw
	SendRawTransaction(txhex string) (string, error)
	GetExplorerAddress(address string, count, offset int64) *explorer.AddressInfo
}

type explorerDataSource interface {
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	AddressHistory(address string, N, offset int64) ([]*dbtypes.AddressRow, *explorer.AddressBalance, error)
	FillAddressTransactions(addrInfo *explorer.AddressInfo) error
}

// dcrdata application context used by all route handlers
type appContext struct {
	nodeClient     *rpcclient.Client
	BlockData      APIDataSource
	ExplorerSource explorerDataSource
	Status         apitypes.Status
	statusMtx      sync.RWMutex
	JSONIndent     string
}

// Constructor for appContext
func NewContext(client *rpcclient.Client, blockData APIDataSource, JSONIndent string) *appContext {
	conns, _ := client.GetConnectionCount()
	nodeHeight, _ := client.GetBlockCount()

	return &appContext{
		nodeClient: client,
		BlockData:  blockData,
		Status: apitypes.Status{
			Height:          uint32(nodeHeight),
			NodeConnections: conns,
			APIVersion:      APIVersion,
			DcrdataVersion:  ver.String(),
		},
		JSONIndent: JSONIndent,
	}
}

func (c *appContext) StatusNtfnHandler(wg *sync.WaitGroup, quit chan struct{}) {
	defer wg.Done()
out:
	for {
	keepon:
		select {
		case height, ok := <-notify.NtfnChans.UpdateStatusNodeHeight:
			if !ok {
				log.Warnf("Block connected channel closed.")
				break out
			}

			c.statusMtx.Lock()
			c.Status.Height = height

			var err error
			c.Status.NodeConnections, err = c.nodeClient.GetConnectionCount()
			if err != nil {
				c.Status.Ready = false
				c.statusMtx.Unlock()
				log.Warn("Failed to get connection count: ", err)
				break keepon
			}
			c.statusMtx.Unlock()

		case height, ok := <-notify.NtfnChans.UpdateStatusDBHeight:
			if !ok {
				log.Warnf("Block connected channel closed.")
				break out
			}

			if c.BlockData == nil {
				panic("BlockData APIDataSource is nil")
			}

			summary := c.BlockData.GetBestBlockSummary()
			if summary == nil {
				log.Errorf("BlockData summary is nil")
				break keepon
			}

			bdHeight := c.BlockData.GetHeight()
			c.statusMtx.Lock()
			if bdHeight >= 0 && summary.Height == uint32(bdHeight) &&
				height == uint32(bdHeight) {
				c.Status.DBHeight = height
				// if DB height agrees with node height, then we're ready
				if c.Status.Height == height {
					c.Status.Ready = true
				} else {
					c.Status.Ready = false
				}
				c.statusMtx.Unlock()
				break keepon
			}

			c.Status.Ready = false
			c.statusMtx.Unlock()
			log.Errorf("New DB height (%d) and stored block data (%d, %d) not consistent.",
				height, bdHeight, summary.Height)

		case _, ok := <-quit:
			if !ok {
				log.Debugf("Got quit signal. Exiting block connected handler for STATUS monitor.")
				break out
			}
		}
	}
}

// root is a http.Handler intended for the API root path. This essentially
// provides a heartbeat, and no information about the application status.
func (c *appContext) root(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "dcrdata api running")
}

func (c *appContext) writeJSONHandlerFunc(thing interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, thing, c.JSONIndent)
	}
}

func writeJSON(w http.ResponseWriter, thing interface{}, indent string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(thing); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getIndentQuery(r *http.Request) (indent string) {
	useIndentation := r.URL.Query().Get("indent")
	if useIndentation == "1" || useIndentation == "true" {
		indent = c.JSONIndent
	}
	return
}

func getVoteVersionQuery(r *http.Request) (int32, string, error) {
	verLatest := int64(m.GetLatestVoteVersionCtx(r))
	voteVersion := r.URL.Query().Get("version")
	if voteVersion == "" {
		return int32(verLatest), voteVersion, nil
	}

	ver, err := strconv.ParseInt(voteVersion, 10, 0)
	if err != nil {
		return -1, voteVersion, err
	}
	if ver > verLatest {
		ver = verLatest
	}

	return int32(ver), voteVersion, nil
}

func (c *appContext) status(w http.ResponseWriter, r *http.Request) {
	c.statusMtx.RLock()
	defer c.statusMtx.RUnlock()
	writeJSON(w, c.Status, c.getIndentQuery(r))
}

func (c *appContext) currentHeight(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.WriteString(w, strconv.Itoa(int(c.Status.Height))); err != nil {
		apiLog.Infof("failed to write height response: %v", err)
	}
}

func (c *appContext) getLatestBlock(w http.ResponseWriter, r *http.Request) {
	latestBlockSummary := c.BlockData.GetBestBlockSummary()
	if latestBlockSummary == nil {
		apiLog.Error("Unable to get latest block summary")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, latestBlockSummary, c.getIndentQuery(r))
}

func (c *appContext) getBlockHeight(w http.ResponseWriter, r *http.Request) {
	idx := c.getBlockHeightCtx(r)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.WriteString(w, strconv.Itoa(int(idx))); err != nil {
		apiLog.Infof("failed to write height response: %v", err)
	}
}

func (c *appContext) getBlockHash(w http.ResponseWriter, r *http.Request) {
	hash := c.getBlockHashCtx(r)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.WriteString(w, hash); err != nil {
		apiLog.Infof("failed to write height response: %v", err)
	}
}

func (c *appContext) getBlockSummary(w http.ResponseWriter, r *http.Request) {
	// attempt to get hash of block set by hash or (fallback) height set on path
	hash := c.getBlockHashCtx(r)
	if hash == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockSummary := c.BlockData.GetSummaryByHash(hash)
	if blockSummary == nil {
		apiLog.Errorf("Unable to get block %s summary", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockSummary, c.getIndentQuery(r))
}

func (c *appContext) getBlockTransactions(w http.ResponseWriter, r *http.Request) {
	hash := c.getBlockHashCtx(r)
	if hash == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockTransactions := c.BlockData.GetTransactionsForBlockByHash(hash)
	if blockTransactions == nil {
		apiLog.Errorf("Unable to get block %s transactions", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockTransactions, c.getIndentQuery(r))
}

func (c *appContext) getBlockTransactionsCount(w http.ResponseWriter, r *http.Request) {
	hash := c.getBlockHashCtx(r)
	if hash == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockTransactions := c.BlockData.GetTransactionsForBlockByHash(hash)
	if blockTransactions == nil {
		apiLog.Errorf("Unable to get block %s transactions", hash)
		return
	}
	writeJSON(w, &struct {
		Tx  int `json:"tx"`
		STx int `json:"stx"`
	}{len(blockTransactions.Tx), len(blockTransactions.STx)}, c.getIndentQuery(r))
}

func (c *appContext) getBlockHeader(w http.ResponseWriter, r *http.Request) {
	idx := c.getBlockHeightCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockHeader := c.BlockData.GetHeader(int(idx))
	if blockHeader == nil {
		apiLog.Errorf("Unable to get block %d header", idx)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockHeader, c.getIndentQuery(r))
}

func (c *appContext) getBlockVerbose(w http.ResponseWriter, r *http.Request) {
	hash := c.getBlockHashCtx(r)
	if hash == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockVerbose := c.BlockData.GetBlockVerboseByHash(hash, false)
	if blockVerbose == nil {
		apiLog.Errorf("Unable to get block %s", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockVerbose, c.getIndentQuery(r))
}

func (c *appContext) getVoteInfo(w http.ResponseWriter, r *http.Request) {
	ver, verStr, err := getVoteVersionQuery(r)
	if err != nil || ver < 0 {
		apiLog.Errorf("Unable to get vote info for stake version %s", verStr)
		http.Error(w, "Unable to get vote info for stake version "+verStr, 422)
		return
	}
	voteVersionInfo, err := c.BlockData.GetVoteVersionInfo(uint32(ver))
	if err != nil || voteVersionInfo == nil {
		apiLog.Errorf("Unable to get vote version %d info: %v", ver, err)
		http.Error(w, "Unable to get vote info for stake version "+verStr, 422)
		return
	}
	writeJSON(w, voteVersionInfo, c.getIndentQuery(r))
}

func (c *appContext) getTransaction(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	tx := c.BlockData.GetRawTransaction(txid)
	if tx == nil {
		apiLog.Errorf("Unable to get transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, tx, c.getIndentQuery(r))
}

func (c *appContext) getTransactionHex(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	hex := c.BlockData.GetTransactionHex(txid)

	fmt.Fprintf(w, hex)
}

func (c *appContext) getDecodedTx(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	tx := c.BlockData.GetTrimmedTransaction(txid)
	if tx == nil {
		apiLog.Errorf("Unable to get transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, tx, c.getIndentQuery(r))
}

func (c *appContext) getTxVoteInfo(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	vinfo, err := c.BlockData.GetVoteInfo(txid)
	if err != nil {
		apiLog.Errorf("Unable to get vote info for transaction %s", txid)
		http.Error(w, "Unable to get vote info. Is tx "+txid+" a vote?", 422)
		return
	}
	writeJSON(w, vinfo, c.getIndentQuery(r))
}

// getTransactionInputs serves []TxIn
func (c *appContext) getTransactionInputs(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	allTxIn := c.BlockData.GetAllTxIn(txid)
	// allTxIn may be empty, but not a nil slice
	if allTxIn == nil {
		apiLog.Errorf("Unable to get all TxIn for transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, allTxIn, c.getIndentQuery(r))
}

// getTransactionInput serves TxIn[i]
func (c *appContext) getTransactionInput(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	index := m.GetTxIOIndexCtx(r)
	if index < 0 {
		http.NotFound(w, r)
		//http.Error(w, http.StatusText(422), 422)
		return
	}

	allTxIn := c.BlockData.GetAllTxIn(txid)
	// allTxIn may be empty, but not a nil slice
	if allTxIn == nil {
		apiLog.Warnf("Unable to get all TxIn for transaction %s", txid)
		http.NotFound(w, r)
		return
	}

	if len(allTxIn) <= index {
		apiLog.Debugf("Index %d larger than []TxIn length %d", index, len(allTxIn))
		http.NotFound(w, r)
		return
	}

	writeJSON(w, *allTxIn[index], c.getIndentQuery(r))
}

// getTransactionOutputs serves []TxOut
func (c *appContext) getTransactionOutputs(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	allTxOut := c.BlockData.GetAllTxOut(txid)
	// allTxOut may be empty, but not a nil slice
	if allTxOut == nil {
		apiLog.Errorf("Unable to get all TxOut for transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, allTxOut, c.getIndentQuery(r))
}

// getTransactionOutput serves TxOut[i]
func (c *appContext) getTransactionOutput(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	index := m.GetTxIOIndexCtx(r)
	if index < 0 {
		http.NotFound(w, r)
		return
	}

	allTxOut := c.BlockData.GetAllTxOut(txid)
	// allTxOut may be empty, but not a nil slice
	if allTxOut == nil {
		apiLog.Errorf("Unable to get all TxOut for transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	if len(allTxOut) <= index {
		apiLog.Debugf("Index %d larger than []TxOut length %d", index, len(allTxOut))
		http.NotFound(w, r)
		return
	}

	writeJSON(w, *allTxOut[index], c.getIndentQuery(r))
}

func (c *appContext) getBlockFeeInfo(w http.ResponseWriter, r *http.Request) {
	idx := c.getBlockHeightCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockFeeInfo := c.BlockData.GetFeeInfo(int(idx))
	if blockFeeInfo == nil {
		apiLog.Errorf("Unable to get block %d fee info", idx)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockFeeInfo, c.getIndentQuery(r))
}

func (c *appContext) getBlockStakeInfoExtended(w http.ResponseWriter, r *http.Request) {
	idx := c.getBlockHeightCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	stakeinfo := c.BlockData.GetStakeInfoExtended(int(idx))
	if stakeinfo == nil {
		apiLog.Errorf("Unable to get block %d fee info", idx)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, stakeinfo, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiffSummary(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.BlockData.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, stakeDiff, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiffCurrent(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.BlockData.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	stakeDiffCurrent := dcrjson.GetStakeDifficultyResult{
		CurrentStakeDifficulty: stakeDiff.CurrentStakeDifficulty,
		NextStakeDifficulty:    stakeDiff.NextStakeDifficulty,
	}

	writeJSON(w, stakeDiffCurrent, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiffEstimates(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.BlockData.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, stakeDiff.Estimates, c.getIndentQuery(r))
}

func (c *appContext) getSSTxSummary(w http.ResponseWriter, r *http.Request) {
	sstxSummary := c.BlockData.GetMempoolSSTxSummary()
	if sstxSummary == nil {
		apiLog.Errorf("Unable to get SSTx info from mempool")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, sstxSummary, c.getIndentQuery(r))
}

func (c *appContext) getSSTxFees(w http.ResponseWriter, r *http.Request) {
	N := m.GetNCtx(r)
	sstxFees := c.BlockData.GetMempoolSSTxFeeRates(N)
	if sstxFees == nil {
		apiLog.Errorf("Unable to get SSTx fees from mempool")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, sstxFees, c.getIndentQuery(r))
}

func (c *appContext) getSSTxDetails(w http.ResponseWriter, r *http.Request) {
	N := m.GetNCtx(r)
	sstxDetails := c.BlockData.GetMempoolSSTxDetails(N)
	if sstxDetails == nil {
		apiLog.Errorf("Unable to get SSTx details from mempool")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, sstxDetails, c.getIndentQuery(r))
}

func (c *appContext) getBlockSize(w http.ResponseWriter, r *http.Request) {
	idx := c.getBlockHeightCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockSize, err := c.BlockData.GetBlockSize(int(idx))
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockSize, "")
}

func (c *appContext) getBlockRangeSize(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 || idx < idx0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockSizes, err := c.BlockData.GetBlockSizeRange(idx0, idx)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockSizes, "")
}

func (c *appContext) getBlockRangeSteppedSize(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 || idx < idx0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	step := m.GetBlockStepCtx(r)
	if step <= 0 {
		http.Error(w, "Yeaaah, that step's not gonna work with me.", 422)
		return
	}

	blockSizesFull, err := c.BlockData.GetBlockSizeRange(idx0, idx)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	var blockSizes []int32
	if step == 1 {
		blockSizes = blockSizesFull
	} else {
		numValues := (idx - idx0 + 1) / step
		blockSizes = make([]int32, 0, numValues)
		for i := idx0; i <= idx; i += step {
			blockSizes = append(blockSizes, blockSizesFull[i-idx0])
		}
		// it's the client's problem if i doesn't go all the way to idx
	}

	writeJSON(w, blockSizes, "")
}

func (c *appContext) getBlockRangeSummary(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// TODO: check that we have all in range

	// N := idx - idx0 + 1
	// summaries := make([]*apitypes.BlockDataBasic, 0, N)
	// for i := idx0; i <= idx; i++ {
	// 	summaries = append(summaries, c.BlockData.GetSummary(i))
	// }

	// writeJSON(w, summaries, c.getIndentQuery(r))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	indent := c.getIndentQuery(r)
	prefix, newline := indent, ""
	encoder.SetIndent(prefix, indent)
	if indent != "" {
		newline = "\n"
	}
	fmt.Fprintf(w, "[%s%s", newline, prefix)
	for i := idx0; i <= idx; i++ {
		summary := c.BlockData.GetSummary(i)
		if summary == nil {
			apiLog.Debugf("Unknown block %d", i)
			http.Error(w, fmt.Sprintf("I don't know block %d", i), http.StatusNotFound)
			return
		}
		// TODO: deal with the extra newline from Encode, if needed
		if err := encoder.Encode(summary); err != nil {
			apiLog.Infof("JSON encode error: %v", err)
			http.Error(w, http.StatusText(422), 422)
			return
		}
		if i != idx {
			fmt.Fprintf(w, ",%s%s", newline, prefix)
		}
	}
	fmt.Fprintf(w, "]")
}

func (c *appContext) getBlockRangeSteppedSummary(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	step := m.GetBlockStepCtx(r)
	if step <= 0 {
		http.Error(w, "Yeaaah, that step's not gonna work with me.", 422)
		return
	}

	// Compute the last block in the range
	numSteps := (idx - idx0) / step
	last := idx0 + step*numSteps
	// Support reverse list (e.g. 10/0/5 counts down from 10 to 0 in steps of 5)
	if idx0 > idx {
		step = -step
		// TODO: support reverse in other endpoints
	}

	// Prepare JSON encode for streaming response
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	indent := c.getIndentQuery(r)
	prefix, newline := indent, ""
	encoder.SetIndent(prefix, indent)
	if indent != "" {
		newline = "\n"
	}

	// Manually structure outer JSON array
	fmt.Fprintf(w, "[%s%s", newline, prefix)
	// Go through blocks in list, stop after last (i.e. on last+step)
	for i := idx0; i != last+step; i += step {
		summary := c.BlockData.GetSummary(i)
		if summary == nil {
			apiLog.Debugf("Unknown block %d", i)
			http.Error(w, fmt.Sprintf("I don't know block %d", i), http.StatusNotFound)
			return
		}
		// TODO: deal with the extra newline from Encode, if needed
		if err := encoder.Encode(summary); err != nil {
			apiLog.Infof("JSON encode error: %v", err)
			http.Error(w, http.StatusText(422), 422)
			return
		}
		// After last block, do not print comma+newline+prefix
		if i != last {
			fmt.Fprintf(w, ",%s%s", newline, prefix)
		}
	}
	fmt.Fprintf(w, "]")
}

func (c *appContext) getTicketPool(w http.ResponseWriter, r *http.Request) {
	// getBlockHeightCtx falls back to try hash if height fails
	idx := c.getBlockHeightCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	tp, err := c.BlockData.GetPool(idx)
	if err != nil {
		apiLog.Errorf("Unable to fetch ticket pool: %v", err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	sortPool := r.URL.Query().Get("sort")
	if sortPool == "1" || sortPool == "true" {
		sort.Strings(tp)
	}

	writeJSON(w, tp, c.getIndentQuery(r))
}

func (c *appContext) getTicketPoolInfo(w http.ResponseWriter, r *http.Request) {
	idx := c.getBlockHeightCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	tpi := c.BlockData.GetPoolInfo(int(idx))
	writeJSON(w, tpi, c.getIndentQuery(r))
}

func (c *appContext) getTicketPoolInfoRange(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	useArray := r.URL.Query().Get("arrays")
	if useArray == "1" || useArray == "true" {
		c.getTicketPoolValAndSizeRange(w, r)
		return
	}

	tpis := c.BlockData.GetPoolInfoRange(idx0, idx)
	if tpis == nil {
		http.Error(w, "invalid range", http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, tpis, c.getIndentQuery(r))
}

func (c *appContext) getTicketPoolValAndSizeRange(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	pvs, pss := c.BlockData.GetPoolValAndSizeRange(idx0, idx)
	if pvs == nil || pss == nil {
		http.Error(w, "invalid range", http.StatusUnprocessableEntity)
		return
	}

	tPVS := apitypes.TicketPoolValsAndSizes{
		StartHeight: uint32(idx0),
		EndHeight:   uint32(idx),
		Value:       pvs,
		Size:        pss,
	}
	writeJSON(w, tPVS, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiff(w http.ResponseWriter, r *http.Request) {
	idx := c.getBlockHeightCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	sdiff := c.BlockData.GetSDiff(int(idx))
	writeJSON(w, []float64{sdiff}, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiffRange(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	sdiffs := c.BlockData.GetSDiffRange(idx0, idx)
	writeJSON(w, sdiffs, c.getIndentQuery(r))
}

func (c *appContext) getAddressTransactions(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	count := m.GetNCtx(r)
	if address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	if count <= 0 {
		count = 10
	} else if count > 2000 {
		count = 2000
	}
	txs := c.BlockData.GetAddressTransactions(address, count)
	if txs == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	writeJSON(w, txs, c.getIndentQuery(r))
}

func (c *appContext) getAddressTransactionsRaw(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	count := m.GetNCtx(r)
	if address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	if count <= 0 {
		count = 10
	} else if count > 2000 {
		count = 2000
	}
	txs := c.BlockData.GetAddressTransactionsRaw(address, count)
	if txs == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	writeJSON(w, txs, c.getIndentQuery(r))
}

func (c *appContext) StakeVersionLatestCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.StakeVersionLatestCtx(r, c.BlockData.GetStakeVersionsLatest)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) BlockHashPathAndIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.BlockHashPathAndIndexCtx(r, c.BlockData)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) BlockIndexLatestCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.BlockIndexLatestCtx(r, c.BlockData)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) getBlockHeightCtx(r *http.Request) int64 {
	idx := m.GetBlockHeightCtx(r, c.BlockData)
	return idx
}

func (c *appContext) getBlockHashCtx(r *http.Request) string {
	hash := m.GetBlockHashCtx(r)
	if hash == "" {
		var err error
		hash, err = c.BlockData.GetBlockHash(int64(m.GetBlockIndexCtx(r)))
		if err != nil {
			apiLog.Errorf("Unable to GetBlockHash: %v", err)
		}
	}
	return hash
}
