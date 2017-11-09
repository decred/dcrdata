// Package explorer handles the block explorer subsystem for generating the
// explorer pages.
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"bytes"
	"encoding/json"
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
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/decred/dcrd/chaincfg/chainhash"
	humanize "github.com/dustin/go-humanize"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
	"golang.org/x/net/websocket"
)

const (
	rootTemplateIndex int = iota
	blockTemplateIndex
	txTemplateIndex
	addressTemplateIndex
)

const (
	maxExplorerRows          = 2000
	minExplorerRows          = 20
	defaultAddressRows int64 = 20
	maxAddressRows     int64 = 1000
)

// explorerDataSourceLite implements an interface for collecting data for the
// explorer pages
type explorerDataSourceLite interface {
	GetExplorerBlock(hash string) *BlockInfo
	GetExplorerBlocks(start int, end int) []*BlockBasic
	GetBlockHeight(hash string) (int64, error)
	GetBlockHash(idx int64) (string, error)
	GetExplorerTx(txid string) *TxInfo
	GetExplorerAddress(address string, count, offset int64) *AddressInfo
	GetHeight() int
}

// explorerDataSource implements extra data retrieval functions that require a
// faster solution than RPC.
type explorerDataSource interface {
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	AddressHistory(address string, N, offset int64) ([]*dbtypes.AddressRow, *AddressBalance, error)
	FillAddressTransactions(addrInfo *AddressInfo) error
}

type explorerUI struct {
	Mux             *chi.Mux
	blockData       explorerDataSourceLite
	explorerSource  explorerDataSource
	liteMode        bool
	templates       []*template.Template
	templateFiles   map[string]string
	templateHelpers template.FuncMap
	wsHub           *WebsocketHub
	NewBlockDataMtx sync.RWMutex
	NewBlockData    BlockBasic
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

func (exp *explorerUI) rootWebsocket(w http.ResponseWriter, r *http.Request) {
	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		// Create channel to signal updated data availability
		updateSig := make(hubSpoke)
		// register websocket client with our signal channel
		exp.wsHub.RegisterClient(&updateSig)
		// unregister (and close signal channel) before return
		defer exp.wsHub.UnregisterClient(&updateSig)

		// Ticker for a regular ping
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		go func() {
			for range ticker.C {
				exp.wsHub.HubRelay <- sigPingAndUserCount
			}
		}()

	loop:
		for {
			// Wait for signal from the hub to update
			select {
			case sig, ok := <-updateSig:
				// Check if the update channel was closed. Either the websocket
				// hub will do it after unregistering the client, or forcibly in
				// response to (http.CloseNotifier).CloseNotify() and only then if
				// the hub has somehow lost track of the client.
				if !ok {
					//ws.WriteClose(1)
					exp.wsHub.UnregisterClient(&updateSig)
					break loop
				}

				if _, ok = eventIDs[sig]; !ok {
					break loop
				}

				log.Tracef("signaling client: %p", &updateSig)
				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))

				// Write block data to websocket client
				exp.NewBlockDataMtx.RLock()
				webData := WebSocketMessage{
					EventId: eventIDs[sig],
				}
				buff := new(bytes.Buffer)
				enc := json.NewEncoder(buff)
				switch sig {
				case sigNewBlock:
					enc.Encode(WebsocketBlock{exp.NewBlockData})
					webData.Messsage = buff.String()
				case sigPingAndUserCount:
					// ping and send user count
					webData.Messsage = strconv.Itoa(exp.wsHub.NumClients())
				}

				err := websocket.JSON.Send(ws, webData)
				exp.NewBlockDataMtx.RUnlock()
				if err != nil {
					log.Debugf("Failed to encode WebSocketMessage %v: %v", sig, err)
					// If the send failed, the client is probably gone, so close
					// the connection and quit.
					return
				}
			case <-exp.wsHub.quitWSHandler:
				break loop
			}
		}
	})

	wsHandler.ServeHTTP(w, r)
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
	// Get the address URL parameter, which should be set in the request context
	// by the addressPathCtx middleware.
	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		log.Trace("address not set")
		http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
		return
	}

	// Number of outputs for the address to query the database for. The URL
	// query parameter "n" is used to specify the limit (e.g. "?n=20").
	limitN, err := strconv.ParseInt(r.URL.Query().Get("n"), 10, 64)
	if err != nil || limitN < 0 {
		limitN = defaultAddressRows
	} else if limitN > maxAddressRows {
		log.Warnf("addressPage: requested up to %d address rows, "+
			"limiting to %d", limitN, maxAddressRows)
		limitN = maxAddressRows
	}

	// Number of outputs to skip (OFFSET in database query). For UX reasons, the
	// "start" URL query parameter is used.
	offsetAddrOuts, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	if err != nil || offsetAddrOuts < 0 {
		offsetAddrOuts = 0
	}

	var addrData *AddressInfo
	if exp.liteMode {
		addrData = exp.blockData.GetExplorerAddress(address, limitN, offsetAddrOuts)
		if addrData == nil {
			log.Errorf("Unable to get address %s", address)
			http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
			return
		}
	} else {
		// Get addresses table rows for the address
		addrHist, balance, errH := exp.explorerSource.AddressHistory(
			address, limitN, offsetAddrOuts)
		if errH != nil {
			log.Errorf("Unable to get address %s history: %v", address, errH)
			http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
			return
		}

		// Generate AddressInfo skeleton from the address table rows
		addrData = ReduceAddressHistory(addrHist)
		if addrData == nil {
			log.Debugf("empty address history (%s): n=%d&start=%d", address, limitN, offsetAddrOuts)
			http.Redirect(w, r, "/error/"+address, http.StatusTemporaryRedirect)
			return
		}
		addrData.Limit, addrData.Offset = limitN, offsetAddrOuts
		addrData.KnownFundingTxns = balance.NumSpent + balance.NumUnspent
		addrData.Balance = balance
		addrData.Path = r.URL.Path
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
		_, err = exp.blockData.GetBlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/explorer/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
	}

	// Call GetExplorerAddress to see if the value is an address hash and
	// then redirect to the address page if it is
	address := exp.blockData.GetExplorerAddress(searchStr, 1, 0)
	if address != nil {
		http.Redirect(w, r, "/explorer/address/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Check if the value is a valid hash
	if _, err = chainhash.NewHashFromStr(searchStr); err != nil {
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

// StopWebsocketHub stops the websocket hub
func (exp *explorerUI) StopWebsocketHub() {
	log.Info("Stopping websocket hub.")
	exp.wsHub.Stop()
}

// New returns an initialized instance of explorerUI
func New(dataSource explorerDataSourceLite, primaryDataSource explorerDataSource,
	useRealIP bool) *explorerUI {
	exp := new(explorerUI)
	exp.Mux = chi.NewRouter()
	exp.blockData = dataSource
	exp.explorerSource = primaryDataSource
	// explorerDataSource is an interface that could have a value of pointer
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
		"int64Comma": func(v int64) string {
			return humanize.Comma(v)
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

	wsh := NewWebsocketHub()
	go wsh.run()

	exp.wsHub = wsh

	return exp
}

func (exp *explorerUI) Store(blockData *blockdata.BlockData) error {
	exp.NewBlockDataMtx.Lock()
	bData := blockData.ToBlockExplorerSummary()
	newBlockData := BlockBasic{
		Height:         int64(bData.Height),
		Voters:         bData.Voters,
		FreshStake:     bData.FreshStake,
		Size:           int32(bData.Size),
		Transactions:   bData.TxLen,
		BlockTime:      bData.Time,
		FormattedTime:  bData.FormattedTime,
		FormattedBytes: humanize.Bytes(uint64(bData.Size)),
		Revocations:    uint32(bData.Revocations),
	}
	exp.NewBlockData = newBlockData
	exp.NewBlockDataMtx.Unlock()

	exp.wsHub.HubRelay <- sigNewBlock

	log.Debugf("Got new block %d", newBlockData.Height)

	return nil
}

func (exp *explorerUI) addRoutes() {
	exp.Mux.Use(middleware.Logger)
	exp.Mux.Use(middleware.Recoverer)
	corsMW := cors.Default()
	exp.Mux.Use(corsMW.Handler)

	exp.Mux.Get("/", exp.root)
	exp.Mux.Get("/ws", exp.rootWebsocket)

	exp.Mux.Route("/block", func(r chi.Router) {
		r.Route("/{blockhash}", func(rd chi.Router) {
			rd.Use(exp.blockHashPathOrIndexCtx)
			rd.Get("/", exp.blockPage)
			rd.Get("/ws", exp.rootWebsocket)
		})
	})

	exp.Mux.Route("/tx", func(r chi.Router) {
		r.Route("/{txid}", func(rd chi.Router) {
			rd.Use(transactionHashCtx)
			rd.Get("/", exp.txPage)
			rd.Get("/ws", exp.rootWebsocket)
		})
	})
	exp.Mux.Route("/address", func(r chi.Router) {
		r.Route("/{address}", func(rd chi.Router) {
			rd.Use(addressPathCtx)
			rd.Get("/", exp.addressPage)
			rd.Get("/ws", exp.rootWebsocket)
		})
	})

	exp.Mux.With(searchPathCtx).Get("/search/{search}", exp.search)
}
