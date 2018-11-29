/* global Dygraph */
/* global Chart */

/* global $ */
import { Controller } from 'stimulus'
import ws from '../services/messagesocket_service'
import { barChartPlotter } from '../helpers/chart_helper'

// Common code for ploting dygraphs
function legendFormatter (data) {
  if (data.x == null) return ''
  var html = this.getLabels()[0] + ': ' + data.xHTML
  data.series.map(function (series) {
    var labeledData = ' <span style="color: ' + series.color + ';">' + series.labelHTML + ': ' + series.yHTML
    html += '<br>' + series.dashHTML + labeledData + '</span>'
  })
  return html
}

// Plotting the actual ticketpool graphs
var ms = 0
var origDate = 0

function Comparator (a, b) {
  if (a[0] < b[0]) return -1
  if (a[0] > b[0]) return 1
  return 0
}

function purchasesGraphData (items, memP) {
  var s = []
  var finalDate = ''

  items.time.map(function (n, i) {
    finalDate = new Date(n)
    s.push([finalDate, 0, items.immature[i], items.live[i], items.price[i]])
  })

  if (memP) {
    s.push([new Date(memP.time), memP.count, 0, 0, memP.price]) // add mempool
  }

  origDate = s[0][0] - new Date(0)
  ms = (finalDate - new Date(0)) + 1000

  return s
}

function priceGraphData (items, memP) {
  var mempl = 0
  var mPrice = 0
  var mCount = 0
  var p = []

  if (memP) {
    mPrice = memP.price
    mCount = memP.count
  }

  items.price.map((n, i) => {
    if (n === mPrice) {
      mempl = mCount
      p.push([n, mempl, items.immature[i], items.live[i]])
    } else {
      p.push([n, 0, items.immature[i], items.live[i]])
    }
  })

  if (mempl === 0) {
    p.push([mPrice, mCount, 0, 0]) // add mempool
    p = p.sort(Comparator)
  }

  return p
}

function getVal (val) { return isNaN(val) ? 0 : val }

function outputsGraphData (items) {
  return [
    getVal(items.solo),
    getVal(items.pooled),
    getVal(items.txsplit)
  ]
}

function getWindow (val) {
  switch (val) {
    case 'day': return [(ms - 8.64E+07) - 1000, ms]
    case 'wk': return [(ms - 6.048e+8) - 1000, ms]
    case 'mo': return [(ms - 2.628e+9) - 1000, ms]
    default: return [origDate, ms]
  }
}

var commonOptions = {
  retainDateWindow: false,
  showRangeSelector: true,
  digitsAfterDecimal: 8,
  fillGraph: true,
  stackedGraph: true,
  plotter: barChartPlotter,
  legendFormatter: legendFormatter,
  labelsSeparateLines: true,
  ylabel: 'Number of Tickets',
  legend: 'follow'
}

export default class extends Controller {
  static get targets () {
    return [ 'zoom', 'bars', 'age', 'wrapper' ]
  }

  initialize () {
    var controller = this
    controller.mempool = false
    controller.tipHeight = 0
    controller.purchasesGraph = null
    controller.priceGraph = null
    controller.outputsGraph = null
    controller.graphData = {
      'time_chart': null,
      'price_chart': null,
      'donut_chart': null
    }
    controller.zoom = 'day'
    controller.bars = 'all'
    $.getScript('/js/vendor/dygraphs.min.js', () => {
      controller.chartCount += 2
      controller.purchasesGraph = controller.makePurchasesGraph()
      controller.priceGraph = controller.makePriceGraph()
    })
    $.getScript('/js/vendor/charts.min.js', () => {
      controller.chartCount += 1
      controller.outputsGraph = controller.makeOutputsGraph()
    })
  }

  connect () {
    var controller = this
    ws.registerEvtHandler('newblock', () => {
      ws.send('getticketpooldata', controller.bars)
    })

    ws.registerEvtHandler('getticketpooldataResp', (evt) => {
      if (evt === '') {
        return
      }
      var data = JSON.parse(evt)
      controller.processData(data)
    })

    controller.fetchAll()
  }

  fetchAll () {
    $('body').addClass('loading')
    var controller = this
    $.ajax({
      type: 'GET',
      url: '/api/ticketpool/charts',
      success: function (data) {
        controller.processData(data)
      },
      complete: function () {
        $('body').removeClass('loading')
      }
    })
  }

  processData (data) {
    var controller = this
    if (data['mempool']) {
      // If mempool data is included, assume the data height is the tip.
      controller.mempool = data['mempool']
      controller.tipHeight = data['height']
    }
    if (data['time_chart']) {
      // Only append the mempool data if this data goes to the tip.
      let mempool = controller.tipHeight === data['height'] ? controller.mempool : false
      controller.graphData['time_chart'] = purchasesGraphData(data['time_chart'], mempool)
      if (controller.purchasesGraph !== null) {
        controller.purchasesGraph.updateOptions({ 'file': controller.graphData['time_chart'] })
        controller.purchasesGraph.resetZoom()
      }
    }
    if (data['price_chart']) {
      controller.graphData['price_chart'] = priceGraphData(data['price_chart'], controller.mempool)
      if (controller.pricesGraph !== null) {
        controller.priceGraph.updateOptions({ 'file': controller.graphData['price_chart'] })
      }
    }
    if (data['donut_chart']) {
      controller.graphData['donut_chart'] = outputsGraphData(data['donut_chart'])
      if (controller.outputsGraph !== null) {
        controller.outputsGraph.data.datasets[0].data = controller.graphData['donut_chart']
        controller.outputsGraph.update()
      }
    }
  }

  disconnect () {
    this.purchasesGraph.destroy()
    this.priceGraph.destroy()

    ws.deregisterEvtHandlers('ticketpool')
    ws.deregisterEvtHandlers('getticketpooldataResp')
  }

  onZoom (e) {
    $(this.zoomTargets).each((i, zoomTarget) => {
      $(zoomTarget).removeClass('btn-active')
    })
    $(e.target).addClass('btn-active')
    this.zoom = e.target.name
    this.purchasesGraph.updateOptions({ dateWindow: getWindow(this.zoom) })
  }

  onBarsChange (e) {
    var controller = this
    $(controller.barsTargets).each((i, barsTarget) => {
      $(barsTarget).removeClass('btn-active')
    })
    controller.bars = e.target.name
    $(e.target).addClass('btn-active')
    $('body').addClass('loading')
    $.ajax({
      type: 'GET',
      url: '/api/ticketpool/bydate/' + controller.bars,
      beforeSend: function () {},
      error: function () {
        $('body').removeClass('loading')
      },
      success: function (data) {
        controller.purchasesGraph.updateOptions({ 'file': purchasesGraphData(data['time_chart']) })
        $('body').removeClass('loading')
      }
    })
  }

  makePurchasesGraph () {
    var d = this.graphData['price_chart'] || [[0, 0, 0, 0, 0]]
    var p = {
      labels: ['Date', 'Mempool Tickets', 'Immature Tickets', 'Live Tickets', 'Ticket Value'],
      colors: ['#FF8C00', '#006600', '#2971FF', '#ff0090'],
      title: 'Tickets Purchase Distribution',
      y2label: 'A.v.g. Tickets Value (DCR)',
      dateWindow: getWindow('day'),
      series: {
        'Ticket Value': {
          axis: 'y2',
          plotter: Dygraph.Plotters.linePlotter
        }
      },
      axes: { y2: { axisLabelFormatter: function (d) { return d.toFixed(1) } } }
    }
    return new Dygraph(
      document.getElementById('tickets_by_purchase_date'),
      d, { ...commonOptions, ...p }
    )
  }

  makePriceGraph () {
    var d = this.graphData['price_chart'] || [[0, 0, 0, 0]]
    var p = {
      labels: ['Price', 'Mempool Tickets', 'Immature Tickets', 'Live Tickets'],
      colors: ['#FF8C00', '#006600', '#2971FF'],
      title: 'Ticket Price Distribution',
      labelsKMB: true,
      xlabel: 'Ticket Price (DCR)'
    }
    return new Dygraph(
      document.getElementById('tickets_by_purchase_price'),
      d, { ...commonOptions, ...p }
    )
  }

  makeOutputsGraph () {
    var d = this.graphData['donut_chart'] || []
    return new Chart(
      document.getElementById('doughnutGraph'), {
        options: {
          width: 200,
          height: 200,
          responsive: false,
          animation: { animateScale: true },
          legend: { position: 'bottom' },
          title: {
            display: true,
            text: 'Number of Ticket Outputs'
          },
          tooltips: {
            callbacks: {
              label: function (tooltipItem, data) {
                var sum = 0
                var currentValue = data.datasets[tooltipItem.datasetIndex].data[tooltipItem.index]
                d.map((u) => { sum += u })
                return currentValue + ' Tickets ( ' + ((currentValue / sum) * 100).toFixed(2) + '% )'
              }
            }
          }
        },
        type: 'doughnut',
        data: {
          labels: ['Solo', 'VSP Tickets', 'TixSplit'],
          datasets: [{
            data: d,
            label: 'Solo Tickets',
            backgroundColor: ['#2971FF', '#FF8C00', '#41BF53'],
            borderColor: ['white', 'white', 'white'],
            borderWidth: 0.5
          }]
        }
      })
  }
}
