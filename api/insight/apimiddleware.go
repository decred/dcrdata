// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/decred/dcrd/chaincfg"
	apitypes "github.com/decred/dcrdata/api/types"
	m "github.com/decred/dcrdata/middleware"
	"github.com/go-chi/chi"
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

// ZeroAddrDenier https://github.com/decred/dcrdata/issues/358
func (c *insightApiContext) ZeroAddrDenier(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := chi.URLParam(r, "address")

		mainnet := &chaincfg.MainNetParams

		fmt.Println("mainnet net:", mainnet.Net.String())
		testnet := &chaincfg.TestNet2Params
		simnet := &chaincfg.SimNetParams
		cn, _ := c.nodeClient.GetCurrentNet()

		da := &apitypes.DeniedAddress{}

		if cn.String() == mainnet.Net.String() {
			apiLog.Info("got mainnet net")
			da = apitypes.NewZeroAddressDenial(mainnet)
		} else if cn.String() == testnet.Net.String() {
			da = apitypes.NewZeroAddressDenial(testnet)
		} else if cn.String() == simnet.Net.String() {
			da = apitypes.NewZeroAddressDenial(simnet)
		}

		apiLog.Info(da)

		apiLog.Info("comparing addresses:", address, da.Addr)
		if address == da.Addr {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			encoder := json.NewEncoder(w)
			if err := encoder.Encode(da); err != nil {
				apiLog.Infof("JSON encode error: %v", err)
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}
