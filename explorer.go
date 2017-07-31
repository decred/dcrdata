package main

import (
	"html/template"
	"io"
	"net/http"
	"path/filepath"

	"github.com/decred/dcrd/dcrjson"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

type explorerMux struct {
	*chi.Mux
}

type explorer struct {
	app          *appContext
	page         string
	pageFunction map[string]func()
}

func (c *appContext) explorerUI(w http.ResponseWriter, r *http.Request) {
	templateFile := filepath.Join("views", "explorer.tmpl")
	idx := c.BlockData.GetHeight()
	N := 10
	summaries := make([]*dcrjson.GetBlockHeaderVerboseResult, 0, N)

	for i := idx; i >= idx-N-1; i-- {
		summaries = append(summaries, c.BlockData.GetHeader(i))
	}

	tmpl, _ := template.New("explorer").ParseFiles(templateFile)
	str, err := TemplateExecToString(tmpl, "explorer", struct {
		Data []*dcrjson.GetBlockHeaderVerboseResult
	}{summaries})

	if err != nil {
		http.Error(w, "template execute failure", http.StatusInternalServerError)
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
	templateFile := filepath.Join("views", "block.tmpl")
	tmpl, _ := template.New("block").ParseFiles(templateFile)
	str, err := TemplateExecToString(tmpl, "block", c.BlockData.GetHeader(idx))
	if err != nil {
		http.Error(w, "template execute failure", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func newExplorer(c *appContext) explorer {
	return explorer{app: c}
}

func newExplorerMux(app *appContext, userRealIP bool) explorerMux {
	mux := chi.NewRouter()

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
