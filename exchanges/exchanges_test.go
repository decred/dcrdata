// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrdata/dcrrates"
	"github.com/decred/slog"
)

func testExchanges(asSlave bool, t *testing.T) {
	UseLogger(slog.NewBackend(os.Stdout).Logger("EXE"))
	log.SetLevel(slog.LevelTrace)

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
		config.MasterBot = ":7778"
		config.MasterCertFile = filepath.Join(dcrrates.DefaultAppDirectory, dcrrates.DefaultCertName)
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
		case update := <-ch.Exchange:
			updated = append(updated, update.Token)
			log.Infof("Update received from exchange %s", update.Token)
		case update := <-ch.Index:
			updated = append(updated, update.Token)
			log.Infof("Update received from index %s", update.Token)
		case <-ch.Quit:
			t.Errorf("Exchange bot has quit.")
			break out
		case <-quitTimer.C:
			break out
		case <-ctx.Done():
			break out
		}
	}

	if bot.IsFailed() {
		log.Infof("ExchangeBot is in failed state")
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
	log.Infof("final state is %d kiB", len(bot.StateBytes())/1024)

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

var initialPoloniexOrderbook = []byte(`[
        14,
        8767,
        [
                [
                        "i",
                        {
                                "currencyPair": "BTC_BTS",
                                "orderBook": [
                                        {
                                                "0.00011358": "127734.81648491",
                                                "0.00011359": "667.14834444",
                                                "0.00011360": "3651.66059723",
                                                "0.00011361": "200.14590282",
                                                "0.00011362": "4816.12553510",
                                                "0.00011363": "37.08390161",
                                                "0.00011365": "3419.78939376",
                                                "0.00011366": "8.05270863",
                                                "0.00011367": "73239.96650974",
                                                "0.00011368": "7958.06486028",
                                                "0.00011369": "142.68135365",
                                                "0.00011370": "24411.40000000",
                                                "0.00011372": "244147.92356157"
                                        },
                                        {
                                                "0.00001358": "27734.81648491",
                                                "0.00001359": "67.14834444",
                                                "0.00001360": "651.66059723",
                                                "0.00001361": "20.14590282",
                                                "0.00001362": "816.12553510",
                                                "0.00001363": "7.08390161",
                                                "0.00001365": "419.78939376",
                                                "0.00001366": ".05270863",
                                                "0.00001367": "3239.96650974",
                                                "0.00001368": "958.06486028",
                                                "0.00001369": "42.68135365",
                                                "0.00001370": "4411.40000000",
                                                "0.00001371": "44147.92356157"
                                        }
                                 ]
                        }
                ]
        ]
]`)

var poloniexEmptyUpdate = []byte(`[
    1010
]`)

var poloniexOrderbookUpdate = []byte(`[
    14,
    85537933,
    [
        [
            "o",
            0,
            "0.00011358",
						"0.00000000"
        ],
				[
            "o",
            1,
            "0.00001372",
						"1.00000000"
        ]
    ]
]`)

var poloniexTrade = []byte(`[
    14,
    85537933,
    [
			[
					"t",
					"10115654",
					1,
					"0.00011359",
					"667.14834444",
					1554856977
			]
    ]
]`)

// Satisfies the websocketFeed interface
type fakePoloniexWebsocket struct{}

var poloniexDoneChannel = make(chan struct{})

var poloniexReadCount int

// Done() chan struct{}
// Read() ([]byte, error)
// Write(interface{}) error
// Close()

func (p *fakePoloniexWebsocket) Done() chan struct{} {
	return poloniexDoneChannel
}

func (p *fakePoloniexWebsocket) Read() ([]byte, error) {
	poloniexReadCount++
	switch poloniexReadCount {
	case 1:
		return initialPoloniexOrderbook, nil
	case 2:
		time.Sleep(100 * time.Millisecond)
		return poloniexEmptyUpdate, nil
	case 3:
		time.Sleep(100 * time.Millisecond)
		return poloniexOrderbookUpdate, nil
	}
	<-poloniexDoneChannel
	return nil, fmt.Errorf("closed (expected)")
}

func (p *fakePoloniexWebsocket) Write(interface{}) error {
	return nil
}

func (p *fakePoloniexWebsocket) Close() {
	close(poloniexDoneChannel)
}

func TestPoloniexWebsocket(t *testing.T) {
	UseLogger(slog.NewBackend(os.Stdout).Logger("EXE"))
	log.SetLevel(slog.LevelTrace)

	poloniex := PoloniexExchange{
		CommonExchange: &CommonExchange{
			token: Poloniex,
			currentState: &ExchangeState{
				Price: 1,
			},
			channels: &BotChannels{
				exchange: make(chan *ExchangeUpdate, 2),
			},
			ws: &fakePoloniexWebsocket{},
		},
		buys: make(poloniexOrders),
		asks: make(poloniexOrders),
	}

	checkLengths := func(askLen, buyLen int) {
		if len(poloniex.asks) != askLen || len(poloniex.buys) != buyLen {
			t.Errorf("unexpected order book lengths (%d, %d). expected (%d, %d)",
				len(poloniex.asks), len(poloniex.buys), askLen, buyLen)
		}
	}
	poloniex.processWsMessage(initialPoloniexOrderbook)
	checkLengths(13, 13)
	poloniex.processWsMessage(poloniexEmptyUpdate)
	checkLengths(13, 13)
	// The update includes a deletion in the asks and a new bin in the buys.
	poloniex.processWsMessage(poloniexOrderbookUpdate)
	checkLengths(12, 14)
	depth := poloniex.wsDepths()
	poloniex.processWsMessage(poloniexTrade)
	if len(depth.Asks) != 12 || len(depth.Bids) != 14 {
		t.Errorf("unexpected depth data lengths (%d, %d). expected (12, 14)", len(depth.Asks), len(depth.Bids))
	}

	poloniex.wsProcessor = poloniex.processWsMessage
	poloniex.buys = make(poloniexOrders)
	poloniex.asks = make(poloniexOrders)
	poloniex.currentState = &ExchangeState{Price: 1}
	poloniex.startWebsocket()
	time.Sleep(300 * time.Millisecond)
	poloniex.ws.Close()
	time.Sleep(100 * time.Millisecond)
	depth = poloniex.wsDepths()
	if len(depth.Asks) != 12 || len(depth.Bids) != 14 {
		t.Errorf("unexpected depth data lengths (%d, %d). expected (12, 14)", len(depth.Asks), len(depth.Bids))
	}
	if poloniex.wsListening() {
		t.Errorf("poloniex websocket unexpectedly listening")
	}
	if !poloniex.wsFailed() {
		t.Errorf("poloniex should be in failed state, but isn't")
	}
	if poloniex.wsErrorCount() != 1 {
		t.Errorf("unexpected poloniex websocket error count: %d", poloniex.wsErrorCount())
	}
}

func TestPoloniexLiveWebsocket(t *testing.T) {
	UseLogger(slog.NewBackend(os.Stdout).Logger("EXE"))
	log.SetLevel(slog.LevelTrace)

	// Skip this test during automated testing.
	if os.Getenv("GORACE") != "" {
		t.Skip("Skipping exchange test")
	}

	poloniex := PoloniexExchange{
		CommonExchange: &CommonExchange{
			token: Poloniex,
			currentState: &ExchangeState{
				Price: 1,
			},
			channels: &BotChannels{
				exchange: make(chan *ExchangeUpdate, 2),
			},
		},
		buys: make(poloniexOrders),
		asks: make(poloniexOrders),
	}

	var msgs int
	processor := func(b []byte) {
		msgs++
		var s string
		if len(b) >= 128 {
			s = string(b[:128]) + "..."
		} else {
			s = string(b)
		}
		if s == "[1010]" {
			log.Infof("heartbeat")
		} else {
			log.Infof("message received: %s", s)
		}
	}

	testConnectWs := func() {
		poloniexDoneChannel = make(chan struct{})
		poloniex.connectWebsocket(processor, &socketConfig{
			address: PoloniexURLs.Websocket,
		})
		poloniex.wsSend(poloniexOrderbookSubscription)
	}
	testConnectWs()
	time.Sleep(30 * time.Second)
	// Test reconnection
	poloniex.ws.Close()
	testConnectWs()
	time.Sleep(30 * time.Second)
	log.Infof("%d messages received", msgs)
}
