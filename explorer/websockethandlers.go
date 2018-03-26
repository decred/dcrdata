// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/net/websocket"
)

// RootWebsocket is the websocket handler for all pages
func (exp *explorerUI) RootWebsocket(w http.ResponseWriter, r *http.Request) {
	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		// Create channel to signal updated data availability
		updateSig := make(hubSpoke, 3)
		// register websocket client with our signal channel
		exp.wsHub.RegisterClient(&updateSig)
		// unregister (and close signal channel) before return
		defer exp.wsHub.UnregisterClient(&updateSig)

		requestLimit := 1 << 20
		// set the max payload size to 1 MB
		ws.MaxPayloadBytes = requestLimit

		// Start listening for websocket messages from client with raw
		// transaction bytes (hex encoded) to decode or broadcast.
		go func() {
			for {
				// Wait to receive a message on the websocket
				msg := &WebSocketMessage{}
				ws.SetReadDeadline(time.Now().Add(wsReadTimeout))
				if err := websocket.JSON.Receive(ws, &msg); err != nil {
					if err.Error() != "EOF" {
						log.Warnf("websocket client receive error: %v", err)
					}
					return
				}

				// handle received message according to event ID
				var webData WebSocketMessage
				switch msg.EventId {
				case "decodetx":
					webData.EventId = msg.EventId + "Resp"
					if len(msg.Message) > requestLimit {
						log.Debug("Request size over limit")
						webData.Message = "Request too large"
						break
					}
					log.Debugf("Received decodetx signal for hex: %.40s...", msg.Message)
					tx, err := exp.blockData.DecodeRawTransaction(msg.Message)
					if err == nil {
						message, err := json.MarshalIndent(tx, "", "    ")
						if err != nil {
							log.Warn("Invalid JSON message: ", err)
							webData.Message = fmt.Sprintf("Error: Could not encode JSON message")
							break
						}
						webData.Message = string(message)
					} else {
						log.Debugf("Could not decode raw tx")
						webData.Message = fmt.Sprintf("Error: %v", err)
					}
				case "sendtx":
					webData.EventId = msg.EventId + "Resp"
					if len(msg.Message) > requestLimit {
						log.Debugf("Request size over limit")
						webData.Message = "Request too large"
						break
					}
					log.Debugf("Received sendtx signal for hex: %.40s...", msg.Message)
					txid, err := exp.blockData.SendRawTransaction(msg.Message)
					if err != nil {
						webData.Message = fmt.Sprintf("Error: %v", err)
					} else {
						webData.Message = fmt.Sprintf("Transaction sent: %s", txid)
					}
				case "getmempooltxs":
					webData.EventId = msg.EventId + "Resp"

					exp.MempoolData.Lock()
					msg, err := json.Marshal(exp.MempoolData)
					exp.MempoolData.Unlock()

					if err != nil {
						log.Warn("Invalid JSON message: ", err)
						webData.Message = fmt.Sprintf("Error: Could not encode JSON message")
						break
					}
					webData.Message = string(msg)
				case "ping":
					log.Tracef("We've been pinged: %.40s...", msg.Message)
					continue
				default:
					log.Warnf("Unrecognized event ID: %v", msg.EventId)
					continue
				}

				// send the response back on the websocket
				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				if err := websocket.JSON.Send(ws, webData); err != nil {
					log.Debugf("Failed to encode WebSocketMessage %s: %v",
						webData.EventId, err)
					// If the send failed, the client is probably gone, so close
					// the connection and quit.
					return
				}
			}
		}()

		// Send loop (ping, new tx, block, etc. update loop)
	loop:
		for {
			// Wait for signal from the hub to update
			select {
			case sig, ok := <-updateSig:
				// Check if the update channel was closed. Either the websocket
				// hub will do it after unregistering the client, or forcibly in
				// response to (http.CloseNotifier).CloseNotify() and only then if
				// the hub has somehow lost track of the client.
				if !ok {
					break loop
				}

				if _, ok = eventIDs[sig]; !ok {
					break loop
				}

				log.Tracef("signaling client: %p", &updateSig)

				// Write block data to websocket client

				webData := WebSocketMessage{
					EventId: eventIDs[sig],
				}
				buff := new(bytes.Buffer)
				enc := json.NewEncoder(buff)
				switch sig {
				case sigNewBlock:
					exp.NewBlockDataMtx.Lock()
					enc.Encode(WebsocketBlock{
						Block: exp.NewBlockData,
						Extra: exp.ExtraInfo,
					})
					exp.NewBlockDataMtx.Unlock()
					webData.Message = buff.String()
				case sigMempoolUpdate:
					exp.MempoolData.Lock()
					enc.Encode(exp.MempoolData.MempoolShort)
					exp.MempoolData.Unlock()
					webData.Message = buff.String()
				case sigPingAndUserCount:
					// ping and send user count
					webData.Message = strconv.Itoa(exp.wsHub.NumClients())
				case sigNewTx:
					exp.wsHub.clients[&updateSig].Lock()
					enc.Encode(exp.wsHub.clients[&updateSig].newTxs)
					exp.wsHub.clients[&updateSig].Unlock()
					webData.Message = buff.String()
				}

				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				err := websocket.JSON.Send(ws, webData)

				if err != nil {
					log.Debugf("Failed to encode WebSocketMessage %v: %v", sig, err)
					// If the send failed, the client is probably gone, so close
					// the connection and quit.
					return
				}
			case <-exp.wsHub.quitWSHandler:
				break loop
			}
		}
	})

	wsHandler.ServeHTTP(w, r)
}
