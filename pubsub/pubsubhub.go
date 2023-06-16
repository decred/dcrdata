// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package pubsub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"

	"github.com/decred/dcrdata/v8/blockdata"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	exptypes "github.com/decred/dcrdata/v8/explorer/types"
	"github.com/decred/dcrdata/v8/mempool"
	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
	"github.com/decred/dcrdata/v8/semver"
	"github.com/decred/dcrdata/v8/txhelpers"
	"golang.org/x/net/websocket"
)

var version = semver.NewSemver(3, 2, 0)

// Version indicates the semantic version of the pubsub module.
func Version() semver.Semver {
	return version
}

const (
	wsWriteTimeout = 5 * time.Second
	wsReadTimeout  = 7 * time.Second
)

// DataSource defines the interface for collecting required data.
type DataSource interface {
	GetExplorerBlock(hash string) *exptypes.BlockInfo
	DecodeRawTransaction(txhex string) (*chainjson.TxRawResult, error)
	SendRawTransaction(txhex string) (string, error)
	GetChainParams() *chaincfg.Params
	BlockSubsidy(height int64, voters uint16) *chainjson.GetBlockSubsidyResult
	Difficulty(timestamp int64) float64
}

// State represents the current state of block chain.
type State struct {
	// State is read locked by the send loop, and read/write locked when
	// occasional updates are made.
	mtx sync.RWMutex

	// GeneralInfo contains a variety of high level status information. Much of
	// GeneralInfo is constant, set in the constructor, while many fields are
	// set when Store provides new block details.
	GeneralInfo *exptypes.HomeInfo

	// BlockInfo contains details on the most recent block. It is updated when
	// Store provides new block details.
	BlockInfo *exptypes.BlockInfo

	// BlockchainInfo contains the result of the getblockchaininfo RPC. It is
	// updated when Store provides new block details.
	BlockchainInfo *chainjson.GetBlockChainInfoResult
}

type connection struct {
	sync.WaitGroup
	ws     *websocket.Conn
	client *clientHubSpoke
}

// PubSubHub manages the collection and distribution of block chain and mempool
// data to WebSocket clients.
type PubSubHub struct {
	sourceBase DataSource
	wsHub      *WebsocketHub
	state      *State
	params     *chaincfg.Params
	invsMtx    sync.RWMutex
	invs       *exptypes.MempoolInfo
	ver        pstypes.Ver
}

// NewPubSubHub constructs a PubSubHub given a data source. The WebSocketHub is
// automatically started.
func NewPubSubHub(dataSource DataSource) (*PubSubHub, error) {
	psh := new(PubSubHub)
	psh.sourceBase = dataSource

	// Allocate Mempool fields.
	psh.invs = new(exptypes.MempoolInfo)

	// Retrieve chain parameters.
	params := psh.sourceBase.GetChainParams()
	psh.params = params

	sv := Version()
	psh.ver = pstypes.NewVer(sv.Split())

	// Development subsidy address of the current network
	devSubsidyAddress, err := dbtypes.DevSubsidyAddress(params)
	if err != nil {
		return nil, fmt.Errorf("bad project fund address: %v", err)
	}

	psh.state = &State{
		// Set the constant parameters of GeneralInfo.
		GeneralInfo: &exptypes.HomeInfo{
			DevAddress: devSubsidyAddress,
			Params: exptypes.ChainParams{
				WindowSize:       params.StakeDiffWindowSize,
				RewardWindowSize: params.SubsidyReductionInterval,
				BlockTime:        params.TargetTimePerBlock.Nanoseconds(),
				MeanVotingBlocks: txhelpers.CalcMeanVotingBlocks(params),
			},
			PoolInfo: exptypes.TicketPoolInfo{
				Target: uint32(params.TicketPoolSize) * uint32(params.TicketsPerBlock),
			},
		},
		// BlockInfo and BlockchainInfo are set by Store()
	}

	psh.wsHub = NewWebsocketHub()
	go psh.wsHub.Run()

	return psh, nil
}

// StopWebsocketHub stops the websocket hub.
func (psh *PubSubHub) StopWebsocketHub() {
	if psh == nil {
		return
	}
	log.Info("Stopping websocket hub.")
	psh.wsHub.Stop()
}

// Ready checks if the WebSocketHub is ready.
func (psh *PubSubHub) Ready() bool {
	return psh.wsHub.Ready()
}

// SetReady updates the ready status of the WebSocketHub.
func (psh *PubSubHub) SetReady(ready bool) {
	psh.wsHub.SetReady(ready)
}

// HubRelay returns the channel used to signal to the WebSocketHub. See
// pstypes.HubSignal for valid signals.
func (psh *PubSubHub) HubRelay() chan pstypes.HubMessage {
	return psh.wsHub.HubRelay
}

// MempoolInventory safely retrieves the current mempool inventory.
func (psh *PubSubHub) MempoolInventory() *exptypes.MempoolInfo {
	psh.invsMtx.RLock()
	defer psh.invsMtx.RUnlock()
	return psh.invs
}

// closeWS attempts to close a websocket.Conn, logging errors other than those
// with messages containing ErrWsClosed.
func closeWS(ws *websocket.Conn) {
	err := ws.Close()
	// Do not log error if connection is just closed
	if err != nil && !pstypes.IsWSClosedErr(err) && !pstypes.IsIOTimeoutErr(err) {
		log.Errorf("Failed to close websocket: %v", err)
	}
}

// receiveLoop receives and processes incoming messages from active websocket
// connections. receiveLoop should be started as a goroutine, after conn.Add(1)
// and before a conn.Wait(). receiveLoop returns when the websocket connection,
// conn.ws, is closed, which should be initiated when sendLoop returns.
func (psh *PubSubHub) receiveLoop(conn *connection) {
	//defer conn.client.cl.unsubscribeAll()

	// receiveLoop should be started after conn.Add(1) and before a conn.Wait().
	defer conn.Done()

	// Receive messages on the websocket.Conn until it is closed.
	ws := conn.ws
	for {
		// Set this Conn's read deadline.
		err := ws.SetReadDeadline(time.Now().Add(wsReadTimeout))
		if err != nil && !pstypes.IsWSClosedErr(err) {
			log.Warnf("SetReadDeadline: %v", err)
		}

		// Wait to receive a message on the websocket
		msg := new(pstypes.WebSocketMessage)
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			// Keep listening for new messages if the read deadline has passed.
			if pstypes.IsIOTimeoutErr(err) {
				//log.Tracef("No data read from client in %v. Trying again.", wsReadTimeout)
				continue
			}
			// EOF is a common client disconnected error.
			if err.Error() != "EOF" && !pstypes.IsWSClosedErr(err) {
				log.Warnf("websocket client receive error: %v", err)
			}
			return
		}

		// Handle the received message according to its event ID.
		resp := pstypes.WebSocketMessage{
			EventId: msg.EventId + "Resp",
		}

		// Reject messages that exceed the limit.
		if len(msg.Message) > psh.wsHub.requestLimit {
			log.Debug("Request size over limit")
			resp.Message = json.RawMessage(`"Request too large"`) // skip json.Marshal for a string.
			continue
		}

		var req pstypes.RequestMessage
		err = json.Unmarshal(msg.Message, &req)
		if err != nil {
			log.Debugf("Unmarshal: %v", err)
			continue
		}
		reqEvent := req.Message

		// Create the ResponseMessage that is marshalled into resp.Message.
		respMsg := pstypes.ResponseMessage{
			RequestEventId: req.Message,
			RequestId:      req.RequestId, // associate the response with the client's request ID
		}

		// Determine response based on EventId and Message content.
		switch msg.EventId {
		case "subscribe":
			sig, sigMsg, valid := pstypes.ValidateSubscription(reqEvent)
			if !valid {
				log.Debugf("Invalid subscribe signal: %.40s...", reqEvent)
				respMsg.Data = "error: invalid subscription"
				break
			}

			var ok bool
			ok, err = conn.client.cl.subscribe(pstypes.HubMessage{Signal: sig, Msg: sigMsg})
			if err != nil {
				log.Debugf("Failed to subscribe: %.40s...", reqEvent)
				respMsg.Data = "error: " + err.Error()
				break
			}
			if !ok && sig != sigPingAndUserCount { // don't error on users over the legacy ping subscription request
				log.Debugf("Client CANNOT subscribe to: %v.", reqEvent)
				respMsg.Data = "cannot subscribed to " + reqEvent
				break
			}

			if sig != sigPingAndUserCount {
				log.Debugf("Client subscribed for: %v.", reqEvent)
				// Do not error on old clients that try to subscribe to ping
				// since they will get pings automatically.
			}
			respMsg.Data = "subscribed to " + reqEvent
			respMsg.Success = true

		case "unsubscribe":
			sig, sigMsg, valid := pstypes.ValidateSubscription(reqEvent)
			if !valid {
				log.Debugf("Invalid unsubscribe signal: %.40s...", reqEvent)
				respMsg.Data = "error: invalid subscription"
				break
			}

			err = conn.client.cl.unsubscribe(pstypes.HubMessage{Signal: sig, Msg: sigMsg})
			if err != nil {
				log.Debugf("Failed to unsubscribe from: %.40s...", reqEvent)
				respMsg.Data = "error: " + err.Error()
				break
			}

			log.Debugf("Client unsubscribed from: %v.", reqEvent)
			respMsg.Data = "unsubscribed from " + reqEvent
			respMsg.Success = true

		case "decodetx":
			log.Debugf("Received decodetx signal for hex: %.40s...", reqEvent)
			tx, err := psh.sourceBase.DecodeRawTransaction(reqEvent)
			if err == nil {
				var decoded []byte
				decoded, err = json.MarshalIndent(tx, "", "    ")
				if err != nil {
					log.Warn("Invalid JSON message: ", err)
					respMsg.Data = "error: Could not encode JSON message"
					break
				}
				respMsg.Success = true
				respMsg.Data = string(decoded)
			} else {
				log.Debugf("Could not decode raw tx: %v", err)
				respMsg.Data = fmt.Sprintf("error: %v", err)
			}

		case "sendtx":
			log.Debugf("Received sendtx signal for hex: %.40s...", reqEvent)
			txid, err := psh.sourceBase.SendRawTransaction(reqEvent)
			if err != nil {
				respMsg.Data = fmt.Sprintf("error: %v", err)
			} else {
				respMsg.Success = true
				respMsg.Data = txid
			}

		case "getmempooltxs": // TODO: maybe disable this case
			// construct mempool object with properties required in template
			inv := psh.MempoolInventory()
			mempoolInfo := inv.Trim() // Trim locks the inventory.

			psh.state.mtx.RLock()
			mempoolInfo.Subsidy = psh.state.GeneralInfo.NBlockSubsidy
			psh.state.mtx.RUnlock()

			var b []byte
			b, err = json.Marshal(mempoolInfo)
			if err != nil {
				log.Warn("Invalid JSON message: ", err)
				respMsg.Data = "error: Could not encode JSON message"
				break
			}
			respMsg.Data = string(b)
			respMsg.Success = true

		case "version":
			var b []byte
			b, err = json.Marshal(psh.ver)
			if err != nil {
				log.Warn("Invalid JSON message: ", err)
				respMsg.Data = "error: Could not encode JSON message"
				break
			}
			respMsg.Data = string(b)
			respMsg.Success = true

		case "ping":
			log.Tracef("We've been pinged!")
			// No response to ping
			continue

		default:
			log.Warnf("Unrecognized event ID: %v", reqEvent)
			// ignore unrecognized events
			continue
		}

		// Marshal the ResponseMessage into the RawJSON type field, Message, of
		// the WebSocketMessage.
		resp.Message, err = json.Marshal(respMsg)
		if err != nil {
			log.Warnf("Failed to Marshal subscribe response for %s: %v", reqEvent, err)
			continue
		}

		// Send the response.
		err = ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
		if err != nil && !pstypes.IsWSClosedErr(err) {
			log.Warnf("SetWriteDeadline: %v", err)
		}
		if err := websocket.JSON.Send(ws, resp); err != nil {
			// Do not log the error if the connection is just closed.
			if !pstypes.IsWSClosedErr(err) {
				log.Debugf("Failed to encode WebSocketMessage (reply) %s: %v",
					resp.EventId, err)
			}
			// If the send failed, the client is probably gone, quit the
			// receive loop, closing the websocket.Conn.
			return
		}
	} // for {
}

// sendLoop receives signals from WebSocketHub via the connections unique signal
// channel, and sends the relevant data to the client. sendLoop will return when
// conn.client.c is closed. On return, the websocket connection, conn.ws, will
// be closed, thus forcing the same connection's receiveLoop to return.
func (psh *PubSubHub) sendLoop(conn *connection) {
	// Use this client's unique channel to receive signals from the
	// WebSocketHub, which broadcasts signals to all clients.
	updateSigChan := *conn.client.c
	clientData := conn.client.cl
	buff := new(bytes.Buffer)

	// sendLoop should be started after conn.Add(1), and before a conn.Wait().
	defer conn.Done()

	// If returning because the WebSocketHub sent a quit signal, the receive
	// loop may still be waiting for a message, so it is necessary to close the
	// websocket.Conn in this case.
	ws := conn.ws
	defer closeWS(ws)

loop:
	for sig := range updateSigChan {
		log.Tracef("(*PubSubHub)sendLoop: updateSigChan received %v for client %d",
			sig, clientData.id)
		// If the update channel is closed, the loop terminates.

		if !sig.IsValid() {
			log.Errorf("invalid signal to send: %s / %d", sig.Signal, int(sig.Signal))
			continue loop
		}

		switch sig.Signal {
		case sigByeNow, sigPingAndUserCount:
			// These signals are not subscription-based.
		default:
			if !clientData.isSubscribed(sig) {
				log.Errorf("Client not subscribed for %s events. "+
					"WebSocketHub should have caught this.", sig)
				continue loop // break
			}
		}

		log.Tracef("signaling client %d with %s", clientData.id, sig)

		// Respond to the websocket client.
		pushMsg := pstypes.WebSocketMessage{
			EventId: sig.Signal.String(),
			// Message is set in switch statement below.
		}

		// JSON encoder for the Message.
		buff.Reset()
		enc := json.NewEncoder(buff)

		switch sig.Signal {
		case sigAddressTx:
			// sig was already validated, but do it again here in case the
			// type changed without changing the type assertion here.
			am, ok := sig.Msg.(*pstypes.AddressMessage)
			if !ok {
				log.Errorf("sigAddressTx did not store a *AddressMessage in Msg.")
				continue loop
			}
			err := enc.Encode(am)
			if err != nil {
				log.Warnf("Encode(AddressMessage) failed: %v", err)
			}

			log.Debugf("Sending sigAddressTx to client %d: %s", clientData.id, am)

			pushMsg.Message = buff.Bytes()
		case sigNewBlock:
			psh.state.mtx.RLock()
			if psh.state.BlockInfo == nil {
				psh.state.mtx.RUnlock()
				break // from switch to send empty message
			}
			err := enc.Encode(exptypes.WebsocketBlock{
				Block: psh.state.BlockInfo,
				Extra: psh.state.GeneralInfo,
			})
			psh.state.mtx.RUnlock()
			if err != nil {
				log.Warnf("Encode(WebsocketBlock) failed: %v", err)
			}

			pushMsg.Message = buff.Bytes()

		case sigMempoolUpdate:
			// You probably want the sigNewTxs event. sigMempoolUpdate sends
			// a summary of mempool contents, and the NumLatestMempoolTxns
			// latest transactions.
			inv := psh.MempoolInventory()
			if inv == nil {
				break // from switch to send empty message
			}
			inv.RLock()
			err := enc.Encode(inv.MempoolShort)
			inv.RUnlock()
			if err != nil {
				log.Warnf("Encode(MempoolShort) failed: %v", err)
			}

			pushMsg.Message = buff.Bytes()

		case sigPingAndUserCount:
			// ping and send user count
			pushMsg.Message = json.RawMessage(strconv.Itoa(psh.wsHub.NumClients())) // No quotes as this is a JSON integer

		case sigNewTxs:
			// Marshal this client's tx buffer if it is not empty.
			clientData.newTxs.Lock()
			if len(clientData.newTxs.t) == 0 {
				clientData.newTxs.Unlock()
				continue loop // break sigselect
			}
			err := enc.Encode(clientData.newTxs.t)

			// Reinit the tx buffer.
			clientData.newTxs.t = make(pstypes.TxList, 0, NewTxBufferSize)
			clientData.newTxs.Unlock()
			if err != nil {
				log.Warnf("Encode([]*exptypes.MempoolTx) failed: %v", err)
			}

			pushMsg.Message = buff.Bytes()

		case sigByeNow:
			pushMsg.Message = []byte(`"The dcrdata server is shutting down. Bye!"`)
			log.Tracef("Sending %v", string(pushMsg.Message))

		// case sigSyncStatus:
		// 	err := enc.Encode(explorer.SyncStatus())
		// 	if err != nil {
		// 		log.Warnf("Encode(SyncStatus()) failed: %v", err)
		// 	}
		// 	pushMsg.Message = buff.String()

		default:
			log.Errorf("Not sending a %v to the client.", sig)
			continue loop // break sigselect
		} // switch sig

		// Send the message.
		err := ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
		if err != nil && !pstypes.IsWSClosedErr(err) {
			log.Warnf("SetWriteDeadline failed: %v", err)
		}
		if err = websocket.JSON.Send(ws, pushMsg); err != nil {
			// Do not log the error if the connection is just closed.
			if !pstypes.IsWSClosedErr(err) {
				log.Debugf("Failed to encode WebSocketMessage (push) %v: %v", sig, err)
			}
			// If the send failed, the client is probably gone, quit the
			// send loop, unregistering the client from the websocket hub.
			log.Errorf("websocket.JSON.Send of %v type message failed: %v", sig, err)
			return
		}
	} // for range { a.k.a. loop:
}

// WebSocketHandler is the http.HandlerFunc for new websocket connections. The
// connection is registered with the WebSocketHub, and the send/receive loops
// are launched.
func (psh *PubSubHub) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	// Register websocket client.
	ch := psh.wsHub.NewClientHubSpoke()
	defer close(ch.cl.killed)

	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		// Set the max payload size for this connection.
		ws.MaxPayloadBytes = psh.wsHub.requestLimit

		// The receive loop will be sitting on websocket.JSON.Receive, while the
		// send loop will be waiting for signals from the WebSocketHub. One must
		// close the other depending on whether the connection was closed/lost,
		// or the WebSocketHub quit or forcibly unregistered the client. The
		// receive loop unregisters the client (thus closing the update signal
		// channel) when the connection is closed and it returns. The send loop
		// closes the websocket.Conn on return, which will interrupt the receive
		// loop from its waiting to receive data on the connection.

		conn := &connection{
			client: ch,
			ws:     ws,
		}

		// Start listening for websocket messages from client, returning when
		// the connection is closed. The connection will be forcibly closed when
		// sendLoop returns if it is still opened.
		conn.Add(1)
		go psh.receiveLoop(conn)

		// Send loop (ping, new tx, block, etc. update loop). sendLoop returns
		// when the client's signaling channel, conn.ch.cl.c, is closed.
		conn.Add(1)
		go psh.sendLoop(conn)

		// Hang out until the send and receive loops have quit.
		conn.Wait()

		// Clean up the client's subscriptions.
		ch.cl.unsubscribeAll()
	})

	// Use a websocket.Server to avoid checking Origin.
	wsServer := websocket.Server{
		Handler: wsHandler,
	}
	wsServer.ServeHTTP(w, r)
}

// StoreMPData stores mempool data. It is advisable to pass a copy of the
// []exptypes.MempoolTx so that it may be modified (e.g. sorted) without
// affecting other MempoolDataSavers. The struct pointed to may be shared, so it
// should not be modified.
func (psh *PubSubHub) StoreMPData(_ *mempool.StakeData, _ []exptypes.MempoolTx, inv *exptypes.MempoolInfo) {
	// Get exclusive access to the Mempool field.
	psh.invsMtx.Lock()
	psh.invs = inv
	psh.invsMtx.Unlock()
	log.Debugf("Updated mempool details for the pubsubhub.")
}

// Store processes and stores new block data, then signals to the WebSocketHub
// that the new data is available.
func (psh *PubSubHub) Store(blockData *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	// treasuryActive := txhelpers.IsTreasuryActive(psh.params.Net, int64(msgBlock.Header.Height))

	// Retrieve block data for the passed block hash.
	newBlockData := psh.sourceBase.GetExplorerBlock(msgBlock.BlockHash().String())

	// Use the latest block's blocktime to get the last 24hr timestamp.
	day := 24 * time.Hour
	targetTimePerBlock := float64(psh.params.TargetTimePerBlock)

	// Hashrate change over last day
	timestamp := newBlockData.BlockTime.T.Add(-day).Unix()
	last24hrDifficulty := psh.sourceBase.Difficulty(timestamp)
	last24HrHashRate := dbtypes.CalculateHashRate(last24hrDifficulty, targetTimePerBlock)

	// Hashrate change over last month
	timestamp = newBlockData.BlockTime.T.Add(-30 * day).Unix()
	lastMonthDifficulty := psh.sourceBase.Difficulty(timestamp)
	lastMonthHashRate := dbtypes.CalculateHashRate(lastMonthDifficulty, targetTimePerBlock)

	difficulty := blockData.Header.Difficulty
	hashrate := dbtypes.CalculateHashRate(difficulty, targetTimePerBlock)

	// If BlockData contains non-nil PoolInfo, compute actual percentage of DCR
	// supply staked.
	stakePerc := 45.0
	if blockData.PoolInfo != nil {
		stakePerc = blockData.PoolInfo.Value / dcrutil.Amount(blockData.ExtraInfo.CoinSupply).ToCoin()
	}

	// Update pageData with block data and chain (home) info.
	p := psh.state
	p.mtx.Lock()

	// Store current block and blockchain data.
	p.BlockInfo = newBlockData
	p.BlockchainInfo = blockData.BlockchainInfo

	// Update GeneralInfo, keeping constant parameters set in NewPubSubHub.
	p.GeneralInfo.HashRate = hashrate
	p.GeneralInfo.HashRateChangeDay = 100 * (hashrate - last24HrHashRate) / last24HrHashRate
	p.GeneralInfo.HashRateChangeMonth = 100 * (hashrate - lastMonthHashRate) / lastMonthHashRate
	p.GeneralInfo.CoinSupply = blockData.ExtraInfo.CoinSupply
	p.GeneralInfo.StakeDiff = blockData.CurrentStakeDiff.CurrentStakeDifficulty
	p.GeneralInfo.NextExpectedStakeDiff = blockData.EstStakeDiff.Expected
	p.GeneralInfo.NextExpectedBoundsMin = blockData.EstStakeDiff.Min
	p.GeneralInfo.NextExpectedBoundsMax = blockData.EstStakeDiff.Max
	p.GeneralInfo.IdxBlockInWindow = blockData.IdxBlockInWindow
	p.GeneralInfo.IdxInRewardWindow = int(newBlockData.Height%psh.params.SubsidyReductionInterval) + 1
	p.GeneralInfo.Difficulty = difficulty
	p.GeneralInfo.NBlockSubsidy.Dev = blockData.ExtraInfo.NextBlockSubsidy.Developer
	p.GeneralInfo.NBlockSubsidy.PoS = blockData.ExtraInfo.NextBlockSubsidy.PoS
	p.GeneralInfo.NBlockSubsidy.PoW = blockData.ExtraInfo.NextBlockSubsidy.PoW
	p.GeneralInfo.NBlockSubsidy.Total = blockData.ExtraInfo.NextBlockSubsidy.Total

	// If BlockData contains non-nil PoolInfo, copy values.
	p.GeneralInfo.PoolInfo = exptypes.TicketPoolInfo{}
	if blockData.PoolInfo != nil {
		tpTarget := uint32(psh.params.TicketPoolSize) * uint32(psh.params.TicketsPerBlock)
		p.GeneralInfo.PoolInfo = exptypes.TicketPoolInfo{
			Size:          blockData.PoolInfo.Size,
			Value:         blockData.PoolInfo.Value,
			ValAvg:        blockData.PoolInfo.ValAvg,
			Percentage:    stakePerc * 100,
			PercentTarget: 100 * float64(blockData.PoolInfo.Size) / float64(tpTarget),
			Target:        tpTarget,
		}
	}

	posSubsPerVote := dcrutil.Amount(blockData.ExtraInfo.NextBlockSubsidy.PoS).ToCoin() /
		float64(psh.params.TicketsPerBlock)
	p.GeneralInfo.TicketReward = 100 * posSubsPerVote /
		blockData.CurrentStakeDiff.CurrentStakeDifficulty

	// The actual reward of a ticket needs to also take into consideration the
	// ticket maturity (time from ticket purchase until its eligible to vote)
	// and coinbase maturity (time after vote until funds distributed to ticket
	// holder are available to use).
	avgSSTxToSSGenMaturity := psh.state.GeneralInfo.Params.MeanVotingBlocks +
		int64(psh.params.TicketMaturity) +
		int64(psh.params.CoinbaseMaturity)
	p.GeneralInfo.RewardPeriod = fmt.Sprintf("%.2f days", float64(avgSSTxToSSGenMaturity)*
		psh.params.TargetTimePerBlock.Hours()/24)
	//p.GeneralInfo.ASR = ASR

	p.mtx.Unlock()

	// Signal to the websocket hub that a new block was received, but do not
	// block Store(), and do not hang forever in a goroutine waiting to send.
	go func() {
		select {
		case psh.wsHub.HubRelay <- pstypes.HubMessage{Signal: sigNewBlock}:
		case <-time.After(time.Second * 10):
			log.Errorf("sigNewBlock send failed: Timeout waiting for WebsocketHub.")
		}
	}()

	log.Debugf("Got new block %d for the pubsubhub.", newBlockData.Height)

	// Since the coinbase transaction is generated by the miner, it will never
	// hit mempool. It must be processed now, with the new block.
	coinbaseTx := msgBlock.Transactions[0]
	coinbaseHash := coinbaseTx.CachedTxHash().String() // data race with other Storers?
	// Check each output's pkScript for subscribed addresses.
	for _, out := range coinbaseTx.TxOut {
		_, scriptAddrs := stdscript.ExtractAddrs(out.Version, out.PkScript, psh.params)
		for _, scriptAddr := range scriptAddrs {
			addr := scriptAddr.String()
			go func() {
				select {
				case psh.wsHub.HubRelay <- pstypes.HubMessage{
					Signal: sigAddressTx,
					Msg: &pstypes.AddressMessage{
						Address: addr,
						TxHash:  coinbaseHash,
					},
				}:
				case <-time.After(time.Second * 10):
					log.Errorf("sigNewBlock send failed: Timeout waiting for WebsocketHub.")
				}
			}()
		}
	}

	// The coinbase transaction is also sent in a new transaction signal to
	// pubsub clients. It's not really mempool.
	newTxCoinbase := exptypes.MempoolTx{
		TxID:    coinbaseHash,
		Version: int32(coinbaseTx.Version),
		// Fees are 0
		VinCount:  len(coinbaseTx.TxIn),
		VoutCount: len(coinbaseTx.TxOut),
		Vin:       exptypes.MsgTxMempoolInputs(coinbaseTx),
		Coinbase:  true,
		Hash:      coinbaseHash,
		Time:      blockData.Header.Time,
		Size:      int32(coinbaseTx.SerializeSize()),
		TotalOut:  txhelpers.TotalOutFromMsgTx(coinbaseTx).ToCoin(),
		Type:      txhelpers.TxTypeToString(0), // "Regular"
		TypeID:    0,                           // stake.TxTypeRegular
	}

	go func() {
		select {
		case psh.wsHub.HubRelay <- pstypes.HubMessage{
			Signal: sigNewTx,
			Msg:    &newTxCoinbase,
		}:
		case <-time.After(time.Second * 10):
			log.Errorf("sigNewTx send failed: Timeout waiting for WebsocketHub.")
		}
	}()

	return nil
}
