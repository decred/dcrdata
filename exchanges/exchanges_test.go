// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"testing"
	"time"

	"github.com/decred/slog"
)

func TestExchanges(t *testing.T) {
	quickTest := false
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
	config.DataExpiry = "2m"
	config.RequestExpiry = "4m"
	config.Indent = true
	config.Disabled = make([]string, 0)
	bot, err := NewExchangeBot(config)
	if err != nil {
		t.Errorf("Error creating bot. Shutting down: %v", err)
		shutdown()
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
			if quickTest && bot.IsUpdated() {
				break out
			}
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
