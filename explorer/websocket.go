// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"sync"
	"time"
)

const (
	wsWriteTimeout     = 10 * time.Second
	wsReadTimeout      = 12 * time.Second
	pingInterval       = 12 * time.Second
	tickerSigReset int = iota
	tickerSigStop
	bufferTickerInterval           = 5
	newTxBufferSize                = 5
	sigNewBlock          hubSignal = iota
	sigMempoolUpdate
	sigPingAndUserCount
	sigNewTx
)

// WebSocketMessage represents the JSON object used to send and received typed
// messages to the web client.
type WebSocketMessage struct {
	EventId string `json:"event"`
	Message string `json:"message"`
}

// Event type field for an SSE event
var eventIDs = map[hubSignal]string{
	sigNewBlock:         "newblock",
	sigMempoolUpdate:    "mempool",
	sigPingAndUserCount: "ping",
	sigNewTx:            "newtx",
}

// WebsocketHub and its event loop manage all websocket client connections.
// WebsocketHub is responsible for closing all connections registered with it.
// If the event loop is running, calling (*WebsocketHub).Stop() will handle it.
type WebsocketHub struct {
	*sync.RWMutex
	clients          map[*hubSpoke]*client
	Register         chan *hubSpoke
	Unregister       chan *hubSpoke
	HubRelay         chan hubSignal
	NewTxChan        chan *MempoolTx
	newTxBuffer      []*MempoolTx
	bufferTickerChan chan int
	quitWSHandler    chan struct{}
}

type client struct {
	sync.RWMutex
	newTxs []*MempoolTx
}

type hubSignal int
type hubSpoke chan hubSignal

// NewWebsocketHub creates a new WebsocketHub
func NewWebsocketHub() *WebsocketHub {
	return &WebsocketHub{
		RWMutex:          new(sync.RWMutex),
		clients:          make(map[*hubSpoke]*client),
		Register:         make(chan *hubSpoke),
		Unregister:       make(chan *hubSpoke),
		HubRelay:         make(chan hubSignal),
		NewTxChan:        make(chan *MempoolTx),
		newTxBuffer:      make([]*MempoolTx, 0, newTxBufferSize),
		bufferTickerChan: make(chan int),
		quitWSHandler:    make(chan struct{}),
	}
}

// NumClients returns the number of clients connected to the websocket hub
func (wsh *WebsocketHub) NumClients() int {
	return len(wsh.clients)
}

// RegisterClient registers a websocket connection with the hub.
func (wsh *WebsocketHub) RegisterClient(c *hubSpoke) {
	log.Debug("Registering new websocket client")
	wsh.Register <- c
}

// registerClient should only be called from the run loop
func (wsh *WebsocketHub) registerClient(c *hubSpoke) {
	wsh.clients[c] = new(client)
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
	// end the buffer interval send loop
	wsh.bufferTickerChan <- tickerSigStop
	// unregister all clients
	for client := range wsh.clients {
		wsh.unregisterClient(client)
	}
}

func (wsh *WebsocketHub) run() {
	log.Info("Starting WebsocketHub run loop.")
	// start the buffer send ticker loop
	go wsh.periodicBufferSend()
	for {
	events:
		select {
		case hubSignal := <-wsh.HubRelay:
			var newtx *MempoolTx
			switch hubSignal {
			case sigNewBlock:
				log.Infof("Signaling new block to %d websocket clients.", len(wsh.clients))
			case sigPingAndUserCount:
				log.Tracef("Signaling ping/user count to %d websocket clients.", len(wsh.clients))
			case sigMempoolUpdate:
				log.Infof("Signaling mempool update to %d websocket clients.", len(wsh.clients))
			case sigNewTx:
				newtx = <-wsh.NewTxChan
				log.Debugf("Received new tx %s", newtx.Hash)
				wsh.addTxToBuffer(newtx)
			default:
				log.Errorf("Unknown hub signal: %v", hubSignal)
				break events
			}
			for client := range wsh.clients {
				if hubSignal == sigNewTx {
					break
				}
				// signal or unregister the client
				select {
				case *client <- hubSignal:

				default:
					wsh.unregisterClient(client)
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

// addTxToBuffer adds a tx to the buffer, then initiates a buffer send and resets
// the send ticker if the buffer is full
func (wsh *WebsocketHub) addTxToBuffer(tx *MempoolTx) {
	wsh.RLock()
	wsh.newTxBuffer = append(wsh.newTxBuffer, tx)
	wsh.RUnlock()
	if len(wsh.newTxBuffer) == newTxBufferSize {
		go wsh.sendNewTxs()
		wsh.bufferTickerChan <- tickerSigReset
	}
}

// sendNewTxs sends all txs in the buffer if any and clears the buffer
func (wsh *WebsocketHub) sendNewTxs() {
	wsh.RLock()
	if len(wsh.newTxBuffer) == 0 {
		wsh.RUnlock()
		return
	}
	txs := make([]*MempoolTx, len(wsh.newTxBuffer))
	copy(txs, wsh.newTxBuffer)
	wsh.newTxBuffer = make([]*MempoolTx, 0, newTxBufferSize)
	wsh.RUnlock()
	log.Debugf("Signaling %d new tx to %d clients", len(txs), len(wsh.clients))
	for signal, client := range wsh.clients {
		client.RLock()
		client.newTxs = txs
		select {
		case *signal <- sigNewTx:

		default:
			wsh.unregisterClient(signal)
		}
	}

}

// periodicBufferSend initiates a buffer send every bufferTickerInterval seconds
func (wsh *WebsocketHub) periodicBufferSend() {
	ticker := time.NewTicker(bufferTickerInterval * time.Second)
	for {
		select {
		case <-ticker.C:
			wsh.sendNewTxs()
		case sig := <-wsh.bufferTickerChan:
			switch sig {
			case tickerSigReset:
				ticker = time.NewTicker(bufferTickerInterval * time.Second)
			case tickerSigStop:
				return
			}
		}
	}
}
