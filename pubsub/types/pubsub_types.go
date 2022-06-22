package types

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/decred/base58"
	exptypes "github.com/decred/dcrdata/v8/explorer/types"
)

// Ver is a json tagged version type.
type Ver struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
	Patch uint32 `json:"patch"`
}

// NewVer creates a Ver from the major/minor/patch version components.
func NewVer(major, minor, patch uint32) Ver {
	return Ver{major, minor, patch}
}

// String implements Stringer for Ver.
func (v Ver) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

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
// DEPRECATED.
func IsTemporaryErr(err error) bool {
	t, ok := err.(net.Error)
	return ok && t.Temporary() //nolint:staticcheck
}

// WebSocketMessage represents the JSON object used to send and receive typed
// messages to the web client.
type WebSocketMessage struct {
	EventId string          `json:"event"`
	Message json.RawMessage `json:"message"`
}

type AddressMessage struct {
	Address string `json:"address"`
	TxHash  string `json:"transaction"`
}

type RequestMessage struct {
	RequestId int64  `json:"request_id"`
	Message   string `json:"message"`
}

type ResponseMessage struct {
	Success        bool   `json:"success"`
	RequestEventId string `json:"request_event"`
	RequestId      int64  `json:"request_id"`
	Data           string `json:"data"`
}

func (am AddressMessage) String() string {
	return am.Address + ":" + am.TxHash
}

type TxList []*exptypes.MempoolTx

type HangUp struct{}

type HubSignal int

// These are the different signal types used for passing messages between the
// client and server, and internally between the pubsub and websocket hubs.
const (
	SigSubscribe HubSignal = iota
	SigUnsubscribe
	SigDecodeTx
	SigGetMempoolTxs
	SigSendTx
	SigVersion
	SigNewBlock
	SigMempoolUpdate
	SigPingAndUserCount
	SigNewTx
	SigNewTxs
	SigAddressTx
	SigSyncStatus
	SigByeNow
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
	SigDecodeTx:         "decodetx",
	SigGetMempoolTxs:    "getmempooltxs",
	SigSendTx:           "sendtx",
	SigVersion:          "getversion",
	SigNewBlock:         "newblock",
	SigMempoolUpdate:    "mempool",
	SigPingAndUserCount: "ping",
	SigNewTx:            "newtx",
	SigNewTxs:           "newtxs",
	SigAddressTx:        "address",
	SigSyncStatus:       "blockchainSync",
	SigByeNow:           "bye",
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
		_, _, err := base58.CheckDecode(msgStr)
		if err != nil {
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
