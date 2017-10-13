// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

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
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/wire"
	humanize "github.com/dustin/go-humanize"
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
	BlockSummary   apitypes.BlockExplorerBasic
	StakeSummary   apitypes.StakeInfoExtendedEstimates
	MempoolFeeInfo apitypes.MempoolTicketFeeInfo
	MempoolFees    apitypes.MempoolTicketFees
}

// WebUI models data for the web page and websocket
type WebUI struct {
	wsHub           *WebsocketHub
	MPC             mempool.MempoolDataCache
	TemplateData    WebTemplateData
	templateDataMtx sync.RWMutex
	templ           *template.Template
	errorTempl      *template.Template
	templFiles      []string
	params          *chaincfg.Params
	ExplorerSource  APIDataSource
	tmpHelpers      template.FuncMap
}

// NewWebUI constructs a new WebUI by loading and parsing the html templates
// then launching the WebSocket event handler
func NewWebUI(expSource APIDataSource) *WebUI {
	fp := filepath.Join("views", "root.tmpl")
	efp := filepath.Join("views", "extras.tmpl")
	errorfp := filepath.Join("views", "error.tmpl")
	helpers := template.FuncMap{
		"divide": func(n int64, d int64) int64 {
			val := n / d
			return val
		},
		"int64Comma": func(v int64) string {
			t := humanize.Comma(v)
			return t
		},
		"timezone": func() string {
			t, _ := time.Now().Zone()
			return t
		},
		"formatBytes": func(v int32) string {
			i64 := uint64(v)
			return humanize.Bytes(i64)
		},
		"getTime": func(btime int64) string {
			t := time.Unix(btime, 0)
			return t.Format("1/_2/06 15:04:05")
		},
		"ticketWindowProgress": func(i int) float64 {
			p := (float64(i) / 144) * 100
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
	tmpl, err := template.New("home").Funcs(helpers).ParseFiles(fp, efp)
	if err != nil {
		return nil
	}

	errtmpl, err := template.New("error").ParseFiles(errorfp, efp)
	if err != nil {
		return nil
	}

	//var templFiles []string
	templFiles := []string{fp, efp, errorfp}

	wsh := NewWebsocketHub()
	go wsh.run()

	return &WebUI{
		wsHub:          wsh,
		templ:          tmpl,
		errorTempl:     errtmpl,
		templFiles:     templFiles,
		params:         activeChain,
		ExplorerSource: expSource,
		tmpHelpers:     helpers,
	}
}

// StopWebsocketHub stops the websocket hub
func (td *WebUI) StopWebsocketHub() {
	log.Info("Stopping websocket hub.")
	td.wsHub.Stop()
}

// ParseTemplates parses all the template files, updating the *html/template.Template.
func (td *WebUI) ParseTemplates() (err error) {
	td.templ, err = template.New("home").Funcs(td.tmpHelpers).ParseFiles(td.templFiles[0], td.templFiles[1])
	if err != nil {
		return err
	}
	td.errorTempl, err = template.New("error").ParseFiles(td.templFiles[2], td.templFiles[1])
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
func (td *WebUI) Store(blockData *blockdata.BlockData, _ *wire.MsgBlock) error {
	td.templateDataMtx.Lock()
	td.TemplateData.BlockSummary = blockData.ToBlockExplorerSummary()
	td.TemplateData.StakeSummary = blockData.ToStakeInfoExtendedEstimates()
	td.templateDataMtx.Unlock()

	td.wsHub.HubRelay <- sigNewBlock

	return nil
}

// StoreMPData stores mempool data in the mempool cache and update the webui via websocket
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
	chainHeight := td.ExplorerSource.GetHeight()

	initialBlocks := make([]*dcrjson.GetBlockVerboseResult, 0, 6)
	for i := chainHeight; i > chainHeight-6; i-- {
		data := td.ExplorerSource.GetBlockVerbose(i, false)
		initialBlocks = append(initialBlocks, data)
	}

	str, err := TemplateExecToString(td.templ, "home", struct {
		InitialData []*dcrjson.GetBlockVerboseResult
		Data        WebTemplateData
	}{
		initialBlocks,
		td.TemplateData,
	})
	td.templateDataMtx.RUnlock()
	if err != nil {
		log.Errorf("Failed to execute template: %v", err)
		http.Error(w, "template execute failure", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// ErrorPage is the http.HandlerFunc for the "/error" http path
func (td *WebUI) ErrorPage(w http.ResponseWriter, r *http.Request) {
	code := "404"
	msg := "Whatever you were looking for... doesn't exit here"
	searchStr, ok := r.Context().Value(ctxSearch).(string)
	if ok {
		code = "Not Found"
		msg = "No Items matching \"" + searchStr + "\" were found"
	}
	str, err := TemplateExecToString(td.errorTempl, "error", struct {
		ErrorCode   string
		ErrorString string
	}{
		code,
		msg,
	})
	if err != nil {
		log.Errorf("Failed to execute template: %v", err)
		http.Error(w, "template execute failure", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusNotFound)
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
		// Create channel to signal updated data availability
		updateSig := make(hubSpoke)
		// register websocket client with our signal channel
		td.wsHub.RegisterClient(&updateSig)
		// unregister (and close signal channel) before return
		defer td.wsHub.UnregisterClient(&updateSig)

		// Ticker for a regular ping
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		go func() {
			for range ticker.C {
				td.wsHub.HubRelay <- sigPingAndUserCount
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
					td.wsHub.UnregisterClient(&updateSig)
					break loop
				}

				if _, ok = eventIDs[sig]; !ok {
					break loop
				}

				log.Tracef("signaling client: %p", &updateSig)
				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))

				// Write block data to websocket client
				td.templateDataMtx.RLock()
				webData := WebSocketMessage{
					EventId: eventIDs[sig],
				}
				buff := new(bytes.Buffer)
				enc := json.NewEncoder(buff)
				switch sig {
				case sigNewBlock:
					enc.Encode(WebBlockInfo{
						BlockDataBasic: &td.TemplateData.BlockSummary,
						StakeInfoExt:   &td.TemplateData.StakeSummary,
					})
					webData.Messsage = buff.String()
				case sigMempoolFeeInfoUpdate:
					enc.Encode(td.TemplateData.MempoolFeeInfo)
					webData.Messsage = buff.String()
				case sigPingAndUserCount:
					// ping and send user count
					webData.Messsage = strconv.Itoa(td.wsHub.NumClients())
				}

				err := websocket.JSON.Send(ws, webData)
				td.templateDataMtx.RUnlock()
				if err != nil {
					log.Debugf("Failed to encode WebSocketMessage %v: %v", sig, err)
					// If the send failed, the client is probably gone, so close
					// the connection and quit.
					return
				}
			case <-td.wsHub.quitWSHandler:
				break loop
			}
		}
	})

	wsHandler.ServeHTTP(w, r)
}

// FileServer conveniently sets up a http.FileServer handler to serve
// static files from a http.FileSystem.
func FileServer(r chi.Router, path string, root http.FileSystem, CacheControlMaxAge int64) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.With(CacheControl(CacheControlMaxAge)).Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	}))
}
