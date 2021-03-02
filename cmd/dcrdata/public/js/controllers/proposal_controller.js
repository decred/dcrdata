import { Controller } from 'stimulus'
import { MiniMeter } from '../helpers/meters.js'
import { darkEnabled } from '../services/theme_service'
import globalEventBus from '../services/event_bus_service'
import { getDefault } from '../helpers/module_helper'
import { multiColumnBarPlotter, synchronize } from '../helpers/chart_helper'
import dompurify from 'dompurify'
import axios from 'axios'
import humanize from '../helpers/humanize_helper'

const common = {
  labelsKMB: true,
  legend: 'always',
  fillGraph: true,
  strokeWidth: 2,
  gridLineColor: '#C4CBD2',
  labelsUTC: true,
  legendFormatter: legendFormatter
}

const percentConfig = {
  ...common,
  labels: ['Date', 'Percent Yes'],
  ylabel: 'Approval (%)',
  colors: ['#2971FF'],
  labelsSeparateLines: true,
  underlayCallback: function (context, area, g) {
    const xVals = g.xAxisExtremes()
    const xl = g.toDomCoords(xVals[0], 60)
    const xr = g.toDomCoords(xVals[1], 60)

    context.beginPath()
    context.strokeStyle = '#ED6D47'
    context.moveTo(xl[0], xl[1])
    context.lineTo(xr[0], xr[1])
    context.fillStyle = '#ED6D47'
    context.fillText('60% Approval', xl[0] + 20, xl[1] + 10)
    context.closePath()
    context.stroke()
  }
}

const cumulativeConfig = {
  ...common,
  labels: ['Date', 'Total Votes'],
  ylabel: 'Vote Count',
  colors: ['#2971FF'],
  underlayCallback: function (context, area, g) {
    const xVals = g.xAxisExtremes()
    const xl = g.toDomCoords(xVals[0], 8269)
    const xr = g.toDomCoords(xVals[1], 8269)

    context.beginPath()
    context.strokeStyle = '#ED6D47'
    context.moveTo(xl[0], xl[1])
    context.lineTo(xr[0], xr[1])
    context.fillStyle = '#ED6D47'
    context.fillText('20% Quorum', xl[0] + 20, xl[1] + 10)
    context.closePath()
    context.stroke()
  }
}

function legendFormatter (data) {
  let html
  if (data.x == null) {
    html = data.series.map(function (series) {
      return series.dashHTML + ' <span style="color:' +
        series.color + ';">' + series.labelHTML + ' </span> '
    }).join('')
  } else {
    html = this.getLabels()[0] + ': ' + humanize.date(data.x) + ' UTC &nbsp;&nbsp;'
    data.series.forEach(function (series, index) {
      if (!series.isVisible) return

      let symbol = ''
      if (series.labelHTML.includes('Percent')) {
        series.labelHTML = 'Yes'
        symbol = '%'
      } else if (index === 0) {
        html = ''
      } else {
        // html += ' <br> '
      }

      const labeledData = '<span style="color:' + series.color + ';">' +
        series.labelHTML + ' </span> : ' + Math.abs(series.y)
      html += series.dashHTML + ' ' + labeledData + '' + symbol + ' &nbsp;'
    })
  }
  dompurify.sanitize(html, { FORBID_TAGS: ['svg', 'math'] })
  return html
}

const hourlyVotesConfig = {
  ...common,
  plotter: multiColumnBarPlotter,
  showRangeSelector: true,
  labels: ['Date', 'Yes', 'No'],
  ylabel: 'Votes Per Hour',
  xlabel: 'Date',
  colors: ['#2DD8A3', '#ED6D47'],
  fillColors: ['rgb(150,235,209)', 'rgb(246,182,163)']
}

let gs = []
let Dygraph
let chartData = {}

let percentData
let cumulativeData
let hourlyVotesData
export default class extends Controller {
  static get targets () {
    return ['token', 'approvalMeter', 'cumulative', 'cumulativeLegend',
      'approval', 'approvalLegend', 'log', 'logLegend'
    ]
  }

  async connect () {
    if (this.hasApprovalMeterTarget) {
      const d = this.approvalMeterTarget.dataset
      const opts = {
        darkMode: darkEnabled(),
        segments: [
          { end: d.threshold, color: '#ed6d47' },
          { end: 1, color: '#2dd8a3' }
        ]
      }
      this.approvalMeter = new MiniMeter(this.approvalMeterTarget, opts)
    }

    const response = await axios.get('/api/proposal/' + this.tokenTarget.dataset.hash)
    chartData = response.data

    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )

    this.setChartsData()
    this.plotGraph()
    this.setNightMode = this._setNightMode.bind(this)
    globalEventBus.on('NIGHT_MODE', this.setNightMode)
  }

  disconnect () {
    gs.map((chart) => { chart.destroy() })
    globalEventBus.off('NIGHT_MODE', this.setNightMode)
  }

  setChartsData () {
    let total = 0
    let yes = 0

    percentData = []
    cumulativeData = []
    hourlyVotesData = []

    chartData.time.map((n, i) => {
      const formatedDate = new Date(n)
      yes += chartData.yes[i]
      total += (chartData.no[i] + chartData.yes[i])

      const percent = ((yes * 100) / total).toFixed(2)

      percentData.push([formatedDate, parseFloat(percent)])
      cumulativeData.push([formatedDate, total])
      hourlyVotesData.push([formatedDate, chartData.yes[i], chartData.no[i] * -1])
    })

    // add empty data at the beginning and end of hourlyVotesData
    // to pad the bar chart data on both ends
    const firstDate = new Date(chartData.time[0])
    firstDate.setHours(firstDate.getHours() - 1)
    const lastDate = new Date(chartData.time[chartData.time.length - 1])
    lastDate.setHours(lastDate.getHours() + 1)
    hourlyVotesData.unshift([firstDate, 0, 0])
    hourlyVotesData.push([lastDate, 0, 0])
  }

  plotGraph () {
    percentConfig.labelsDiv = this.approvalLegendTarget
    cumulativeConfig.labelsDiv = this.cumulativeLegendTarget
    hourlyVotesConfig.labelsDiv = this.logLegendTarget
    gs = [
      new Dygraph(
        this.approvalTarget,
        percentData,
        percentConfig
      ),
      new Dygraph(
        this.cumulativeTarget,
        cumulativeData,
        cumulativeConfig
      ),
      new Dygraph(
        this.logTarget,
        hourlyVotesData,
        hourlyVotesConfig
      )
    ]

    const options = {
      zoom: true,
      selection: true
    }

    synchronize(gs, options)
  }

  _setNightMode (state) {
    if (this.approvalMeter) {
      this.approvalMeter.setDarkMode(state.nightMode)
    }
  }
}
