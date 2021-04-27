// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/decred/dcrdata/exchanges/v3"
	dcrrates "github.com/decred/dcrdata/exchanges/v3/ratesproto"
	grpcPeer "google.golang.org/grpc/peer"
)

// StreamID is a unique ID for gRPC client streams.
type StreamID uint

var streamCounter StreamID

// RateServer manages the data sources and client subscriptions.
type RateServer struct {
	btcIndex   string
	xcBot      *exchanges.ExchangeBot
	clientLock *sync.RWMutex
	clients    map[StreamID]RateClient
}

// RateClient is an interface for rateClient to enable testing the server via
// stubs.
type RateClient interface {
	SendExchangeUpdate(*dcrrates.ExchangeRateUpdate) error
	Stream() GRPCStream
}

// NewRateServer is a constructor for a RateServer.
func NewRateServer(index string, xcBot *exchanges.ExchangeBot) *RateServer {
	return &RateServer{
		btcIndex:   index,
		clientLock: new(sync.RWMutex),
		clients:    make(map[StreamID]RateClient),
		xcBot:      xcBot,
	}
}

// GRPCStream wraps the grpc.ClientStream.
type GRPCStream interface {
	Send(*dcrrates.ExchangeRateUpdate) error
	Context() context.Context
}

// sendStateList is a helper for parsing the ExchangeBotState when a new client
// subscription is received.
func sendStateList(client RateClient, states map[string]*exchanges.ExchangeState) (err error) {
	for token, state := range states {
		err = client.SendExchangeUpdate(makeExchangeRateUpdate(&exchanges.ExchangeUpdate{
			Token: token,
			State: state,
		}))
		if err != nil {
			log.Errorf("SendExchangeUpdate error for %s: %v", token, err)
			return
		}
	}
	return
}

// SubscribeExchanges is a gRPC method defined in dcrrates/dcrrates.proto. It
// satisfies the DCRRatesServer interface. The gRPC server will call this method
// when a client subscription is received.
func (server *RateServer) SubscribeExchanges(hello *dcrrates.ExchangeSubscription, stream dcrrates.DCRRates_SubscribeExchangesServer) (err error) {
	return server.ReallySubscribeExchanges(hello, stream)
}

// ReallySubscribeExchanges stores the client and their exchange subscriptions.
// It waits for the client contexts Done channel to close and deletes the client
// from the RateServer.clients map.
func (server *RateServer) ReallySubscribeExchanges(hello *dcrrates.ExchangeSubscription, stream GRPCStream) (err error) {
	// For now, require the ExchangeBot clients to have the same base currency.
	// ToDo: Allow any index.
	if hello.BtcIndex != server.btcIndex {
		return fmt.Errorf("Exchange subscription has wrong BTC index. Given: %s, Required: %s", hello.BtcIndex, server.btcIndex)
	}
	// Save the client for use in the main loop.
	client, sid := server.addClient(stream, hello)

	// Get the address for an Infof. Seems like peerInfo is always found, but
	// am checking anyway.
	peerInfo, peerFound := grpcPeer.FromContext(client.Stream().Context())
	var clientAddr string
	if peerFound {
		clientAddr = peerInfo.Addr.String()
		log.Infof("Client has connected from %s", clientAddr)
	}

	state := server.xcBot.State()
	// Send Decred exchanges.
	err = sendStateList(client, state.DcrBtc)
	if err != nil {
		return err
	}
	// Send Bitcoin-fiat indices.
	for token := range state.FiatIndices {
		err = client.SendExchangeUpdate(&dcrrates.ExchangeRateUpdate{
			Token:   token,
			Indices: server.xcBot.Indices(token),
		})
		if err != nil {
			log.Errorf("Error encountered while sending fiat indices to client at %s: %v", clientAddr, err)
			// Assuming the Done channel will be closed on error, no further iteration
			// is necessary.
			break
		}
	}

	// Keep stream alive.
	<-client.Stream().Context().Done()

	server.deleteClient(sid)
	log.Infof("Client at %s has disconnected", clientAddr)
	return
}

// addClient adds a client to the map and advance the streamCounter.
func (server *RateServer) addClient(stream GRPCStream, hello *dcrrates.ExchangeSubscription) (RateClient, StreamID) {
	server.clientLock.Lock()
	defer server.clientLock.Unlock()
	client := NewRateClient(stream, hello.GetExchanges())
	streamCounter++
	server.clients[streamCounter] = client
	return client, streamCounter
}

// deleteClient deletes the client from the map.
func (server *RateServer) deleteClient(sid StreamID) {
	server.clientLock.Lock()
	defer server.clientLock.Unlock()
	delete(server.clients, sid)
}

// A rateClient stores a client's gRPC stream and a list of exchange tokens
// to which they are subscribed. rateClient satisfies the RateClient interface.
type rateClient struct {
	stream    GRPCStream
	exchanges []string
}

// NewRateClient is a constructor for rate client. It returns the RateClient
// interface rather than rateClient itself.
func NewRateClient(stream GRPCStream, exchanges []string) RateClient {
	return &rateClient{
		stream:    stream,
		exchanges: exchanges,
	}
}

// Translate from the ExchangeBot's type to the gRPC type.
func makeExchangeRateUpdate(update *exchanges.ExchangeUpdate) *dcrrates.ExchangeRateUpdate {
	state := update.State
	protoUpdate := &dcrrates.ExchangeRateUpdate{
		Token:      update.Token,
		Price:      state.Price,
		BaseVolume: state.BaseVolume,
		Volume:     state.Volume,
		Change:     state.Change,
		Stamp:      state.Stamp,
	}
	if state.Candlesticks != nil {
		protoUpdate.Candlesticks = make([]*dcrrates.ExchangeRateUpdate_Candlesticks, 0, len(state.Candlesticks))
		for bin, sticks := range state.Candlesticks {
			candlesticks := &dcrrates.ExchangeRateUpdate_Candlesticks{
				Bin:    string(bin),
				Sticks: make([]*dcrrates.ExchangeRateUpdate_Candlestick, 0, len(sticks)),
			}
			for _, stick := range sticks {
				candlesticks.Sticks = append(candlesticks.Sticks, &dcrrates.ExchangeRateUpdate_Candlestick{
					High:   stick.High,
					Low:    stick.Low,
					Open:   stick.Open,
					Close:  stick.Close,
					Volume: stick.Volume,
					Start:  stick.Start.Unix(),
				})
			}
			protoUpdate.Candlesticks = append(protoUpdate.Candlesticks, candlesticks)
		}
	}

	if state.Depth != nil {
		depth := &dcrrates.ExchangeRateUpdate_DepthData{
			Time: state.Depth.Time,
			Bids: make([]*dcrrates.ExchangeRateUpdate_DepthPoint, 0, len(state.Depth.Bids)),
			Asks: make([]*dcrrates.ExchangeRateUpdate_DepthPoint, 0, len(state.Depth.Asks)),
		}
		for _, pt := range state.Depth.Bids {
			depth.Bids = append(depth.Bids, &dcrrates.ExchangeRateUpdate_DepthPoint{
				Quantity: pt.Quantity,
				Price:    pt.Price,
			})
		}
		for _, pt := range state.Depth.Asks {
			depth.Asks = append(depth.Asks, &dcrrates.ExchangeRateUpdate_DepthPoint{
				Quantity: pt.Quantity,
				Price:    pt.Price,
			})
		}
		protoUpdate.Depth = depth
	}

	return protoUpdate
}

// SendExchangeUpdate sends the update if the client is subscribed to the exchange.
func (client *rateClient) SendExchangeUpdate(update *dcrrates.ExchangeRateUpdate) (err error) {
	for i := range client.exchanges {
		if client.exchanges[i] == update.Token {
			err = client.stream.Send(update)
			return
		}
	}
	return
}

// Stream is a getter for the gRPC stream.
func (client *rateClient) Stream() GRPCStream {
	return client.stream
}

// Determine if the grpc.ServerStream's context Done() channel has been closed.
func (client *rateClient) isDone() bool {
	return client.stream.Context().Err() != nil
}
