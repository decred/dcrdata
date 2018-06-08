// Copyright (c) 2018, The Decred developers
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

	apitypes "github.com/decred/dcrdata/api/types"
	m "github.com/decred/dcrdata/middleware"
)

type contextKey int

const (
	ctxRawHexTx contextKey = iota
	ctxFrom
	ctxTo
	ctxNoAsm
	ctxNoScriptSig
	ctxNoSpent
)

// BlockHashPathAndIndexCtx is a middleware that embeds the value at the url
// part {blockhash}, and the corresponding block index, into a request context.
func (c *insightApiContext) BlockHashPathAndIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.BlockHashPathAndIndexCtx(r, c.BlockData.ChainDB)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// StatusCtx is a middleware that embeds into the request context the data for
// the "?q=x" URL query, where x is "getInfo" or "getDifficulty".
func (c *insightApiContext) StatusInfoCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.StatusInfoCtx(r, c.BlockData.ChainDB)
		next.ServeHTTP(w, r.WithContext(ctx))
	})

}

func (c *insightApiContext) getBlockHashCtx(r *http.Request) string {
	hash := m.GetBlockHashCtx(r)
	if hash == "" {
		var err error
		hash, err = c.BlockData.ChainDB.GetBlockHash(int64(m.GetBlockIndexCtx(r)))
		if err != nil {
			apiLog.Errorf("Unable to GetBlockHash: %v", err)
		}
	}
	return hash
}

// GetRawHexTx retrieves the ctxRawHexTx data from the request context. If not
// set, the return value is an empty string.
func (c *insightApiContext) GetRawHexTx(r *http.Request) (string, bool) {
	rawHexTx, ok := r.Context().Value(ctxRawHexTx).(string)
	if !ok {
		apiLog.Trace("Rawtx hex transaction not set")
		return "", false
	}
	return rawHexTx, true
}

// Process params given in post body for an broadcast tx endpoint.
func (c *insightApiContext) PostBroadcastTxCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.InsightRawTx
		body, err := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			apiLog.Errorf("error reading JSON message: %v", err)
			writeInsightError(w, fmt.Sprintf("error reading JSON message: %v", err))
			return
		}
		err = json.Unmarshal(body, &req)
		if err != nil {
			apiLog.Errorf("Failed to parse request: %v", err)
			writeInsightError(w, fmt.Sprintf("Failed to parse request: %v", err))
			return
		}
		// Successful extraction of Body JSON as long as the rawtx is not empty
		// string we should return it
		if req.Rawtx == "" {
			writeInsightError(w, fmt.Sprintf("rawtx cannot be an empty string."))
			return
		}

		ctx := context.WithValue(r.Context(), ctxRawHexTx, req.Rawtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetToCtx retrieves the ctxTo data ("to") from the request context. If not
// set, the return value ok is false.
func (c *insightApiContext) GetToCtx(r *http.Request) (int64, bool) {
	to, ok := r.Context().Value(ctxTo).(int)
	if !ok {
		return int64(0), false
	}
	return int64(to), true
}

// GetFromCtx retrieves the ctxFrom data ("from") from the request context.
// If not set, the return value is 0
func (c *insightApiContext) GetFromCtx(r *http.Request) int64 {
	from, ok := r.Context().Value(ctxFrom).(int)
	if !ok {
		return int64(0)
	}
	return int64(from)
}

// GetNoAsmCtx retrieves the ctxNoAsm data ("noAsm") from the request context.
// If not set, the return value is false.
func (c *insightApiContext) GetNoAsmCtx(r *http.Request) bool {
	noAsm, ok := r.Context().Value(ctxNoAsm).(bool)
	if !ok {
		return false
	}
	return noAsm
}

// GetNoScriptSigCtx retrieves the ctxNoScriptSig data ("noScriptSig") from the
// request context. If not set, the return value is false.
func (c *insightApiContext) GetNoScriptSigCtx(r *http.Request) bool {
	noScriptSig, ok := r.Context().Value(ctxNoScriptSig).(bool)
	if !ok {
		return false
	}
	return noScriptSig
}

// GetNoSpentCtx retrieves the ctxNoSpent data ("noSpent") from the
// request context. If not set, the return value is false.
func (c *insightApiContext) GetNoSpentCtx(r *http.Request) bool {
	noSpent, ok := r.Context().Value(ctxNoSpent).(bool)
	if !ok {
		return false
	}
	return noSpent
}

// FromToPaginationCtx will parse the query parameters for from/to values.
func (c *insightApiContext) FromToPaginationCtx(next http.Handler) http.Handler {
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
func (c *insightApiContext) ValidatePostCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentLengthString := r.Header.Get("Content-Length")
		contentLength, err := strconv.Atoi(contentLengthString)
		if err != nil {
			writeInsightError(w, "Content-Length Header must be set")
			return
		}
		// Broadcast Tx has the largest possible body.  Cap max content length
		// to c.params.MaxTxSize * 2 plus some arbitrary extra for JSON
		// encapsulation.
		maxPayload := (c.params.MaxTxSize * 2) + 50
		if contentLength > maxPayload {
			writeInsightError(w, fmt.Sprintf("Maximum Content-Length is %d", maxPayload))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Process params given in post body for an addrs endpoint
func (c *insightApiContext) PostAddrsTxsCtx(next http.Handler) http.Handler {
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
		// Successful extraction of Body JSON
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
			noScriptSig, err = req.To.Int64()
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
