// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// An interface wraps the websocket to enable testing.
type websocketFeed interface {
	// Done should return a channel that will be closed when there is a
	// disconnection.
	Done() chan struct{}
	// Read will block until a message is received or an error occurs. Use
	// Close from another thread to force a disconnection error.
	Read() ([]byte, error)
	// Write sends a message. Write will safely sequence messages from multiple
	// threads.
	Write(interface{}) error
	// Close will disconnect, causing any pending Read operations to error out.
	Close()
}

// The socketConfig is the configuration passed to newSocketConnection. It is
// just an address for now, but enables more customized settings as exchanges'
// websocket protocols are eventually implemented.
type socketConfig struct {
	address   string
	tlsConfig *tls.Config
}

// A manager for a gorilla websocket connection.
// Satisfies websocketFeed interface.
type socketClient struct {
	mtx  sync.Mutex
	on   bool
	conn *websocket.Conn
	done chan struct{}
}

// Read is a wrapper for gorilla's ReadMessage that satisfies websocketFeed.Read.
func (client *socketClient) Read() (msg []byte, err error) {
	_, msg, err = client.conn.ReadMessage()
	return
}

// Write is a wrapper for gorilla WriteMessage. Satisfies websocketFeed.Write.
// JSON marshaling is performed before sending. Writes are sequenced with a
// mutex lock for per-connection multi-threaded use.
func (client *socketClient) Write(msg interface{}) error {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return client.conn.WriteMessage(websocket.TextMessage, b)
}

// Done returns a channel that will be closed when the websocket connection is
// closed. Satisfies websocketFeed.Done.
func (client *socketClient) Done() chan struct{} {
	return client.done
}

// Close is wrapper for gorilla's Close that satisfies websocketFeed.Close.
func (client *socketClient) Close() {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	if !client.on {
		return
	}
	client.on = false
	close(client.done)
	client.conn.Close()
}

// Constructor for a socketClient, but returned as a websocketFeed.
func newSocketConnection(cfg *socketConfig) (websocketFeed, error) {
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment, // Same as DefaultDialer.
		HandshakeTimeout: 10 * time.Second,          // DefaultDialer is 45 seconds.
		TLSClientConfig:  cfg.tlsConfig,
	}

	conn, _, err := dialer.Dial(cfg.address, nil)
	if err != nil {
		return nil, err
	}
	return &socketClient{
		conn: conn,
		done: make(chan struct{}),
		on:   true,
	}, nil
}
