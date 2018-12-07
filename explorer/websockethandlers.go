// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	apitypes "github.com/decred/dcrdata/v3/api/types"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"golang.org/x/net/websocket"
)

// ErrWsClosed is the error message text used websocket Conn.Close tries to
// close an already closed connection.
var ErrWsClosed = "use of closed network connection"

// RootWebsocket is the websocket handler for all pages
func (exp *explorerUI) RootWebsocket(w http.ResponseWriter, r *http.Request) {
	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		// Create channel to signal updated data availability
		updateSig := make(hubSpoke, 3)
		// register websocket client with our signal channel
		clientData := exp.wsHub.RegisterClient(&updateSig)
		// unregister (and close signal channel) before return
		defer exp.wsHub.UnregisterClient(&updateSig)

		// close the websocket
		closeWS := func() {
			err := ws.Close()
			// Do not log error if connection is just closed
			if err != nil && !strings.Contains(err.Error(), ErrWsClosed) {
				log.Errorf("Failed to close websocket: %v", err)
			}
		}
		defer closeWS()

		requestLimit := 1 << 20
		// set the max payload size to 1 MB
		ws.MaxPayloadBytes = requestLimit

		// Start listening for websocket messages from client with raw
		// transaction bytes (hex encoded) to decode or broadcast.
		go func() {
			defer closeWS()
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
				//  If the request sent is past the limit continue to the next iteration.
				if len(msg.Message) > requestLimit {
					log.Debug("Request size over limit")
					webData.Message = "Request too large"
					continue
				}

				switch msg.EventId {
				case "decodetx":
					log.Debugf("Received decodetx signal for hex: %.40s...", msg.Message)
					tx, err := exp.blockData.DecodeRawTransaction(msg.Message)
					if err == nil {
						message, err := json.MarshalIndent(tx, "", "    ")
						if err != nil {
							log.Warn("Invalid JSON message: ", err)
							webData.Message = "Error: Could not encode JSON message"
							break
						}
						webData.Message = string(message)
					} else {
						log.Debugf("Could not decode raw tx")
						webData.Message = fmt.Sprintf("Error: %v", err)
					}

				case "sendtx":
					log.Debugf("Received sendtx signal for hex: %.40s...", msg.Message)
					txid, err := exp.blockData.SendRawTransaction(msg.Message)
					if err != nil {
						webData.Message = fmt.Sprintf("Error: %v", err)
					} else {
						webData.Message = fmt.Sprintf("Transaction sent: %s", txid)
					}

				case "getmempooltxs":
					// construct mempool object with properties required in template
					mempoolInfo := exp.TrimmedMempoolInfo()
					// mempool fees appear incorrect, temporarily set to zero for now
					mempoolInfo.Fees = 0

					exp.pageData.RLock()
					mempoolInfo.Subsidy = exp.pageData.HomeInfo.NBlockSubsidy
					exp.pageData.RUnlock()

					msg, err := json.Marshal(mempoolInfo)

					if err != nil {
						log.Warn("Invalid JSON message: ", err)
						webData.Message = "Error: Could not encode JSON message"
						break
					}
					webData.Message = string(msg)

				case "getticketpooldata":
					// Retrieve chart data on the given interval.
					interval := dbtypes.TimeGroupingFromStr(msg.Message)
					// Chart height is returned since the cache may be stale,
					// although it is automatically updated by the first caller
					// who requests data from a stale cache.
					timeChart, priceChart, donutChart, chartHeight, err := exp.explorerSource.TicketPoolVisualization(interval)
					if dbtypes.IsTimeoutErr(err) {
						log.Warnf("TicketPoolVisualization DB timeout: %v", err)
						webData.Message = "Error: DB timeout"
						break
					}
					if err != nil {
						if strings.HasPrefix(err.Error(), "unknown interval") {
							log.Debugf("invalid ticket pool interval provided "+
								"via TicketPoolVisualization: %s", msg.Message)
							webData.Message = "Error: " + err.Error()
							break
						}
						log.Errorf("TicketPoolVisualization error: %v", err)
						webData.Message = "Error: failed to fetch ticketpool data"
						break
					}

					mp := new(apitypes.PriceCountTime)
					exp.MempoolData.RLock()

					if len(exp.MempoolData.Tickets) > 0 {
						mp.Price = exp.MempoolData.Tickets[0].TotalOut
						mp.Count = len(exp.MempoolData.Tickets)
						mp.Time = dbtypes.TimeDef{T: time.Unix(exp.MempoolData.Tickets[0].Time, 0)}
					} else {
						log.Debug("No tickets exists in the mempool")
					}

					exp.MempoolData.RUnlock()

					data := &apitypes.TicketPoolChartsData{
						ChartHeight: chartHeight,
						TimeChart:   timeChart,
						PriceChart:  priceChart,
						DonutChart:  donutChart,
						Mempool:     mp,
					}

					msg, err := json.Marshal(data)
					if err != nil {
						log.Warn("Invalid JSON message: ", err)
						webData.Message = "Error: Could not encode JSON message"
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

				webData.EventId = msg.EventId + "Resp"

				// send the response back on the websocket
				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				if err := websocket.JSON.Send(ws, webData); err != nil {
					// Do not log error if connection is just closed
					if !strings.Contains(err.Error(), ErrWsClosed) {
						log.Debugf("Failed to encode WebSocketMessage (reply) %s: %v",
							webData.EventId, err)
					}
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
				// response to (http.CloseNotifier).CloseNotify() and only then
				// if the hub has somehow lost track of the client.
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
					exp.pageData.RLock()
					enc.Encode(WebsocketBlock{
						Block: exp.pageData.BlockInfo,
						Extra: exp.pageData.HomeInfo,
					})
					exp.pageData.RUnlock()

					webData.Message = buff.String()
				case sigMempoolUpdate:
					exp.MempoolData.RLock()
					enc.Encode(exp.MempoolData.MempoolShort)
					exp.MempoolData.RUnlock()
					webData.Message = buff.String()
				case sigPingAndUserCount:
					// ping and send user count
					webData.Message = strconv.Itoa(exp.wsHub.NumClients())
				case sigNewTx:
					clientData.RLock()
					enc.Encode(clientData.newTxs)
					clientData.RUnlock()
					webData.Message = buff.String()
				case sigSyncStatus:
					enc.Encode(SyncStatus())
					webData.Message = buff.String()
				}

				ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				if err := websocket.JSON.Send(ws, webData); err != nil {
					// Do not log error if connection is just closed
					if !strings.Contains(err.Error(), ErrWsClosed) {
						log.Debugf("Failed to encode WebSocketMessage (push) %v: %v", sig, err)
					}
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
