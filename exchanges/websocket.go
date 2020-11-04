// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/carterjones/signalr"
	"github.com/carterjones/signalr/hubs"
	"github.com/gorilla/websocket"
)

const (
	wsWriteTimeout = 5 * time.Second
	fauxBrowserUA  = "Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36"
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

// Dump the signalr.Message to something readable.
func dumpSignalrMsg(msg signalr.Message) {
	fmt.Println("=================================")
	fmt.Printf("C: %s\n", jsonify(msg.C))
	fmt.Printf("S: %s\n", jsonify(msg.S))
	fmt.Printf("G: %s\n", jsonify(msg.G))
	fmt.Printf("I: %s\n", jsonify(msg.I))
	fmt.Printf("E: %s\n", jsonify(msg.E))
	s, _ := msg.R.MarshalJSON()
	fmt.Printf("R: %s\n", string(s))
	s, _ = msg.H.MarshalJSON()
	fmt.Printf("H: %s\n", string(s))
	s, _ = msg.D.MarshalJSON()
	fmt.Printf("D: %s\n", string(s))
	s, _ = msg.T.MarshalJSON()
	fmt.Printf("T: %s\n", string(s))
	for _, hubMsg := range msg.M {
		fmt.Printf("  M: %s\n", hubMsg.M)
		for _, arg := range hubMsg.A {
			fmt.Printf("    A: %s\n", jsonify(arg))
		}
	}
	fmt.Println("=================================")
}

// The interface for a signalr connection.
type signalrClient interface {
	Send(hubs.ClientMsg) error
	Close()
}

type signalrConfig struct {
	host           string
	protocol       string
	endpoint       string
	connectionData string
	params         map[string]string
	msgHandler     signalr.MsgHandler // func(msg signalr.Message)
	errHandler     signalr.ErrHandler // func(err error)
}

// A wrapper for the signalr.Client. Satisfies signalrClient.
type signalrConnection struct {
	c   *signalr.Client
	mtx sync.Mutex
	on  bool
}

// Send sends the ClientMsg on the connection. A mutex makes Send thread-safe.
func (conn *signalrConnection) Send(msg hubs.ClientMsg) error {
	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	return conn.c.Send(msg)
}

// Close closes the underlying signalr connection.
func (conn *signalrConnection) Close() {
	// Underlying connection Close can block, so measures should be taken prevent
	// calls to Close on an already closed connection.
	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	if !conn.on {
		return
	}
	conn.on = false
	conn.c.Close()
}

// Create a new signalr connection. Returns the signalrClient interface rather
// than the signalrConnection.
func newSignalrConnection(cfg *signalrConfig) (signalrClient, error) {
	// Prepare a SignalR client.
	c := signalr.New(
		cfg.host,
		cfg.protocol,
		cfg.endpoint,
		cfg.connectionData,
		cfg.params,
	)

	// Set the user agent to one that looks like a browser.
	c.Headers["User-Agent"] = fauxBrowserUA

	// Start the connection.
	err := c.Run(cfg.msgHandler, cfg.errHandler)
	if err != nil {
		return nil, err
	}
	return &signalrConnection{
		c:  c,
		on: true,
	}, nil
}
