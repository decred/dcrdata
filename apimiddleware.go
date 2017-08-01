package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/docgen"
)

// Middlewares

type contextKey int

const (
	ctxAPIDocs contextKey = iota
	ctxAPIStatus
	ctxBlockIndex0
	ctxBlockIndex
	ctxN
)

func (c *appContext) StatusCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set API status context
		ctx := context.WithValue(r.Context(), ctxAPIStatus, &c.Status)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SomeCtx sets X in the http.Request context used by all handlers with certain
// endpoints
// func SomeCtx(next http.Handler) http.Handler {
// 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		ctx := context.WithValue(r.Context(), ctxX, "A")
// 		next.ServeHTTP(w, r.WithContext(ctx))
// 	})
// }

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
			apiLog.Infof("No/invalid idx0 value (int64): %v", err)
			http.NotFound(w, r)
			//http.Error(w, http.StatusText(404), 404)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex0, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func NPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathNStr := chi.URLParam(r, "N")
		N, err := strconv.Atoi(pathNStr)
		if err != nil {
			apiLog.Infof("No/invalid numeric value (uint64): %v", err)
			http.NotFound(w, r)
			//http.Error(w, http.StatusText(404), 404)
			return
		}
		ctx := context.WithValue(r.Context(), ctxN, N)
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
