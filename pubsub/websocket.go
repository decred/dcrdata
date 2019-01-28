// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package pubsub

import (
	"sync"
	"sync/atomic"
	"time"

	exptypes "github.com/decred/dcrdata/v4/explorer/types"
	pstypes "github.com/decred/dcrdata/v4/pubsub/types"
)

type hubSpoke chan pstypes.HubSignal

const (
	pingInterval = 45 * time.Second

	tickerSigReset int = iota
	tickerSigStop

	bufferTickerInterval = 5
	NewTxBufferSize      = 5
	clientSignalSize     = 5

	MaxPayloadBytes = 1 << 20
)

// Type aliases for the different HubSignals.
var (
	sigSubscribe        = pstypes.SigSubscribe
	sigUnsubscribe      = pstypes.SigUnsubscribe
	sigNewBlock         = pstypes.SigNewBlock
	sigMempoolUpdate    = pstypes.SigMempoolUpdate
	sigPingAndUserCount = pstypes.SigPingAndUserCount
	sigNewTx            = pstypes.SigNewTx
	sigSyncStatus       = pstypes.SigSyncStatus
)

type txList struct {
	sync.Mutex
	t pstypes.TxList
}

func newTxList(cap int) *txList {
	txl := new(txList)
	txl.t = make([]*exptypes.MempoolTx, 0, cap)
	return txl
}

func (tl *txList) addTxToBuffer(tx *exptypes.MempoolTx) (readyToSend bool) {
	tl.Lock()
	defer tl.Unlock()
	tl.t = append(tl.t, tx)
	if len(tl.t) >= NewTxBufferSize {
		readyToSend = true
	}
	return
}

// WebsocketHub and its event loop manage all websocket client connections.
// WebsocketHub is responsible for closing all connections registered with it.
// If the event loop is running, calling (*WebsocketHub).Stop() will handle it.
type WebsocketHub struct {
	clients            map[*hubSpoke]*client
	Register           chan *clientHubSpoke
	Unregister         chan *hubSpoke
	HubRelay           chan pstypes.HubSignal
	NewTxChan          chan *exptypes.MempoolTx
	bufferTickerChan   chan int
	timeToSendTxBuffer atomic.Value
	quitWSHandler      chan struct{}
	requestLimit       int
	ready              atomic.Value
}

func (wsh *WebsocketHub) TimeToSendTxBuffer() bool {
	ready, ok := wsh.timeToSendTxBuffer.Load().(bool)
	return ok && ready
}

func (wsh *WebsocketHub) SetTimeToSendTxBuffer(ready bool) {
	wsh.timeToSendTxBuffer.Store(ready)
}

// Ready is a thread-safe way to fetch the boolean in ready.
func (wsh *WebsocketHub) Ready() bool {
	syncing, ok := wsh.ready.Load().(bool)
	return ok && syncing
}

// SetReady is a thread-safe way to update the ready.
func (wsh *WebsocketHub) SetReady(ready bool) {
	wsh.ready.Store(ready)
}

type client struct {
	sync.RWMutex
	subs   map[pstypes.HubSignal]struct{}
	newTxs *txList
}

func newClient() *client {
	return &client{
		subs:   make(map[pstypes.HubSignal]struct{}, 16),
		newTxs: newTxList(NewTxBufferSize),
	}
}

func (c *client) isSubscribed(sig pstypes.HubSignal) bool {
	c.RLock()
	defer c.RUnlock()
	_, subd := c.subs[sig]
	return subd
}

func (c *client) subscribe(sig pstypes.HubSignal) {
	c.Lock()
	defer c.Unlock()
	c.subs[sig] = struct{}{}
}

func (c *client) unsubscribe(sig pstypes.HubSignal) {
	c.Lock()
	defer c.Unlock()
	delete(c.subs, sig)
}

func (c *client) unsubscribeAll() {
	c.Lock()
	defer c.Unlock()
	for sub := range c.subs {
		delete(c.subs, sub)
	}
}

// NewWebsocketHub creates a new WebsocketHub.
func NewWebsocketHub() *WebsocketHub {
	return &WebsocketHub{
		clients:          make(map[*hubSpoke]*client),
		Register:         make(chan *clientHubSpoke),
		Unregister:       make(chan *hubSpoke),
		HubRelay:         make(chan pstypes.HubSignal),
		NewTxChan:        make(chan *exptypes.MempoolTx, 1),
		bufferTickerChan: make(chan int, clientSignalSize),
		quitWSHandler:    make(chan struct{}),
		requestLimit:     MaxPayloadBytes, // 1 MB
	}
}

// clientHubSpoke associates a client (subscriptions and data) with its the
// WebSocketHub communication channel.
type clientHubSpoke struct {
	cl *client
	c  *hubSpoke
}

// NumClients returns the number of clients connected to the websocket hub
func (wsh *WebsocketHub) NumClients() int {
	return len(wsh.clients)
}

// NewClientHubSpoke registers a connection with the hub, and returns a pointer
// to the new client data object. Use UnregisterClient on this object to stop
// signaling messages, and close the signal channel.
func (wsh *WebsocketHub) NewClientHubSpoke() *clientHubSpoke {
	c := make(hubSpoke, 16)
	ch := &clientHubSpoke{
		cl: newClient(),
		c:  &c,
	}
	wsh.Register <- ch
	return ch
}

// registerClient should only be called from the run loop.
func (wsh *WebsocketHub) registerClient(ch *clientHubSpoke) {
	wsh.clients[ch.c] = ch.cl
	log.Debugf("Registered new websocket client (%d).", wsh.NumClients())
}

// UnregisterClient unregisters the client with the hub and closes the client's
// update signal channel. The client is unregistered via the main run() loop, so
// this call will block if the run() loop is not running.
func (wsh *WebsocketHub) UnregisterClient(ch *clientHubSpoke) {
	wsh.Unregister <- ch.c
}

// unregisterClient should only be called from the loop in run().
func (wsh *WebsocketHub) unregisterClient(c *hubSpoke) {
	if _, ok := wsh.clients[c]; !ok {
		// unknown client, do not close channel
		log.Warnf("unknown client")
		return
	}
	delete(wsh.clients, c)

	close(*c)
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
				if ok {
					log.Errorf("Do not send on stopPing channel, only close it.")
				}
				return
			}
		}
	}()

	return stopPing
}

// Stop kills the run() loop and unregisters all clients (connections).
func (wsh *WebsocketHub) Stop() {
	// Close the receiving new tx channel. Senders should use a select with a
	// default or timer/timeout case to avoid a panic.
	close(wsh.NewTxChan)
	// End the run() loop, allowing in progress operations to complete.
	wsh.quitWSHandler <- struct{}{}
	// Lastly close the hub relay channel sine the quitWSHandler signal is
	// handled in the Run loop.
	close(wsh.HubRelay)
}

// Run starts the main event loop, which handles the following: 1. receiving
// signals on the WebsocketHub's HubRelay and broadcasting them to all
// registered clients, 2. registering clients, 3. unregistering clients, 4.
// periodically sending client's new transaction buffers, and 5. handling the
// shutdown signal from Stop.
func (wsh *WebsocketHub) Run() {
	log.Info("Starting WebsocketHub run loop.")

	// Start the transaction buffer send ticker loop.
	go wsh.periodicTxBufferSend()

	// Start the client ping ticker.
	stopPing := wsh.pingClients()
	defer close(stopPing)

	defer func() {
		// Drain the receiving channels if they were not already closed by Stop.
		for range wsh.NewTxChan {
		}
		for range wsh.HubRelay {
		}
	}()

	for {
		//events:
		select {
		case hubSignal, ok := <-wsh.HubRelay:
			if !ok {
				log.Debugf("wsh.HubRelay closed.")
				return
			}
			// Number of connected clients
			clientsCount := len(wsh.clients)
			// Even of there are no connected clients, it is necessary to
			// process the hubSignal below since signals may be followed by data
			// sent on other channels (e.g. NewTxChan).

			var newtx *exptypes.MempoolTx
			var someTxBuffersReady bool
			switch hubSignal {
			case sigNewBlock:
				// Do not log when explorer update status is active.
				if !wsh.Ready() {
					log.Infof("Signaling new block to %d websocket clients.", clientsCount)
				}
			case sigPingAndUserCount:
				log.Tracef("Signaling ping/user count to %d websocket clients.", clientsCount)
			case sigMempoolUpdate:
				log.Infof("Signaling mempool inventory refresh to %d websocket clients.", clientsCount)
			case sigNewTx:
				log.Tracef("Received sigNewTx")
				newtx = <-wsh.NewTxChan
				if newtx == nil { // channel likely closed by Stop so quit signal can operate
					continue
				}
				log.Tracef("Received new tx %s. Queueing in each client's send buffer...", newtx.Hash)
				someTxBuffersReady = wsh.MaybeSendTxns(newtx)
			case sigSubscribe:
				continue // break events
			case sigSyncStatus:
				// TODO
			default:
				log.Errorf("Unknown hub signal: %v", hubSignal)
				continue // break events
			}

			// No clients, skip the rest.
			if clientsCount == 0 {
				break
			}

			if hubSignal == sigNewTx {
				// Only signal clients if there are tx buffers ready to send or
				// the ticker has fired.
				if !(someTxBuffersReady || wsh.TimeToSendTxBuffer()) {
					break
				}
			}

			// Send the signal to all clients.
			for spoke, client := range wsh.clients {
				if !client.isSubscribed(hubSignal) {
					log.Tracef("Client not subscribed to %s.", hubSignal.String())
					continue
				}

				// Signal or unregister the client.
				select {
				case *spoke <- hubSignal:
				default:
					wsh.unregisterClient(spoke)
				}
			}

			if hubSignal == sigNewTx {
				// The Tx buffers were just sent.
				wsh.SetTimeToSendTxBuffer(false)
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

			// End the buffer interval send loop,
			wsh.bufferTickerChan <- tickerSigStop

			// Unregister all clients,
			for client := range wsh.clients {
				wsh.unregisterClient(client)
			}
			// Quit the Run loop.
			return

		} // select { a.k.a. events:
	} // for {
}

// MaybeSendTxns adds a mempool transaction to the client broadcast buffer. If
// the buffer is at capacity, a goroutine is launched to signal for the
// transactions to be sent to the clients.
func (wsh *WebsocketHub) MaybeSendTxns(tx *exptypes.MempoolTx) (someReadyToSend bool) {
	// addTxToBuffer adds the transaction to each client's tx buffer, and
	// indicates if at least one client has a buffer at or above the send limit.
	someReadyToSend = wsh.addTxToBuffer(tx)
	if someReadyToSend {
		// Reset the "time to send" ticker since the event loop is about send.
		wsh.bufferTickerChan <- tickerSigReset
	}
	return
}

// addTxToBuffer adds a tx to each client's tx buffer. The return boolean value
// indicates if at least one buffer is ready to be sent.
func (wsh *WebsocketHub) addTxToBuffer(tx *exptypes.MempoolTx) (someReadyToSend bool) {
	for _, client := range wsh.clients {
		someReadyToSend = client.newTxs.addTxToBuffer(tx)
	}
	return
}

// periodicTxBufferSend initiates a transaction buffer send via sendTxBufferChan
// every bufferTickerInterval seconds.
func (wsh *WebsocketHub) periodicTxBufferSend() {
	ticker := time.NewTicker(bufferTickerInterval * time.Second)
	for {
		select {
		case <-ticker.C:
			wsh.SetTimeToSendTxBuffer(true)
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
