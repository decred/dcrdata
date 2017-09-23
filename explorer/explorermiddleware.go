// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
package explorer

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"strconv"

	"github.com/go-chi/chi"
)

type contextKey int

const (
	ctxSearch contextKey = iota
	ctxBlockIndex
	ctxBlockHash
	ctxTxHash
	ctxAddress
)

// searchPathCtx returns a http.HandlerFunc that embeds the value at the url part
// {search} into the request context
func searchPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		str := chi.URLParam(r, "search")
		ctx := context.WithValue(r.Context(), ctxSearch, str)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (exp *explorerUI) blockHashPathOrIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		height, err := strconv.ParseInt(chi.URLParam(r, "blockhash"), 10, 0)
		var hash string
		if err != nil {
			hash = chi.URLParam(r, "blockhash")
			height, err = exp.blockData.GetBlockHeight(hash)
			if err != nil {
				log.Errorf("GetBlockHeight(%s) failed: %v", hash, err)
				http.NotFound(w, r)
				return
			}
		} else {
			hash, err = exp.blockData.GetBlockHash(height)
			if err != nil {
				log.Errorf("GetBlockHeight(%d) failed: %v", height, err)
				http.NotFound(w, r)
				return
			}
		}

		ctx := context.WithValue(r.Context(), ctxBlockHash, hash)
		ctx = context.WithValue(ctx, ctxBlockIndex, height)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func getBlockHashCtx(r *http.Request) string {
	hash, ok := r.Context().Value(ctxBlockHash).(string)
	if !ok {
		log.Trace("Block Hash not set")
		return ""
	}
	return hash
}

func getBlockHeightCtx(r *http.Request) int64 {
	idxI, ok := r.Context().Value(ctxBlockIndex).(int)
	idx := int64(idxI)
	if !ok {
		log.Trace("Block Height not set")
		return -1
	}
	return idx
}

func getTxIDCtx(r *http.Request) string {
	hash, ok := r.Context().Value(ctxTxHash).(string)
	if !ok {
		log.Trace("Txid not set")
		return ""
	}
	return hash
}

func transactionHashCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		txid := chi.URLParam(r, "txid")
		ctx := context.WithValue(r.Context(), ctxTxHash, txid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// templateExecToString executes the input template with given name using the
// supplied data, and writes the result into a string. If the template fails to
// execute, a non-nil error will be returned. Check it before writing to the
// client, otherwise you might as well execute directly into your response
// writer instead of the internal buffer of this function.
func templateExecToString(t *template.Template, name string, data interface{}) (string, error) {
	var page bytes.Buffer
	err := t.ExecuteTemplate(&page, name, data)
	return page.String(), err
}

func addressPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := chi.URLParam(r, "address")
		ctx := context.WithValue(r.Context(), ctxAddress, address)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
