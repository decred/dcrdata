package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/go-chi/docgen"
)

// Middlewares

type contextKey int

const (
	ctxAPIDocs contextKey = iota
	ctxAPIStatus
	ctxAddress
	ctxBlockIndex0
	ctxBlockIndex
	ctxBlockStep
	ctxBlockHash
	ctxTxHash
	ctxTxInOutIndex
	ctxSearch
	ctxN
	ctxStakeVersionLatest
)

// CacheControl creates a new middleware to set the HTTP response header with
// "Cache-Control: max-age=maxAge" where maxAge is in seconds.
func CacheControl(maxAge int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "max-age="+strconv.FormatInt(maxAge, 10))
			next.ServeHTTP(w, r)
		})
	}
}

func (c *appContext) StatusCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set API status context
		ctx := context.WithValue(r.Context(), ctxAPIStatus, &c.Status)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockStepPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {step} into the request context
func BlockStepPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stepIdxStr := chi.URLParam(r, "step")
		step, err := strconv.Atoi(stepIdxStr)
		if err != nil {
			apiLog.Infof("No/invalid step value (int64): %v", err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockStep, step)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockIndexPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {idx} into the request context
func BlockIndexPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathIdxStr := chi.URLParam(r, "idx")
		idx, err := strconv.Atoi(pathIdxStr)
		if err != nil {
			apiLog.Infof("No/invalid idx value (int64): %v", err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockIndex0PathCtx returns a http.HandlerFunc that embeds the value at the url
// part {idx0} into the request context
func BlockIndex0PathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathIdxStr := chi.URLParam(r, "idx0")
		idx, err := strconv.Atoi(pathIdxStr)
		if err != nil {
			apiLog.Infof("No/invalid idx0 value (int64): %v", err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex0, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// NPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {N} into the request context
func NPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathNStr := chi.URLParam(r, "N")
		N, err := strconv.Atoi(pathNStr)
		if err != nil {
			apiLog.Infof("No/invalid numeric value (uint64): %v", err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxN, N)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) StakeVersionLatestCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ver := -1
		if c.BlockData != nil {
			stkVers, err := c.BlockData.GetStakeVersionsLatest()
			if err == nil && stkVers != nil {
				ver = int(stkVers.StakeVersion)
			}
		}
		ctx := context.WithValue(r.Context(), ctxStakeVersionLatest, ver)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) BlockIndexLatestCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := -1
		if c.BlockData != nil && c.BlockData.GetHeight() >= 0 {
			idx = c.BlockData.GetHeight()
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) BlockHashLatestCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := -1
		hash := ""
		if c.BlockData != nil {
			var err error
			// if hash, err = c.BlockData.GetBestBlockHash(int64(idx)); err != nil {
			// 	apiLog.Errorf("Unable to GetBestBlockHash: %v", idx, err)
			// }
			if idx = c.BlockData.GetHeight(); idx >= 0 {
				if hash, err = c.BlockData.GetBlockHash(int64(idx)); err != nil {
					apiLog.Errorf("Unable to GetBlockHash(%d): %v", idx, err)
				}
			}
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex, idx)
		ctx = context.WithValue(ctx, ctxBlockHash, hash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockHashPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {blockhash} into the request context
func BlockHashPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "blockhash")
		ctx := context.WithValue(r.Context(), ctxBlockHash, hash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) BlockHashPathAndIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "blockhash")
		height, err := c.BlockData.GetBlockHeight(hash)
		if err != nil {
			apiLog.Errorf("Unable to GetBlockHeight(%d): %v", height, err)
		}
		ctx := context.WithValue(r.Context(), ctxBlockHash, hash)
		ctx = context.WithValue(ctx, ctxBlockIndex, height)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TransactionHashCtx returns a http.HandlerFunc that embeds the value at the url
// part {txid} into the request context
func TransactionHashCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		txid := chi.URLParam(r, "txid")
		ctx := context.WithValue(r.Context(), ctxTxHash, txid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TransactionIOIndexCtx returns a http.HandlerFunc that embeds the value at the url
// part {txinoutindex} into the request context
func TransactionIOIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idxStr := chi.URLParam(r, "txinoutindex")
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			apiLog.Infof("No/invalid numeric value (%v): %v", idxStr, err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxTxInOutIndex, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AddressPathCtx returns a http.HandlerFunc that embeds the value at the url part
// {address} into the request context.
func AddressPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := chi.URLParam(r, "address")
		ctx := context.WithValue(r.Context(), ctxAddress, address)
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

// SearchPathCtx returns a http.HandlerFunc that embeds the value at the url part
// {search} into the request context (Still need this for the error page)
// TODO: make new error system
func SearchPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		str := chi.URLParam(r, "search")
		ctx := context.WithValue(r.Context(), ctxSearch, str)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// APIDirectory is the actual handler used with apiDocs
// (e.g. mux.With(apiDocs(mux)).HandleFunc("/help", APIDirectory))
func APIDirectory(w http.ResponseWriter, r *http.Request) {
	docs := r.Context().Value(ctxAPIDocs).(string)
	io.WriteString(w, docs)
}
