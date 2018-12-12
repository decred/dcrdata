import { Controller } from 'stimulus'
import { barChartPlotter, ensureDygraph } from '../helpers/chart_helper'
import ajax from '../helpers/ajax_helper'

var Dygraph = window.Dygraph

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

export default class extends Controller {
  static get targets () {
    return [
      'cumulativeVoteChoices',
      'voteChoicesByBlock'
    ]
  }

  initialize () {
    var controller = this
    controller.emptydata = [[0, 0, 0, 0]]
    controller.cumulativeVoteChoicesChart = false
    controller.voteChoicesByBlockChart = false
  }

  connect () {
    var controller = this
    controller.agendaId = controller.data.get('id')
    controller.element.classList.add('loading')

    ensureDygraph(() => {
      Dygraph = window.Dygraph
      controller.drawCharts()

      let url = '/api/agenda/' + controller.agendaId

      let final = () => {
        controller.element.classList.remove('loading')
      }

      let success = (data) => {
        if (controller.cumulativeVoteChoicesChart) {
          controller.cumulativeVoteChoicesChart.updateOptions({
            'file': cumulativeVoteChoicesData(data.by_time)
          })
        }
        if (controller.voteChoicesByBlockChart) {
          controller.voteChoicesByBlockChart.updateOptions({
            'file': voteChoicesByBlockData(data.by_height)
          })
        }
      }

      ajax(url, success, final)
    })
  }

  disconnect () {
    this.cumulativeVoteChoicesChart.destroy()
    this.voteChoicesByBlockChart.destroy()
  }

  drawChart (el, options) {
    return new Dygraph(
      el,
      this.emptydata,
      {
        ...chartLayout,
        ...options
      }
    )
  }

  drawCharts () {
    var controller = this
    controller.cumulativeVoteChoicesChart = controller.drawChart(
      controller.cumulativeVoteChoicesTarget,
      {
        labels: ['Date', 'Yes', 'Abstain', 'No'],
        ylabel: 'Cumulative Vote Choices Cast',
        title: 'Cumulative Vote Choices',
        labelsKMB: true
      }
    )
    controller.voteChoicesByBlockChart = controller.drawChart(
      controller.voteChoicesByBlockTarget,
      {
        labels: ['Block Height', 'Yes', 'Abstain', 'No'],
        ylabel: 'Vote Choices Cast',
        title: 'Vote Choices By Block',
        plotter: barChartPlotter
      }
    )
  }
}
