package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

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
)

// SomeCtx sets X in the http.Request context used by all handlers with certain
// endpoints
func SomeCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxX, "A")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func PathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathXStr := chi.URLParam(r, "pathX")
		pathXval, err := strconv.Atoi(pathXStr)
		if err != nil {
			apiLog.Infof("No/invalid pathX value (int64): %v", err)
			http.NotFound(w, r)
			//http.Error(w, http.StatusText(404), 404)
			return
		}
		ctx := context.WithValue(r.Context(), ctxPathX, int(pathXval))
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
