package main

import (
	"html/template"
	"io"
	"net/http"
	"time"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/dcrjson"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

type explorerMux struct {
	*chi.Mux
}

func (c *appContext) explorerUI(w http.ResponseWriter, r *http.Request) {

	helpers := template.FuncMap{"getTime": getTime}
	explorerTemplate, _ := template.New("explorer").Funcs(helpers).ParseFiles("views/explorer.tmpl", "views/extras.tmpl")

	idx := c.BlockData.GetHeight()
	N := 20
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
	hash := c.getBlockHashCtx(r)

	helpers := template.FuncMap{"getTime": getTime, "getTotal": getTotaljs, "len": func(s string) int { return len(s) / 2 }}
	blockTemplate, _ := template.New("block").Funcs(helpers).ParseFiles("views/block.tmpl", "views/extras.tmpl")

	str, err := TemplateExecToString(blockTemplate, "block", c.BlockData.GetBlockVerboseByHash(hash, true))
	if err != nil {
		http.Error(w, "template execute failure Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (c *appContext) txPage(w http.ResponseWriter, r *http.Request) {
	hash, ok := r.Context().Value(ctxTxHash).(string)

	helpers := template.FuncMap{"getTime": getTime, "getTotal": getTotalapi, "len": func(s string) int { return len(s) / 2 }}
	txTemplate, _ := template.New("tx").Funcs(helpers).ParseFiles("views/tx.tmpl", "views/extras.tmpl")

	if !ok {
		apiLog.Trace("txid not set")
		http.Error(w, "txid not set", http.StatusInternalServerError)
		return
	}

	str, err := TemplateExecToString(txTemplate, "tx", c.BlockData.GetRawTransaction(hash))
	if err != nil {
		http.Error(w, "template execute failure Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func getTime(btime int64) string {
	t := time.Unix(btime, 0)
	return t.String()
}

func getTotaljs(vout []dcrjson.Vout) float64 {
	total := 0.0
	for _, v := range vout {
		total = total + v.Value
	}
	return total
}
func getTotalapi(vout []apitypes.Vout) float64 {
	total := 0.0
	for _, v := range vout {
		total = total + v.Value
	}
	return total
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
			rd.Use(app.BlockHashLatestCtx)
			rd.Get("/", app.blockPage)
		})

		r.Route("/{blockhash}", func(rd chi.Router) {
			rd.Use(app.BlockHashPathAndIndexCtx)
			rd.Get("/", app.blockPage)
		})
	})

	mux.Route("/tx", func(r chi.Router) {
		r.Route("/{txid}", func(rd chi.Router) {
			rd.Use(TransactionHashCtx)
			rd.Get("/", app.txPage)
		})
	})
	return explorerMux{mux}
}
