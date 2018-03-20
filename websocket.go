// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"sync"

	apitypes "github.com/decred/dcrdata/api/types"
)

// WebSocketMessage represents the JSON object used to send and received typed
// messages to the web client.
type WebSocketMessage struct {
	EventId  string `json:"event"`
	Messsage string `json:"message"`
}

// Event type field for an SSE event
var eventIDs = map[hubSignal]string{
	sigNewBlock:             "newblock",
	sigMempoolFeeInfoUpdate: "mempoolsstxfeeinfo",
	sigPingAndUserCount:     "ping",
}

// WebBlockInfo represents the JSON object used to send block data and stake
// info to the web client
type WebBlockInfo struct {
	BlockDataBasic *apitypes.BlockExplorerBasic         `json:"block"`
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
	sigPingAndUserCount
)

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
			case sigPingAndUserCount:
				log.Tracef("Signaling ping/user count to %d clients.", len(wsh.clients))
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
