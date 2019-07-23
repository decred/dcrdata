// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

// Package insight implements the Insight API.
package insight

import (
	"net/http"

	m "github.com/decred/dcrdata/middleware/v3"
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth_chi"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
)

// ApiMux contains the struct mux
type ApiMux struct {
	*chi.Mux
}

// APIVersion is an integer value, incremented for breaking changes
const APIVersion = 0

// NewInsightApiRouter returns a new HTTP path router, ApiMux, for the Insight
// API, app.
func NewInsightApiRouter(app *InsightApi, useRealIP, compression bool) ApiMux {
	// chi router
	mux := chi.NewRouter()

	// Create a rate limiter struct.
	limiter := tollbooth.NewLimiter(app.ReqPerSecLimit, nil)

	if useRealIP {
		mux.Use(middleware.RealIP)
		// RealIP sets RemoteAddr
		limiter.SetIPLookups([]string{"RemoteAddr"})
	} else {
		limiter.SetIPLookups([]string{"X-Forwarded-For", "X-Real-IP", "RemoteAddr"})
	}

	// Put the limiter after RealIP
	mux.Use(tollbooth_chi.LimitHandler(limiter))

	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.StripSlashes)
	if compression {
		mux.Use(middleware.NewCompressor(3).Handler())
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

	// Addresses endpoints
	mux.Route("/addrs", func(rd chi.Router) {
		rd.Route("/{address}", func(ra chi.Router) {
			ra.Use(m.AddressPathCtx, FromToPaginationCtx)
			ra.Get("/txs", app.getAddressesTxn)
			ra.Get("/utxo", app.getAddressesTxnOutput)
		})
		// POST methods
		rd.With(middleware.AllowContentType("application/json"),
			app.ValidatePostCtx, PostAddrsTxsCtx).Post("/txs", app.getAddressesTxn)
		rd.With(middleware.AllowContentType("application/json"),
			app.ValidatePostCtx, PostAddrsUtxoCtx).Post("/utxo", app.getAddressesTxnOutput)
	})

	// Address endpoints
	mux.Route("/addr/{address}", func(rd chi.Router) {
		rd.Use(m.AddressPathCtx)
		rd.With(FromToPaginationCtx, NoTxListCtx).Get("/", app.getAddressInfo)
		rd.Get("/utxo", app.getAddressesTxnOutput)
		rd.Route("/{command}", func(ra chi.Router) {
			ra.With(AddressCommandCtx).Get("/", app.getAddressInfo)
		})
	})

	return ApiMux{mux}
}
