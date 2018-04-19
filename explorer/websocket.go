// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"sync"
	"time"
)

const (
	wsWriteTimeout = 10 * time.Second
	wsReadTimeout  = 12 * time.Second
	pingInterval   = 8 * time.Second

	tickerSigReset int = iota
	tickerSigStop
	bufferSend

	bufferTickerInterval = 5
	newTxBufferSize      = 5
	clientSignalSize     = 5

	sigNewBlock hubSignal = iota
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
	clients          map[*hubSpoke]*client
	Register         chan *clientHubSpoke
	Unregister       chan *hubSpoke
	HubRelay         chan hubSignal
	NewTxChan        chan *MempoolTx
	newTxBuffer      []*MempoolTx
	bufferMtx        *sync.Mutex
	bufferTickerChan chan int
	sendBufferChan   chan int
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
		clients:          make(map[*hubSpoke]*client),
		Register:         make(chan *clientHubSpoke),
		Unregister:       make(chan *hubSpoke),
		HubRelay:         make(chan hubSignal),
		NewTxChan:        make(chan *MempoolTx),
		newTxBuffer:      make([]*MempoolTx, 0, newTxBufferSize),
		bufferTickerChan: make(chan int, clientSignalSize),
		bufferMtx:        new(sync.Mutex),
		sendBufferChan:   make(chan int, clientSignalSize),
		quitWSHandler:    make(chan struct{}),
	}
}

type clientHubSpoke struct {
	cl *client
	c  *hubSpoke
}

// NumClients returns the number of clients connected to the websocket hub
func (wsh *WebsocketHub) NumClients() int {
	return len(wsh.clients)
}

// RegisterClient registers a websocket connection with the hub, and returns a
// pointer to the new client data object.
func (wsh *WebsocketHub) RegisterClient(c *hubSpoke) *client {
	cl := new(client)
	wsh.Register <- &clientHubSpoke{cl, c}
	return cl
}

// registerClient should only be called from the run loop
func (wsh *WebsocketHub) registerClient(ch *clientHubSpoke) {
	wsh.clients[ch.c] = ch.cl
	log.Debugf("Registered new websocket client (%d).", wsh.NumClients())
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

// Periodically ping clients over websocket connection. Stop the ping loop by
// closing the returned channel.
func (wsh *WebsocketHub) pingClients() chan<- struct{} {
	stopPing := make(chan struct{})

	go func() {
		// start the client ping ticker
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				wsh.HubRelay <- sigPingAndUserCount
			case _, ok := <-stopPing:
				if !ok {
					log.Errorf("Do not send on stopPing channel, only close it.")
				}
				return
			}
		}
	}()

	return stopPing
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
}

func (wsh *WebsocketHub) run() {
	log.Info("Starting WebsocketHub run loop.")

	// start the buffer send ticker loop
	go wsh.periodicBufferSend()

	// start the client ping ticker
	stopPing := wsh.pingClients()
	defer close(stopPing)

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
				log.Tracef("Received new tx %s", newtx.Hash)
				wsh.MaybeSendTxns(newtx)
			default:
				log.Errorf("Unknown hub signal: %v", hubSignal)
				break events
			}
			for client := range wsh.clients {
				// Don't signal the client on new tx, another case handles that
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
		case ch := <-wsh.Register:
			wsh.registerClient(ch)
		case c := <-wsh.Unregister:
			wsh.unregisterClient(c)
		case _, ok := <-wsh.quitWSHandler:
			if !ok {
				log.Error("close channel already closed. This should not happen.")
				return
			}
			close(wsh.quitWSHandler)

			// end the buffer interval send loop
			wsh.bufferTickerChan <- tickerSigStop

			// unregister all clients
			for client := range wsh.clients {
				wsh.unregisterClient(client)
			}
			return
		case <-wsh.sendBufferChan:
			wsh.bufferMtx.Lock()
			if len(wsh.newTxBuffer) == 0 {
				wsh.bufferMtx.Unlock()
				continue
			}
			txs := make([]*MempoolTx, len(wsh.newTxBuffer))
			copy(txs, wsh.newTxBuffer)
			wsh.newTxBuffer = make([]*MempoolTx, 0, newTxBufferSize)
			wsh.bufferMtx.Unlock()
			log.Debugf("Signaling %d new tx to %d clients", len(txs), len(wsh.clients))
			for signal, client := range wsh.clients {
				client.Lock()
				client.newTxs = txs
				client.Unlock()
				select {
				case *signal <- sigNewTx:
				default:
					wsh.unregisterClient(signal)
				}
			}
		}
	}
}

// MaybeSendTxns adds a mempool transaction to the client broadcast buffer. If
// the buffer is at capacity, a goroutine is launched to signal for the
// transactions to be sent to the clients.
func (wsh *WebsocketHub) MaybeSendTxns(tx *MempoolTx) {
	if wsh.addTxToBuffer(tx) {
		// This is called from the event loop, so these sends channel may not be
		// blocking.
		go func() {
			wsh.bufferTickerChan <- tickerSigReset
			wsh.sendBufferChan <- bufferSend
		}()
	}
}

// addTxToBuffer adds a tx to the buffer, then returns if the buffer is full
func (wsh *WebsocketHub) addTxToBuffer(tx *MempoolTx) bool {
	wsh.bufferMtx.Lock()
	defer wsh.bufferMtx.Unlock()

	wsh.newTxBuffer = append(wsh.newTxBuffer, tx)

	return len(wsh.newTxBuffer) >= newTxBufferSize
}

// periodicBufferSend initiates a buffer send every bufferTickerInterval seconds
func (wsh *WebsocketHub) periodicBufferSend() {
	ticker := time.NewTicker(bufferTickerInterval * time.Second)
	for {
		select {
		case <-ticker.C:
			wsh.sendBufferChan <- bufferSend
		case sig := <-wsh.bufferTickerChan:
			switch sig {
			case tickerSigReset:
				ticker.Stop()
				ticker = time.NewTicker(bufferTickerInterval * time.Second)
			case tickerSigStop:
				close(wsh.bufferTickerChan)
				return
			}
		}
	}
}
