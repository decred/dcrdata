// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	dexcandles "decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/msgjson"
	dcrrates "github.com/decred/dcrdata/exchanges/v3/ratesproto"
)

// Tokens. Used to identify the exchange.
const (
	Coinbase     = "coinbase"
	Binance      = "binance"
	DexDotDecred = "dcrdex"
	Mexc         = "mexc"
)

// A few candlestick bin sizes.
type candlestickKey string

const (
	halfHourKey candlestickKey = "30m"
	hourKey     candlestickKey = "1h"
	dayKey      candlestickKey = "1d"
	monthKey    candlestickKey = "1mo"
)

var candlestickDurations = map[candlestickKey]time.Duration{
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

// CurrencyPair is any currency pair, e.g DCR-{Asset} or currency index, e.g
// BTC-Index, USDT-Index.
type CurrencyPair string

const (
	CurrencyPairDCRBTC  CurrencyPair = "DCR-BTC"
	CurrencyPairDCRUSDT CurrencyPair = "DCR-USDT"

	// BTCIndex is an index pair and not a valid DCR-{Asset} market.
	BTCIndex  CurrencyPair = "BTC-Index"
	USDTIndex CurrencyPair = "USDT-Index"
)

func (cp CurrencyPair) IsValidDCRPair() bool {
	return cp == CurrencyPairDCRBTC || cp == CurrencyPairDCRUSDT
}

func (cp CurrencyPair) IsValidIndex() bool {
	return cp == BTCIndex || cp == USDTIndex
}

func (cp CurrencyPair) QuoteAsset() string {
	if !cp.IsValidDCRPair() {
		return string(cp)
	}

	v := strings.Split(string(cp), "-")
	return strings.ToTitle(v[1])
}

// URLs is a set of endpoints for an exchange's various datasets.
type URLs struct {
	Markets      []CurrencyPair
	Price        map[CurrencyPair]string
	Stats        map[CurrencyPair]string
	Depth        map[CurrencyPair]string
	Candlesticks map[CurrencyPair]map[candlestickKey]string
	Websocket    string
}

type requests struct {
	price        *http.Request
	stats        *http.Request //nolint
	depth        *http.Request
	candlesticks map[candlestickKey]*http.Request
}

func newRequests(markets []CurrencyPair) map[CurrencyPair]*requests {
	reqs := make(map[CurrencyPair]*requests, len(markets))
	for _, mkt := range markets {
		reqs[mkt] = &requests{
			candlesticks: make(map[candlestickKey]*http.Request),
		}
	}
	return reqs
}

// Prepare the URLs.
var (
	CoinbaseURLs = URLs{
		Markets: []CurrencyPair{BTCIndex, USDTIndex},
		Price: map[CurrencyPair]string{
			BTCIndex:  "https://api.coinbase.com/v2/exchange-rates?currency=BTC",
			USDTIndex: "https://api.coinbase.com/v2/exchange-rates?currency=USDT",
		},
	}
	// https://api.mexc.com/api/v3/depth?symbol=DCRUSDT
	MexcURLs = URLs{
		Markets: []CurrencyPair{CurrencyPairDCRUSDT},
		Price: map[CurrencyPair]string{
			CurrencyPairDCRUSDT: "https://api.mexc.com/api/v3/ticker/24hr?symbol=DCRUSDT",
		},
		Depth: map[CurrencyPair]string{
			// Mexc returns a maximum of 5000 depth chart points. This seems
			// like it is the entire order book at least sometimes.
			CurrencyPairDCRUSDT: "https://api.mexc.com/api/v3/depth?symbol=DCRUSDT&limit=5000",
		},
		Candlesticks: map[CurrencyPair]map[candlestickKey]string{
			CurrencyPairDCRUSDT: {
				// 1000 is the maximum sticks returned.
				hourKey:  "https://api.mexc.com/api/v3/klines?symbol=DCRUSDT&limit=1000&interval=60m",
				dayKey:   "https://api.mexc.com/api/v3/klines?symbol=DCRUSDT&limit=1000&interval=1d",
				monthKey: "https://api.mexc.com/api/v3/klines?symbol=DCRUSDT&limit=1000&interval=1M",
			},
		},
	}
	BinanceURLs = URLs{
		Markets: []CurrencyPair{CurrencyPairDCRUSDT},
		Price: map[CurrencyPair]string{
			CurrencyPairDCRUSDT: "https://api.binance.com/api/v3/ticker/24hr?symbol=DCRUSDT",
		},
		Depth: map[CurrencyPair]string{
			// Binance returns a maximum of 5000 depth chart points. This seems
			// like it is the entire order book at least sometimes.
			CurrencyPairDCRUSDT: "https://api.binance.com/api/v3/depth?symbol=DCRUSDT&limit=5000",
		},
		Candlesticks: map[CurrencyPair]map[candlestickKey]string{
			CurrencyPairDCRUSDT: {
				hourKey:  "https://api.binance.com/api/v3/klines?symbol=DCRUSDT&interval=1h",
				dayKey:   "https://api.binance.com/api/v3/klines?symbol=DCRUSDT&interval=1d",
				monthKey: "https://api.binance.com/api/v3/klines?symbol=DCRUSDT&interval=1M",
			},
		},
	}
)

// Indices maps tokens to constructors for {BTC, USDT}-fiat exchanges.
var Indices = map[string]func(*http.Client, *BotChannels) (Exchange, error){
	Coinbase: NewCoinbase,
}

// DcrExchanges maps tokens to constructors for DCR-{Asset} exchanges.
var DcrExchanges = map[string]func(*http.Client, *BotChannels) (Exchange, error){
	Binance: NewBinance,
	DexDotDecred: NewDecredDEXConstructor(&DEXConfig{
		Token:    DexDotDecred,
		Host:     "dex.decred.org:7232",
		Cert:     core.CertStore[dex.Mainnet]["dex.decred.org:7232"],
		CertHost: "dex.decred.org",
	}),
	Mexc: NewMexc,
}

// IsIndex checks whether the given token is a known {Bitcoin, USDT} index, as
// opposed to a Decred-to-{Bitcoin, USDT} Exchange.
func IsIndex(token string) bool {
	_, ok := Indices[token]
	return ok
}

// IsDcrExchange checks whether the given token is a known Decred-{Asset} exchange.
func IsDcrExchange(token string) bool {
	_, ok := DcrExchanges[token]
	return ok
}

// Tokens is a new slice of available exchange tokens.
func Tokens() []string {
	tokens := make([]string, 0, len(Indices)+len(DcrExchanges))
	var token string
	for token = range Indices {
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
	// BaseVolume is poorly named. This is the volume in terms of (usually) BTC
	// or USDT, not the base asset of any particular market.
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
func exchangeStateFromProto(proto *dcrrates.ExchangeRateUpdate) (CurrencyPair, *ExchangeState) {
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

	return CurrencyPair(proto.CurrencyPair), state
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
	CurrencyPair
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
	Update(CurrencyPair, *ExchangeState)
	SilentUpdate(CurrencyPair, *ExchangeState) // skip passing update to the update channel
	UpdateIndices(CurrencyPair, FiatIndices)
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
	currentState map[CurrencyPair]*ExchangeState
	client       Doer
	lastUpdate   time.Time
	lastFail     time.Time
	lastRequest  time.Time
	requests     map[CurrencyPair]*requests
	channels     *BotChannels
	wsMtx        sync.RWMutex
	ws           websocketFeed
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
	// CommonExchange. These fields are only for the BTC_DCR market.
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
func (xc *CommonExchange) Update(market CurrencyPair, state *ExchangeState) {
	xc.update(market, state, true)
}

// SilentUpdate stores the update for internal use, but does not signal an
// update to the ExchangeBot.
func (xc *CommonExchange) SilentUpdate(market CurrencyPair, state *ExchangeState) {
	xc.update(market, state, false)
}

func (xc *CommonExchange) update(market CurrencyPair, state *ExchangeState, send bool) {
	xc.mtx.Lock()
	defer xc.mtx.Unlock()
	xc.lastUpdate = time.Now()
	currentState := xc.currentState[market]
	if currentState != nil {
		state.stealSticks(currentState)
	}
	xc.currentState[market] = state
	if !send {
		return
	}
	xc.channels.exchange <- &ExchangeUpdate{
		CurrencyPair: market,
		Token:        xc.token,
		State:        state,
	}
}

// UpdateIndices sends a bitcoin index update to the ExchangeBot.
func (xc *CommonExchange) UpdateIndices(index CurrencyPair, indices FiatIndices) {
	xc.mtx.Lock()
	defer xc.mtx.Unlock()
	xc.lastUpdate = time.Now()
	xc.channels.index <- &IndexUpdate{
		Token:        xc.token,
		CurrencyPair: index,
		Indices:      indices,
	}
}

// Send the exchange request and decode the response.
func (xc *CommonExchange) fetch(request *http.Request, response interface{}) (err error) {
	resp, err := xc.client.Do(request)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("Request failed: %v", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed: server returned %v", resp.StatusCode)
	}
	err = json.NewDecoder(resp.Body).Decode(response)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("Failed to decode json from %s: %v", request.URL.String(), err))
	}
	return
}

// A thread-safe getter for the last known ExchangeState for supported markets.
func (xc *CommonExchange) state(market CurrencyPair) *ExchangeState {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.currentState[market]
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
	if ws == nil {
		// TODO: figure out why we are sending in this state
		return errors.New("no connection")
	}
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
	reqs map[CurrencyPair]*requests, channels *BotChannels) *CommonExchange {
	currentState := make(map[CurrencyPair]*ExchangeState, len(reqs))
	for mkt := range reqs {
		currentState[mkt] = new(ExchangeState)
	}

	var tZero time.Time
	return &CommonExchange{
		token:        token,
		client:       client,
		channels:     channels,
		currentState: currentState,
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
	reqs := newRequests(CoinbaseURLs.Markets)
	for mkt, price := range CoinbaseURLs.Price {
		reqs[mkt].price, err = http.NewRequest(http.MethodGet, price, nil)
		if err != nil {
			return
		}
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
	for mkt, reqs := range coinbase.requests {
		coinbase.refresh(mkt, reqs)
	}
}

func (coinbase *CoinbaseExchange) refresh(mkt CurrencyPair, requests *requests) {
	response := new(CoinbaseResponse)
	err := coinbase.fetch(requests.price, response)
	if err != nil {
		coinbase.fail(fmt.Sprintf("%s: Fetch", mkt), err)
		return
	}

	indices := make(FiatIndices)
	for code, floatStr := range response.Data.Rates {
		price, err := strconv.ParseFloat(floatStr, 64)
		if err != nil {
			coinbase.fail(fmt.Sprintf("%s: Failed to parse float for index %s. Given %s", mkt, code, floatStr), err)
			continue
		}
		indices[code] = price
	}
	coinbase.UpdateIndices(mkt, indices)
}

// BinanceExchange is a high-volume and well-respected crypto exchange.
type BinanceExchange struct {
	*CommonExchange
}

// NewBinance constructs a BinanceExchange.
func NewBinance(client *http.Client, channels *BotChannels) (binance Exchange, err error) {
	reqs := newRequests(BinanceURLs.Markets)
	for mkt, price := range BinanceURLs.Price {
		reqs[mkt].price, err = http.NewRequest(http.MethodGet, price, nil)
		if err != nil {
			return
		}
	}

	for mkt, depth := range BinanceURLs.Depth {
		reqs[mkt].depth, err = http.NewRequest(http.MethodGet, depth, nil)
		if err != nil {
			return
		}
	}

	for mkt, candlesticks := range BinanceURLs.Candlesticks {
		for dur, url := range candlesticks {
			reqs[mkt].candlesticks[dur], err = http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return
			}
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

// CandlestickResponse models candlestick data returned from the Mexc and Binance
// API. The candlestick response has mixed-type arrays, so type-checking is
// appropriate. Sample response is [
//
//	[
//	  1499040000000,      // Open time
//	  "0.01634790",       // Open
//	  "0.80000000",       // High
//	  "0.01575800",       // Low
//	  "0.01577100",       // Close
//	  "148976.11427815",  // Volume
//	  1640804940000,      // Close Time (Mexc Only)
//	  "168387.3"          // Quote Asset Volume (Mexc Only)
//	]
//
// ]
type CandlestickResponse [][]interface{}

func badStickElement(key string, element interface{}) Candlesticks {
	log.Errorf("Unable to decode %s from candlestick: %T: %v", key, element, element)
	return Candlesticks{}
}

func (r CandlestickResponse) translate() Candlesticks {
	sticks := make(Candlesticks, 0, len(r))
	for _, rawStick := range r {
		if len(rawStick) < 6 {
			log.Error("Unable to decode candlestick response. Not enough elements.")
			return Candlesticks{}
		}
		unixMsFlt, ok := rawStick[0].(float64)
		if !ok {
			return badStickElement("start time", rawStick[0])
		}
		startTime := time.Unix(int64(unixMsFlt/1e3), 0)

		openStr, ok := rawStick[1].(string)
		if !ok {
			return badStickElement("open", rawStick[1])
		}
		open, err := strconv.ParseFloat(openStr, 64)
		if err != nil {
			return badStickElement("open float", err)
		}

		highStr, ok := rawStick[2].(string)
		if !ok {
			return badStickElement("high", rawStick[2])
		}
		high, err := strconv.ParseFloat(highStr, 64)
		if err != nil {
			return badStickElement("high float", err)
		}

		lowStr, ok := rawStick[3].(string)
		if !ok {
			return badStickElement("low", rawStick[3])
		}
		low, err := strconv.ParseFloat(lowStr, 64)
		if err != nil {
			return badStickElement("low float", err)
		}

		closeStr, ok := rawStick[4].(string)
		if !ok {
			return badStickElement("close", rawStick[4])
		}
		close, err := strconv.ParseFloat(closeStr, 64)
		if err != nil {
			return badStickElement("close float", err)
		}

		volumeStr, ok := rawStick[5].(string)
		if !ok {
			return badStickElement("volume", rawStick[5])
		}
		volume, err := strconv.ParseFloat(volumeStr, 64)
		if err != nil {
			return badStickElement("volume float", err)
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

func parseDepthPoints(pts [][2]string) ([]DepthPoint, error) {
	outPts := make([]DepthPoint, 0, len(pts))
	for _, pt := range pts {
		price, err := strconv.ParseFloat(pt[0], 64)
		if err != nil {
			return outPts, fmt.Errorf("Unable to parse depth point price: %v", err)
		}

		quantity, err := strconv.ParseFloat(pt[1], 64)
		if err != nil {
			return outPts, fmt.Errorf("Unable to parse depth point quantity: %v", err)
		}

		outPts = append(outPts, DepthPoint{
			Quantity: quantity,
			Price:    price,
		})
	}
	return outPts, nil
}

func translateDepthPoints(xc string, asks [][2]string, bids [][2]string) *DepthData {
	depth := new(DepthData)
	depth.Time = time.Now().Unix()
	var err error
	depth.Asks, err = parseDepthPoints(asks)
	if err != nil {
		log.Errorf("%s: %v", xc, err)
		return nil
	}
	depth.Bids, err = parseDepthPoints(bids)
	if err != nil {
		log.Errorf("%s: %v", xc, err)
		return nil
	}
	return depth
}

// Refresh retrieves and parses API data from Binance.
func (binance *BinanceExchange) Refresh() {
	binance.LogRequest()
	for mkt, requests := range binance.requests {
		binance.refresh(mkt, requests)
	}
}

func (binance *BinanceExchange) refresh(mkt CurrencyPair, requests *requests) {
	priceResponse := new(BinancePriceResponse)
	err := binance.fetch(requests.price, priceResponse)
	if err != nil {
		binance.fail(fmt.Sprintf("%s: Fetch price", mkt), err)
		return
	}
	price, err := strconv.ParseFloat(priceResponse.LastPrice, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("%s: Failed to parse float from LastPrice=%s", mkt, priceResponse.LastPrice), err)
		return
	}
	baseVolume, err := strconv.ParseFloat(priceResponse.QuoteVolume, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("%s: Failed to parse float from QuoteVolume=%s", mkt, priceResponse.QuoteVolume), err)
		return
	}

	dcrVolume, err := strconv.ParseFloat(priceResponse.Volume, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("%s: Failed to parse float from Volume=%s", mkt, priceResponse.Volume), err)
		return
	}
	priceChange, err := strconv.ParseFloat(priceResponse.PriceChange, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("%s: Failed to parse float from PriceChange=%s", mkt, priceResponse.PriceChange), err)
		return
	}

	// Get the depth chart
	depthResponse := new(BinanceDepthResponse)
	err = binance.fetch(requests.depth, depthResponse)
	if err != nil {
		log.Errorf("Error retrieving depth chart data from Binance(%s): %v", mkt, err)
	}
	depth := translateDepthPoints(Binance, depthResponse.Asks, depthResponse.Bids)

	// Grab the current state to check if candlesticks need updating
	state := binance.state(mkt)

	candlesticks := map[candlestickKey]Candlesticks{}
	for bin, req := range requests.candlesticks {
		oldSticks, found := state.Candlesticks[bin]
		if !found || oldSticks.needsUpdate(bin) {
			log.Tracef("Signalling candlestick update for %s, market %s, bin size %s", binance.token, mkt, bin)
			response := new(CandlestickResponse)
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

	binance.Update(mkt, &ExchangeState{
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

// candleCache embeds *candles.Cache and adds some fields for internal
// handling.
type candleCache struct {
	*dexcandles.Cache
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
	lastRate     float64
	seq          uint64
	stamp        int64
	cfg          *DEXConfig
}

// NewDecredDEXConstructor creates a constructor for a DEX with the provided
// configuration.
func NewDecredDEXConstructor(cfg *DEXConfig) func(*http.Client, *BotChannels) (Exchange, error) {
	return func(client *http.Client, channels *BotChannels) (Exchange, error) {
		dcr := &DecredDEX{
			CommonExchange: newCommonExchange(cfg.Token, client, make(map[CurrencyPair]*requests), channels),
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
		wc := cache.WireCandles(dexcandles.CacheSize)
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
		deepEnough := binSize*dexcandles.CacheSize > aDayMS
		if bestVolDur == 0 || (binSize < bestVolDur && deepEnough) {
			bestVolDur = binSize
			change, volume, _, _ = cache.Delta(time.Now().Add(-time.Hour * 24))
		}
	}

	if dcr.lastRate == 0 {
		return // no rate, nothing to do.
	}

	dcr.Update(CurrencyPairDCRBTC, &ExchangeState{
		BaseState: BaseState{
			Price:  dcr.lastRate,
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

			dcr.lastRate = float64(note.Candle.EndRate) / 1e8

			candle := &note.Candle
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
						NumCandles: dexcandles.CacheSize,
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
		Cache: dexcandles.NewCache(len(candles), binSize),
		key:   key,
	}

	for _, candle := range candles {
		cache.Add(candle)
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
			NumCandles: dexcandles.CacheSize,
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

	if dcr.lastRate == 0 {
		if len(ob.Orders) == 0 {
			return // don't send rate update if we don't have a valid rate and there are no orders to get a sane midGap.
		}
		// Use mid gap as a sane default if the orderbook is not empty.
		dcr.lastRate = depth.MidGap()
	}

	dcr.Update(CurrencyPairDCRBTC, &ExchangeState{
		BaseState: BaseState{
			Price: dcr.lastRate,
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

// MexcExchange is a high-volume and well-respected crypto exchange.
type MexcExchange struct {
	*CommonExchange
}

// NewMexc constructs a *MexcExchange.
func NewMexc(client *http.Client, channels *BotChannels) (mexc Exchange, err error) {
	reqs := newRequests(MexcURLs.Markets)
	for mkt, price := range MexcURLs.Price {
		reqs[mkt].price, err = http.NewRequest(http.MethodGet, price, nil)
		if err != nil {
			return
		}
	}

	for mkt, depth := range MexcURLs.Depth {
		reqs[mkt].depth, err = http.NewRequest(http.MethodGet, depth, nil)
		if err != nil {
			return
		}
	}

	for mkt, candlesticks := range MexcURLs.Candlesticks {
		for dur, url := range candlesticks {
			reqs[mkt].candlesticks[dur], err = http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return
			}
		}
	}

	mexc = &MexcExchange{
		CommonExchange: newCommonExchange(Mexc, client, reqs, channels),
	}
	return
}

// MexcPriceResponse models the JSON price data returned from the Mexc API.
type MexcPriceResponse struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	PrevClosePrice     string `json:"prevClosePrice"`
	LastPrice          string `json:"lastPrice"`
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
}

// MexcDepthResponse models the response for Mexc depth chart data.
type MexcDepthResponse struct {
	UpdateID int64 `json:"lastUpdateId"`
	Bids     [][2]string
	Asks     [][2]string
}

// Refresh retrieves and parses API data from Mexc Exchange.
func (mexc *MexcExchange) Refresh() {
	mexc.LogRequest()
	for currencyPair, requests := range mexc.requests {
		mexc.refresh(currencyPair, requests)
	}
}

func (mexc *MexcExchange) refresh(pair CurrencyPair, requests *requests) {
	priceResponse := new(MexcPriceResponse)
	err := mexc.fetch(requests.price, priceResponse)
	if err != nil {
		mexc.fail(fmt.Sprintf("%s: Fetch price", pair), err)
		return
	}
	price, err := strconv.ParseFloat(priceResponse.LastPrice, 64)
	if err != nil {
		mexc.fail(fmt.Sprintf("%s: Failed to parse float from LastPrice=%s", pair, priceResponse.LastPrice), err)
		return
	}
	baseVolume, err := strconv.ParseFloat(priceResponse.QuoteVolume, 64)
	if err != nil {
		mexc.fail(fmt.Sprintf("%s: Failed to parse float from QuoteVolume=%s", pair, priceResponse.QuoteVolume), err)
		return
	}

	dcrVolume, err := strconv.ParseFloat(priceResponse.Volume, 64)
	if err != nil {
		mexc.fail(fmt.Sprintf("%s: Failed to parse float from Volume=%s", pair, priceResponse.Volume), err)
		return
	}
	priceChange, err := strconv.ParseFloat(priceResponse.PriceChange, 64)
	if err != nil {
		mexc.fail(fmt.Sprintf("%s: Failed to parse float from PriceChange=%s", pair, priceResponse.PriceChange), err)
		return
	}

	// Get the depth chart
	depthResponse := new(MexcDepthResponse)
	err = mexc.fetch(requests.depth, depthResponse)
	if err != nil {
		log.Errorf("Error retrieving depth chart data from Mexc(%s): %v", pair, err)
	}
	depth := translateDepthPoints(Mexc, depthResponse.Asks, depthResponse.Bids)

	// Grab the current state to check if candlesticks need updating
	state := mexc.state(pair)

	candlesticks := map[candlestickKey]Candlesticks{}
	for bin, req := range requests.candlesticks {
		oldSticks, found := state.Candlesticks[bin]
		if !found || oldSticks.needsUpdate(bin) {
			log.Tracef("Signalling candlestick update for %s, market %s, bin size %s", mexc.token, pair, bin)
			response := new(CandlestickResponse)
			err := mexc.fetch(req, response)
			if err != nil {
				log.Errorf("Error retrieving candlestick data from mexc for bin size %s: %v", string(bin), err)
				continue
			}
			sticks := response.translate()

			if !found || sticks.time().After(oldSticks.time()) {
				candlesticks[bin] = sticks
			}
		}
	}

	mexc.Update(pair, &ExchangeState{
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
