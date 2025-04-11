// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package exchanges

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	dcrrates "github.com/decred/dcrdata/exchanges/v3/ratesproto"
	"google.golang.org/grpc"
	credentials "google.golang.org/grpc/credentials"
)

const (
	// DefaultCurrency is overridden by ExchangeBotConfig.BtcIndex. Data
	// structures are cached for DefaultCurrency, so requests are a little bit
	// faster.
	DefaultCurrency = "USD"
	// DefaultDataExpiry is the amount of time between calls to the exchange API.
	DefaultDataExpiry = "5m"
	// DefaultRequestExpiry : Any data older than RequestExpiry will be discarded.
	DefaultRequestExpiry = "60m"

	defaultDCRRatesPort = "7778"

	orderbookKey = "depth"
)

// ExchangeBotConfig is the configuration options for ExchangeBot.
// DataExpiry must be less than RequestExpiry.
// Recommend RequestExpiry > 2*DataExpiry, which will permit the exchange API
// request to fail a couple of times before the exchange's data is discarded.
type ExchangeBotConfig struct {
	Disabled       []string
	DataExpiry     string
	RequestExpiry  string
	Index          string
	Indent         bool
	MasterBot      string
	MasterCertFile string
}

// ExchangeBot monitors exchanges and processes updates. When an update is
// received from an exchange, the state is updated, and some convenient data
// structures are prepared. Make ExchangeBot with NewExchangeBot.
type ExchangeBot struct {
	mtx             sync.RWMutex
	DcrExchanges    map[string]Exchange
	IndexExchanges  map[string]Exchange
	Exchanges       map[string]Exchange
	versionedCharts map[string]*versionedChart
	chartVersions   map[string]int
	// Index is the (typically fiat) currency to which the DCR price should be
	// converted by default. Other conversions are available via a lookup in
	// indexMap, but with slightly lower performance.
	// 3-letter currency code, e.g. USD.
	Index string
	// indexMap is a map of exchanges to supported indices for valid currencies
	// like BTC and USDT or any other asset that is added for dcr in the future.
	// New currency pairs must have at least one entry.
	indexMap     map[string]map[CurrencyPair]FiatIndices
	currentState ExchangeBotState
	// Both currentState and stateCopy hold the same information. currentState
	// is updated by ExchangeBot, and a copy stored in stateCopy. After creation,
	// stateCopy will never be updated, so can be used read-only by multiple
	// threads.
	stateCopy         *ExchangeBotState
	currentStateBytes []byte
	DataExpiry        time.Duration
	RequestExpiry     time.Duration
	minTick           time.Duration
	// A gRPC connection
	masterConnection *grpc.ClientConn
	TLSCredentials   credentials.TransportCredentials
	// Channels requested by the user.
	updateChans []chan *ExchangeUpdate
	indexChans  []chan *IndexUpdate
	quitChans   []chan struct{}
	// exchangeChan and indexChan are passed to the individual exchanges and
	// receive updates after a refresh is triggered.
	exchangeChan chan *ExchangeUpdate
	indexChan    chan *IndexUpdate
	client       *http.Client
	config       *ExchangeBotConfig
	// The failed flag is set when there are either no up-to-date Bitcoin-fiat
	// exchanges or no up-to-date Decred exchanges. IsFailed is a getter for failed.
	failed bool
}

// ExchangeBotState is the current known state of all exchanges, in a certain
// base currency, and a volume-averaged price and total volume in DCR.
type ExchangeBotState struct {
	Index        string                                     `json:"index"`
	BtcPrice     float64                                    `json:"btc_fiat_price"`
	Price        float64                                    `json:"price"`
	Volume       float64                                    `json:"volume"`
	DCRExchanges map[string]map[CurrencyPair]*ExchangeState `json:"dcr_exchanges"`
	// FiatIndices:
	// TODO: We only really need the BaseState for the fiat indices.
	FiatIndices map[string]map[CurrencyPair]*ExchangeState `json:"indices"`
}

// Copy an ExchangeState map.
func copyStates(m map[string]map[CurrencyPair]*ExchangeState) map[string]map[CurrencyPair]*ExchangeState {
	c := make(map[string]map[CurrencyPair]*ExchangeState)
	for t, v := range m {
		mc := make(map[CurrencyPair]*ExchangeState)
		for p, s := range v {
			mc[p] = s
		}
		c[t] = mc
	}
	return c
}

// Creates a pointer to a copy of the ExchangeBotState.
func (state ExchangeBotState) copy() *ExchangeBotState {
	state.DCRExchanges = copyStates(state.DCRExchanges)
	state.FiatIndices = copyStates(state.FiatIndices)
	return &state
}

// BtcToFiat converts an amount of {Bitcoin, USDT} to fiat using the current
// calculated exchange rate.
func (state *ExchangeBotState) PriceToFiat(price float64, currencyPair CurrencyPair) float64 {
	switch currencyPair {
	case CurrencyPairDCRBTC:
		return state.BtcPrice * price

	case CurrencyPairDCRUSDT:
		var usdtPrice, nSources float64
		for _, currencyIndices := range state.FiatIndices {
			state := currencyIndices[USDTIndex]
			if state != nil {
				usdtPrice += state.Price
				nSources++
			}
		}
		if usdtPrice != 0 {
			usdtPrice = usdtPrice / nSources
		}
		return usdtPrice * price

	default:
		return 0
	}
}

// FiatToBtc converts an amount of fiat in the default index to a value in BTC.
func (state *ExchangeBotState) FiatToBtc(fiat float64) float64 {
	if state.BtcPrice == 0 {
		return -1
	}
	return fiat / state.BtcPrice
}

// BitcoinIndices returns a map of all exchanges that provide a bitcoin index.
func (state *ExchangeBotState) BitcoinIndices() map[string]BaseState {
	fiatIndices := make(map[string]BaseState)
	for token, states := range state.FiatIndices {
		s := states[BTCIndex]
		if s != nil {
			fiatIndices[token] = s.BaseState
		}
	}
	return fiatIndices
}

// Indices returns a map of known indices to their current fiat price. Returns
// an empty json object ({}) if it encounters an error.
func (state *ExchangeBotState) Indices() string {
	sumIndexPrice := func(index CurrencyPair) float64 {
		var price, nSource float64
		for _, states := range state.FiatIndices {
			s := states[index]
			if s != nil && s.Price > 0 {
				price += s.Price
				nSource++
			}
		}
		if nSource == 0 {
			return 0
		}
		return price / nSource
	}

	fiatIndices := make(map[string]float64)
	for _, states := range state.FiatIndices {
		for i := range states {
			if _, found := fiatIndices[string(i)]; !found {
				fiatIndices[string(i)] = sumIndexPrice(i)
			}
		}
	}

	b, err := json.Marshal(fiatIndices)
	if err != nil {
		log.Errorf("ExchangeBotState.Indices: json.Marshal error: %v", err)
		return "{}"
	}

	return string(b)
}

// ExchangeState doesn't have a Token field, so if the states are returned as a
// slice (rather than ranging over a map), a token is needed.
type tokenedExchange struct {
	Token string
	CurrencyPair
	State *ExchangeState
}

// VolumeOrderedExchanges returns a list of tokenedExchange sorted by volume,
// highest volume first.
func (state *ExchangeBotState) VolumeOrderedExchanges() []*tokenedExchange {
	var xcList []*tokenedExchange
	for token, states := range state.DCRExchanges {
		for pair, state := range states {
			xcList = append(xcList, &tokenedExchange{
				Token:        token,
				CurrencyPair: pair,
				State:        state,
			})
		}
	}
	sort.Slice(xcList, func(i, j int) bool {
		return xcList[i].State.Volume > xcList[j].State.Volume
	})
	return xcList
}

// FiatIndices maps currency codes to an asset's exchange rates, e.g
// Bitcoin-USD etc.
type FiatIndices map[string]float64

// IndexUpdate is sent from the Exchange to the ExchangeBot indexChan when new
// data is received.
type IndexUpdate struct {
	Token string
	CurrencyPair
	Indices FiatIndices
}

// BotChannels is passed to exchanges for communication with the Start loop.
type BotChannels struct {
	index    chan *IndexUpdate
	exchange chan *ExchangeUpdate
	done     chan struct{}
}

// UpdateChannels are requested by the user with ExchangeBot.UpdateChannels.
type UpdateChannels struct {
	Exchange chan *ExchangeUpdate
	Index    chan *IndexUpdate
	Quit     chan struct{}
}

// The chart data structures that are encoded and cached are the
// candlestickResponse and the depthResponse.
type candlestickResponse struct {
	Index      string       `json:"index"`
	Price      float64      `json:"price"`
	Sticks     Candlesticks `json:"sticks"`
	Expiration int64        `json:"expiration"`
}

type depthResponse struct {
	BtcIndex   string     `json:"index"`
	Price      float64    `json:"price"`
	Data       *DepthData `json:"data"`
	Expiration int64      `json:"expiration"`
}

// versionedChart holds a pre-encoded byte slice of a chart's data along with a
// version number that can be compared for use in caching.
type versionedChart struct {
	chartID string
	dataID  int
	chart   []byte
}

func genCacheID(parts ...string) string {
	return strings.Join(parts, "-")
}

// NewExchangeBot constructs a new ExchangeBot with the provided configuration.
func NewExchangeBot(config *ExchangeBotConfig) (*ExchangeBot, error) {
	// Validate configuration
	if config.DataExpiry == "" {
		config.DataExpiry = DefaultDataExpiry
	}
	if config.RequestExpiry == "" {
		config.RequestExpiry = DefaultRequestExpiry
	}
	dataExpiry, err := time.ParseDuration(config.DataExpiry)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse data expiration from %s", config.DataExpiry)
	}
	requestExpiry, err := time.ParseDuration(config.RequestExpiry)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse request expiration from %s", config.RequestExpiry)
	}
	if requestExpiry < dataExpiry {
		return nil, fmt.Errorf("Request expiration must be longer than data expiration")
	}
	if dataExpiry < time.Minute {
		return nil, fmt.Errorf("Expiration must be at least one minute")
	}
	if config.Index == "" {
		config.Index = DefaultCurrency
	}

	bot := &ExchangeBot{
		DcrExchanges:    make(map[string]Exchange),
		IndexExchanges:  make(map[string]Exchange),
		Exchanges:       make(map[string]Exchange),
		versionedCharts: make(map[string]*versionedChart),
		chartVersions:   make(map[string]int),
		Index:           config.Index,
		indexMap:        make(map[string]map[CurrencyPair]FiatIndices),
		currentState: ExchangeBotState{
			Index:        config.Index,
			Price:        0,
			Volume:       0,
			DCRExchanges: make(map[string]map[CurrencyPair]*ExchangeState),
			FiatIndices:  make(map[string]map[CurrencyPair]*ExchangeState),
		},
		currentStateBytes: []byte{},
		DataExpiry:        dataExpiry,
		RequestExpiry:     requestExpiry,
		minTick:           5 * time.Second,
		updateChans:       []chan *ExchangeUpdate{},
		indexChans:        []chan *IndexUpdate{},
		quitChans:         []chan struct{}{},
		exchangeChan:      make(chan *ExchangeUpdate, 16),
		indexChan:         make(chan *IndexUpdate, 16),
		client:            new(http.Client),
		config:            config,
		failed:            false,
	}

	if config.MasterBot != "" {
		if config.MasterCertFile == "" {
			return nil, fmt.Errorf("No TLS certificate path provided")
		}
		bot.TLSCredentials, err = credentials.NewClientTLSFromFile(config.MasterCertFile, "")
		if err != nil {
			return nil, fmt.Errorf("Failed to load TLS certificate: %v", err)
		}
		host, port, err := net.SplitHostPort(config.MasterBot)
		if err != nil {
			if !strings.Contains(err.Error(), "missing port in address") {
				return nil, fmt.Errorf("Unable to parse master bot address %s: %v", config.MasterBot, err)
			}
			port = defaultDCRRatesPort
		}
		if host == "" {
			// For addresses passed in form :[port], quietly substitute localhost. A
			// hostname is required for the gRPC TLS connection.
			host = "localhost"
		}
		config.MasterBot = host + ":" + port
	}

	isDisabled := func(token string) bool {
		for _, tkn := range config.Disabled {
			if tkn == token {
				return true
			}
		}
		return false
	}

	quit := make(chan struct{})
	bot.quitChans = append(bot.quitChans, quit)

	channels := &BotChannels{
		index:    bot.indexChan,
		exchange: bot.exchangeChan,
		done:     quit,
	}

	buildExchange := func(token string, constructor func(*http.Client, *BotChannels) (Exchange, error), xcMap map[string]Exchange) {
		if isDisabled(token) {
			return
		}
		xc, err := constructor(bot.client, channels)
		if err != nil {
			return
		}
		xcMap[token] = xc
		bot.Exchanges[token] = xc
	}

	for token, constructor := range Indices {
		buildExchange(token, constructor, bot.IndexExchanges)
	}

	for token, constructor := range DcrExchanges {
		buildExchange(token, constructor, bot.DcrExchanges)
	}

	if len(bot.DcrExchanges) == 0 {
		return nil, fmt.Errorf("no DCR exchanges were initialized")
	}

	if len(bot.IndexExchanges) == 0 {
		return nil, fmt.Errorf("no {BTC, USDT}-fiat exchanges were initialized")
	}

	return bot, nil
}

// Start is the main ExchangeBot loop, reading from the exchange update channel
// and scheduling refresh cycles.
func (bot *ExchangeBot) Start(ctx context.Context, wg *sync.WaitGroup) {
	tick := time.NewTimer(time.Second)

	config := bot.config

	reconnectionAttempt := 0

	if config.MasterBot != "" {
		stream, err := bot.connectMasterBot(ctx, 0)
		if err != nil {
			log.Errorf("Failed to initialize gRPC stream. Falling back to direct connection: %v", err)
		} else {
			// Drain the timer to prevent the first cycle
			if !tick.Stop() {
				<-tick.C
			}
			// Start a loop to listen for updates from the dcrrates server.
			go func() {
				for {
					update, err := stream.Recv()
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						log.Errorf("DCRRates error. Attempting to reconnect in 10 seconds: %v", err)
						delay := 10 * time.Second
						delayString := "10 seconds"
						// Try to reconnect every minute until a connection is made.
						for {
							reconnectionAttempt++
							stream, err = bot.connectMasterBot(ctx, delay)
							if err == nil {
								break
							} else {
								if ctx.Err() != nil {
									return
								}
								if reconnectionAttempt > 12 { // ~ two minutes
									delay = time.Minute
									delayString = "1 minute"
								}
								log.Errorf("Failed to reconnect to DCRRates. Attempting to reconnect in %s: %v", delayString, err)
							}
						}
						log.Infof("DCRRates connection re-established.")
						reconnectionAttempt = 0
						continue
					}
					// Send the update through the Exchange so that appropriate
					// attributes are set.
					if IsDcrExchange(update.Token) {
						currencyPair, state := exchangeStateFromProto(update)
						if !currencyPair.IsValidDCRPair() {
							log.Errorf("Received update for unknown currency pair %s", currencyPair)
						} else {
							bot.Exchanges[update.Token].Update(currencyPair, state)
						}
					} else if IsIndex(update.Token) {
						currencyIndex := CurrencyPair(update.GetCurrencyPair())
						if !currencyIndex.IsValidIndex() {
							log.Errorf("Received update for unknown index %s", currencyIndex)
						} else {
							bot.Exchanges[update.Token].UpdateIndices(currencyIndex, update.GetIndices())
						}
					}
				}
			}()
		}
	} // End DCRRates master
	if bot.masterConnection == nil {
		// Start refresh on all exchanges, and then change the updateTimes to
		// de-sync the updates.
		timeBetween := bot.DataExpiry / time.Duration(len(bot.Exchanges))
		idx := 0
		for _, xc := range bot.Exchanges {
			go func(xc Exchange, d int) {
				xc.Refresh()
				if !xc.IsFailed() {
					xc.Hurry(timeBetween * time.Duration(d))
				}
			}(xc, idx)
			idx++
		}
	}

out:
	for {
		select {
		case update := <-bot.exchangeChan:
			log.Tracef("exchange update received from %s (Currency Pair: %s) with price %f, ", update.Token, update.CurrencyPair, update.State.Price)
			err := bot.updateExchange(update)
			if err != nil {
				log.Warnf("Error encountered in exchange update: %v", err)
				continue
			}
			bot.signalExchangeUpdate(update)
		case update := <-bot.indexChan:
			price, found := update.Indices[bot.Index]
			if found {
				log.Tracef("index update received from %s with %d indices, %s price for %s is %f", update.Token, len(update.Indices), bot.Index, update.CurrencyPair, price)
			}
			err := bot.updateIndices(update)
			if err != nil {
				log.Warnf("Error encountered in index update: %v", err)
				continue
			}
			bot.signalIndexUpdate(update)
		case <-tick.C:
			bot.Cycle()
		case <-ctx.Done():
			break out
		}
		if bot.masterConnection == nil {
			tick = bot.nextTick()
		}
	}
	if wg != nil {
		wg.Done()
	}
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	for _, ch := range bot.quitChans {
		close(ch)
	}
	if bot.masterConnection != nil {
		bot.masterConnection.Close()
	}
}

// Attempt DCRRates connection after delay.
func (bot *ExchangeBot) connectMasterBot(ctx context.Context, delay time.Duration) (dcrrates.DCRRates_SubscribeExchangesClient, error) {
	if bot.masterConnection != nil {
		bot.masterConnection.Close()
	}
	if delay > 0 {
		expiration := time.NewTimer(delay)
		select {
		case <-expiration.C:
		case <-ctx.Done():
			return nil, fmt.Errorf("Context cancelled before reconnection")
		}
	}
	conn, err := grpc.Dial(bot.config.MasterBot, grpc.WithTransportCredentials(bot.TLSCredentials))
	if err != nil {
		log.Warnf("gRPC connection error when trying to connect to %s. Falling back to direct connection: %v", bot.config.MasterBot, err)
		return nil, err
	}
	bot.masterConnection = conn
	grpcClient := dcrrates.NewDCRRatesClient(conn)
	stream, err := grpcClient.SubscribeExchanges(ctx, &dcrrates.ExchangeSubscription{
		Index:     bot.Index,
		Exchanges: bot.subscribedExchanges(),
	})
	if err != nil {
		return nil, err
	}
	return stream, nil
}

// A list of exchanges which the ExchangeBot is monitoring.
func (bot *ExchangeBot) subscribedExchanges() []string {
	xcList := make([]string, 0, len(bot.Exchanges))
	for token := range bot.Exchanges {
		xcList = append(xcList, token)
	}
	return xcList
}

// UpdateChannels creates an UpdateChannels, which holds a channel to receive
// exchange updates and a channel which is closed when the start loop exits.
func (bot *ExchangeBot) UpdateChannels() *UpdateChannels {
	update := make(chan *ExchangeUpdate, 16)
	index := make(chan *IndexUpdate, 16)
	quit := make(chan struct{})
	bot.mtx.Lock()
	defer bot.mtx.Unlock()
	bot.updateChans = append(bot.updateChans, update)
	bot.indexChans = append(bot.indexChans, index)
	bot.quitChans = append(bot.quitChans, quit)
	return &UpdateChannels{
		Exchange: update,
		Index:    index,
		Quit:     quit,
	}
}

// Send an update to any channels requested with bot.UpdateChannels().
func (bot *ExchangeBot) signalExchangeUpdate(update *ExchangeUpdate) {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	for _, ch := range bot.updateChans {
		select {
		case ch <- update:
		default:
			log.Warnf("Failed to write update to exchange update channel")
		}
	}
}

func (bot *ExchangeBot) signalIndexUpdate(update *IndexUpdate) {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	for _, ch := range bot.indexChans {
		select {
		case ch <- update:
		default:
		}
	}
}

// State is a copy of the current ExchangeBotState. A JSON-encoded byte array
// of the current state can be accessed through StateBytes().
func (bot *ExchangeBot) State() *ExchangeBotState {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	return bot.stateCopy
}

// indicesForCode must be called under bot.mtx lock.
func (bot *ExchangeBot) indicesForCode(code string) map[string]map[CurrencyPair]*ExchangeState {
	fiatIndices := make(map[string]map[CurrencyPair]*ExchangeState)
	for token, indices := range bot.indexMap {
		for currencyPair, indice := range indices {
			for symbol, price := range indice {
				if symbol == code {
					fiatIndices[token] = map[CurrencyPair]*ExchangeState{
						currencyPair: {BaseState: BaseState{Price: price}},
					}
				}
			}
		}
	}
	return fiatIndices
}

// ConvertedState returns an ExchangeBotState with a base of the provided
// currency code, if available.
func (bot *ExchangeBot) ConvertedState(code string) (*ExchangeBotState, error) {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	dcrPrice, volume := bot.dcrPriceAndVolume(code)
	btcPrice := bot.indexPrice(BTCIndex, code)
	if dcrPrice == 0 || btcPrice == 0 {
		bot.failed = true
		return nil, fmt.Errorf("Unable to process price for currency %s", code)
	}

	state := ExchangeBotState{
		Index:        code,
		Volume:       volume,
		Price:        dcrPrice,
		BtcPrice:     dcrPrice,
		DCRExchanges: bot.currentState.DCRExchanges,
		FiatIndices:  bot.indicesForCode(code),
	}

	return state.copy(), nil
}

// ExchangeRates is the dcr and btc prices converted to fiat.
type ExchangeRates struct {
	Index     string                                `json:"index"`
	DcrPrice  float64                               `json:"dcrPrice"`
	BtcPrice  float64                               `json:"btcPrice"`
	Exchanges map[string]map[CurrencyPair]BaseState `json:"exchanges"`
}

// Rates is the current exchange rates for dcr and btc.
func (bot *ExchangeBot) Rates() *ExchangeRates {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	s := bot.stateCopy

	xcMarkets := make(map[string]map[CurrencyPair]BaseState, len(s.DCRExchanges))
	for token, xcStates := range s.DCRExchanges {
		xcs := make(map[CurrencyPair]BaseState, len(xcStates))
		for currencyPair, xcState := range xcStates {
			xcs[currencyPair] = xcState.BaseState
		}
		xcMarkets[token] = xcs
	}

	return &ExchangeRates{
		Index:     s.Index,
		DcrPrice:  s.Price,
		BtcPrice:  s.BtcPrice,
		Exchanges: xcMarkets,
	}
}

// ConvertedRates returns an ExchangeRates with a base of the provided currency
// code, if available.
func (bot *ExchangeBot) ConvertedRates(code string) (*ExchangeRates, error) {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	dcrPrice, _ := bot.dcrPriceAndVolume(code)
	btcPrice := bot.indexPrice(BTCIndex, code)
	if btcPrice == 0 || dcrPrice == 0 {
		bot.failed = true
		return nil, fmt.Errorf("Unable to process price for currency %s", code)
	}

	return &ExchangeRates{
		Index:    code,
		DcrPrice: dcrPrice,
		BtcPrice: btcPrice,
	}, nil
}

// StateBytes is a JSON-encoded byte array of the currentState.
func (bot *ExchangeBot) StateBytes() []byte {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	return bot.currentStateBytes
}

// Encodes the thing as JSON, with indentation if configured.
func (bot *ExchangeBot) encodeJSON(thing interface{}) ([]byte, error) {
	if bot.config.Indent {
		return json.MarshalIndent(thing, "", "    ")
	}
	return json.Marshal(thing)
}

// ConvertedStateBytes gives a JSON-encoded byte array of the currentState
// with a base of the provided currency code, if available.
func (bot *ExchangeBot) ConvertedStateBytes(symbol string) ([]byte, error) {
	state, err := bot.ConvertedState(symbol)
	if err != nil {
		return nil, err
	}
	return bot.encodeJSON(state)
}

// AvailableIndices creates a fresh slice of all available index currency codes.
func (bot *ExchangeBot) AvailableIndices() []string {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	var indices sort.StringSlice
	add := func(index string) {
		for _, symbol := range indices {
			if symbol == index {
				return
			}
		}
		indices = append(indices, index)
	}
	for _, fiatIndices := range bot.indexMap {
		for _, indices := range fiatIndices {
			for symbol := range indices {
				add(symbol)
			}
		}
	}
	sort.Sort(indices)
	return indices
}

// Indices is the fiat indices for a given {BTC, USDT} index exchange.
func (bot *ExchangeBot) Indices(token string) map[CurrencyPair]FiatIndices {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	indices := make(map[CurrencyPair]FiatIndices)
	for currencyIndex, indice := range bot.indexMap[token] {
		indices[currencyIndex] = indice
	}
	return indices
}

func (bot *ExchangeBot) incrementChart(chartId string) {
	_, found := bot.chartVersions[chartId]
	if found {
		bot.chartVersions[chartId]++
	} else {
		bot.chartVersions[chartId] = 0
	}
}

func (bot *ExchangeBot) cachedChartVersion(chartId string) int {
	cid, found := bot.chartVersions[chartId]
	if !found {
		return -1
	}
	return cid
}

// processState is a helper function to process a slice of ExchangeState into a
// price, and optionally a volume sum, and perform some cleanup along the way.
// If volumeAveraged is false, all exchanges are given equal weight in the avg.
// If exchange is invalid, a bool false is returned as a last return value.
func (bot *ExchangeBot) processState(token, code string, states map[CurrencyPair]*ExchangeState, volumeAveraged bool) (float64, float64, bool) {
	oldestValid := time.Now().Add(-bot.RequestExpiry)
	if bot.Exchanges[token].LastUpdate().Before(oldestValid) {
		return 0, 0, false
	}

	var priceAccumulator, volSum float64
	for currencyPair, state := range states {
		volume := 1.0
		if volumeAveraged {
			volume = state.Volume
		}
		volSum += volume

		// Convert price to bot.Index.
		price := state.Price
		switch currencyPair {
		case CurrencyPairDCRBTC:
			price = bot.indexPrice(BTCIndex, code) * price
		case CurrencyPairDCRUSDT:
			price = bot.indexPrice(USDTIndex, code) * price
		}
		if price == 0 { // missing index price for currencyPair.
			return 0, 0, false
		}

		priceAccumulator += volume * price
	}

	if volSum == 0 {
		return 0, 0, true
	}
	return priceAccumulator / volSum, volSum, true
}

// indexPrice retrieves the index price for the provided currency index
// {BTC-Index, USDT-Index}. Must be called under bot.mutex lock.
func (bot *ExchangeBot) indexPrice(index CurrencyPair, code string) float64 {
	var price, nSource float64
	for _, currencyIndex := range bot.indexMap {
		indices := currencyIndex[index]
		if len(indices) != 0 && indices[code] > 0 {
			price += indices[code]
			nSource++
		}
	}
	if nSource == 0 {
		return 0
	}
	return price / nSource
}

// updateExchange processes an update from a Decred-{Asset} Exchange.
func (bot *ExchangeBot) updateExchange(update *ExchangeUpdate) error {
	bot.mtx.Lock()
	defer bot.mtx.Unlock()
	if update.State.Candlesticks != nil {
		for bin := range update.State.Candlesticks {
			bot.incrementChart(genCacheID(string(update.CurrencyPair), update.Token, string(bin)))
		}
	}
	if update.State.Depth != nil {
		bot.incrementChart(genCacheID(string(update.CurrencyPair), update.Token, orderbookKey))
	}

	if bot.currentState.DCRExchanges[update.Token] == nil {
		bot.currentState.DCRExchanges[update.Token] = make(map[CurrencyPair]*ExchangeState)
	}
	bot.currentState.DCRExchanges[update.Token][update.CurrencyPair] = update.State
	return bot.updateState()
}

// updateIndices processes an update from an {Bitcoin, USDT} index source,
// essentially a map pairing currency codes to bitcoin or usdt prices.
func (bot *ExchangeBot) updateIndices(update *IndexUpdate) error {
	bot.mtx.Lock()
	defer bot.mtx.Unlock()
	if bot.indexMap[update.Token] == nil {
		bot.indexMap[update.Token] = make(map[CurrencyPair]FiatIndices)
	}

	bot.indexMap[update.Token][update.CurrencyPair] = update.Indices
	price, hasCode := update.Indices[bot.config.Index]
	if hasCode {
		if bot.currentState.FiatIndices[update.Token] == nil {
			bot.currentState.FiatIndices[update.Token] = make(map[CurrencyPair]*ExchangeState)
		}

		bot.currentState.FiatIndices[update.Token][update.CurrencyPair] = &ExchangeState{
			BaseState: BaseState{
				Price: price,
				Stamp: time.Now().Unix(),
			},
		}
		return bot.updateState()
	}
	log.Warnf("Default currency code, %s, not contained in update from %s", bot.Index, update.Token)
	return nil
}

// dcrPriceAndVolume calculates and returns dcr price and volume. The returned
// dcr price is converted to the provided index code. Must be called under
// bot.mtx lock.
func (bot *ExchangeBot) dcrPriceAndVolume(code string) (float64, float64) {
	var dcrPrice, volume, nSources float64
	for token, xcStates := range bot.currentState.DCRExchanges {
		processedDcrPrice, processedVolume, ok := bot.processState(token, code, xcStates, true)
		if !ok {
			continue
		}

		volume += processedVolume
		if processedDcrPrice != 0 {
			dcrPrice += processedDcrPrice
			nSources++
		}
	}

	if dcrPrice == 0 {
		return 0, 0
	}

	return dcrPrice / nSources, volume
}

// Called from both updateIndices and updateExchange (under mutex lock).
func (bot *ExchangeBot) updateState() error {
	btcPrice := bot.indexPrice(BTCIndex, bot.Index)
	dcrPrice, volume := bot.dcrPriceAndVolume(bot.Index)
	if btcPrice == 0 || dcrPrice == 0 {
		bot.failed = true
	} else {
		bot.failed = false
		bot.currentState.Price = dcrPrice
		bot.currentState.BtcPrice = btcPrice
		bot.currentState.Volume = volume
	}

	var jsonBytes []byte
	var err error
	if bot.config.Indent {
		jsonBytes, err = json.MarshalIndent(bot.currentState, "", "    ")
	} else {
		jsonBytes, err = json.Marshal(bot.currentState)
	}
	if err != nil {
		return fmt.Errorf("Failed to write bytes: %v", err)
	}
	bot.currentStateBytes = jsonBytes
	bot.stateCopy = bot.currentState.copy()

	return nil
}

// IsFailed is whether the failed flag was set during the last IndexUpdate
// or ExchangeUpdate. The failed flag is set when either no Bitcoin Index
// sources or no Decred Exchanges are up-to-date. Individual exchanges can
// be outdated/failed without IsFailed being false, as long as there is at least
// one Bitcoin index and one Decred exchange.
func (bot *ExchangeBot) IsFailed() bool {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	return bot.failed
}

// nextTick checks the exchanges' last update and fail times, and calculates
// when the next Cycle should run.
func (bot *ExchangeBot) nextTick() *time.Timer {
	tNow := time.Now()
	tOldest := tNow
	for _, xc := range bot.Exchanges {
		t := xc.LastTry()
		if t.Before(tOldest) {
			tOldest = t
		}
	}
	tSince := tNow.Sub(tOldest)
	tilNext := bot.DataExpiry - tSince
	if tilNext < bot.minTick {
		tilNext = bot.minTick
	}
	return time.NewTimer(tilNext)
}

// Cycle refreshes all expired exchanges.
func (bot *ExchangeBot) Cycle() {
	tNow := time.Now()
	for _, xc := range bot.Exchanges {
		if tNow.Sub(xc.LastTry()) > bot.DataExpiry {
			go xc.Refresh()
		}
	}
}

// Price gets the latest Price in the default currency (Index).
func (bot *ExchangeBot) Price() float64 {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	return bot.currentState.Price
}

// Conversion is a representation of some amount of DCR in another index.
type Conversion struct {
	Value float64 `json:"value"`
	Index string  `json:"index"`
}

// TwoDecimals is a string representation of the value with two digits after
// the decimal point, but will show more to achieve at least three significant
// digits.
func (c *Conversion) TwoDecimals() string {
	if c.Value == 0.0 {
		return "0.00"
	} else if c.Value < 1.0 && c.Value > -1.0 {
		return fmt.Sprintf("%3g", c.Value)
	}
	return fmt.Sprintf("%.2f", c.Value)
}

// Conversion attempts to multiply the supplied float with the default index.
// Nil pointer will be returned if there is no valid exchangeState.
func (bot *ExchangeBot) Conversion(dcrVal float64) *Conversion {
	if bot == nil {
		return nil
	}
	xcState := bot.State()
	if xcState != nil {
		return &Conversion{
			Value: xcState.Price * dcrVal,
			Index: xcState.Index,
		}
	}
	// Haven't gotten data yet, but we're running.
	return &Conversion{Value: 0, Index: DefaultCurrency}
}

// Fetch the pre-encoded JSON chart data from the cache, if it exists and is not
// expired. Data is considered expired only when newer data has been received,
// but not necessarily requested/encoded yet. Boolean `hit` indicates success.
func (bot *ExchangeBot) fetchFromCache(chartID string) (data []byte, bestVersion int, hit bool) {
	bot.mtx.RLock()
	defer bot.mtx.RUnlock()
	bestVersion = bot.cachedChartVersion(chartID)
	cache, found := bot.versionedCharts[chartID]
	if found && cache.dataID == bestVersion {
		data = cache.chart
		hit = true
	}
	return
}

// QuickSticks returns the up-to-date candlestick data for the specified
// exchange and bin width, pulling from the cache if appropriate.
func (bot *ExchangeBot) QuickSticks(token string, market CurrencyPair, rawBin string) ([]byte, error) {
	if !market.IsValidDCRPair() {
		return nil, fmt.Errorf("invalid market %s", market)
	}

	chartID := genCacheID(string(market), token, rawBin)
	bin := candlestickKey(rawBin)
	data, bestVersion, isGood := bot.fetchFromCache(chartID)
	if isGood {
		return data, nil
	}

	// No hit on cache. Re-encode.

	bot.mtx.Lock()
	defer bot.mtx.Unlock()
	xcStates, found := bot.currentState.DCRExchanges[token]
	if !found {
		return nil, fmt.Errorf("Failed to find DCR exchange state for %s", token)
	}

	state, found := xcStates[market]
	if !found {
		return nil, fmt.Errorf("Failed to find DCR exchange state for %s (Currency Pair: %s)", token, market)
	}
	if state.Candlesticks == nil {
		return nil, fmt.Errorf("Failed to find candlesticks for %s", token)
	}

	sticks, found := state.Candlesticks[bin]
	if !found {
		return nil, fmt.Errorf("Failed to find candlesticks for %s and bin %s", token, rawBin)
	}
	if len(sticks) == 0 {
		return nil, fmt.Errorf("Empty candlesticks for %s and bin %s", token, rawBin)
	}

	expiration := sticks[len(sticks)-1].Start.Add(2 * bin.duration())
	chart, err := bot.encodeJSON(&candlestickResponse{
		Index:      bot.Index,
		Price:      bot.currentState.Price,
		Sticks:     sticks,
		Expiration: expiration.Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("JSON encode error for %s and bin %s", token, rawBin)
	}

	vChart := &versionedChart{
		chartID: chartID,
		dataID:  bestVersion,
		chart:   chart,
	}

	bot.versionedCharts[chartID] = vChart
	return vChart.chart, nil
}

// QuickDepth returns the up-to-date depth chart data for the specified exchange
// market, pulling from the cache if appropriate.
func (bot *ExchangeBot) QuickDepth(token string, market CurrencyPair) (chart []byte, err error) {
	if !market.IsValidDCRPair() {
		return nil, fmt.Errorf("invalid market %s", market)
	}

	chartID := genCacheID(string(market), token, orderbookKey)
	data, bestVersion, isGood := bot.fetchFromCache(chartID)
	if isGood {
		return data, nil
	}

	bot.mtx.Lock()
	defer bot.mtx.Unlock()
	xcStates, found := bot.currentState.DCRExchanges[token]
	if !found {
		return nil, fmt.Errorf("Failed to find DCR exchange state for %s (Currency Pair: %s)", token, market)
	}

	state, ok := xcStates[market]
	if !ok || state.Depth == nil {
		return nil, fmt.Errorf("Failed to find depth for %s (Currency Pair: %s)", token, market)
	}

	chart, err = bot.encodeJSON(&depthResponse{
		BtcIndex:   bot.Index,
		Price:      bot.currentState.Price,
		Data:       state.Depth,
		Expiration: state.Depth.Time + int64(bot.RequestExpiry.Seconds()),
	})
	if err != nil {
		return nil, fmt.Errorf("JSON encode error for %s depth chart", token)
	}

	vChart := &versionedChart{
		chartID: chartID,
		dataID:  bestVersion,
		chart:   chart,
	}

	bot.versionedCharts[chartID] = vChart
	return vChart.chart, nil
}
