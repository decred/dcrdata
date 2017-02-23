package main

import (
	"net/http"

	"github.com/pressly/chi"
	//"github.com/pressly/chi/docgen"
	"github.com/pressly/chi/middleware"
)

type apiMux struct {
	*chi.Mux
}

func newAPIRouter(app *appContext) apiMux {
	// chi router
	mux := chi.NewRouter()

	mux.Use(middleware.Logger)
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Recoverer)
	//mux.Use(middleware.DefaultCompress)
	//mux.Use(middleware.Compress(2))

	mux.HandleFunc("/", app.root)

	mux.With(app.StatusCtx).HandleFunc("/status", app.status)

	mux.Route("/block", func(r chi.Router) {
		r.Use(app.StatusCtx)

		r.Route("/best", func(rd chi.Router) {
			rd.Use(app.BlockIndexLatestCtx)
			rd.Get("/", app.getBlockSummary) // app.getLatestBlock
			rd.Get("/height", app.currentHeight)
			rd.Get("/header", app.getBlockHeader)
			rd.Get("/pos", app.getBlockStakeInfoExtended)
		})

		r.Route("/:idx", func(rd chi.Router) {
			rd.Use(BlockIndexPathCtx)
			rd.Get("/", app.getBlockSummary)
			rd.Get("/header", app.getBlockHeader)
			rd.Get("/pos", app.getBlockStakeInfoExtended)
		})

		r.Route("/range/:idx0/:idx", func(rd chi.Router) {
			rd.Use(BlockIndex0PathCtx, BlockIndexPathCtx)
			rd.Get("/", app.getBlockRangeSummary)
			// rd.Get("/header", app.getBlockHeader)
			// rd.Get("/pos", app.getBlockStakeInfoExtended)
		})

		//r.With(middleware.DefaultCompress).Get("/raw", app.someLargeResponse)
	})

	mux.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./favicon.ico")
	})

	//mux.FileServer("/browse", http.Dir(context.RootDataFolder))

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

	return apiMux{mux}
}

func (mux *apiMux) ListenAndServeProto(listen, proto string) {
	apiLog.Infof("Now serving on %s://%v/", proto, listen)
	if proto == "https" {
		go http.ListenAndServeTLS(listen, "dcrdata.cert", "dcrdata.key", mux)
	}
	go http.ListenAndServe(listen, mux)
}
