// Copyright (c) 2018-2021, The Decred developers
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

	apitypes "github.com/decred/dcrdata/v8/api/types"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/explorer/types"
	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
	"golang.org/x/net/websocket"
)

// RootWebsocket is the websocket handler for all pages
func (exp *explorerUI) RootWebsocket(w http.ResponseWriter, r *http.Request) {
	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		// Create channel to signal updated data availability
		updateSig := make(hubSpoke, 3)
		// Create a channel for exchange updates
		xcChan := make(exchangeChannel, 3)
		// register websocket client with our signal channel
		clientData := exp.wsHub.RegisterClient(&updateSig, xcChan)
		// unregister (and close signal channel) before return
		defer exp.wsHub.UnregisterClient(&updateSig)

		// close the websocket
		closeWS := func() {
			err := ws.Close()
			// Do not log error if connection is just closed.
			if err != nil && !pstypes.IsWSClosedErr(err) &&
				!pstypes.IsIOTimeoutErr(err) {
				log.Errorf("Failed to close websocket: %v", err)
			}
		}
		defer closeWS()

		send := func(webData WebSocketMessage) error {
			err := ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err != nil && !pstypes.IsWSClosedErr(err) {
				log.Warnf("SetWriteDeadline failed: %v", err)
			}
			if err := websocket.JSON.Send(ws, webData); err != nil {
				// Do not log error if connection is just closed
				if !pstypes.IsWSClosedErr(err) {
					log.Debugf("Failed to send web socket message %s: %v", webData.EventId, err)
				}
				// If the send failed, the client is probably gone, so close
				// the connection and quit.
				return fmt.Errorf("Send fail")
			}
			return nil
		}

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
				err := ws.SetReadDeadline(time.Now().Add(wsReadTimeout))
				if err != nil && !pstypes.IsWSClosedErr(err) {
					log.Warnf("SetReadDeadline failed: %v", err)
				}
				if err = websocket.JSON.Receive(ws, &msg); err != nil {
					if err.Error() != "EOF" && !pstypes.IsWSClosedErr(err) {
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
					tx, err := exp.dataSource.DecodeRawTransaction(msg.Message)
					if err == nil {
						message, err := json.MarshalIndent(tx, "", "    ")
						if err != nil {
							log.Warn("Invalid JSON message: ", err)
							webData.Message = errMsgJSONEncode
							break
						}
						webData.Message = string(message)
					} else {
						log.Debugf("Could not decode raw tx")
						webData.Message = fmt.Sprintf("Error: %v", err)
					}

				case "sendtx":
					log.Debugf("Received sendtx signal for hex: %.40s...", msg.Message)
					txid, err := exp.dataSource.SendRawTransaction(msg.Message)
					if err != nil {
						webData.Message = fmt.Sprintf("Error: %v", err)
					} else {
						webData.Message = fmt.Sprintf("Transaction sent: %s", txid)
					}

				case "getmempooltxs":
					// MempoolInfo. Used on mempool and home page.
					inv := exp.MempoolInventory()

					// Check if client supplied a mempool ID. If so, check that an update
					// is needed before sending.
					if msg.Message != "" {
						clientID, err := strconv.ParseUint(msg.Message, 10, 64)
						if err != nil {
							// For now, just log a warning and return the mempool anyway.
							log.Warn("Unable to parse supplied mempool ID %s", msg.Message)
						} else if inv.ID() == clientID {
							// Client is up-to-date. No need to send anything.
							continue
						}
					}

					inv.RLock()
					msg, err := json.Marshal(inv)
					inv.RUnlock()

					if err != nil {
						log.Warn("Invalid JSON message: ", err)
						webData.Message = errMsgJSONEncode
						break
					}
					webData.Message = string(msg)

				case "getmempooltrimmed":
					// TrimmedMempoolInfo. Used in visualblocks.
					// construct mempool object with properties required in template
					inv := exp.MempoolInventory()
					mempoolInfo := inv.Trim() // Trim internally locks the MempoolInfo.

					exp.pageData.RLock()
					mempoolInfo.Subsidy = exp.pageData.HomeInfo.NBlockSubsidy
					exp.pageData.RUnlock()

					msg, err := json.Marshal(mempoolInfo)

					if err != nil {
						log.Warn("Invalid JSON message: ", err)
						webData.Message = errMsgJSONEncode
						break
					}
					webData.Message = string(msg)

				case "getticketpooldata":
					// Retrieve chart data on the given interval.
					interval := dbtypes.TimeGroupingFromStr(msg.Message)
					// Chart height is returned since the cache may be stale,
					// although it is automatically updated by the first caller
					// who requests data from a stale cache.
					timeChart, priceChart, outputsChart, chartHeight, err :=
						exp.dataSource.TicketPoolVisualization(interval)
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

					inv := exp.MempoolInventory()
					inv.RLock()
					if len(inv.Tickets) > 0 {
						mp.Price = inv.Tickets[0].TotalOut
						mp.Count = len(inv.Tickets)
						mp.Time = dbtypes.NewTimeDefFromUNIX(inv.Tickets[0].Time)
					} else {
						log.Debug("No tickets exists in the mempool")
					}
					inv.RUnlock()

					data := &apitypes.TicketPoolChartsData{
						ChartHeight:  uint64(chartHeight),
						TimeChart:    timeChart,
						PriceChart:   priceChart,
						OutputsChart: outputsChart,
						Mempool:      mp,
					}

					msg, err := json.Marshal(data)
					if err != nil {
						log.Warn("Invalid JSON message: ", err)
						webData.Message = errMsgJSONEncode
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

				err = send(webData)
				if err != nil {
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

				if !sig.IsValid() {
					log.Errorf("invalid signal to send: %s / %d", sig.Signal.String(), int(sig.Signal))
					continue
				}

				log.Tracef("signaling client: %p", &updateSig)

				// Write block data to websocket client

				webData := WebSocketMessage{
					// Use HubSignal's string, not HubMessage's string, which is decorated.
					EventId: sig.Signal.String(),
					Message: "error",
				}
				buff := new(bytes.Buffer)
				enc := json.NewEncoder(buff)
				switch sig.Signal {
				case sigNewBlock:
					exp.pageData.RLock()
					err := enc.Encode(types.WebsocketBlock{
						Block: exp.pageData.BlockInfo,
						Extra: exp.pageData.HomeInfo,
					})
					exp.pageData.RUnlock()
					if err == nil {
						webData.Message = buff.String()
					} else {
						log.Errorf("json.Encode(WebsocketBlock) failed: %v", err)
					}

				case sigMempoolUpdate:
					inv := exp.MempoolInventory()
					inv.RLock()
					err := enc.Encode(inv.MempoolShort)
					inv.RUnlock()
					if err == nil {
						webData.Message = buff.String()
					} else {
						log.Errorf("json.Encode(MempoolShort) failed: %v", err)
					}

				case sigPingAndUserCount:
					// ping and send user count
					webData.Message = strconv.Itoa(exp.wsHub.NumClients())
				case sigNewTxs:
					// Do not use any tx slice in sig.Msg. Instead use client's
					// new transactions slice, newTxs.
					clientData.RLock()
					err := enc.Encode(clientData.newTxs)
					clientData.RUnlock()
					if err == nil {
						webData.Message = buff.String()
					} else {
						log.Errorf("json.Encode([]*MempoolTx) failed: %v", err)
					}

				case sigSyncStatus:
					err := enc.Encode(SyncStatus())
					if err == nil {
						webData.Message = buff.String()
					} else {
						log.Errorf("json.Encode([]SyncStatusInfo) failed: %v", err)
					}

				default:
					log.Errorf("RootWebsocket: Unhandled signal: %v", sig)
				}

				err := send(webData)
				if err != nil {
					return
				}

			case update := <-xcChan:
				buff := new(bytes.Buffer)
				enc := json.NewEncoder(buff)
				webData := WebSocketMessage{
					EventId: exchangeUpdateID,
					Message: "error",
				}
				err := enc.Encode(update)
				if err == nil {
					webData.Message = buff.String()
				} else {
					log.Errorf("json.Encode(*WebsocketExchangeUpdate) failed: %v", err)
				}
				err = send(webData)
				if err != nil {
					return
				}

			case <-exp.wsHub.quitWSHandler:
				break loop
			} // select
		} // for a.k.a. loop:
	}) // wsHandler := websocket.Handler(func(ws *websocket.Conn) {

	wsHandler.ServeHTTP(w, r)
}
