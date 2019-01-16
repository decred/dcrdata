package types

import (
	exptypes "github.com/decred/dcrdata/v4/explorer/types"
)

// WebSocketMessage represents the JSON object used to send and receive typed
// messages to the web client.
type WebSocketMessage struct {
	EventId string `json:"event"`
	Message string `json:"message"`
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
	SigSyncStatus
)

// Event type field for an event.
var eventIDs = map[HubSignal]string{
	SigSubscribe:        "subscribe",
	SigUnsubscribe:      "unsubscribe",
	SigNewBlock:         "newblock",
	SigMempoolUpdate:    "mempool",
	SigPingAndUserCount: "ping",
	SigNewTx:            "newtx",
	SigSyncStatus:       "blockchainSync",
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

// var (
// 	SigNewBlock         = "newblock" // exptypes.WebsocketBlock
// 	SigMempoolUpdate    = "mempool"  // exptypes.MempoolShort
// 	SigPingAndUserCount = "ping"     // string (number of connected clients)
// 	SigNewTx            = "newtx"    // TxList a.k.a. []*exptypes.MempoolTx
// 	//SigSyncStatus       = "blockchainSync"
// )
