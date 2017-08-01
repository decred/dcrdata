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
	wsWriteTimeout = 10 * time.Second
	pingInterval   = 12 * time.Second
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

type WebBlockInfo struct {
	BlockDataBasic *apitypes.BlockDataBasic             `json:"block"`
	StakeInfoExt   *apitypes.StakeInfoExtendedEstimates `json:"stake"`
}

// WebsocketHub and its event loop manage all websocket client connections.
// WebsocketHub is responsible for closing all connections registered with it.
// If the event loop is running, calling (*WebsocketHub).Stop() will handle it.
type WebsocketHub struct {
	sync.RWMutex
	clients         map[*hubSpoke]struct{}
	Register        chan *hubSpoke
	Unregister      chan *hubSpoke
	HubRelay        chan hubSignal
	NewBlockInfo    chan WebBlockInfo
	NewBlockSummary chan apitypes.BlockDataBasic
	NewStakeSummary chan apitypes.StakeInfoExtendedEstimates
	quitWSHandler   chan struct{}
}

type hubSignal int
type hubSpoke chan hubSignal

const (
	sigNewBlock hubSignal = iota
	sigMempoolFeeInfoUpdate
)

// Event type field for an SSE event
var eventIDs = map[hubSignal]string{
	sigNewBlock:             "newblock",
	sigMempoolFeeInfoUpdate: "mempoolsstxfeeinfo",
}

// NewWebsocketHub creates a new WebsocketHub
func NewWebsocketHub() *WebsocketHub {
	return &WebsocketHub{
		clients:       make(map[*hubSpoke]struct{}),
		Register:      make(chan *hubSpoke),
		Unregister:    make(chan *hubSpoke),
		HubRelay:      make(chan hubSignal),
		NewBlockInfo:  make(chan WebBlockInfo),
		quitWSHandler: make(chan struct{}),
	}
}

// RegisterClient registers a websocket connection with the hub.
func (wsh *WebsocketHub) RegisterClient(c *hubSpoke) {
	log.Debug("Registering new websocket client")
	wsh.Register <- c
}

// registerClient should only be called from the run loop
func (wsh *WebsocketHub) registerClient(c *hubSpoke) {
	wsh.clients[c] = struct{}{}
}

// UnregisterClient unregisters the input websocket connection via the main
// run() loop.  This call will block if the run() loop is not running.
func (wsh *WebsocketHub) UnregisterClient(c *hubSpoke) {
	wsh.Unregister <- c
}

// unregisterClient should only be called from the loop in run().
func (wsh *WebsocketHub) unregisterClient(c *hubSpoke) {
	if _, ok := wsh.clients[c]; !ok {
		// unknown client, do not close channel
		log.Warnf("unknown client")
		return
	}
	delete(wsh.clients, c)

	// Close the channel, but make sure the client didn't do it
	safeClose(*c)
}

func safeClose(cc hubSpoke) {
	select {
	case _, ok := <-cc:
		if !ok {
			log.Debug("Channel already closed!")
			return
		}
	default:
	}
	close(cc)
}

// Stop kills the run() loop and unregisteres all clients (connections).
func (wsh *WebsocketHub) Stop() {
	// end the run() loop, allowing in progress operations to complete
	wsh.quitWSHandler <- struct{}{}
	// unregister all clients
	for client := range wsh.clients {
		wsh.unregisterClient(client)
	}
}

func (wsh *WebsocketHub) run() {
	log.Info("Starting WebsocketHub run loop.")
	for {
	events:
		select {
		case hubSignal := <-wsh.HubRelay:
			switch hubSignal {
			case sigNewBlock:
				log.Infof("Signaling new block to %d clients.", len(wsh.clients))
			case sigMempoolFeeInfoUpdate:
				log.Infof("Signaling mempool info update to %d clients.", len(wsh.clients))
			default:
				log.Errorf("Unknown hub signal: %v", hubSignal)
				break events
			}
			for client := range wsh.clients {
				// signal or unregister the client
				select {
				case *client <- hubSignal:
				default:
					go wsh.unregisterClient(client)
				}
			}
		case c := <-wsh.Register:
			wsh.registerClient(c)
		case c := <-wsh.Unregister:
			wsh.unregisterClient(c)
		case _, ok := <-wsh.quitWSHandler:
			if !ok {
				log.Error("close channel already closed. This should not happen.")
				return
			}
			close(wsh.quitWSHandler)
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

func (td *WebUI) StopWebsocketHub() {
	log.Info("Stopping websocket hub.")
	td.wsHub.Stop()
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

	td.wsHub.HubRelay <- sigNewBlock

	return nil
}

func (td *WebUI) StoreMPData(data *mempool.MempoolData, timestamp time.Time) error {
	td.MPC.StoreMPData(data, timestamp)

	td.MPC.RLock()
	defer td.MPC.RUnlock()

	_, fie := td.MPC.GetFeeInfoExtra()

	td.templateDataMtx.Lock()
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
	td.templateDataMtx.Unlock()

	td.wsHub.HubRelay <- sigMempoolFeeInfoUpdate

	return nil
}

// RootPage is the http.HandlerFunc for the "/" http path
func (td *WebUI) RootPage(w http.ResponseWriter, r *http.Request) {
	td.templateDataMtx.RLock()
	// Execute template to a string instead of directly to the
	// http.ResponseWriter so that execute errors can be handled first. This can
	// avoid partial writes of the page to the client.
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
		// end the run() loop, allowing in progress operations to complete
		td.wsHub.quitWSHandler <- struct{}{}
		// unregister all clients
		for client := range td.wsHub.clients {
			td.wsHub.UnregisterClient(client)
		}

		// Create channel to signal updated data availability
		updateSig := make(hubSpoke)
		// register websocket client with our signal channel
		td.wsHub.RegisterClient(&updateSig)
		// unregister (and close signal channel) before return
		defer td.wsHub.UnregisterClient(&updateSig)

		// Ticker for a regular ping
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		// Listen for the connection closing
		notify := w.(http.CloseNotifier).CloseNotify()
		go func() {
			<-notify
			// Ensure that channel is closed so the loop in this function can exit
			//td.ewHub.UnregisterClient(&updateSig)
			safeClose(updateSig)
			log.Debug("Websocket client connection closed (CloseNotify)")
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
					td.wsHub.UnregisterClient(&updateSig)
					break loop
				}

				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))

				log.Tracef("signaling client: %p", &updateSig)

				// TODO: use a custom websocket event thinger with a custom JSON format

				// Write block data to websocket client
				td.templateDataMtx.RLock()
				var webData interface{}
				switch sig {
				case sigNewBlock:
					webData = WebBlockInfo{&td.TemplateData.BlockSummary, &td.TemplateData.StakeSummary}
				case sigMempoolFeeInfoUpdate:
					webData = &td.TemplateData.MempoolFeeInfo
				}
				err := websocket.JSON.Send(ws, webData)
				td.templateDataMtx.RUnlock()

				// If the send failed, the client is probably gone, so close the
				// connection and quit.
				if err != nil {
					log.Error(err)
					td.wsHub.UnregisterClient(&updateSig)
					return
				}
			case <-td.wsHub.quitWSHandler:
				break loop
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
