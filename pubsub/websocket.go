// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package pubsub

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	exptypes "github.com/decred/dcrdata/v8/explorer/types"
	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
)

type hubSpoke chan pstypes.HubMessage

const (
	// PingInterval is how frequently the server will ping all clients. The
	// clients should set their read deadlines to more than this.
	PingInterval = 30 * time.Second

	tickerSigReset int = iota
	tickerSigStop

	// NewTxBufferSize is the maximum length of the new transaction slice sent
	// to websocket clients.
	NewTxBufferSize      = 5
	bufferTickerInterval = 3

	maxPayloadBytes = 1 << 20
)

// Type aliases for the different HubSignals.
var (
	sigSubscribe        = pstypes.SigSubscribe
	sigUnsubscribe      = pstypes.SigUnsubscribe
	sigDecodeTx         = pstypes.SigDecodeTx
	sigSentTx           = pstypes.SigSendTx
	sigNewBlock         = pstypes.SigNewBlock
	sigMempoolUpdate    = pstypes.SigMempoolUpdate
	sigPingAndUserCount = pstypes.SigPingAndUserCount
	sigNewTx            = pstypes.SigNewTx
	sigNewTxs           = pstypes.SigNewTxs
	sigAddressTx        = pstypes.SigAddressTx
	sigSyncStatus       = pstypes.SigSyncStatus
	sigByeNow           = pstypes.SigByeNow
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
	numClients         atomic.Value
	Register           chan *clientHubSpoke
	Unregister         chan *hubSpoke
	HubRelay           chan pstypes.HubMessage
	bufferTickerChan   chan int
	timeToSendTxBuffer atomic.Value
	quitWSHandler      chan struct{}
	killed             chan struct{}
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
	mtx    sync.RWMutex
	id     uint64
	subs   map[pstypes.HubSignal]struct{}
	addrs  map[string]struct{}
	killed chan struct{}
	newTxs *txList
}

func newClient() *client {
	return &client{
		id:     newClientID(),
		subs:   make(map[pstypes.HubSignal]struct{}, 16),
		addrs:  make(map[string]struct{}, 16),
		killed: make(chan struct{}),
		newTxs: newTxList(NewTxBufferSize),
	}
}

func (c *client) isSubscribed(msg pstypes.HubMessage) bool {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	_, subd := c.subs[msg.Signal]
	if !subd {
		return subd
	}

	switch msg.Signal {
	case pstypes.SigAddressTx:
		am, ok := msg.Msg.(*pstypes.AddressMessage)
		if !ok {
			log.Errorf("n AddressMessage (SigAddressTx): %T", msg.Msg)
			return false
		}
		_, subd = c.addrs[am.Address]
	default:
	}

	return subd
}

func (c *client) subscribe(msg pstypes.HubMessage) (bool, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	switch msg.Signal {
	case pstypes.SigAddressTx:
		am, ok := msg.Msg.(*pstypes.AddressMessage)
		if !ok {
			return false, fmt.Errorf("msg.Msg not a string (SigAddressTx): %T", msg.Msg)
		}
		c.addrs[am.Address] = struct{}{}
	case sigPingAndUserCount, sigByeNow, sigDecodeTx, sigSentTx, sigSubscribe, sigUnsubscribe:
		// These are not subscription-based events, do not clutter the subs map.
		return false, nil
	default:
	}

	c.subs[msg.Signal] = struct{}{}
	return true, nil
}

func (c *client) unsubscribe(msg pstypes.HubMessage) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	switch msg.Signal {
	case pstypes.SigAddressTx:
		am, ok := msg.Msg.(*pstypes.AddressMessage)
		if !ok {
			return fmt.Errorf("msg.Msg not an AddressMessage (SigAddressTx): %T", msg.Msg)
		}
		delete(c.addrs, am.Address)
		// Unsubscribe from address signals ONLY if this client has no more
		// watched addresses.
		if len(c.addrs) == 0 {
			delete(c.subs, pstypes.SigAddressTx)
		}
	default:
		delete(c.subs, msg.Signal)
	}

	return nil
}

func (c *client) unsubscribeAll() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for sub := range c.subs {
		delete(c.subs, sub)
	}
	for addr := range c.addrs {
		delete(c.addrs, addr)
	}
}

// NewWebsocketHub creates a new WebsocketHub.
func NewWebsocketHub() *WebsocketHub {
	return &WebsocketHub{
		clients:          make(map[*hubSpoke]*client),
		Register:         make(chan *clientHubSpoke),
		Unregister:       make(chan *hubSpoke),
		HubRelay:         make(chan pstypes.HubMessage),
		bufferTickerChan: make(chan int, 6),
		quitWSHandler:    make(chan struct{}),
		killed:           make(chan struct{}),
		requestLimit:     maxPayloadBytes, // 1 MB
	}
}

// clientHubSpoke associates a client (subscriptions and data) with its the
// WebSocketHub communication channel.
type clientHubSpoke struct {
	cl *client
	c  *hubSpoke
}

var idCounter uint64

func newClientID() uint64 {
	return atomic.AddUint64(&idCounter, 1)
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

// NumClients returns the number of clients connected to the websocket hub.
func (wsh *WebsocketHub) NumClients() int {
	// Swallow any type assertion error since the default int of 0 is OK.
	n, _ := wsh.numClients.Load().(int)
	return n
}

func (wsh *WebsocketHub) setNumClients(n int) {
	wsh.numClients.Store(n)
}

// registerClient should only be called from the run loop.
func (wsh *WebsocketHub) registerClient(ch *clientHubSpoke) {
	wsh.clients[ch.c] = ch.cl
	wsh.setNumClients(len(wsh.clients))
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
	cl, ok := wsh.clients[c]
	if !ok {
		// unknown client, do not close channel
		log.Warnf("unknown client")
		return
	}
	delete(wsh.clients, c)
	wsh.setNumClients(len(wsh.clients))

	close(*c)
	<-cl.killed
}

// unregisterAllClients should only be called from the loop in run() or when no
// other goroutines are accessing the clients map.
func (wsh *WebsocketHub) unregisterAllClients() {
	// Closing the client hubSpoke terminates the connection's send and receive
	// loops, and thus the http.HandlerFunc.
	spokes := make([]*hubSpoke, 0, len(wsh.clients))
	// A client's killed channel is closed when the http.HandlerFunc returns.
	kills := make([]chan struct{}, 0, len(wsh.clients))
	for c, cl := range wsh.clients {
		spokes = append(spokes, c)
		kills = append(kills, cl.killed)
	}

	// Unregister each client (tracked by hubSpoke), and signal for the client
	// to shutdown by closing the channel.
	for _, c := range spokes {
		delete(wsh.clients, c)
		close(*c) // will terminate the connection's (*PubSubHub).sendLoop
	}
	wsh.setNumClients(0)

	// Wait for each client to shutdown. The client.killed channels are closed
	// as the http.HandlerFunc of each connection returns.
	for _, k := range kills {
		<-k
	}
	log.Debugf("Unregistered and killed %d clients.", len(kills))
}

// Periodically ping clients over websocket connection. Stop the ping loop by
// closing the returned channel.
func (wsh *WebsocketHub) pingClients() chan<- struct{} {
	stopPing := make(chan struct{})

	go func() {
		// start the client ping ticker
		ticker := time.NewTicker(PingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				wsh.HubRelay <- pstypes.HubMessage{Signal: sigPingAndUserCount}
			case <-stopPing:
				return
			}
		}
	}()

	return stopPing
}

// Stop kills the run() loop and unregisters all clients (connections).
func (wsh *WebsocketHub) Stop() {
	// Tell the clients we're forcibly stopping the connection.
	wsh.HubRelay <- pstypes.HubMessage{Signal: sigByeNow}

	// End the Run() loop, allowing in progress operations to complete.
	close(wsh.quitWSHandler)
	// Do not close HubRelay since there are multiple senders; Run() is the
	// receiver.

	// Wait for Run and its defers to complete.
	select {
	case <-time.NewTimer(5 * time.Second).C:
		log.Warnf("Timed out waiting for Run loop to terminate.")
	case <-wsh.killed:
	}
}

// Run starts the main event loop, which handles the following: 1. receiving
// signals on the WebsocketHub's HubRelay and broadcasting them to all
// registered clients, 2. registering clients, 3. unregistering clients, 4.
// periodically sending client's new transaction buffers, and 5. handling the
// shutdown signal from Stop.
func (wsh *WebsocketHub) Run() {
	log.Info("Starting WebsocketHub run loop.")

	defer close(wsh.killed) // must be last since this is a sentinel

	// Start the transaction buffer send ticker loop.
	go wsh.periodicTxBufferSend()

	// Start the client ping ticker.
	stopPing := wsh.pingClients()
	defer close(stopPing)

	defer func() {
		// Drain the receiving channels so that PubSubHub or any other
		// goroutines presently sending on HubRelay do not hang.
		for {
			select {
			case <-wsh.HubRelay:
			default:
				return
			}
		}
	}()

	// Unregister and wait for each client to shutdown.
	defer wsh.unregisterAllClients()

	// Only use sendMsg and sendToAll from inside the loop.
	sendMsg := func(spoke *hubSpoke, client *client, hubMsg pstypes.HubMessage) {
		// Signal or unregister the client.
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-client.killed:
			log.Tracef("Unable to send %s message to client %d: gone (killed)",
				hubMsg, client.id)
			wsh.unregisterClient(spoke)
		case *spoke <- hubMsg:
			log.Tracef("Sent %s message to client %d.", hubMsg, client.id)
		case <-timer.C:
			// TODO: remove this case (and timer) once we are
			// confident there is no change of a deadlock.
			log.Errorf("Timeout sending %s message to client %d.", hubMsg, client.id)
		}
	}

	sendToAll := func(hubMsg pstypes.HubMessage) {
		for spoke, client := range wsh.clients {
			sendMsg(spoke, client, hubMsg)
		}
	}

	for {
		//events:
		select {
		case hubMsg, ok := <-wsh.HubRelay:
			if !ok {
				log.Debugf("wsh.HubRelay closed.")
				return
			}
			// Number of connected clients
			clientsCount := len(wsh.clients)

			// No clients, skip the rest.
			if clientsCount == 0 {
				break
			}

			if !hubMsg.IsValid() {
				log.Warnf("Invalid message on HubRelay: %s", hubMsg)
				break
			}

			switch hubMsg.Signal {
			case sigNewBlock:
				// Do not log when explorer update status is active.
				if !wsh.Ready() {
					log.Infof("Signaling new block to %d websocket clients.", clientsCount)
				}
			case sigPingAndUserCount:
				log.Tracef("Signaling ping/user count to %d websocket clients.", clientsCount)
				// Ping all clients (not a subscription).
				sendToAll(hubMsg)
				continue // break events
			case sigMempoolUpdate:
				log.Infof("Signaling mempool inventory refresh to %d websocket clients.", clientsCount)
			case sigAddressTx:
				// AddressMessage already validated, but check again.
				addrMsg, ok := hubMsg.Msg.(*pstypes.AddressMessage)
				if !ok || addrMsg == nil {
					log.Errorf("sigAddressTx did not store a *AddressMessage in Msg.")
					continue
				}
			case sigNewTx:
				log.Tracef("Received sigNewTx")
				newTx, ok := hubMsg.Msg.(*exptypes.MempoolTx)
				if !ok || newTx == nil {
					continue
				}
				log.Tracef("Received new tx %s. Queueing in each client's send buffer...", newTx.Hash)
				// Only signal clients if there are tx buffers ready to send or
				// the ticker has fired.
				if !(wsh.maybeSendTxns(newTx) || wsh.TimeToSendTxBuffer()) {
					break
				}

				// In PubSubHub, the outgoing client message will be a
				// SigNewTxs, with a slice of transactions. Since the single new
				// transaction received from the mempool monitor is already
				// added to each client's slice, just relay a sigNewTxs to
				// PubSubHub with a nil slice to be a valid message.
				hubMsg.Signal = sigNewTxs
				hubMsg.Msg = ([]*exptypes.MempoolTx)(nil) // PubSubHub accesses each client's own slice.
			case sigSubscribe, sigUnsubscribe:
				log.Warnf("sigSubscribe and sigUnsubscribe are not broadcastable events.")
				continue // break events
			case sigSyncStatus:
				// TODO
			case sigByeNow:
				log.Infof("Warning all %d clients of impending hang-up.", len(wsh.clients))
				// Broadcast "bye" to all clients (not a subscription).
				sendToAll(hubMsg)
				continue
			default:
				log.Errorf("Unknown hub signal: %v", hubMsg.Signal)
				continue // break events
			}

			// Send the signal to subscribed PubSubHub clients.
			for spoke, client := range wsh.clients {
				// Verify the client subscription before bothering PubSubHub.
				// This is why the signal must be changed from sigNewTx to
				// sigNewTxs in the case of hubMsg.Signal==sigNewTx case above.
				if !client.isSubscribed(hubMsg) {
					log.Tracef("Client %d is NOT subscribed to %s.", client.id, hubMsg)
					continue
				}
				log.Tracef("Client %d is subscribed to %s.", client.id, hubMsg)

				// Signal or unregister the client.
				sendMsg(spoke, client, hubMsg)
			}

			if hubMsg.Signal == sigNewTxs {
				// The Tx buffers were just sent.
				wsh.SetTimeToSendTxBuffer(false)
			}

		case ch := <-wsh.Register:
			wsh.registerClient(ch)

		case c := <-wsh.Unregister:
			wsh.unregisterClient(c)

		case <-wsh.quitWSHandler:
			// End the buffer interval send loop,
			wsh.bufferTickerChan <- tickerSigStop

			// Quit the Run loop.
			return
		} // select { a.k.a. events:
	} // for {
}

// maybeSendTxns adds a mempool transaction to the client broadcast buffer. If
// the buffer is at capacity, a goroutine is launched to signal for the
// transactions to be sent to the clients.
func (wsh *WebsocketHub) maybeSendTxns(tx *exptypes.MempoolTx) (someReadyToSend bool) {
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
