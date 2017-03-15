package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
)

type APIDataSource interface {
	GetHeight() int
	//Get(idx int) *blockdata.BlockData
	GetHeader(idx int) *dcrjson.GetBlockHeaderVerboseResult
	GetFeeInfo(idx int) *dcrjson.FeeInfoBlock
	//GetStakeDiffEstimate(idx int) *dcrjson.EstimateStakeDiffResult
	GetStakeInfoExtended(idx int) *apitypes.StakeInfoExtended
	GetStakeDiffEstimates() *apitypes.StakeDiff
	//GetBestBlock() *blockdata.BlockData
	GetSummary(idx int) *apitypes.BlockDataBasic
	GetBestBlockSummary() *apitypes.BlockDataBasic
	GetMempoolSSTxSummary() *apitypes.MempoolTicketFeeInfo
	//GetMempoolSSTxFees(N int) *apitypes.MempoolTicketFees
	//GetMempoolSSTxDetails(N int) *apitypes.MempoolTicketDetails
}

// dcrdata application context used by all route handlers
type appContext struct {
	nodeClient *dcrrpcclient.Client
	BlockData  APIDataSource
}

// Constructor for appContext
func newContext(client *dcrrpcclient.Client, blockData APIDataSource) *appContext {
	return &appContext{
		nodeClient: client,
		BlockData:  blockData,
	}
}

// root is a http.Handler for the "/" path
func (c *appContext) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	fmt.Fprint(w, "dcrdata api running")
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

func (c *appContext) status(w http.ResponseWriter, r *http.Request) {
	status := getStatusCtx(r)
	if status == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(*status); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) currentHeight(w http.ResponseWriter, r *http.Request) {
	status := getStatusCtx(r)
	if status == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.WriteString(w, strconv.Itoa(int(status.Height))); err != nil {
		apiLog.Infof("failed to write height response: %v", err)
	}
}

func (c *appContext) getLatestBlock(w http.ResponseWriter, r *http.Request) {
	latestBlockSummary := c.BlockData.GetBestBlockSummary()
	if latestBlockSummary == nil {
		apiLog.Error("Unable to get latest block summary")
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(latestBlockSummary); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getBlockSummary(w http.ResponseWriter, r *http.Request) {
	idx := getBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockSummary := c.BlockData.GetSummary(idx)
	if blockSummary == nil {
		apiLog.Errorf("Unable to get block %d summary", idx)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(blockSummary); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getBlockHeader(w http.ResponseWriter, r *http.Request) {
	idx := getBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockHeader := c.BlockData.GetHeader(idx)
	if blockHeader == nil {
		apiLog.Errorf("Unable to get block %d header", idx)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(blockHeader); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getBlockFeeInfo(w http.ResponseWriter, r *http.Request) {
	idx := getBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockFeeInfo := c.BlockData.GetFeeInfo(idx)
	if blockFeeInfo == nil {
		apiLog.Errorf("Unable to get block %d fee info", idx)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(blockFeeInfo); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getBlockStakeInfoExtended(w http.ResponseWriter, r *http.Request) {
	idx := getBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	stakeinfo := c.BlockData.GetStakeInfoExtended(idx)
	if stakeinfo == nil {
		apiLog.Errorf("Unable to get block %d fee info", idx)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(stakeinfo); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getStakeDiff(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.BlockData.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(stakeDiff); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getStakeDiffCurrent(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.BlockData.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
	}

	stakeDiffCurrent := dcrjson.GetStakeDifficultyResult{
		CurrentStakeDifficulty: stakeDiff.CurrentStakeDifficulty,
		NextStakeDifficulty:    stakeDiff.NextStakeDifficulty,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(stakeDiffCurrent); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getStakeDiffEstimates(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.BlockData.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(stakeDiff.Estimates); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func (c *appContext) getSSTxSummary(w http.ResponseWriter, r *http.Request) {
	sstxSummary := c.BlockData.GetMempoolSSTxSummary()
	if sstxSummary == nil {
		apiLog.Errorf("Unable to get SSTx info from mempool")
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(sstxSummary); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

// func (c *appContext) getSSTxFees(w http.ResponseWriter, r *http.Request) {
// 	N := getNCtx(r)
// 	sstxFees := c.BlockData.GetMempoolSSTxFees(N)
// 	if sstxFees == nil {
// 		apiLog.Errorf("Unable to get SSTx fees from mempool")
// 	}

// 	w.Header().Set("Content-Type", "application/json; charset=utf-8")
// 	if err := json.NewEncoder(w).Encode(sstxFees); err != nil {
// 		apiLog.Infof("JSON encode error: %v", err)
// 	}
// }

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

	N := idx - idx0 + 1
	summaries := make([]*apitypes.BlockDataBasic, 0, N)
	for i := idx0; i <= idx; i++ {
		summaries = append(summaries, c.BlockData.GetSummary(i))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(summaries); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}

	// DEBUGGING
	// msg := fmt.Sprintf("block range: %d to %d", idx0, idx)

	// w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// if _, err := io.WriteString(w, msg); err != nil {
	// 	apiLog.Infof("failed to write response: %v", err)
	// }
}
