package main

import (
	"net/http"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

type apiMux struct {
	*chi.Mux
}

// APIVersion is an integer value, incremented for breaking changes
const APIVersion = 0

func newAPIRouter(app *appContext, userRealIP bool) apiMux {
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
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtended)
			rd.Route("/tx", func(rt chi.Router) {
				rt.Get("/", app.getBlockTransactions)
			})
		})

		r.Route("/hash/{blockhash}", func(rd chi.Router) {
			rd.Use(app.BlockHashPathAndIndexCtx)
			rd.Get("/", app.getBlockSummary)
			rd.Get("/height", app.getBlockHeight)
			rd.Get("/header", app.getBlockHeader)
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtended)
			rd.Route("/tx", func(rt chi.Router) {
				rt.Get("/", app.getBlockTransactions)
			})
		})

		r.Route("/{idx}", func(rd chi.Router) {
			rd.Use(BlockIndexPathCtx)
			rd.Get("/", app.getBlockSummary)
			rd.Get("/header", app.getBlockHeader)
			rd.Get("/hash", app.getBlockHash)
			rd.With((middleware.Compress(1))).Get("/verbose", app.getBlockVerbose)
			rd.Get("/pos", app.getBlockStakeInfoExtended)
			rd.Route("/tx", func(rt chi.Router) {
				rt.Get("/", app.getBlockTransactions)
			})
		})

		r.Route("/range/{idx0}/{idx}", func(rd chi.Router) {
			rd.Use(BlockIndex0PathCtx, BlockIndexPathCtx)
			rd.Use(middleware.Compress(1))
			rd.Get("/", app.getBlockRangeSummary)
			// rd.Get("/header", app.getBlockHeader)
			// rd.Get("/pos", app.getBlockStakeInfoExtended)
		})

		//r.With(middleware.DefaultCompress).Get("/raw", app.someLargeResponse)
	})

	mux.Route("/stake", func(r chi.Router) {
		r.Route("/pool", func(rd chi.Router) {
			rd.With(app.BlockIndexLatestCtx).Get("/", app.getTicketPoolInfo)
			rd.With(BlockIndexPathCtx).Get("/b/{idx}", app.getTicketPoolInfo)
			rd.With(BlockIndex0PathCtx, BlockIndexPathCtx).Get("/r/{idx0}/{idx}", app.getTicketPoolInfoRange)
		})
		r.Route("/diff", func(rd chi.Router) {
			rd.Get("/", app.getStakeDiffSummary)
			rd.Get("/current", app.getStakeDiffCurrent)
			rd.Get("/estimates", app.getStakeDiffEstimates)
			rd.With(BlockIndexPathCtx).Get("/b/{idx}", app.getStakeDiff)
			rd.With(BlockIndex0PathCtx, BlockIndexPathCtx).Get("/r/{idx0}/{idx}", app.getStakeDiffRange)
		})
	})

	mux.Route("/mempool", func(r chi.Router) {
		r.Get("/", http.NotFound /*app.getMempoolOverview*/)
		// ticket purchases
		r.Route("/sstx", func(rd chi.Router) {
			rd.Get("/", app.getSSTxSummary)
			rd.Get("/fees", app.getSSTxFees)
			rd.With(NPathCtx).Get("/fees/{N}", app.getSSTxFees)
			rd.Get("/details", app.getSSTxDetails)
			rd.With(NPathCtx).Get("/details/{N}", app.getSSTxDetails)
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
	// 		ProjectPath: "github.com/dcrdata/dcrdata",
	// 		Intro:       "dcrdata HTTP router directory",
	// 	}))
	// 	return
	// }

	mux.HandleFunc("/directory", APIDirectory)
	mux.With(apiDocs(mux)).HandleFunc("/directory", APIDirectory)

	var listRoutePatterns func(routes []chi.Route) []string
	listRoutePatterns = func(routes []chi.Route) []string {
		patterns := []string{}
		for _, rt := range routes {
			patterns = append(patterns, rt.Pattern)
			if rt.SubRoutes == nil {
				continue
			}
			for _, pt := range listRoutePatterns(rt.SubRoutes.Routes()) {
				patterns = append(patterns, rt.Pattern+pt)
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
