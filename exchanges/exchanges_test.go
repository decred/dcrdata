// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/slog"
)

func testExchanges(asSlave bool, t *testing.T) {
	UseLogger(slog.NewBackend(os.Stdout).Logger("EXE"))

	// Skip this test during automated testing.
	if os.Getenv("GORACE") != "" {
		t.Skip("Skipping exchange test")
	}

	ctx, shutdown := context.WithCancel(context.Background())

	killSwitch := make(chan os.Signal, 1)
	signal.Notify(killSwitch, os.Interrupt)

	wg := new(sync.WaitGroup)

	wg.Add(1)
	go func() {
		select {
		case <-killSwitch:
			shutdown()
		case <-ctx.Done():
		}
		wg.Done()
	}()

	config := new(ExchangeBotConfig)
	config.Disabled = make([]string, 0)
	config.Indent = true
	if asSlave {
		config.MasterBot = "127.0.0.1:7778"
		config.MasterCertFile = filepath.Join(dcrutil.AppDataDir("dcrdata", false), "rpc.cert")
	} else {
		config.DataExpiry = "2m"
		config.RequestExpiry = "4m"
	}
	bot, err := NewExchangeBot(config)
	if err != nil {
		shutdown()
		t.Fatalf("Error creating bot. Shutting down: %v", err)
	}

	wg.Add(1)
	go bot.Start(ctx, wg)

	quitTimer := time.NewTimer(time.Minute * 7)
	var updated []string

	ch := bot.UpdateChannels()

out:
	for {
		select {
		case update := <-ch.Update:
			updated = append(updated, update.Token)
			log.Infof("Update recieved from %s", update.Token)
		case <-ch.Quit:
			t.Errorf("Exchange bot has quit.")
			break out
		case <-quitTimer.C:
			break out
		case <-ctx.Done():
			break out
		}
	}

	if !bot.IsFailed() {
		log.Infof("Final state: %s", string(bot.StateBytes()))
	}

	logMissing := func(token string) {
		for _, xc := range updated {
			if xc == token {
				return
			}
		}
		t.Errorf("No update received for %s", token)
	}

	for _, token := range Tokens() {
		logMissing(token)
	}

	log.Infof("%d Bitcoin indices available", len(bot.AvailableIndices()))
	log.Infof(string(bot.StateBytes()))

	shutdown()
	wg.Wait()
}

func TestExchanges(t *testing.T) {
	testExchanges(false, t)
}

func TestSlaveBot(t *testing.T) {
	// Points to DCRData on local machine port 7778.
	// Start server with --exchange-refresh=1m --exchange-expiry=2m
	testExchanges(true, t)
}
