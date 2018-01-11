// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package api

import (
	"net/http"
	"strings"

	m "github.com/decred/dcrdata/middleware"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

type apiMux struct {
	*chi.Mux
}

// APIVersion is an integer value, incremented for breaking changes
const APIVersion = 0

func NewAPIRouter(app *appContext, userRealIP bool) apiMux {
	// chi router
	mux := chi.NewRouter()

	if userRealIP {
		mux.Use(middleware.RealIP)
	}
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	//mux.Use(middleware.DefaultCompress)
	//mux.Use(middleware.Compress(2))
	corsMW := cors.Default()
	mux.Use(corsMW.Handler)

	mux.Get("/", app.root)

	mux.HandleFunc("/status", app.status)

	mux.Route("/block", func(r chi.Router) {
		r.Route("/best", func(rd chi.Router) {
			rd.Use(app.BlockIndexLatestCtx)
			rd.Get("/", app.getBlockSummary) // app.getLatestBlock
			rd.Get("/height", app.currentHeight)
			rd.Get("/hash", app.getBlockHash)
			rd.Get("/header", app.getBlockHeader)
			rd.Get("/size", app.getBlockSize)
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtended)
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
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtended)
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
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtended)
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
			// rd.Get("/pos", app.getBlockStakeInfoExtended)
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

	mux.Route("/address", func(r chi.Router) {
		r.Route("/{address}", func(rd chi.Router) {
			rd.Use(m.AddressPathCtx)
			rd.Get("/", app.getAddressTransactions)
			rd.With((middleware.Compress(1))).Get("/raw", app.getAddressTransactionsRaw)
			rd.Route("/count/{N}", func(ri chi.Router) {
				ri.Use(m.NPathCtx)
				ri.Get("/", app.getAddressTransactions)
				ri.With((middleware.Compress(1))).Get("/raw", app.getAddressTransactionsRaw)
			})
		})
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

	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, r.URL.RequestURI()+" ain't no country I've ever heard of! (404)", http.StatusNotFound)
	})

	// if cfg.PrintAPIDirectory {
	// 	var buf bytes.Buffer
	// 	json.Indent(&buf, []byte(docgen.JSONRoutesDoc(mux)), "", "\t")
	// 	buf.WriteTo(os.Stdout)

	// 	fmt.Println(docgen.MarkdownRoutesDoc(mux, docgen.MarkdownOpts{
	// 		ProjectPath: "github.com/decred/dcrdata",
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
