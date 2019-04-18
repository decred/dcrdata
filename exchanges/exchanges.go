// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrdata/dcrrates"
)

// Tokens. Used to identify the exchange.
const (
	Coinbase = "coinbase"
	Coindesk = "coindesk"
	Binance  = "binance"
	Bittrex  = "bittrex"
	DragonEx = "dragonex"
	Huobi    = "huobi"
	Poloniex = "poloniex"
)

// A few candlestick bin sizes.
type candlestickKey string

const (
	halfHourKey candlestickKey = "30m" // Poloniex doesn't have hourly, but does have half-hourly
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

// URLs is a set of endpoints for an exchange's various datasets.
type URLs struct {
	Price        string
	Depth        string
	Candlesticks map[candlestickKey]string
}

type requests struct {
	price        *http.Request
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
		Price: "https://bittrex.com/api/v1.1/public/getmarketsummary?market=btc-dcr",
		// Bittrex gives no documentation on the amount of data that will be
		// returned, and has no parameters to get more data.
		Depth: "https://bittrex.com/api/v1.1/public/getorderbook?market=btc-dcr&type=both",
		// Kline data is not in official docs. Found in development API v2.0
		// Unofficial docs: https://github.com/thebotguys/golang-bittrex-api/wiki/Bittrex-API-Reference-(Unofficial)
		// When v3 goes public, candlesticks will be /markets/{marketName}/candles
		Candlesticks: map[candlestickKey]string{
			hourKey: "https://bittrex.com/api/v2.0/pub/market/GetTicks?marketName=BTC-DCR&tickInterval=hour",
			dayKey:  "https://bittrex.com/api/v2.0/pub/market/GetTicks?marketName=BTC-DCR&tickInterval=day",
		},
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

// ExchangeState is the simple template for a price. The only member that is
// guaranteed is a price. For Decred exchanges, the volumes will also be
// populated.
type ExchangeState struct {
	Price        float64                         `json:"price"`
	BaseVolume   float64                         `json:"base_volume,omitempty"`
	Volume       float64                         `json:"volume,omitempty"`
	Change       float64                         `json:"change,omitempty"`
	Stamp        int64                           `json:"timestamp,omitempty"`
	Depth        *DepthData                      `json:"depth,omitempty"`
	Candlesticks map[candlestickKey]Candlesticks `json:"candlesticks,omitempty"`
}

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
		Price:      proto.GetPrice(),
		BaseVolume: proto.GetBaseVolume(),
		Volume:     proto.GetVolume(),
		Change:     proto.GetChange(),
		Stamp:      proto.GetStamp(),
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
	UpdateIndices(FiatIndices)
}

// CommonExchange is embedded in all of the exchange types and handles some
// state tracking and token handling for ExchangeBot communications. The
// http.Request must be created individually for each exchange.
type CommonExchange struct {
	mtx          sync.RWMutex
	token        string
	URL          string
	currentState *ExchangeState
	client       *http.Client
	lastUpdate   time.Time
	lastFail     time.Time
	lastRequest  time.Time
	requests     requests
	channels     *BotChannels
}

// LastUpdate gets a time.Time of the last successful exchange update.
func (xc *CommonExchange) LastUpdate() time.Time {
	xc.mtx.RLock()
	defer xc.mtx.RUnlock()
	return xc.lastUpdate
}

// Hurry can be used to subtract some amount of time from the lastUpate
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
	xc.mtx.Lock()
	defer xc.mtx.Unlock()
	xc.lastUpdate = time.Now()
	state.stealSticks(xc.currentState)
	xc.currentState = state
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
		Price:        price,
		BaseVolume:   baseVolume,
		Volume:       dcrVolume,
		Change:       priceChange,
		Stamp:        priceResponse.CloseTime / 1000,
		Candlesticks: candlesticks,
		Depth:        depth,
	})
}

// BittrexExchange is an unregulated U.S. crypto exchange with good volume.
type BittrexExchange struct {
	*CommonExchange
	MarketName string
}

// NewBittrex constructs a BittrexExchange.
func NewBittrex(client *http.Client, channels *BotChannels) (bittrex Exchange, err error) {
	reqs := newRequests()
	reqs.price, err = http.NewRequest(http.MethodGet, BittrexURLs.Price, nil)
	if err != nil {
		return
	}

	reqs.depth, err = http.NewRequest(http.MethodGet, BittrexURLs.Depth, nil)
	if err != nil {
		return
	}

	for dur, url := range BittrexURLs.Candlesticks {
		reqs.candlesticks[dur], err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return
		}
	}

	bittrex = &BittrexExchange{
		CommonExchange: newCommonExchange(Bittrex, client, reqs, channels),
		MarketName:     "BTC-DCR",
	}
	return
}

// BittrexPriceResponse models the JSON data returned from the Bittrex API.
type BittrexPriceResponse struct {
	Success bool                    `json:"success"`
	Message string                  `json:"message"`
	Result  []BittrexResponseResult `json:"result"`
}

// BittrexResponseResult models the "result" field of the Bittrex API response.
type BittrexResponseResult struct {
	MarketName     string  `json:"MarketName"`
	High           float64 `json:"High"`
	Low            float64 `json:"Low"`
	Volume         float64 `json:"Volume"`
	Last           float64 `json:"Last"`
	BaseVolume     float64 `json:"BaseVolume"`
	TimeStamp      string  `json:"TimeStamp"`
	Bid            float64 `json:"Bid"`
	Ask            float64 `json:"Ask"`
	OpenBuyOrders  int     `json:"OpenBuyOrders"`
	OpenSellOrders int     `json:"OpenSellOrders"`
	PrevDay        float64 `json:"PrevDay"`
	Created        string  `json:"Created"`
}

// BittrexDepthPoint models a single point of the bittrex order book.
type BittrexDepthPoint struct {
	Quantity float64 `json:"Quantity"`
	Rate     float64 `json:"Rate"`
}

// BittrexDepthArray is a slice of BittrexDepthPoint
type BittrexDepthArray []BittrexDepthPoint

// BittrexDepthResult models the result field of BittrexDepthResponse
type BittrexDepthResult struct {
	Buy  BittrexDepthArray `json:"buy"`
	Sell BittrexDepthArray `json:"sell"`
}

// Translate the bittrex list to DepthPoints.
func (pts BittrexDepthArray) makePts() []DepthPoint {
	depth := make([]DepthPoint, 0, len(pts))
	for _, pt := range pts {
		depth = append(depth, DepthPoint{
			Quantity: pt.Quantity,
			Price:    pt.Rate,
		})
	}
	return depth
}

// BittrexDepthResponse models the Bittrex order book response.
type BittrexDepthResponse struct {
	Success bool               `json:"success"`
	Message string             `json:"message"`
	Result  BittrexDepthResult `json:"result"`
}

// Translate the Bittrex response to DepthData.
func (r *BittrexDepthResponse) translate() *DepthData {
	if r == nil {
		return nil
	}
	if !r.Success {
		log.Errorf("Bittrex depth result error: %s", r.Message)
		return nil
	}
	depth := new(DepthData)
	depth.Time = time.Now().Unix()
	depth.Asks = r.Result.Sell.makePts()
	depth.Bids = r.Result.Buy.makePts()
	return depth
}

// BittrexCandlestick models a single candlestick in the Bittrex API response.
type BittrexCandlestick struct {
	Open       float64 `json:"O"`
	High       float64 `json:"H"`
	Low        float64 `json:"L"`
	Close      float64 `json:"C"`
	Volume     float64 `json:"V"`
	Time       string  `json:"T"`
	BaseVolume float64 `json:"BV"`
}

// BittrexCandlesticks is a slice of BittrexCandlestick
type BittrexCandlesticks []BittrexCandlestick

func (rawSticks BittrexCandlesticks) translate() Candlesticks {
	sticks := make(Candlesticks, 0, len(rawSticks))
	for _, stick := range rawSticks {
		t, err := time.Parse(time.RFC3339, stick.Time+"Z")
		if err != nil {
			log.Errorf("Failed to parse time from bittrex candlestick. Given: %s", stick.Time)
			return sticks
		}
		sticks = append(sticks, Candlestick{
			High:   stick.High,
			Low:    stick.Low,
			Open:   stick.Open,
			Close:  stick.Close,
			Volume: stick.Volume,
			Start:  t,
		})
	}
	return sticks
}

// BittrexCandlestickResponse models the response from the Bittrex kline data
// endpoint.
type BittrexCandlestickResponse struct {
	Success     bool                `json:"success"`
	Message     string              `json:"message"`
	Result      BittrexCandlesticks `json:"result"`
	Explanation *string             `json:"explanation"`
}

// Refresh retrieves and parses API data from Bittrex.
// Bittrex provides timestamps in a string format that is not quite RFC 3339.
func (bittrex *BittrexExchange) Refresh() {
	bittrex.LogRequest()
	priceResponse := new(BittrexPriceResponse)
	err := bittrex.fetch(bittrex.requests.price, priceResponse)
	if err != nil {
		bittrex.fail("Fetch", err)
		return
	}
	if !priceResponse.Success {
		bittrex.fail("Unsuccessful resquest", err)
		return
	}
	if len(priceResponse.Result) == 0 {
		bittrex.fail("No result", err)
		return
	}
	result := priceResponse.Result[0]
	if result.MarketName != bittrex.MarketName {
		bittrex.fail("Wrong market", fmt.Errorf("Expected market %s. Received %s", bittrex.MarketName, result.MarketName))
		return
	}

	// Depth chart
	depthResponse := new(BittrexDepthResponse)
	err = bittrex.fetch(bittrex.requests.depth, depthResponse)
	if err != nil {
		log.Errorf("Failed to retrieve Bittrex depth chart data: %v", err)
	}
	depth := depthResponse.translate()

	// Check for expired candlesticks
	state := bittrex.state()
	candlesticks := map[candlestickKey]Candlesticks{}
	for bin, req := range bittrex.requests.candlesticks {
		oldSticks, found := state.Candlesticks[bin]
		if !found || oldSticks.needsUpdate(bin) {
			log.Tracef("Signalling candlestick update for %s, bin size %s", bittrex.token, bin)
			response := new(BittrexCandlestickResponse)
			err := bittrex.fetch(req, response)
			if err != nil {
				log.Errorf("Error retrieving candlestick data from Bittrex for bin size %s: %v", string(bin), err)
				continue
			}
			if !response.Success {
				log.Errorf("DragonEx server error while fetching candlestick data. Message: %s", response.Message)
			}

			sticks := response.Result.translate()
			if !found || sticks.time().After(oldSticks.time()) {
				candlesticks[bin] = sticks
			}
		}
	}

	for bin, sticks := range candlesticks {
		log.Infof("%d sticks for bin size %s", len(sticks), string(bin))
	}

	bittrex.Update(&ExchangeState{
		Price:        result.Last,
		BaseVolume:   result.BaseVolume,
		Volume:       result.Volume,
		Change:       result.Last - result.PrevDay,
		Depth:        depth,
		Candlesticks: candlesticks,
	})
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

// DragonExCandlestickLists is a list of DragonExCandlestickList
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
		Price:        price,
		BaseVolume:   btcVolume,
		Volume:       volume,
		Change:       priceChange,
		Stamp:        data.Timestamp,
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
		Price:        priceResponse.Tick.Close,
		BaseVolume:   baseVolume,
		Volume:       baseVolume / priceResponse.Tick.Close,
		Change:       priceResponse.Tick.Close - priceResponse.Tick.Open,
		Stamp:        priceResponse.Ts / 1000,
		Depth:        depth,
		Candlesticks: candlesticks,
	})
}

// PoloniexExchange is a U.S.-based exchange.
type PoloniexExchange struct {
	*CommonExchange
	CurrencyPair string
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

	poloniex = &PoloniexExchange{
		CommonExchange: newCommonExchange(Poloniex, client, reqs, channels),
		CurrencyPair:   "BTC_DCR",
	}
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

	// Depth chart
	depthResponse := new(PoloniexDepthResponse)
	err = poloniex.fetch(poloniex.requests.depth, depthResponse)
	if err != nil {
		log.Errorf("Poloniex depth chart fetch error: %v", err)
	}
	depth := depthResponse.translate()

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

	poloniex.Update(&ExchangeState{
		Price:        price,
		BaseVolume:   baseVolume,
		Volume:       volume,
		Change:       price - oldPrice,
		Depth:        depth,
		Candlesticks: candlesticks,
	})
}
