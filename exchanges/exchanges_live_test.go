// +build livexc
// run these tests with go test -race -tags-livexc -run FuncName

package exchanges

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrutil/v3"
)

func makeKillSwitch() chan os.Signal {
	killSwitch := make(chan os.Signal, 1)
	signal.Notify(killSwitch, os.Interrupt)
	return killSwitch
}

func testExchanges(asSlave, quickTest bool, t *testing.T) {
	enableTestLog()

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
		appDirectory := dcrutil.AppDataDir("rateserver", false)
		config.MasterCertFile = filepath.Join(appDirectory, "rpc.cert")
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

func TestPoloniexLiveWebsocket(t *testing.T) {
	enableTestLog()

	// Skip this test during automated testing.
	if os.Getenv("GORACE") != "" {
		t.Skip("Skipping Poloniex websocket test")
	}

	killSwitch := makeKillSwitch()

	poloniex := newTestPoloniexExchange()
	processor := func(b []byte) {
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
	// subsequent calls to close should be inconsequential.
	poloniex.ws.Close()
	poloniex.ws.Close()
	// wsDepthStatus should recognize the closed connection and create a real
	// websocket connection, signalling to use the HTTP fallback in the meantime.
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
	checkWsDepths(t, poloniex.wsDepths())
	poloniex.ws.Close()
}
func TestBittrexLiveWebsocket(t *testing.T) {
	enableTestLog()
	// Skip this test during automated testing.
	if os.Getenv("GORACE") != "" {
		t.Skip("Skipping Bittrex websocket test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killSwitch := makeKillSwitch()
	go func() {
		select {
		case <-killSwitch:
			cancel()
			return
		case <-ctx.Done():
			return
		}
	}()

	chans := &BotChannels{
		index:    make(chan *IndexUpdate),
		exchange: make(chan *ExchangeUpdate, 1),
		done:     make(chan struct{}),
	}

	xc, err := NewBittrex(new(http.Client), chans)
	if err != nil {
		t.Fatalf("NewBittrex error: %v", err)
	}

	bittrex := xc.(*BittrexExchange)

	ticker := time.NewTicker(time.Second * 10)
	testTimeout := time.NewTimer(time.Minute * 2)
	bittrex.Refresh()
	for {
		select {
		case <-ticker.C:
			bittrex.Refresh()
		case <-testTimeout.C:
			cancel()
			return
		case xcUpdate := <-chans.exchange:
			fmt.Printf("Bittrex emitted an exchange update: %d bids, %d asks, %d candlestick sets \n",
				len(xcUpdate.State.Depth.Bids), len(xcUpdate.State.Depth.Asks), len(xcUpdate.State.Candlesticks))
		case <-ctx.Done():
			return
		}
	}
}

func TestDecredDEXLive(t *testing.T) {
	enableTestLog()

	ctx, cancel := context.WithCancel(context.Background())

	chans := &BotChannels{
		index:    make(chan *IndexUpdate),
		exchange: make(chan *ExchangeUpdate),
		done:     make(chan struct{}),
	}

	killSwitch := makeKillSwitch()

	usr, _ := user.Current()
	dextestCertPath := filepath.Join(usr.HomeDir, "dextest", "dcrdex", "rpc.cert")
	cert, err := ioutil.ReadFile(dextestCertPath)
	if err != nil {
		log.Errorf("error reading file for simnet cert: %v", err)
		return
	}

	constructor := NewDecredDEXConstructor(&DEXConfig{
		Token:    DexDotDecred,
		Host:     "127.0.0.1:17273",
		Cert:     cert,
		CertHost: "127.0.0.1",
	})

	xc, err := constructor(nil, chans)
	if err != nil {
		t.Fatalf("NewDecredDEX error: %v", err)
	}
	dcr := xc.(*DecredDEX)
	defer func() { dcr.ws.Close() }()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-killSwitch:
			cancel()
		case <-ctx.Done():
			return
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-chans.index:
				log.Infof("Why are we receiving index updates?")
			case u := <-chans.exchange:
				log.Infof("Exchange update received: %s", mustEncode(t, u))
			}
		}
	}()
	ticker := time.NewTicker(time.Second * 5)
	testTimeout := time.NewTimer(time.Second * 90)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ticker.C:
				dcr.Refresh()
			case <-testTimeout.C:
				cancel()
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
}
