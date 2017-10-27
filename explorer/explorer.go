// Package explorer handles the explorer subsystem for displaying the explorer pages
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
package explorer

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/decred/dcrd/chaincfg/chainhash"
	humanize "github.com/dustin/go-humanize"
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
	AddressRows     = 500
)

// ExplorerDataSource implements an interface for collecting data for the explorer pages
type explorerDataSource interface {
	GetExplorerBlock(hash string) *BlockInfo
	GetExplorerBlocks(start int, end int) []*BlockBasic
	GetBlockHeight(hash string) (int64, error)
	GetBlockHash(idx int64) (string, error)
	GetExplorerTx(txid string) *TxInfo
	GetExplorerAddress(address string, count int) *AddressInfo
	GetHeight() int
}

type explorerDataSourceAlt interface {
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	AddressHistory(address string) ([]*dbtypes.AddressRow, error)
	FillAddressTransactions(addrInfo *AddressInfo) error
}

type explorerUI struct {
	Mux             *chi.Mux
	blockData       explorerDataSource
	explorerSource  explorerDataSourceAlt
	liteMode        bool
	templates       []*template.Template
	templateFiles   map[string]string
	templateHelpers template.FuncMap
}

func (exp *explorerUI) root(w http.ResponseWriter, r *http.Request) {
	idx := exp.blockData.GetHeight()

	height, err := strconv.Atoi(r.URL.Query().Get("height"))
	if err != nil || height > idx {
		height = idx
	}

	rows, err := strconv.Atoi(r.URL.Query().Get("rows"))
	if err != nil || rows > maxExplorerRows || rows < minExplorerRows || height-rows < 0 {
		rows = minExplorerRows
	}
	summaries := exp.blockData.GetExplorerBlocks(height, height-rows)
	if summaries == nil {
		log.Errorf("Unable to get blocks: height=%d&rows=%d", height, rows)
		http.Redirect(w, r, "/error", http.StatusTemporaryRedirect)
		return
	}

	str, err := templateExecToString(exp.templates[rootTemplateIndex], "explorer", struct {
		Data      []*BlockBasic
		BestBlock int
	}{
		summaries,
		idx,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		http.Redirect(w, r, "/error", http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (exp *explorerUI) blockPage(w http.ResponseWriter, r *http.Request) {
	hash := getBlockHashCtx(r)

	data := exp.blockData.GetExplorerBlock(hash)
	if data == nil {
		log.Errorf("Unable to get block %s", hash)
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}

	str, err := templateExecToString(exp.templates[blockTemplateIndex], "block", data)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
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
		log.Trace("txid not set")
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}
	tx := exp.blockData.GetExplorerTx(hash)
	if tx == nil {
		log.Errorf("Unable to get transaction %s", hash)
		http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
		return
	}
	if !exp.liteMode {
		// For each output of this transaction, look up any spending transactions,
		// and the index of the spending transaction input.
		spendingTxHashes, spendingTxVinInds, voutInds, err := exp.explorerSource.SpendingTransactions(hash)
		if err != nil {
			log.Errorf("Unable to retrieve spending transactions for %s: %v", hash, err)
			http.Redirect(w, r, "/error/"+hash, http.StatusTemporaryRedirect)
			return
		}
		for i, vout := range voutInds {
			if int(vout) >= len(tx.SpendingTxns) {
				log.Errorf("Invalid spending transaction data (%s:%d)", hash, vout)
				continue
			}
			tx.SpendingTxns[vout] = TxInID{
				Hash:  spendingTxHashes[i],
				Index: spendingTxVinInds[i],
			}
		}
	}
	str, err := templateExecToString(exp.templates[txTemplateIndex], "tx", tx)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
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
		log.Trace("address not set")
		http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
		return
	}

	var addrData *AddressInfo
	if exp.liteMode {
		addrData = exp.blockData.GetExplorerAddress(address, AddressRows)
		if addrData == nil {
			log.Errorf("Unable to get address %s", address)
			http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
			return
		}
	} else {
		// Get addresses table rows for the address
		addrHist, err := exp.explorerSource.AddressHistory(address)
		if err != nil {
			log.Errorf("Unable to get address %s history: %v", address, err)
			http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
			return
		}

		// Generate AddressInfo template from the address table rows
		addrData = ReduceAddressHistory(addrHist)
		// still need []*AddressTx filled out and NumUnconfirmed

		// Query database for transaction details
		err = exp.explorerSource.FillAddressTransactions(addrData)
		if err != nil {
			log.Errorf("Unable to fill address %s transactions: %v", address, err)
			http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
			return
		}
	}
	str, err := templateExecToString(exp.templates[addressTemplateIndex],
		"address", addrData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
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
		log.Trace("search parameter missing")
		http.Redirect(w, r, "/error/", http.StatusTemporaryRedirect)
		return
	}

	// Attempt to get a block hash by calling GetBlockHash to see if the value
	// is a block index and then redirect to the block page if it is
	idx, err := strconv.ParseInt(searchStr, 10, 0)
	if err == nil {
		_, err := exp.blockData.GetBlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/explorer/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
	}

	// Call GetExplorerAddress to see if the value is an address hash and
	// then redirect to the address page if it is
	address := exp.blockData.GetExplorerAddress(searchStr, 1)
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
	_, err = exp.blockData.GetBlockHeight(searchStr)
	if err == nil {
		http.Redirect(w, r, "/explorer/block/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Call GetExplorerTx to see if the value is a transaction hash and then
	// redirect to the tx page if it is
	tx := exp.blockData.GetExplorerTx(searchStr)
	if tx != nil {
		http.Redirect(w, r, "/explorer/tx/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Display an error since searchStr is not a block index, block hash, address hash or transaction hash
	http.Redirect(w, r, "/error/"+searchStr, http.StatusTemporaryRedirect)
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

// New returns an initialized instance of explorerUI
func New(dataSource explorerDataSource, primaryDataSource explorerDataSourceAlt,
	useRealIP bool) *explorerUI {
	exp := new(explorerUI)
	exp.Mux = chi.NewRouter()
	exp.blockData = dataSource
	exp.explorerSource = primaryDataSource
	// explorerDataSourceAlt is an interface that could have a value of pointer
	// type, and if either is nil this means lite mode.
	if exp.explorerSource == nil || reflect.ValueOf(exp.explorerSource).IsNil() {
		exp.liteMode = true
	}

	if useRealIP {
		exp.Mux.Use(middleware.RealIP)
	}

	exp.templateFiles = make(map[string]string)
	exp.templateFiles["explorer"] = filepath.Join("views", "explorer.tmpl")
	exp.templateFiles["block"] = filepath.Join("views", "block.tmpl")
	exp.templateFiles["tx"] = filepath.Join("views", "tx.tmpl")
	exp.templateFiles["extras"] = filepath.Join("views", "extras.tmpl")
	exp.templateFiles["address"] = filepath.Join("views", "address.tmpl")
	exp.templateHelpers = template.FuncMap{
		"add": func(a int64, b int64) int64 {
			val := a + b
			return val
		},
		"subtract": func(a int64, b int64) int64 {
			val := a - b
			return val
		},
		"timezone": func() string {
			t, _ := time.Now().Zone()
			return t
		},
		"percentage": func(a int64, b int64) float64 {
			p := (float64(a) / float64(b)) * 100
			return p
		},
		"float64AsDecimalParts": func(v float64, useCommas bool) []string {
			clipped := fmt.Sprintf("%.8f", v)
			oldLength := len(clipped)
			clipped = strings.TrimRight(clipped, "0")
			trailingZeros := strings.Repeat("0", oldLength-len(clipped))
			valueChunks := strings.Split(clipped, ".")
			integer := valueChunks[0]
			var dec string
			if len(valueChunks) == 2 {
				dec = valueChunks[1]
			} else {
				dec = ""
				log.Errorf("float64AsDecimalParts has no decimal value. Input: %v", v)
			}
			if useCommas {
				integerAsInt64, err := strconv.ParseInt(integer, 10, 64)
				if err != nil {
					log.Errorf("float64AsDecimalParts comma formatting failed. Input: %v Error: %v", v, err.Error())
					integer = "ERROR"
					dec = "VALUE"
					zeros := ""
					return []string{integer, dec, zeros}
				}
				integer = humanize.Comma(integerAsInt64)
			}
			return []string{integer, dec, trailingZeros}
		},
		"amountAsDecimalParts": func(v int64, useCommas bool) []string {
			amt := strconv.FormatInt(v, 10)
			if len(amt) <= 8 {
				dec := strings.TrimRight(amt, "0")
				trailingZeros := strings.Repeat("0", len(amt)-len(dec))
				leadingZeros := strings.Repeat("0", 8-len(amt))
				return []string{"0", leadingZeros + dec, trailingZeros}
			}
			integer := amt[:len(amt)-8]
			if useCommas {
				integerAsInt64, err := strconv.ParseInt(integer, 10, 64)
				if err != nil {
					log.Errorf("amountAsDecimalParts comma formatting failed. Input: %v Error: %v", v, err.Error())
					integer = "ERROR"
					dec := "VALUE"
					zeros := ""
					return []string{integer, dec, zeros}
				}
				integer = humanize.Comma(integerAsInt64)
			}
			dec := strings.TrimRight(amt[len(amt)-8:], "0")
			zeros := strings.Repeat("0", 8-len(dec))
			return []string{integer, dec, zeros}
		},
	}

	exp.templates = make([]*template.Template, 0, 4)

	explorerTemplate, err := template.New("explorer").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["explorer"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, explorerTemplate)

	blockTemplate, err := template.New("block").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["block"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, blockTemplate)

	txTemplate, err := template.New("tx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["tx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, txTemplate)
	addrTemplate, err := template.New("address").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["address"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, addrTemplate)

	exp.addRoutes()

	return exp
}

func (exp *explorerUI) addRoutes() {
	exp.Mux.Use(middleware.Logger)
	exp.Mux.Use(middleware.Recoverer)
	corsMW := cors.Default()
	exp.Mux.Use(corsMW.Handler)

	exp.Mux.Get("/", exp.root)

	exp.Mux.Route("/block", func(r chi.Router) {
		r.Route("/{blockhash}", func(rd chi.Router) {
			rd.Use(exp.blockHashPathOrIndexCtx)
			rd.Get("/", exp.blockPage)
		})
	})

	exp.Mux.Route("/tx", func(r chi.Router) {
		r.Route("/{txid}", func(rd chi.Router) {
			rd.Use(transactionHashCtx)
			rd.Get("/", exp.txPage)
		})
	})
	exp.Mux.Route("/address", func(r chi.Router) {
		r.Route("/{address}", func(rd chi.Router) {
			rd.Use(addressPathCtx)
			rd.Get("/", exp.addressPage)
		})
	})

	exp.Mux.With(searchPathCtx).Get("/search/{search}", exp.search)
}
