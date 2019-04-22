import { Controller } from 'stimulus'
import { map, assign, merge } from 'lodash-es'
import Zoom from '../helpers/zoom_helper'
import { darkEnabled } from '../services/theme_service'
import { animationFrame } from '../helpers/animation_helper'
import { getDefault } from '../helpers/module_helper'
import axios from 'axios'
import TurboQuery from '../helpers/turbolinks_helper'
import globalEventBus from '../services/event_bus_service'
import dompurify from 'dompurify'

var selectedChart
let Dygraph // lazy loaded on connect

const blockTime = 5 * 60 * 1000
const atomsToDCR = 1e-8
const windowScales = ['ticket-price', 'pow-difficulty']
var userBins = {}

function usesWindowUnits (chart) {
  return windowScales.indexOf(chart) > -1
}

function intComma (amount) {
  return amount.toLocaleString(undefined, { maximumFractionDigits: 0 })
}

function legendFormatter (data) {
  var html = ''
  if (data.x == null) {
    html = `<div class="d-flex flex-wrap justify-content-center align-items-center">
            <div class="pr-3">${this.getLabels()[0]}: N/A</div>
            <div class="d-flex flex-wrap">
            ${map(data.series, (series) => {
    return `<div class="pr-2">${series.dashHTML} ${series.labelHTML}</div>`
  }).join('')}
            </div>
        </div>
        `
  }

  return `<div class="d-flex flex-wrap justify-content-center align-items-center">
        <div class="pr-3">${this.getLabels()[0]}: ${data.xHTML}</div>
        <div class="d-flex flex-wrap">
        ${map(data.series, (series) => {
    if (!series.isVisible) return
    let yVal = series.label.toLowerCase().includes('coin supply') ? intComma(series.y) + ' DCR' : series.yHTML
    return `<div class="pr-2">${series.dashHTML} ${series.labelHTML}: ${yVal}</div>`
  }).join('') + percentChange}
          </div>
      </div>`
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
    colors: ['#2970FF', '#2DD8A3']
  }
}

function zipYvDate (gData, coefficient) {
  coefficient = coefficient || 1
  return map(gData.x, (n, i) => {
    return [
      new Date(n * 1000),
      gData.y[i] * coefficient
    ]
  })
}

function poolSizeFunc (gData, networkTarget) {
  var data = map(gData.x, (n, i) => { return [new Date(n), gData.y[i], null] })
  if (data.length) {
    data[0][2] = networkTarget
    data[data.length - 1][2] = networkTarget
  }
  return data
}

// function ticketSpendTypeFunc (gData) {
//   return map(gData.height, (n, i) => { return [n, gData.unspent[i], gData.revoked[i]] })
// }
//
// function ticketByOutputCountFunc (gData) {
//   return map(gData.height, (n, i) => { return [n, gData.solo[i], gData.pooled[i]] })
// }

function mapDygraphOptions (data, labelsVal, isDrawPoint, yLabel, xLabel, titleName, labelsMG, labelsMG2) {
  return merge({
    'file': data,
    digitsAfterDecimal: 8,
    labels: labelsVal,
    drawPoints: isDrawPoint,
    ylabel: yLabel,
    xlabel: xLabel,
    labelsKMB: labelsMG,
    labelsKMG2: labelsMG2,
    title: titleName,
    fillGraph: false,
    stackedGraph: false,
    plotter: null
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
      'linearBttn',
      'logBttn',
      'binSelector',
      'binSize'
    ]
  }

  async connect () {
    this.query = new TurboQuery()
    this.ticketPoolSizeTarget = parseInt(this.data.get('tps'))
    this.settings = TurboQuery.nullTemplate(['chart', 'zoom', 'scale', 'bin'])
    this.query.update(this.settings)
    if (this.settings.zoom) {
      this.setSelectedZoom(this.settings.zoom)
    }
    if (this.settings.scale && this.settings.scale === 'log') {
      this.logScale()
    }
    this.settings.chart = this.settings.chart || 'ticket-price'
    if (this.settings.bin) {
      this.setBinButton(this.settings.bin)
      userBins[this.settings.chart] = this.settings.bin
    }
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
      labels: ['Date', 'Ticket Price'],
      digitsAfterDecimal: 8,
      showRangeSelector: true,
      rangeSelectorPlotFillColor: '#8997A5',
      rangeSelectorPlotFillGradientColor: '',
      rangeSelectorPlotStrokeColor: '',
      rangeSelectorAlpha: 0.4,
      rangeSelectorHeight: 40,
      drawPoints: true,
      pointSize: 0.25,
      labelsSeparateLines: true,
      plotter: Dygraph.Plotters.linePlotter,
      labelsDiv: this.labelsTarget,
      legend: 'always',
      legendFormatter: legendFormatter,
      highlightCircleSize: 4,
      labelsUTC: true
    }

    this.chartsView = new Dygraph(
      this.chartsViewTarget,
      [[1, 1]],
      options
    )
    this.chartSelectTarget.value = this.settings.chart
    this.selectChart()
  }

  plotGraph (chartName, data) {
    var d = []
    var gOptions = {
      zoomCallback: null,
      drawCallback: null,
      logscale: this.settings.scale === 'log',
      stepPlot: false
    }
    switch (chartName) {
      case 'ticket-price': // price graph
        d = zipYvDate(data, atomsToDCR)
        gOptions.stepPlot = true
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Price'], true, 'Price (DCR)', 'Date', undefined, false, false))
        break

      case 'ticket-pool-size': // pool size graph
        d = poolSizeFunc(data, this.ticketPoolSizeTarget)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Pool Size', 'Network Target'], false, 'Ticket Pool Size', 'Date', undefined, true, false))
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
        d = zipYvDate(data, atomsToDCR)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Pool Value'], true, 'Ticket Pool Value', 'Date',
          undefined, true, false))
        break

      case 'block-size': // block size graph
        d = zipYvDate(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Block Size'], false, 'Block Size', 'Date', undefined, true, false))
        break

      case 'blockchain-size': // blockchain size graph
        d = zipYvDate(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Blockchain Size'], true, 'Blockchain Size', 'Date', undefined, false, true))
        break

      case 'tx-count': // tx per block graph
        d = zipYvDate(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Number of Transactions'], false, '# of Transactions', 'Date',
          undefined, false, false))
        break

        // case 'tx-per-day': // tx per day graph
        //   d = zipYvDate(data)
        //   assign(gOptions, mapDygraphOptions(d, ['Date', 'Number of Transactions Per Day'], true, '# of Transactions', 'Date',
        //     undefined, true, false))
        //   break

      case 'pow-difficulty': // difficulty graph
        d = zipYvDate(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Difficulty'], true, 'Difficulty', 'Date', undefined, true, false))
        break

      case 'coin-supply': // supply graph
        d = zipYvDate(data, atomsToDCR)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Coin Supply'], true, 'Coin Supply (DCR)', 'Date', undefined, true, false))
        break

      case 'fees': // block fee graph
        d = zipYvDate(data, atomsToDCR)
        assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Total Fee'], false, 'Total Fee (DCR)', 'Block Height',
          undefined, true, false))
        break

      case 'duration-btw-blocks': // Duration between blocks graph
        d = zipYvDate(data)
        assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Duration Between Block'], false, 'Duration Between Block (seconds)', 'Block Height',
          undefined, false, false))
        break

        // case 'ticket-spend-type': // Tickets spendtype per block graph
        //   d = ticketSpendTypeFunc(data)
        //   assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Unspent', 'Revoked'], false, '# of Tickets Spend Type', 'Block Height',
        //     undefined, false, false), {
        //     fillGraph: true,
        //     stackedGraph: true,
        //     plotter: barChartPlotter
        //   })
        //   break

        // case 'ticket-by-outputs-windows': // Tickets by output count graph for ticket windows
        //   d = ticketByOutputCountFunc(data)
        //   assign(gOptions, mapDygraphOptions(d, ['Window', '3 Outputs (likely Solo)', '5 Outputs (likely Pooled)'], false, '# of Tickets By Output Count', 'Ticket Price Window',
        //     undefined, false, false), {
        //     fillGraph: true,
        //     stackedGraph: true,
        //     plotter: barChartPlotter
        //   })
        //   break
        // case 'ticket-by-outputs-blocks': // Tickets by output count graph for all blocks
        //   d = ticketByOutputCountFunc(data)
        //   assign(gOptions, mapDygraphOptions(
        //     d,
        //     ['Block Height', 'Solo', 'Pooled'],
        //     false,
        //     '# of Tickets By Output Count',
        //     'Block Height',
        //     undefined,
        //     false,
        //     false
        //   ), {
        //     fillGraph: true,
        //     stackedGraph: true,
        //     plotter: barChartPlotter
        //   })
        //   break

      case 'chainwork': // Total chainwork over time
        d = zipYvDate(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Cumulative Chainwork (exahash)'],
          false, 'Cumulative Chainwork (exahash)', 'Date', undefined, true, false))
        break

      case 'hashrate': // Total chainwork over time
        d = zipYvDate(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Network Hashrate (terahash/s)'],
          false, 'Network Hashrate (terahash/s)', 'Date', undefined, true, false))
        break
    }
    this.chartsView.updateOptions(gOptions, false)
    this.validateZoom()
  }

  async selectChart () {
    var selection = this.settings.chart = this.chartSelectTarget.value
    this.chartWrapperTarget.classList.add('loading')
    if (selectedChart !== selection) {
      let url = '/api/chart/' + selection
      if (usesWindowUnits(selection)) {
        this.binSelectorTarget.classList.add('d-hide')
      } else {
        this.binSelectorTarget.classList.remove('d-hide')
        if (userBins[selection]) {
          this.settings.bin = userBins[selection]
          url += `?zoom=${this.settings.bin}`
          this.setBinButton(this.settings.bin)
        } else {
          this.settings.bin = null
          this.setBinButton('day')
        }
      }
      let chartResponse = await axios.get(url)
      console.log('got api data', chartResponse, this, selection)
      selectedChart = selection
      this.plotGraph(selection, chartResponse.data)
    } else {
      this.chartWrapperTarget.classList.remove('loading')
    }
  }

  onZoom (event) {
    var target = event.srcElement || event.target
    this.zoomOptionTargets.forEach((el) => {
      el.classList.remove('active')
    })
    target.classList.add('active')
    this.settings.zoom = null
    this.validateZoom()
  }

  async validateZoom () {
    await animationFrame()
    this.chartWrapperTarget.classList.add('loading')
    await animationFrame()
    let oldLimits = this.limits || this.chartsView.xAxisExtremes()
    this.limits = this.chartsView.xAxisExtremes()
    if (this.selectedZoom) {
      this.lastZoom = Zoom.validate(this.selectedZoom, this.limits, blockTime)
    } else {
      this.lastZoom = Zoom.project(this.settings.zoom, oldLimits, this.limits)
    }
    if (this.lastZoom) {
      this.chartsView.updateOptions({
        dateWindow: [this.lastZoom.start, this.lastZoom.end]
      })
    }
    this.settings.zoom = Zoom.encode(this.lastZoom)
    this.query.replace(this.settings)
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
    this.zoomOptionTargets.forEach((button) => {
      button.classList.remove('active')
    })
    this.query.replace(this.settings)
    this.setSelectedZoom(Zoom.mapKey(this.settings.zoom, this.limits))
  }

  _drawCallback (graph, first) {
    if (first) return
    var start, end
    [start, end] = this.chartsView.xAxisRange()
    if (start === end) return
    if (this.lastZoom.start === start) return // only handle slide event.
    this.lastZoom = Zoom.object(start, end)
    this.settings.zoom = Zoom.encode(this.lastZoom)
    this.query.replace(this.settings)
    this.setSelectedZoom(Zoom.mapKey(this.lastZoom, this.limits))
  }

  setSelectedZoom (zoomKey) {
    this.zoomOptionTargets.forEach((button) => {
      if (button.dataset.zoom === zoomKey) {
        button.classList.add('active')
      } else {
        button.classList.remove('active')
      }
    })
  }

  setBin (e) {
    var target = e.srcElement || e.target
    this.binSizeTargets.forEach(li => li.classList.remove('active'))
    target.classList.add('active')
    userBins[selectedChart] = target.dataset.bin
    selectedChart = null // Force fetch
    this.selectChart()
  }

  setBinButton (bin) {
    this.binSizeTargets.forEach(li => {
      if (li.dataset.bin === bin) {
        li.classList.add('active')
      } else {
        li.classList.remove('active')
      }
    })
  }

  linearScale () {
    this.settings.scale = null // default
    this.linearBttnTarget.classList.add('active')
    this.logBttnTarget.classList.remove('active')
    if (this.chartsView !== undefined) {
      this.chartsView.updateOptions({
        logscale: false
      })
    }
    this.query.replace(this.settings)
  }

  logScale () {
    this.settings.scale = 'log'
    this.linearBttnTarget.classList.remove('active')
    this.logBttnTarget.classList.add('active')
    if (this.chartsView !== undefined) {
      this.chartsView.updateOptions({
        logscale: true
      })
    }
    this.query.replace(this.settings)
  }

  get selectedZoom () {
    var key = false
    this.zoomOptionTargets.forEach((el) => {
      if (el.classList.contains('active')) key = el.dataset.zoom
    })
    return key
  }
}
