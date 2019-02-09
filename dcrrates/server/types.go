// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/decred/dcrdata/v4/dcrrates"
	"github.com/decred/dcrdata/v4/exchanges"
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
func sendStateList(client RateClient, updates map[string]*exchanges.ExchangeState) (err error) {
	for token, update := range updates {
		err = client.SendExchangeUpdate(makeExchangeUpdate(token, update))
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
	// For now, require the ExhcnageBot clients to have the same base currency.
	// ToDo: Allow any index.
	if hello.BtcIndex != server.btcIndex {
		return fmt.Errorf("Exchange subscription has wrong BTC index. Given: %s, Required: %s", hello.BtcIndex, server.btcIndex)
	}
	// Save the client for use in the main loop.
	client, sid := server.AddClient(stream, hello)
	state := server.xcBot.State()
	// Send Decred exchanges.
	err = sendStateList(client, state.DcrBtc)
	if err != nil {
		return err
	}
	// Send Bitcoin-fiat indices.
	for token := range state.FiatIndices {
		client.SendExchangeUpdate(&dcrrates.ExchangeRateUpdate{
			Token:   token,
			Indices: server.xcBot.Indices(token),
		})
	}

	// Get the address for an Infof. Seems like peerInfo is always found, but
	// am checking anyway.
	peerInfo, peerFound := grpcPeer.FromContext(client.Stream().Context())
	if peerFound {
		log.Infof("Client has connected from %s", peerInfo.Addr.String())
	}

	// Keep stream alive.
	<-client.Stream().Context().Done()

	server.DeleteClient(sid)
	if peerFound {
		log.Infof("Client at %s has disconnected", peerInfo.Addr.String())
	}
	return
}

// AddClient adds a client to the map and advance the streamCounter.
func (server *RateServer) AddClient(stream GRPCStream, hello *dcrrates.ExchangeSubscription) (RateClient, StreamID) {
	server.clientLock.Lock()
	defer server.clientLock.Unlock()
	client := NewRateClient(stream, hello.GetExchanges())
	streamCounter++
	server.clients[streamCounter] = client
	return client, streamCounter
}

// DeleteClient deletes the client from the map.
func (server *RateServer) DeleteClient(sid StreamID) {
	server.clientLock.Lock()
	defer server.clientLock.Unlock()
	delete(server.clients, sid)
}

// A rateClient stores a cient's gRPC stream and a list of exchange tokens
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
func makeExchangeUpdate(token string, state *exchanges.ExchangeState) *dcrrates.ExchangeRateUpdate {
	return &dcrrates.ExchangeRateUpdate{
		Token:      token,
		Price:      state.Price,
		BaseVolume: state.BaseVolume,
		Volume:     state.Volume,
		Change:     state.Change,
		Stamp:      state.Stamp,
	}
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
