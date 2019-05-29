import { Controller } from 'stimulus'
import { assign, merge } from 'lodash-es'
import Zoom from '../helpers/zoom_helper'
import { darkEnabled } from '../services/theme_service'
import { animationFrame } from '../helpers/animation_helper'
import { Job, killJobs } from '../helpers/async'
import { getDefault } from '../helpers/module_helper'
import { linePlotterAsync } from '../helpers/chart_helper'
import axios from 'axios'
import TurboQuery from '../helpers/turbolinks_helper'
import globalEventBus from '../services/event_bus_service'
import dompurify from 'dompurify'

var selectedChart
let Dygraph // lazy loaded on connect
let defaultAxisformatter

const blockTime = 5 * 60 * 1000
const aDay = 86400 * 1000
const aMonth = aDay * 30
const atomsToDCR = 1e-8
const windowScales = ['ticket-price', 'pow-difficulty']
const chartJobID = 'chartJobID'
var ticketPoolSizeTarget, premine, stakeValHeight, stakeShare
var baseSubsidy, subsidyInterval, subsidyExponent
var userBins = {}

function usesWindowUnits (chart) {
  return windowScales.indexOf(chart) > -1
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
    colors: ['#2970FF', '#2DD8A3']
  }
}

async function zipYvDate (job, coefficient) {
  coefficient = coefficient || 1
  var rawData = job.get('rawData')
  var x = rawData.x
  var y = rawData.y
  return job.irun(x.length, i => {
    return [new Date(x[i] * 1000), y[i] * coefficient]
  })
}

async function poolSizeFunc (job) {
  var rawData = job.get('rawData')
  var time = rawData.x
  var poolSize = rawData.y
  var translated = await job.irun(time.length, i => {
    return [new Date(time[i] * 1000), poolSize[i], null]
  })
  if (translated.length) {
    translated[0][2] = ticketPoolSizeTarget
    translated[translated.length - 1][2] = ticketPoolSizeTarget
  }
  return translated
}

async function circulationFunc (job, blocks) {
  var rawData = job.get('rawData')
  var time = rawData.x
  var newDough = rawData.y
  var heights = rawData.z
  var circ = 0
  var h = -1
  var addDough = (newHeight) => {
    while (h < newHeight) {
      h++
      circ += blockReward(h) * atomsToDCR
    }
  }
  var translated = await job.irun(time.length, i => {
    addDough(blocks ? i : heights[i])
    return [new Date(time[i] * 1000), newDough[i] * atomsToDCR, circ]
  })
  var stamp = translated[translated.length - 1][0].getTime()
  var end = stamp + aMonth
  while (stamp < end) {
    addDough(h + aDay / blockTime)
    translated.push([new Date(stamp), null, circ])
    stamp += aDay
  }
  return translated
}

function mapDygraphOptions (labelsVal, isDrawPoint, yLabel, xLabel, titleName, labelsMG, labelsMG2) {
  return merge({
    digitsAfterDecimal: 8,
    labels: labelsVal,
    drawPoints: isDrawPoint,
    ylabel: yLabel,
    xlabel: xLabel,
    labelsKMB: labelsMG,
    labelsKMG2: labelsMG2,
    title: titleName,
    axes: { y: { axisLabelFormatter: defaultAxisformatter } }
  }, nightModeOptions(darkEnabled()))
}

export default class extends Controller {
  static get targets () {
    return [
      'labels',
      'chartsView',
      'chartSelect',
      'zoomSelector',
      'zoomOption',
      'linearBttn',
      'logBttn',
      'binSelector',
      'binSize',
      'chartLoader'
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
      this.submitPlotJob(new Job(chartJobID), nightModeOptions(params.nightMode))
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
      axes: { y: { axisLabelWidth: 70 } },
      labels: ['Date', 'Ticket Price'],
      digitsAfterDecimal: 8,
      showRangeSelector: true,
      rangeSelectorPlotFillColor: '#8997A5',
      rangeSelectorAlpha: 0.4,
      rangeSelectorHeight: 40,
      drawPoints: true,
      pointSize: 0.25,
      labelsSeparateLines: true,
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
    defaultAxisformatter = this.chartsView.optionsViewForAxis_('y')('axisLabelFormatter')
    this.chartSelectTarget.value = this.settings.chart
    this.selectChart()
  }

  async plotGraph (chartName, url) {
    var gOptions = {
      zoomCallback: null,
      drawCallback: null,
      logscale: this.settings.scale === 'log',
      stepPlot: false,
      plotter: linePlotterAsync
    }
    killJobs(chartJobID)
    var job = new Job(chartJobID)
    // We'll need to set a 'dataSource' for the job, which is a tuple of
    // 1. URL, 2. translation function, 3. optional array of additional
    // arguments for the translator.
    switch (chartName) {
      case 'ticket-price': // price graph
        gOptions.stepPlot = true
        assign(gOptions, mapDygraphOptions(['Date', 'Ticket Price'], true, 'Price (DCR)', 'Date', undefined, false, false))
        job.set('dataSource', [url, zipYvDate, [atomsToDCR]])
        break

      case 'ticket-pool-size': // pool size graph
        assign(gOptions, mapDygraphOptions(['Date', 'Ticket Pool Size', 'Network Target'], false, 'Ticket Pool Size', 'Date', undefined, true, false))
        gOptions.series = {
          'Network Target': {
            strokePattern: [5, 3],
            connectSeparatedPoints: true,
            strokeWidth: 2,
            color: '#888'
          }
        }
        job.set('dataSource', [url, poolSizeFunc, []])
        break

      case 'ticket-pool-value': // pool value graph
        assign(gOptions, mapDygraphOptions(['Date', 'Ticket Pool Value'], true, 'Ticket Pool Value', 'Date',
          undefined, true, false))
        job.set('dataSource', [url, zipYvDate, [atomsToDCR]])
        break

      case 'block-size': // block size graph
        assign(gOptions, mapDygraphOptions(['Date', 'Block Size'], false, 'Block Size', 'Date', undefined, true, false))
        job.set('dataSource', [url, zipYvDate, []])
        break

      case 'blockchain-size': // blockchain size graph
        assign(gOptions, mapDygraphOptions(['Date', 'Blockchain Size'], true, 'Blockchain Size', 'Date', undefined, false, true))
        job.set('dataSource', [url, zipYvDate, []])
        break

      case 'tx-count': // tx per block graph
        assign(gOptions, mapDygraphOptions(['Date', 'Number of Transactions'], false, '# of Transactions', 'Date',
          undefined, false, false))
        job.set('dataSource', [url, zipYvDate, []])
        break

      case 'pow-difficulty': // difficulty graph
        assign(gOptions, mapDygraphOptions(['Date', 'Difficulty'], true, 'Difficulty', 'Date', undefined, true, false))
        job.set('dataSource', [url, zipYvDate, []])
        break

      case 'coin-supply': // supply graph
        assign(gOptions, mapDygraphOptions(['Date', 'Coin Supply', 'Predicted Coin Supply'], true, 'Coin Supply (DCR)', 'Date', undefined, true, false))
        job.set('dataSource', [url, circulationFunc, [this.settings.bin === 'block']])
        break

      case 'fees': // block fee graph
        assign(gOptions, mapDygraphOptions(['Block Height', 'Total Fee'], false, 'Total Fee (DCR)', 'Block Height',
          undefined, true, false))
        job.set('dataSource', [url, zipYvDate, [atomsToDCR]])
        break

      case 'duration-btw-blocks': // Duration between blocks graph
        assign(gOptions, mapDygraphOptions(['Block Height', 'Duration Between Block'], false, 'Duration Between Block (seconds)', 'Block Height',
          undefined, false, false))
        job.set('dataSource', [url, zipYvDate, []])
        break

      case 'chainwork': // Total chainwork over time
        assign(gOptions, mapDygraphOptions(['Date', 'Cumulative Chainwork (exahash)'],
          false, 'Cumulative Chainwork (exahash)', 'Date', undefined, true, false))
        job.set('dataSource', [url, zipYvDate, []])
        break

      case 'hashrate': // Total chainwork over time
        assign(gOptions, mapDygraphOptions(['Date', 'Hashrate'], false, 'Hashrate (hashes per second)', 'Date', undefined, false, false))
        gOptions.axes.y.axisLabelFormatter = (v) => { return formatHashRate(v, 'axis') }
        job.set('dataSource', [url, zipYvDate, []])
        break
    }
    this.submitPlotJob(job, gOptions)
    await job.completion
    this.validateZoom()
  }

  async submitPlotJob (job, plotOpts) {
    plotOpts.job = () => { return job }
    job.run(async (job) => {
      let dataSource = job.get('dataSource')
      if (dataSource) {
        let url, translate, tOpts
        [url, translate, tOpts] = dataSource
        var rawData = await axios.get(url)
        job.set('rawData', rawData.data)
        if (job.expired()) return
        plotOpts.file = translate ? await translate(job, ...tOpts) : rawData
        if (job.expired()) return
      }
      this.chartsView.updateOptions(plotOpts)
    })
  }

  async selectChart () {
    var selection = this.settings.chart = this.chartSelectTarget.value
    this.chartLoaderTarget.classList.add('loading')
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
      selectedChart = selection
      this.plotGraph(selection, url)
    } else {
      this.chartLoaderTarget.classList.remove('loading')
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
    this.chartLoaderTarget.classList.add('loading')
    await animationFrame()
    let oldLimits = this.limits || this.chartsView.xAxisExtremes()
    this.limits = this.chartsView.xAxisExtremes()
    var newZoom = null
    if (this.selectedZoom) {
      newZoom = Zoom.validate(this.selectedZoom, this.limits, blockTime)
    } else {
      newZoom = Zoom.project(this.settings.zoom, oldLimits, this.limits)
    }
    if (newZoom && this.lastZoom && (this.lastZoom.start !== newZoom.start || this.lastZoom.end !== newZoom.end)) {
      this.submitPlotJob(new Job(chartJobID), {
        dateWindow: [newZoom.start, newZoom.end]
      })
    }
    this.lastZoom = newZoom
    this.settings.zoom = Zoom.encode(this.lastZoom)
    this.query.replace(this.settings)
    await animationFrame()
    this.chartLoaderTarget.classList.remove('loading')
    this.chartsView.updateOptions({
      zoomCallback: this.zoomCallback,
      drawCallback: this.drawCallback
    }, true)
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
      this.submitPlotJob(new Job(chartJobID), {
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
      this.submitPlotJob(new Job(chartJobID), {
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
