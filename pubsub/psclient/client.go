package client

import (
	"encoding/json"
	"fmt"
	"time"

	exptypes "github.com/decred/dcrdata/v4/explorer/types"
	pstypes "github.com/decred/dcrdata/v4/pubsub/types"
	"golang.org/x/net/websocket"
)

func newSubscribeMsg(event string) string {
	return fmt.Sprintf(`{"event":"subscribe","message":"%s"}`, event)
}

func newUnsubscribeMsg(event string) string {
	return fmt.Sprintf(`{"event":"unsubscribe","message":"%s"}`, event)
}

var defaultTimeout = 10 * time.Second

// Client wraps a *websocket.Conn.
type Client struct {
	*websocket.Conn
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// New creates a new Client from a *websocket.Conn.
func New(ws *websocket.Conn) *Client {
	if ws == nil {
		return nil
	}
	return &Client{
		Conn:         ws,
		ReadTimeout:  defaultTimeout,
		WriteTimeout: defaultTimeout,
	}
}

// Subscribe sends a subscribe type WebSocketMessage for the given event name
// after validating it. The response is returned.
func (c *Client) Subscribe(event string) (*pstypes.WebSocketMessage, error) {
	// Validate the event type.
	_, ok := pstypes.Subscriptions[event]
	if !ok {
		return nil, fmt.Errorf("invalid subscription %s", event)
	}

	// Send the subscribe message.
	msg := newSubscribeMsg(event)
	_ = c.SetWriteDeadline(time.Now().Add(c.WriteTimeout))
	_, err := c.Write([]byte(msg))
	if err != nil {
		return nil, fmt.Errorf("failed to send subscribe message: %v", err)
	}

	// Read the response.
	return c.ReceiveMsg()
}

// Unsubscribe sends an unsubscribe type WebSocketMessage for the given event
// name after validating it. The response is returned.
func (c *Client) Unsubscribe(event string) (*pstypes.WebSocketMessage, error) {
	// Validate the event type.
	_, ok := pstypes.Subscriptions[event]
	if !ok {
		return nil, fmt.Errorf("invalid subscription %s", event)
	}

	// Send the unsubscribe message.
	msg := newUnsubscribeMsg(event)
	_ = c.SetWriteDeadline(time.Now().Add(c.WriteTimeout))
	_, err := c.Write([]byte(msg))
	if err != nil {
		return nil, fmt.Errorf("failed to send unsubscribe message: %v", err)
	}

	// Read the response.
	return c.ReceiveMsg()
}

// ReceiveMsgTimeout waits for the specified time Duration for a message,
// returned decoded into a WebSocketMessage.
func (c *Client) ReceiveMsgTimeout(timeout time.Duration) (*pstypes.WebSocketMessage, error) {
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	msg := new(pstypes.WebSocketMessage)
	if err := websocket.JSON.Receive(c.Conn, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// ReceiveMsg waits for a message, returned decoded into a WebSocketMessage. The
// Client's configured ReadTimeout is used.
func (c *Client) ReceiveMsg() (*pstypes.WebSocketMessage, error) {
	return c.ReceiveMsgTimeout(c.ReadTimeout)
}

// ReceiveRaw for a message, returned undecoded as a string.
func (c *Client) ReceiveRaw() (message string, err error) {
	_ = c.SetReadDeadline(time.Now().Add(c.ReadTimeout))
	err = websocket.Message.Receive(c.Conn, &message)
	return
}

// DecodeMsg attempts to decode the Message content of the given
// WebSocketMessage based on its EventId. The type contained in the returned
// interface{} depends on the EventId.
func DecodeMsg(msg *pstypes.WebSocketMessage) (interface{}, error) {
	if msg == nil {
		return nil, fmt.Errorf("empty message")
	}

	switch msg.EventId {
	// Event types with a raw string Message field
	case "subscribeResp", "unsubscribeResp", "ping":
		return msg.Message, nil
	case "newtx":
		var newtxs pstypes.TxList
		err := json.Unmarshal([]byte(msg.Message), &newtxs)
		return &newtxs, err
	case "newblock":
		var newblock exptypes.WebsocketBlock
		err := json.Unmarshal([]byte(msg.Message), &newblock)
		return &newblock, err
	case "mempool":
		var mpshort exptypes.MempoolShort
		err := json.Unmarshal([]byte(msg.Message), &mpshort)
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

// DecodeMsgPing attempts to decode the Message content of the given
// WebSocketMessage ping response message (string).
func DecodeMsgPing(msg *pstypes.WebSocketMessage) (string, error) {
	return DecodeMsgString(msg)
}

// DecodeMsgSubscribeResponse attempts to decode the Message content of the
// given WebSocketMessage as a subscribe response message (string).
func DecodeMsgSubscribeResponse(msg *pstypes.WebSocketMessage) (string, error) {
	return DecodeMsgString(msg)
}

// DecodeMsgUnsubscribeResponse attempts to decode the Message content of the
// given WebSocketMessage as an unsubscribe response message (string).
func DecodeMsgUnsubscribeResponse(msg *pstypes.WebSocketMessage) (string, error) {
	return DecodeMsgString(msg)
}

// DecodeMsgTxList attempts to decode the Message content of the given
// WebSocketMessage as a newtx message (*pstypes.TxList).
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
