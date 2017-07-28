package main

import (
	"html/template"
	"io"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

type explorerMux struct {
	*chi.Mux
}

func (c *appContext) explorerUI(w http.ResponseWriter, r *http.Request) {
	fp := filepath.Join("views", "explorer.tmpl")
	tmpl, err := template.New("home").ParseFiles(fp)
	str, err := TemplateExecToString(tmpl, "explorer", c.getBlockRangeSummary)
	if err != nil {
		http.Error(w, "template execute failure", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func newExplorer(app *appContext, userRealIP bool) explorerMux {
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

	mux.Get("/", app.explorerUI)
	return explorerMux{mux}
}
