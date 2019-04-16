package types

import (
	"net"
	"strconv"
	"strings"

	"github.com/decred/dcrd/dcrutil"
	exptypes "github.com/decred/dcrdata/explorer/types"
)

var (
	// ErrWsClosed is the error message when a websocket.(*Conn).Close tries to
	// close an already closed connection. See Go's src/internal/poll/fd.go.
	ErrWsClosed = "use of closed network connection"
)

// IsWSClosedErr checks if the passed error indicates a closed websocket
// connection.
func IsWSClosedErr(err error) (closedErr bool) {
	// Must use strings.Contains to catch errors like "write tcp
	// 127.0.0.1:7777->127.0.0.1:39196: use of closed network connection".
	return err != nil && strings.Contains(err.Error(), ErrWsClosed)
}

// IsIOTimeoutErr checks if the passed error indicates an I/O timeout error.
func IsIOTimeoutErr(err error) bool {
	t, ok := err.(net.Error)
	return ok && t.Timeout()
}

// IsTemporaryErr checks if the passed error indicates a transient error.
func IsTemporaryErr(err error) bool {
	t, ok := err.(net.Error)
	return ok && t.Temporary()
}

// WebSocketMessage represents the JSON object used to send and receive typed
// messages to the web client.
type WebSocketMessage struct {
	EventId string `json:"event"`
	Message string `json:"message"`
}

type AddressMessage struct {
	Address string `json:"address"`
	TxHash  string `json:"transaction"`
}

func (am AddressMessage) String() string {
	return am.Address + ":" + am.TxHash
}

type TxList []*exptypes.MempoolTx

type HubSignal int

const (
	SigSubscribe HubSignal = iota
	SigUnsubscribe
	SigNewBlock
	SigMempoolUpdate
	SigPingAndUserCount
	SigNewTx
	SigNewTxs
	SigAddressTx
	SigSyncStatus
	SigUnknown
)

var Subscriptions = map[string]HubSignal{
	"newblock":       SigNewBlock,
	"mempool":        SigMempoolUpdate,
	"ping":           SigPingAndUserCount,
	"newtxs":         SigNewTxs,
	"address":        SigAddressTx,
	"blockchainSync": SigSyncStatus,
}

// Event type field for an event.
var eventIDs = map[HubSignal]string{
	SigSubscribe:        "subscribe",
	SigUnsubscribe:      "unsubscribe",
	SigNewBlock:         "newblock",
	SigMempoolUpdate:    "mempool",
	SigPingAndUserCount: "ping",
	SigNewTx:            "newtx",
	SigNewTxs:           "newtxs",
	SigAddressTx:        "address",
	SigSyncStatus:       "blockchainSync",
	SigUnknown:          "unknown",
}

func ValidateSubscription(event string) (sub HubSignal, msg interface{}, valid bool) {
	sig, msgStr := event, ""
	idx := strings.Index(event, ":")
	if idx != -1 {
		sig = event[:idx]
		if idx+1 < len(event) {
			msgStr = event[idx+1:]
		}
	}

	sub, valid = Subscriptions[sig]
	if !valid {
		return SigUnknown, nil, valid
	}

	switch sub {
	case SigAddressTx:
		if _, err := dcrutil.DecodeAddress(msgStr); err != nil {
			return SigUnknown, nil, false
		}
		msg = &AddressMessage{
			Address: msgStr,
		}
	default:
		// Other signals do not have a message.
		if msgStr != "" {
			return SigUnknown, nil, false
		}
	}

	return
}

func (s HubSignal) String() string {
	str, found := eventIDs[s]
	if !found {
		return "invalid"
	}
	return str
}

func (s HubSignal) IsValid() bool {
	_, found := eventIDs[s]
	return found
}

type HubMessage struct {
	Signal HubSignal
	Msg    interface{}
}

func (m HubMessage) IsValid() bool {
	_, found := eventIDs[m.Signal]
	if !found {
		return false
	}

	ok := true
	switch m.Signal {
	case SigAddressTx:
		_, ok = m.Msg.(*AddressMessage)
	case SigNewTx:
		_, ok = m.Msg.(*exptypes.MempoolTx)
	case SigNewTxs:
		_, ok = m.Msg.([]*exptypes.MempoolTx)
	}

	return ok
}

func (m HubMessage) String() string {
	if !m.IsValid() {
		return "invalid"
	}

	sigStr := m.Signal.String()

	switch m.Signal {
	case SigAddressTx:
		am := m.Msg.(*AddressMessage)
		sigStr += ":" + am.String()
	case SigNewTx:
		tx := m.Msg.(*exptypes.MempoolTx)
		sigStr += ":" + tx.Hash
	case SigNewTxs:
		txs := m.Msg.([]*exptypes.MempoolTx)
		sigStr += ":len=" + strconv.Itoa(len(txs))
	}

	return sigStr
}
