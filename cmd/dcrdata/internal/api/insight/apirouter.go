// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

// Package insight implements the Insight API.
package insight

import (
	"fmt"
	"net/http"

	m "github.com/decred/dcrdata/cmd/dcrdata/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ApiMux contains the struct mux
type ApiMux struct {
	*chi.Mux
}

// APIVersion is an integer value, incremented for breaking changes
const APIVersion = 0

// NewInsightAPIRouter returns a new HTTP path router, ApiMux, for the Insight
// API, app.
func NewInsightAPIRouter(app *InsightApi, useRealIP, compression bool, maxAddrs int) ApiMux {
	// chi router
	mux := chi.NewRouter()

	// Create a rate limiter struct.
	limiter := m.NewLimiter(app.ReqPerSecLimit)
	limiter.SetMessage(fmt.Sprintf(
		"You have reached the maximum request limit (%g req/s)", app.ReqPerSecLimit))

	if useRealIP {
		mux.Use(middleware.RealIP)
		// RealIP sets RemoteAddr
		limiter.SetIPLookups([]string{"RemoteAddr"})
	} else {
		limiter.SetIPLookups([]string{"X-Forwarded-For", "X-Real-IP", "RemoteAddr"})
	}

	// Put the limiter after RealIP
	mux.Use(m.Tollbooth(limiter))

	// Check for and validate the "indent" URL query. Each API request handler
	// may now access the configured indentation string if indent was specified
	// and parsed as a boolean, otherwise the empty string, from
	// m.GetIndentCtx(*http.Request).

	mux.Use(m.Indent(app.JSONIndent))

	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.StripSlashes)
	if compression {
		mux.Use(middleware.Compress(3))
	}

	mux.With(m.OriginalRequestURI).Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"/status", http.StatusSeeOther)
	})

	// Block endpoints
	mux.With(BlockDateLimitQueryCtx).Get("/blocks", app.getBlockSummaryByTime)
	mux.With(m.BlockIndexOrHashPathCtx).Get("/block/{idxorhash}", app.getBlockSummary)
	mux.With(m.BlockIndexOrHashPathCtx).Get("/block-index/{idxorhash}", app.getBlockHash)
	mux.With(m.BlockIndexOrHashPathCtx).Get("/rawblock/{idxorhash}", app.getRawBlock)

	// Transaction endpoints
	mux.With(middleware.AllowContentType("application/json"),
		app.ValidatePostCtx, m.PostBroadcastTxCtx).Post("/tx/send", app.broadcastTransactionRaw)
	mux.With(m.TransactionHashCtx).Get("/tx/{txid}", app.getTransaction)
	mux.With(m.TransactionHashCtx).Get("/rawtx/{txid}", app.getTransactionHex)
	mux.With(m.TransactionsCtx, m.PageNumCtx).Get("/txs", app.getTransactions)

	// Status and Utility
	mux.With(app.StatusInfoCtx).Get("/status", app.getStatusInfo)
	mux.Get("/sync", app.getSyncInfo)
	mux.With(NbBlocksCtx).Get("/utils/estimatefee", app.getEstimateFee)
	mux.Get("/peer", app.GetPeerStatus)

	addrs1Ctx := m.AddressPathCtxN(1)
	addrsMaxCtx := m.AddressPathCtxN(maxAddrs)

	// Addresses endpoints
	mux.Route("/addrs", func(rd chi.Router) {
		rd.Route("/{address}", func(ra chi.Router) {
			ra.Use(addrsMaxCtx, FromToPaginationCtx)
			ra.Get("/txs", app.getAddressesTxn)
			ra.Get("/utxo", app.getAddressesTxnOutput)
		})
		// POST methods
		rd.With(middleware.AllowContentType("application/json"),
			app.ValidatePostCtx, PostAddrsTxsCtxN(maxAddrs)).Post("/txs", app.getAddressesTxn)
		rd.With(middleware.AllowContentType("application/json"),
			app.ValidatePostCtx, PostAddrsUtxoCtxN(maxAddrs)).Post("/utxo", app.getAddressesTxnOutput)
	})

	// Address endpoints
	mux.Route("/addr/{address}", func(rd chi.Router) {
		rd.With(addrs1Ctx, FromToPaginationCtx, NoTxListCtx).Get("/", app.getAddressInfo)
		rd.With(addrsMaxCtx).Get("/utxo", app.getAddressesTxnOutput)
		rd.Route("/{command}", func(ra chi.Router) {
			ra.With(addrs1Ctx, AddressCommandCtx).Get("/", app.getAddressInfo)
		})
	})

	return ApiMux{mux}
}
