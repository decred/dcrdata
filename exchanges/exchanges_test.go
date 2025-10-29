// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/slog"
)

func enableTestLog() {
	if log == slog.Disabled {
		UseLogger(slog.NewBackend(os.Stdout).Logger("EXE"))
		log.SetLevel(slog.LevelTrace)
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
			currentState: map[CurrencyPair]*ExchangeState{
				CurrencyPairDCRBTC: {
					BaseState: BaseState{Price: 1},
				},
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
	dcr.currentState = map[CurrencyPair]*ExchangeState{
		CurrencyPairDCRBTC: {BaseState: BaseState{Price: 1}},
	}
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
