// Copyright (c) 2019-2021, The Decred developers
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

	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrdata/exchanges/v3"
	dcrrates "github.com/decred/dcrdata/exchanges/v3/ratesproto"
	"google.golang.org/grpc"
)

// Default TLS configuration.
const (
	DefaultKeyName  = "rpc.key"
	DefaultCertName = "rpc.cert"
)

var (
	// DefaultAppDirectory is the default location of the rateserver application
	// data folder.
	DefaultAppDirectory = dcrutil.AppDataDir("rateserver", false)
)

func main() {
	killSwitch := make(chan os.Signal, 1)
	signal.Notify(killSwitch, os.Interrupt)

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}

	err = os.MkdirAll(cfg.AppDirectory, 0700)
	if err != nil {
		fmt.Printf("Unable to create application directory: %v", err)
		return
	}

	initializeLogging(filepath.Join(cfg.LogPath, "rateserver.log"), cfg.LogLevel)

	if cfg.CertificatePath == "" || cfg.KeyPath == "" {
		log.Errorf("TLS certificate and key files must be provided")
		return
	}
	creds, err := openRPCKeyPair(cfg)
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

	var wg sync.WaitGroup
	wg.Add(1)
	go xcBot.Start(ctx, &wg)

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

	printUpdate := func(token string) {
		msg := fmt.Sprintf("Update received from %s", token)
		if !xcBot.IsFailed() {
			msg += fmt.Sprintf(". Current price: %.2f %s", xcBot.Price(), xcBot.BtcIndex)
		}
		log.Infof(msg)
	}

	sendUpdate := func(update *dcrrates.ExchangeRateUpdate) {
		rateServer.clientLock.RLock()
		for _, client := range rateServer.clients {
			err := client.SendExchangeUpdate(update)
			if err != nil {
				log.Warnf("send error: %v", err)
			}
		}
		rateServer.clientLock.RUnlock()
	}

	// Start the main loop in a goroutine, shutting down the grpcServer when done.
	go func() {
	out:
		for {
			select {
			case <-killSwitch:
				break out
			case update := <-xcSignals.Exchange:
				printUpdate(update.Token)
				sendUpdate(makeExchangeRateUpdate(update))
			case update := <-xcSignals.Index:
				printUpdate(update.Token)
				sendUpdate(&dcrrates.ExchangeRateUpdate{
					Token:   update.Token,
					Indices: update.Indices,
				})
			case <-xcSignals.Quit:
				log.Infof("ExchangeBot Quit signal received.")
				break out
			}
		}
		shutdown()
		grpcServer.Stop()
	}()

	if err = grpcServer.Serve(listener); err != nil {
		log.Errorf("Failed to start gRPC server listening: %v", err)
		shutdown()
	}

	wg.Wait()
}
