import { Controller } from 'stimulus'
import { getDefault } from '../helpers/module_helper'
import { multiColumnBarPlotter, synchronize } from '../helpers/chart_helper'
import dompurify from 'dompurify'
import axios from 'axios'

let common = {
  labelsKMB: true,
  legend: 'always',
  fillGraph: true,
  strokeWidth: 2,
  gridLineColor: '#C4CBD2',
  legendFormatter: legendFormatter
}

let percentConfig = {
  ...common,
  labels: ['Date', 'Percent Yes'],
  ylabel: 'Approval (%)',
  colors: ['#2971FF'],
  labelsSeparateLines: true,
  labelsDiv: 'percent-of-votes-legend',
  underlayCallback: function (context, area, g) {
    let xVals = g.xAxisExtremes()
    let xl = g.toDomCoords(xVals[0], 60)
    let xr = g.toDomCoords(xVals[1], 60)

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

let cumulativeConfig = {
  ...common,
  labels: ['Date', 'Total Votes'],
  ylabel: 'Vote Count',
  colors: ['#2971FF'],
  labelsDiv: 'cumulative-legend',
  underlayCallback: function (context, area, g) {
    let xVals = g.xAxisExtremes()
    let xl = g.toDomCoords(xVals[0], 8269)
    let xr = g.toDomCoords(xVals[1], 8269)

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
        series.color + ';">' + series.labelHTML + ' </span>'
    }).join('<br>')
  } else {
    html = this.getLabels()[0] + ': ' + data.xHTML + ' UTC <br>'
    data.series.forEach(function (series, index) {
      if (!series.isVisible) return

      var symbol = ''
      if (series.labelHTML.includes('Percent')) {
        series.labelHTML = 'Yes'
        symbol = '%'
      } else if (index === 0) {
        html = ''
      } else {
        html += ' <br> '
      }

      var labeledData = '<span style="color:' + series.color + ';">' +
        series.labelHTML + ' </span> : ' + Math.abs(series.y)
      html += series.dashHTML + ' ' + labeledData + '' + symbol
    })
  }
  dompurify.sanitize(html)
  return html
}

var hourlyVotesConfig = {
  ...common,
  plotter: multiColumnBarPlotter,
  showRangeSelector: true,
  labels: ['Date', 'Yes', 'No'],
  ylabel: 'Votes Per Hour',
  xlabel: 'Date',
  colors: ['#2DD8A3', '#ED6D47'],
  labelsDiv: 'votes-by-time-legend',
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
    return ['token']
  }

  async connect () {
    let response = await axios.get('/api/proposal/' + this.tokenTarget.dataset.hash)
    chartData = response.data

    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )

    this.setChartsData()
    this.plotGraph()
  }

  disconnect () {
    gs.map((chart) => { chart.destroy() })
  }

  setChartsData () {
    var total = 0
    var yes = 0

    percentData = []
    cumulativeData = []
    hourlyVotesData = []

    chartData.time.map((n, i) => {
      let formatedDate = new Date(n)
      yes += chartData.yes[i]
      total += (chartData.no[i] + chartData.yes[i])

      let percent = ((yes * 100) / total).toFixed(2)

      percentData.push([formatedDate, parseFloat(percent)])
      cumulativeData.push([formatedDate, total])
      hourlyVotesData.push([formatedDate, chartData.yes[i], chartData.no[i] * -1])
    })
  }

  plotGraph () {
    gs = [
      new Dygraph(
        document.getElementById('percent-of-votes'),
        percentData,
        percentConfig
      ),
      new Dygraph(
        document.getElementById('cumulative'),
        cumulativeData,
        cumulativeConfig
      ),
      new Dygraph(
        document.getElementById('votes-by-time'),
        hourlyVotesData,
        hourlyVotesConfig
      )
    ]

    var options = {
      zoom: true,
      selection: true
    }

    synchronize(gs, options)
  }
}
