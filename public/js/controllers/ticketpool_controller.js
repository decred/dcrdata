import { Controller } from 'stimulus'
import ws from '../services/messagesocket_service'
import { barChartPlotter } from '../helpers/chart_helper'
import { getDefault } from '../helpers/module_helper'
import axios from 'axios'

let Dygraph // lazy loaded on connect
let Chart // lazy loaded on connect

// Common code for ploting dygraphs
function legendFormatter (data) {
  if (data.x == null) return ''
  var html = this.getLabels()[0] + ': ' + data.xHTML
  data.series.map((series) => {
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

  items.time.map((n, i) => {
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

  async initialize () {
    this.mempool = false
    this.tipHeight = 0
    this.purchasesGraph = null
    this.priceGraph = null
    this.outputsGraph = null
    this.graphData = {
      'time_chart': null,
      'price_chart': null,
      'donut_chart': null
    }
    this.zoom = 'all'
    this.bars = 'all'

    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )
    this.chartCount += 2
    this.purchasesGraph = this.makePurchasesGraph()
    this.priceGraph = this.makePriceGraph()

    Chart = await getDefault(
      import(/* webpackChunkName: "charts" */ '../vendor/charts.min.js')
    )
    this.chartCount += 1
    this.outputsGraph = this.makeOutputsGraph()
  }

  connect () {
    ws.registerEvtHandler('newblock', () => {
      ws.send('getticketpooldata', this.bars)
    })

    ws.registerEvtHandler('getticketpooldataResp', (evt) => {
      if (evt === '') {
        return
      }
      var data = JSON.parse(evt)
      this.processData(data)
    })

    this.fetchAll()
  }

  async fetchAll () {
    this.wrapperTarget.classList.add('loading')
    let chartsResponse = await axios.get('/api/ticketpool/charts')
    this.processData(chartsResponse.data)
    this.wrapperTarget.classList.remove('loading')
  }

  processData (data) {
    if (data['mempool']) {
      // If mempool data is included, assume the data height is the tip.
      this.mempool = data['mempool']
      this.tipHeight = data['height']
    }
    if (data['time_chart']) {
      // Only append the mempool data if this data goes to the tip.
      let mempool = this.tipHeight === data['height'] ? this.mempool : false
      this.graphData['time_chart'] = purchasesGraphData(data['time_chart'], mempool)
      if (this.purchasesGraph !== null) {
        this.purchasesGraph.updateOptions({ 'file': this.graphData['time_chart'] })
        this.purchasesGraph.resetZoom()
      }
    }
    if (data['price_chart']) {
      this.graphData['price_chart'] = priceGraphData(data['price_chart'], this.mempool)
      if (this.priceGraph !== null) {
        this.priceGraph.updateOptions({ 'file': this.graphData['price_chart'] })
      }
    }
    if (data['donut_chart']) {
      this.graphData['donut_chart'] = outputsGraphData(data['donut_chart'])
      if (this.outputsGraph !== null) {
        this.outputsGraph.data.datasets[0].data = this.graphData['donut_chart']
        this.outputsGraph.update()
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
    var target = e.srcElement || e.target
    this.zoomTargets.forEach((zoomTarget) => {
      zoomTarget.classList.remove('btn-active')
    })
    target.classList.add('btn-active')
    this.zoom = e.target.name
    this.purchasesGraph.updateOptions({ dateWindow: getWindow(this.zoom) })
  }

  async onBarsChange (e) {
    var target = e.srcElement || e.target
    this.barsTargets.forEach((barsTarget) => {
      barsTarget.classList.remove('btn-active')
    })
    this.bars = e.target.name
    target.classList.add('btn-active')
    this.wrapperTarget.classList.add('loading')
    var url = '/api/ticketpool/bydate/' + this.bars
    let ticketPoolResponse = await axios.get(url)
    this.purchasesGraph.updateOptions({
      'file': purchasesGraphData(ticketPoolResponse.data['time_chart'])
    })
    this.wrapperTarget.classList.remove('loading')
  }

  makePurchasesGraph () {
    var d = this.graphData['time_chart'] || [[0, 0, 0, 0, 0]]
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
      axes: { y2: { axisLabelFormatter: (d) => { return d.toFixed(1) } } }
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
              label: (tooltipItem, data) => {
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
