import { Controller } from 'stimulus'
import { map, assign, merge } from 'lodash-es'
import Zoom from '../helpers/zoom_helper'
import { darkEnabled } from '../services/theme_service'
import { animationFrame } from '../helpers/animation_helper'
import { getDefault } from '../helpers/module_helper'
import axios from 'axios'
import TurboQuery from '../helpers/turbolinks_helper'
import globalEventBus from '../services/event_bus_service'
import { barChartPlotter } from '../helpers/chart_helper'
import dompurify from 'dompurify'

var selectedChart
let Dygraph // lazy loaded on connect

const aDay = 86400 * 1000 // in milliseconds
const aMonth = 30 // in days
const atomsToDCR = 1e-8
const windowScales = ['ticket-price', 'pow-difficulty']
const lineScales = ['ticket-price']
var ticketPoolSizeTarget, premine, stakeValHeight, stakeShare
var baseSubsidy, subsidyInterval, subsidyExponent, windowSize, avgBlockTime

function usesWindowUnits (chart) {
  return windowScales.indexOf(chart) > -1
}

function isScaleDisabled (chart) {
  return lineScales.indexOf(chart) > -1
}

function intComma (amount) {
  return amount.toLocaleString(undefined, { maximumFractionDigits: 0 })
}

function formatHashRate (value, displayType) {
  value = parseInt(value)
  if (value <= 0) return value
  var shortUnits = ['Th', 'Ph', 'Eh']
  var labelUnits = ['terahash/s', 'petahash/s', 'exahash/s']
  for (var i = 0; i < labelUnits.length; i++) {
    var quo = Math.pow(1000, i)
    var max = Math.pow(1000, i + 1)
    if ((value > quo && value <= max) || i + 1 === labelUnits.length) {
      var data = intComma(Math.floor(value / quo))
      if (displayType === 'axis') return data + '' + shortUnits[i]
      return data + ' ' + labelUnits[i]
    }
  }
}

function blockReward (height) {
  if (height >= stakeValHeight) return baseSubsidy * Math.pow(subsidyExponent, Math.floor(height / subsidyInterval))
  if (height > 1) return baseSubsidy * (1 - stakeShare)
  if (height === 1) return premine
  return 0
}

function legendFormatter (data) {
  var html = ''
  if (data.x == null) {
    let dashLabels = data.series.reduce((nodes, series) => {
      return `${nodes} <div class="pr-2">${series.dashHTML} ${series.labelHTML}</div>`
    }, '')
    html = `<div class="d-flex flex-wrap justify-content-center align-items-center">
              <div class="pr-3">${this.getLabels()[0]}: N/A</div>
              <div class="d-flex flex-wrap">${dashLabels}</div>
            </div>`
  } else {
    data.series.sort((a, b) => a.y > b.y ? -1 : 1)
    var extraHTML = ''
    // The circulation chart has an additional legend entry showing percent
    // difference.
    if (data.series.length === 2 && data.series[1].label.toLowerCase() === 'coin supply') {
      let predicted = data.series[0].y
      let actual = data.series[1].y
      let change = (((actual - predicted) / predicted) * 100).toFixed(2)
      extraHTML = `<div class="pr-2">&nbsp;&nbsp;Change: ${change} %</div>`
    }

    let yVals = data.series.reduce((nodes, series) => {
      if (!series.isVisible) return nodes
      let yVal = series.yHTML
      switch (series.label.toLowerCase()) {
        case 'coin supply':
          yVal = intComma(series.y) + ' DCR'
          break

        case 'hashrate':
          yVal = formatHashRate(series.y)
          break
      }
      return `${nodes} <div class="pr-2">${series.dashHTML} ${series.labelHTML}: ${yVal}</div>`
    }, '')

    html = `<div class="d-flex flex-wrap justify-content-center align-items-center">
                <div class="pr-3">${this.getLabels()[0]}: ${data.xHTML}</div>
                <div class="d-flex flex-wrap"> ${yVals}</div>
            </div>${extraHTML}`
  }

  dompurify.sanitize(html)
  return html
}

function nightModeOptions (nightModeOn) {
  if (nightModeOn) {
    return {
      rangeSelectorAlpha: 0.3,
      gridLineColor: '#596D81',
      colors: ['#2DD8A3', '#2970FF']
    }
  }
  return {
    rangeSelectorAlpha: 0.4,
    gridLineColor: '#C4CBD2',
    colors: ['#2970FF', '#006600']
  }
}

function zipXYData (gData, isHeightAxis, isDayBinned, coefficient, windowS) {
  coefficient = coefficient || 1
  windowS = windowS || 1
  return map(gData.x, (n, i) => {
    var xAxisVal
    if (isHeightAxis && isDayBinned) {
      xAxisVal = n
    } else if (isHeightAxis) {
      xAxisVal = i * windowS
    } else {
      xAxisVal = new Date(n * 1000)
    }
    return [xAxisVal, gData.y[i] * coefficient]
  })
}

function poolSizeFunc (gData, isHeightAxis, isDayBinned) {
  var data = map(gData.x, (n, i) => {
    var xAxisVal
    if (isHeightAxis && isDayBinned) {
      xAxisVal = n
    } else if (isHeightAxis) {
      xAxisVal = i
    } else {
      xAxisVal = new Date(n * 1000)
    }
    return [xAxisVal, gData.y[i], null]
  })
  if (data.length) {
    data[0][2] = ticketPoolSizeTarget
    data[data.length - 1][2] = ticketPoolSizeTarget
  }
  return data
}

function zipXYZData (gData, isHeightAxis, isDayBinned, yCoefficient, zCoefficient, windowS) {
  windowS = windowS || 1
  yCoefficient = yCoefficient || 1
  zCoefficient = zCoefficient || 1
  return map(gData.x, (n, i) => {
    var xAxisVal
    if (isHeightAxis && isDayBinned) {
      xAxisVal = n
    } else if (isHeightAxis) {
      xAxisVal = i * windowS
    } else {
      xAxisVal = new Date(n * 1000)
    }
    return [xAxisVal, gData.y[i] * yCoefficient, gData.z[i] * zCoefficient]
  })
}

function circulationFunc (gData, isHeightAxis, isDayBinned) {
  var circ = 0
  var h = -1
  var addDough = (newHeight) => {
    while (h < newHeight) {
      h++
      circ += blockReward(h) * atomsToDCR
    }
  }

  var data = map(gData.x, (n, i) => {
    var xAxisVal, height
    if (isHeightAxis && isDayBinned) {
      xAxisVal = n
      height = n
    } else if (isHeightAxis) {
      xAxisVal = i
      height = i
    } else {
      xAxisVal = new Date(n * 1000)
      height = !gData.z ? i : gData.z[i]
    }
    addDough(height)
    return [xAxisVal, gData.y[i] * atomsToDCR, circ]
  })

  var dailyBlocks = aDay / avgBlockTime
  var lastxValueSet = data[data.length - 1][0]
  if (!isHeightAxis) lastxValueSet = lastxValueSet.getTime()
  for (var i = 1; i <= aMonth; i++) {
    addDough(h + dailyBlocks)
    if (isHeightAxis) {
      lastxValueSet += dailyBlocks
      data.push([lastxValueSet, null, circ])
    } else {
      lastxValueSet += aDay
      data.push([new Date(lastxValueSet), null, circ])
    }
  }
  return data
}

function mapDygraphOptions (data, labelsVal, isDrawPoint, yLabel, labelsMG, labelsMG2) {
  return merge({
    'file': data,
    labels: labelsVal,
    drawPoints: isDrawPoint,
    ylabel: yLabel,
    labelsKMB: labelsMG,
    labelsKMG2: labelsMG2
  }, nightModeOptions(darkEnabled()))
}

export default class extends Controller {
  static get targets () {
    return [
      'chartWrapper',
      'labels',
      'chartsView',
      'chartSelect',
      'zoomSelector',
      'zoomOption',
      'scaleType',
      'axisOption',
      'binSelector',
      'scaleSelector',
      'binSize'
    ]
  }

  async connect () {
    this.query = new TurboQuery()
    ticketPoolSizeTarget = parseInt(this.data.get('tps'))
    premine = parseInt(this.data.get('premine'))
    stakeValHeight = parseInt(this.data.get('svh'))
    stakeShare = parseInt(this.data.get('pos')) / 10.0
    baseSubsidy = parseInt(this.data.get('bs'))
    subsidyInterval = parseInt(this.data.get('sri'))
    subsidyExponent = parseFloat(this.data.get('mulSubsidy')) / parseFloat(this.data.get('divSubsidy'))
    windowSize = parseInt(this.data.get('windowSize'))
    avgBlockTime = parseInt(this.data.get('blockTime')) * 1000

    this.settings = TurboQuery.nullTemplate(['chart', 'zoom', 'scale', 'bin', 'axis'])
    this.query.update(this.settings)
    this.settings.chart = this.settings.chart || 'ticket-price'
    this.zoomCallback = this._zoomCallback.bind(this)
    this.drawCallback = this._drawCallback.bind(this)
    this.limits = null
    this.lastZoom = null
    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )
    this.drawInitialGraph()
    this.processNightMode = (params) => {
      this.chartsView.updateOptions(
        nightModeOptions(params.nightMode)
      )
    }
    globalEventBus.on('NIGHT_MODE', this.processNightMode)
  }

  disconnect () {
    globalEventBus.off('NIGHT_MODE', this.processNightMode)
    if (this.chartsView !== undefined) {
      this.chartsView.destroy()
    }
    selectedChart = null
  }

  drawInitialGraph () {
    var options = {
      axes: { y: { axisLabelWidth: 70 }, y2: { axisLabelWidth: 70 } },
      labels: ['Date', 'Ticket Price', 'Tickets Bought'],
      digitsAfterDecimal: 8,
      showRangeSelector: true,
      rangeSelectorPlotFillColor: '#8997A5',
      rangeSelectorAlpha: 0.4,
      rangeSelectorHeight: 40,
      drawPoints: true,
      pointSize: 0.25,
      legend: 'always',
      labelsSeparateLines: true,
      labelsDiv: this.labelsTarget,
      legendFormatter: legendFormatter,
      highlightCircleSize: 4,
      xlabel: 'Date',
      ylabel: 'Ticket Price',
      y2label: 'Tickets Bought',
      labelsUTC: true
    }

    this.chartsView = new Dygraph(
      this.chartsViewTarget,
      [[1, 1, 5], [2, 5, 11]],
      options
    )
    this.chartSelectTarget.value = this.settings.chart

    if (this.settings.axis) this.setAxis(this.settings.axis) // set first
    if (this.settings.scale === 'log') this.setScale(this.settings.scale)
    if (this.settings.zoom) this.setZoom(this.settings.zoom)
    if (this.settings.bin) this.setBin(this.settings.bin)
    this.selectChart()
  }

  plotGraph (chartName, data) {
    var d = []
    var gOptions = {
      zoomCallback: null,
      drawCallback: null,
      logscale: this.settings.scale === 'log',
      y2label: null,
      stepPlot: false
    }
    var isHeightAxis = this.selectedAxis() === 'height'
    var xlabel = isHeightAxis ? 'Block Height' : 'Date'
    var isDayBinned = this.selectedBin() === 'day'

    switch (chartName) {
      case 'ticket-price': // price graph
        d = zipXYZData(data, isHeightAxis, isDayBinned, atomsToDCR, 1, windowSize)
        gOptions.stepPlot = true
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Price', 'Tickets Bought'], true,
          'Price (DCR)', 'Date', undefined, false, false))
        gOptions.y2label = 'Tickets Bought'
        gOptions.series = { 'Tickets Bought': { axis: 'y2' } }
        gOptions.axes.y2 = {
          plotter: barChartPlotter,
          valueRange: [null, windowSize * 20 * 3],
          axisLabelFormatter: (y) => Math.round(y)
        }
        break

      case 'ticket-pool-size': // pool size graph
        d = poolSizeFunc(data, isHeightAxis, isDayBinned)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Ticket Pool Size', 'Network Target'],
          false, 'Ticket Pool Size', true, false))
        gOptions.series = {
          'Network Target': {
            strokePattern: [5, 3],
            connectSeparatedPoints: true,
            strokeWidth: 2,
            color: '#888'
          }
        }
        break

      case 'ticket-pool-value': // pool value graph
        d = zipXYData(data, isHeightAxis, isDayBinned, atomsToDCR)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Ticket Pool Value'], true,
          'Ticket Pool Value', true, false))
        break

      case 'block-size': // block size graph
        d = zipXYData(data, isHeightAxis, isDayBinned)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Block Size'], false, 'Block Size', true, false))
        break

      case 'blockchain-size': // blockchain size graph
        d = zipXYData(data, isHeightAxis, isDayBinned)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Blockchain Size'], true,
          'Blockchain Size', false, true))
        break

      case 'tx-count': // tx per block graph
        d = zipXYData(data, isHeightAxis, isDayBinned)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Number of Transactions'], false,
          '# of Transactions', false, false))
        break

      case 'pow-difficulty': // difficulty graph
        d = zipXYData(data, isHeightAxis, false, 1, windowSize)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Difficulty'], true, 'Difficulty', true, false))
        break

      case 'coin-supply': // supply graph
        d = circulationFunc(data, isHeightAxis, isDayBinned)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Coin Supply', 'Predicted Coin Supply'],
          true, 'Coin Supply (DCR)', true, false))
        break

      case 'fees': // block fee graph
        d = zipXYData(data, isHeightAxis, isDayBinned, atomsToDCR)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Total Fee'], false, 'Total Fee (DCR)', true, false))
        break

      case 'duration-btw-blocks': // Duration between blocks graph
        d = zipXYData(data, isHeightAxis, isDayBinned)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Duration Between Blocks'], false,
          'Duration Between Blocks (seconds)', false, false))
        break

      case 'chainwork': // Total chainwork over time
        d = zipXYData(data, isHeightAxis, isDayBinned)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Cumulative Chainwork (exahash)'],
          false, 'Cumulative Chainwork (exahash)', true, false))
        break

      case 'hashrate': // Total chainwork over time
        d = zipXYData(data, isHeightAxis, isDayBinned)
        assign(gOptions, mapDygraphOptions(d, [xlabel, 'Network Hashrate (terahash/s)'],
          false, 'Network Hashrate (terahash/s)', true, false))
        break
    }

    this.chartsView.updateOptions(gOptions, false)
    this.validateZoom()
  }

  async selectChart () {
    var selection = this.settings.chart = this.chartSelectTarget.value
    this.chartWrapperTarget.classList.add('loading')
    if (isScaleDisabled(selection)) {
      this.scaleSelectorTarget.classList.add('d-hide')
    } else {
      this.scaleSelectorTarget.classList.remove('d-hide')
    }
    if (selectedChart !== selection || this.settings.bin !== this.selectedBin() ||
      this.settings.axis !== this.selectedAxis()) {
      let url = '/api/chart/' + selection
      if (usesWindowUnits(selection)) {
        this.binSelectorTarget.classList.add('d-hide')
      } else {
        this.binSelectorTarget.classList.remove('d-hide')
        this.settings.bin = this.selectedBin()
        if (!this.settings.bin) this.settings.bin = 'day' // Set the default.
        url += `?bin=${this.settings.bin}`
        this.setActiveOptionBtn(this.settings.bin, this.binSizeTargets)

        this.settings.axis = this.selectedAxis()
        if (!this.settings.axis) this.settings.axis = 'time' // Set the default.
        if (this.settings.bin === 'day' && this.settings.axis === 'height') {
          url += `&axis=${this.settings.axis}`
        }
        this.setActiveOptionBtn(this.settings.axis, this.axisOptionTargets)
      }

      let chartResponse = await axios.get(url)
      console.log('got api data', chartResponse, this, selection)
      selectedChart = selection
      this.plotGraph(selection, chartResponse.data)
    } else {
      this.chartWrapperTarget.classList.remove('loading')
    }
  }

  async validateZoom () {
    await animationFrame()
    this.chartWrapperTarget.classList.add('loading')
    await animationFrame()
    let oldLimits = this.limits || this.chartsView.xAxisExtremes()
    this.limits = this.chartsView.xAxisExtremes()
    var selected = this.selectedZoom()
    if (selected) {
      this.lastZoom = Zoom.validate(selected, this.limits,
        this.isTimeAxis() ? avgBlockTime : 1, this.isTimeAxis() ? 1 : avgBlockTime)
    } else {
      this.lastZoom = Zoom.project(this.settings.zoom, oldLimits, this.limits)
    }
    if (this.lastZoom) {
      this.chartsView.updateOptions({
        dateWindow: [this.lastZoom.start, this.lastZoom.end]
      })
    }
    if (selected !== this.settings.zoom) {
      this._zoomCallback(this.lastZoom.start, this.lastZoom.end)
    }
    await animationFrame()
    this.chartWrapperTarget.classList.remove('loading')
    this.chartsView.updateOptions({
      zoomCallback: this.zoomCallback,
      drawCallback: this.drawCallback
    })
  }

  _zoomCallback (start, end) {
    this.lastZoom = Zoom.object(start, end)
    this.settings.zoom = Zoom.encode(this.lastZoom)
    this.query.replace(this.settings)
    let option = Zoom.mapKey(this.settings.zoom, this.chartsView.xAxisExtremes())
    this.setActiveOptionBtn(option, this.zoomOptionTargets)
  }

  isTimeAxis () {
    return this.selectedAxis() === 'time'
  }

  _drawCallback (graph, first) {
    if (first) return
    var start, end
    [start, end] = this.chartsView.xAxisRange()
    if (start === end) return
    if (this.lastZoom.start === start) return // only handle slide event.
    this._zoomCallback(start, end)
  }

  setZoom (e) {
    var target = e.srcElement || e.target
    var option
    if (!target) {
      let ex = this.chartsView.xAxisExtremes()
      option = Zoom.mapKey(e, ex, this.isTimeAxis() ? 1 : avgBlockTime)
    } else {
      option = target.dataset.option
    }
    this.setActiveOptionBtn(option, this.zoomOptionTargets)
    if (!target) return // Exit if running for the first time
    this.validateZoom()
  }

  setBin (e) {
    var target = e.srcElement || e.target
    var option = target ? target.dataset.option : e
    if (!option) return
    this.setActiveOptionBtn(option, this.binSizeTargets)
    if (!target) return // Exit if running for the first time.
    selectedChart = null // Force fetch
    this.selectChart()
  }

  setScale (e) {
    var target = e.srcElement || e.target
    var option = target ? target.dataset.option : e
    if (!option) return
    this.setActiveOptionBtn(option, this.scaleTypeTargets)
    if (!target) return // Exit if running for the first time.
    if (this.chartsView) {
      this.chartsView.updateOptions({ logscale: option === 'log' })
    }
    this.settings.scale = option
    this.query.replace(this.settings)
  }

  setAxis (e) {
    var target = e.srcElement || e.target
    var option = target ? target.dataset.option : e
    if (!option) return
    this.setActiveOptionBtn(option, this.axisOptionTargets)
    if (!target) return // Exit if running for the first time.
    this.settings.axis = null
    this.selectChart()
  }

  setActiveOptionBtn (opt, optTargets) {
    optTargets.forEach(li => {
      if (li.dataset.option === opt) {
        li.classList.add('active')
      } else {
        li.classList.remove('active')
      }
    })
  }

  selectedZoom () { return this.selectedOption(this.zoomOptionTargets) }
  selectedBin () { return this.selectedOption(this.binSizeTargets) }
  selectedScale () { return this.selectedOption(this.scaleTypeTargets) }
  selectedAxis () { return this.selectedOption(this.axisOptionTargets) }

  selectedOption (optTargets) {
    var key = false
    optTargets.forEach((el) => {
      if (el.classList.contains('active')) key = el.dataset.option
    })
    return key
  }
}
