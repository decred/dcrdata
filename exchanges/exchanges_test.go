// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/carterjones/signalr"
	"github.com/carterjones/signalr/hubs"
	"github.com/decred/dcrdata/dcrrates"
	"github.com/decred/slog"
)

func enableTestLog() {
	if log == slog.Disabled {
		UseLogger(slog.NewBackend(os.Stdout).Logger("EXE"))
		log.SetLevel(slog.LevelTrace)
	}
}

func makeKillSwitch() chan os.Signal {
	killSwitch := make(chan os.Signal, 1)
	signal.Notify(killSwitch, os.Interrupt)
	return killSwitch
}

func testExchanges(asSlave, quickTest bool, t *testing.T) {
	enableTestLog()

	// Skip this test during automated testing.
	if os.Getenv("GORACE") != "" {
		t.Skip("skipping exchange test")
	}

	ctx, shutdown := context.WithCancel(context.Background())

	killSwitch := makeKillSwitch()

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
		t.Fatalf("error creating bot. Shutting down: %v", err)
	}

	updateCounts := make(map[string]int)
	for token := range bot.Exchanges {
		updateCounts[token] = 0
	}
	logUpdate := func(token string) {
		if !quickTest {
			return
		}
		updateCounts[token]++
		lowest := updateCounts[token]
		for _, v := range updateCounts {
			if v < lowest {
				lowest = v
			}
		}
		if lowest > 0 {
			log.Infof("quick test conditions met. Shutting down early")
			shutdown()
		}
	}

	wg.Add(1)
	go bot.Start(ctx, wg)

	quitTimer := time.NewTimer(time.Minute * 7)
	ch := bot.UpdateChannels()

out:
	for {
		select {
		case update := <-ch.Exchange:
			logUpdate(update.Token)
			log.Infof("update received from exchange %s", update.Token)
		case update := <-ch.Index:
			logUpdate(update.Token)
			log.Infof("update received from index %s", update.Token)
		case <-ch.Quit:
			t.Errorf("ExchangeBot has quit.")
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
		for xc := range updateCounts {
			if xc == token {
				return
			}
		}
		t.Errorf("no update received for %s", token)
	}

	for _, token := range Tokens() {
		logMissing(token)
	}

	depth, err := bot.QuickDepth(aggregatedOrderbookKey)
	if err != nil {
		t.Errorf("failed to create aggregated orderbook")
	}
	log.Infof("aggregated orderbook size: %d kiB", len(depth)/1024)

	log.Infof("%d Bitcoin indices available", len(bot.AvailableIndices()))
	log.Infof("final state is %d kiB", len(bot.StateBytes())/1024)

	shutdown()
	wg.Wait()
}

func TestExchanges(t *testing.T) {
	testExchanges(false, false, t)
}

func TestSlaveBot(t *testing.T) {
	// Points to DCRData on local machine port 7778.
	// Start server with --exchange-refresh=1m --exchange-expiry=2m
	testExchanges(true, false, t)
}

func TestQuickExchanges(t *testing.T) {
	testExchanges(false, true, t)
}

func checkWsDepths(t *testing.T, depths *DepthData) {
	askLen := len(depths.Asks)
	bidLen := len(depths.Bids)
	log.Infof("%d asks", askLen)
	log.Infof("%d bids", bidLen)
	if askLen > 0 && bidLen > 0 {
		midGap := (depths.Asks[0].Price + depths.Bids[0].Price) / 2
		highRange := (depths.Asks[askLen-1].Price - midGap) / midGap * 100
		lowRange := (midGap - depths.Bids[bidLen-1].Price) / midGap * 100
		log.Infof("depth range +%f%% / -%f%%", highRange, lowRange)
	} else {
		t.Fatalf("missing orderbook data")
	}
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
    8768,
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
    8769,
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
	select {
	case <-poloniexDoneChannel:
	default:
		close(poloniexDoneChannel)
	}
}

func newTestPoloniexExchange() *PoloniexExchange {
	return &PoloniexExchange{
		CommonExchange: &CommonExchange{
			token: Poloniex,
			currentState: &ExchangeState{
				Price: 1,
			},
			channels: &BotChannels{
				exchange: make(chan *ExchangeUpdate, 2),
			},
			asks: make(wsOrders),
			buys: make(wsOrders),
		},
	}
}

func TestPoloniexWebsocket(t *testing.T) {
	enableTestLog()

	poloniex := newTestPoloniexExchange()
	poloniex.ws = &fakePoloniexWebsocket{}

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
	poloniex.buys = make(wsOrders)
	poloniex.asks = make(wsOrders)
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
	enableTestLog()

	// Skip this test during automated testing.
	if os.Getenv("GORACE") != "" {
		t.Skip("Skipping Poloniex websocket test")
	}

	killSwitch := makeKillSwitch()

	poloniex := newTestPoloniexExchange()
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
		poloniex.processWsMessage(b)
	}

	testConnectWs := func() {
		poloniexDoneChannel = make(chan struct{})
		poloniex.connectWebsocket(processor, &socketConfig{
			address: PoloniexURLs.Websocket,
		})
		poloniex.wsSend(poloniexOrderbookSubscription)
	}
	testConnectWs()
	select {
	case <-time.NewTimer(30 * time.Second).C:
	case <-killSwitch:
		t.Errorf("ctrl+c detected")
		return
	}
	// Test reconnection by forcing a fail, then checking the wsDepthStatus
	poloniex.setWsFail(fmt.Errorf("test failure. ignore"))
	tryHttp, initializing, depth := poloniex.wsDepthStatus(testConnectWs)
	if !tryHttp {
		t.Errorf("tryHttp not set as expected")
		return
	}
	if initializing {
		t.Errorf("websocket unexpectedly in initializing status")
		return
	}
	if depth != nil {
		t.Errorf("unexpected non-nil depth after forced websocket error")
		return
	}
	select {
	case <-time.NewTimer(30 * time.Second).C:
	case <-killSwitch:
		t.Errorf("ctrl+c detected")
		return
	}
	log.Infof("%d messages received", msgs)
	checkWsDepths(t, poloniex.wsDepths())
}

var (
	bittrexSignalrTemplate = signalr.Message{}
	bittrexMsgTemplate     = hubs.ClientMsg{}
	bittrexTestUpdateChan  = make(chan *BittrexOrderbookUpdate)
)

type testBittrexConnection struct {
	xc *BittrexExchange
}

func (conn testBittrexConnection) Close() {}

func (conn testBittrexConnection) IsOpen() bool {
	// Doesn't matter right now.
	return false
}

func (conn testBittrexConnection) Send(subscription hubs.ClientMsg) error {
	if subscription.M == "SubscribeToExchangeDeltas" {
		go func() {
			for update := range bittrexTestUpdateChan {
				if update == nil {
					return
				}
				conn.xc.msgHandler(signalr.Message{
					M: []hubs.ClientMsg{
						hubs.ClientMsg{
							M: updateMsgKey,
							A: []interface{}{update},
						},
					},
				})
			}
		}()
	}
	if subscription.M == "QueryExchangeState" {
		go func() {
			book := BittrexOrderbookUpdate{
				Nonce:      2,
				MarketName: "BTC-DCR",
				Buys: []*BittrexWsOrder{
					&BittrexWsOrder{
						Quantity: 5.,
						Rate:     5.,
						Type:     BittrexOrderAdd,
					},
					&BittrexWsOrder{
						Quantity: 5.,
						Rate:     6.,
						Type:     BittrexOrderAdd,
					},
				},
				Sells: []*BittrexWsOrder{
					&BittrexWsOrder{
						Quantity: 5.,
						Rate:     105.,
						Type:     BittrexOrderAdd,
					},
					&BittrexWsOrder{
						Quantity: 5.,
						Rate:     106.,
						Type:     BittrexOrderAdd,
					},
				},
				Fills: []*BittrexWsFill{},
			}
			msgBytes, _ := json.Marshal(book)
			conn.xc.msgHandler(signalr.Message{
				I: "1",
				R: json.RawMessage(msgBytes),
			})
		}()
	}
	return nil
}

func newTestBittrexExchange() *BittrexExchange {
	bittrex := &BittrexExchange{
		CommonExchange: &CommonExchange{
			token: Bittrex,
			currentState: &ExchangeState{
				Price: 1,
			},
			channels: &BotChannels{
				exchange: make(chan *ExchangeUpdate, 2),
			},
			asks: make(wsOrders),
			buys: make(wsOrders),
		},
		queue: make([]*BittrexOrderbookUpdate, 0),
	}
	bittrex.sr = testBittrexConnection{xc: bittrex}
	return bittrex
}

func TestBittrexWebsocket(t *testing.T) {
	defer close(bittrexTestUpdateChan)

	bittrex := newTestBittrexExchange()

	template := func() BittrexOrderbookUpdate {
		return BittrexOrderbookUpdate{
			Buys:  []*BittrexWsOrder{},
			Sells: []*BittrexWsOrder{},
			Fills: []*BittrexWsFill{},
		}
	}

	bittrex.sr.Send(bittrexWsOrderUpdateRequest)
	checkUpdate := func(test string, update *BittrexOrderbookUpdate, askLen, buyLen int) {
		bittrexTestUpdateChan <- update
		// That should trigger the order book to be requested
		<-time.NewTimer(time.Millisecond * 100).C
		// Check that the initial orderbook was processed.
		bittrex.orderMtx.RLock()
		defer bittrex.orderMtx.RUnlock()
		if len(bittrex.asks) != askLen {
			t.Fatalf("bittrex asks slice has unexpected length %d for test ''%s'", len(bittrex.asks), test)
		}
		if len(bittrex.buys) != buyLen {
			t.Fatalf("bittrex buys slice has unexpected length %d for test ''%s'", len(bittrex.buys), test)
		}
	}

	// Set up a buy order that should be ignored because the nonce is lower than
	// the initial nonce is too low. This update should be queued and eventually
	// discarded.
	update := template()
	update.Buys = []*BittrexWsOrder{
		&BittrexWsOrder{
			Quantity: 5.,
			Rate:     4.,
			Type:     BittrexOrderAdd,
		},
	}
	update.Nonce = 2
	checkUpdate("add early nonce", &update, 2, 2)

	// Remove a buy order
	update = template()
	update.Nonce = 3
	update.Buys = []*BittrexWsOrder{
		&BittrexWsOrder{
			Quantity: 0.,
			Rate:     5.,
			Type:     BittrexOrderRemove,
		},
	}
	checkUpdate("remove buy", &update, 2, 1)

	// Add a sell order
	update = template()
	update.Nonce = 4
	update.Sells = []*BittrexWsOrder{
		&BittrexWsOrder{
			Quantity: 0.,
			Rate:     107.,
			Type:     BittrexOrderAdd,
		},
	}
	checkUpdate("add sell", &update, 3, 1)

	// Update a sell order
	update = template()
	update.Nonce = 5
	update.Sells = []*BittrexWsOrder{
		&BittrexWsOrder{
			Quantity: 0.,
			Rate:     107.,
			Type:     BittrexOrderUpdate,
		},
	}
	checkUpdate("update sell", &update, 3, 1)

	if bittrex.wsFailed() {
		t.Fatalf("bittrex websocket unexpectedly failed")
	}

	// Add too many out of order updates. Should trigger a failed state.
	for i := 0; i < maxBittrexQueueSize+1; i++ {
		update := template()
		update.Nonce = 1000
		bittrexTestUpdateChan <- &update
	}
	<-time.NewTimer(time.Millisecond * 100).C
	if !bittrex.wsFailed() {
		t.Fatalf("bittrex not in failed state as expected")
	}
}

func TestBittrexLiveWebsocket(t *testing.T) {
	enableTestLog()

	// Skip this test during automated testing.
	if os.Getenv("GORACE") != "" {
		t.Skip("Skipping Bittrex websocket test")
	}

	killSwitch := makeKillSwitch()

	bittrex := newTestBittrexExchange()

	bittrex.connectWs()
	sr := bittrex.signalr()
	if sr == nil {
		t.Errorf("failed to initialize signalr client")
	}
	defer sr.Close()

	testDuration := 180
	log.Infof("listening for %d seconds total", testDuration*2)
	select {
	case <-time.NewTimer(time.Second * time.Duration(testDuration)).C:
	case <-killSwitch:
		t.Errorf("ctrl+c detected")
		return
	}
	// Test reconnection by forcing a fail, then checking the wsDepthStatus.
	bittrex.setWsFail(fmt.Errorf("test failure. ignore"))
	tryHttp, initializing, depth := bittrex.wsDepthStatus(bittrex.connectWs)
	if !tryHttp {
		t.Errorf("tryHttp not set as expected")
		return
	}
	if initializing {
		// initializing is only true the first time the socket is started.
		t.Errorf("websocket unexpectedly in initializing status")
		return
	}
	if depth != nil {
		t.Errorf("unexpected non-nil depth after forced websocket error")
		return
	}
	select {
	case <-time.NewTimer(time.Second * time.Duration(testDuration)).C:
	case <-killSwitch:
		t.Errorf("ctrl+c detected")
		return
	}
	if bittrex.wsFailed() {
		t.Fatalf("bittrex connection in failed state")
	}
	checkWsDepths(t, bittrex.wsDepths())
}
