/* global Dygraph */

/* global $ */
import { Controller } from 'stimulus'
import { barChartPlotter } from '../helpers/chart_helper'

var chartLayout = {
  showRangeSelector: true,
  legend: 'follow',
  fillGraph: true,
  colors: ['rgb(0,153,0)', 'orange', 'red'],
  stackedGraph: true,
  legendFormatter: agendasLegendFormatter,
  labelsSeparateLines: true,
  labelsKMB: true
}

function agendasLegendFormatter (data) {
  if (data.x == null) return ''
  var html = this.getLabels()[0] + ': ' + data.xHTML
  var total = data.series.reduce((total, n) => {
    return total + n.y
  }, 0)
  data.series.forEach((series) => {
    let percentage = ((series.y * 100) / total).toFixed(2)
    html = '<span style="color:#2d2d2d;">' + html + '</span>'
    html += `<br>${series.dashHTML}<span style="color: ${series.color};">${series.labelHTML}: ${series.yHTML} (${percentage}%)</span>`
  })
  return html
}

function cumulativeVoteChoicesData (d) {
  if (!(d.yes instanceof Array)) return [[0, 0, 0]]
  return d.yes.map((n, i) => {
    return [
      new Date(d.time[i]),
      +d.yes[i],
      d.abstain[i],
      d.no[i]
    ]
  })
}

function voteChoicesByBlockData (d) {
  if (!(d.yes instanceof Array)) return [[0, 0, 0, 0]]
  return d.yes.map((n, i) => {
    return [
      d.height[i],
      +d.yes[i],
      d.abstain[i],
      d.no[i]
    ]
  })
}

function drawChart (el, data, options) {
  return new Dygraph(
    el,
    data,
    {
      ...chartLayout,
      ...options
    }
  )
}

export default class extends Controller {
  static get targets () {
    return [
      'cumulativeVoteChoices',
      'voteChoicesByBlock'
    ]
  }

  connect () {
    var _this = this
    $.getScript('/js/vendor/dygraphs.min.js', function () {
      _this.drawCharts()
    })
  }

  disconnect () {
    this.cumulativeVoteChoicesChart.destroy()
    this.voteChoicesByBlockChart.destroy()
  }

  drawCharts () {
    this.cumulativeVoteChoicesChart = drawChart(
      this.cumulativeVoteChoicesTarget,
      cumulativeVoteChoicesData(window.chartDataByTime),
      {
        labels: ['Date', 'Yes', 'Abstain', 'No'],
        ylabel: 'Cumulative Vote Choices Cast',
        title: 'Cumulative Vote Choices',
        labelsKMB: true
      }
    )
    this.voteChoicesByBlockChart = drawChart(
      this.voteChoicesByBlockTarget,
      voteChoicesByBlockData(window.chartDataByBlock),
      {
        labels: ['Block Height', 'Yes', 'Abstain', 'No'],
        ylabel: 'Vote Choices Cast',
        title: 'Vote Choices By Block',
        plotter: barChartPlotter
      }
    )
  }
}
