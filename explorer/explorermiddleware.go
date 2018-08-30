// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi"
)

type contextKey int

const (
	ctxSearch contextKey = iota
	ctxBlockIndex
	ctxBlockHash
	ctxTxHash
	ctxAddress
	ctxAgendaId
)

func (exp *explorerUI) BlockHashPathOrIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		height, err := strconv.ParseInt(chi.URLParam(r, "blockhash"), 10, 0)
		var hash string
		if err != nil {
			hash = chi.URLParam(r, "blockhash")
			height, err = exp.blockData.GetBlockHeight(hash)
			if err != nil {
				log.Errorf("GetBlockHeight(%s) failed: %v", hash, err)
				exp.StatusPage(w, defaultErrorCode, "could not find that block", NotFoundStatusType)
				return
			}
		} else {
			//Handle future blocks
			maxHeight := int64(exp.blockData.GetHeight())
			if height > maxHeight {
				expectedTime := time.Duration(height-maxHeight) * exp.ChainParams.TargetTimePerBlock
				message := fmt.Sprintf("This block is expected to arrive in approximately in %v. ", expectedTime)
				log.Errorf("Block with height %d will arrive in future.", height)
				exp.StatusPage(w, defaultErrorCode, message, NotFoundStatusType)
				return
			}

			hash, err = exp.blockData.GetBlockHash(height)
			if err != nil {
				log.Errorf("GetBlockHeight(%d) failed: %v", height, err)
				exp.StatusPage(w, defaultErrorCode, "could not find that block", NotFoundStatusType)
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

func getAgendaIDCtx(r *http.Request) string {
	hash, ok := r.Context().Value(ctxAgendaId).(string)
	if !ok {
		log.Trace("Agendaid not set")
		return ""
	}
	return hash
}

// TransactionHashCtx embeds "txid" into the request context
func TransactionHashCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		txid := chi.URLParam(r, "txid")
		ctx := context.WithValue(r.Context(), ctxTxHash, txid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AddressPathCtx embeds "address" into the request context
func AddressPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := chi.URLParam(r, "address")
		ctx := context.WithValue(r.Context(), ctxAddress, address)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AgendaPathCtx embeds "agendaid" into the request context
func AgendaPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agendaid := chi.URLParam(r, "agendaid")
		ctx := context.WithValue(r.Context(), ctxAgendaId, agendaid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
