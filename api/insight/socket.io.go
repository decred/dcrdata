// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package insight

import (
	"encoding/json"
	"regexp"
	"sync"

	"github.com/googollee/go-socket.io"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v3/blockdata"
	"github.com/decred/dcrdata/v3/txhelpers"
)

var isAlphaNumeric = regexp.MustCompile(`^[a-zA-Z0-9]+$`).MatchString

type roomSubscriptionCounter struct {
	*sync.RWMutex
	c map[string]int
}

// SocketServer wraps the socket.io server with the watched address list.
type SocketServer struct {
	socketio.Server
	params           *chaincfg.Params
	watchedAddresses roomSubscriptionCounter
}

// InsightSocketVout represents a single vout for the Insight "vout" JSON object
// that appears in a "tx" message from the "inv" room.
type InsightSocketVout struct {
	Address string
	Value   int64
}

// MarshalJSON implements json.Marshaler so that an InsightSocketVout will
// marshal to JSON like:
//	{
//	  "DsZQaCQES5vh3JmcyyFokJYz3aSw8Sm1dsQ": 13741789
//	}
func (v *InsightSocketVout) MarshalJSON() ([]byte, error) {
	vout := map[string]int64{
		v.Address: v.Value,
	}
	return json.Marshal(vout)
}

// WebSocketTx models the JSON data sent as the tx event in the inv room.
type WebSocketTx struct {
	Hash     string              `json:"txid"`
	Size     int                 `json:"size"`
	TotalOut int64               `json:"valueOut"`
	Vouts    []InsightSocketVout `json:"vout,omitempty"`
}

// NewTx models data from the notification handler
type NewTx struct {
	Hex   string
	Vouts []dcrjson.Vout
}

// NewSocketServer constructs a new SocketServer, registering handlers for the
// "connection", "disconnection", and "subscribe" events.
func NewSocketServer(newTxChan chan *NewTx, params *chaincfg.Params) (*SocketServer, error) {
	server, err := socketio.NewServer(nil)
	if err != nil {
		apiLog.Errorf("Could not create socket.io server: %v", err)
		return nil, err
	}

	// Each address subscription uses its own room, which has the same name as
	// the address. The number of subscribers for each room is tracked.
	addrs := roomSubscriptionCounter{
		RWMutex: new(sync.RWMutex),
		c:       make(map[string]int),
	}

	server.On("connection", func(so socketio.Socket) {
		apiLog.Debug("New socket.io connection")
		// New connections automatically join the inv and sync rooms.
		so.Join("inv")
		so.Join("sync")

		// Disconnection decrements or deletes the subscriber counter for each
		// address room to which the client was subscribed.
		so.On("disconnection", func() {
			apiLog.Debug("socket.io client disconnected")
			addrs.Lock()
			for _, str := range so.Rooms() {
				if c, ok := addrs.c[str]; ok {
					if c == 1 {
						delete(addrs.c, str)
					} else {
						addrs.c[str]--
					}
				}
			}
			addrs.Unlock()
		})

		// Subscription to a room checks the room name is as expected for an
		// address (TODO: do this better), joins the room, and increments the
		// room's subscriber count.
		so.On("subscribe", func(room string) {
			if len(room) > 64 || !isAlphaNumeric(room) {
				return
			}
			if addr, err := dcrutil.DecodeAddress(room); err == nil {
				if addr.IsForNet(params) {
					so.Join(room)
					apiLog.Debugf("socket.io client joining room: %s", room)

					addrs.Lock()
					addrs.c[room]++
					addrs.Unlock()
				}
			}
		})
	})

	server.On("error", func(_ socketio.Socket, err error) {
		apiLog.Errorf("Insight socket.io server error: %v", err)
	})

	sockServ := SocketServer{
		Server:           *server,
		params:           params,
		watchedAddresses: addrs,
	}
	go sockServ.sendNewTx(newTxChan)
	return &sockServ, nil
}

// Store broadcasts the lastest block hash to the the inv room
func (soc *SocketServer) Store(blockData *blockdata.BlockData, _ *wire.MsgBlock) error {
	apiLog.Debugf("Sending new websocket block %s", blockData.Header.Hash)
	soc.BroadcastTo("inv", "block", blockData.Header.Hash)
	return nil
}

func (soc *SocketServer) sendNewTx(newTxChan chan *NewTx) {
	for {
		ntx, ok := <-newTxChan
		if !ok {
			break
		}
		msgTx, err := txhelpers.MsgTxFromHex(ntx.Hex)
		if err != nil {
			continue
		}
		hash := msgTx.TxHash().String()
		var vouts []InsightSocketVout
		var total int64
		for i, v := range msgTx.TxOut {
			total += v.Value
			if len(ntx.Vouts[i].ScriptPubKey.Addresses) != 0 {
				soc.watchedAddresses.RLock()
				for _, address := range ntx.Vouts[i].ScriptPubKey.Addresses {
					if _, ok := soc.watchedAddresses.c[address]; ok {
						soc.BroadcastTo(address, address, hash)
					}
					vouts = append(vouts, InsightSocketVout{
						Address: address,
						Value:   v.Value,
					})
				}
				soc.watchedAddresses.RUnlock()
			}
		}
		tx := WebSocketTx{
			Hash:     hash,
			Size:     len(ntx.Hex) / 2,
			TotalOut: total,
			Vouts:    vouts,
		}
		apiLog.Tracef("Sending new websocket tx %s", hash)
		soc.BroadcastTo("inv", "tx", tx)
	}
}
