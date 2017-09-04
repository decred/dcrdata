package main

import (
	"html/template"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/decred/dcrutil"

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
	helpers := template.FuncMap{
		"getTime": getTime,
	}
	explorerTemplate, _ := template.New("explorer").Funcs(helpers).ParseFiles("views/explorer.tmpl", "views/extras.tmpl")

	idx := c.BlockData.GetHeight()

	end, err := strconv.Atoi(r.URL.Query().Get("end"))
	if err != nil || end == 0 || end > idx {
		end = idx
	}
	start, err := strconv.Atoi(r.URL.Query().Get("start"))
	if err != nil || start < 0 || end-start < 10 {
		start = end - 25
	} else if end-start > 200 {
		start = end - 200
	}
	type explorerData struct {
		*dcrjson.GetBlockVerboseResult
		TxCount int
	}
	summaries := make([]explorerData, 0, end-start)
	for i := end; i >= start; i-- {
		data := c.BlockData.GetBlockVerbose(i, false)
		count := len(data.Tx) + len(data.STx)
		summaries = append(summaries, explorerData{
			data,
			count,
		})
	}
	str, err := TemplateExecToString(explorerTemplate, "explorer", struct {
		Data      []explorerData
		BestBlock int
	}{
		summaries,
		idx,
	})

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

	helpers := template.FuncMap{
		"getTime":   getTime,
		"getTotal":  getTotaljs,
		"getAmount": getAmount,
		"size": func(h string) int {
			return len(h) / 2
		},
	}
	blockTemplate, _ := template.New("block").Funcs(helpers).ParseFiles("views/block.tmpl", "views/extras.tmpl")

	data := c.BlockData.GetBlockVerboseWithTxTypes(hash)
	if data == nil {
		apiLog.Errorf("Unable to get block %s", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}
	str, err := TemplateExecToString(blockTemplate, "block", data)
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

	helpers := template.FuncMap{
		"getTime":   getTime,
		"getTotal":  getTotalapi,
		"getAmount": getAmount,
	}

	txTemplate, _ := template.New("tx").Funcs(helpers).ParseFiles("views/tx.tmpl", "views/extras.tmpl")

	if !ok {
		apiLog.Trace("txid not set")
		http.Error(w, "txid not set", http.StatusInternalServerError)
		return
	}
	data := c.BlockData.GetRawTransaction(hash)
	if data == nil {
		apiLog.Errorf("Unable to get transaction %s", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}
	str, err := TemplateExecToString(txTemplate, "tx", data)
	if err != nil {
		http.Error(w, "template execute failure Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (c *appContext) addressPage(w http.ResponseWriter, r *http.Request) {
	address, ok := r.Context().Value(ctxAddress).(string)

	helpers := template.FuncMap{
		"getTime":   getTime,
		"getTotal":  getTotalapi,
		"getAmount": getAmount,
	}
	txTemplate, _ := template.New("address").Funcs(helpers).ParseFiles("views/address.tmpl", "views/extras.tmpl")

	if !ok {
		apiLog.Trace("address not set")
		http.Error(w, "address not set", http.StatusInternalServerError)
		return
	}
	data := c.BlockData.GetAddressTransactions(address, 10)
	if data == nil {
		apiLog.Errorf("Unable to get address %s", address)
		http.Error(w, http.StatusText(422), 422)
		return
	}
	str, err := TemplateExecToString(txTemplate, "address", data)
	if err != nil {
		http.Error(w, "template execute failure Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (c *appContext) search(w http.ResponseWriter, r *http.Request) {
	searchStr := r.URL.Query().Get("search")
	_, err := c.BlockData.GetBlockHeight(searchStr)
	if err == nil {
		http.Redirect(w, r, "/explorer/block/"+searchStr, http.StatusPermanentRedirect)
		return
	}
	idx, err := strconv.ParseInt(searchStr, 10, 0)
	if err == nil {
		_, err := c.BlockData.GetBlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/explorer/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
	}
	address := c.BlockData.GetAddressTransactions(searchStr, 1)
	if address != nil {
		http.Redirect(w, r, "/explorer/address/"+searchStr, http.StatusPermanentRedirect)
		return
	}
	tx := c.BlockData.DoesTxExist(searchStr)
	if tx {
		http.Redirect(w, r, "/explorer/tx/"+searchStr, http.StatusPermanentRedirect)
		return
	}
	http.Error(w, "Cannot find "+searchStr, http.StatusNotFound)
	return
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

func getAmount(v float64) dcrutil.Amount {
	amount, _ := dcrutil.NewAmount(v)
	return amount
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
			rd.Use(app.BlockHashPathOrIndexCtx)
			rd.Get("/", app.blockPage)
		})
	})

	mux.Route("/tx", func(r chi.Router) {
		r.Route("/{txid}", func(rd chi.Router) {
			rd.Use(TransactionHashCtx)
			rd.Get("/", app.txPage)
		})
	})

	mux.Route("/address", func(r chi.Router) {
		r.Route("/{address}", func(rd chi.Router) {
			rd.Use(AddressPathCtx)
			rd.Get("/", app.addressPage)
		})
	})

	mux.Get("/search", app.search)

	return explorerMux{mux}
}
