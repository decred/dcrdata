package psclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	exptypes "github.com/decred/dcrdata/v8/explorer/types"
	pubsub "github.com/decred/dcrdata/v8/pubsub"
	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
	"github.com/decred/dcrdata/v8/semver"
	"golang.org/x/net/websocket"
)

// Version indicates the semantic version of the pubsub module, to which the
// psclient belongs.
func Version() semver.Semver {
	return pubsub.Version()
}

func makeRequestMsg(event string, reqID int64) []byte {
	event = strings.Trim(event, `"`)

	reqMsg, err := json.Marshal(pstypes.RequestMessage{
		RequestId: reqID,
		Message:   event,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to json.Marshal a RequestMessage: %v", err))
	}
	return reqMsg
}

// newSubscribeMsg creates a new subscribe message equivalent to marshaling a
// pstypes.WebSocketMessage with EventId set to "subscribe" and Message set to
// the input string, event.
func newSubscribeMsg(event string, reqID int64) []byte {
	subMsg, err := json.Marshal(pstypes.WebSocketMessage{
		EventId: "subscribe",
		Message: makeRequestMsg(event, reqID),
	})
	if err != nil {
		panic(fmt.Sprintf("failed to json.Marshal a WebSocketMessage: %v", err))
	}

	return subMsg
}

// newUnsubscribeMsg creates a new unsubscribe message equivalent to marshaling
// a pstypes.WebSocketMessage with EventId set to "unsubscribe" and Message set
// to the input string, event.
func newUnsubscribeMsg(event string, reqID int64) []byte {
	unsubMsg, err := json.Marshal(pstypes.WebSocketMessage{
		EventId: "unsubscribe",
		Message: makeRequestMsg(event, reqID),
	})
	if err != nil {
		panic(fmt.Sprintf("failed to json.Marshal a WebSocketMessage: %v", err))
	}

	return unsubMsg
}

// newServerVersionMsg creates a new server version query with EventId set to
// "version", and request message content generated for the specified reqID.
func newServerVersionMsg(reqID int64) []byte {
	verMsg, err := json.Marshal(pstypes.WebSocketMessage{
		EventId: "version",
		Message: makeRequestMsg("", reqID),
	})
	if err != nil {
		panic(fmt.Sprintf("failed to json.Marshal a WebSocketMessage: %v", err))
	}

	return verMsg
}

// newPingMsg creates a new ping message with EventId set to "ping", and request
// message content generated for the specified reqID.
func newPingMsg(reqID int64) []byte {
	pingMsg, err := json.Marshal(pstypes.WebSocketMessage{
		EventId: "ping",
		Message: makeRequestMsg("", reqID),
	})
	if err != nil {
		panic(fmt.Sprintf("failed to json.Marshal a WebSocketMessage: %v", err))
	}

	return pingMsg
}

const (
	DefaultReadTimeout  = pubsub.PingInterval * 10 / 9
	DefaultWriteTimeout = 5 * time.Second
)

// Opts defines the psclient Client options.
type Opts struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Client wraps a *websocket.Conn.
type Client struct {
	*websocket.Conn
	ourConn       bool
	readTimeout   time.Duration
	writeTimeout  time.Duration
	reqMtx        sync.Mutex
	recvMsgChan   chan *ClientMessage
	nextRequestID int64
	requests      map[int64]chan *pstypes.ResponseMessage
	sendMtx       sync.Mutex
	ctx           context.Context
	shutdown      context.CancelFunc
}

// New creates a new Client from a URL.
func New(url string, ctx context.Context, opts *Opts) (*Client, error) {
	ws, err := websocket.Dial(url, "", "/")
	if err != nil {
		return nil, err
	}

	readTimeout, writeTimeout := DefaultReadTimeout, DefaultWriteTimeout
	if opts != nil {
		readTimeout = opts.ReadTimeout
		writeTimeout = opts.WriteTimeout
	}

	ctx, shutdown := context.WithCancel(ctx)
	cl := &Client{
		Conn:         ws,
		ourConn:      true,
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
		recvMsgChan:  make(chan *ClientMessage, 16),
		requests:     make(map[int64]chan *pstypes.ResponseMessage),
		ctx:          ctx,
		shutdown:     shutdown,
	}

	go cl.receiver()

	// Query for the server's pubsub version.
	serverVer, err := cl.ServerVersion()
	if err != nil {
		cl.Stop()
		return nil, fmt.Errorf("failed to get server pubsub version: %v", err)
	}
	log.Infof("Server pubsub version: %s\n", serverVer)

	// Ensure the server's pubsub version (actual) is compatible with the
	// client's version (required). This allows the client to have a high minor
	// version for equal major versions.
	clientSemVer := Version()
	serverSemVer := semver.NewSemver(serverVer.Major, serverVer.Minor, serverVer.Patch)
	if !semver.Compatible(clientSemVer, serverSemVer) {
		cl.Stop()
		return nil, fmt.Errorf("server pubsub version is %v, but client is version %v",
			serverSemVer, clientSemVer)
	}

	return cl, nil
}

// NewFromConn creates a new Client from a *websocket.Conn.
func NewFromConn(ws *websocket.Conn, ctx context.Context, opts *Opts) *Client {
	if ws == nil {
		return nil
	}

	readTimeout, writeTimeout := DefaultReadTimeout, DefaultWriteTimeout
	if opts != nil {
		readTimeout = opts.ReadTimeout
		writeTimeout = opts.WriteTimeout
	}

	ctx, shutdown := context.WithCancel(ctx)
	cl := &Client{
		Conn:         ws,
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
		recvMsgChan:  make(chan *ClientMessage, 16),
		requests:     make(map[int64]chan *pstypes.ResponseMessage),
		ctx:          ctx,
		shutdown:     shutdown,
	}

	go cl.receiver()

	return cl
}

// Stop shutsdown the Client. If the websocket connection was created during
// Client construction, it is is shutdown too.
func (c *Client) Stop() {
	log.Trace("Stopping psclient Client...")

	// Cancel the Context to stop the receiver loop, which closes the channel.
	c.shutdown()

	// Close the websocket connection.
	if c.ourConn {
		if err := c.Conn.Close(); err != nil {
			log.Errorf("Failed to Close websocket connection: %v", err)
		}
	}
}

// ClientMessage represents a message for a client connection.  The
// (*Client).Receive method provides a channel such messages from the server.
type ClientMessage struct {
	EventId string
	Message interface{}
}

// Receive gets a receive-only *ClientMessage channel, through which all
// messages from the server to the client should be received.
func (c *Client) Receive() <-chan *ClientMessage {
	return c.recvMsgChan
}

// receiver receives and decodes messages from the server. It is to be run as a
// goroutine by the constructor of the Client. The decoded ClientMessages are
// sent on the recvMsgChan, the channel that is obtained via Receive.
func (c *Client) receiver() {
	// This goroutine may return on its own (e.g. error), or if the parent
	// context is cancelled. In case of the former, call the Context's cancel
	// function. In the latter case, it is a no-op.
	defer c.shutdown()

	// This goroutine sends on recvMsgChan, so it is responsible for closing it.
	defer close(c.recvMsgChan)

	for {
		if c.ctx.Err() != nil {
			log.Trace("receiver: context canceled...")
			return
		}

		resp, err := c.receiveMsg()
		if err != nil {
			// Even a timeout should close shutdown the client since that
			// indicates pings from the server did not arrive in time.
			log.Errorf("ReceiveMsg failed: %v", err)
			return
		}

		msg, err := DecodeMsg(resp)
		if err != nil {
			log.Errorf("Failed to decode message: %v", err)
			continue
		}

		switch m := msg.(type) {
		case *pstypes.ResponseMessage:
			log.Debugf("Response to %s request ID=%d received. Success = %v. Data: %v",
				m.RequestEventId, m.RequestId, m.Success, m.Data)
			respChan := c.responseChan(m.RequestId)
			if respChan == nil {
				log.Errorf("receiver failed to find request ID %d", m.RequestId)
				continue
			}
			go func() {
				respChan <- m // unbuffered
				close(respChan)
				c.deleteRequestID(m.RequestId)
			}()
			continue
		case *pstypes.HangUp:
			log.Infof("The server is hanging up on us! Shutting down.")
			c.recvMsgChan <- &ClientMessage{
				EventId: resp.EventId,
				Message: msg,
			}
			return
		case string:
			// generic "message"
			log.Debugf("Message (%s): %s", resp.EventId, m)
		case int:
			// e.g. "ping"
			log.Debugf("Message (%s): %d", resp.EventId, m)
		case *exptypes.WebsocketBlock:
			log.Debugf("Message (%s): WebsocketBlock(hash=%s)", resp.EventId, m.Block.Hash)
		case *exptypes.MempoolShort:
			t := time.Unix(m.Time, 0)
			log.Debugf("Message (%s): MempoolShort(numTx=%d, time=%v)",
				resp.EventId, m.NumAll, t)
		case *pstypes.TxList:
			log.Debugf("Message (%s): TxList(len=%d)", resp.EventId, len(*m))
		case *pstypes.AddressMessage:
			log.Debugf("Message (%s): AddressMessage(address=%s, txHash=%s)",
				resp.EventId, m.Address, m.TxHash)
		default:
			log.Debugf("Message of type %v unhandled.", resp.EventId)
			continue
		}

		c.recvMsgChan <- &ClientMessage{
			EventId: resp.EventId,
			Message: msg,
		}
	}
}

func (c *Client) send(msg []byte) error {
	c.sendMtx.Lock()
	defer c.sendMtx.Unlock()
	_ = c.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	_, err := c.Write(msg)
	return err
}

func (c *Client) responseChan(reqID int64) chan *pstypes.ResponseMessage {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	return c.requests[reqID]
}

func (c *Client) newResponseChan() (chan *pstypes.ResponseMessage, int64) {
	c.reqMtx.Lock()
	reqID := c.nextRequestID
	c.nextRequestID++
	respChan := make(chan *pstypes.ResponseMessage)
	c.requests[reqID] = respChan
	c.reqMtx.Unlock()
	return respChan, reqID
}

func (c *Client) deleteRequestID(reqID int64) {
	c.reqMtx.Lock()
	delete(c.requests, reqID)
	c.reqMtx.Unlock()
}

// Subscribe sends a subscribe type WebSocketMessage for the given event name
// after validating it. The response is returned.
func (c *Client) Subscribe(event string) (*pstypes.ResponseMessage, error) {
	// Validate the event type.
	sig, _, ok := pstypes.ValidateSubscription(event)
	if !ok {
		return nil, fmt.Errorf("invalid subscription %s", event)
	}

	if sig == pstypes.SigPingAndUserCount {
		log.Warn("Pings from the server no longer require a subscription. " +
			"Pings are sent to all clients.")
		// Let the request go through.
	}

	respChan, reqID := c.newResponseChan()
	msg := newSubscribeMsg(event, reqID)
	defer c.deleteRequestID(reqID)

	// Send the subscribe message.
	log.Tracef("Sending subscribe %s message...", event)
	if err := c.send(msg); err != nil {
		return nil, fmt.Errorf("failed to send subscribe message: %v", err)
	}

	// Wait for a response with the requestID.
	log.Tracef("Waiting for subscribe %s response...", event)
	resp, ok := <-respChan
	if !ok {
		return nil, fmt.Errorf("Response channel closed.")
	}

	// Read the response.
	return resp, nil
}

// Unsubscribe sends an unsubscribe type WebSocketMessage for the given event
// name after validating it. The response is returned.
func (c *Client) Unsubscribe(event string) (*pstypes.ResponseMessage, error) {
	// Validate the event type.
	_, _, ok := pstypes.ValidateSubscription(event)
	if !ok {
		return nil, fmt.Errorf("invalid subscription %s", event)
	}

	respChan, reqID := c.newResponseChan()
	msg := newUnsubscribeMsg(event, reqID)
	defer c.deleteRequestID(reqID)

	// Send the subscribe message.
	if err := c.send(msg); err != nil {
		return nil, fmt.Errorf("failed to send unsubscribe message: %v", err)
	}

	// Wait for a response with the requestID.
	resp := <-respChan

	// Read the response.
	return resp, nil
}

// ServerVersion sends a server version query, and returns the response.
func (c *Client) ServerVersion() (*pstypes.Ver, error) {
	respChan, reqID := c.newResponseChan()
	msg := newServerVersionMsg(reqID)
	defer c.deleteRequestID(reqID)

	// Send the server version message.
	if err := c.send(msg); err != nil {
		return nil, fmt.Errorf("failed to send unsubscribe message: %v", err)
	}

	// Wait for a response with the requestID
	resp := <-respChan
	if !resp.Success {
		return nil, fmt.Errorf("failed to obtain server version")
	}

	var ver pstypes.Ver
	if err := json.Unmarshal([]byte(resp.Data), &ver); err != nil {
		return nil, fmt.Errorf("failed to decode server version response: %v", err)
	}

	// Read the response.
	return &ver, nil
}

// Ping sends a ping to the server. There is no response.
func (c *Client) Ping() error {
	_, reqID := c.newResponseChan()
	msg := newPingMsg(reqID)
	defer c.deleteRequestID(reqID)

	// Send the server version message.
	if err := c.send(msg); err != nil {
		return fmt.Errorf("failed to send ping message: %v", err)
	}
	return nil
}

// receiveMsgTimeout waits for the specified time Duration for a message,
// returned decoded into a WebSocketMessage.
func (c *Client) receiveMsgTimeout(timeout time.Duration) (*pstypes.WebSocketMessage, error) {
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	msg := new(pstypes.WebSocketMessage)
	if err := websocket.JSON.Receive(c.Conn, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// receiveMsg waits for a message, returned decoded into a WebSocketMessage. The
// Client's configured ReadTimeout is used.
func (c *Client) receiveMsg() (*pstypes.WebSocketMessage, error) {
	return c.receiveMsgTimeout(c.readTimeout)
}

// DecodeMsg attempts to decode the Message content of the given
// WebSocketMessage based on its EventId. The type contained in the returned
// interface{} depends on the EventId.
func DecodeMsg(msg *pstypes.WebSocketMessage) (interface{}, error) {
	if msg == nil {
		return nil, fmt.Errorf("empty message")
	}

	if strings.HasSuffix(msg.EventId, "Resp") {
		var rm pstypes.ResponseMessage
		err := json.Unmarshal(msg.Message, &rm)
		return &rm, err
	}

	switch msg.EventId {
	case "message":
		var message string
		err := json.Unmarshal(msg.Message, &message)
		return message, err
	case "bye":
		return &pstypes.HangUp{}, nil
	case "ping":
		var numClients int
		err := json.Unmarshal(msg.Message, &numClients)
		return numClients, err
	case "address":
		var am pstypes.AddressMessage
		err := json.Unmarshal(msg.Message, &am)
		return &am, err
	case "newtxs":
		var newtxs pstypes.TxList
		err := json.Unmarshal(msg.Message, &newtxs)
		return &newtxs, err
	case "newblock":
		var newblock exptypes.WebsocketBlock
		err := json.Unmarshal(msg.Message, &newblock)
		return &newblock, err
	case "mempool":
		var mpshort exptypes.MempoolShort
		err := json.Unmarshal(msg.Message, &mpshort)
		return &mpshort, err
	default:
		return nil, fmt.Errorf("unrecognized event type")
	}
}

// DecodeMsgString attempts to decode the Message content of the given
// WebSocketMessage as a string.
func DecodeMsgString(msg *pstypes.WebSocketMessage) (string, error) {
	s, err := DecodeMsg(msg)
	if err != nil {
		return "", err
	}
	str, ok := s.(string)
	if !ok {
		return "", fmt.Errorf("content of Message was not of type string")
	}
	return str, nil
}

// DecodeMsgInt attempts to decode the Message content of the given
// WebSocketMessage as an int.
func DecodeMsgInt(msg *pstypes.WebSocketMessage) (int, error) {
	s, err := DecodeMsg(msg)
	if err != nil {
		return -1, err
	}
	i, ok := s.(int)
	if !ok {
		return -1, fmt.Errorf("content of Message was not of type int")
	}
	return i, nil
}

// DecodeMsgPing attempts to decode the Message content of the given
// WebSocketMessage ping response message (int).
func DecodeMsgPing(msg *pstypes.WebSocketMessage) (int, error) {
	return DecodeMsgInt(msg)
}

// DecodeResponseMsg attempts to decode the Message content of the given
// WebSocketMessage as a request response message (string).
func DecodeResponseMsg(msg *pstypes.WebSocketMessage) (*pstypes.ResponseMessage, error) {
	respMsg, err := DecodeMsg(msg)
	if err != nil {
		return nil, err
	}
	respMessage, ok := respMsg.(*pstypes.ResponseMessage)
	if !ok {
		return nil, fmt.Errorf("content of Message was not of type *pstypes.ResponseMessage")
	}
	return respMessage, nil
}

// DecodeMsgTxList attempts to decode the Message content of the given
// WebSocketMessage as a newtxs message (*pstypes.TxList).
func DecodeMsgTxList(msg *pstypes.WebSocketMessage) (*pstypes.TxList, error) {
	txl, err := DecodeMsg(msg)
	if err != nil {
		return nil, err
	}
	txlist, ok := txl.(*pstypes.TxList)
	if !ok {
		return nil, fmt.Errorf("content of Message was not of type *pstypes.TxList")
	}
	return txlist, nil
}

// DecodeMsgMempool attempts to decode the Message content of the given
// WebSocketMessage as a mempool message (*exptypes.MempoolShort).
func DecodeMsgMempool(msg *pstypes.WebSocketMessage) (*exptypes.MempoolShort, error) {
	mps, err := DecodeMsg(msg)
	if err != nil {
		return nil, err
	}
	mpShort, ok := mps.(*exptypes.MempoolShort)
	if !ok {
		return nil, fmt.Errorf("content of Message was not of type *exptypes.MempoolShort")
	}
	return mpShort, nil
}

// DecodeMsgNewBlock attempts to decode the Message content of the given
// WebSocketMessage as a newblock message (*exptypes.WebsocketBlock).
func DecodeMsgNewBlock(msg *pstypes.WebSocketMessage) (*exptypes.WebsocketBlock, error) {
	nb, err := DecodeMsg(msg)
	if err != nil {
		return nil, err
	}
	newBlock, ok := nb.(*exptypes.WebsocketBlock)
	if !ok {
		return nil, fmt.Errorf("content of Message was not of type *exptypes.WebsocketBlock")
	}
	return newBlock, nil
}

// DecodeMsgNewAddressTx attempts to decode the Message content of the given
// WebSocketMessage as an address message (*DecodeMsgNewAddressTx).
func DecodeMsgNewAddressTx(msg *pstypes.WebSocketMessage) (*pstypes.AddressMessage, error) {
	nb, err := DecodeMsg(msg)
	if err != nil {
		return nil, err
	}
	am, ok := nb.(*pstypes.AddressMessage)
	if !ok {
		return nil, fmt.Errorf("content of Message was not of type *pstypes.AddressMessage")
	}
	return am, nil
}
