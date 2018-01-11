// Package insight handles the insight api
//
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
//
// This is tested against Insight API Implementation v5.3.04beta

package insight

import (
	m "github.com/decred/dcrdata/middleware"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
)

// ApiMux contains the struct mux
type ApiMux struct {
	*chi.Mux
}

// APIVersion is an integer value, incremented for breaking changes
const APIVersion = 0

// NewInsightApiRouter returns the mux instance for the insight api
func NewInsightApiRouter(app *insightApiContext, userRealIP bool) ApiMux {
	// chi router
	mux := chi.NewRouter()

	if userRealIP {
		mux.Use(middleware.RealIP)
	}

	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)

	mux.With(m.TransactionHashCtx).Get("/tx/{txid}", app.getTransaction)
	mux.With(m.TransactionHashCtx).Get("/rawtx/{txid}", app.getTransactionHex)
	mux.With(app.BlockHashPathAndIndexCtx).Get("/block/{blockhash}", app.getBlockSummary)
	mux.With(m.BlockIndexPathCtx).Get("/block-index/{idx}", app.getBlockHash)
	// TODO Missing implementation for rawblock
	mux.With(m.BlockIndexPathCtx).Get("/rawblock/{idx}", app.getRawBlock)

	mux.With(m.RawTransactionCtx).Post("/tx/send", app.broadcastTransactionRaw)
	mux.With(m.AddressPathCtx).Get("/addr/{address}/utxo", app.getAddressTxnOutput)
	mux.With(m.AddressPathCtx).Get("/addrs/{address}/utxo", app.getAddressesTxnOutput)
	mux.With(m.AddressPostCtx).Post("/addrs/utxo", app.getAddressesTxnOutput)

	mux.With(m.TransactionsCtx).Get("/txs", app.getTransactions)
	mux.With(m.PaginationCtx).With(m.AddressPathCtx).Get("/addrs/{address}/txs", app.getAddressesTxn)
	mux.With(m.PaginationCtx).With(m.AddressPostCtx).Post("/addrs/txs", app.getAddressesTxn)

	mux.With(m.BlockDateQueryCtx).Get("/blocks", app.getBlockSummaryByTime)
	mux.With(app.StatusInfoCtx).Get("/status", app.getStatusInfo)

	mux.Route("/addr/{address}", func(rd chi.Router) {
		rd.Use(m.AddressPathCtx)
		rd.With(m.PaginationCtx).Get("/", app.getAddressInfo)
		rd.Get("/balance", app.getAddressBalance)
		rd.Get("/totalReceived", app.getAddressTotalReceived)
		// TODO Missing unconfirmed balance implementation
		rd.Get("/unconfirmedBalance", app.getAddressUnconfirmedBalance)
		rd.Get("/totalSent", app.getAddressTotalSent)
	})

	return ApiMux{mux}
}
