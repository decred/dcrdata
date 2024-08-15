import { Controller } from '@hotwired/stimulus'
import { requestJSON } from '../helpers/http'
import humanize from '../helpers/humanize_helper'
import { getDefault } from '../helpers/module_helper'
import TurboQuery from '../helpers/turbolinks_helper'
import globalEventBus from '../services/event_bus_service'
import { darkEnabled } from '../services/theme_service'

let Dygraph
const SELL = 1
const BUY = 2
const candlestick = 'candlestick'
const orders = 'orders'
const depth = 'depth'
const history = 'history'
const volume = 'volume'
const aggregatedKey = 'aggregated'
const anHour = '1h'
const minuteMap = {
  '5m': 5,
  '30m': 30,
  '1h': 60,
  '1d': 1440,
  '1mo': 43200
}
const PIPI = 2 * Math.PI
const prettyDurations = {
  '5m': '5 min',
  '30m': '30 min',
  '1h': 'hour',
  '1d': 'day',
  '1mo': 'month'
}
const exchangeLinks = {
  CurrencyPairDCRBTC: {
    binance: 'https://www.binance.com/en/trade/DCR_BTC',
    bittrex: 'https://bittrex.com/Market/Index?MarketName=BTC-DCR',
    poloniex: 'https://poloniex.com/exchange#btc_dcr',
    dragonex: 'https://dragonex.io/en-us/trade/index/dcr_btc',
    huobi: 'https://www.hbg.com/en-us/exchange/?s=dcr_btc',
    dcrdex: 'https://dex.decred.org'
  },
  CurrencyPairDCRUSDT: {
    binance: 'https://www.binance.com/en/trade/DCR_USDT',
    dcrdex: 'https://dex.decred.org',
    mexc: 'https://www.mexc.com/exchange/DCR_USDT'
  }
}
const CurrencyPairDCRUSDT = 'DCR-USDT'
const CurrencyPairDCRBTC = 'DCR-BTC'

function isValidDCRPair (pair) {
  return pair === CurrencyPairDCRBTC || pair === CurrencyPairDCRUSDT
}

const BTCIndex = 'BTC-Index'
const USDTIndex = 'USDT-Index'

function quoteAsset (currencyPair) {
  const v = currencyPair.split('-')
  if (v.length === 1) {
    return currencyPair
  }
  return v[1].toUpperCase()
}

const printNames = {
  dcrdex: 'dex.decred.org'
  // default is capitalize
}

function printName (token) {
  const name = printNames[token]
  if (name) return name
  return humanize.capitalize(token)
}

const defaultZoomPct = 20
let hidden, visibilityChange
if (typeof document.hidden !== 'undefined') { // Opera 12.10 and Firefox 18 and later support
  hidden = 'hidden'
  visibilityChange = 'visibilitychange'
} else if (typeof document.msHidden !== 'undefined') {
  hidden = 'msHidden'
  visibilityChange = 'msvisibilitychange'
} else if (typeof document.webkitHidden !== 'undefined') {
  hidden = 'webkitHidden'
  visibilityChange = 'webkitvisibilitychange'
}
let focused = true
let refreshAvailable = false
let availableCandlesticks, availableDepths

function screenIsBig () {
  return window.innerWidth >= 992
}

function validDepthExchange (token) {
  return availableDepths.indexOf(token) > -1
}

function hasBin (xc, bin) {
  return availableCandlesticks[xc].indexOf(bin) !== -1
}

function usesOrderbook (chart) {
  return chart === depth || chart === orders
}

function usesCandlesticks (chart) {
  return chart === candlestick || chart === volume || chart === history
}

let requestCounter = 0
let responseCache = {}

function hasCache (k) {
  if (!responseCache[k]) return false
  const expiration = new Date(responseCache[k].expiration)
  return expiration > new Date()
}

function clearCache (k) {
  if (!responseCache[k]) return
  delete responseCache[k]
}

let indices = {}
function currentPairFiatPrice () {
  switch (settings.pair) {
    case CurrencyPairDCRBTC:
      return indices[BTCIndex]
    case CurrencyPairDCRUSDT:
      return indices[USDTIndex]
    default:
      return -1
  }
}

const lightStroke = '#333'
const darkStroke = '#ddd'
let chartStroke = lightStroke
let conversionFactor = 1
let fiatCode
const gridColor = '#7774'
let settings = {}

const commonChartOpts = {
  gridLineColor: gridColor,
  axisLineColor: 'transparent',
  underlayCallback: (ctx, area, dygraph) => {
    ctx.lineWidth = 1
    ctx.strokeStyle = gridColor
    ctx.strokeRect(area.x, area.y, area.w, area.h)
  },
  // these should be set to avoid Dygraph strangeness
  labels: [' ', ' '], // To avoid an annoying console message,
  xlabel: ' ',
  ylabel: ' ',
  pointSize: 6
}

const chartResetOpts = {
  fillGraph: false,
  strokeWidth: 2,
  drawPoints: false,
  logscale: false,
  xRangePad: 0,
  yRangePad: 0,
  zoomCallback: null
}

function convertedThreeSigFigs (x) {
  return humanize.threeSigFigs(x * conversionFactor)
}

function convertedEightDecimals (x) {
  return (x * conversionFactor).toFixed(8)
}

function adjustAxis (axis, zoomInPercentage, bias) {
  const delta = axis[1] - axis[0]
  const increment = delta * zoomInPercentage
  const foo = [increment * bias, increment * (1 - bias)]
  return [axis[0] + foo[0], axis[1] - foo[1]]
}

function gScroll (event, g, context) {
  const percentage = event.detail ? event.detail * -1 / 1000 : event.wheelDelta ? event.wheelDelta / 1000 : event.deltaY / -25

  if (!(event.offsetX && event.offsetY)) {
    event.offsetX = event.layerX - event.target.offsetLeft
    event.offsetY = event.layerY - event.target.offsetTop
  }

  const xOffset = g.toDomCoords(g.xAxisRange()[0], null)[0]
  const x = event.offsetX - xOffset
  const w = g.toDomCoords(g.xAxisRange()[1], null)[0] - xOffset
  const xPct = w === 0 ? 0 : (x / w)
  const newWindow = adjustAxis(g.xAxisRange(), percentage, xPct)
  g.updateOptions({
    dateWindow: newWindow
  })
  const zoomCallback = g.getOption('zoomCallback')
  if (zoomCallback) zoomCallback(newWindow[0], newWindow[1], g.yAxisRanges())
  event.preventDefault()
  event.stopPropagation()
}

function orderbookStats (bids, asks) {
  const bidEdge = bids[0].price
  const askEdge = asks[0].price
  const midGap = (bidEdge + askEdge) / 2
  return {
    bidEdge: bidEdge,
    askEdge: askEdge,
    gap: askEdge - bidEdge,
    midGap: midGap,
    lowCut: 0.1 * midGap, // Low cutoff of 10% market.
    highCut: midGap * 2 // High cutoff + 100%
  }
}

const dummyOrderbook = {
  pts: [[0, 0, 0]],
  outliers: {
    asks: [],
    bids: []
  }
}

function rangedPts (pts, cutoff) {
  const l = []
  const outliers = []
  pts.forEach(pt => {
    if (cutoff(pt)) {
      outliers.push(pt)
      return
    }
    l.push(pt)
  })
  return { pts: l, outliers: outliers }
}

function translateDepthSide (pts, idx, cutoff) {
  const sorted = rangedPts(pts, cutoff)
  let accumulator = 0
  const translated = sorted.pts.map(pt => {
    accumulator += pt.quantity
    pt = [pt.price, null, null]
    pt[idx] = accumulator
    return pt
  })
  return { pts: translated, outliers: sorted.outliers }
}

function translateOrderbookSide (pts, idx, cutoff) {
  const sorted = rangedPts(pts, cutoff)
  const translated = sorted.pts.map(pt => {
    const l = [pt.price, null, null]
    l[idx] = pt.quantity
    return l
  })
  return { pts: translated, outliers: sorted.outliers }
}

function processOrderbook (response, translator) {
  const bids = response.data.bids
  const asks = response.data.asks
  if (!bids || !asks) {
    console.warn('no bid/ask data in API response')
    return dummyOrderbook
  }
  if (!bids.length || !asks.length) {
    console.warn('empty bid/ask data in API response')
    return dummyOrderbook
  }
  // Add the dummy points to make the chart line connect to the baseline and
  // because otherwise Dygraph has a bug that adds an offset to the asks side.
  bids.splice(0, 0, { price: bids[0].price + 1e-8, quantity: 0 })
  asks.splice(0, 0, { price: asks[0].price - 1e-8, quantity: 0 })
  const stats = orderbookStats(bids, asks)
  const buys = translator(bids, BUY, pt => pt.price < stats.lowCut)
  buys.pts.reverse()
  const sells = translator(asks, SELL, pt => pt.price > stats.highCut)

  return {
    pts: buys.pts.concat(sells.pts),
    outliers: buys.outliers.concat(sells.outliers),
    stats: stats
  }
}

function candlestickPlotter (e) {
  if (e.seriesIndex !== 0) return
  const area = e.plotArea
  const ctx = e.drawingContext
  ctx.strokeStyle = chartStroke
  ctx.lineWidth = 1
  const sets = e.allSeriesPoints
  if (sets.length < 2) {
    // do nothing
    return
  }

  const barWidth = area.w * Math.abs(sets[0][1].x - sets[0][0].x) * 0.8
  const [opens, closes, highs, lows] = sets
  let open, close, high, low
  for (let i = 0; i < sets[0].length; i++) {
    ctx.strokeStyle = '#777'
    open = opens[i]
    close = closes[i]
    high = highs[i]
    low = lows[i]
    const centerX = area.x + open.x * area.w
    const topY = area.h * high.y + area.y
    const bottomY = area.h * low.y + area.y
    ctx.beginPath()
    ctx.moveTo(centerX, topY)
    ctx.lineTo(centerX, bottomY)
    ctx.stroke()
    ctx.strokeStyle = 'black'
    let top
    if (open.yval > close.yval) {
      ctx.fillStyle = '#f93f39cc'
      top = area.h * open.y + area.y
    } else {
      ctx.fillStyle = '#1acc84cc'
      top = area.h * close.y + area.y
    }
    const h = area.h * Math.abs(open.y - close.y)
    const left = centerX - barWidth / 2
    ctx.fillRect(left, top, barWidth, h)
    ctx.strokeRect(left, top, barWidth, h)
  }
}

function drawOrderPt (ctx, pt) {
  return drawPt(ctx, pt, orderPtSize, true)
}

function drawPt (ctx, pt, size, bordered) {
  ctx.beginPath()
  ctx.arc(pt.x, pt.y, size, 0, PIPI)
  ctx.fill()
  if (bordered) ctx.stroke()
}

function drawLine (ctx, start, end) {
  ctx.beginPath()
  ctx.moveTo(start.x, start.y)
  ctx.lineTo(end.x, end.y)
  ctx.stroke()
}

function makePt (x, y) { return { x, y } }

function canvasXY (area, pt) {
  return {
    x: area.x + pt.x * area.w,
    y: area.y + pt.y * area.h
  }
}

let orderPtSize = 7
if (!screenIsBig()) orderPtSize = 4

function orderPlotter (e) {
  if (e.seriesIndex !== 0) return

  const area = e.plotArea
  const ctx = e.drawingContext

  // let buyColor, sellColor
  const [buyColor, sellColor] = e.dygraph.getColors()

  const [buys, sells] = e.allSeriesPoints
  ctx.lineWidth = 1
  ctx.strokeStyle = darkEnabled() ? 'black' : 'white'
  for (let i = 0; i < buys.length; i++) {
    const buy = buys[i]
    const sell = sells[i]
    if (buy) {
      ctx.fillStyle = buyColor
      drawOrderPt(ctx, canvasXY(area, buy))
    }
    if (sell) {
      ctx.fillStyle = sellColor
      drawOrderPt(ctx, canvasXY(area, sell))
    }
  }
}

const greekCapDelta = String.fromCharCode(916)

function depthLegendPlotter (e) {
  const stats = e.dygraph.getOption('stats')

  const area = e.plotArea
  const ctx = e.drawingContext

  const dark = darkEnabled()
  const big = screenIsBig()
  const mg = e.dygraph.toDomCoords(stats.midGap, 0)
  const midGap = makePt(mg[0], mg[1])
  const fontSize = big ? 15 : 13
  ctx.textAlign = 'left'
  ctx.textBaseline = 'top'
  ctx.font = `${fontSize}px arial`
  ctx.lineWidth = 1
  ctx.strokeStyle = chartStroke
  const boxColor = dark ? '#2228' : '#fff8'

  const midGapPrice = humanize.threeSigFigs(stats.midGap)
  const deltaPctTxt = `${greekCapDelta} : ${humanize.threeSigFigs(stats.gap / stats.midGap * 100)}%`
  const fiatGapTxt = `${humanize.threeSigFigs(stats.gap * currentPairFiatPrice())} ${fiatCode}`
  const btcGapTxt = `${humanize.threeSigFigs(stats.gap)} ${quoteAsset(settings.pair)}`
  let boxW = 0
  const txts = [fiatGapTxt, btcGapTxt, deltaPctTxt, midGapPrice]
  txts.forEach(txt => {
    const w = ctx.measureText(txt).width
    if (w > boxW) boxW = w
  })
  let rowHeight = fontSize * 1.5
  const rowPad = big ? (rowHeight - fontSize) / 2 : (rowHeight - fontSize) / 3
  const boxPad = big ? rowHeight / 3 : rowHeight / 5
  let y = big ? fontSize * 2 : fontSize
  y += area.h / 4
  const x = midGap.x - boxW / 2 - 25
  // Label the gap size.
  rowHeight -= 2 // just looks better
  ctx.fillStyle = boxColor
  const rect = makePt(x - boxPad, y - boxPad)
  const dims = makePt(boxW + boxPad * 3, rowHeight * 4 + boxPad * 2)
  ctx.fillRect(rect.x, rect.y, dims.x, dims.y)
  ctx.strokeRect(rect.x, rect.y, dims.x, dims.y)
  ctx.fillStyle = chartStroke
  const centerX = x + (boxW / 2)
  const write = s => {
    const cornerX = centerX - (ctx.measureText(s).width / 2)
    ctx.fillText(s, cornerX + rowPad, y + rowPad)
    y += rowHeight
  }

  ctx.save()
  ctx.font = `bold ${fontSize}px arial`
  write(midGapPrice)
  ctx.restore()
  write(deltaPctTxt)
  write(fiatGapTxt)
  write(btcGapTxt)

  // Draw a line from the box to the gap
  drawLine(ctx,
    makePt(x + boxW / 2, y + boxPad * 2 + boxPad),
    makePt(midGap.x, midGap.y - boxPad))
}

function depthPlotter (e) {
  Dygraph.Plotters.fillPlotter(e)
  Dygraph.Plotters.linePlotter(e)

  // Callout box with color legend
  if (e.seriesIndex === e.allSeriesPoints.length - 1) depthLegendPlotter(e)
}

let stickZoom
function calcStickWindow (start, end, bin) {
  const halfBin = minuteMap[bin] / 2
  start = new Date(start.getTime())
  end = new Date(end.getTime())
  return [
    start.setMinutes(start.getMinutes() - halfBin),
    end.setMinutes(end.getMinutes() + halfBin)
  ]
}

function isValidExchange (xc) {
  return xc === 'binance' || xc === 'dcrdex' || xc === 'poloniex' ||
  xc === 'bittrex' || xc === 'huobi' || xc === 'dragonex' || xc === 'mexc'
}

export default class extends Controller {
  static get targets () {
    return ['chartSelect', 'exchanges', 'bin', 'chart', 'legend', 'conversion',
      'xcName', 'xcLogo', 'actions', 'sticksOnly', 'depthOnly', 'chartLoader',
      'xcRow', 'xcIndex', 'price', 'age', 'ageSpan', 'link', 'zoom', 'marketName', 'marketSection']
  }

  async connect () {
    this.query = new TurboQuery()
    settings = TurboQuery.nullTemplate(['chart', 'xc', 'bin', 'pair'])
    this.query.update(settings)
    this.processors = {
      orders: this.processOrders,
      candlestick: this.processCandlesticks,
      history: this.processHistory,
      depth: this.processDepth.bind(this),
      volume: this.processVolume
    }
    commonChartOpts.labelsDiv = this.legendTarget
    this.converted = false
    indices = JSON.parse(this.conversionTarget.dataset.indices)
    fiatCode = this.conversionTarget.dataset.code
    this.binButtons = this.binTarget.querySelectorAll('button')
    this.lastUrl = null
    this.zoomButtons = this.zoomTarget.querySelectorAll('button')
    this.zoomCallback = this._zoomCallback.bind(this)

    availableCandlesticks = {}
    availableDepths = []
    let opts = this.exchangesTarget.options
    for (let i = 0; i < opts.length; i++) {
      const option = opts[i]
      if (option.dataset.sticks) {
        availableCandlesticks[option.value] = option.dataset.bins.split(';')
      }
      if (option.dataset.depth) availableDepths.push(option.value)
    }

    this.chartOptions = []
    opts = this.chartSelectTarget.options
    for (let i = 0; i < opts.length; i++) {
      this.chartOptions.push(opts[i])
    }

    if (settings.chart == null) {
      settings.chart = depth
    }
    if (!isValidExchange(settings.xc)) {
      settings.xc = 'binance'
    }
    if (!isValidDCRPair(settings.pair)) {
      settings.pair = CurrencyPairDCRUSDT
    }
    this.setExchangeName()
    if (settings.bin == null) {
      settings.bin = anHour
    }

    this.setButtons()

    this.resize = this._resize.bind(this)
    window.addEventListener('resize', this.resize)
    this.tabVis = this._tabVis.bind(this)
    document.addEventListener(visibilityChange, this.tabVis)
    this.processNightMode = this._processNightMode.bind(this)
    globalEventBus.on('NIGHT_MODE', this.processNightMode)
    this.processXcUpdate = this._processXcUpdate.bind(this)
    globalEventBus.on('EXCHANGE_UPDATE', this.processXcUpdate)
    if (darkEnabled()) chartStroke = darkStroke

    this.setNameDisplay()
    this.fetchInitialData()
  }

  disconnect () {
    responseCache = {}
    window.removeEventListener('resize', this.resize)
    document.removeEventListener(visibilityChange, this.tabVis)
    globalEventBus.off('NIGHT_MODE', this.processNightMode)
    globalEventBus.off('EXCHANGE_UPDATE', this.processXcUpdate)
  }

  _resize () {
    if (this.graph) {
      orderPtSize = screenIsBig() ? 7 : 4
      this.graph.resize()
    }
    this.setNameDisplay()
  }

  setNameDisplay () {
    if (screenIsBig()) {
      this.xcNameTarget.classList.remove('d-hide')
    } else {
      this.xcNameTarget.classList.add('d-hide')
    }
  }

  _tabVis () {
    focused = !document[hidden]
    if (focused && refreshAvailable) this.refreshChart()
  }

  async fetchInitialData () {
    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )
    const dummyGraph = new Dygraph(document.createElement('div'), [[0, 1]], { labels: ['', ''] })

    // A little hack to start with the default interaction model. Updating the
    // interactionModel with updateOptions later does not appear to work.
    const model = dummyGraph.getOption('interactionModel')
    model.wheel = gScroll
    model.mousedown = (event, g, context) => {
      // End panning even if the mouseup event is not on the chart.
      const mouseup = () => {
        context.isPanning = false
        document.removeEventListener('mouseup', mouseup)
      }
      document.addEventListener('mouseup', mouseup)
      context.initializeMouseDown(event, g, context)
      Dygraph.startPan(event, g, context)
    }
    model.mouseup = (event, g, context) => {
      if (!context.isPanning) return
      Dygraph.endPan(event, g, context)
      context.isPanning = false // I think Dygraph is supposed to set this, but they don't.
      const zoomCallback = g.getOption('zoomCallback')
      if (zoomCallback) {
        const range = g.xAxisRange()
        zoomCallback(range[0], range[1], g.yAxisRanges())
      }
    }
    model.mousemove = (event, g, context) => {
      if (!context.isPanning) return
      Dygraph.movePan(event, g, context)
    }
    commonChartOpts.interactionModel = model

    this.graph = new Dygraph(this.chartTarget, [[0, 0], [0, 1]], commonChartOpts)
    this.fetchChart()
  }

  async fetchChart (isRefresh) {
    let url = null
    requestCounter++
    const thisRequest = requestCounter
    const bin = settings.bin
    const xc = settings.xc
    const cacheKey = this.xcTokenAndPair()
    const chart = settings.chart
    const oldZoom = this.graph.xAxisRange()
    if (usesCandlesticks(chart)) {
      if (!(cacheKey in availableCandlesticks)) {
        console.warn('invalid candlestick exchange:', cacheKey)
        return
      }
      if (availableCandlesticks[cacheKey].indexOf(bin) === -1) {
        console.warn('invalid bin:', bin)
        return
      }
      url = `/api/chart/market/${xc}/candlestick/${bin}?currencyPair=${settings.pair}`
    } else if (usesOrderbook(chart)) {
      if (!validDepthExchange(cacheKey)) {
        console.warn('invalid depth exchange:', cacheKey)
        return
      }
      url = `/api/chart/market/${xc}/depth?currencyPair=${settings.pair}`
    }
    if (!url) {
      console.warn('invalid chart:', chart)
      return
    }

    this.chartLoaderTarget.classList.add('loading')

    let response
    if (hasCache(url)) {
      response = responseCache[url]
    } else {
      // response = await axios.get(url)
      response = await requestJSON(url)
      responseCache[url] = response
      if (thisRequest !== requestCounter) {
        // new request was issued while waiting.
        this.chartLoaderTarget.classList.remove('loading')
        return
      }
    }
    // Fiat conversion only available for order books for now.
    if (usesOrderbook(chart)) {
      this.conversionTarget.classList.remove('d-hide')
      this.ageSpanTarget.dataset.age = response.data.time
      this.ageSpanTarget.textContent = humanize.timeSince(response.data.time)
      this.ageTarget.classList.remove('d-hide')
    } else {
      this.conversionTarget.classList.add('d-hide')
      this.ageTarget.classList.add('d-hide')
    }
    this.graph.updateOptions(chartResetOpts, true)
    this.graph.updateOptions(this.processors[chart](response))
    this.query.replace(settings)
    if (isRefresh) this.graph.updateOptions({ dateWindow: oldZoom })
    else this.resetZoom()
    this.chartLoaderTarget.classList.remove('loading')
    this.lastUrl = url
    refreshAvailable = false
  }

  xcTokenAndPair () {
    return settings.xc + ':' + settings.pair
  }

  processCandlesticks (response) {
    const halfDuration = minuteMap[settings.bin] / 2
    const data = response.sticks.map(stick => {
      const t = new Date(stick.start)
      t.setMinutes(t.getMinutes() + halfDuration)
      return [t, stick.open, stick.close, stick.high, stick.low]
    })
    if (data.length === 0) return
    // limit to 50 points to start. Too many candlesticks = bad.
    let start = data[0][0]
    if (data.length > 50) {
      start = data[data.length - 50][0]
    }
    stickZoom = calcStickWindow(start, data[data.length - 1][0], settings.bin)
    return {
      file: data,
      labels: ['time', 'open', 'close', 'high', 'low'],
      xlabel: 'Time',
      ylabel: `Price (${quoteAsset(settings.pair)})`,
      plotter: candlestickPlotter,
      axes: {
        x: {
          axisLabelFormatter: Dygraph.dateAxisLabelFormatter
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs,
          valueFormatter: humanize.threeSigFigs
        }
      }
    }
  }

  processHistory (response) {
    const halfDuration = minuteMap[settings.bin] / 2
    return {
      file: response.sticks.map(stick => {
        const t = new Date(stick.start)
        t.setMinutes(t.getMinutes() + halfDuration)
        // Not sure what the best way to reduce a candlestick to a single number
        // Trying this simple approach for now.
        const avg = (stick.open + stick.close + stick.high + stick.low) / 4
        return [t, avg]
      }),
      labels: ['time', 'price'],
      xlabel: 'Time',
      ylabel: `Price (${quoteAsset(settings.pair)})`,
      colors: [chartStroke],
      plotter: Dygraph.Plotters.linePlotter,
      axes: {
        x: {
          axisLabelFormatter: Dygraph.dateAxisLabelFormatter
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs,
          valueFormatter: humanize.threeSigFigs
        }
      },
      strokeWidth: 3
    }
  }

  processVolume (response) {
    const halfDuration = minuteMap[settings.bin] / 2
    return {
      file: response.sticks.map(stick => {
        const t = new Date(stick.start)
        t.setMinutes(t.getMinutes() + halfDuration)
        return [t, stick.volume]
      }),
      labels: ['time', 'volume'],
      xlabel: 'Time',
      ylabel: `Volume (DCR / ${prettyDurations[settings.bin]})`,
      colors: [chartStroke],
      plotter: Dygraph.Plotters.linePlotter,
      axes: {
        x: {
          axisLabelFormatter: Dygraph.dateAxisLabelFormatter
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs,
          valueFormatter: humanize.threeSigFigs
        }
      },
      strokeWidth: 3
    }
  }

  processDepth (response) {
    const data = processOrderbook(response, translateDepthSide)
    return {
      labels: ['price', 'cumulative sell', 'cumulative buy'],
      file: data.pts,
      fillGraph: true,
      colors: ['#ed6d47', '#41be53'],
      xlabel: `Price (${this.converted ? fiatCode : quoteAsset(settings.pair)})`,
      ylabel: 'Volume (DCR)',
      stats: data.stats,
      plotter: depthPlotter, // Don't use Dygraph.linePlotter here. fillGraph won't work.
      zoomCallback: this.zoomCallback,
      axes: {
        x: {
          axisLabelFormatter: convertedThreeSigFigs,
          valueFormatter: convertedEightDecimals
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs,
          valueFormatter: humanize.threeSigFigs
        }
      }
    }
  }

  processOrders (response) {
    const data = processOrderbook(response, translateOrderbookSide)
    return {
      labels: ['price', 'sell', 'buy'],
      file: data.pts,
      colors: ['#f93f39cc', '#1acc84cc'],
      xlabel: `Price (${this.converted ? fiatCode : quoteAsset(settings.pair)})`,
      ylabel: 'Volume (DCR)',
      plotter: orderPlotter,
      stats: data.stats,
      axes: {
        x: {
          axisLabelFormatter: convertedThreeSigFigs
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs,
          valueFormatter: humanize.threeSigFigs
        }
      },
      strokeWidth: 0,
      drawPoints: true,
      logscale: true,
      xRangePad: 15,
      yRangePad: 15
    }
  }

  justifyBins () {
    const bins = availableCandlesticks[this.xcTokenAndPair()]
    if (bins.indexOf(settings.bin) === -1) {
      settings.bin = bins[0]
      this.setBinSelection()
    }
  }

  setButtons () {
    this.chartSelectTarget.value = settings.chart
    this.exchangesTarget.value = this.xcTokenAndPair()
    if (usesOrderbook(settings.chart)) {
      this.binTarget.classList.add('d-hide')
      this.zoomTarget.classList.remove('d-hide')
    } else {
      this.binTarget.classList.remove('d-hide')
      this.zoomTarget.classList.add('d-hide')
      this.binButtons.forEach(button => {
        if (hasBin(this.exchangesTarget.value, button.name)) {
          button.classList.remove('d-hide')
        } else {
          button.classList.add('d-hide')
        }
      })
      this.setBinSelection()
    }
    const sticksDisabled = !availableCandlesticks[this.exchangesTarget.value]
    this.sticksOnlyTargets.forEach(option => {
      option.disabled = sticksDisabled
    })
    const depthDisabled = !validDepthExchange(this.exchangesTarget.value)
    this.depthOnlyTargets.forEach(option => {
      option.disabled = depthDisabled
    })
  }

  setBinSelection () {
    const bin = settings.bin
    this.binButtons.forEach(button => {
      if (button.name === bin) {
        button.classList.add('btn-selected')
      } else {
        button.classList.remove('btn-selected')
      }
    })
  }

  changeGraph (e) {
    const target = e.target || e.srcElement
    settings.chart = target.value
    if (usesCandlesticks(settings.chart)) {
      this.justifyBins()
    }
    this.setButtons()
    this.fetchChart()
  }

  changeExchange () {
    settings.xc = this.exchangesTarget.value.split(':')[0]
    settings.pair = this.exchangesTarget.value.split(':')[1]
    this.setExchangeName()
    if (usesCandlesticks(settings.chart)) {
      if (!availableCandlesticks[this.exchangesTarget.value]) {
        // exchange does not have candlestick data
        // show the depth chart.
        settings.chart = depth
      } else {
        this.justifyBins()
      }
    }
    this.setButtons()
    this.fetchChart()
    this.resetZoom()
  }

  setExchange (e) {
    let node = e.target || e.srcElement
    while (node && node.nodeName !== 'TR') node = node.parentNode
    if (!node || !node.dataset || !node.dataset.token) return
    settings.xc = node.dataset.token
    settings.pair = node.dataset.pair
    this.exchangesTarget.value = this.xcTokenAndPair()
    this.changeExchange()
  }

  changeBin (e) {
    const btn = e.target || e.srcElement
    if (btn.nodeName !== 'BUTTON' || !this.graph) return
    settings.bin = btn.name
    this.justifyBins()
    this.setBinSelection()
    this.fetchChart()
  }

  resetZoom () {
    if (settings.chart === candlestick) {
      this.graph.updateOptions({ dateWindow: stickZoom })
    } else if (usesOrderbook(settings.chart)) {
      this.setZoomPct(defaultZoomPct)
    } else {
      this.graph.resetZoom()
    }
  }

  refreshChart () {
    refreshAvailable = true
    if (!focused) {
      return
    }
    this.fetchChart(true)
  }

  setConversion (e) {
    const btn = e.target || e.srcElement
    if (btn.nodeName !== 'BUTTON' || !this.graph) return
    this.conversionTarget.querySelectorAll('button').forEach(b => b.classList.remove('btn-selected'))
    btn.classList.add('btn-selected')
    this.updateConversion(e.target.name)
  }

  updateConversion (targetName) {
    if (!this.graph) return
    let cLabel = quoteAsset(settings.pair)
    if (targetName === cLabel) {
      this.converted = false
      conversionFactor = 1
    } else {
      this.converted = true
      conversionFactor = currentPairFiatPrice()
      cLabel = fiatCode
    }
    this.graph.updateOptions({ xlabel: `Price (${cLabel})` })
  }

  setExchangeName () {
    this.xcLogoTarget.className = `exchange-logo ${settings.xc} me-2`
    const prettyName = printName(settings.xc)
    this.xcNameTarget.textContent = prettyName
    let href
    if (settings.pair === CurrencyPairDCRUSDT) href = exchangeLinks.CurrencyPairDCRUSDT[settings.xc]
    else href = exchangeLinks.CurrencyPairDCRBTC[settings.xc]
    if (href) {
      this.linkTarget.href = href
      this.linkTarget.textContent = `Visit ${prettyName}`
      this.actionsTarget.classList.remove('d-hide')
    } else {
      this.actionsTarget.classList.add('d-hide')
    }
    this.conversionTarget.querySelectorAll('button').forEach(b => {
      if (b.textContent !== fiatCode) {
        b.name = quoteAsset(settings.pair)
        b.textContent = b.name
        b.classList.add('btn-selected')
        this.updateConversion(b.textContent)
      } else {
        b.classList.remove('btn-selected')
      }
    })
  }

  _processNightMode (data) {
    if (!this.graph) return
    chartStroke = data.nightMode ? darkStroke : lightStroke
    if (settings.chart === history || settings.chart === volume) {
      this.graph.updateOptions({ colors: [chartStroke] })
    }
    if (settings.chart === orders || settings.chart === depth) {
      this.graph.setAnnotations([])
    }
  }

  getExchangeRow (token, pair) {
    const rows = this.xcRowTargets
    for (let i = 0; i < rows.length; i++) {
      const tr = rows[i]
      const hasPair = tr.dataset.pair !== undefined && tr.dataset.pair !== null && tr.dataset.pair === pair
      if ((hasPair && tr.dataset.token === token) || tr.dataset.token === token) {
        const row = {}
        tr.querySelectorAll('td').forEach(td => {
          switch (td.dataset.type) {
            case 'price':
              row.price = td
              break
            case 'volume':
              row.volume = td
              break
            case 'fiat':
              row.fiat = td
              break
            case 'arrow':
              row.arrow = td.querySelector('span')
              break
          }
        })
        return row
      }
    }
    return null
  }

  setZoom (e) {
    const btn = e.target || e.srcElement
    if (btn.nodeName !== 'BUTTON' || !this.graph) return
    this.setZoomPct(parseInt(btn.name))
    const stats = this.graph.getOption('stats')
    const spread = stats.midGap * parseFloat(btn.name) / 100
    this.graph.updateOptions({ dateWindow: [stats.midGap - spread, stats.midGap + spread] })
  }

  setZoomPct (pct) {
    this.zoomButtons.forEach(b => {
      if (parseInt(b.name) === pct) b.classList.add('btn-selected')
      else b.classList.remove('btn-selected')
    })
    const stats = this.graph.getOption('stats')
    const spread = stats.midGap * pct / 100
    let low = stats.midGap - spread
    let high = stats.midGap + spread
    const [min, max] = this.graph.xAxisExtremes()
    if (low < min) low = min
    if (high > max) high = max
    this.graph.updateOptions({ dateWindow: [low, high] })
  }

  _zoomCallback () {
    this.zoomButtons.forEach(b => b.classList.remove('btn-selected'))
  }

  _processXcUpdate (update) {
    const xc = update.updater
    indices = update.indices
    if (update.fiat) { // btc-fiat exchange update
      if (xc.pair === BTCIndex) { // we also receive updates for USDTIndex but we don't use it atm.
        this.xcIndexTargets.forEach(span => {
          if (span.dataset.token === xc.token) {
            span.textContent = humanize.commaWithDecimal(xc.price, 2)
          }
        })
      }
    } else { // dcr-{Asset} exchange update
      const row = this.getExchangeRow(xc.token, xc.pair)
      row.volume.textContent = humanize.threeSigFigs(xc.volume)
      row.price.textContent = humanize.threeSigFigs(xc.price)
      if (xc.pair === CurrencyPairDCRBTC) {
        row.fiat.textContent = (xc.price * indices[BTCIndex]).toFixed(2)
      } else if (xc.pair === CurrencyPairDCRUSDT) {
        row.fiat.textContent = (xc.price * indices[USDTIndex]).toFixed(2)
      }
      if (xc.change === 0) {
        row.arrow.className = ''
      } else if (xc.change > 0) {
        row.arrow.className = 'dcricon-arrow-up text-green'
      } else {
        row.arrow.className = 'dcricon-arrow-down text-danger'
      }
    }
    // Update the big displayed value and the aggregated row
    const fmtPrice = update.price.toFixed(2)
    this.priceTarget.textContent = fmtPrice
    const aggRow = this.getExchangeRow(aggregatedKey)
    const btcPrice = indices[BTCIndex]
    aggRow.price.textContent = humanize.threeSigFigs(update.price / btcPrice)
    aggRow.volume.textContent = humanize.threeSigFigs(update.volume)
    aggRow.fiat.textContent = fmtPrice
    if (usesOrderbook(settings.chart)) {
      clearCache(this.lastUrl)
      this.refreshChart()
    } else if (usesCandlesticks(settings.chart)) {
      // Only show refresh button if cache is expired
      if (!hasCache(this.lastUrl)) {
        this.refreshChart()
      }
    }
  }
}
