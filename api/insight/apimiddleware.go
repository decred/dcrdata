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

	apitypes "github.com/decred/dcrdata/api/types"
	m "github.com/decred/dcrdata/middleware"
)

type contextKey int

const (
	ctxRawHexTx contextKey = iota
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
