package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	// "goji.io"
	// "goji.io/pat"
	// "golang.org/x/net/context"
	// "github.com/julienschmidt/httprouter"

	apitypes "github.com/chappjc/dcrdata/dcrdataapi"
	"github.com/decred/dcrrpcclient"
)

// dcrdata application context used by all route handlers
type appContext struct {
	nodeClient *dcrrpcclient.Client
	BlockData  *BlockDataToMemdb
}

// Constructor for appContext
func newContext(client *dcrrpcclient.Client, blockData *BlockDataToMemdb) *appContext {
	return &appContext{
		nodeClient: client,
		BlockData:  blockData,
	}
}

// Hanlders

// root is a http.Handler for the "/" path
func (c *appContext) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	fmt.Fprint(w, "dcrdata api running")
}

func (c *appContext) StatusCtx(next http.Handler) http.Handler {
	status := &apitypes.Status{}
	// When no data yet, BlockData.Height = -1
	if c.BlockData != nil && c.BlockData.Height >= 0 {
		if summary := c.BlockData.GetBestBlockSummary(); summary != nil {
			if summary.Height == uint32(c.BlockData.Height) &&
				c.BlockData.GetBestBlock() != nil {
				status.Ready = true
				status.Height = summary.Height
			}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxAPIStatus, status)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
