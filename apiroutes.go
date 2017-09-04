package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
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
	GetBlockVerboseWithTxTypes(hash string) *apitypes.BlockDataWithTxType
	GetRawTransaction(txid string) *apitypes.Tx
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
	GetPoolValAndSizeRange(idx0, idx1 int) ([]float64, []float64)
	GetSDiff(idx int) float64
	GetSDiffRange(idx0, idx1 int) []float64
	GetMempoolSSTxSummary() *apitypes.MempoolTicketFeeInfo
	GetMempoolSSTxFeeRates(N int) *apitypes.MempoolTicketFees
	GetMempoolSSTxDetails(N int) *apitypes.MempoolTicketDetails
	GetAddressTransactions(addr string, count int) *apitypes.Address
	GetAddressTransactionsRaw(addr string, count int) []*apitypes.AddressTxRaw
}

// dcrdata application context used by all route handlers
type appContext struct {
	nodeClient *dcrrpcclient.Client
	BlockData  APIDataSource
	Status     apitypes.Status
	statusMtx  sync.RWMutex
	JSONIndent string
}

// Constructor for appContext
func newContext(client *dcrrpcclient.Client, blockData APIDataSource, JSONIndent string) *appContext {
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
		case height, ok := <-ntfnChans.updateStatusNodeHeight:
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

		case height, ok := <-ntfnChans.updateStatusDBHeight:
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

func getBlockStepCtx(r *http.Request) int {
	step, ok := r.Context().Value(ctxBlockStep).(int)
	if !ok {
		apiLog.Error("block step not set")
		return -1
	}
	return step
}

func getBlockIndexCtx(r *http.Request) int {
	idx, ok := r.Context().Value(ctxBlockIndex).(int)
	if !ok {
		apiLog.Error("block index not set")
		return -1
	}
	return idx
}

func getBlockIndex0Ctx(r *http.Request) int {
	idx, ok := r.Context().Value(ctxBlockIndex0).(int)
	if !ok {
		apiLog.Error("block index0 not set")
		return -1
	}
	return idx
}

func getBlockHashOnlyCtx(r *http.Request) string {
	hash, ok := r.Context().Value(ctxBlockHash).(string)
	if !ok {
		apiLog.Trace("block hash not set")
		return ""
	}
	return hash
}

func (c *appContext) getBlockHashCtx(r *http.Request) string {
	hash := getBlockHashOnlyCtx(r)
	if hash == "" {
		var err error
		hash, err = c.BlockData.GetBlockHash(int64(getBlockIndexCtx(r)))
		if err != nil {
			apiLog.Errorf("Unable to GetBlockHash: %v", err)
		}
	}
	return hash
}

func (c *appContext) getBlockHeightCtx(r *http.Request) int64 {
	idxI, ok := r.Context().Value(ctxBlockIndex).(int)
	idx := int64(idxI)
	if !ok || idx < 0 {
		var err error
		idx, err = c.BlockData.GetBlockHeight(getBlockHashOnlyCtx(r))
		if err != nil {
			apiLog.Errorf("Unable to GetBlockHeight: %v", err)
		}
	}
	return idx
}

func getTxIDCtx(r *http.Request) string {
	hash, ok := r.Context().Value(ctxTxHash).(string)
	if !ok {
		apiLog.Trace("txid not set")
		return ""
	}
	return hash
}

func getTxIOIndexCtx(r *http.Request) int {
	index, ok := r.Context().Value(ctxTxInOutIndex).(int)
	if !ok {
		apiLog.Trace("txinoutindex not set")
		return -1
	}
	return index
}

func getAddressCtx(r *http.Request) string {
	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		apiLog.Trace("address not set")
		return ""
	}
	return address
}

func getNCtx(r *http.Request) int {
	N, ok := r.Context().Value(ctxN).(int)
	if !ok {
		apiLog.Trace("N not set")
		return -1
	}
	return N
}

func getStatusCtx(r *http.Request) *apitypes.Status {
	status, ok := r.Context().Value(ctxAPIStatus).(*apitypes.Status)
	if !ok {
		apiLog.Error("apitypes.Status not set")
		return nil
	}
	return status
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

func (c *appContext) getTransaction(w http.ResponseWriter, r *http.Request) {
	txid := getTxIDCtx(r)
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

// getTransactionInputs serves []TxIn
func (c *appContext) getTransactionInputs(w http.ResponseWriter, r *http.Request) {
	txid := getTxIDCtx(r)
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
	txid := getTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	index := getTxIOIndexCtx(r)
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
	txid := getTxIDCtx(r)
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
	txid := getTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	index := getTxIOIndexCtx(r)
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
	N := getNCtx(r)
	sstxFees := c.BlockData.GetMempoolSSTxFeeRates(N)
	if sstxFees == nil {
		apiLog.Errorf("Unable to get SSTx fees from mempool")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, sstxFees, c.getIndentQuery(r))
}

func (c *appContext) getSSTxDetails(w http.ResponseWriter, r *http.Request) {
	N := getNCtx(r)
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
	idx0 := getBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := getBlockIndexCtx(r)
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
	idx0 := getBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := getBlockIndexCtx(r)
	if idx < 0 || idx < idx0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	step := getBlockStepCtx(r)
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
	idx0 := getBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := getBlockIndexCtx(r)
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
		// TODO: deal with the extra newline from Encode, if needed
		if err := encoder.Encode(c.BlockData.GetSummary(i)); err != nil {
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
	idx0 := getBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := getBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	step := getBlockStepCtx(r)
	if step <= 0 {
		http.Error(w, "Yeaaah, that step's not gonna work with me.", 422)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	indent := c.getIndentQuery(r)
	prefix, newline := indent, ""
	encoder.SetIndent(prefix, indent)
	if indent != "" {
		newline = "\n"
	}
	fmt.Fprintf(w, "[%s%s", newline, prefix)
	for i := idx0; i <= idx; i += step {
		// TODO: deal with the extra newline from Encode, if needed
		if err := encoder.Encode(c.BlockData.GetSummary(i)); err != nil {
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
	idx0 := getBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := getBlockIndexCtx(r)
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
	idx0 := getBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := getBlockIndexCtx(r)
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
	idx0 := getBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := getBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	sdiffs := c.BlockData.GetSDiffRange(idx0, idx)
	writeJSON(w, sdiffs, c.getIndentQuery(r))
}

func (c *appContext) getAddressTransactions(w http.ResponseWriter, r *http.Request) {
	address := getAddressCtx(r)
	count := getNCtx(r)
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
	address := getAddressCtx(r)
	count := getNCtx(r)
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
