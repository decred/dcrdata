// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

	"github.com/decred/dcrd/chaincfg/chainhash"
	apitypes "github.com/decred/dcrdata/api/types"
	m "github.com/decred/dcrdata/middleware"
	"github.com/go-chi/chi"
)

type contextKey int

const (
	ctxFrom contextKey = iota
	ctxTo
	ctxNoAsm
	ctxNoScriptSig
	ctxNoSpent
	ctxNoTxList
	ctxAddrCmd
	ctxNbBlocks
)

// BlockHashPathAndIndexCtx is a middleware that embeds the value at the url
// part {blockhash}, and the corresponding block index, into a request context.
func (iapi *InsightApi) BlockHashPathAndIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.BlockHashPathAndIndexCtx(r, iapi.BlockData.ChainDB)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// StatusInfoCtx is a middleware that embeds into the request context the data
// for the "?q=x" URL query, where x is "getInfo" or "getDifficulty" or
// "getBestBlockHash" or "getLastBlockHash".
func (iapi *InsightApi) StatusInfoCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.StatusInfoCtx(r, iapi.BlockData.ChainDB)
		next.ServeHTTP(w, r.WithContext(ctx))
	})

}

func (iapi *InsightApi) getBlockHashCtx(r *http.Request) (string, error) {
	hash, err := m.GetBlockHashCtx(r)
	if err != nil {
		idx := int64(m.GetBlockIndexCtx(r))
		hash, err = iapi.BlockData.ChainDB.GetBlockHash(idx)
		if err != nil {
			apiLog.Errorf("Unable to GetBlockHash: %v", err)
			return "", err
		}
	}
	return hash, nil
}

func (iapi *InsightApi) getBlockChainHashCtx(r *http.Request) (*chainhash.Hash, error) {
	hashStr, err := iapi.getBlockHashCtx(r)
	if err != nil {
		return nil, err
	}
	hash, err := chainhash.NewHashFromStr(hashStr)
	if err != nil {
		apiLog.Errorf("Failed to parse block hash: %v", err)
		return nil, err
	}
	return hash, nil
}

// GetToCtx retrieves the ctxTo data ("to") from the request context. If not
// set, the return value ok is false.
func GetToCtx(r *http.Request) (int64, bool) {
	to, ok := r.Context().Value(ctxTo).(int)
	if !ok {
		return int64(0), false
	}
	return int64(to), true
}

// GetFromCtx retrieves the ctxFrom data ("from") from the request context.
// If not set, the return value is 0
func GetFromCtx(r *http.Request) int64 {
	from, ok := r.Context().Value(ctxFrom).(int)
	if !ok {
		return int64(0)
	}
	return int64(from)
}

// GetNoAsmCtx retrieves the ctxNoAsm data ("noAsm") from the request context.
// If not set, the return value is false.
func GetNoAsmCtx(r *http.Request) bool {
	noAsm, ok := r.Context().Value(ctxNoAsm).(bool)
	if !ok {
		return false
	}
	return noAsm
}

// GetNoScriptSigCtx retrieves the ctxNoScriptSig data ("noScriptSig") from the
// request context. If not set, the return value is false.
func GetNoScriptSigCtx(r *http.Request) bool {
	noScriptSig, ok := r.Context().Value(ctxNoScriptSig).(bool)
	if !ok {
		return false
	}
	return noScriptSig
}

// GetNoSpentCtx retrieves the ctxNoSpent data ("noSpent") from the
// request context. If not set, the return value is false.
func GetNoSpentCtx(r *http.Request) bool {
	noSpent, ok := r.Context().Value(ctxNoSpent).(bool)
	if !ok {
		return false
	}
	return noSpent
}

// FromToPaginationCtx will parse the query parameters for from/to values.
func FromToPaginationCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		to, from := r.FormValue("to"), r.FormValue("from")
		fromint, err := strconv.Atoi(from)
		if err == nil {
			ctx = context.WithValue(r.Context(), ctxFrom, fromint)
		}
		toint, err := strconv.Atoi(to)
		if err == nil {
			ctx = context.WithValue(ctx, ctxTo, toint)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ValidatePostCtx will confirm Post content length is valid.
func (iapi *InsightApi) ValidatePostCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentLengthString := r.Header.Get("Content-Length")
		contentLength, err := strconv.Atoi(contentLengthString)
		if err != nil {
			writeInsightError(w, "Content-Length Header must be set")
			return
		}
		// Broadcast Tx has the largest possible body.  Cap max content length
		// to iapi.params.MaxTxSize * 2 plus some arbitrary extra for JSON
		// encapsulation.
		maxPayload := (iapi.params.MaxTxSize * 2) + 50
		if contentLength > maxPayload {
			writeInsightError(w, fmt.Sprintf("Maximum Content-Length is %d", maxPayload))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PostAddrsTxsCtx middleware processes parameters given in the POST request
// body for an addrs endpoint.
func PostAddrsTxsCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.InsightMultiAddrsTx
		var from, to, noAsm, noScriptSig, noSpent int64

		body, err := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			writeInsightError(w, fmt.Sprintf("error reading JSON message: %v", err))
			return
		}
		err = json.Unmarshal(body, &req)
		if err != nil {
			writeInsightError(w, fmt.Sprintf("Failed to parse request: %v", err))
			return
		}
		// Successful extraction of body JSON.
		ctx := context.WithValue(r.Context(), m.CtxAddress, req.Addresses)

		if req.From != "" {
			from, err = req.From.Int64()
			if err == nil {
				ctx = context.WithValue(ctx, ctxFrom, int(from))
			}
		}
		if req.To != "" {
			to, err = req.To.Int64()
			if err == nil {
				ctx = context.WithValue(ctx, ctxTo, int(to))
			}
		}
		if req.NoAsm != "" {
			noAsm, err = req.NoAsm.Int64()
			if err == nil && noAsm != 0 {
				ctx = context.WithValue(ctx, ctxNoAsm, true)
			}
		}
		if req.NoScriptSig != "" {
			noScriptSig, err = req.NoScriptSig.Int64()
			if err == nil && noScriptSig != 0 {
				ctx = context.WithValue(ctx, ctxNoScriptSig, true)
			}
		}

		if req.NoSpent != "" {
			noSpent, err = req.NoSpent.Int64()
			if err == nil && noSpent != 0 {
				ctx = context.WithValue(ctx, ctxNoSpent, true)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PostAddrsUtxoCtx middleware processes parameters given in the POST request
// body for an addrs utxo endpoint.
func PostAddrsUtxoCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := apitypes.InsightAddr{}
		body, err := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			writeInsightError(w, fmt.Sprintf("error reading JSON message: %v", err))
			return
		}

		err = json.Unmarshal(body, &req)
		if err != nil {
			writeInsightError(w, fmt.Sprintf("Failed to parse request: %v", err))
			return
		}

		// Successful extraction of Body JSON
		ctx := context.WithValue(r.Context(), m.CtxAddress, req.Addrs)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AddressCommandCtx returns a http.HandlerFunc that embeds the value at the url
// part {command} into the request context.
func AddressCommandCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		command := chi.URLParam(r, "command")
		ctx := context.WithValue(r.Context(), ctxAddrCmd, command)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetAddressCommandCtx retrieves the ctxAddrCmd data from the request context.
// If not set the return value is "" and false.
func GetAddressCommandCtx(r *http.Request) (string, bool) {
	command, ok := r.Context().Value(ctxAddrCmd).(string)
	if !ok {
		return "", false
	}
	return command, true
}

// NoTxListCtx returns a http.Handlerfunc that embeds the {noTxList} value in
// the request into the request context.
func NoTxListCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notxlist := r.FormValue("noTxList")
		notxlistint, err := strconv.Atoi(notxlist)
		if err != nil {
			notxlistint = 0
		}
		ctx := context.WithValue(r.Context(), ctxNoTxList, notxlistint)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetLimitCtx retrieves the ctxLimit data from the request context. If not set,
// the return value is 0 which is interpreted as no limit.
func GetLimitCtx(r *http.Request) int {
	limit, ok := r.Context().Value(m.CtxLimit).(string)
	if !ok {
		return 0
	}
	intValue, err := strconv.Atoi(limit)
	if err != nil {
		return 0
	}
	return intValue
}

// GetNoTxListCtx retrieves the ctxNoTxList data ("noTxList") from the request context.
// If not set, the return value is false.
func GetNoTxListCtx(r *http.Request) int {
	notxlist, ok := r.Context().Value(ctxNoTxList).(int)
	if !ok {
		return 0
	}
	return notxlist
}

// BlockDateLimitQueryCtx returns a http.Handlerfunc that embeds the
// {blockdate,limit} value in the request into the request context.
func BlockDateLimitQueryCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blockDate := r.FormValue("blockDate")
		ctx := context.WithValue(r.Context(), m.CtxBlockDate, blockDate)
		limit := r.FormValue("limit")
		ctx = context.WithValue(ctx, m.CtxLimit, limit)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetNbBlocksCtx retrieves the ctxNbBlocks data from the request context. If not
// set, the return value is 0.
func GetNbBlocksCtx(r *http.Request) int {
	nbBlocks, ok := r.Context().Value(ctxNbBlocks).(int)
	if !ok {
		return 0
	}
	return nbBlocks
}

// NbBlocksCtx will parse the query parameters for nbBlocks.
func NbBlocksCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		nbBlocks := r.FormValue("nbBlocks")
		nbBlocksint, err := strconv.Atoi(nbBlocks)
		if err == nil {
			ctx = context.WithValue(r.Context(), ctxNbBlocks, nbBlocksint)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
