// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package insight

import (
	"regexp"
	"sync"

	"github.com/googollee/go-socket.io"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/blockdata"
	"github.com/decred/dcrdata/txhelpers"
)

var isAlphaNumeric = regexp.MustCompile(`^[a-zA-Z0-9]+$`).MatchString

// SocketServer wraps the socket.io server with the watched address list
type SocketServer struct {
	socketio.Server
	params           *chaincfg.Params
	watchedAddresses map[string]int
	addressesMtx     *sync.RWMutex
}

// NewSocketServer creates and returns new instance of the SocketServer
func NewSocketServer(newTxChan chan *NewTx, params *chaincfg.Params) (*SocketServer, error) {
	server, err := socketio.NewServer(nil)
	if err != nil {
		apiLog.Errorf("Could not create socket.io server: %v", err)
		return nil, err
	}

	addrMtx := new(sync.RWMutex)
	addrs := make(map[string]int)
	server.On("connection", func(so socketio.Socket) {
		apiLog.Debug("New socket.io connection")
		so.Join("inv")
		so.Join("sync")
		so.On("disconnection", func() {
			apiLog.Debug("socket.io client disconnected")
			addrMtx.Lock()
			for _, str := range so.Rooms() {
				if c, ok := addrs[str]; ok {
					if c == 1 {
						delete(addrs, str)
					} else {
						addrs[str]--
					}
				}
			}
			addrMtx.Unlock()
		})
		so.On("subscribe", func(room string) {
			if len(room) > 64 || !isAlphaNumeric(room) {
				return
			}
			if addr, err := dcrutil.DecodeAddress(room); err == nil {
				if addr.IsForNet(params) {
					so.Join(room)
					apiLog.Debugf("socket.io client joining room: %s", room)

					addrMtx.Lock()
					addrs[room]++
					addrMtx.Unlock()
				}
			}
		})
	})

	server.On("error", func(so socketio.Socket, err error) {
		apiLog.Errorf("Insight socket.io server error: %v", err)
	})

	sockServ := SocketServer{
		Server:           *server,
		params:           params,
		watchedAddresses: addrs,
		addressesMtx:     addrMtx,
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
		vouts := make(map[string]int64)
		var total int64
		for i, v := range msgTx.TxOut {
			total += v.Value
			if len(ntx.Vouts[i].ScriptPubKey.Addresses) != 0 {
				soc.addressesMtx.RLock()
				for _, address := range ntx.Vouts[i].ScriptPubKey.Addresses {
					if _, ok := soc.watchedAddresses[address]; ok {
						soc.BroadcastTo(address, address, hash)
					}
					vouts[address] = v.Value
				}
				soc.addressesMtx.RUnlock()
			}
		}
		tx := WebSocketTx{
			Hash:     hash,
			Size:     len(ntx.Hex) / 2,
			TotalOut: total,
			Vout:     vouts,
		}
		apiLog.Tracef("Sending new websocket tx %s", hash)
		soc.BroadcastTo("inv", "tx", tx)
	}
}

// WebSocketTx models the json data send as the tx event in the inv room
type WebSocketTx struct {
	Hash     string           `json:"txid"`
	Size     int              `json:"size"`
	TotalOut int64            `json:"valueOut"`
	Vout     map[string]int64 `json:"vout,omitempty"`
}

// NewTx models data from the notification handler
type NewTx struct {
	Hex   string
	Vouts []dcrjson.Vout
}
