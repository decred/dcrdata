// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/carterjones/signalr"
	"github.com/carterjones/signalr/hubs"
	"github.com/decred/slog"
)

func enableTestLog() {
	if log == slog.Disabled {
		UseLogger(slog.NewBackend(os.Stdout).Logger("EXE"))
		log.SetLevel(slog.LevelTrace)
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

var poloMtx sync.Mutex
var poloOn bool = true

func (p *fakePoloniexWebsocket) Close() {
	poloMtx.Lock()
	defer poloMtx.Unlock()
	if poloOn {
		poloOn = false
		close(poloniexDoneChannel)
	}
}

func (p *fakePoloniexWebsocket) On() bool {
	poloMtx.Lock()
	defer poloMtx.Unlock()
	return poloOn
}

func newTestPoloniexExchange() *PoloniexExchange {
	return &PoloniexExchange{
		CommonExchange: &CommonExchange{
			token: Poloniex,
			currentState: &ExchangeState{
				BaseState: BaseState{Price: 1},
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
	poloniex.currentState = &ExchangeState{BaseState: BaseState{Price: 1}}
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

type tDoer struct {
	responses []*http.Response
}

func (d *tDoer) Do(*http.Request) (*http.Response, error) {
	if len(d.responses) == 0 {
		return nil, fmt.Errorf("no test response queued")
	}
	resp := d.responses[0]
	d.responses = d.responses[1:]
	return resp, nil
}

func (d *tDoer) queue(body interface{}) *http.Response {
	b, err := json.Marshal(body)
	if err != nil {
		panic("tDoer.queue error:" + err.Error())
	}
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(b)),
	}
	d.responses = append(d.responses, resp)
	return resp
}

type testBittrexConnection struct {
	xc *BittrexExchange
}

func (conn testBittrexConnection) Close() {}

func (conn testBittrexConnection) On() bool {
	// Doesn't matter right now.
	return false
}

func (conn testBittrexConnection) Send(hubs.ClientMsg) error {
	return nil
}

func newTestBittrexExchange() (*BittrexExchange, *tDoer) {
	doer := &tDoer{}
	bittrex := &BittrexExchange{
		CommonExchange: &CommonExchange{
			token: Bittrex,
			currentState: &ExchangeState{
				BaseState: BaseState{Price: 1},
			},
			channels: &BotChannels{
				exchange: make(chan *ExchangeUpdate, 2),
			},
			asks:   make(wsOrders),
			buys:   make(wsOrders),
			client: doer,
		},
		queue: make([]*BittrexOrderbookUpdate, 0),
	}
	bittrex.sr = testBittrexConnection{xc: bittrex}
	return bittrex, doer
}

func TestBittrexWebsocket(t *testing.T) {
	var seq uint64

	// helper function to prepare a websocket orderbook update.
	obUpdate := func(bids []*BittrexOrderbookDelta, asks []*BittrexOrderbookDelta) signalr.Message {
		t.Helper()
		u := BittrexOrderbookUpdate{
			MarketSymbol: "DCR-BTC",
			Depth:        123,
			Sequence:     atomic.AddUint64(&seq, 1),
			BidDeltas:    bids,
			AskDeltas:    asks,
		}

		b, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("json.Marshal error: %v", err)
		}

		var buf bytes.Buffer
		fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			t.Fatalf("flate NewWriter error: %v", err)
		}
		if _, err := io.Copy(fw, bytes.NewReader(b)); err != nil {
			t.Fatal(err)
		}
		if err := fw.Close(); err != nil {
			t.Fatalf("flate Close error: %v", err)
		}

		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

		return signalr.Message{
			M: []hubs.ClientMsg{
				{
					M: BittrexMsgBookUpdate,
					A: []interface{}{b64},
				},
			},
		}
	}

	bittrex, doer := newTestBittrexExchange()

	// Create the book, but don't send it yet. Test the sequencing logic by
	// sending an update before the book.
	ob := &BittrexDepthResponse{
		Seq: atomic.AddUint64(&seq, 1),
		Bid: []*BittrexRateQty{
			{
				Rate: 123456,
				Qty:  654321,
			},
		},
		Ask: []*BittrexRateQty{},
	}

	// Send an update that comes before the book. This update should be cached
	// and re-processed after the book is received.
	asks := []*BittrexOrderbookDelta{{
		Qty:  456789,
		Rate: 987654,
	}}
	bittrex.msgHandler(obUpdate(nil, asks))

	bittrex.processFullOrderbook(ob)

	// Should have one update in the exchange channel.
	var u *ExchangeUpdate
	select {
	case u = <-bittrex.channels.exchange:
	case <-time.After(time.Second):
		t.Fatalf("no exchange update received")
	}

	depth := u.State.Depth
	if len(depth.Asks) != 1 {
		t.Fatalf("pre-update ask not added")
	}
	if len(depth.Bids) != 1 {
		t.Fatalf("orderbook bid not added")
	}

	// Now remove a bin by sending through a zero-quantity update.
	asks = []*BittrexOrderbookDelta{{
		Qty:  0,
		Rate: 987654,
	}}
	bittrex.msgHandler(obUpdate(nil, asks))

	depths := bittrex.wsDepths()
	if len(depths.Asks) != 0 {
		t.Fatalf("failed to remove order bin")
	}

	// Test some Refresh paths.
	queueRefresh := func(initializing, failed bool) {
		doer.queue(&BittrexPriceResponse{})
		doer.queue(&BittrexMarketSummary{})
		// xc.wsSync.init.After(xc.wsSync.fail) => wsListening() == true
		// xc.wsSync.fail.After(xc.wsSync.init) => wsFailed() == true
		// 1. if wsListening() == true, wsDepthStatus will return the *DepthData.
		// 2. if xc.wsSync.init == xc.wsSync.fail, wsDepthStatus will return false
		// for tryHttp, and false for initializing.
		// 3. if wsFailed() == , wsDepthStatus will return true for tryHttp,
		// simulating an error state.
		var initTime int64 = 2
		if failed {
			if initializing {
				t.Fatal("don't set initializing and failed both true")
			}
			initTime = 0
			resp := doer.queue(&BittrexDepthResponse{})
			resp.Header = http.Header{"Sequence": []string{"1"}}
		} else if initializing {
			initTime = 1
		}
		bittrex.wsSync.fail = time.Unix(1, 0)
		bittrex.wsSync.init = time.Unix(initTime, 0)
	}

	// This is the initializing run. This should just be a silent update.
	queueRefresh(true, false)
	bittrex.Refresh()
	select {
	case <-bittrex.channels.exchange:
		t.Fatalf("emitted an update when we shouldn't have")
	default:
	}

	// This would be a typical no-error Refresh. A full update is expected.
	queueRefresh(false, false)
	bittrex.Refresh()
	select {
	case <-bittrex.channels.exchange:
	default:
		t.Fatalf("no update emitted for no-error Refresh")
	}

	// Now simulate an error. Should still get an update, after http depth.
	queueRefresh(false, true)
	bittrex.Refresh()
	select {
	case <-bittrex.channels.exchange:
	default:
		t.Fatalf("no update emitted for no-error Refresh")
	}

	// Presumably, the next Refresh should work along normal paths.
	queueRefresh(false, false)
	bittrex.Refresh()
	select {
	case <-bittrex.channels.exchange:
	default:
		t.Fatalf("no update emitted for post-error Refresh")
	}
}

// Satisfies the websocketFeed interface
type dexWS struct {
	r        chan []byte
	readDone chan struct{}
	done     chan struct{}
	closed   uint32
	candleID chan uint64
}

func newDexWS() *dexWS {
	return &dexWS{
		r:        make(chan []byte),
		readDone: make(chan struct{}),
		done:     make(chan struct{}),
		candleID: make(chan uint64, 1),
	}
}

func (ws *dexWS) Done() chan struct{} {
	return ws.done
}

func (ws *dexWS) Read() ([]byte, error) {
	b, ok := <-ws.r
	if !ok {
		return nil, fmt.Errorf("closed (expected)")
	}
	return b, nil
}

func (ws *dexWS) Write(thing interface{}) error {
	msg, ok := thing.(*msgjson.Message)
	if !ok {
		panic("sent a non message")
	}
	switch msg.Route {
	case msgjson.CandlesRoute:
		ws.candleID <- msg.ID
	}
	return nil
}

func (ws *dexWS) Close() {
	if !atomic.CompareAndSwapUint32(&ws.closed, 0, 1) {
		return
	}
	close(ws.done)
	close(ws.r)
}

func (ws *dexWS) On() bool {
	select {
	case <-ws.done:
		return false
	default:
	}
	return true
}

func newTestDex() *DecredDEX {
	return &DecredDEX{
		CommonExchange: &CommonExchange{
			token: DexDotDecred,
			currentState: &ExchangeState{
				BaseState: BaseState{Price: 1},
			},
			channels: &BotChannels{
				exchange: make(chan *ExchangeUpdate, 2),
			},
			asks: make(wsOrders),
			buys: make(wsOrders),
		},
		ords:         make(map[string]*msgjson.BookOrderNote),
		candleCaches: make(map[uint64]*candleCache),
		reqs:         make(map[uint64]func(*msgjson.Message)),
	}
}

func TestDecredDEX(t *testing.T) {
	enableTestLog()

	dcr := newTestDex()
	ws := newDexWS()
	dcr.ws = ws

	mktID := "dcr_btc"

	var oidCounter int
	newOID := func() []byte {
		oidCounter++
		return []byte(strconv.Itoa(oidCounter))
	}
	var seqCounter uint64
	nextSeq := func() uint64 {
		seqCounter++
		return seqCounter
	}

	bookOrderNote := func(side uint8, qty, rate, seq uint64) *msgjson.BookOrderNote {
		return &msgjson.BookOrderNote{
			OrderNote: msgjson.OrderNote{
				Seq:      seq,
				MarketID: mktID,
				OrderID:  newOID(),
			},
			TradeNote: msgjson.TradeNote{
				Side:     side,
				Quantity: qty,
				Rate:     rate,
			},
		}
	}

	subID, _ := dcr.request(msgjson.OrderBookRoute, nil, dcr.handleSubResponse)

	seq := nextSeq()

	initialOrderBook, _ := msgjson.NewResponse(subID, &msgjson.OrderBook{
		MarketID: mktID,
		Seq:      nextSeq(),
		Orders: []*msgjson.BookOrderNote{
			bookOrderNote(msgjson.BuyOrderNum, 2, 11, seq), // oid 1
			bookOrderNote(msgjson.BuyOrderNum, 3, 12, seq), // oid 2
			bookOrderNote(msgjson.BuyOrderNum, 4, 13, seq), // oid 3
			// mid gap = 14
			bookOrderNote(msgjson.SellOrderNum, 4, 15, seq), // oid 4
			bookOrderNote(msgjson.SellOrderNum, 3, 16, seq), // oid 5
			bookOrderNote(msgjson.SellOrderNum, 2, 17, seq), // oid 6
		},
	}, nil)

	newBookOrderMsg := func(side uint8, qty, rate uint64) *msgjson.Message {
		msg, _ := msgjson.NewNotification(msgjson.BookOrderRoute, bookOrderNote(side, qty, rate, nextSeq()))
		return msg
	}

	newUnbookOrderMsg := func(oid []byte) *msgjson.Message {
		msg, _ := msgjson.NewNotification(msgjson.UnbookOrderRoute, &msgjson.UnbookOrderNote{
			Seq:      nextSeq(),
			MarketID: mktID,
			OrderID:  oid,
		})
		return msg
	}

	newUpdateRemainingMsg := func(oid []byte, remaining uint64) *msgjson.Message {
		msg, _ := msgjson.NewNotification(msgjson.UpdateRemainingRoute, &msgjson.UpdateRemainingNote{
			OrderNote: msgjson.OrderNote{
				Seq:      nextSeq(),
				MarketID: mktID,
				OrderID:  oid,
			},
			Remaining: remaining,
		})
		return msg
	}

	checkLengths := func(askLen, buyLen int) {
		t.Helper()
		time.Sleep(10 * time.Millisecond)
		dcr.orderMtx.RLock()
		defer dcr.orderMtx.RUnlock()
		if len(dcr.asks) != askLen || len(dcr.buys) != buyLen {
			t.Errorf("unexpected order book lengths (%d, %d). expected (%d, %d)",
				len(dcr.asks), len(dcr.buys), askLen, buyLen)
		}
	}

	dcr.wsProcessor = dcr.processWsMessage
	dcr.buys = make(wsOrders)
	dcr.asks = make(wsOrders)
	dcr.currentState = &ExchangeState{BaseState: BaseState{Price: 1}}
	dcr.startWebsocket()
	defer dcr.ws.Close()

	ws.r <- mustEncode(initialOrderBook)
	checkLengths(3, 3)

	ws.r <- mustEncode(newBookOrderMsg(msgjson.BuyOrderNum, 1, 10))
	checkLengths(3, 4)

	ws.r <- mustEncode(newUnbookOrderMsg([]byte(strconv.Itoa(oidCounter))))
	checkLengths(3, 3)

	ws.r <- mustEncode(newUpdateRemainingMsg([]byte("3"), 1))
	checkLengths(3, 3)
	depths := dcr.wsDepths()
	bestBuy := depths.Bids[0]
	if eightPtKey(bestBuy.Quantity) != 1 {
		t.Fatalf("wrong quantity after update_remaining: wanted 0.00000001, got %.8f", bestBuy.Quantity)
	}
	if eightPtKey(depths.MidGap()) != 14 {
		t.Fatalf("mid-gap wrong. wanted 0.00000014, got %.8f", depths.MidGap())
	}

	// Send through a config response to trigger the candles request.
	msg, _ := msgjson.NewResponse(2, &msgjson.ConfigResult{BinSizes: []string{"24h"}}, nil)
	dcr.handleConfigResponse(msg)

	// Get the ID of the request, and prepare a response.
	var candleID uint64
	select {
	case candleID = <-ws.candleID:
	case <-time.After(time.Second):
		t.Fatalf("no candles request received")
	}

	msg, _ = msgjson.NewResponse(candleID, &msgjson.WireCandles{
		StartStamps:  []uint64{1000},
		EndStamps:    []uint64{87400000},
		MatchVolumes: []uint64{1},
		QuoteVolumes: []uint64{2},
		HighRates:    []uint64{3},
		LowRates:     []uint64{4},
		StartRates:   []uint64{5},
		EndRates:     []uint64{6},
	}, nil)

	ws.r <- mustEncode(msg)

	// Just make sure that a cache was created and stored.
	tStart := time.Now()
	var numCaches int
	for {
		if time.Since(tStart) > 100*time.Millisecond {
			t.Fatalf("candles never received")
		}
		dcr.cacheMtx.RLock()
		numCaches = len(dcr.candleCaches)
		dcr.cacheMtx.RUnlock()
		if numCaches > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
}

func mustEncode(thing interface{}) []byte {
	b, err := json.Marshal(thing)
	if err != nil {
		panic(fmt.Sprintf("Marshal error encoding thing of type %T: %v", thing, err))
	}
	return b
}
