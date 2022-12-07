// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

// This was almost entirely written using
// https://blog.3d-logic.com/2015/03/29/signalr-on-the-wire-an-informal-description-of-the-signalr-protocol/
// and github.com/carterjones/signalr as a reference guide.

package exchanges

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"time"
)

// defaultClientProtocol is the default protocol version used when connecting to
// a signalR websocket.
const defaultClientProtocol = "1.5"

// signalRClientMsg represents a message sent from or to the signalR server on a
// persistent websocket connection.
type signalRClientMsg struct {
	// invocation identifier – allows to match up responses with requests
	I int
	// the name of the hub
	H string
	// the name of the method
	M string
	// arguments (an array, can be empty if the method does not have any
	// parameters)
	A []interface{}
	// state – a dictionary containing additional custom data (optional)
	S *json.RawMessage `json:",omitempty"`
}

// signalRMessage represents a signalR message sent from the server to the
// persistent websocket connection.
type signalRMessage struct {
	// message id, present for all non-KeepAlive messages
	C string
	// an array containing actual data
	M []signalRClientMsg
	// indicates that the transport was initialized (a.k.a. init message)
	S int
	// groups token – an encrypted string representing group membership
	G string
	// other miscellaneous variables that sometimes are sent by the server
	I string
	E string
	R json.RawMessage
	H json.RawMessage // could be bool or string depending on a message type
	D json.RawMessage
	T json.RawMessage
}

// signalRNegotiation represents a response sent after a negotiation with a
// signalR server. A bunch of other fields have been removed because they are
// not needed.
type signalRNegotiation struct {
	ConnectionToken string
}

// connectSignalRWebsocket connects to a signalR websocket in three steps
// (negotiate, connect, and start) and returns a websocketFeed which can be used
// to read websocket messages from the signalR server. There are no retires if
// connection to signalR websocket fails.
func connectSignalRWebsocket(host, endpoint string, tlsConfig *tls.Config) (websocketFeed, error) {
	params := map[string]string{
		"clientProtocol": defaultClientProtocol,
	}

	// Step 1: Negotiate with the signalR server to receive a connection token.
	sn := new(signalRNegotiation)
	err := fetch(http.MethodGet, makeURL("negotiate", host, endpoint, params), sn)
	if err != nil {
		return nil, err
	}

	// Step 2: Connect to signalR websocket.
	params["transport"] = "webSockets"
	params["connectionToken"] = sn.ConnectionToken

	cfg := &socketConfig{
		address:   makeURL("connect", host, endpoint, params),
		tlsConfig: tlsConfig,
		headers: map[string][]string{
			"User-Agent": {fauxBrowserUA},
		},
	}

	var success bool
	ws, err := newSocketConnection(cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if success {
			return
		}

		// Gracefully close this websocket connection if we encounter an error
		// below.
		ws.Close()
	}()

	// Step 3: Start the connection before returning the websocket connection.
	// The websocket connection can be used without this step but we'd like to
	// keep this step to be sure we can successfully received websocket messages
	// from the signalR server.
	confirmation := &struct{ Response string }{}
	err = fetch(http.MethodGet, makeURL("start", host, endpoint, params), confirmation)
	if err != nil {
		return nil, err
	}

	// Wait for the init message.
	initMsg, err := ws.Read()
	if err != nil {
		return nil, err
	}

	// Extract the server message.
	var msg signalRMessage
	err = json.Unmarshal(initMsg, &msg)
	if err != nil {
		return nil, fmt.Errorf("json.Unmarshal error: %w", err)
	}

	serverInitialized := 1
	if msg.S != serverInitialized {
		return nil, fmt.Errorf("unexpected S value received from server: %d | message: %s", msg.S, string(initMsg))
	}

	success = true
	return ws, nil
}

// makeURL is used to construct a signalR connection URL for the action
// specified.
func makeURL(action, host, endpoint string, params map[string]string) string {
	var u url.URL
	u.Scheme = "https"
	u.Host = host
	u.Path = endpoint

	param := url.Values{}
	for key, value := range params {
		param.Set(key, value)
	}

	switch action {
	case "negotiate":
		u.Path += "/negotiate"
	case "connect":
		u.Path += "/connect"
		u.Scheme = "wss"
		param.Set("tid", fmt.Sprintf("%.0f", math.Floor(rand.Float64()*11)))
	case "start":
		u.Path += "/start"
	}

	u.RawQuery = param.Encode()
	return u.String()
}

// fetch makes a request to the provided url and decodes the request response
// into response.
func fetch(method, url string, response interface{}) error {
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return fmt.Errorf("http.NewRequestWithContext error: %w", err)
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("http.DefaultClient.Do error: %w", err)
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(response)
	if err != nil {
		return fmt.Errorf("Failed to decode json from %s: %w", request.URL.String(), err)
	}

	return nil
}
