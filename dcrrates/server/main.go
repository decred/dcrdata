// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"

	"github.com/decred/dcrdata/v4/dcrrates"
	"github.com/decred/dcrdata/v4/exchanges"
	"google.golang.org/grpc"
)

func main() {
	killSwitch := make(chan os.Signal, 1)
	signal.Notify(killSwitch, os.Interrupt)

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}

	inititializeLogging(filepath.Join(cfg.LogPath, "dcrrate-server.log"))

	if cfg.CertificatePath == "" || cfg.KeyPath == "" {
		log.Errorf("TLS certificate and key files must be provided")
		return
	}
	creds, err := openRPCKeyPair(cfg.CertificatePath, cfg.KeyPath)
	if err != nil {
		log.Errorf("TLS certificate error: %v", err)
		return
	}

	// Initialize the ExchangeBot
	var xcBot *exchanges.ExchangeBot
	botCfg := exchanges.ExchangeBotConfig{
		DataExpiry:    cfg.ExchangeRefresh,
		RequestExpiry: cfg.ExchangeExpiry,
		BtcIndex:      cfg.ExchangeCurrency,
	}
	if cfg.DisabledExchanges != "" {
		botCfg.Disabled = strings.Split(cfg.DisabledExchanges, ",")
	}
	xcBot, err = exchanges.NewExchangeBot(&botCfg)
	if err != nil {
		log.Errorf("Could not create exchange monitor: %v", err)
		return
	}
	var xcList, prepend string
	for k := range xcBot.Exchanges {
		xcList += prepend + k
		prepend = ", "
	}
	log.Infof("ExchangeBot monitoring %s", xcList)

	xcSignals := xcBot.UpdateChannels()

	ctx, shutdown := context.WithCancel(context.Background())
	wg := new(sync.WaitGroup)

	wg.Add(1)
	go xcBot.Start(ctx, wg)

	rateServer := NewRateServer(cfg.ExchangeCurrency, xcBot)

	// Set up gRPC server.
	listener, err := net.Listen("tcp", cfg.GRPCListen)
	if err != nil {
		log.Errorf("Failed to create net.Listener at %s", cfg.GRPCListen)
		shutdown()
		return
	}
	grpcServer := grpc.NewServer(grpc.Creds(creds))
	dcrrates.RegisterDCRRatesServer(grpcServer, rateServer)

	// Start the main loop in a goroutine, shutting down the grpcServer when done.
	go func() {
	out:
		for {
			select {
			case <-killSwitch:
				break out
			case update := <-xcSignals.Update:
				var grpcUpdate *dcrrates.ExchangeRateUpdate
				msg := fmt.Sprintf("Update received from %s", update.Token)
				if !xcBot.IsFailed() {
					msg += fmt.Sprintf(". Current price: %.2f %s", xcBot.Price(), xcBot.BtcIndex)
				}
				log.Infof(msg)
				if exchanges.IsDcrExchange(update.Token) {
					state, err := update.TriggerState()
					if err != nil {
						log.Errorf("Failed to get TriggerState: %v", err)
						continue
					}
					grpcUpdate = makeExchangeUpdate(update.Token, state)
					if err != nil {
						log.Errorf("No exchange state for %s found in update", update.Token)
						continue
					}
				} else {
					grpcUpdate = &dcrrates.ExchangeRateUpdate{
						Token:   update.Token,
						Indices: xcBot.Indices(update.Token),
					}
				}
				rateServer.clientLock.RLock()
				for _, client := range rateServer.clients {
					err = client.SendExchangeUpdate(grpcUpdate)
					if err != nil {
						log.Warnf("send error: %v")
					}
				}
				rateServer.clientLock.RUnlock()
			case <-xcSignals.Quit:
				log.Infof("ExchangeBot Quit signal received.")
				break out
			}
		}
		shutdown()
		grpcServer.Stop()
		wg.Wait()
	}()

	grpcServer.Serve(listener)

}
