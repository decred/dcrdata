package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/pressly/chi"
	"github.com/pressly/chi/docgen"
	//"github.com/decred/dcrrpcclient"
)

// Middlewares

type contextKey int

const (
	ctxX contextKey = iota
	ctxPathX
	ctxAPIDocs
	ctxAPIStatus
	ctxBlockIndex0
	ctxBlockIndex
)

func (c *appContext) StatusCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := &apitypes.Status{}
		// When no data yet, BlockData.Height = -1
		if c.BlockData != nil && c.BlockData.Height >= 0 {
			if summary := c.BlockData.GetBestBlockSummary(); summary != nil {
				apiLog.Trace("have block summary")
				if summary.Height == uint32(c.BlockData.Height) &&
					c.BlockData.GetBestBlock() != nil {
					apiLog.Trace("full block data agrees with summary data")
					status.Ready = true
					status.Height = summary.Height
				}
			}
		}

		// Set API status context
		ctx := context.WithValue(r.Context(), ctxAPIStatus, status)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SomeCtx sets X in the http.Request context used by all handlers with certain
// endpoints
func SomeCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxX, "A")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func BlockIndexPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathIdxStr := chi.URLParam(r, "idx")
		idx, err := strconv.Atoi(pathIdxStr)
		if err != nil {
			apiLog.Infof("No/invalid idx value (int64): %v", err)
			http.NotFound(w, r)
			//http.Error(w, http.StatusText(404), 404)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func BlockIndex0PathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathIdxStr := chi.URLParam(r, "idx0")
		idx, err := strconv.Atoi(pathIdxStr)
		if err != nil {
			apiLog.Infof("No/invalid idx value (int64): %v", err)
			http.NotFound(w, r)
			//http.Error(w, http.StatusText(404), 404)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex0, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) BlockIndexLatestCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := -1
		if c.BlockData != nil && c.BlockData.Height >= 0 {
			idx = c.BlockData.Height
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// apiDocs generates a middleware with a "docs" in the context containing a
// map of the routers handlers, etc.
func apiDocs(mux *chi.Mux) func(next http.Handler) http.Handler {
	var buf bytes.Buffer
	json.Indent(&buf, []byte(docgen.JSONRoutesDoc(mux)), "", "\t")
	docs := buf.String()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxAPIDocs, docs)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIDirectory is the actual handler used with apiDocs
// (e.g. mux.With(apiDocs(mux)).HandleFunc("/help", APIDirectory))
func APIDirectory(w http.ResponseWriter, r *http.Request) {
	docs := r.Context().Value(ctxAPIDocs).(string)
	io.WriteString(w, docs)
}
