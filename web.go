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
	wsWriteTimeout  = 10 * time.Second
	pingInterval    = 30 * time.Second
)

// TemplateExecToString executes the input template with given name using the
// supplied data, and writes the result into a string. If the template fails to
// execute, a non-nil error will be returned. Check it before writing to the
// client, otherwise you might as well execute directly into your response
// writer instead of the internal buffer of this function.
func TemplateExecToString(t *template.Template, name string, data interface{}) (string, error) {
	var page bytes.Buffer
	err := t.ExecuteTemplate(&page, name, data)
	return page.String(), err
}

// WebTemplateData holds all of the data structures used to update the web page.
type WebTemplateData struct {
	BlockSummary   apitypes.BlockDataBasic
	StakeSummary   apitypes.StakeInfoExtendedEstimates
	MempoolFeeInfo apitypes.MempoolTicketFeeInfo
	MempoolFees    apitypes.MempoolTicketFees
}

// WebsocketHub and its event loop manage all websocket client connections.
// WebsocketHub is responsible for closing all connections registered with it.
// If the event loop is running, calling (*WebsocketHub).Stop() will handle it.
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

// RegisterClient registers a websocket connection with the hub. It does not use
// the run() loop, and instead locks the client map. This is not ideal (TODO).
func (wsh *WebsocketHub) RegisterClient(c *websocket.Conn) chan struct{} {
	wsh.Lock()
	defer wsh.Unlock()
	clientChan := make(chan struct{})
	wsh.clients[c] = clientChan
	return clientChan
}

// UnregisterClient unregisters the input websocket connection via the main
// run() loop.  This call will block if the run() loop is not running.
func (wsh *WebsocketHub) UnregisterClient(c *websocket.Conn) {
	wsh.Unregister <- c
}

// unregisterClient should only be called from the loop in run().
func (wsh *WebsocketHub) unregisterClient(c *websocket.Conn) {
	wsh.Lock()
	defer wsh.Unlock()
	c.Close()
	if clientChan, ok := wsh.clients[c]; ok {
		close(clientChan)
		delete(wsh.clients, c)
	}
}

// Stop kills the run() loop and unregisteres all clients (connections).
func (wsh *WebsocketHub) Stop() {
	// end the run() loop
	close(wsh.quitWSHandler)
	// unregister all clients
	for client := range wsh.clients {
		wsh.unregisterClient(client)
	}
}

func (wsh *WebsocketHub) run() {
	log.Info("Starting WebsocketHub run loop.")
	for {
		select {
		case <-wsh.NewBlockSummary:
			log.Info("Signaling to clients.")
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
	wsHub           *WebsocketHub
	MPC             mempool.MempoolDataCache
	TemplateData    WebTemplateData
	templateDataMtx sync.RWMutex
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

// ParseTemplates parses all the template files, updating the *html/template.Template.
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

// Store extracts the block and stake data from the input BlockData and stores
// it in the HTML template data. Store also signals the WebsocketHub of the
// updated data.
func (td *WebUI) Store(blockData *blockdata.BlockData) error {
	td.templateDataMtx.Lock()
	td.TemplateData.BlockSummary = blockData.ToBlockSummary()
	td.TemplateData.StakeSummary = blockData.ToStakeInfoExtendedEstimates()
	td.templateDataMtx.Unlock()

	td.templateDataMtx.RLock()
	td.wsHub.NewBlockSummary <- td.TemplateData.BlockSummary
	//td.wsHub.NewStakeSummary <- td.TemplateData.StakeSummary
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

// WSBlockUpdater handles requests on the websocket path (e.g. /ws). The wrapped
// websocket.Handler registers the connection with the WebsocketHub, which
// provides an update channel, and starts an update loop. The loop writes the
// current block data when it receives a signal on the update channel. The run()
// loop must be running to receive signals on the update channel. The update
// loop quits in the following situations: when the quitWSHandler channel is
// closed, when the update channel is closed, or when a write on the
// websocket.Conn fails.
func (td *WebUI) WSBlockUpdater(w http.ResponseWriter, r *http.Request) {
	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		// Register this websocket connection with the hub
		updateSig := td.wsHub.RegisterClient(ws)

		// ping ticker
		ticker := time.NewTicker(pingInterval)
		defer func() {
			ticker.Stop()
			ws.Close()
		}()

		for {
			// Wait for signal from the hub to update
			select {
			case _, ok := <-updateSig:
				// Check if the updaate channel was closed
				if !ok {
					//ws.WriteClose(1)
					td.wsHub.UnregisterClient(ws)
					return
				}

				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))

				// Write block data to websocket client
				td.templateDataMtx.RLock()
				err := websocket.JSON.Send(ws, td.TemplateData.BlockSummary)
				td.templateDataMtx.RUnlock()
				// If the send failed, the client is probably gone, so close the
				// connection and quit.
				if err != nil {
					log.Error(err)
					td.wsHub.UnregisterClient(ws)
					return
				}
			case <-td.wsHub.quitWSHandler:
				return
			// ping fired
			case <-ticker.C:
				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				// ping
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

