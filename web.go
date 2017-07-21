// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fmt"
	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/decred/dcrd/chaincfg"
	"github.com/go-chi/chi"
)

const (
	wsWriteTimeout = 10 * time.Second
	pingInterval   = 30 * time.Second
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

// EventStreamHub and its event loop manage all event-stream client connections.
type EventStreamHub struct {
	sync.RWMutex
	clients         map[*chan struct{}]struct{}
	Register        chan *chan struct{}
	Unregister      chan *chan struct{}
	NewBlockSummary chan apitypes.BlockDataBasic
	NewStakeSummary chan apitypes.StakeInfoExtendedEstimates
	quitESHandler   chan struct{}
}

// NewEventStreamHub creates a new EventStreamHub
func NewEventStreamHub() *EventStreamHub {
	return &EventStreamHub{
		clients:         make(map[*chan struct{}]struct{}),
		Register:        make(chan *chan struct{}),
		Unregister:      make(chan *chan struct{}),
		NewBlockSummary: make(chan apitypes.BlockDataBasic),
		NewStakeSummary: make(chan apitypes.StakeInfoExtendedEstimates),
		quitESHandler:   make(chan struct{}),
	}
}

// RegisterClient registers a event-stream writer
func (esh *EventStreamHub) RegisterClient(cc *chan struct{}) {
	log.Debug("Registering new event stream client")
	esh.Register <- cc
}

// registerClient should only be called from the run loop
func (esh *EventStreamHub) registerClient(cc *chan struct{}) {
	esh.clients[cc] = struct{}{}
}

// UnregisterClient unregisters the input websocket connection via the main
// run() loop.  This call will block if the run() loop is not running.
func (esh *EventStreamHub) UnregisterClient(cc *chan struct{}) {
	esh.Unregister <- cc
}

// unregisterClient should only be called from the loop in run().
func (esh *EventStreamHub) unregisterClient(cc *chan struct{}) {
	if _, ok := esh.clients[cc]; !ok {
		// unknown client, do not close channel
		log.Warnf("unknown client")
		return
	}
	delete(esh.clients, cc)

	// Close the channel, but make sure the client didn't do it
	safeClose(*cc)
}

func safeClose(cc chan struct{}) {
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
func (esh *EventStreamHub) Stop() {
	// end the run() loop, allowing in progress operations to complete
	esh.quitESHandler <- struct{}{}
	// unregister all clients
	for client := range esh.clients {
		esh.unregisterClient(client)
	}
}

func (esh *EventStreamHub) run() {
	log.Info("Starting EventStreamHub run loop.")
	for {
		select {
		case <-esh.NewBlockSummary:
			log.Infof("Signaling to %d clients.", len(esh.clients))
			for client := range esh.clients {
				// signal or unregister the client
				select {
				case *client <- struct{}{}:
				default:
					esh.unregisterClient(client)
				}
			}
		case c := <-esh.Register:
			esh.registerClient(c)
		case c := <-esh.Unregister:
			esh.unregisterClient(c)
		case _, ok := <-esh.quitESHandler:
			if !ok {
				log.Error("close channel already closed. This should not happen.")
				return
			}
			close(esh.quitESHandler)
			return
		}
	}
}

type WebUI struct {
	esHub           *EventStreamHub
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

	esh := NewEventStreamHub()
	go esh.run()

	return &WebUI{
		esHub:      esh,
		templ:      tmpl,
		templFiles: templFiles,
		params:     activeChain,
	}
}

func (td *WebUI) StopEventStreamHub() {
	log.Info("Stopping event stream hub.")
	td.esHub.Stop()
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
// it in the HTML template data. Store also signals the EventStreamHub of the
// updated data.
func (td *WebUI) Store(blockData *blockdata.BlockData) error {
	td.templateDataMtx.Lock()
	td.TemplateData.BlockSummary = blockData.ToBlockSummary()
	td.TemplateData.StakeSummary = blockData.ToStakeInfoExtendedEstimates()
	td.templateDataMtx.Unlock()

	td.templateDataMtx.RLock()
	td.esHub.NewBlockSummary <- td.TemplateData.BlockSummary
	//td.esHub.NewStakeSummary <- td.TemplateData.StakeSummary
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

// ESBlockUpdater handles requests on the event stream path (e.g. /sse).
// Register the connection with the EventStreamHub, which provides an update
// channel, and starts an update loop. The loop writes the current block data
// when it receives a signal on the update channel. The hub's run() loop must be
// running to receive signals on the update channel. The update loop quits in
// the following situations: when the quitESHandler channel is closed, when the
// update channel is closed, when a write on the http.ResponseWriter fails, or
// when a closed connection triggers CloseNotify().
func (td *WebUI) ESBlockUpdater(w http.ResponseWriter, r *http.Request) {
	// Ensure event streaming is supported (sorry, IE and Edge)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Event streaming unsupported", http.StatusInternalServerError)
		log.Warnf("connetion does not support HTML5 event streaming")
		return
	}

	// Create channel to signal updated data availability
	updateSig := make(chan struct{})
	// register event stream client with our signal channel
	td.esHub.RegisterClient(&updateSig)
	// unregister (and close signal channel) before return
	defer td.esHub.UnregisterClient(&updateSig)

	// Ticker for a regular ping
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	// Listen for the connection closing
	notify := w.(http.CloseNotifier).CloseNotify()
	go func() {
		<-notify
		// Ensure that channel is closed so the loop in this function can exit
		//td.esHub.UnregisterClient(&updateSig)
		safeClose(updateSig)
		log.Debug("connection closed (CloseNotify)")
	}()

	// Even stream HTTP response headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Get this party started
	fmt.Fprintf(w, ": beep boop\n\n") // ":" indicates comment in event
	flusher.Flush()

loop:
	for {
		// Wait for signal from the hub to update
		select {
		case _, ok := <-updateSig:
			// Check if the update channel was closed. Either the event stream
			// hub will do it after unregistering the client, or forcibly in
			// response to (http.CloseNotifier).CloseNotify() and only then if
			// the hub has somehow lost track of the client.
			if !ok {
				break loop
			}

			// Send message to event stream client:
			// https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events
			log.Tracef("signaling client: %p", &updateSig)
			// Event type field. Use addEventListener with named event in JS.
			if _, err := fmt.Fprintf(w, "event: newblock\n"); err != nil {
				log.Error(err)
				return
			}
			// data field
			if _, err := fmt.Fprintf(w, "data: "); err != nil {
				log.Error(err)
				return
			}

			// Marshal block data to JSON directly to the event stream client
			td.templateDataMtx.RLock()
			err := json.NewEncoder(w).Encode(td.TemplateData.BlockSummary)
			td.templateDataMtx.RUnlock()
			// If the send failed, the client is probably gone, so close the
			// connection and quit (unregister is deferred).
			if err != nil {
				log.Error(err)
				return
			}

			// End event stream message with two newlines
			fmt.Fprintf(w, "\n\n")

			// Send any buffered data to client
			flusher.Flush()
		case <-td.esHub.quitESHandler:
			break loop
		case <-ticker.C:
			// ping with an event comprising a single comment
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				log.Error(err)
				return
			}
		}
	}
	log.Debug("Done handling event stream")
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
