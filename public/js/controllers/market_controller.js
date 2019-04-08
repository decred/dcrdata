import { Controller } from 'stimulus'
import TurboQuery from '../helpers/turbolinks_helper'
import { getDefault } from '../helpers/module_helper'
import humanize from '../helpers/humanize_helper'
import { darkEnabled } from '../services/theme_service'
import globalEventBus from '../services/event_bus_service'
import axios from 'axios'

var Dygraph
const candlestick = 'candlestick'
const orders = 'orders'
const depth = 'depth'
const history = 'history'
const volume = 'volume'
const binance = 'binance'
const anHour = '1h'
const minuteMap = {
  '30m': 30,
  '1h': 60,
  '1d': 1440,
  '1mo': 43200
}
const PIPI = 2 * Math.PI
const prettyDurations = {
  '30m': '30 min',
  '1h': 'hour',
  '1d': 'day',
  '1mo': 'month'
}
const exchangeLinks = {
  binance: 'https://www.binance.com/en/trade/DCR_BTC',
  bittrex: 'https://bittrex.com/Market/Index?MarketName=BTC-DCR',
  poloniex: 'https://poloniex.com/exchange#btc_dcr',
  dragonex: 'https://dragonex.io/en-us/trade/index/dcr_btc',
  huobi: 'https://www.hbg.com/en-us/exchange/?s=dcr_btc'
}
var hidden, visibilityChange
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
var focused = true
var refreshAvailable = false
var availableCandlesticks, availableDepths

function screenIsBig () {
  return window.screen.availWidth >= 992
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

var requestCounter = 0
var responseCache = {}

function hasCache (k) {
  if (!responseCache[k]) return false
  var expiration = new Date(responseCache[k].data.expiration)
  return expiration > new Date()
}

function clearCache (k) {
  if (!responseCache[k]) return
  delete responseCache[k]
}

const lightStroke = '#333'
const darkStroke = '#ddd'
var chartStroke = lightStroke
var conversionFactor = 1
var gridColor = '#7774'
var settings = {}

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
  yRangePad: 0
}

function adjustAxis (axis, zoomInPercentage, bias) {
  var delta = axis[1] - axis[0]
  var increment = delta * zoomInPercentage
  var foo = [increment * bias, increment * (1 - bias)]
  return [axis[0] + foo[0], axis[1] - foo[1]]
}

function gScroll (event, g, context) {
  var percentage = event.detail ? event.detail * -1 / 1000 : event.wheelDelta / 1000

  if (!(event.offsetX && event.offsetY)) {
    event.offsetX = event.layerX - event.target.offsetLeft
    event.offsetY = event.layerY - event.target.offsetTop
  }

  var xOffset = g.toDomCoords(g.xAxisRange()[0], null)[0]
  var x = event.offsetX - xOffset
  var w = g.toDomCoords(g.xAxisRange()[1], null)[0] - xOffset
  var xPct = w === 0 ? 0 : (x / w)

  g.updateOptions({
    dateWindow: adjustAxis(g.xAxisRange(), percentage, xPct)
  })
  event.preventDefault()
  event.stopPropagation()
}

function candlestickStats (bids, asks) {
  var bidEdge = bids[0].price
  var askEdge = asks[0].price
  return {
    bidEdge: bidEdge,
    askEdge: askEdge,
    gap: askEdge - bidEdge,
    midGap: (bidEdge + askEdge) / 2
  }
}

var dummyOrderbook = {
  pts: [[0, 0, 0]],
  outliers: {
    asks: [],
    bids: []
  }
}

function processOrderbook (response, accumulate) {
  var pts = []
  var accumulator = 0
  var bids = response.data.bids
  var asks = response.data.asks
  if (!bids || !asks) {
    console.warn('no bid/ask data in API response')
    return dummyOrderbook
  }
  if (!bids.length || !asks.length) {
    console.warn('empty bid/ask data in API response')
    return dummyOrderbook
  }
  var stats = candlestickStats(bids, asks)
  // Just track outliers for now. May display value in the future.
  var outliers = {
    asks: [],
    bids: []
  }
  var cutoff = 0.1 * stats.midGap // Low cutoff of 10% market.
  bids.forEach(pt => {
    if (pt.price < cutoff) {
      outliers.bids.push(pt)
      return
    }
    accumulator = accumulate ? accumulator + pt.quantity : pt.quantity
    pts.push([pt.price, null, accumulator])
  })
  pts.reverse()
  accumulator = 0
  cutoff = stats.midGap * 2 // Hard cutoff of 2 * market price
  asks.forEach(pt => {
    if (pt.price > cutoff) {
      outliers.asks.push(pt)
      return
    }
    accumulator = accumulate ? accumulator + pt.quantity : pt.quantity
    pts.push([pt.price, accumulator, null])
  })
  return {
    pts: pts,
    outliers: outliers
  }
}

function candlestickPlotter (e) {
  if (e.seriesIndex !== 0) return

  var area = e.plotArea
  var ctx = e.drawingContext
  ctx.strokeStyle = chartStroke
  ctx.lineWidth = 1

  var sets = e.allSeriesPoints
  if (sets.length < 2) {
    // do nothing
    return
  }

  var barWidth = area.w * Math.abs(sets[0][1].x - sets[0][0].x) * 0.8
  let opens, closes, highs, lows
  [opens, closes, highs, lows] = sets
  let open, close, high, low
  for (let i = 0; i < sets[0].length; i++) {
    ctx.strokeStyle = '#777'
    open = opens[i]
    close = closes[i]
    high = highs[i]
    low = lows[i]
    var centerX = area.x + open.x * area.w
    var topY = area.h * high.y + area.y
    var bottomY = area.h * low.y + area.y
    ctx.beginPath()
    ctx.moveTo(centerX, topY)
    ctx.lineTo(centerX, bottomY)
    ctx.stroke()

    ctx.strokeStyle = 'black'

    var top
    if (open.yval > close.yval) {
      ctx.fillStyle = '#f93f39cc'
      top = area.h * open.y + area.y
    } else {
      ctx.fillStyle = '#1acc84cc'
      top = area.h * close.y + area.y
    }
    var h = area.h * Math.abs(open.y - close.y)
    var left = centerX - barWidth / 2
    ctx.fillRect(left, top, barWidth, h)
    ctx.strokeRect(left, top, barWidth, h)
  }
}

function drawOrderPt (ctx, pt, r) {
  ctx.beginPath()
  ctx.arc(pt.x, pt.y, r, 0, PIPI)
  ctx.fill()
  // ctx.beginPath()
  // ctx.arc(pt.x, pt.y, r, 0, PIPI)
  ctx.stroke()
}

function orderXY (area, pt) {
  return {
    x: area.x + pt.x * area.w,
    y: area.y + pt.y * area.h
  }
}

var orderPtSize = 7
if (!screenIsBig()) orderPtSize = 4

function orderPlotter (e) {
  if (e.seriesIndex !== 0) return

  var area = e.plotArea
  var ctx = e.drawingContext

  let buyColor, sellColor
  [buyColor, sellColor] = e.dygraph.getColors()

  let buys, sells
  [buys, sells] = e.allSeriesPoints
  ctx.lineWidth = 1
  ctx.strokeStyle = darkEnabled() ? 'black' : 'white'
  for (let i = 0; i < buys.length; i++) {
    let buy = buys[i]
    let sell = sells[i]
    if (buy) {
      ctx.fillStyle = buyColor
      drawOrderPt(ctx, orderXY(area, buy), orderPtSize)
    }
    if (sell) {
      ctx.fillStyle = sellColor
      drawOrderPt(ctx, orderXY(area, sell), orderPtSize)
    }
  }
}

var stickZoom
function calcStickWindow (start, end, bin) {
  var halfBin = minuteMap[bin] / 2
  start = new Date(start.getTime())
  end = new Date(end.getTime())
  return [
    start.setMinutes(start.getMinutes() - halfBin),
    end.setMinutes(end.getMinutes() + halfBin)
  ]
}

export default class extends Controller {
  static get targets () {
    return ['chartSelect', 'exchanges', 'bin', 'chart', 'legend', 'conversion',
      'xcName', 'xcLogo', 'actions', 'sticksOnly', 'depthOnly', 'chartLoader',
      'xcRow', 'xcIndex', 'price', 'age', 'ageSpan', 'link']
  }

  async connect () {
    this.query = new TurboQuery()
    settings = TurboQuery.nullTemplate(['chart', 'xc', 'bin'])
    this.query.update(settings)
    this.processors = {
      orders: this.processOrders,
      candlestick: this.processCandlesticks,
      history: this.processHistory,
      depth: this.processDepth,
      volume: this.processVolume
    }
    commonChartOpts.labelsDiv = this.legendTarget
    this.converted = false
    this.conversionFactor = parseFloat(this.conversionTarget.dataset.factor)
    this.currencyCode = this.conversionTarget.dataset.code
    this.binButtons = this.binTarget.querySelectorAll('button')
    this.lastUrl = null

    availableCandlesticks = {}
    availableDepths = []
    this.exchangeOptions = []
    var opts = this.exchangesTarget.options
    for (let i = 0; i < opts.length; i++) {
      let option = opts[i]
      this.exchangeOptions.push(option)
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
    if (settings.xc == null) {
      settings.xc = binance
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
  }

  _tabVis () {
    focused = !document[hidden]
    if (focused && refreshAvailable) this.refreshChart()
  }

  async fetchInitialData () {
    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )
    var dummyGraph = new Dygraph(document.createElement('div'), [[0, 1]], { labels: ['', ''] })

    // A little hack to start with the default interaction model. Updating the
    // interactionModel with updateOptions later does not appear to work.
    var model = dummyGraph.getOption('interactionModel')
    model.mousewheel = gScroll
    model.mousedown = (event, g, context) => {
      // End panning even if the mouseup event is not on the chart.
      var mouseup = () => {
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
    var url = null
    requestCounter++
    var thisRequest = requestCounter
    var bin = settings.bin
    var xc = settings.xc
    var chart = settings.chart
    if (usesCandlesticks(chart)) {
      if (!(xc in availableCandlesticks)) {
        console.warn('invalid candlestick exchange:', xc)
        return
      }
      if (availableCandlesticks[xc].indexOf(bin) === -1) {
        console.warn('invalid bin:', bin)
        return
      }
      url = `/api/chart/market/${xc}/candlestick/${bin}`
    } else if (usesOrderbook(chart)) {
      if (!validDepthExchange(xc)) {
        console.warn('invalid depth exchange:', xc)
        return
      }
      url = `/api/chart/market/${xc}/depth`
    }
    if (!url) {
      console.warn('invalid chart:', chart)
      return
    }

    this.chartLoaderTarget.classList.add('loading')

    var response
    if (hasCache(url)) {
      response = responseCache[url]
    } else {
      response = await axios.get(url)
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
      this.ageSpanTarget.dataset.age = response.data.data.time
      this.ageSpanTarget.textContent = humanize.timeSince(response.data.data.time)
      this.ageTarget.classList.remove('d-hide')
    } else {
      this.conversionTarget.classList.add('d-hide')
      this.ageTarget.classList.add('d-hide')
    }
    this.graph.updateOptions(chartResetOpts, true)
    this.graph.updateOptions(this.processors[chart](response.data))
    this.query.replace(settings)
    if (!isRefresh) this.resetZoom()
    this.chartLoaderTarget.classList.remove('loading')
    this.lastUrl = url
    refreshAvailable = false
  }

  processCandlesticks (response) {
    var halfDuration = minuteMap[settings.bin] / 2
    var data = response.sticks.map(stick => {
      var t = new Date(stick.start)
      t.setMinutes(t.getMinutes() + halfDuration)
      return [t, stick.open, stick.close, stick.high, stick.low]
    })
    if (data.length === 0) return
    // limit to 50 points to start. Too many candlesticks = bad.
    var start = data[0][0]
    if (data.length > 50) {
      start = data[data.length - 50][0]
    }
    stickZoom = calcStickWindow(start, data[data.length - 1][0], settings.bin)
    return {
      file: data,
      labels: ['time', 'open', 'close', 'high', 'low'],
      xlabel: 'Time',
      ylabel: `Price (BTC)`,
      plotter: candlestickPlotter,
      axes: {
        x: {
          axisLabelFormatter: Dygraph.dateAxisLabelFormatter
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs
        }
      }
    }
  }

  processHistory (response) {
    var halfDuration = minuteMap[settings.bin] / 2
    return {
      file: response.sticks.map(stick => {
        var t = new Date(stick.start)
        t.setMinutes(t.getMinutes() + halfDuration)
        // Not sure what the best way to reduce a candlestick to a single number
        // Trying this simple approach for now.
        var avg = (stick.open + stick.close + stick.high + stick.low) / 4
        return [t, avg]
      }),
      labels: ['time', 'price'],
      xlabel: 'Time',
      ylabel: `Price (BTC)`,
      colors: [chartStroke],
      plotter: Dygraph.Plotters.linePlotter,
      axes: {
        x: {
          axisLabelFormatter: Dygraph.dateAxisLabelFormatter
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs
        }
      },
      strokeWidth: 3
    }
  }

  processVolume (response) {
    var halfDuration = minuteMap[settings.bin] / 2
    return {
      file: response.sticks.map(stick => {
        var t = new Date(stick.start)
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
          axisLabelFormatter: humanize.threeSigFigs
        }
      },
      strokeWidth: 3
    }
  }

  processDepth (response) {
    var data = processOrderbook(response, true)
    return {
      labels: ['price', 'cumulative sell', 'cumulative buy'],
      file: data.pts,
      fillGraph: true,
      colors: ['#ed6d47', '#41be53'],
      xlabel: `Price (${this.converted ? this.currencyCode : 'BTC'})`,
      ylabel: 'Volume (DCR)',
      plotter: null, // Don't use Dygraph.linePlotter here. fillGraph won't work.
      axes: {
        x: {
          axisLabelFormatter: (x) => {
            return humanize.threeSigFigs(x * conversionFactor)
          }
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs
        }
      }
    }
  }

  processOrders (response) {
    var data = processOrderbook(response, false)
    return {
      labels: ['price', 'sell', 'buy'],
      file: data.pts,
      colors: ['#f93f39cc', '#1acc84cc'],
      xlabel: `Price (${this.converted ? this.currencyCode : 'BTC'})`,
      ylabel: 'Volume (DCR)',
      plotter: orderPlotter,
      axes: {
        x: {
          axisLabelFormatter: (x) => {
            return humanize.threeSigFigs(x * conversionFactor)
          }
        },
        y: {
          axisLabelFormatter: humanize.threeSigFigs
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
    var bins = availableCandlesticks[settings.xc]
    if (bins.indexOf(settings.bin) === -1) {
      settings.bin = bins[0]
      this.setBinSelection()
    }
  }

  setButtons () {
    this.chartSelectTarget.value = settings.chart
    this.exchangesTarget.value = settings.xc
    if (usesOrderbook(settings.chart)) {
      this.binTarget.classList.add('d-hide')
    } else {
      this.binTarget.classList.remove('d-hide')
      this.binButtons.forEach(button => {
        if (hasBin(settings.xc, button.name)) {
          button.classList.remove('d-hide')
        } else {
          button.classList.add('d-hide')
        }
      })
      this.setBinSelection()
    }
    var sticksDisabled = !availableCandlesticks[settings.xc]
    this.sticksOnlyTargets.forEach(option => {
      option.disabled = sticksDisabled
    })
    var depthDisabled = !validDepthExchange(settings.xc)
    this.depthOnlyTargets.forEach(option => {
      option.disabled = depthDisabled
    })
  }

  setBinSelection () {
    var bin = settings.bin
    this.binButtons.forEach(button => {
      if (button.name === bin) {
        button.classList.add('btn-selected')
      } else {
        button.classList.remove('btn-selected')
      }
    })
  }

  changeGraph (e) {
    var target = e.target || e.srcElement
    settings.chart = target.value
    if (usesCandlesticks(settings.chart)) {
      this.justifyBins()
    }
    this.setButtons()
    this.fetchChart()
  }

  changeExchange () {
    settings.xc = this.exchangesTarget.value
    this.setExchangeName()
    if (usesCandlesticks(settings.chart)) {
      if (!availableCandlesticks[settings.xc]) {
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
    var node = e.target || e.srcElement
    while (node && node.nodeName !== 'TR') node = node.parentNode
    if (!node || !node.dataset || !node.dataset.token) return
    this.exchangesTarget.value = node.dataset.token
    this.changeExchange()
  }

  changeBin (e) {
    var btn = e.target || e.srcElement
    if (btn.nodeName !== 'BUTTON' || !this.graph) return
    settings.bin = btn.name
    this.justifyBins()
    this.setBinSelection()
    this.fetchChart()
  }

  resetZoom () {
    if (settings.chart === candlestick) {
      this.graph.updateOptions({ dateWindow: stickZoom })
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
    var btn = e.target || e.srcElement
    if (btn.nodeName !== 'BUTTON' || !this.graph) return
    this.conversionTarget.querySelectorAll('button').forEach(b => b.classList.remove('btn-selected'))
    btn.classList.add('btn-selected')
    var cLabel = 'BTC'
    if (e.target.name === 'BTC') {
      this.converted = false
      conversionFactor = 1
    } else {
      this.converted = true
      conversionFactor = this.conversionFactor
      cLabel = this.currencyCode
    }
    this.graph.updateOptions({ xlabel: `Price (${cLabel})` })
  }

  setExchangeName () {
    this.xcLogoTarget.className = `exchange-logo ${settings.xc}`
    var prettyName = humanize.capitalize(settings.xc)
    this.xcNameTarget.textContent = prettyName
    this.linkTarget.href = exchangeLinks[settings.xc]
    this.linkTarget.textContent = `Visit ${prettyName}`
  }

  _processNightMode (data) {
    if (!this.graph) return
    chartStroke = data.nightMode ? darkStroke : lightStroke
    if (settings.chart === history || settings.chart === volume) {
      this.graph.updateOptions({ colors: [chartStroke] })
    }
    if (settings.chart === orders) {
      this.graph.setAnnotations([])
    }
  }

  getExchangeRow (token) {
    var rows = this.xcRowTargets
    for (let i = 0; i < rows.length; i++) {
      let tr = rows[i]
      if (tr.dataset.token === token) {
        let row = {}
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

  _processXcUpdate (update) {
    let xc = update.updater
    if (update.fiat) {
      this.xcIndexTargets.forEach(span => {
        if (span.dataset.token === xc.token) {
          span.textContent = xc.price.toFixed(2)
        }
      })
    } else {
      let row = this.getExchangeRow(xc.token)
      row.volume.textContent = humanize.threeSigFigs(xc.volume)
      row.price.textContent = humanize.threeSigFigs(xc.price)
      row.fiat.textContent = (xc.price * update.btc_price).toFixed(2)
      if (xc.change >= 0) {
        row.arrow.className = 'dcricon-arrow-up text-green'
      } else {
        row.arrow.className = 'dcricon-arrow-down text-danger'
      }
    }
    this.priceTarget.textContent = update.price.toFixed(2)
    if (settings.xc !== xc.token) return
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
