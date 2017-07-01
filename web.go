// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/decred/dcrd/chaincfg"
	"github.com/go-chi/chi"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

func TemplateExecToString(t *template.Template, name string, data interface{}) (string, error) {
	var page bytes.Buffer
	err := t.ExecuteTemplate(&page, name, data)
	return page.String(), err
}

type WebTemplateData struct {
	BlockSummary   apitypes.BlockDataBasic
	StakeSummary   apitypes.StakeInfoExtendedEstimates
	MempoolFeeInfo apitypes.MempoolTicketFeeInfo
	MempoolFees    apitypes.MempoolTicketFees
}

type WebsocketHub struct {
	sync.RWMutex
	clients         map[*websocket.Conn]chan struct{}
	Register        chan *websocket.Conn
	Unregister      chan *websocket.Conn
	NewBlockSummary chan apitypes.BlockDataBasic
	NewStakeSummary chan apitypes.StakeInfoExtendedEstimates
	quitWSHandler   chan struct{}
}

func NewWebsocketHub() *WebsocketHub {
	return &WebsocketHub{
		clients:         make(map[*websocket.Conn]chan struct{}),
		Register:        make(chan *websocket.Conn),
		Unregister:      make(chan *websocket.Conn),
		NewBlockSummary: make(chan apitypes.BlockDataBasic),
		NewStakeSummary: make(chan apitypes.StakeInfoExtendedEstimates),
		quitWSHandler:   make(chan struct{}),
	}
}

// func (wsh *WebsocketHub) RegisterClient(c *websocket.Conn) chan struct{} {
// 	wsh.Register <- c
// }

func (wsh *WebsocketHub) RegisterClient(c *websocket.Conn) chan struct{} {
	wsh.Lock()
	defer wsh.Unlock()
	clientChan := make(chan struct{})
	wsh.clients[c] = clientChan
	return clientChan
}

func (wsh *WebsocketHub) UnregisterClient(c *websocket.Conn) {
	wsh.Unregister <- c
}

func (wsh *WebsocketHub) unregisterClient(c *websocket.Conn) {
	// wsh.Lock()
	// defer wsh.Unlock()
	c.Close()
	if clientChan, ok := wsh.clients[c]; ok {
		close(clientChan)
		delete(wsh.clients, c)
	}
}

func (wsh *WebsocketHub) Stop() {
	close(wsh.quitWSHandler)
	wsh.Lock()
	for client := range wsh.clients {
		wsh.unregisterClient(client)
	}
	wsh.Unlock()
}

func (wsh *WebsocketHub) run() {
	for {
		select {
		case <-wsh.NewBlockSummary:
			wsh.RLock()
			for client, clientChan := range wsh.clients {
				select {
				case clientChan <- struct{}{}:
				default:
					go wsh.UnregisterClient(client)
				}
			}
			wsh.RUnlock()
		// case c := <- wsh.Register:
		// 	wsh.registerClient(c)
		case c := <-wsh.Unregister:
			wsh.unregisterClient(c)
		case <-wsh.quitWSHandler:
			return
		}
	}
}

type WebUI struct {
	MPC             mempool.MempoolDataCache
	TemplateData    WebTemplateData
	templateDataMtx sync.RWMutex
	wsHub           *WebsocketHub
	templ           *template.Template
	templFiles      []string
	params          *chaincfg.Params
}

func NewWebUI() *WebUI {
	fp := filepath.Join("views", "root.tmpl")
	tmpl, err := template.New("home").ParseFiles(fp)
	if err != nil {
		return nil
	}

	//var templFiles []string
	templFiles := []string{fp}

	wsh := NewWebsocketHub()
	go wsh.run()

	return &WebUI{
		wsHub:      wsh,
		templ:      tmpl,
		templFiles: templFiles,
		params:     activeChain,
	}
}

func (td *WebUI) ParseTemplates() (err error) {
	td.templ, err = template.New("home").ParseFiles(td.templFiles...)
	return
}

// See reloadsig*.go for an exported method
func (td *WebUI) reloadTemplatesSig(sig os.Signal) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, sig)

	go func() {
		for {
			sigr := <-sigChan
			log.Infof("Received %s", sig)
			if sigr == sig {
				if err := td.ParseTemplates(); err != nil {
					log.Error(err)
					continue
				}
				log.Infof("Web UI html templates reparsed.")
			}
		}
	}()
}

func (td *WebUI) Store(blockData *blockdata.BlockData) error {
	td.templateDataMtx.Lock()
	td.TemplateData.BlockSummary = blockData.ToBlockSummary()
	td.TemplateData.StakeSummary = blockData.ToStakeInfoExtendedEstimates()
	td.templateDataMtx.Unlock()

	td.templateDataMtx.RLock()
	td.wsHub.NewBlockSummary <- td.TemplateData.BlockSummary
	td.wsHub.NewStakeSummary <- td.TemplateData.StakeSummary
	td.templateDataMtx.RUnlock()
	return nil
}

func (td *WebUI) StoreMPData(data *mempool.MempoolData, timestamp time.Time) error {
	td.MPC.StoreMPData(data, timestamp)

	td.MPC.RLock()
	defer td.MPC.RUnlock()

	_, fie := td.MPC.GetFeeInfoExtra()

	td.templateDataMtx.Lock()
	defer td.templateDataMtx.Unlock()
	td.TemplateData.MempoolFeeInfo = *fie

	// LowestMineable is the lowest fee of those in the top 20 (mainnet), but
	// for the web interface, we want to interpret "lowest mineable" as the
	// lowest fee the user needs to get a new ticket purchase mined right away.
	if td.TemplateData.MempoolFeeInfo.Number < uint32(td.params.MaxFreshStakePerBlock) {
		td.TemplateData.MempoolFeeInfo.LowestMineable = 0.001
	}

	mpf := &td.TemplateData.MempoolFees
	mpf.Height, mpf.Time, _, mpf.FeeRates = td.MPC.GetFeeRates(25)
	mpf.Length = uint32(len(mpf.FeeRates))

	return nil
}

func (td *WebUI) RootPage(w http.ResponseWriter, r *http.Request) {
	td.templateDataMtx.RLock()
	//err := td.templ.Execute(w, td.TemplateData)
	str, err := TemplateExecToString(td.templ, "home", td.TemplateData)
	td.templateDataMtx.RUnlock()
	if err != nil {
		http.Error(w, "template execute failure", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (td *WebUI) WSBlockUpdater(w http.ResponseWriter, r *http.Request) {
	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		// Register this websocket connection with the hub
		updateSig := td.wsHub.RegisterClient(ws)

		ticker := time.NewTicker(pingPeriod)
		defer func() {
			ticker.Stop()
			ws.Close()
		}()

		for {
			// Wait for signal from the hub to update
			select {
			case _, ok := <-updateSig:
				if !ok {
					//ws.WriteClose(1)
					td.wsHub.UnregisterClient(ws)
					return
				}

				ws.SetWriteDeadline(time.Now().Add(writeWait))

				// Write
				td.templateDataMtx.RLock()
				err := websocket.JSON.Send(ws, td.TemplateData.BlockSummary)
				td.templateDataMtx.RUnlock()
				if err != nil {
					log.Error(err)
					td.wsHub.UnregisterClient(ws)
					return
				}
			case <-td.wsHub.quitWSHandler:
				return
			case <-ticker.C:
				// ping
				ws.SetWriteDeadline(time.Now().Add(writeWait))
				if _, err := ws.Write([]byte{}); err != nil {
					return
				}
			}
		}
	})

	wsHandler.ServeHTTP(w, r)
}

// FileServer conveniently sets up a http.FileServer handler to serve
// static files from a http.FileSystem.
func FileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	}))
}

