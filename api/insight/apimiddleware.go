// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"net/http"

	m "github.com/decred/dcrdata/middleware"
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
