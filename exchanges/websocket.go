// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const fauxBrowserUA = "Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36"

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
	// WriteJSON is modeled after the function defined at
	// https://godoc.org/github.com/gorilla/websocket#Conn.WriteJSON
	//
	// WriteJSON is like Write but it will encode a message to json before
	// sending it.
	WriteJSON(interface{}) error
	// Close will disconnect, causing any pending Read operations to error out.
	Close()
}

// The socketConfig is the configuration passed to newSocketConnection. It is
// just an address for now, but enables more customized settings as exchanges'
// websocket protocols are eventually implemented.
type socketConfig struct {
	address   string
	tlsConfig *tls.Config
	headers   http.Header
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

// WriteJSON is a wrapper for gorilla WriteJSON. Satisfies
// websocketFeed.WriteJSON. Writes are sequenced with a mutex lock for
// per-connection multi-threaded use.
func (client *socketClient) WriteJSON(msg interface{}) error {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	return client.conn.WriteJSON(msg)
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

	conn, resp, err := dialer.Dial(cfg.address, cfg.headers)
	if err == nil { // Return early if no error.
		return &socketClient{
			conn: conn,
			done: make(chan struct{}),
			on:   true,
		}, nil
	}

	// Handle response properly.
	if resp == nil {
		return nil, fmt.Errorf("received empty response body and an error: %w", err)
	}

	return nil, fmt.Errorf("unexpected response status %s and error: %w", resp.Status, err)
}
