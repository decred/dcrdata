package main

import (
	"html/template"
	"io"
	"net/http"
	"time"

	"github.com/decred/dcrd/dcrjson"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

type explorerMux struct {
	*chi.Mux
}

var explorerTemplate *template.Template
var blockTemplate *template.Template

func (c *appContext) explorerUI(w http.ResponseWriter, r *http.Request) {
	idx := c.BlockData.GetHeight()
	N := 10
	summaries := make([]*dcrjson.GetBlockHeaderVerboseResult, 0, N)
	for i := idx; i >= idx-N-1; i-- {
		summaries = append(summaries, c.BlockData.GetHeader(i))
	}
	str, err := TemplateExecToString(explorerTemplate, "explorer", struct {
		Data []*dcrjson.GetBlockHeaderVerboseResult
	}{summaries})

	if err != nil {
		http.Error(w, "template execute failure, Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (c *appContext) blockPage(w http.ResponseWriter, r *http.Request) {
	idx := getBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	str, err := TemplateExecToString(blockTemplate, "block", c.BlockData.GetHeader(idx))
	if err != nil {
		http.Error(w, "template execute failure", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func gettime(btime int64) string {
	t := time.Unix(btime, 0)
	return t.String()
}

func nextBlock(n uint32) uint32 {
	return n + 1
}
func prevBlock(n uint32) uint32 {
	return n - 1
}

func newExplorerMux(app *appContext, userRealIP bool) explorerMux {
	mux := chi.NewRouter()
	helpers := template.FuncMap{"getTime": gettime, "next": nextBlock, "prev": prevBlock}
	explorerTemplate, _ = template.New("explorer").Funcs(helpers).ParseFiles("views/explorer.tmpl")
	blockTemplate, _ = template.New("block").Funcs(helpers).ParseFiles("views/block.tmpl")
	if userRealIP {
		mux.Use(middleware.RealIP)
	}
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	corsMW := cors.Default()
	mux.Use(corsMW.Handler)
	//Basically following the same format as the apiroutes
	mux.Get("/", app.explorerUI)

	mux.Route("/block", func(r chi.Router) {
		r.Route("/best", func(rd chi.Router) {
			rd.Use(app.BlockIndexLatestCtx)
			rd.Get("/", app.blockPage)
		})

		r.Route("/{idx}", func(rd chi.Router) {
			rd.Use(BlockIndexPathCtx)
			rd.Get("/", app.blockPage)
		})
	})
	return explorerMux{mux}
}
