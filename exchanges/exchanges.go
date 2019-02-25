// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Tokens. Used to identify the exchange.
const (
	Coinbase    = "coinbase"
	CoinbaseURL = "https://api.coinbase.com/v2/exchange-rates?currency=BTC"
	Coindesk    = "coindesk"
	CoindeskURL = "https://api.coindesk.com/v1/bpi/currentprice.json"
	Binance     = "binance"
	BinanceURL  = "https://api.binance.com/api/v1/ticker/24hr?symbol=DCRBTC"
	Bittrex     = "bittrex"
	BittrexURL  = "https://bittrex.com/api/v1.1/public/getmarketsummary?market=btc-dcr"
	DragonEx    = "dragonex"
	DragonExURL = "https://openapi.dragonex.io/api/v1/market/real/?symbol_id=1520101"
	Huobi       = "huobi"
	HuobiURL    = "https://api.huobi.pro/market/detail/merged?symbol=dcrbtc"
	Poloniex    = "poloniex"
	PoloniexURL = "https://poloniex.com/public?command=returnTicker"
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
	var tokens []string
	var token string
	for token = range BtcIndices {
		tokens = append(tokens, token)
	}
	for token = range DcrExchanges {
		tokens = append(tokens, token)
	}
	return tokens
}

// ExchangeState is the simple template for a price. The only member that is
// guaranteed is a price. For Decred exchanges, the volumes will also be
// populated.
type ExchangeState struct {
	Price      float64 `json:"price"`
	BaseVolume float64 `json:"base_volume,omitempty"`
	Volume     float64 `json:"volume,omitempty"`
	Change     float64 `json:"change,omitempty"`
	Stamp      int64   `json:"timestamp,omitempty"`
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
	*sync.RWMutex
	token        string
	URL          string
	currentState *ExchangeState
	client       *http.Client
	lastUpdate   time.Time
	lastFail     time.Time
	lastRequest  time.Time
	request      *http.Request
	channels     *BotChannels
}

// LastUpdate gets a time.Time of the last successful exchange update.
func (xc *CommonExchange) LastUpdate() time.Time {
	xc.RLock()
	defer xc.RUnlock()
	return xc.lastUpdate
}

// Hurry can be used to subtract some amount of time from the lastUpate
// and lastFail, and can be used to de-sync the exchange updates.
func (xc *CommonExchange) Hurry(d time.Duration) {
	xc.Lock()
	defer xc.Unlock()
	xc.lastRequest = xc.lastRequest.Add(-d)
}

// LastFail gets the last time.Time of a failed exchange update.
func (xc *CommonExchange) LastFail() time.Time {
	xc.RLock()
	defer xc.RUnlock()
	return xc.lastFail
}

// IsFailed will be true if xc.lastFail > xc.lastUpdate.
func (xc *CommonExchange) IsFailed() bool {
	xc.RLock()
	defer xc.RUnlock()
	return xc.lastFail.After(xc.lastUpdate)
}

// LogRequest sets the lastRequest time.Time.
func (xc *CommonExchange) LogRequest() {
	xc.Lock()
	defer xc.Unlock()
	xc.lastRequest = time.Now()
}

// LastTry is the more recent of lastFail and LastUpdate.
func (xc *CommonExchange) LastTry() time.Time {
	xc.RLock()
	defer xc.RUnlock()
	return xc.lastRequest
}

// Token is the string associated with the exchange's token.
func (xc *CommonExchange) Token() string {
	return xc.token
}

// setLastFail sets the last failure time.
func (xc *CommonExchange) setLastFail(t time.Time) {
	xc.Lock()
	defer xc.Unlock()
	xc.lastFail = t
}

// Log the error along with the token and an additional passed identifier.
func (xc *CommonExchange) fail(msg string, err error) {
	log.Errorf("%s: %s: %v", xc.token, msg, err)
	xc.setLastFail(time.Now())
}

// Update sends an updated ExchangeState to the ExchangeBot.
func (xc *CommonExchange) Update(state *ExchangeState) {
	xc.Lock()
	defer xc.Unlock()
	xc.lastUpdate = time.Now()
	xc.channels.exchange <- &ExchangeUpdate{
		Token: xc.token,
		State: state,
	}
}

// UpdateIndices sends a bitcoin index update to the ExchangeBot.
func (xc *CommonExchange) UpdateIndices(indices FiatIndices) {
	xc.Lock()
	defer xc.Unlock()
	xc.lastUpdate = time.Now()
	xc.channels.index <- &IndexUpdate{
		Token:   xc.token,
		Indices: indices,
	}
}

// Send the exchange request and decode the response.
func (xc *CommonExchange) fetch(response interface{}) (err error) {
	resp, err := xc.client.Do(xc.request)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("Request failed: %v", err))
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(response)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("Failed to decode json: %v", err))
	}
	return
}

// Used to initialize the embedding exchanges.
func newCommonExchange(token string, client *http.Client,
	request *http.Request, channels *BotChannels) *CommonExchange {
	var tZero time.Time
	return &CommonExchange{
		RWMutex:      new(sync.RWMutex),
		token:        token,
		client:       client,
		channels:     channels,
		currentState: new(ExchangeState),
		lastUpdate:   tZero,
		lastFail:     tZero,
		lastRequest:  tZero,
		request:      request,
	}
}

// CoinbaseExchange provides tons of bitcoin-fiat exchange pairs.
type CoinbaseExchange struct {
	*CommonExchange
}

// NewCoinbase constructs a CoinbaseExchange.
func NewCoinbase(client *http.Client, channels *BotChannels) (coinbase Exchange, err error) {
	request, err := http.NewRequest(http.MethodGet, CoinbaseURL, nil)
	if err != nil {
		return
	}
	coinbase = &CoinbaseExchange{
		CommonExchange: newCommonExchange(Coinbase, client, request, channels),
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
	err := coinbase.fetch(response)
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
	request, err := http.NewRequest(http.MethodGet, CoindeskURL, nil)
	if err != nil {
		return
	}
	coindesk = &CoindeskExchange{
		CommonExchange: newCommonExchange(Coindesk, client, request, channels),
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
	err := coindesk.fetch(response)
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
	request, err := http.NewRequest(http.MethodGet, BinanceURL, nil)
	if err != nil {
		return
	}
	binance = &BinanceExchange{
		CommonExchange: newCommonExchange(Binance, client, request, channels),
	}
	return
}

// BinanceResponse models the JSON data returned from the Binance API.
type BinanceResponse struct {
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

// Refresh retrieves and parses API data from Binance.
func (binance *BinanceExchange) Refresh() {
	binance.LogRequest()
	response := new(BinanceResponse)
	err := binance.fetch(response)
	if err != nil {
		binance.fail("Fetch", err)
		return
	}
	price, err := strconv.ParseFloat(response.LastPrice, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("Failed to parse float from LastPrice=%s", response.LastPrice), err)
		return
	}
	baseVolume, err := strconv.ParseFloat(response.QuoteVolume, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("Failed to parse float from QuoteVolume=%s", response.QuoteVolume), err)
		return
	}
	dcrVolume, err := strconv.ParseFloat(response.Volume, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("Failed to parse float from Volume=%s", response.Volume), err)
		return
	}
	priceChange, err := strconv.ParseFloat(response.PriceChange, 64)
	if err != nil {
		binance.fail(fmt.Sprintf("Failed to parse float from PriceChange=%s", response.PriceChange), err)
		return
	}
	binance.Update(&ExchangeState{
		Price:      price,
		BaseVolume: baseVolume,
		Volume:     dcrVolume,
		Change:     priceChange,
		Stamp:      response.CloseTime / 1000,
	})
}

// BittrexExchange is an unregulated U.S. crypto exchange with good volume.
type BittrexExchange struct {
	*CommonExchange
	MarketName string
}

// NewBittrex constructs a BittrexExchange.
func NewBittrex(client *http.Client, channels *BotChannels) (bittrex Exchange, err error) {
	request, err := http.NewRequest(http.MethodGet, BittrexURL, nil)
	if err != nil {
		return
	}
	bittrex = &BittrexExchange{
		CommonExchange: newCommonExchange(Bittrex, client, request, channels),
		MarketName:     "BTC-DCR",
	}
	return
}

// BittrexResponse models the JSON data returned from the Bittrex API.
type BittrexResponse struct {
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

// Refresh retrieves and parses API data from Bittrex.
// Bittrex provides timestamps in a string format that is not quite RFC 3339.
func (bittrex *BittrexExchange) Refresh() {
	bittrex.LogRequest()
	response := new(BittrexResponse)
	err := bittrex.fetch(response)
	if err != nil {
		bittrex.fail("Fetch", err)
		return
	}
	if !response.Success {
		bittrex.fail("Unsuccessful resquest", err)
		return
	}
	if len(response.Result) == 0 {
		bittrex.fail("No result", err)
		return
	}
	result := response.Result[0]
	if result.MarketName != bittrex.MarketName {
		bittrex.fail("Wrong market", fmt.Errorf("Expected market %s. Recieved %s", bittrex.MarketName, result.MarketName))
		return
	}
	bittrex.Update(&ExchangeState{
		Price:      result.Last,
		BaseVolume: result.BaseVolume,
		Volume:     result.Volume,
		Change:     result.Last - result.PrevDay,
	})
}

// DragonExchange is a Singapore-based crytocurrency exchange.
type DragonExchange struct {
	*CommonExchange
	SymbolID int
}

// NewDragonEx constructs a DragonExchange.
func NewDragonEx(client *http.Client, channels *BotChannels) (dragonex Exchange, err error) {
	request, err := http.NewRequest(http.MethodGet, DragonExURL, nil)
	if err != nil {
		return
	}
	dragonex = &DragonExchange{
		CommonExchange: newCommonExchange(DragonEx, client, request, channels),
		SymbolID:       1520101,
	}
	return
}

// DragonExResponse models the JSON data returned from the DragonEx API.
type DragonExResponse struct {
	Ok   bool                   `json:"ok"`
	Code int                    `json:"code"`
	Data []DragonExResponseData `json:"data"`
	Msg  string                 `json:"msg"`
}

// DragonExResponseData models the JSON data from the DragonEx API.
// Dragonex has the current price in close_price
type DragonExResponseData struct {
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

// Refresh retrieves and parses API data from DragonEx.
func (dragonex *DragonExchange) Refresh() {
	dragonex.LogRequest()
	response := new(DragonExResponse)
	err := dragonex.fetch(response)
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
	dragonex.Update(&ExchangeState{
		Price:      price,
		BaseVolume: btcVolume,
		Volume:     volume,
		Change:     priceChange,
		Stamp:      data.Timestamp,
	})
}

// HuobiExchange is based in Hong Kong and Singapore.
type HuobiExchange struct {
	*CommonExchange
	Ok string
}

// NewHuobi constructs a HuobiExchange.
func NewHuobi(client *http.Client, channels *BotChannels) (huobi Exchange, err error) {
	request, err := http.NewRequest(http.MethodGet, HuobiURL, nil)
	if err != nil {
		return
	}
	request.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	huobi = &HuobiExchange{
		CommonExchange: newCommonExchange(Huobi, client, request, channels),
		Ok:             "ok",
	}
	return
}

// HuobiResponse models the JSON data returned from the Huobi API.
type HuobiResponse struct {
	Status string            `json:"status"`
	Ch     string            `json:"ch"`
	Ts     int64             `json:"ts"`
	Tick   HuobiResponseTick `json:"tick"`
}

// HuobiResponseTick models the "tick" field of the Huobi API response.
type HuobiResponseTick struct {
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

// Refresh retrieves and parses API data from Huobi.
func (huobi *HuobiExchange) Refresh() {
	huobi.LogRequest()
	response := new(HuobiResponse)
	err := huobi.fetch(response)
	if err != nil {
		huobi.fail("Fetch", err)
		return
	}
	if response.Status != huobi.Ok {
		huobi.fail("Status not ok", fmt.Errorf("Expected status %s. Recieved %s", huobi.Ok, response.Status))
		return
	}
	dcrVolume := response.Tick.Vol
	huobi.Update(&ExchangeState{
		Price:      response.Tick.Close,
		BaseVolume: response.Tick.Vol,
		Volume:     dcrVolume,
		Change:     response.Tick.Close - response.Tick.Open,
		Stamp:      response.Ts / 1000,
	})
}

// PoloniexExchange is a U.S.-based exchange.
type PoloniexExchange struct {
	*CommonExchange
	CurrencyPair string
}

// NewPoloniex constructs a PoloniexExchange.
func NewPoloniex(client *http.Client, channels *BotChannels) (poloniex Exchange, err error) {
	request, err := http.NewRequest(http.MethodGet, PoloniexURL, nil)
	if err != nil {
		return
	}
	poloniex = &PoloniexExchange{
		CommonExchange: newCommonExchange(Poloniex, client, request, channels),
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

// Refresh retrieves and parses API data from Poloniex.
func (poloniex *PoloniexExchange) Refresh() {
	poloniex.LogRequest()
	var response map[string]*PoloniexPair
	err := poloniex.fetch(&response)
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
	poloniex.Update(&ExchangeState{
		Price:      price,
		BaseVolume: baseVolume,
		Volume:     volume,
		Change:     price - oldPrice,
	})
}
