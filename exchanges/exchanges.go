// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"bytes"
	"compress/flate"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/msgjson"
	dexapi "decred.org/dcrdex/server/apidata"
	dexdb "decred.org/dcrdex/server/db"
	"github.com/carterjones/signalr"
	"github.com/carterjones/signalr/hubs"
	dcrrates "github.com/decred/dcrdata/exchanges/v3/ratesproto"
)

// Tokens. Used to identify the exchange.
const (
	Coinbase     = "coinbase"
	Coindesk     = "coindesk"
	Binance      = "binance"
	Bittrex      = "bittrex"
	DragonEx     = "dragonex"
	Huobi        = "huobi"
	Poloniex     = "poloniex"
	DexDotDecred = "dcrdex"
)

// A few candlestick bin sizes.
type candlestickKey string

const (
	fiveMinKey  candlestickKey = "5m"
	halfHourKey candlestickKey = "30m"
	hourKey     candlestickKey = "1h"
	dayKey      candlestickKey = "1d"
	monthKey    candlestickKey = "1mo"
)

var candlestickDurations = map[candlestickKey]time.Duration{
	fiveMinKey:  time.Minute * 5,
	halfHourKey: time.Minute * 30,
	hourKey:     time.Hour,
	dayKey:      time.Hour * 24,
	monthKey:    time.Hour * 24 * 30,
}

func (k candlestickKey) duration() time.Duration {
	d, found := candlestickDurations[k]
	if !found {
		log.Errorf("Candlestick duration parse error for key %s", string(k))
		return time.Duration(1)
	}
	return d
}

// URLs is a set of endpoints for an exchange's various datasets.
type URLs struct {
	Price        string
	Stats        string
	Depth        string
	Candlesticks map[candlestickKey]string
	Websocket    string
}

type requests struct {
	price        *http.Request
	stats        *http.Request
	depth        *http.Request
	candlesticks map[candlestickKey]*http.Request
}

func newRequests() requests {
	return requests{
		candlesticks: make(map[candlestickKey]*http.Request),
	}
}

// Prepare the URLs.
var (
	CoinbaseURLs = URLs{
		Price: "https://api.coinbase.com/v2/exchange-rates?currency=BTC",
	}
	CoindeskURLs = URLs{
		Price: "https://api.coindesk.com/v1/bpi/currentprice.json",
	}
	BinanceURLs = URLs{
		Price: "https://api.binance.com/api/v1/ticker/24hr?symbol=DCRBTC",
		// Binance returns a maximum of 1000 depth chart points. This seems like it
		// is the entire order book at least sometimes.
		Depth: "https://api.binance.com/api/v1/depth?symbol=DCRBTC&limit=1000",
		Candlesticks: map[candlestickKey]string{
			hourKey:  "https://api.binance.com/api/v1/klines?symbol=DCRBTC&interval=1h",
			dayKey:   "https://api.binance.com/api/v1/klines?symbol=DCRBTC&interval=1d",
			monthKey: "https://api.binance.com/api/v1/klines?symbol=DCRBTC&interval=1M",
		},
	}
	BittrexURLs = URLs{
		Price: "https://api.bittrex.com/v3/markets/dcr-btc/ticker",
		Stats: "https://api.bittrex.com/v3/markets/dcr-btc/summary",
		Depth: "https://api.bittrex.com/v3/markets/dcr-btc/orderbook?depth=500",
		Candlesticks: map[candlestickKey]string{
			hourKey: "https://api.bittrex.com/v3/markets/dcr-btc/candles/HOUR_1/recent",
			dayKey:  "https://api.bittrex.com/v3/markets/dcr-btc/candles/DAY_1/recent",
		},
		// Bittrex uses SignalR, which retrieves the actual websocket endpoint via
		// HTTP.
		Websocket: "socket.bittrex.com",
	}
	DragonExURLs = URLs{
		Price: "https://openapi.dragonex.io/api/v1/market/real/?symbol_id=1520101",
		// DragonEx depth chart has no parameters for configuring amount of data.
		Depth: "https://openapi.dragonex.io/api/v1/market/%s/?symbol_id=1520101", // Separate buy and sell endpoints
		Candlesticks: map[candlestickKey]string{
			hourKey: "https://openapi.dragonex.io/api/v1/market/kline/?symbol_id=1520101&count=100&kline_type=5",
			dayKey:  "https://openapi.dragonex.io/api/v1/market/kline/?symbol_id=1520101&count=100&kline_type=6",
		},
	}
	HuobiURLs = URLs{
		Price: "https://api.huobi.pro/market/detail/merged?symbol=dcrbtc",
		// Huobi's only depth parameter defines bin size, 'step0' seems to mean bin
		// width of zero.
		Depth: "https://api.huobi.pro/market/depth?symbol=dcrbtc&type=step0",
		Candlesticks: map[candlestickKey]string{
			hourKey:  "https://api.huobi.pro/market/history/kline?symbol=dcrbtc&period=60min&size=2000",
			dayKey:   "https://api.huobi.pro/market/history/kline?symbol=dcrbtc&period=1day&size=2000",
			monthKey: "https://api.huobi.pro/market/history/kline?symbol=dcrbtc&period=1mon&size=2000",
		},
	}
	PoloniexURLs = URLs{
		Price: "https://poloniex.com/public?command=returnTicker",
		// Maximum value of 100 for depth parameter.
		Depth: "https://poloniex.com/public?command=returnOrderBook&currencyPair=BTC_DCR&depth=100",
		Candlesticks: map[candlestickKey]string{
			halfHourKey: "https://poloniex.com/public?command=returnChartData&currencyPair=BTC_DCR&period=1800&start=0&resolution=auto",
			dayKey:      "https://poloniex.com/public?command=returnChartData&currencyPair=BTC_DCR&period=86400&start=0&resolution=auto",
		},
		Websocket: "wss://api2.poloniex.com",
	}
)

// BtcIndices maps tokens to constructors for BTC-fiat exchanges.
var BtcIndices = map[string]func(*http.Client, *BotChannels) (Exchange, error){
	Coinbase: NewCoinbase,
	Coindesk: NewCoindesk,
}

// DcrExchanges maps tokens to constructors for DCR-BTC exchanges.
var DcrExchanges = map[string]func(*http.Client, *BotChannels) (Exchange, error){
	Binance:  NewBinance,
	Bittrex:  NewBittrex,
	DragonEx: NewDragonEx,
	Huobi:    NewHuobi,
	Poloniex: NewPoloniex,
	DexDotDecred: NewDecredDEXConstructor(&DEXConfig{
		Token:    DexDotDecred,
		Host:     "dex.decred.org:7232",
		Cert:     core.CertStore["dex.decred.org:7232"],
		CertHost: "dex.decred.org",
	}),
}

// IsBtcIndex checks whether the given token is a known Bitcoin index, as
// opposed to a Decred-to-Bitcoin Exchange.
func IsBtcIndex(token string) bool {
	_, ok := BtcIndices[token]
	return ok
}

// IsDcrExchange checks whether the given token is a known Decred-BTC exchange.
func IsDcrExchange(token string) bool {
	_, ok := DcrExchanges[token]
	return ok
}

// Tokens is a new slice of available exchange tokens.
func Tokens() []string {
	tokens := make([]string, 0, len(BtcIndices)+len(DcrExchanges))
	var token string
	for token = range BtcIndices {
		tokens = append(tokens, token)
	}
	for token = range DcrExchanges {
		tokens = append(tokens, token)
	}
	return tokens
}

// Most exchanges bin price values on a float precision of 8 decimal points.
// eightPtKey reliably converts the float to an int64 that is unique for a price
// bin.
func eightPtKey(rate float64) int64 {
	return int64(math.Round(rate * 1e8))
}

// Set a hard limit of an hour old for order book data. This could also be
// based on some multiple of ExchangeBotConfig.requestExpiry, but should have
// some reasonable limit anyway.
const depthDataExpiration = time.Hour

// DepthPoint is a single point in a set of depth chart data.
type DepthPoint struct {
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
}

// DepthData is an exchanges order book for use in a depth chart.
type DepthData struct {
	Time int64        `json:"time"`
	Bids []DepthPoint `json:"bids"`
	Asks []DepthPoint `json:"asks"`
}

// IsFresh will be true if the data is older than depthDataExpiration.
func (depth *DepthData) IsFresh() bool {
	return time.Duration(time.Now().Unix()-depth.Time)*
		time.Second < depthDataExpiration
}

// MidGap returns the mid-gap price based on the best bid and ask. If the book
// is empty, the value 1.0 is returned.
func (depth *DepthData) MidGap() float64 {
	if len(depth.Bids) == 0 {
		if len(depth.Asks) == 0 {
			return 1
		}
		return depth.Asks[0].Price
	} else if len(depth.Asks) == 0 {
		return depth.Bids[0].Price
	}
	return (depth.Bids[0].Price + depth.Asks[0].Price) / 2
}

// Candlestick is the record of price change over some bin width of time.
type Candlestick struct {
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Open   float64   `json:"open"`
	Close  float64   `json:"close"`
	Volume float64   `json:"volume"`
	Start  time.Time `json:"start"`
}

// Candlesticks is a slice of CandleStick.
type Candlesticks []Candlestick

// returns the start time of the last Candlestick, else the zero time,
func (sticks Candlesticks) time() time.Time {
	if len(sticks) > 0 {
		return sticks[len(sticks)-1].Start
	}
	return time.Time{}
}

// Checks whether the candlestick data for the given bin size is up-to-date.
func (sticks Candlesticks) needsUpdate(bin candlestickKey) bool {
	if len(sticks) == 0 {
		return true
	}
	lastStick := sticks[len(sticks)-1]
	return time.Now().After(lastStick.Start.Add(bin.duration() * 2))
}

// BaseState are the non-iterable fields of the ExchangeState, which embeds
// BaseState.
type BaseState struct {
	Price float64 `json:"price"`
	// BaseVolume is poorly named. This is the volume in terms of (usually) BTC,
	// not the base asset of any particular market.
	BaseVolume float64 `json:"base_volume,omitempty"`
	Volume     float64 `json:"volume,omitempty"`
	Change     float64 `json:"change,omitempty"`
	Stamp      int64   `json:"timestamp,omitempty"`
}

// ExchangeState is the simple template for a price. The only member that is
// guaranteed is a price. For Decred exchanges, the volumes will also be
// populated.
type ExchangeState struct {
	BaseState
	Depth        *DepthData                      `json:"depth,omitempty"`
	Candlesticks map[candlestickKey]Candlesticks `json:"candlesticks,omitempty"`
}

/*
func (state *ExchangeState) copy() *ExchangeState {
	newState := &ExchangeState{
		Price:      state.Price,
		BaseVolume: state.BaseVolume,
		Volume:     state.Volume,
		Change:     state.Change,
		Stamp:      state.Stamp,
		Depth:      state.Depth,
	}
	if state.Candlesticks != nil {
		newState.Candlesticks = make(map[candlestickKey]Candlesticks)
		for bin, sticks := range state.Candlesticks {
			newState.Candlesticks[bin] = sticks
		}
	}
	return newState
}
*/

// Grab any candlesticks from the top that are not in the receiver. Candlesticks
// are historical data, so never need to be discarded.
func (state *ExchangeState) stealSticks(top *ExchangeState) {
	if len(top.Candlesticks) == 0 {
		return
	}
	if state.Candlesticks == nil {
		state.Candlesticks = make(map[candlestickKey]Candlesticks)
	}
	for bin := range top.Candlesticks {
		_, have := state.Candlesticks[bin]
		if !have {
			state.Candlesticks[bin] = top.Candlesticks[bin]
		}
	}
}

// Parse an ExchangeState from a protocol buffer message.
func exchangeStateFromProto(proto *dcrrates.ExchangeRateUpdate) *ExchangeState {
	state := &ExchangeState{
		BaseState: BaseState{
			Price:      proto.GetPrice(),
			BaseVolume: proto.GetBaseVolume(),
			Volume:     proto.GetVolume(),
			Change:     proto.GetChange(),
			Stamp:      proto.GetStamp(),
		},
	}

	updateDepth := proto.GetDepth()
	if updateDepth != nil {
		depth := &DepthData{
			Time: updateDepth.Time,
			Bids: make([]DepthPoint, 0, len(updateDepth.Bids)),
			Asks: make([]DepthPoint, 0, len(updateDepth.Asks)),
		}
		for _, bid := range updateDepth.Bids {
			depth.Bids = append(depth.Bids, DepthPoint{
				Quantity: bid.Quantity,
				Price:    bid.Price,
			})
		}
		for _, ask := range updateDepth.Asks {
			depth.Asks = append(depth.Asks, DepthPoint{
				Quantity: ask.Quantity,
				Price:    ask.Price,
			})
		}
		state.Depth = depth
	}

	if proto.Candlesticks != nil {
		stickMap := make(map[candlestickKey]Candlesticks)
		for _, candlesticks := range proto.Candlesticks {
			sticks := make(Candlesticks, 0, len(candlesticks.Sticks))
			for _, stick := range candlesticks.Sticks {
				sticks = append(sticks, Candlestick{
					High:   stick.High,
					Low:    stick.Low,
					Open:   stick.Open,
					Close:  stick.Close,
					Volume: stick.Volume,
					Start:  time.Unix(stick.Start, 0),
				})
			}
			stickMap[candlestickKey(candlesticks.Bin)] = sticks
		}
		state.Candlesticks = stickMap
	}
	return state
}

// HasCandlesticks checks for data in the candlesticks map.
func (state *ExchangeState) HasCandlesticks() bool {
	return len(state.Candlesticks) > 0
}

// HasDepth is true if the there is data in the depth field.
func (state *ExchangeState) HasDepth() bool {
	return state.Depth != nil
}

// StickList is a semicolon-delimited list of available binSize.
func (state *ExchangeState) StickList() string {
	sticks := make([]string, 0, len(state.Candlesticks))
	for bin := range state.Candlesticks {
		sticks = append(sticks, string(bin))
	}
	return strings.Join(sticks, ";")
}

// ExchangeUpdate packages the ExchangeState for the update channel.
type ExchangeUpdate struct {
	Token string
	State *ExchangeState
}

// Exchange is the interface that ExchangeBot understands. Most of the methods
// are implemented by CommonExchange, but Refresh is implemented in the
// individual exchange types.
type Exchange interface {
	LastUpdate() time.Time
	LastFail() time.Time
	LastTry() time.Time
	Refresh()
	IsFailed() bool
	Token() string
	Hurry(time.Duration)
	Update(*ExchangeState)
	SilentUpdate(*ExchangeState) // skip passing update to the update channel
	UpdateIndices(FiatIndices)
}

// Doer is an interface for a *http.Client to allow testing of Refresh paths.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// CommonExchange is embedded in all of the exchange types and handles some
// state tracking and token handling for ExchangeBot communications. The
// http.Request must be created individually for each exchange.
type CommonExchange struct {
	mtx          sync.RWMutex
	token        string
	URL          string
	currentState *ExchangeState
	client       Doer
	lastUpdate   time.Time
	lastFail     time.Time
	lastRequest  time.Time
	requests     requests
	channels     *BotChannels
	wsMtx        sync.RWMutex
	ws           websocketFeed
	sr           signalrClient
	wsSync       struct {
		err      error
		errCount int
		init     time.Time
		update   time.Time
		fail     time.Time
	}
	// wsProcessor is only used for websockets, not SignalR. For SignalR, the
	// callback function is passed as part of the signalrConfig.
	wsProcessor WebsocketProcessor
	// Exchanges that use websockets or signalr to maintain a live orderbook can
	// use the buy and sell slices to leverage some useful methods on
	// CommonExchange.
	orderMtx sync.RWMutex
	buys     wsOrders
	asks     wsOrders
}

// LastUpdate gets a time.Time of the last successful exchange update.
func (xc *CommonExchange) LastUpdate() time.Time {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.lastUpdate
}

// Hurry can be used to subtract some amount of time from the lastUpdate
// and lastFail, and can be used to de-sync the exchange updates.
func (xc *CommonExchange) Hurry(d time.Duration) {
	xc.mtx.Lock()
	defer xc.mtx.Unlock()
	xc.lastRequest = xc.lastRequest.Add(-d)
}

// LastFail gets the last time.Time of a failed exchange update.
func (xc *CommonExchange) LastFail() time.Time {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.lastFail
}

// IsFailed will be true if xc.lastFail > xc.lastUpdate.
func (xc *CommonExchange) IsFailed() bool {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.lastFail.After(xc.lastUpdate)
}

// LogRequest sets the lastRequest time.Time.
func (xc *CommonExchange) LogRequest() {
	xc.mtx.Lock()
	defer xc.mtx.Unlock()
	xc.lastRequest = time.Now()
}

// LastTry is the more recent of lastFail and LastUpdate.
func (xc *CommonExchange) LastTry() time.Time {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.lastRequest
}

// Token is the string associated with the exchange's token.
func (xc *CommonExchange) Token() string {
	return xc.token
}

// setLastFail sets the last failure time.
func (xc *CommonExchange) setLastFail(t time.Time) {
	xc.mtx.Lock()
	defer xc.mtx.Unlock()
	xc.lastFail = t
}

// Log the error along with the token and an additional passed identifier.
func (xc *CommonExchange) fail(msg string, err error) {
	log.Errorf("%s: %s: %v", xc.token, msg, err)
	xc.setLastFail(time.Now())
}

// Update sends an updated ExchangeState to the ExchangeBot.
func (xc *CommonExchange) Update(state *ExchangeState) {
	xc.update(state, true)
}

// SilentUpdate stores the update for internal use, but does not signal an
// update to the ExchangeBot.
func (xc *CommonExchange) SilentUpdate(state *ExchangeState) {
	xc.update(state, false)
}

func (xc *CommonExchange) update(state *ExchangeState, send bool) {
	xc.mtx.Lock()
	defer xc.mtx.Unlock()
	xc.lastUpdate = time.Now()
	state.stealSticks(xc.currentState)
	xc.currentState = state
	if !send {
		return
	}
	xc.channels.exchange <- &ExchangeUpdate{
		Token: xc.token,
		State: state,
	}
}

// UpdateIndices sends a bitcoin index update to the ExchangeBot.
func (xc *CommonExchange) UpdateIndices(indices FiatIndices) {
	xc.mtx.Lock()
	defer xc.mtx.Unlock()
	xc.lastUpdate = time.Now()
	xc.channels.index <- &IndexUpdate{
		Token:   xc.token,
		Indices: indices,
	}
}

// Send the exchange request and decode the response.
func (xc *CommonExchange) fetch(request *http.Request, response interface{}) (err error) {
	resp, err := xc.client.Do(request)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("Request failed: %v", err))
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(response)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("Failed to decode json from %s: %v", request.URL.String(), err))
	}
	return
}

// A thread-safe getter for the last known ExchangeState.
func (xc *CommonExchange) state() *ExchangeState {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.currentState
}

// WebsocketProcessor is a callback for new websocket messages from the server.
type WebsocketProcessor func([]byte)

// Only the fields are protected for these. (websocketFeed).Write has
// concurrency control.
func (xc *CommonExchange) websocket() (websocketFeed, WebsocketProcessor) {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.ws, xc.wsProcessor
}

// Grab the SignalR client, which is nil for most exchanges.
func (xc *CommonExchange) signalr() signalrClient {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.sr
}

// Creates a websocket connection and starts a listen loop. Closes any existing
// connections for this exchange.
func (xc *CommonExchange) connectWebsocket(processor WebsocketProcessor, cfg *socketConfig) error {
	ws, err := newSocketConnection(cfg)
	if err != nil {
		return err
	}

	xc.wsMtx.Lock()
	// Ensure that any previous websocket is closed.
	if xc.ws != nil {
		xc.ws.Close()
	}
	xc.wsProcessor = processor
	xc.ws = ws
	xc.wsMtx.Unlock()

	xc.startWebsocket()
	return nil
}

// The listen loop for a websocket connection.
func (xc *CommonExchange) startWebsocket() {
	ws, processor := xc.websocket()
	go func() {
		for {
			message, err := ws.Read()
			if err != nil {
				xc.setWsFail(err)
				return
			}
			processor(message)
		}
	}()
}

// wsSend sends a message on a standard websocket connection. For SignalR
// connections, use xc.sr.Send directly.
func (xc *CommonExchange) wsSend(msg interface{}) error {
	ws, _ := xc.websocket()
	return ws.Write(msg)
}

// Checks whether the websocketFeed Done channel is closed.
func (xc *CommonExchange) wsListening() bool {
	xc.wsMtx.RLock()
	defer xc.wsMtx.RUnlock()
	return xc.wsSync.init.After(xc.wsSync.fail)
}

// Log the error and time, and increment the error counter.
func (xc *CommonExchange) setWsFail(err error) {
	log.Errorf("%s websocket error: %v", xc.token, err)
	xc.wsMtx.Lock()
	defer xc.wsMtx.Unlock()
	if xc.ws != nil {
		xc.ws.Close()
		// Clear the field to prevent double Close'ing.
		xc.ws = nil
	}
	if xc.sr != nil {
		// The carterjones/signalr can hang on Close. The goroutine is a stopgap while
		// we migrate to a new signalr client.
		// https://github.com/decred/dcrdata/issues/1818
		go xc.sr.Close()
		// Clear the field to prevent double Close'ing. signalr will hang on
		// second call.
		xc.sr = nil
	}
	xc.wsSync.err = err
	xc.wsSync.errCount++
	xc.wsSync.fail = time.Now()
}

func (xc *CommonExchange) wsFailTime() time.Time {
	xc.wsMtx.RLock()
	defer xc.wsMtx.RUnlock()
	return xc.wsSync.fail
}

// Set the init flag. The websocket is considered failed if the failed flag
// is later than the init flag.
func (xc *CommonExchange) wsInitialized() {
	xc.wsMtx.Lock()
	defer xc.wsMtx.Unlock()
	xc.wsSync.init = time.Now()
	xc.wsSync.update = xc.wsSync.init
}

// Set the updated flag. Set the error count to 0 when the client has
// successfully updated.
func (xc *CommonExchange) wsUpdated() {
	xc.wsMtx.Lock()
	defer xc.wsMtx.Unlock()
	xc.wsSync.update = time.Now()
	xc.wsSync.errCount = 0
}

func (xc *CommonExchange) wsLastUpdate() time.Time {
	xc.wsMtx.RLock()
	defer xc.wsMtx.RUnlock()
	return xc.wsSync.update
}

// Checks whether the websocket is in a failed state.
func (xc *CommonExchange) wsFailed() bool {
	xc.wsMtx.RLock()
	defer xc.wsMtx.RUnlock()
	return xc.wsSync.fail.After(xc.wsSync.init)
}

// The count of errors logged since the last success-triggered reset.
func (xc *CommonExchange) wsErrorCount() int {
	xc.wsMtx.RLock()
	defer xc.wsMtx.RUnlock()
	return xc.wsSync.errCount
}

// For exchanges that have SignalR-wrapped websockets, connectSignalr will be
// used instead of connectWebsocket.
func (xc *CommonExchange) connectSignalr(cfg *signalrConfig) (err error) {
	if cfg.errHandler == nil {
		cfg.errHandler = func(err error) {
			xc.wsMtx.Lock()
			xc.sr = nil
			xc.wsMtx.Unlock()
			xc.setWsFail(err)
		}
	}
	xc.wsMtx.Lock()
	defer xc.wsMtx.Unlock()
	if xc.sr != nil {
		xc.sr.Close()
	}
	xc.sr, err = newSignalrConnection(cfg)
	return
}

// An intermediate order representation used to track an orderbook over a
// websocket connection.
type wsOrder struct {
	price  float64
	volume float64
}
type wsOrders map[int64]*wsOrder

// Get the *wsOrder at the specified rateKey. Adds one first, if necessary.
func (ords wsOrders) order(rateKey int64, rate float64) *wsOrder {
	ord, ok := ords[rateKey]
	if ok {
		return ord
	}
	ord = &wsOrder{price: rate}
	ords[rateKey] = ord
	return ord
}

// Pull out the int64 bin keys from the map.
func wsOrderBinKeys(book wsOrders) []int64 {
	keys := make([]int64, 0, len(book))
	for k := range book {
		keys = append(keys, k)
	}
	return keys
}

// Convert the intermediate websocket orderbook to a DepthData. This function
// should be called under at least an orderMtx.RLock.
func (xc *CommonExchange) wsDepthSnapshot() *DepthData {
	askKeys := wsOrderBinKeys(xc.asks)
	sort.Slice(askKeys, func(i, j int) bool {
		return askKeys[i] < askKeys[j]
	})
	buyKeys := wsOrderBinKeys(xc.buys)
	sort.Slice(buyKeys, func(i, j int) bool {
		return buyKeys[i] > buyKeys[j]
	})
	a := make([]DepthPoint, 0, len(askKeys))
	for _, bin := range askKeys {
		pt := xc.asks[bin]
		a = append(a, DepthPoint{
			Quantity: pt.volume,
			Price:    pt.price,
		})
	}
	b := make([]DepthPoint, 0, len(buyKeys))
	for _, bin := range buyKeys {
		pt := xc.buys[bin]
		b = append(b, DepthPoint{
			Quantity: pt.volume,
			Price:    pt.price,
		})
	}
	return &DepthData{
		Time: time.Now().Unix(),
		Asks: a,
		Bids: b,
	}
}

// Grab a wsDepthSnapshot under RLock.
func (xc *CommonExchange) wsDepths() *DepthData {
	xc.orderMtx.RLock()
	defer xc.orderMtx.RUnlock()
	return xc.wsDepthSnapshot()
}

// For exchanges that have a websocket-synced orderbook, wsDepthStatus will
// return the DepthData. tryHttp will be true if the websocket is in a
// questionable state. The value of initializing will be true if this is the
// initial connection.
func (xc *CommonExchange) wsDepthStatus(connector func()) (tryHttp, initializing bool, depth *DepthData) {
	if xc.wsListening() {
		depth = xc.wsDepths()
		return
	}
	if !xc.wsFailed() {
		// Connection has not been initialized. Trigger a silent update, since an
		// update will be triggered on initial websocket message, which contains
		// the full orderbook.
		initializing = true
		log.Tracef("Initializing websocket connection for %s", xc.token)
		connector()
		return
	}
	log.Tracef("using http fallback for %s orderbook data", xc.token)
	tryHttp = true
	errCount := xc.wsErrorCount()
	var delay time.Duration
	// wsDepthStatus is only called every DataExpiry, so a delay of zero is ok
	// until there are a few consecutive errors.
	switch {
	case errCount < 5:
	case errCount < 20:
		delay = 10 * time.Minute
	default:
		delay = time.Minute * 60
	}
	okToTry := xc.wsFailTime().Add(delay)
	if time.Now().After(okToTry) {
		// Try to connect, but don't wait for the response. Grab the order
		// book over HTTP anyway.
		connector()
	} else {
		log.Errorf("%s websocket disabled. Too many errors. Will attempt to reconnect after %.1f minutes", xc.token, time.Until(okToTry).Minutes())
	}
	return

}

// Used to initialize the embedding exchanges.
func newCommonExchange(token string, client *http.Client,
	reqs requests, channels *BotChannels) *CommonExchange {
	var tZero time.Time
	return &CommonExchange{
		token:        token,
		client:       client,
		channels:     channels,
		currentState: new(ExchangeState),
		lastUpdate:   tZero,
		lastFail:     tZero,
		lastRequest:  tZero,
		requests:     reqs,
		asks:         make(wsOrders),
		buys:         make(wsOrders),
	}
}

// CoinbaseExchange provides tons of bitcoin-fiat exchange pairs.
type CoinbaseExchange struct {
	*CommonExchange
}

// NewCoinbase constructs a CoinbaseExchange.
func NewCoinbase(client *http.Client, channels *BotChannels) (coinbase Exchange, err error) {
	reqs := newRequests()
	reqs.price, err = http.NewRequest(http.MethodGet, CoinbaseURLs.Price, nil)
	if err != nil {
		return
	}
	coinbase = &CoinbaseExchange{
		CommonExchange: newCommonExchange(Coinbase, client, reqs, channels),
	}
	return
}

// CoinbaseResponse models the JSON data returned from the Coinbase API.
type CoinbaseResponse struct {
	Data CoinbaseResponseData `json:"data"`
}

// CoinbaseResponseData models the "data" field of the Coinbase API response.
type CoinbaseResponseData struct {
	Currency string            `json:"currency"`
	Rates    map[string]string `json:"rates"`
}

// Refresh retrieves and parses API data from Coinbase.
func (coinbase *CoinbaseExchange) Refresh() {
	coinbase.LogRequest()
	response := new(CoinbaseResponse)
	err := coinbase.fetch(coinbase.requests.price, response)
	if err != nil {
		coinbase.fail("Fetch", err)
		return
	}

	indices := make(FiatIndices)
	for code, floatStr := range response.Data.Rates {
		price, err := strconv.ParseFloat(floatStr, 64)
		if err != nil {
			coinbase.fail(fmt.Sprintf("Failed to parse float for index %s. Given %s", code, floatStr), err)
			continue
		}
		indices[code] = price
	}
	coinbase.UpdateIndices(indices)
}

// CoindeskExchange provides Bitcoin indices for USD, GBP, and EUR by default.
// Others are available, but custom requests would need to be implemented.
type CoindeskExchange struct {
	*CommonExchange
}

// NewCoindesk constructs a CoindeskExchange.
func NewCoindesk(client *http.Client, channels *BotChannels) (coindesk Exchange, err error) {
	reqs := newRequests()
	reqs.price, err = http.NewRequest(http.MethodGet, CoindeskURLs.Price, nil)
	if err != nil {
		return
	}
	coindesk = &CoindeskExchange{
		CommonExchange: newCommonExchange(Coindesk, client, reqs, channels),
	}
	return
}

// CoindeskResponse models the JSON data returned from the Coindesk API.
type CoindeskResponse struct {
	Time       CoindeskResponseTime           `json:"time"`
	Disclaimer string                         `json:"disclaimer"`
	ChartName  string                         `json:"chartName"`
	Bpi        map[string]CoindeskResponseBpi `json:"bpi"`
}

// CoindeskResponseTime models the "time" field of the Coindesk API response.
type CoindeskResponseTime struct {
	Updated    string    `json:"updated"`
	UpdatedIso time.Time `json:"updatedISO"`
	Updateduk  string    `json:"updateduk"`
}

// CoindeskResponseBpi models the "bpi" field of the Coindesk API response.
type CoindeskResponseBpi struct {
	Code        string  `json:"code"`
	Symbol      string  `json:"symbol"`
	Rate        string  `json:"rate"`
	Description string  `json:"description"`
	RateFloat   float64 `json:"rate_float"`
}

// Refresh retrieves and parses API data from Coindesk.
func (coindesk *CoindeskExchange) Refresh() {
	coindesk.LogRequest()
	response := new(CoindeskResponse)
	err := coindesk.fetch(coindesk.requests.price, response)
	if err != nil {
		coindesk.fail("Fetch", err)
		return
	}

	indices := make(FiatIndices)
	for code, bpi := range response.Bpi {
		indices[code] = bpi.RateFloat
	}
	coindesk.UpdateIndices(indices)
}

// BinanceExchange is a high-volume and well-respected crypto exchange.
type BinanceExchange struct {
	*CommonExchange
}

// NewBinance constructs a BinanceExchange.
func NewBinance(client *http.Client, channels *BotChannels) (binance Exchange, err error) {
	reqs := newRequests()
	reqs.price, err = http.NewRequest(http.MethodGet, BinanceURLs.Price, nil)
	if err != nil {
		return
	}

	reqs.depth, err = http.NewRequest(http.MethodGet, BinanceURLs.Depth, nil)
	if err != nil {
		return
	}

	for dur, url := range BinanceURLs.Candlesticks {
		reqs.candlesticks[dur], err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return
		}
	}
	binance = &BinanceExchange{
		CommonExchange: newCommonExchange(Binance, client, reqs, channels),
	}
	return
}

// BinancePriceResponse models the JSON price data returned from the Binance API.
type BinancePriceResponse struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	WeightedAvgPrice   string `json:"weightedAvgPrice"`
	PrevClosePrice     string `json:"prevClosePrice"`
	LastPrice          string `json:"lastPrice"`
	LastQty            string `json:"lastQty"`
	BidPrice           string `json:"bidPrice"`
	BidQty             string `json:"bidQty"`
	AskPrice           string `json:"askPrice"`
	AskQty             string `json:"askQty"`
	OpenPrice          string `json:"openPrice"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
	OpenTime           int64  `json:"openTime"`
	CloseTime          int64  `json:"closeTime"`
	FirstID            int64  `json:"firstId"`
	LastID             int64  `json:"lastId"`
	Count              int64  `json:"count"`
}

// BinanceCandlestickResponse models candlestick data returned from the Binance
// API. Binance has a response with mixed-type arrays, so type-checking is
// appropriate. Sample response is
// [
//   [
//     1499040000000,      // Open time
//     "0.01634790",       // Open
//     "0.80000000",       // High
//     "0.01575800",       // Low
//     "0.01577100",       // Close
//     "148976.11427815",  // Volume
//     ...
//   ]
// ]
type BinanceCandlestickResponse [][]interface{}

func badBinanceStickElement(key string, element interface{}) Candlesticks {
	log.Errorf("Unable to decode %s from Binance candlestick: %T: %v", key, element, element)
	return Candlesticks{}
}

func (r BinanceCandlestickResponse) translate() Candlesticks {
	sticks := make(Candlesticks, 0, len(r))
	for _, rawStick := range r {
		if len(rawStick) < 6 {
			log.Error("Unable to decode Binance candlestick response. Not enough elements.")
			return Candlesticks{}
		}
		unixMsFlt, ok := rawStick[0].(float64)
		if !ok {
			return badBinanceStickElement("start time", rawStick[0])
		}
		startTime := time.Unix(int64(unixMsFlt/1e3), 0)

		openStr, ok := rawStick[1].(string)
		if !ok {
			return badBinanceStickElement("open", rawStick[1])
		}
		open, err := strconv.ParseFloat(openStr, 64)
		if err != nil {
			return badBinanceStickElement("open float", err)
		}

		highStr, ok := rawStick[2].(string)
		if !ok {
			return badBinanceStickElement("high", rawStick[2])
		}
		high, err := strconv.ParseFloat(highStr, 64)
		if err != nil {
			return badBinanceStickElement("high float", err)
		}

		lowStr, ok := rawStick[3].(string)
		if !ok {
			return badBinanceStickElement("low", rawStick[3])
		}
		low, err := strconv.ParseFloat(lowStr, 64)
		if err != nil {
			return badBinanceStickElement("low float", err)
		}

		closeStr, ok := rawStick[4].(string)
		if !ok {
			return badBinanceStickElement("close", rawStick[4])
		}
		close, err := strconv.ParseFloat(closeStr, 64)
		if err != nil {
			return badBinanceStickElement("close float", err)
		}

		volumeStr, ok := rawStick[5].(string)
		if !ok {
			return badBinanceStickElement("volume", rawStick[5])
		}
		volume, err := strconv.ParseFloat(volumeStr, 64)
		if err != nil {
			return badBinanceStickElement("volume float", err)
		}

		sticks = append(sticks, Candlestick{
			High:   high,
			Low:    low,
			Open:   open,
			Close:  close,
			Volume: volume,
			Start:  startTime,
		})
	}
	return sticks
}

// BinanceDepthResponse models the response for Binance depth chart data.
type BinanceDepthResponse struct {
	UpdateID int64
	Bids     [][2]string
	Asks     [][2]string
}

func parseBinanceDepthPoints(pts [][2]string) ([]DepthPoint, error) {
	outPts := make([]DepthPoint, 0, len(pts))
	for _, pt := range pts {
		price, err := strconv.ParseFloat(pt[0], 64)
		if err != nil {
			return outPts, fmt.Errorf("Unable to parse Binance depth point price: %v", err)
		}

		quantity, err := strconv.ParseFloat(pt[1], 64)
		if err != nil {
			return outPts, fmt.Errorf("Unable to parse Binance depth point quantity: %v", err)
		}

		outPts = append(outPts, DepthPoint{
			Quantity: quantity,
			Price:    price,
		})
	}
	return outPts, nil
}

func (r *BinanceDepthResponse) translate() *DepthData {
	if r == nil {
		return nil
	}
	depth := new(DepthData)
	depth.Time = time.Now().Unix()
	var err error
	depth.Asks, err = parseBinanceDepthPoints(r.Asks)
	if err != nil {
		log.Errorf("%v", err)
		return nil
	}
	depth.Bids, err = parseBinanceDepthPoints(r.Bids)
	if err != nil {
		log.Errorf("%v", err)
		return nil
	}
	return depth
}

// Refresh retrieves and parses API data from Binance.
func (binance *BinanceExchange) Refresh() {
	binance.LogRequest()
	priceResponse := new(BinancePriceResponse)
	err := binance.fetch(binance.requests.price, priceResponse)
	if err != nil {
		binance.fail("Fetch price", err)
		return
	}
	price, err := strconv.ParseFloat(priceResponse.LastPrice, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("Failed to parse float from LastPrice=%s", priceResponse.LastPrice), err)
		return
	}
	baseVolume, err := strconv.ParseFloat(priceResponse.QuoteVolume, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("Failed to parse float from QuoteVolume=%s", priceResponse.QuoteVolume), err)
		return
	}

	dcrVolume, err := strconv.ParseFloat(priceResponse.Volume, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("Failed to parse float from Volume=%s", priceResponse.Volume), err)
		return
	}
	priceChange, err := strconv.ParseFloat(priceResponse.PriceChange, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("Failed to parse float from PriceChange=%s", priceResponse.PriceChange), err)
		return
	}

	// Get the depth chart
	depthResponse := new(BinanceDepthResponse)
	err = binance.fetch(binance.requests.depth, depthResponse)
	if err != nil {
		log.Errorf("Error retrieving depth chart data from Binance: %v", err)
	}
	depth := depthResponse.translate()

	// Grab the current state to check if candlesticks need updating
	state := binance.state()

	candlesticks := map[candlestickKey]Candlesticks{}
	for bin, req := range binance.requests.candlesticks {
		oldSticks, found := state.Candlesticks[bin]
		if !found || oldSticks.needsUpdate(bin) {
			log.Tracef("Signalling candlestick update for %s, bin size %s", binance.token, bin)
			response := new(BinanceCandlestickResponse)
			err := binance.fetch(req, response)
			if err != nil {
				log.Errorf("Error retrieving candlestick data from binance for bin size %s: %v", string(bin), err)
				continue
			}
			sticks := response.translate()

			if !found || sticks.time().After(oldSticks.time()) {
				candlesticks[bin] = sticks
			}
		}
	}

	binance.Update(&ExchangeState{
		BaseState: BaseState{
			Price:      price,
			BaseVolume: baseVolume,
			Volume:     dcrVolume,
			Change:     priceChange,
			Stamp:      priceResponse.CloseTime / 1000,
		},
		Candlesticks: candlesticks,
		Depth:        depth,
	})
}

// BittrexExchange is an unregulated U.S. crypto exchange with good volume.
type BittrexExchange struct {
	*CommonExchange
	MarketName string
	queue      []*BittrexOrderbookUpdate
	orderSeq   uint64
}

// NewBittrex constructs a BittrexExchange.
func NewBittrex(client *http.Client, channels *BotChannels) (bittrex Exchange, err error) {
	reqs := newRequests()
	reqs.price, err = http.NewRequest(http.MethodGet, BittrexURLs.Price, nil)
	if err != nil {
		return
	}
	reqs.price.Header.Add("Content-Type", "application/json")

	reqs.stats, err = http.NewRequest(http.MethodGet, BittrexURLs.Stats, nil)
	if err != nil {
		return
	}
	reqs.stats.Header.Add("Content-Type", "application/json")

	reqs.depth, err = http.NewRequest(http.MethodGet, BittrexURLs.Depth, nil)
	if err != nil {
		return
	}

	for dur, url := range BittrexURLs.Candlesticks {
		reqs.candlesticks[dur], err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return
		}
		reqs.candlesticks[dur].Header.Add("Content-Type", "application/json")
	}

	b := &BittrexExchange{
		CommonExchange: newCommonExchange(Bittrex, client, reqs, channels),
		MarketName:     "BTC-DCR",
		queue:          make([]*BittrexOrderbookUpdate, 0),
	}
	go func() {
		<-channels.done
		sr := b.signalr()
		if sr != nil {
			sr.Close()
		}
	}()
	bittrex = b
	return
}

// StringFloat handles JSON marshaling of floats that are encoded as strings
// on the wire.
type StringFloat float64

func (v *StringFloat) UnmarshalJSON(b []byte) error {
	if len(b) < 2 || b[0] != '"' || b[len(b)-1] != '"' {
		return fmt.Errorf("invalid StringFloat format: %s", string(b))
	}
	x, err := strconv.ParseFloat(string(b[1:len(b)-1]), 64)
	if err != nil {
		return err
	}
	*v = StringFloat(x)
	return nil
}

func (v StringFloat) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatFloat(float64(v), 'f', -1, 64))
}

// BittrexPriceResponse is the response from markets/{market}/tickers.
type BittrexPriceResponse struct {
	LastTradeRate StringFloat `json:"lastTradeRate"`
	BidRate       StringFloat `json:"bidRate"`
	AskRate       StringFloat `json:"askRate"`
}

// BittrexMarketSummary is the response from markets/{market}/summary.
type BittrexMarketSummary struct {
	Symbol        string      `json:"symbol"`
	High          StringFloat `json:"high"`
	Low           StringFloat `json:"low"`
	Volume        StringFloat `json:"volume"`
	QuoteVolume   StringFloat `json:"quoteVolume"`
	PercentChange StringFloat `json:"percentChange"`
	UpdatedAt     time.Time   `json:"updatedAt"`
}

// BittrexRateQty is an orderbook/depth data point.
type BittrexRateQty struct {
	Qty  StringFloat `json:"quantity"`
	Rate StringFloat `json:"rate"`
}

func translateBittrexDepth(bins []*BittrexRateQty) []DepthPoint {
	pts := make([]DepthPoint, 0, len(bins))
	for _, bin := range bins {
		pts = append(pts, DepthPoint{
			Quantity: float64(bin.Qty),
			Price:    float64(bin.Rate),
		})
	}
	return pts
}

func translateBittrexWSOrders(bins []*BittrexRateQty) wsOrders {
	ords := make(wsOrders, len(bins))
	for _, bin := range bins {
		r, q := float64(bin.Rate), float64(bin.Qty)
		ords[eightPtKey(r)] = &wsOrder{
			price:  r,
			volume: q,
		}
	}
	return ords
}

// BittrexDepthResponse is the response from /orderbook. The "orders" are
// actually binned pseudo-orders, but that's cool.
type BittrexDepthResponse struct {
	Seq uint64
	Bid []*BittrexRateQty `json:"bid"`
	Ask []*BittrexRateQty `json:"ask"`
}

// Translate the Bittrex response to DepthData.
func (r *BittrexDepthResponse) translate() *DepthData {
	if r == nil {
		return nil
	}
	return &DepthData{
		Time: time.Now().Unix(),
		Asks: translateBittrexDepth(r.Ask),
		Bids: translateBittrexDepth(r.Bid),
	}
}

// BittrexCandlestick is the response from one of the /candles endpoints.
type BittrexCandlestick struct {
	StartsAt    time.Time   `json:"startsAt"`
	Open        StringFloat `json:"open"`
	High        StringFloat `json:"high"`
	Low         StringFloat `json:"low"`
	Close       StringFloat `json:"close"`
	Volume      StringFloat `json:"volume"`
	QuoteVolume StringFloat `json:"quoteVolume"`
}

func translateBittrexCandlesticks(inSticks []*BittrexCandlestick) Candlesticks {
	sticks := make(Candlesticks, 0, len(inSticks))
	for _, stick := range inSticks {
		sticks = append(sticks, Candlestick{
			High:   float64(stick.High),
			Low:    float64(stick.Low),
			Open:   float64(stick.Open),
			Close:  float64(stick.Close),
			Volume: float64(stick.Volume),
			Start:  stick.StartsAt,
		})
	}
	return sticks
}

const maxBittrexQueueSize = 50

var bittrexSubscribeOrderbook = hubs.ClientMsg{
	H: "c3",
	M: "Subscribe",
	A: []interface{}{[]interface{}{"orderbook_DCR-BTC_500", "heartbeat"}},
}

// Process an update at a single rate.
func (bittrex *BittrexExchange) processBittrexOrderbookPoint(order *BittrexOrderbookDelta, book wsOrders) {
	rate, qty := float64(order.Rate), float64(order.Qty)
	k := eightPtKey(rate)
	switch qty {
	case 0:
		_, found := book[k]
		if !found {
			bittrex.setWsFail(fmt.Errorf("no order found for bittrex orderbook removal-type update at key %d\n", k))
			return
		}
		delete(book, k)
	default:
		book[k] = &wsOrder{
			price:  rate,
			volume: qty,
		}
	}
}

// Add an orderbook update to the queue. Also sorts the queue and checks for
// too many queued updates. Returns the nonce of the first update after sorting.
func (bittrex *BittrexExchange) queueOrderbookUpdate(update *BittrexOrderbookUpdate) uint64 {
	bittrex.queue = append(bittrex.queue, update)
	sort.Slice(bittrex.queue, func(i, j int) bool {
		return bittrex.queue[i].Sequence < bittrex.queue[j].Sequence
	})
	if len(bittrex.queue) > maxBittrexQueueSize {
		bittrex.setWsFail(fmt.Errorf("bittrex order update queue size exceeded"))
		bittrex.queue = make([]*BittrexOrderbookUpdate, 0)
		return 0
	}
	return bittrex.queue[0].Sequence
}

func (bittrex *BittrexExchange) processQueue() {
	queue := bittrex.queue
	bittrex.queue = make([]*BittrexOrderbookUpdate, 0)
	for _, update := range queue {
		bittrex.processOrderbookUpdate(update)
	}
}

// Process an update. Queues the order if it is not sequential.
func (bittrex *BittrexExchange) processOrderbookUpdate(update *BittrexOrderbookUpdate) {
	if update.Sequence <= bittrex.orderSeq {
		// Not necessarily an error. Simply discard.
		return
	}
	if update.Sequence != bittrex.orderSeq+1 {
		nextSeq := bittrex.queueOrderbookUpdate(update)
		if nextSeq <= bittrex.orderSeq+1 {
			bittrex.processQueue()
		}
		return
	}
	bittrex.orderSeq++

	for _, ask := range update.AskDeltas {
		bittrex.processBittrexOrderbookPoint(ask, bittrex.asks)
	}
	for _, buy := range update.BidDeltas {
		bittrex.processBittrexOrderbookPoint(buy, bittrex.buys)
	}
}

// Handle the initial orderbook from the websocket.
func (bittrex *BittrexExchange) processFullOrderbook(book *BittrexDepthResponse) {
	bittrex.orderMtx.Lock()
	defer bittrex.orderMtx.Unlock()

	bittrex.buys = translateBittrexWSOrders(book.Bid)
	bittrex.asks = translateBittrexWSOrders(book.Ask)
	bittrex.orderSeq = book.Seq
	bittrex.processQueue()
	bittrex.queue = make([]*BittrexOrderbookUpdate, 0)
	state := bittrex.state()
	if state != nil { // Only send update if price has been fetched
		bittrex.Update(&ExchangeState{
			BaseState: BaseState{
				Price:      state.Price,
				BaseVolume: state.BaseVolume,
				Volume:     state.Volume,
				Change:     state.Change,
			},
			Depth:        bittrex.wsDepthSnapshot(),
			Candlesticks: state.Candlesticks,
		})
	}
	bittrex.wsInitialized()
}

// Handles an update to the orderbook.
func (bittrex *BittrexExchange) processNextUpdate(update *BittrexOrderbookUpdate) {
	bittrex.orderMtx.Lock()
	defer bittrex.orderMtx.Unlock()
	if bittrex.orderSeq == 0 { // initial orderbook has not been received yet.
		bittrex.queueOrderbookUpdate(update)
		return
	}
	bittrex.processOrderbookUpdate(update)
}

// BittrexOrderbookUpdate is a websocket update to the orderbook.
type BittrexOrderbookUpdate struct {
	MarketSymbol string                   `json:"marketSymbol"`
	Depth        int64                    `json:"depth"`
	Sequence     uint64                   `json:"sequence"`
	BidDeltas    []*BittrexOrderbookDelta `json:"bidDeltas"`
	AskDeltas    []*BittrexOrderbookDelta `json:"askDeltas"`
}

// BittrexOrderbookDelta is the new quantity for the order bin at the specified
// rate. If the Qty is zero, there order should be removed.
type BittrexOrderbookDelta struct {
	Qty  StringFloat `json:"quantity"`
	Rate StringFloat `json:"rate"`
}

// BittrexWSMsg is used to parse the ridiculous signalr message format into
// something sane.
type BittrexWSMsg struct {
	Name    string
	Updates []*BittrexOrderbookUpdate
}

const (
	BittrexMsgHeartbeat  = "heartbeat"
	BittrexMsgBookUpdate = "orderBook"
)

func decodeBittrexWSMessage(msg signalr.Message) ([]*BittrexWSMsg, error) {
	msgs := make([]*BittrexWSMsg, 0, len(msg.M))
	for _, hubMsg := range msg.M {
		msg := &BittrexWSMsg{
			Name: hubMsg.M,
		}
		msgs = append(msgs, msg)
		if hubMsg.M == BittrexMsgHeartbeat {
			continue
		}
		if hubMsg.M != BittrexMsgBookUpdate {
			return nil, fmt.Errorf("unknown message type %q: %+v", hubMsg.M, hubMsg)
		}
		msg.Updates = make([]*BittrexOrderbookUpdate, 0, len(hubMsg.A))
		for _, arg := range hubMsg.A {

			s, ok := arg.(string)
			if !ok {
				return nil, fmt.Errorf("message not a string")
			}

			data, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return nil, fmt.Errorf("base64 error: %v", err)
			}

			buf := bytes.NewBuffer(data)
			zr := flate.NewReader(buf)
			defer zr.Close()

			var b bytes.Buffer
			if _, err := io.Copy(&b, zr); err != nil {
				return nil, fmt.Errorf("copy error: %v", err)
			}

			update := new(BittrexOrderbookUpdate)
			err = json.Unmarshal(b.Bytes(), update)
			if err != nil {
				return nil, fmt.Errorf("json error: %v", err)
			}

			msg.Updates = append(msg.Updates, update)
		}
	}
	return msgs, nil
}

// Handle the SignalR message. The message can be either a full orderbook at
// msg.R (msg.I == "1"), or a list of updates in msg.M[i].A.
func (bittrex *BittrexExchange) msgHandler(inMsg signalr.Message) {

	msgs, err := decodeBittrexWSMessage(inMsg)
	if err != nil {
		bittrex.setWsFail(fmt.Errorf("Bittrex websocket message decode error: %v", err))
		return
	}

	var count int
	for _, msg := range msgs {
		if msg.Name == BittrexMsgHeartbeat {
			// Do something ?
			continue
		}
		// Order book update
		for _, u := range msg.Updates {
			count++
			bittrex.processNextUpdate(u)
		}
	}
	if count > 0 {
		bittrex.wsUpdated()
	}
}

func (bittrex *BittrexExchange) orderbook() (*BittrexDepthResponse, error) {
	resp, err := bittrex.client.Do(bittrex.requests.depth)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("Request failed: %v", err))
	}
	defer resp.Body.Close()

	seqs, ok := resp.Header["Sequence"]
	if !ok {
		return nil, fmt.Errorf("Sequence not found in header")
	}

	if len(seqs) != 1 {
		return nil, fmt.Errorf("Invalid Sequence header length %d", len(seqs))
	}

	seq, err := strconv.ParseUint(seqs[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("Request failed: %v", err))
	}

	depthResponse := new(BittrexDepthResponse)
	err = json.NewDecoder(resp.Body).Decode(depthResponse)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("Failed to decode json from %s: %v", bittrex.requests.depth.URL.String(), err))
	}
	if err != nil {
		return nil, fmt.Errorf("Failed to retrieve Bittrex depth chart data: %v", err)
	}
	depthResponse.Seq = seq

	return depthResponse, nil
}

// Connect to the websocket and send the update subscription. Delay sending the
// full orderbook subscription until the first delta is received because sending
// it too soon can cause missed updates. Even if there is no action on the
// bittrex order book, they will periodically send empty updates, which will
// trigger the full order book request.
func (bittrex *BittrexExchange) connectWs() {
	bittrex.orderMtx.Lock()
	bittrex.queue = make([]*BittrexOrderbookUpdate, 0)
	bittrex.orderSeq = 0
	bittrex.orderMtx.Unlock()

	err := bittrex.connectSignalr(&signalrConfig{
		host:       "socket-v3.bittrex.com",
		protocol:   "1.5",
		endpoint:   "/signalr",
		msgHandler: bittrex.msgHandler,
	})
	if err != nil {
		bittrex.setWsFail(err)
		return
	}

	// Subscribe to the feed. The full orderbook will be requested once the first
	// delta is received.
	err = bittrex.sr.Send(bittrexSubscribeOrderbook)
	if err != nil {
		bittrex.setWsFail(fmt.Errorf("Failed to send order update request to bittrex: %v", err))
		return
	}

	book, err := bittrex.orderbook()
	if err != nil {
		bittrex.setWsFail(err)
		return
	}

	bittrex.processFullOrderbook(book)
}

// Refresh retrieves and parses API data from Bittrex.
// Bittrex provides timestamps in a string format that is not quite RFC 3339.
func (bittrex *BittrexExchange) Refresh() {
	bittrex.LogRequest()
	priceResponse := new(BittrexPriceResponse)
	err := bittrex.fetch(bittrex.requests.price, priceResponse)
	if err != nil {
		bittrex.fail("Fetch price", err)
		return
	}

	dayStats := new(BittrexMarketSummary)
	err = bittrex.fetch(bittrex.requests.stats, dayStats)
	if err != nil {
		bittrex.fail("Fetch stats", err)
		return
	}

	// Check for a depth chart from the websocket orderbook.
	tryHttp, wsStarting, depth := bittrex.wsDepthStatus(bittrex.connectWs)

	// If not expecting depth data from the websocket, grab it from HTTP
	if tryHttp {
		depthResponse, err := bittrex.orderbook()
		if err != nil {
			log.Errorf("Failed to retrieve Bittrex depth chart data: %v", err)
		}
		depth = depthResponse.translate()
	}

	if !wsStarting {
		sinceLast := time.Since(bittrex.wsLastUpdate())
		log.Tracef("last bittrex websocket update %.3f seconds ago", sinceLast.Seconds())
		if sinceLast > depthDataExpiration && !bittrex.wsFailed() {
			bittrex.setWsFail(fmt.Errorf("lost connection detected. bittrex websocket will restart during next refresh"))
		}
	}

	// Check for expired candlesticks
	state := bittrex.state()
	candlesticks := map[candlestickKey]Candlesticks{}
	for bin, req := range bittrex.requests.candlesticks {
		oldSticks, found := state.Candlesticks[bin]
		if !found || oldSticks.needsUpdate(bin) {
			log.Tracef("Signalling candlestick update for %s, bin size %s", bittrex.token, bin)
			var inSticks []*BittrexCandlestick
			err := bittrex.fetch(req, &inSticks)
			if err != nil {
				log.Errorf("Error retrieving candlestick data from Bittrex for bin size %s: %v", string(bin), err)
				continue
			}
			candlesticks[bin] = translateBittrexCandlesticks(inSticks)
		}
	}

	cFactor := 1 + (dayStats.PercentChange / 100)
	oldPrice := priceResponse.LastTradeRate / cFactor
	dayChange := priceResponse.LastTradeRate - oldPrice

	update := &ExchangeState{
		BaseState: BaseState{
			Price:      float64(priceResponse.LastTradeRate),
			BaseVolume: float64(dayStats.QuoteVolume),
			Volume:     float64(dayStats.Volume),
			Change:     float64(dayChange),
		},
		Depth:        depth,
		Candlesticks: candlesticks,
	}

	if wsStarting {
		bittrex.SilentUpdate(update)
	} else {
		bittrex.Update(update)
	}
}

// DragonExchange is a Singapore-based crytocurrency exchange.
type DragonExchange struct {
	*CommonExchange
	SymbolID         int
	depthBuyRequest  *http.Request
	depthSellRequest *http.Request
}

// NewDragonEx constructs a DragonExchange.
func NewDragonEx(client *http.Client, channels *BotChannels) (dragonex Exchange, err error) {
	reqs := newRequests()
	reqs.price, err = http.NewRequest(http.MethodGet, DragonExURLs.Price, nil)
	if err != nil {
		return
	}

	// Dragonex has separate endpoints for buy and sell, so the requests are
	// stored as fields of DragonExchange
	var depthSell, depthBuy *http.Request
	depthSell, err = http.NewRequest(http.MethodGet, fmt.Sprintf(DragonExURLs.Depth, "sell"), nil)
	if err != nil {
		return
	}

	depthBuy, err = http.NewRequest(http.MethodGet, fmt.Sprintf(DragonExURLs.Depth, "buy"), nil)
	if err != nil {
		return
	}

	for dur, url := range DragonExURLs.Candlesticks {
		reqs.candlesticks[dur], err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return
		}
	}

	dragonex = &DragonExchange{
		CommonExchange:   newCommonExchange(DragonEx, client, reqs, channels),
		SymbolID:         1520101,
		depthBuyRequest:  depthBuy,
		depthSellRequest: depthSell,
	}
	return
}

// DragonExResponse models the generic fields returned in every response.
type DragonExResponse struct {
	Ok   bool   `json:"ok"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// DragonExPriceResponse models the JSON data returned from the DragonEx API.
type DragonExPriceResponse struct {
	DragonExResponse
	Data []DragonExPriceResponseData `json:"data"`
}

// DragonExPriceResponseData models the JSON data from the DragonEx API.
// Dragonex has the current price in close_price
type DragonExPriceResponseData struct {
	ClosePrice      string `json:"close_price"`
	CurrentVolume   string `json:"current_volume"`
	MaxPrice        string `json:"max_price"`
	MinPrice        string `json:"min_price"`
	OpenPrice       string `json:"open_price"`
	PriceBase       string `json:"price_base"`
	PriceChange     string `json:"price_change"`
	PriceChangeRate string `json:"price_change_rate"`
	Timestamp       int64  `json:"timestamp"`
	TotalAmount     string `json:"total_amount"`
	TotalVolume     string `json:"total_volume"`
	UsdtVolume      string `json:"usdt_amount"`
	SymbolID        int    `json:"symbol_id"`
}

// DragonExDepthPt models a single point of data in a Dragon Exchange depth
// chart data set.
type DragonExDepthPt struct {
	Price  string `json:"price"`
	Volume string `json:"volume"`
}

// DragonExDepthArray is a slice of DragonExDepthPt.
type DragonExDepthArray []DragonExDepthPt

func (pts DragonExDepthArray) translate() []DepthPoint {
	outPts := make([]DepthPoint, 0, len(pts))
	for _, pt := range pts {
		price, err := strconv.ParseFloat(pt.Price, 64)
		if err != nil {
			log.Errorf("DragonExDepthArray.translate failed to parse float from %s", pt.Price)
			return []DepthPoint{}
		}

		volume, err := strconv.ParseFloat(pt.Volume, 64)
		if err != nil {
			log.Errorf("DragonExDepthArray.translate failed to parse volume from %s", pt.Volume)
			return []DepthPoint{}
		}
		outPts = append(outPts, DepthPoint{
			Quantity: volume,
			Price:    price,
		})
	}
	return outPts
}

// DragonExDepthResponse models the Dragon Exchange depth chart data response.
type DragonExDepthResponse struct {
	DragonExResponse
	Data DragonExDepthArray `json:"data"`
}

// DragonExCandlestickColumns models the column list returned in a candlestick
// chart data response from Dragon Exchange.
type DragonExCandlestickColumns []string

func (keys DragonExCandlestickColumns) index(dxKey string) (int, error) {
	for idx, key := range keys {
		if key == dxKey {
			return idx, nil
		}
	}
	return -1, fmt.Errorf("Unable to locate DragonEx candlestick key %s", dxKey)
}

const (
	dxHighKey   = "max_price"
	dxLowKey    = "min_price"
	dxOpenKey   = "open_price"
	dxCloseKey  = "close_price"
	dxVolumeKey = "volume"
	dxTimeKey   = "timestamp"
)

// DragonExCandlestickList models the value list returned in a candlestick
// chart data response from Dragon Exchange.
type DragonExCandlestickList []interface{}

func (list DragonExCandlestickList) getFloat(idx int) (float64, error) {
	if len(list) < idx+1 {
		return -1, fmt.Errorf("DragonEx candlestick point index %d out of range", idx)
	}
	valStr, ok := list[idx].(string)
	if !ok {
		return -1, fmt.Errorf("DragonEx.getFloat found unexpected type at index %d", idx)
	}
	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return -1, fmt.Errorf("DragonEx candlestick parseFloat error: %v", err)
	}
	return val, nil
}

// DragonExCandlestickPts is a list of DragonExCandlestickList.
type DragonExCandlestickPts []DragonExCandlestickList

// DragonExCandlestickData models the Data field of DragonExCandlestickResponse.
type DragonExCandlestickData struct {
	Columns DragonExCandlestickColumns `json:"columns"`
	Lists   DragonExCandlestickPts     `json:"lists"`
}

func badDragonexStickElement(key string, err error) Candlesticks {
	log.Errorf("Unable to decode %s from Binance candlestick: %v", key, err)
	return Candlesticks{}
}

func (data DragonExCandlestickData) translate( /*cKey candlestickKey*/ ) Candlesticks {
	sticks := make(Candlesticks, 0, len(data.Lists))
	var idx int
	var err error
	for _, pt := range data.Lists {
		idx, err = data.Columns.index(dxHighKey)
		if err != nil {
			return badDragonexStickElement(dxHighKey, err)
		}
		high, err := pt.getFloat(idx)
		if err != nil {
			return badDragonexStickElement(dxHighKey, err)
		}

		idx, err = data.Columns.index(dxLowKey)
		if err != nil {
			return badDragonexStickElement(dxLowKey, err)
		}
		low, err := pt.getFloat(idx)
		if err != nil {
			return badDragonexStickElement(dxLowKey, err)
		}

		idx, err = data.Columns.index(dxOpenKey)
		if err != nil {
			return badDragonexStickElement(dxOpenKey, err)
		}
		open, err := pt.getFloat(idx)
		if err != nil {
			return badDragonexStickElement(dxOpenKey, err)
		}

		idx, err = data.Columns.index(dxCloseKey)
		if err != nil {
			return badDragonexStickElement(dxCloseKey, err)
		}
		close, err := pt.getFloat(idx)
		if err != nil {
			return badDragonexStickElement(dxCloseKey, err)
		}

		idx, err = data.Columns.index(dxVolumeKey)
		if err != nil {
			return badDragonexStickElement(dxVolumeKey, err)
		}
		volume, err := pt.getFloat(idx)
		if err != nil {
			return badDragonexStickElement(dxVolumeKey, err)
		}

		idx, err = data.Columns.index(dxTimeKey)
		if err != nil {
			return badDragonexStickElement(dxTimeKey, err)
		}
		if len(pt) < idx+1 {
			return badDragonexStickElement(dxTimeKey, fmt.Errorf("DragonEx time index %d out of range", idx))
		}
		unixFloat, ok := pt[idx].(float64)
		if !ok {
			return badDragonexStickElement(dxTimeKey, fmt.Errorf("DragonEx found unexpected type for time at index %d", idx))
		}
		startTime := time.Unix(int64(unixFloat), 0)

		sticks = append(sticks, Candlestick{
			High:   high,
			Low:    low,
			Open:   open,
			Close:  close,
			Volume: volume,
			Start:  startTime,
		})
	}
	return sticks
}

// DragonExCandlestickResponse models the response from DragonEx for the
// historical k-line data.
type DragonExCandlestickResponse struct {
	DragonExResponse
	Data DragonExCandlestickData
}

func (dragonex *DragonExchange) getDragonExDepthData(req *http.Request, response *DragonExDepthResponse) error {
	err := dragonex.fetch(req, response)
	if err != nil {
		return fmt.Errorf("DragonEx buy order book response error: %v", err)
	}
	if !response.Ok {
		return fmt.Errorf("DragonEx depth response server error with message: %s", response.Msg)
	}
	return nil
}

// Refresh retrieves and parses API data from DragonEx.
func (dragonex *DragonExchange) Refresh() {
	dragonex.LogRequest()
	response := new(DragonExPriceResponse)
	err := dragonex.fetch(dragonex.requests.price, response)
	if err != nil {
		dragonex.fail("Fetch", err)
		return
	}
	if !response.Ok {
		dragonex.fail("Response not ok", err)
		return
	}
	if len(response.Data) == 0 {
		dragonex.fail("No data", fmt.Errorf("Response data array is empty"))
		return
	}
	data := response.Data[0]
	if data.SymbolID != dragonex.SymbolID {
		dragonex.fail("Wrong code", fmt.Errorf("Pair id %d in response is not the expected id %d", data.SymbolID, dragonex.SymbolID))
		return
	}
	price, err := strconv.ParseFloat(data.ClosePrice, 64)
	if err != nil {
		dragonex.fail(fmt.Sprintf("Failed to parse float from ClosePrice=%s", data.ClosePrice), err)
		return
	}
	volume, err := strconv.ParseFloat(data.TotalVolume, 64)
	if err != nil {
		dragonex.fail(fmt.Sprintf("Failed to parse float from TotalVolume=%s", data.TotalVolume), err)
		return
	}
	btcVolume := volume * price
	priceChange, err := strconv.ParseFloat(data.PriceChange, 64)
	if err != nil {
		dragonex.fail(fmt.Sprintf("Failed to parse float from PriceChange=%s", data.PriceChange), err)
		return
	}

	// Depth chart
	depthSellResponse := new(DragonExDepthResponse)
	sellErr := dragonex.getDragonExDepthData(dragonex.depthSellRequest, depthSellResponse)
	if sellErr != nil {
		log.Errorf("DragonEx sell order book response error: %v", sellErr)
	}

	depthBuyResponse := new(DragonExDepthResponse)
	buyErr := dragonex.getDragonExDepthData(dragonex.depthBuyRequest, depthBuyResponse)
	if buyErr != nil {
		log.Errorf("DragonEx buy order book response error: %v", buyErr)
	}

	var depth *DepthData
	if sellErr == nil && buyErr == nil {
		depth = &DepthData{
			Time: time.Now().Unix(),
			Bids: depthBuyResponse.Data.translate(),
			Asks: depthSellResponse.Data.translate(),
		}
	}

	// Grab the current state to check if candlesticks need updating
	state := dragonex.state()

	candlesticks := map[candlestickKey]Candlesticks{}
	for bin, req := range dragonex.requests.candlesticks {
		oldSticks, found := state.Candlesticks[bin]
		if !found || oldSticks.needsUpdate(bin) {
			log.Tracef("Signalling candlestick update for %s, bin size %s", dragonex.token, bin)
			response := new(DragonExCandlestickResponse)
			err := dragonex.fetch(req, response)
			if err != nil {
				log.Errorf("Error retrieving candlestick data from dragonex for bin size %s: %v", string(bin), err)
				continue
			}
			if !response.Ok {
				log.Errorf("DragonEx server error while fetching candlestick data. Message: %s", response.Msg)
			}

			sticks := response.Data.translate()
			if !found || sticks.time().After(oldSticks.time()) {
				candlesticks[bin] = sticks
			}
		}
	}

	dragonex.Update(&ExchangeState{
		BaseState: BaseState{
			Price:      price,
			BaseVolume: btcVolume,
			Volume:     volume,
			Change:     priceChange,
			Stamp:      data.Timestamp,
		},
		Depth:        depth,
		Candlesticks: candlesticks,
	})
}

// HuobiExchange is based in Hong Kong and Singapore.
type HuobiExchange struct {
	*CommonExchange
	Ok string
}

// NewHuobi constructs a HuobiExchange.
func NewHuobi(client *http.Client, channels *BotChannels) (huobi Exchange, err error) {
	reqs := newRequests()
	reqs.price, err = http.NewRequest(http.MethodGet, HuobiURLs.Price, nil)
	if err != nil {
		return
	}
	reqs.price.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	reqs.depth, err = http.NewRequest(http.MethodGet, HuobiURLs.Depth, nil)
	if err != nil {
		return
	}
	reqs.depth.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	for dur, url := range HuobiURLs.Candlesticks {
		reqs.candlesticks[dur], err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return
		}
		reqs.candlesticks[dur].Header.Add("Content-Type", "application/x-www-form-urlencoded")
	}

	return &HuobiExchange{
		CommonExchange: newCommonExchange(Huobi, client, reqs, channels),
		Ok:             "ok",
	}, nil
}

// HuobiResponse models the common response fields in all API BittrexResponseResult
type HuobiResponse struct {
	Status string `json:"status"`
	Ch     string `json:"ch"`
	Ts     int64  `json:"ts"`
}

// HuobiPriceTick models the "tick" field of the Huobi API response.
type HuobiPriceTick struct {
	Amount  float64   `json:"amount"`
	Open    float64   `json:"open"`
	Close   float64   `json:"close"`
	High    float64   `json:"high"`
	ID      int64     `json:"id"`
	Count   int64     `json:"count"`
	Low     float64   `json:"low"`
	Version int64     `json:"version"`
	Ask     []float64 `json:"ask"`
	Vol     float64   `json:"vol"`
	Bid     []float64 `json:"bid"`
}

// HuobiPriceResponse models the JSON data returned from the Huobi API.
type HuobiPriceResponse struct {
	HuobiResponse
	Tick HuobiPriceTick `json:"tick"`
}

// HuobiDepthPts is a list of tuples [price, volume].
type HuobiDepthPts [][2]float64

func (pts HuobiDepthPts) translate() []DepthPoint {
	outPts := make([]DepthPoint, 0, len(pts))
	for _, pt := range pts {
		outPts = append(outPts, DepthPoint{
			Quantity: pt[1],
			Price:    pt[0],
		})
	}
	return outPts
}

// HuobiDepthTick models the tick field of the Huobi depth chart response.
type HuobiDepthTick struct {
	ID   int64         `json:"id"`
	Ts   int64         `json:"ts"`
	Bids HuobiDepthPts `json:"bids"`
	Asks HuobiDepthPts `json:"asks"`
}

// HuobiDepthResponse models the response from a Huobi API depth chart response.
type HuobiDepthResponse struct {
	HuobiResponse
	Tick HuobiDepthTick `json:"tick"`
}

// HuobiCandlestickPt is a single candlestick pt in a Huobi API candelstick
// response.
type HuobiCandlestickPt struct {
	ID     int64   `json:"id"` // ID is actually start time as unix stamp
	Open   float64 `json:"open"`
	Close  float64 `json:"close"`
	Low    float64 `json:"low"`
	High   float64 `json:"high"`
	Amount float64 `json:"amount"` // Volume BTC
	Vol    float64 `json:"vol"`    // Volume DCR
	Count  int64   `json:"count"`
}

// HuobiCandlestickData is a list of candlestick data pts.
type HuobiCandlestickData []*HuobiCandlestickPt

func (pts HuobiCandlestickData) translate() Candlesticks {
	sticks := make(Candlesticks, 0, len(pts))
	// reverse the order
	for i := len(pts) - 1; i >= 0; i-- {
		pt := pts[i]
		sticks = append(sticks, Candlestick{
			High:   pt.High,
			Low:    pt.Low,
			Open:   pt.Open,
			Close:  pt.Close,
			Volume: pt.Vol,
			Start:  time.Unix(pt.ID, 0),
		})
	}
	return sticks
}

// HuobiCandlestickResponse models the response from Huobi for candlestick data.
type HuobiCandlestickResponse struct {
	HuobiResponse
	Data HuobiCandlestickData `json:"data"`
}

// Refresh retrieves and parses API data from Huobi.
func (huobi *HuobiExchange) Refresh() {
	huobi.LogRequest()
	priceResponse := new(HuobiPriceResponse)
	err := huobi.fetch(huobi.requests.price, priceResponse)
	if err != nil {
		huobi.fail("Fetch", err)
		return
	}
	if priceResponse.Status != huobi.Ok {
		huobi.fail("Status not ok", fmt.Errorf("Expected status %s. Received %s", huobi.Ok, priceResponse.Status))
		return
	}
	baseVolume := priceResponse.Tick.Vol

	// Depth data
	var depth *DepthData
	depthResponse := new(HuobiDepthResponse)
	err = huobi.fetch(huobi.requests.depth, depthResponse)
	if err != nil {
		log.Errorf("Huobi depth chart fetch error: %v", err)
	} else if depthResponse.Status != huobi.Ok {
		log.Errorf("Huobi server depth response error. status: %s", depthResponse.Status)
	} else {
		depth = &DepthData{
			Time: depthResponse.Ts / 1000,
			Bids: depthResponse.Tick.Bids.translate(),
			Asks: depthResponse.Tick.Asks.translate(),
		}
	}

	// Candlestick data
	state := huobi.state()
	candlesticks := map[candlestickKey]Candlesticks{}
	for bin, req := range huobi.requests.candlesticks {
		oldSticks, found := state.Candlesticks[bin]
		if !found || oldSticks.needsUpdate(bin) {
			log.Tracef("Signalling candlestick update for %s, bin size %s", huobi.token, bin)
			response := new(HuobiCandlestickResponse)
			err := huobi.fetch(req, response)
			if err != nil {
				log.Errorf("Error retrieving candlestick data from huobi for bin size %s: %v", string(bin), err)
				continue
			}
			if response.Status != huobi.Ok {
				log.Errorf("Huobi server error while fetching candlestick data. status: %s", response.Status)
				continue
			}

			sticks := response.Data.translate()
			if !found || sticks.time().After(oldSticks.time()) {
				candlesticks[bin] = sticks
			}
		}
	}

	huobi.Update(&ExchangeState{
		BaseState: BaseState{
			Price:      priceResponse.Tick.Close,
			BaseVolume: baseVolume,
			Volume:     baseVolume / priceResponse.Tick.Close,
			Change:     priceResponse.Tick.Close - priceResponse.Tick.Open,
			Stamp:      priceResponse.Ts / 1000,
		},
		Depth:        depth,
		Candlesticks: candlesticks,
	})
}

// PoloniexExchange is a U.S.-based exchange.
type PoloniexExchange struct {
	*CommonExchange
	CurrencyPair string
	orderSeq     int64
}

// NewPoloniex constructs a PoloniexExchange.
func NewPoloniex(client *http.Client, channels *BotChannels) (poloniex Exchange, err error) {
	reqs := newRequests()
	reqs.price, err = http.NewRequest(http.MethodGet, PoloniexURLs.Price, nil)
	if err != nil {
		return
	}

	reqs.depth, err = http.NewRequest(http.MethodGet, PoloniexURLs.Depth, nil)
	if err != nil {
		return
	}

	for dur, url := range PoloniexURLs.Candlesticks {
		reqs.candlesticks[dur], err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return
		}
	}

	p := &PoloniexExchange{
		CommonExchange: newCommonExchange(Poloniex, client, reqs, channels),
		CurrencyPair:   "BTC_DCR",
	}
	go func() {
		<-channels.done
		ws, _ := p.websocket()
		if ws != nil {
			ws.Close()
		}
	}()
	poloniex = p
	return
}

// PoloniexPair models the data returned from the Poloniex API.
type PoloniexPair struct {
	ID            int    `json:"id"`
	Last          string `json:"last"`
	LowestAsk     string `json:"lowestAsk"`
	HighestBid    string `json:"highestBid"`
	PercentChange string `json:"percentChange"`
	BaseVolume    string `json:"baseVolume"`
	QuoteVolume   string `json:"quoteVolume"`
	IsFrozen      string `json:"isFrozen"`
	High24hr      string `json:"high24hr"`
	Low24hr       string `json:"low24hr"`
}

// PoloniexDepthPt is a tuple of ["price", volume].
type PoloniexDepthPt [2]interface{}

func (pt *PoloniexDepthPt) price() (float64, error) {
	pStr, ok := pt[0].(string)
	if !ok {
		return -1, fmt.Errorf("Poloniex depth price translation type error. Failed to parse string from %v, type %T", pt[0], pt[0])
	}
	price, err := strconv.ParseFloat(pStr, 64)
	if err != nil {
		return -1, fmt.Errorf("Poloniex depth price parseFloat error: %v", err)
	}
	return price, nil
}

func (pt *PoloniexDepthPt) volume() (float64, error) {
	volume, ok := pt[1].(float64)
	if !ok {
		return -1, fmt.Errorf("Poloniex depth volume translation type error. Failed to parse float from %v, type %T", pt[0], pt[0])
	}
	return volume, nil
}

// PoloniexDepthArray is a slice of depth chart data points.
type PoloniexDepthArray []*PoloniexDepthPt

func (pts PoloniexDepthArray) translate() ([]DepthPoint, error) {
	outPts := make([]DepthPoint, 0, len(pts))
	for _, pt := range pts {
		price, err := pt.price()
		if err != nil {
			return []DepthPoint{}, err
		}

		volume, err := pt.volume()
		if err != nil {
			return []DepthPoint{}, err
		}

		outPts = append(outPts, DepthPoint{
			Quantity: volume,
			Price:    price,
		})
	}
	return outPts, nil
}

// PoloniexDepthResponse models the response from Poloniex for depth chart data.
type PoloniexDepthResponse struct {
	Asks     PoloniexDepthArray `json:"asks"`
	Bids     PoloniexDepthArray `json:"bids"`
	IsFrozen string             `json:"isFrozen"`
	Seq      int64              `json:"seq"`
}

func (r *PoloniexDepthResponse) translate() *DepthData {
	if r == nil {
		return nil
	}
	depth := new(DepthData)
	depth.Time = time.Now().Unix()
	var err error
	depth.Asks, err = r.Asks.translate()
	if err != nil {
		log.Errorf("%v")
		return nil
	}

	depth.Bids, err = r.Bids.translate()
	if err != nil {
		log.Errorf("%v", err)
		return nil
	}

	return depth
}

// PoloniexCandlestickResponse models the k-line data response from Poloniex.
// {"date":1463356800,"high":1,"low":0.0037,"open":1,"close":0.00432007,"volume":357.23057396,"quoteVolume":76195.11422729,"weightedAverage":0.00468836}

type PoloniexCandlestickPt struct {
	Date            int64   `json:"date"`
	High            float64 `json:"high"`
	Low             float64 `json:"low"`
	Open            float64 `json:"open"`
	Close           float64 `json:"close"`
	Volume          float64 `json:"volume"`
	QuoteVolume     float64 `json:"quoteVolume"`
	WeightedAverage float64 `json:"weightedAverage"`
}

type PoloniexCandlestickResponse []*PoloniexCandlestickPt

func (r PoloniexCandlestickResponse) translate( /*bin candlestickKey*/ ) Candlesticks {
	sticks := make(Candlesticks, 0, len(r))
	for _, stick := range r {
		sticks = append(sticks, Candlestick{
			High:   stick.High,
			Low:    stick.Low,
			Open:   stick.Open,
			Close:  stick.Close,
			Volume: stick.QuoteVolume,
			Start:  time.Unix(stick.Date, 0),
		})
	}
	return sticks
}

// All poloniex websocket subscriptions messages have this form.
type poloniexWsSubscription struct {
	Command string `json:"command"`
	Channel int    `json:"channel"`
}

var poloniexOrderbookSubscription = poloniexWsSubscription{
	Command: "subscribe",
	Channel: 162,
}

// The final structure to parse in the initial websocket message is a map of the
// form {"12.3456":"23.4567", "12.4567":"123.4567", ...} where the price is
// a string-float and is the key to string-float volumes.
func (poloniex *PoloniexExchange) parseOrderMap(book map[string]interface{}, orders wsOrders) error {
	for p, v := range book {
		price, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return fmt.Errorf("Failed to parse float from poloniex orderbook price. given %s: %v", p, err)
		}
		vStr, ok := v.(string)
		if !ok {
			return fmt.Errorf("Failed to cast poloniex orderbook volume to string. given %s", v)
		}
		volume, err := strconv.ParseFloat(vStr, 64)
		if err != nil {
			return fmt.Errorf("Failed to parse float from poloniex orderbook volume string. given %s: %v", p, err)
		}
		binKey := eightPtKey(price)
		orders[binKey] = &wsOrder{
			price:  price,
			volume: volume,
		}
	}
	return nil
}

// This initial websocket message is a full orderbook.
func (poloniex *PoloniexExchange) processWsOrderbook(sequenceID int64, responseList []interface{}) {
	subList, ok := responseList[0].([]interface{})
	if !ok {
		poloniex.setWsFail(fmt.Errorf("Failed to parse 0th element of poloniex response array"))
		return
	}
	if len(subList) < 2 {
		poloniex.setWsFail(fmt.Errorf("Unexpected sub-list length in poloniex websocket response: %d", len(subList)))
		return
	}
	d, ok := subList[1].(map[string]interface{})
	if !ok {
		poloniex.setWsFail(fmt.Errorf("Failed to parse response map from poloniex websocket response"))
		return
	}
	orderBook, ok := d["orderBook"].([]interface{})
	if !ok {
		poloniex.setWsFail(fmt.Errorf("Failed to parse orderbook list from poloniex websocket response"))
		return
	}
	if len(orderBook) < 2 {
		poloniex.setWsFail(fmt.Errorf("Unexpected orderBook list length in poloniex websocket response: %d", len(subList)))
		return
	}
	asks, ok := orderBook[0].(map[string]interface{})
	if !ok {
		poloniex.setWsFail(fmt.Errorf("Failed to parse asks from poloniex orderbook"))
		return
	}

	buys, ok := orderBook[1].(map[string]interface{})
	if !ok {
		poloniex.setWsFail(fmt.Errorf("Failed to parse buys from poloniex orderbook"))
		return
	}

	poloniex.orderMtx.Lock()
	defer poloniex.orderMtx.Unlock()
	poloniex.orderSeq = sequenceID
	err := poloniex.parseOrderMap(asks, poloniex.asks)
	if err != nil {
		poloniex.setWsFail(err)
		return
	}

	err = poloniex.parseOrderMap(buys, poloniex.buys)
	if err != nil {
		poloniex.setWsFail(err)
		return
	}
	poloniex.wsInitialized()
}

// A helper for merging a source map into a target map. Poloniex order in the
// source map with volume 0 will trigger a deletion from the target map.
func mergePoloniexDepthUpdates(target, source wsOrders) {
	for bin, pt := range source {
		if pt.volume == 0 {
			delete(source, bin)
			delete(target, bin)
			continue
		}
		target[bin] = pt
	}
}

// Merge order updates under a write lock.
func (poloniex *PoloniexExchange) accumulateOrders(sequenceID int64, asks, buys wsOrders) {
	poloniex.orderMtx.Lock()
	defer poloniex.orderMtx.Unlock()
	poloniex.orderSeq++
	if sequenceID != poloniex.orderSeq {
		poloniex.setWsFail(fmt.Errorf("poloniex sequence id failure. expected %d, received %d", poloniex.orderSeq, sequenceID))
		return
	}
	mergePoloniexDepthUpdates(poloniex.asks, asks)
	mergePoloniexDepthUpdates(poloniex.buys, buys)
}

const (
	poloniexHeartbeatCode       = 1010
	poloniexInitialOrderbookKey = "i"
	poloniexOrderUpdateKey      = "o"
	poloniexTradeUpdateKey      = "t"
	poloniexAskDirection        = 0
	poloniexBuyDirection        = 1
)

// Poloniex has a string code in the result array indicating what type of
// message it is.
func firstCode(responseList []interface{}) string {
	firstElement, ok := responseList[0].([]interface{})
	if !ok {
		log.Errorf("parse failure in poloniex websocket message")
		return ""
	}
	if len(firstElement) < 1 {
		log.Errorf("unexpected number of parameters in poloniex websocket message")
		return ""
	}
	updateType, ok := firstElement[0].(string)
	if !ok {
		log.Errorf("failed to type convert poloniex message update type")
		return ""
	}
	return updateType
}

// For Poloniex message "o", an update to the orderbook.
func processPoloniexOrderbookUpdate(updateParams []interface{}) (*wsOrder, int, error) {
	floatDir, ok := updateParams[1].(float64)
	if !ok {
		return nil, -1, fmt.Errorf("failed to type convert poloniex orderbook update direction")
	}
	direction := int(floatDir)
	priceStr, ok := updateParams[2].(string)
	if !ok {
		return nil, -1, fmt.Errorf("failed to type convert poloniex orderbook update price")
	}
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to convert poloniex orderbook update price to float: %v", err)
	}
	volStr, ok := updateParams[3].(string)
	if !ok {
		return nil, -1, fmt.Errorf("failed to type convert poloniex orderbook update volume")
	}
	volume, err := strconv.ParseFloat(volStr, 64)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to convert poloniex orderbook update volume to float: %v", err)
	}
	return &wsOrder{
		price:  price,
		volume: volume,
	}, direction, nil
}

// For Poloniex message "t", a trade. This seems to be used rarely and
// sporadically, but it is used. For the BTC_DCR endpoint, almost all updates
// are of the "o" type, an orderbook update. The docs are unclear about whether a trade updates the
// order book, but testing seems to indicate that a "t" message is for trades
// that occur off of the orderbook.
/*
func (poloniex *PoloniexExchange) processTrade(tradeParams []interface{}) (*wsOrder, int, error) {
	if len(tradeParams) != 6 {
		return nil, -1, fmt.Errorf("Not enough parameters in poloniex trade notification. given: %d", len(tradeParams))
	}
	floatDir, ok := tradeParams[2].(float64)
	if !ok {
		return nil, -1, fmt.Errorf("failed to type convert poloniex orderbook update direction")
	}
	direction := (int(floatDir) + 1) % 2
	priceStr, ok := tradeParams[3].(string)
	if !ok {
		return nil, -1, fmt.Errorf("failed to type convert poloniex orderbook update price")
	}
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to convert poloniex orderbook update price to float: %v", err)
	}
	volStr, ok := tradeParams[4].(string)
	if !ok {
		return nil, -1, fmt.Errorf("failed to type convert poloniex orderbook update volume")
	}
	volume, err := strconv.ParseFloat(volStr, 64)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to convert poloniex orderbook update volume to float: %v", err)
	}
	trade := &wsOrder{
		price:  price,
		volume: volume,
	}
	return trade, direction, nil
}
*/

// Poloniex's WebsocketProcessor. Handles messages of type "i", "o", and "t".
func (poloniex *PoloniexExchange) processWsMessage(raw []byte) {
	msg := make([]interface{}, 0)
	err := json.Unmarshal(raw, &msg)
	if err != nil {
		poloniex.setWsFail(err)
		return
	}
	switch len(msg) {
	case 1:
		// Likely a heatbeat
		code, ok := msg[0].(float64)
		if !ok {
			poloniex.setWsFail(fmt.Errorf("non-integer single-element poloniex response of implicit type %T", msg[0]))
			return
		}
		intCode := int(code)
		if intCode == poloniexHeartbeatCode {
			return
		}
		poloniex.setWsFail(fmt.Errorf("unknown code in single-element poloniex response: %d", intCode))
		return
	case 3:
		responseList, ok := msg[2].([]interface{})
		if !ok {
			poloniex.setWsFail(fmt.Errorf("poloniex websocket message type assertion failure: %T", msg[2]))
			return
		}

		if len(responseList) == 0 {
			poloniex.setWsFail(fmt.Errorf("zero-length response list received from poloniex"))
			return
		}

		code := firstCode(responseList)
		rawSeq, ok := msg[1].(float64)
		if !ok {
			poloniex.setWsFail(fmt.Errorf("poloniex websocket sequence id type assertion failure: %T", msg[2]))
			return
		}
		seq := int64(rawSeq)

		if code == poloniexInitialOrderbookKey {
			poloniex.processWsOrderbook(seq, responseList)
			state := poloniex.state()
			if state != nil { // Only send update if price has been fetched
				depth := poloniex.wsDepths()
				poloniex.Update(&ExchangeState{
					BaseState: BaseState{
						Price:      state.Price,
						BaseVolume: state.BaseVolume,
						Volume:     state.Volume,
						Change:     state.Change,
					},
					Depth:        depth,
					Candlesticks: state.Candlesticks,
				})
			}
			return
		}

		if code != poloniexOrderUpdateKey && code != poloniexTradeUpdateKey {
			poloniex.setWsFail(fmt.Errorf("Unexpected code in first element of poloniex websocket response list: %s", code))
			return
		}

		newAsks := make(wsOrders)
		newBids := make(wsOrders)
		var count int
		for _, update := range responseList {
			updateParams, ok := update.([]interface{})
			if !ok {
				poloniex.setWsFail(fmt.Errorf("failed to type convert poloniex orderbook update array"))
				return
			}
			if len(updateParams) < 4 {
				poloniex.setWsFail(fmt.Errorf("unexpected number of parameters in poloniex orderboook update"))
				return
			}
			updateType, ok := updateParams[0].(string)
			if !ok {
				poloniex.setWsFail(fmt.Errorf("failed to type convert poloniex orderbook update type"))
				return
			}

			var order *wsOrder
			var direction int
			if updateType == poloniexOrderUpdateKey {
				order, direction, err = processPoloniexOrderbookUpdate(updateParams)
				if err != nil {
					poloniex.setWsFail(err)
				}
			} else if updateType == poloniexTradeUpdateKey {
				continue
				// trade, direction, err = poloniex.processTrade(updateParams)
			}

			switch direction {
			case poloniexAskDirection:
				newAsks[eightPtKey(order.price)] = order
			case poloniexBuyDirection:
				newBids[eightPtKey(order.price)] = order
			default:
				poloniex.setWsFail(fmt.Errorf("Unknown poloniex update direction indicator: %d", direction))
				return
			}
			count++
		}
		poloniex.accumulateOrders(seq, newAsks, newBids)
		if count > 0 {
			poloniex.wsUpdated()
		}
	default:
		poloniex.setWsFail(fmt.Errorf("poloniex websocket message had unexpected length %d", len(msg)))
		return
	}
}

// Create a websocket connection and send the orderbook subscription.
func (poloniex *PoloniexExchange) connectWs() {
	err := poloniex.connectWebsocket(poloniex.processWsMessage, &socketConfig{
		address: PoloniexURLs.Websocket,
	})
	if err != nil {
		log.Errorf("connectWs: %v", err)
		return
	}
	err = poloniex.wsSend(poloniexOrderbookSubscription)
	if err != nil {
		log.Errorf("Failed to send order book sub to polo: %v", err)
	}
}

// Refresh retrieves and parses API data from Poloniex.
func (poloniex *PoloniexExchange) Refresh() {
	poloniex.LogRequest()

	var response map[string]*PoloniexPair
	err := poloniex.fetch(poloniex.requests.price, &response)
	if err != nil {
		poloniex.fail("Fetch", err)
		return
	}
	market, ok := response[poloniex.CurrencyPair]
	if !ok {
		poloniex.fail("Market not in response", fmt.Errorf("Response did not have expected CurrencyPair %s", poloniex.CurrencyPair))
		return
	}
	price, err := strconv.ParseFloat(market.Last, 64)
	if err != nil {
		poloniex.fail(fmt.Sprintf("Failed to parse float from Last=%s", market.Last), err)
		return
	}
	baseVolume, err := strconv.ParseFloat(market.BaseVolume, 64)
	if err != nil {
		poloniex.fail(fmt.Sprintf("Failed to parse float from BaseVolume=%s", market.BaseVolume), err)
		return
	}
	volume, err := strconv.ParseFloat(market.QuoteVolume, 64)
	if err != nil {
		poloniex.fail(fmt.Sprintf("Failed to parse float from QuoteVolume=%s", market.QuoteVolume), err)
		return
	}
	percentChange, err := strconv.ParseFloat(market.PercentChange, 64)
	if err != nil {
		poloniex.fail(fmt.Sprintf("Failed to parse float from PercentChange=%s", market.PercentChange), err)
		return
	}
	oldPrice := price / (1 + percentChange)

	// Check for a depth chart from the websocket orderbook.
	tryHttp, wsStarting, depth := poloniex.wsDepthStatus(poloniex.connectWs)

	// If not expecting depth data from the websocket, grab it from HTTP
	if tryHttp {
		depthResponse := new(PoloniexDepthResponse)
		err = poloniex.fetch(poloniex.requests.depth, depthResponse)
		if err != nil {
			log.Errorf("Poloniex depth chart fetch error: %v", err)
		}
		depth = depthResponse.translate()
	}

	if !wsStarting {
		sinceLast := time.Since(poloniex.wsLastUpdate())
		log.Tracef("last bittrex websocket update %.3f seconds ago", sinceLast.Seconds())
		if sinceLast > depthDataExpiration && !poloniex.wsFailed() {
			poloniex.setWsFail(fmt.Errorf("lost connection detected. bittrex websocket will reconnect during next refresh"))
		}
	}

	// Candlesticks
	state := poloniex.state()

	candlesticks := map[candlestickKey]Candlesticks{}
	for bin, req := range poloniex.requests.candlesticks {
		oldSticks, found := state.Candlesticks[bin]
		if !found || oldSticks.needsUpdate(bin) {
			log.Tracef("Signalling candlestick update for %s, bin size %s", poloniex.token, bin)
			response := new(PoloniexCandlestickResponse)
			err := poloniex.fetch(req, response)
			if err != nil {
				log.Errorf("Error retrieving candlestick data from poloniex for bin size %s: %v", string(bin), err)
				continue
			}

			sticks := response.translate()
			if !found || sticks.time().After(oldSticks.time()) {
				candlesticks[bin] = sticks
			}
		}
	}

	update := &ExchangeState{
		BaseState: BaseState{
			Price:      price,
			BaseVolume: baseVolume,
			Volume:     volume,
			Change:     price - oldPrice,
		},
		Depth:        depth,
		Candlesticks: candlesticks,
	}
	if wsStarting {
		poloniex.SilentUpdate(update)
	} else {
		poloniex.Update(update)
	}
}

// dexDotDecredMsgID is used as an atomic counter for msgjson.Message IDs.
var dexDotDecredMsgID uint64 = 1

// dexSubscription is the DEX request for the order book feed.
var dexSubscription = &msgjson.OrderBookSubscription{
	Base:  42, // BIP44 coin ID for Decred
	Quote: 0,  // Bitcoin
}

// DEXConfig is the configuration for the Decred DEX server.
type DEXConfig struct {
	Token    string
	Host     string
	Cert     []byte
	CertHost string
}

// candleCache embeds *dexdb.CandleCache and adds some fields for internal
// handling.
type candleCache struct {
	*dexdb.CandleCache
	mtx       sync.RWMutex
	lastStamp uint64
	key       candlestickKey
}

// DecredDEX is a Decred DEX.
type DecredDEX struct {
	*CommonExchange
	ords         map[string]*msgjson.BookOrderNote
	reqMtx       sync.Mutex
	reqs         map[uint64]func(*msgjson.Message)
	cacheMtx     sync.RWMutex
	candleCaches map[uint64]*candleCache
	seq          uint64
	stamp        int64
	cfg          *DEXConfig
}

// NewDecredDEXConstructor creates a constructor for a DEX with the provided
// configuration.
func NewDecredDEXConstructor(cfg *DEXConfig) func(*http.Client, *BotChannels) (Exchange, error) {
	return func(client *http.Client, channels *BotChannels) (Exchange, error) {
		dcr := &DecredDEX{
			CommonExchange: newCommonExchange(cfg.Token, client, requests{}, channels),
			candleCaches:   make(map[uint64]*candleCache),
			reqs:           make(map[uint64]func(*msgjson.Message)),
			cfg:            cfg,
		}
		go func() {
			<-channels.done
			ws, _ := dcr.websocket()
			if ws != nil {
				ws.Close()
			}
		}()
		return dcr, nil
	}
}

// Refresh grabs a book snapshot and sends the exchange update.
func (dcr *DecredDEX) Refresh() {
	dcr.LogRequest()
	// Check for a depth chart from the websocket orderbook.
	tryHTTP, wsStarting, depth := dcr.wsDepthStatus(dcr.connectWs)
	if tryHTTP {
		log.Debugf("Failed to get WebSocket depth chart for %s", dcr.cfg.Host)
		return
	}
	if wsStarting {
		// Do nothing in this case. We'll update the bot when we get some data.
		return
	}

	candlesticks := make(map[candlestickKey]Candlesticks)
	var change float64
	var volume, bestVolDur uint64
	var aDayMS uint64 = 86400 * 1000
	// Ugh. I need to export the CandleCache.candles.
	for binSize, cache := range dcr.candles() {
		cache.mtx.RLock()
		wc := cache.WireCandles(dexapi.CacheSize)
		sticks := make(Candlesticks, 0, len(wc.EndStamps))
		for i := range wc.EndStamps {
			sticks = append(sticks, Candlestick{
				High:   float64(wc.HighRates[i]) / 1e8,
				Low:    float64(wc.LowRates[i]) / 1e8,
				Open:   float64(wc.StartRates[i]) / 1e8,
				Close:  float64(wc.EndRates[i]) / 1e8,
				Volume: float64(wc.MatchVolumes[i]) / 1e8,
				Start:  time.Unix(int64(wc.StartStamps[i]/1000), 0),
			})
		}
		cache.mtx.RUnlock()

		candlesticks[cache.key] = sticks
		deepEnough := binSize*dexapi.CacheSize > aDayMS
		if bestVolDur == 0 || (binSize < bestVolDur && deepEnough) {
			bestVolDur = binSize
			change, volume = cache.Delta(time.Now().Add(-time.Hour * 24))
		}
	}

	dcr.Update(&ExchangeState{
		BaseState: BaseState{
			Price:  depth.MidGap(),
			Change: change,
			Volume: float64(volume) / 1e8,
			Stamp:  dcr.lastStamp(),
		},
		Candlesticks: candlesticks,
		Depth:        depth,
	})
}

// candles gets a copy of the candleCaches map.
func (dcr *DecredDEX) candles() map[uint64]*candleCache {
	dcr.cacheMtx.RLock()
	defer dcr.cacheMtx.RUnlock()
	cs := make(map[uint64]*candleCache, len(dcr.candleCaches))
	for binSize, cache := range dcr.candleCaches {
		cs[binSize] = cache
	}
	return cs
}

// clearCandleCache clears the candle cache for the specified bin size.
func (dcr *DecredDEX) clearCandleCache(binSize uint64) {
	dcr.cacheMtx.Lock()
	defer dcr.cacheMtx.Unlock()
	delete(dcr.candleCaches, binSize)
}

// setCandleCache sets the candle cache for the specified bin size.
func (dcr *DecredDEX) setCandleCache(binSize uint64, cache *candleCache) {
	dcr.cacheMtx.Lock()
	defer dcr.cacheMtx.Unlock()
	dcr.candleCaches[binSize] = cache
}

// logRequest stores the response handler for the request ID.
func (dcr *DecredDEX) logRequest(id uint64, handler func(*msgjson.Message)) {
	dcr.reqMtx.Lock()
	defer dcr.reqMtx.Unlock()
	dcr.reqs[id] = handler
}

// responseHandler retrieves and deletes the response handler from the reqs map.
func (dcr *DecredDEX) responseHandler(id uint64) func(*msgjson.Message) {
	dcr.reqMtx.Lock()
	defer dcr.reqMtx.Unlock()
	f := dcr.reqs[id]
	delete(dcr.reqs, id)
	return f
}

// request sends a request, and records the handler for the response.
func (dcr *DecredDEX) request(route string, payload interface{}, handler func(*msgjson.Message)) (uint64, error) {
	msg, _ := msgjson.NewRequest(atomic.AddUint64(&dexDotDecredMsgID, 1), route, payload)
	dcr.logRequest(msg.ID, handler)

	err := dcr.wsSend(msg)
	if err != nil {
		return 0, fmt.Errorf("error sending %s request to %q: %v", route, dcr.cfg.Host, err)
	}
	return msg.ID, nil
}

// Create a websocket connection and send the orderbook subscription.
func (dcr *DecredDEX) connectWs() {
	// Configure TLS.
	if len(dcr.cfg.Cert) == 0 {
		dcr.setWsFail(fmt.Errorf("failed to find certificate for %s", dcr.cfg.CertHost))
		return
	}

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(dcr.cfg.Cert); !ok {
		dcr.setWsFail(fmt.Errorf("invalid certificate"))
		return
	}

	err := dcr.connectWebsocket(dcr.processWsMessage, &socketConfig{
		address: "wss://" + dcr.cfg.Host + "/ws",
		tlsConfig: &tls.Config{
			RootCAs:    pool,
			ServerName: dcr.cfg.CertHost,
		},
	})
	if err != nil {
		dcr.setWsFail(fmt.Errorf("dcr.connectWs: %v", err))
		return
	}

	// Get 'config' to get current bin sizes.
	_, err = dcr.request(msgjson.ConfigRoute, nil, dcr.handleConfigResponse)
	if err != nil {
		dcr.setWsFail(err)
		return
	}

	_, err = dcr.request(msgjson.OrderBookRoute, dexSubscription, dcr.handleSubResponse)
	if err != nil {
		dcr.setWsFail(err)
		return
	}
}

// processWsMessage is DecredDEX's WebsocketProcessor. Handles messages of type
// *msgjson.Message.
func (dcr *DecredDEX) processWsMessage(raw []byte) {
	msg, err := msgjson.DecodeMessage(raw)
	if err != nil {
		dcr.setWsFail(fmt.Errorf("DecodeMessage error: %v", err))
		return
	}

	dcr.orderMtx.Lock()
	defer dcr.orderMtx.Unlock()
	dcr.stamp = time.Now().Unix()
	switch msg.Type {
	case msgjson.Response:
		handler := dcr.responseHandler(msg.ID)
		if handler != nil {
			handler(msg)
		} else {
			log.Warnf("Received response from %q with no request handler registered: %s", dcr.cfg.Host, string(raw))
		}

	case msgjson.Notification:
		switch msg.Route {
		case msgjson.BookOrderRoute:
			bookOrder := new(msgjson.BookOrderNote)
			err := msg.Unmarshal(bookOrder)
			if err != nil {
				dcr.setWsFail(fmt.Errorf("book_order Unmarshal error: %v", err))
				return
			}
			if !dcr.checkSeq(bookOrder.Seq) {
				return
			}
			dcr.bookOrder(bookOrder)
		case msgjson.UnbookOrderRoute:
			unbookOrder := new(msgjson.UnbookOrderNote)
			err := msg.Unmarshal(unbookOrder)
			if err != nil {
				dcr.setWsFail(fmt.Errorf("unbook_order Unmarshal error: %v", err))
				return
			}
			if !dcr.checkSeq(unbookOrder.Seq) {
				return
			}
			dcr.unbookOrder(unbookOrder)
		case msgjson.UpdateRemainingRoute:
			update := new(msgjson.UpdateRemainingNote)
			err := msg.Unmarshal(update)
			if err != nil {
				dcr.setWsFail(fmt.Errorf("update_remaining Unmarshal error: %v", err))
				return
			}
			if !dcr.checkSeq(update.Seq) {
				return
			}
			dcr.updateRemaining(update)
		case msgjson.EpochOrderRoute:
			// We don't actually track epoch orders, but we need to progress the
			// sequence.
			note := new(msgjson.EpochOrderNote)
			err := msg.Unmarshal(note)
			if err != nil {
				dcr.setWsFail(fmt.Errorf("epoch_order Unmarshal error: %v", err))
				return
			}
			dcr.checkSeq(note.Seq)
			return // Skip wsUpdate. Nothing has changed.
		case msgjson.SuspensionRoute:
			note := new(msgjson.TradeSuspension)
			err := msg.Unmarshal(note)
			if err != nil {
				dcr.setWsFail(fmt.Errorf("suspension Unmarshal error: %v", err))
				return
			}
			if note.Persist {
				return
			}
			dcr.checkSeq(note.Seq)
			dcr.clearOrderBook()
		case msgjson.EpochReportRoute:
			note := new(msgjson.EpochReportNote)
			err := msg.Unmarshal(note)
			if err != nil {
				dcr.setWsFail(fmt.Errorf("epoch_report Unmarshal error: %v", err))
				return
			}
			// EpochReportNote.Candle is a value, not a pointer.
			if note.Candle.EndStamp == 0 {
				return
			}
			candle := convertDEXCandle(&note.Candle)
			for binSize, cache := range dcr.candles() {
				cache.mtx.Lock()
				if cache.lastStamp == note.StartStamp {
					cache.Add(candle)
					cache.lastStamp = candle.EndStamp
					cache.mtx.Unlock()
				} else {
					// Our candles are out of sync. Get a fresh set.
					log.Infof("Epoch report out of sync (last stamp %d, note start stamp %d). Requesting new candles.",
						cache.lastStamp, note.StartStamp)
					cacheKey := cache.key
					cache.mtx.Unlock()
					dcr.clearCandleCache(binSize)
					_, err := dcr.request(msgjson.CandlesRoute, &msgjson.CandlesRequest{
						BaseID:     42,
						QuoteID:    0,
						BinSize:    (time.Duration(binSize) * time.Millisecond).String(),
						NumCandles: dexapi.CacheSize,
					}, func(msg *msgjson.Message) {
						dcr.handleCandles(cacheKey, msg)
					})
					if err != nil {
						dcr.setWsFail(fmt.Errorf("error requesting candles for bin size %d: %w", binSize, err))
						break
					}
				}
			}
		}
	}
	dcr.wsUpdated()
}

// convertDEXCandle converts the *msgjson.Candle to a dexdb.Candle.
func convertDEXCandle(c *msgjson.Candle) *dexdb.Candle {
	dexDBCandle := dexdb.Candle(*c)
	return &dexDBCandle
}

// handleSubResponse handles the response to the order book subscription.
func (dcr *DecredDEX) handleSubResponse(msg *msgjson.Message) {
	ob := new(msgjson.OrderBook)
	err := msg.UnmarshalResult(ob)
	if err != nil {
		dcr.setWsFail(fmt.Errorf("error unmarshaling orderbook response: %v", err))
		return
	}
	dcr.setOrderBook(ob)
}

// handleCandles handles the response for a set of candles from the data API.
func (dcr *DecredDEX) handleCandles(key candlestickKey, msg *msgjson.Message) {
	wireCandles := new(msgjson.WireCandles)
	err := msg.UnmarshalResult(wireCandles)
	if err != nil {
		log.Errorf("error encountered in candlestick response from DEX at %s: %v", dcr.cfg.Host, err)
		return
	}

	binSize := uint64(key.duration().Milliseconds())

	candles := wireCandles.Candles()

	cache := &candleCache{
		CandleCache: dexdb.NewCandleCache(len(candles), binSize),
		key:         key,
	}

	for _, candle := range candles {
		cache.Add(convertDEXCandle(candle))
	}
	if len(candles) > 0 {
		cache.lastStamp = candles[len(candles)-1].EndStamp
	}
	dcr.setCandleCache(binSize, cache)
}

// handleConfigResponse handles the response for the DEX configuration.
func (dcr *DecredDEX) handleConfigResponse(msg *msgjson.Message) {
	cfg := new(msgjson.ConfigResult)
	err := msg.UnmarshalResult(cfg)
	if err != nil {
		dcr.setWsFail(fmt.Errorf("error unmarshaling config response: %v", err))
		return
	}
	// If the server is not of sufficient version to support the data API,
	// BinSizes will be nil and we won't create any candle caches.
	for _, durStr := range cfg.BinSizes {
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			dcr.setWsFail(fmt.Errorf("unparseable bin size in dcrdex config response: %q: %v", durStr, err))
			return
		}

		var key candlestickKey
		for k, d := range candlestickDurations {
			if d == dur {
				key = k
				break
			}
		}
		if key == "" {
			log.Debugf("Skipping unknown candlestick duration %q", durStr)
			continue
		}

		_, err = dcr.request(msgjson.CandlesRoute, &msgjson.CandlesRequest{
			BaseID:     42,
			QuoteID:    0,
			BinSize:    durStr,
			NumCandles: dexapi.CacheSize,
		}, func(msg *msgjson.Message) {
			dcr.handleCandles(key, msg)
		})
		if err != nil {
			dcr.setWsFail(fmt.Errorf("error requesting candles for bin size %s: %w", durStr, err))
			return
		}
	}
}

// checkSeq verifies that the seq is sequential, and increments the seq counter.
// checkSeq should only be called with the orderMtx write-locked.
func (dcr *DecredDEX) checkSeq(seq uint64) bool {
	if seq != dcr.seq+1 {
		dcr.setWsFail(fmt.Errorf("incorrect sequence. wanted %d, got %d", dcr.seq+1, seq))
		return false
	}
	dcr.seq = seq
	return true
}

// clearOrderBook clears the order book. clearOrderBook should only be called
// with the orderMtx write-locked.
func (dcr *DecredDEX) clearOrderBook() {
	dcr.buys = make(wsOrders)
	dcr.asks = make(wsOrders)
	dcr.ords = make(map[string]*msgjson.BookOrderNote)
}

// lastStamp is the unix timestamp of the received response or notification.
func (dcr *DecredDEX) lastStamp() int64 {
	dcr.orderMtx.RLock()
	defer dcr.orderMtx.RUnlock()
	return dcr.stamp
}

// setOrderBook processes the order book data from 'orderbook' request.
// setOrderBook should only be called with the orderMtx write-locked.
func (dcr *DecredDEX) setOrderBook(ob *msgjson.OrderBook) {
	dcr.clearOrderBook()
	dcr.seq = ob.Seq
	addToSide := func(side wsOrders, ord *msgjson.BookOrderNote) {
		bucket := side.order(int64(ord.Rate), float64(ord.Rate)/1e8)
		bucket.volume += float64(ord.Quantity) / 1e8
		dcr.ords[ord.OrderID.String()] = ord
	}

	for _, ord := range ob.Orders {
		if ord == nil {
			dcr.setWsFail(fmt.Errorf("nil order encountered"))
			return
		}
		if ord.Side == msgjson.BuyOrderNum {
			addToSide(dcr.buys, ord)
		} else {
			addToSide(dcr.asks, ord)
		}
	}
	dcr.wsInitialized()

	depth := dcr.wsDepthSnapshot()

	dcr.Update(&ExchangeState{
		BaseState: BaseState{
			Price: depth.MidGap(),
			// Change:       priceChange, // With candlesticks
			Stamp: dcr.stamp,
		},
		// Candlesticks: candlesticks, // Not yet
		Depth: depth,
	})
}

// bookOrder processes the 'book_order' notification.
// bookOrder should only be called with the orderMtx write-locked.
func (dcr *DecredDEX) bookOrder(ord *msgjson.BookOrderNote) {
	side := dcr.asks
	if ord.Side == msgjson.BuyOrderNum {
		side = dcr.buys
	}
	bucket := side.order(int64(ord.Rate), float64(ord.Rate)/1e8)
	bucket.volume += float64(ord.Quantity) / 1e8
	dcr.ords[ord.OrderID.String()] = ord
}

// unbookOrder processes the 'unbook_order' notification.
// unbookOrder should only be called with the orderMtx write-locked.
func (dcr *DecredDEX) unbookOrder(note *msgjson.UnbookOrderNote) {
	if len(note.OrderID) == 0 {
		dcr.setWsFail(fmt.Errorf("received unbook_order notification without an order ID"))
		return
	}
	oid := note.OrderID.String()
	ord := dcr.ords[oid]
	if ord == nil {
		dcr.setWsFail(fmt.Errorf("no order found to unbook"))
		return
	}
	delete(dcr.ords, oid)
	side := dcr.asks
	if ord.Side == msgjson.BuyOrderNum {
		side = dcr.buys
	}
	rateKey := int64(ord.Rate)
	bucket := side.order(rateKey, float64(ord.Rate)/1e8)
	bucket.volume -= float64(ord.Quantity) / 1e8
	if bucket.volume < 1e-8 { // Account for floating point imprecision.
		delete(side, rateKey)
	}
}

// updateRemaining processes the 'update_remaining' notification.
// updateRemaining should only be called with the orderMtx write-locked.
func (dcr *DecredDEX) updateRemaining(update *msgjson.UpdateRemainingNote) {
	if len(update.OrderID) == 0 {
		dcr.setWsFail(fmt.Errorf("received update_remaining notification without an order ID"))
		return
	}
	oid := update.OrderID.String()
	ord := dcr.ords[oid]
	if ord == nil {
		dcr.setWsFail(fmt.Errorf("order %s from dex.decred.org was not in our book", oid))
		return
	}

	diff := ord.Quantity - update.Remaining
	ord.Quantity = update.Remaining
	side := dcr.asks
	if ord.Side == msgjson.BuyOrderNum {
		side = dcr.buys
	}
	rateKey := int64(ord.Rate)
	bucket := side.order(rateKey, float64(ord.Rate)/1e8)
	bucket.volume -= float64(diff) / 1e8
	if bucket.volume < 1e-8 {
		delete(side, rateKey)
	}
}
