// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
//
// This is tested against Insight API Implementation v5.3.04beta

// Package insight handles the insight api
package insight

import (
	m "github.com/decred/dcrdata/v4/middleware"
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
func NewInsightApiRouter(app *insightApiContext, useRealIP bool) ApiMux {
	// chi router
	mux := chi.NewRouter()

	// Create a rate limiter struct.
	reqPerSecLimit := 10.0
	limiter := tollbooth.NewLimiter(reqPerSecLimit, nil)

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
	mux.Use(middleware.DefaultCompress)

	// Block endpoints
	mux.With(app.BlockDateLimitQueryCtx).Get("/blocks", app.getBlockSummaryByTime)
	mux.With(app.BlockIndexOrHashPathCtx).Get("/block/{idxorhash}", app.getBlockSummary)
	mux.With(app.BlockIndexOrHashPathCtx).Get("/block-index/{idxorhash}", app.getBlockHash)
	mux.With(app.BlockIndexOrHashPathCtx).Get("/rawblock/{idxorhash}", app.getRawBlock)

	// Transaction endpoints
	mux.With(middleware.AllowContentType("application/json"),
		app.ValidatePostCtx, app.PostBroadcastTxCtx).Post("/tx/send", app.broadcastTransactionRaw)
	mux.With(m.TransactionHashCtx).Get("/tx/{txid}", app.getTransaction)
	mux.With(m.TransactionHashCtx).Get("/rawtx/{txid}", app.getTransactionHex)
	mux.With(m.TransactionsCtx).Get("/txs", app.getTransactions)

	// Status and Utility
	mux.With(app.StatusInfoCtx).Get("/status", app.getStatusInfo)
	mux.Get("/sync", app.getSyncInfo)
	mux.With(app.NbBlocksCtx).Get("/utils/estimatefee", app.getEstimateFee)
	mux.Get("/peer", app.GetPeerStatus)

	// Addresses endpoints
	mux.Route("/addrs", func(rd chi.Router) {
		rd.Route("/{address}", func(ra chi.Router) {
			ra.Use(m.AddressPathCtx, app.FromToPaginationCtx)
			ra.Get("/txs", app.getAddressesTxn)
			ra.Get("/utxo", app.getAddressesTxnOutput)
		})
		// POST methods
		rd.With(middleware.AllowContentType("application/json"),
			app.ValidatePostCtx, app.PostAddrsTxsCtx).Post("/txs", app.getAddressesTxn)
		rd.With(middleware.AllowContentType("application/json"),
			app.ValidatePostCtx, app.PostAddrsUtxoCtx).Post("/utxo", app.getAddressesTxnOutput)
	})

	// Address endpoints
	mux.Route("/addr/{address}", func(rd chi.Router) {
		rd.Use(m.AddressPathCtx)
		rd.With(app.FromToPaginationCtx, app.NoTxListCtx).Get("/", app.getAddressInfo)
		rd.Get("/utxo", app.getAddressesTxnOutput)
		rd.Route("/{command}", func(ra chi.Router) {
			ra.With(app.AddressCommandCtx).Get("/", app.getAddressInfo)
		})
	})

	return ApiMux{mux}
}
