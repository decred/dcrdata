package main

import (
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrutil"
	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

const (
	rootTemplateIndex int = iota
	blockTemplateIndex
	txTemplateIndex
	addressTemplateIndex
	maxExplorerRows = 2000
	minExplorerRows = 20
	addressRows     = 2000
)

func voutTotal(vouts []dcrjson.Vout) (total float64) {
	for i := range vouts {
		total += vouts[i].Value
	}
	return
}

type explorerUI struct {
	Mux             *chi.Mux
	app             *appContext
	templates       []*template.Template
	templateFiles   map[string]string
	templateHelpers template.FuncMap
}

func (exp *explorerUI) root(w http.ResponseWriter, r *http.Request) {
	idx := exp.app.BlockData.GetHeight()

	height, err := strconv.Atoi(r.URL.Query().Get("height"))
	if err != nil || height > idx {
		height = idx
	}

	rows, err := strconv.Atoi(r.URL.Query().Get("rows"))
	if err != nil || rows > maxExplorerRows || rows < minExplorerRows || height-rows < 0 {
		rows = minExplorerRows
	}

	summaries := make([]*dcrjson.GetBlockVerboseResult, 0, rows)
	for i := height; i > height-rows; i-- {
		data := exp.app.BlockData.GetBlockVerbose(i, false)
		summaries = append(summaries, data)
	}
	str, err := TemplateExecToString(exp.templates[rootTemplateIndex], "explorer", struct {
		Data      []*dcrjson.GetBlockVerboseResult
		BestBlock int
	}{
		summaries,
		idx,
	})

	if err != nil {
		apiLog.Errorf("Template execute failure: %v", err)
		http.Redirect(w, r, "/error", http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (exp *explorerUI) blockPage(w http.ResponseWriter, r *http.Request) {
	hash := exp.app.getBlockHashCtx(r)
	height := exp.app.getBlockHeightCtx(r)
	if height == -1 {
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}

	data := exp.app.BlockData.GetBlockVerboseWithStakeTxDetails(hash)
	if data == nil {
		apiLog.Errorf("Unable to get block %s", hash)
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}
	sort.Slice(data.RawTx, func(i, j int) bool {
		return voutTotal(data.RawTx[i].Vout) > voutTotal(data.RawTx[j].Vout)
	})
	sort.Slice(data.Tickets, func(i, j int) bool {
		return voutTotal(data.Tickets[i].Vout) > voutTotal(data.Tickets[j].Vout)
	})
	sort.Slice(data.Votes, func(i, j int) bool {
		return voutTotal(data.Votes[i].Vout) > voutTotal(data.Votes[j].Vout)
	})
	sort.Slice(data.Revs, func(i, j int) bool {
		return voutTotal(data.Revs[i].Vout) > voutTotal(data.Revs[j].Vout)
	})
	str, err := TemplateExecToString(exp.templates[blockTemplateIndex], "block", data)
	if err != nil {
		apiLog.Errorf("Template execute failure: %v", err)
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (exp *explorerUI) txPage(w http.ResponseWriter, r *http.Request) {
	// attempt to get tx hash string from URL path
	hash, ok := r.Context().Value(ctxTxHash).(string)
	if !ok {
		apiLog.Trace("txid not set")
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}

	// Get transaction information with addresses exctracted from pkScripts of
	// previous outpoints redeemed by the transaction.
	tx, prevOutAddresses, txtype, msgTx := exp.app.BlockData.GetRawTransactionWithPrevOutAddresses(hash)
	if tx == nil {
		apiLog.Errorf("Unable to get transaction %s", hash)
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}

	// If the transaction is a vote, extract the vote info
	var vinfo *apitypes.VoteInfo
	if isVote, _ := stake.IsSSGen(msgTx); isVote {
		var err error
		vinfo, err = exp.app.BlockData.GetVoteInfo(hash)
		if err != nil {
			apiLog.Errorf("Unable to get vote info for transaction %s", hash)
			http.Error(w, "Unable to get vote info. Is tx "+hash+" a vote?", 422)
			return
		}
	}

	// Execute template with anon struct containing the above information.
	txSuppl := struct {
		*apitypes.Tx
		VinAddrs [][]string
		Type     string
		VoteInfo *apitypes.VoteInfo
	}{
		tx, prevOutAddresses, txtype, vinfo,
	}
	str, err := TemplateExecToString(exp.templates[txTemplateIndex], "tx", txSuppl)
	if err != nil {
		apiLog.Errorf("Template execute failure: %v", err)
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (exp *explorerUI) addressPage(w http.ResponseWriter, r *http.Request) {
	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		apiLog.Trace("address not set")
		http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
		return
	}
	data := exp.app.BlockData.GetAddressTransactions(address, addressRows)
	if data == nil {
		apiLog.Errorf("Unable to get address %s", address)
		http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
		return
	}
	str, err := TemplateExecToString(exp.templates[addressTemplateIndex], "address", data)
	if err != nil {
		apiLog.Errorf("Template execute failure: %v", err)
		http.Redirect(w, r, "/error", http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// search implements a primitive search algorithm by checking if the value in
// question is a block index, block hash, address hash or transaction hash and
// redirects to the appropriate page or displays an error
func (exp *explorerUI) search(w http.ResponseWriter, r *http.Request) {
	searchStr, ok := r.Context().Value(ctxSearch).(string)
	if !ok {
		apiLog.Trace("search parameter missing")
		http.Redirect(w, r, "/error/", http.StatusTemporaryRedirect)
		return
	}

	// Attempt to get a block hash by calling GetBlockHash to see if the value
	// is a block index and then redirect to the block page if it is
	idx, err := strconv.ParseInt(searchStr, 10, 0)
	if err == nil {
		_, err := exp.app.BlockData.GetBlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/explorer/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
	}

	// Call GetAddressTransactions to see if the value is an address hash and
	// then redirect to the address page if it is
	address := exp.app.BlockData.GetAddressTransactions(searchStr, 1)
	if address != nil {
		http.Redirect(w, r, "/explorer/address/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Check if the value is a valid hash
	if _, err := chainhash.NewHashFromStr(searchStr); err != nil {
		http.Redirect(w, r, "/error/"+searchStr, http.StatusTemporaryRedirect)
		return
	}

	// Attempt to get a block index by calling GetBlockHeight to see if the
	// value is a block hash and then redirect to the block page if it is
	_, err = exp.app.BlockData.GetBlockHeight(searchStr)
	if err == nil {
		http.Redirect(w, r, "/explorer/block/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Call GetRawTransaction to see if the value is a transaction hash and then
	// redirect to the tx page if it is
	tx := exp.app.BlockData.GetRawTransaction(searchStr)
	if tx != nil {
		http.Redirect(w, r, "/explorer/tx/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Display an error since searchStr is not a block index, block hash, address hash or transaction hash
	http.Redirect(w, r, "/error/"+searchStr, http.StatusTemporaryRedirect)
	return
}

func (exp *explorerUI) reloadTemplates() error {
	explorerTemplate, err := template.New("explorer").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["explorer"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	blockTemplate, err := template.New("block").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["block"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	txTemplate, err := template.New("tx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["tx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	addressTemplate, err := template.New("address").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["address"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	exp.templates[rootTemplateIndex] = explorerTemplate
	exp.templates[blockTemplateIndex] = blockTemplate
	exp.templates[txTemplateIndex] = txTemplate
	exp.templates[addressTemplateIndex] = addressTemplate

	return nil
}

// See reloadsig*.go for an exported method
func (exp *explorerUI) reloadTemplatesSig(sig os.Signal) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, sig)

	go func() {
		for {
			sigr := <-sigChan
			log.Infof("Received %s", sig)
			if sigr == sig {
				if err := exp.reloadTemplates(); err != nil {
					log.Error(err)
					continue
				}
				log.Infof("Explorer UI html templates reparsed.")
			}
		}
	}()
}

func newExplorerMux(app *appContext, userRealIP bool) *explorerUI {
	exp := new(explorerUI)
	exp.Mux = chi.NewRouter()
	exp.app = app

	if userRealIP {
		exp.Mux.Use(middleware.RealIP)
	}

	exp.templateFiles = make(map[string]string)
	exp.templateFiles["explorer"] = filepath.Join("views", "explorer.tmpl")
	exp.templateFiles["block"] = filepath.Join("views", "block.tmpl")
	exp.templateFiles["address"] = filepath.Join("views", "address.tmpl")
	exp.templateFiles["tx"] = filepath.Join("views", "tx.tmpl")
	exp.templateFiles["extras"] = filepath.Join("views", "extras.tmpl")

	exp.templateHelpers = template.FuncMap{
		"uint32Comma": func(v uint32) string {
			i64 := int64(v)
			return humanize.Comma(i64)
		},
		"int64Comma": func(v int64) string {
			t := humanize.Comma(v)
			return t
		},
		"float64Commaf": func(v float64) string {
			return humanize.Commaf(v)
		},
		"formatBytes": func(v int32) string {
			i64 := uint64(v)
			return humanize.Bytes(i64)
		},
		"formatBytesInt": func(v int) string {
			i64 := uint64(v)
			return humanize.Bytes(i64)
		},
		"timezone": func() string {
			t, _ := time.Now().Zone()
			return t
		},
		"getTime": func(btime int64) string {
			t := time.Unix(btime, 0)
			return t.Format("1/_2/06 15:04:05")
		},
		"getTotalFromBlock": func(vout []dcrjson.Vout) float64 {
			total := 0.0
			for _, v := range vout {
				total = total + v.Value
			}
			return total
		},
		"getTotalFromTx": func(vout []apitypes.Vout) float64 {
			total := 0.0
			for _, v := range vout {
				total = total + v.Value
			}
			return total
		},
		"getAmount": func(v float64) dcrutil.Amount {
			amount, _ := dcrutil.NewAmount(v)
			return amount
		},
		"size": func(h string) int {
			return len(h) / 2
		},
		"totalSentInBlock": func(block *apitypes.BlockDataWithTxType) string {
			var total float64
			for _, i := range block.RawTx {
				for _, j := range i.Vout {
					total = total + j.Value
				}
			}
			for _, i := range block.RawSTx {
				for _, j := range i.Vout {
					total = total + j.Value
				}
			}
			return humanize.Commaf(total) + " DCR"
		},
	}

	exp.templates = make([]*template.Template, 0, 4)

	explorerTemplate, err := template.New("explorer").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["explorer"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		apiLog.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, explorerTemplate)

	blockTemplate, err := template.New("block").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["block"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		apiLog.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, blockTemplate)

	txTemplate, err := template.New("tx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["tx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		apiLog.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, txTemplate)

	addressTemplate, err := template.New("address").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["address"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		apiLog.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, addressTemplate)

	exp.addRoutes()

	return exp
}

func (exp *explorerUI) addRoutes() {
	exp.Mux.Use(middleware.Logger)
	exp.Mux.Use(middleware.Recoverer)
	corsMW := cors.Default()
	exp.Mux.Use(corsMW.Handler)
	//Basically following the same format as the apiroutes
	exp.Mux.Get("/", exp.root)

	exp.Mux.Route("/block", func(r chi.Router) {
		r.Route("/best", func(rd chi.Router) {
			rd.Use(exp.app.BlockHashLatestCtx)
			rd.Get("/", exp.blockPage)
		})

		r.Route("/{blockhash}", func(rd chi.Router) {
			rd.Use(exp.app.BlockHashPathOrIndexCtx)
			rd.Get("/", exp.blockPage)
		})
	})

	exp.Mux.Route("/tx", func(r chi.Router) {
		r.Route("/{txid}", func(rd chi.Router) {
			rd.Use(TransactionHashCtx)
			rd.Get("/", exp.txPage)
		})
	})

	exp.Mux.Route("/address", func(r chi.Router) {
		r.Route("/{address}", func(rd chi.Router) {
			rd.Use(AddressPathCtx)
			rd.Get("/", exp.addressPage)
		})
	})

	exp.Mux.With(SearchPathCtx).Get("/search/{search}", exp.search)
}
