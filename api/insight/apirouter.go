// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
//
// This is tested against Insight API Implementation v5.3.04beta

// Package insight handles the insight api
package insight

import (
	m "github.com/decred/dcrdata/middleware"
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
// API.
func NewInsightApiRouter(app *insightApiContext, userRealIP bool) ApiMux {
	// Create a limiter struct.
	limiter := tollbooth.NewLimiter(1, nil)

	// chi router
	mux := chi.NewRouter()

	mux.Use(tollbooth_chi.LimitHandler(limiter))

	if userRealIP {
		mux.Use(middleware.RealIP)
	}

	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.StripSlashes)
	mux.Use(middleware.DefaultCompress)

	// Block endpoints
	mux.With(m.BlockDateQueryCtx).Get("/blocks", app.getBlockSummaryByTime)
	mux.With(app.BlockHashPathAndIndexCtx).Get("/block/{blockhash}", app.getBlockSummary)
	mux.With(m.BlockIndexPathCtx).Get("/block-index/{idx}", app.getBlockHash)
	mux.With(m.BlockIndexOrHashPathCtx).Get("/rawblock/{idx}", app.getRawBlock)

	// Transaction endpoints
	mux.With(middleware.AllowContentType("application/json"),
		app.ValidatePostCtx, app.PostBroadcastTxCtx).Post("/tx/send", app.broadcastTransactionRaw)
	mux.With(m.TransactionHashCtx).Get("/tx/{txid}", app.getTransaction)
	mux.With(m.TransactionHashCtx).Get("/rawtx/{txid}", app.getTransactionHex)
	mux.With(m.TransactionsCtx).Get("/txs", app.getTransactions)

	// Status
	mux.With(app.StatusInfoCtx).Get("/status", app.getStatusInfo)

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
		rd.With(m.AddressPostCtx).Post("/utxo", app.getAddressesTxnOutput)
	})

	// Address endpoints
	mux.Route("/addr/{address}", func(rd chi.Router) {
		rd.Use(m.AddressPathCtx)
		rd.With(m.PaginationCtx).Get("/", app.getAddressInfo)
		rd.Get("/utxo", app.getAddressTxnOutput)
		rd.Get("/balance", app.getAddressBalance)
		rd.Get("/totalReceived", app.getAddressTotalReceived)
		// TODO Missing unconfirmed balance implementation
		rd.Get("/unconfirmedBalance", app.getAddressUnconfirmedBalance)
		rd.Get("/totalSent", app.getAddressTotalSent)
	})

	return ApiMux{mux}
}
