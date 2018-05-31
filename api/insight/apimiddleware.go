// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"

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
	ctxAddress
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

// AddressPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {address} into the request context.
func (c *insightApiContext) AddressPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := chi.URLParam(r, "address")
		ctx := context.WithValue(r.Context(), ctxAddress, address)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetAddressCtx retrieves the ctxAddress data from the request context. If not
// set, the return value is an empty string.
func (c *insightApiContext) GetAddressCtx(r *http.Request) string {
	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		apiLog.Trace("address not set")
		return ""
	}
	return address
}

// GetToCtx retrieves the ctxTo data ("to") from the request context. If not
// set, the return value ok is false.
func (c *insightApiContext) GetToCtx(r *http.Request) (int64, bool) {
	to, ok := r.Context().Value(ctxTo).(int)
	if !ok {
		apiLog.Errorf("to param not set")
		return int64(0), false
	}
	return int64(to), true
}

// GetFromCtx retrieves the ctxFrom data ("from") from the request context.
// If not set, the return value ok is false.
func (c *insightApiContext) GetFromCtx(r *http.Request) (int64, bool) {
	from, ok := r.Context().Value(ctxFrom).(int)
	if !ok {
		apiLog.Errorf("from param not set")
		return int64(0), false
	}
	return int64(from), true
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
	noNoSpent, ok := r.Context().Value(ctxNoSpent).(bool)
	if !ok {
		return false
	}
	return noNoSpent
}

// PaginationCtx will parse the query parameters for from/to values.
func (c *insightApiContext) PaginationCtx(next http.Handler) http.Handler {
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

// Process params given in post body for an addrs endpoint
func (c *insightApiContext) PostAddrsTxsCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.InsightMultiAddrsTx
		var from, to, noAsm, noScriptSig, noSpent int64
		body, err := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			apiLog.Errorf("error reading JSON message: %v", err)
			return
		}
		err = json.Unmarshal(body, &req)
		if err != nil {
			apiLog.Errorf("Failed to parse request: %v", err)
			return
		}
		// Successful extraction of Body JSON
		ctx := context.WithValue(r.Context(), ctxAddress, req.Addresses)
		if req.From != "" {
			from, err = req.From.Int64()
			if err != nil {
				apiLog.Errorf("Error converting 'from' to Int.  Expecting Integer.")
			} else {
				ctx = context.WithValue(ctx, ctxFrom, int(from))
			}
		}
		if req.To != "" {
			to, err = req.To.Int64()
			if err != nil {
				apiLog.Errorf("Error converting 'to' to Int.  Expecting Integer.")
			} else {
				ctx = context.WithValue(ctx, ctxTo, int(to))
			}
		}
		if req.NoAsm != "" {
			noAsm, err = req.NoAsm.Int64()
			if err != nil {
				apiLog.Errorf("Error converting 'noAsm' to Int.  Expecting 0 or 1.")
			} else {
				if noAsm == 0 {
					ctx = context.WithValue(ctx, ctxNoAsm, false)
				} else {
					ctx = context.WithValue(ctx, ctxNoAsm, true)
				}
			}
		}
		if req.NoScriptSig != "" {
			noScriptSig, err = req.To.Int64()
			if err != nil {
				apiLog.Errorf("Error converting 'noScriptSig' to Int.  Expecting 0 or 1.")
			} else {
				if noScriptSig == 0 {
					ctx = context.WithValue(ctx, ctxNoScriptSig, false)
				} else {
					ctx = context.WithValue(ctx, ctxNoScriptSig, true)
				}
			}
		}

		if req.NoSpent != "" {
			noSpent, err = req.NoSpent.Int64()
			if err != nil {
				apiLog.Errorf("Error converting 'noSpent' to Int.  Expecting 0 or 1.")
			} else {
				if noSpent == 0 {
					ctx = context.WithValue(ctx, ctxNoSpent, false)
				} else {
					ctx = context.WithValue(ctx, ctxNoSpent, true)
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
