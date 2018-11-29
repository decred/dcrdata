// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package api

import (
	"net/http"
	"strings"

	m "github.com/decred/dcrdata/v3/middleware"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

type apiMux struct {
	*chi.Mux
}

// NewAPIRouter creates a new HTTP request path router/mux for the given API,
// appContext.
func NewAPIRouter(app *appContext, useRealIP bool) apiMux {
	// chi router
	mux := chi.NewRouter()

	if useRealIP {
		mux.Use(middleware.RealIP)
	}
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	//mux.Use(middleware.DefaultCompress)
	//mux.Use(middleware.Compress(2))
	corsMW := cors.Default()
	mux.Use(corsMW.Handler)

	mux.Get("/", app.root)

	mux.Get("/status", app.status)
	mux.Get("/supply", app.coinSupply)

	mux.Route("/block", func(r chi.Router) {
		r.Route("/best", func(rd chi.Router) {
			rd.Use(app.BlockIndexLatestCtx)
			rd.Get("/", app.getBlockSummary) // app.getLatestBlock
			rd.Get("/height", app.currentHeight)
			rd.Get("/hash", app.getBlockHash)
			rd.Get("/header", app.getBlockHeader)
			rd.Get("/size", app.getBlockSize)
			rd.Get("/subsidy", app.blockSubsidies)
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtendedByHeight)
			rd.Route("/tx", func(rt chi.Router) {
				rt.Get("/", app.getBlockTransactions)
				rt.Get("/count", app.getBlockTransactionsCount)
			})
		})

		r.Route("/hash/{blockhash}", func(rd chi.Router) {
			rd.Use(app.BlockHashPathAndIndexCtx)
			rd.Get("/", app.getBlockSummary)
			rd.Get("/height", app.getBlockHeight)
			rd.Get("/header", app.getBlockHeader)
			rd.Get("/size", app.getBlockSize)
			rd.Get("/subsidy", app.blockSubsidies)
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtendedByHash)
			rd.Route("/tx", func(rt chi.Router) {
				rt.Get("/", app.getBlockTransactions)
				rt.Get("/count", app.getBlockTransactionsCount)
			})
		})

		r.Route("/{idx}", func(rd chi.Router) {
			rd.Use(m.BlockIndexPathCtx)
			rd.Get("/", app.getBlockSummary)
			rd.Get("/header", app.getBlockHeader)
			rd.Get("/hash", app.getBlockHash)
			rd.Get("/size", app.getBlockSize)
			rd.Get("/subsidy", app.blockSubsidies)
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtendedByHeight)
			rd.Route("/tx", func(rt chi.Router) {
				rt.Get("/", app.getBlockTransactions)
				rt.Get("/count", app.getBlockTransactionsCount)
			})
		})

		r.Route("/range/{idx0}/{idx}", func(rd chi.Router) {
			rd.Use(m.BlockIndex0PathCtx, m.BlockIndexPathCtx)
			rd.Use(middleware.Compress(1))
			rd.Get("/", app.getBlockRangeSummary)
			rd.Get("/size", app.getBlockRangeSize)
			rd.Route("/{step}", func(rs chi.Router) {
				rs.Use(m.BlockStepPathCtx)
				rs.Get("/", app.getBlockRangeSteppedSummary)
				rs.Get("/size", app.getBlockRangeSteppedSize)
			})
			// rd.Get("/header", app.getBlockHeader)
			// rd.Get("/pos", app.getBlockStakeInfoExtendedByHeight)
		})

		//r.With(middleware.DefaultCompress).Get("/raw", app.someLargeResponse)
	})

	mux.Route("/stake", func(r chi.Router) {
		r.Route("/vote", func(rd chi.Router) {
			rd.Use(app.StakeVersionLatestCtx)
			rd.Get("/info", app.getVoteInfo)
		})
		r.Route("/pool", func(rd chi.Router) {
			rd.With(app.BlockIndexLatestCtx).Get("/", app.getTicketPoolInfo)
			rd.With(app.BlockIndexLatestCtx).Get("/full", app.getTicketPool)
			rd.With(m.BlockIndexPathCtx).Get("/b/{idx}", app.getTicketPoolInfo)
			rd.With(m.BlockIndexOrHashPathCtx).Get("/b/{idxorhash}/full", app.getTicketPool)
			rd.With(m.BlockIndex0PathCtx, m.BlockIndexPathCtx).Get("/r/{idx0}/{idx}", app.getTicketPoolInfoRange)
		})
		r.Route("/diff", func(rd chi.Router) {
			rd.Get("/", app.getStakeDiffSummary)
			rd.Get("/current", app.getStakeDiffCurrent)
			rd.Get("/estimates", app.getStakeDiffEstimates)
			rd.With(m.BlockIndexPathCtx).Get("/b/{idx}", app.getStakeDiff)
			rd.With(m.BlockIndex0PathCtx, m.BlockIndexPathCtx).Get("/r/{idx0}/{idx}", app.getStakeDiffRange)
		})
	})

	mux.Route("/tx", func(r chi.Router) {
		r.Route("/", func(rt chi.Router) {
			rt.Route("/{txid}", func(rd chi.Router) {
				rd.Use(m.TransactionHashCtx)
				rd.Get("/", app.getTransaction)
				rd.Get("/trimmed", app.getDecodedTransactions)
				rd.Route("/out", func(ro chi.Router) {
					ro.Get("/", app.getTransactionOutputs)
					ro.With(m.TransactionIOIndexCtx).Get("/{txinoutindex}", app.getTransactionOutput)
				})
				rd.Route("/in", func(ri chi.Router) {
					ri.Get("/", app.getTransactionInputs)
					ri.With(m.TransactionIOIndexCtx).Get("/{txinoutindex}", app.getTransactionInput)
				})
				rd.Get("/vinfo", app.getTxVoteInfo)
			})
		})
		r.With(m.TransactionHashCtx).Get("/hex/{txid}", app.getTransactionHex)
		r.With(m.TransactionHashCtx).Get("/decoded/{txid}", app.getDecodedTx)
	})

	mux.Route("/txs", func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json"),
			m.ValidateTxnsPostCtx, m.PostTxnsCtx)
		r.Post("/", app.getTransactions)
		r.Post("/trimmed", app.getDecodedTransactions)
	})

	mux.Route("/address", func(r chi.Router) {
		r.Route("/{address}", func(rd chi.Router) {
			rd.Use(m.AddressPathCtx)
			rd.Get("/totals", app.addressTotals)
			rd.Get("/", app.getAddressTransactions)
			rd.With(m.ChartGroupingCtx).Get("/types/{chartgrouping}", app.getAddressTxTypesData)
			rd.With(m.ChartGroupingCtx).Get("/amountflow/{chartgrouping}", app.getAddressTxAmountFlowData)
			rd.With(m.ChartGroupingCtx).Get("/unspent/{chartgrouping}", app.getAddressTxUnspentAmountData)
			rd.With((middleware.Compress(1))).Get("/raw", app.getAddressTransactionsRaw)
			rd.Route("/count/{N}", func(ri chi.Router) {
				ri.Use(m.NPathCtx)
				ri.Get("/", app.getAddressTransactions)
				ri.With((middleware.Compress(1))).Get("/raw", app.getAddressTransactionsRaw)
				ri.Route("/skip/{M}", func(rj chi.Router) {
					rj.Use(m.MPathCtx)
					rj.Get("/", app.getAddressTransactions)
					rj.With((middleware.Compress(1))).Get("/raw", app.getAddressTransactionsRaw)
				})
			})
		})
	})

	mux.Route("/agenda", func(r chi.Router) {
		r.With(m.AgendIdCtx).Get("/{agendaId}", app.getAgendaData)
	})

	mux.Route("/mempool", func(r chi.Router) {
		r.Get("/", http.NotFound /*app.getMempoolOverview*/)
		// ticket purchases
		r.Route("/sstx", func(rd chi.Router) {
			rd.Get("/", app.getSSTxSummary)
			rd.Get("/fees", app.getSSTxFees)
			rd.With(m.NPathCtx).Get("/fees/{N}", app.getSSTxFees)
			rd.Get("/details", app.getSSTxDetails)
			rd.With(m.NPathCtx).Get("/details/{N}", app.getSSTxDetails)
		})
	})

	mux.Route("/chart", func(r chi.Router) {
		// Return default chart data (ticket price)
		r.Get("/", app.getTicketPriceChartData)
		r.With(m.ChartTypeCtx).Get("/{charttype}", app.ChartTypeData)
	})

	mux.Route("/ticketpool", func(r chi.Router) {
		r.Get("/", app.getTicketPoolByDate)
		r.With(m.TicketPoolCtx).Get("/bydate/{tp}", app.getTicketPoolByDate)
		r.Get("/charts", app.getTicketPoolCharts)
	})

	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, r.URL.RequestURI()+" ain't no country I've ever heard of! (404)", http.StatusNotFound)
	})

	// if cfg.PrintAPIDirectory {
	// 	var buf bytes.Buffer
	// 	json.Indent(&buf, []byte(docgen.JSONRoutesDoc(mux)), "", "\t")
	// 	buf.WriteTo(os.Stdout)

	// 	fmt.Println(docgen.MarkdownRoutesDoc(mux, docgen.MarkdownOpts{
	// 		ProjectPath: "github.com/decred/dcrdata/v3",
	// 		Intro:       "dcrdata HTTP router directory",
	// 	}))
	// 	return
	// }

	// mux.HandleFunc("/directory", APIDirectory)
	// mux.With(apiDocs(mux)).HandleFunc("/directory", APIDirectory)

	var listRoutePatterns func(routes []chi.Route) []string
	listRoutePatterns = func(routes []chi.Route) []string {
		patterns := []string{}
		for _, rt := range routes {
			patterns = append(patterns, strings.Replace(rt.Pattern, "/*", "", -1))
			if rt.SubRoutes == nil {
				continue
			}
			for _, pt := range listRoutePatterns(rt.SubRoutes.Routes()) {
				patterns = append(patterns, strings.Replace(rt.Pattern+pt, "/*", "", -1))
			}
		}
		return patterns
	}

	mux.HandleFunc("/list", app.writeJSONHandlerFunc(listRoutePatterns(mux.Routes())))

	return apiMux{mux}
}

func (mux *apiMux) ListenAndServeProto(listen, proto string) {
	apiLog.Infof("Now serving on %s://%v/", proto, listen)
	if proto == "https" {
		go http.ListenAndServeTLS(listen, "dcrdata.cert", "dcrdata.key", mux)
	}
	go http.ListenAndServe(listen, mux)
}
