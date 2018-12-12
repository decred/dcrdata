/* global Turbolinks */
/* global $ */

import { Controller } from 'stimulus'
import { map, assign, merge } from 'lodash-es'
import { barChartPlotter, ensureDygraph } from '../helpers/chart_helper'
import { darkEnabled } from '../services/theme_service'
import { animationFrame } from '../helpers/animation_helper'
import ajax from '../helpers/ajax_helper'

var selectedChart

var Dygraph = window.Dygraph

function legendFormatter (data) {
  if (data.x == null) {
    return `<div class="d-flex flex-wrap justify-content-center align-items-center">
            <div class="pr-3">${this.getLabels()[0]}: N/A</div>
            <div class="d-flex flex-wrap">
            ${map(data.series, (series) => {
    return `<div class="pr-2">${series.dashHTML} ${series.labelHTML}</div>`
  }).join('')}
            </div>
        </div>
        `
  }

  let xAxisAdditionalInfo
  if (selectedChart === 'ticket-by-outputs-windows') {
    let start = data.x * 144
    let end = start + 143
    xAxisAdditionalInfo = ` (Blocks ${start} &mdash; ${end})`
  } else {
    xAxisAdditionalInfo = ''
  }

  return `<div class="d-flex flex-wrap justify-content-center align-items-center">
        <div class="pr-3">${this.getLabels()[0]}: ${data.xHTML}${xAxisAdditionalInfo}</div>
        <div class="d-flex flex-wrap">
        ${map(data.series, (series) => {
    if (!series.isVisible) return
    return `<div class="pr-2">${series.dashHTML} ${series.labelHTML}: ${series.yHTML}</div>`
  }).join('')}
        </div>
    </div>
    `
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

function ticketsFunc (gData) {
  return map(gData.time, (n, i) => {
    return [
      new Date(n),
      gData.valuef[i]
    ]
  })
}

function difficultyFunc (gData) {
  return map(gData.time, (n, i) => { return [new Date(n), gData.difficulty[i]] })
}

function supplyFunc (gData) {
  return map(gData.time, (n, i) => { return [new Date(n), gData.valuef[i]] })
}

function timeBtwBlocksFunc (gData) {
  var d = []
  gData.value.forEach((n, i) => { if (n === 0) { return } d.push([n, gData.valuef[i]]) })
  return d
}

function blockSizeFunc (gData) {
  return map(gData.time, (n, i) => {
    return [new Date(n), gData.size[i]]
  })
}

function blockChainSizeFunc (gData) {
  return map(gData.time, (n, i) => { return [new Date(n), gData.chainsize[i]] })
}

function txPerBlockFunc (gData) {
  return map(gData.value, (n, i) => { return [n, gData.count[i]] })
}

function txPerDayFunc (gData) {
  return map(gData.timestr, (n, i) => { return [new Date(n), gData.count[i]] })
}

function poolSizeFunc (gData) {
  return map(gData.time, (n, i) => { return [new Date(n), gData.sizef[i]] })
}

function poolValueFunc (gData) {
  return map(gData.time, (n, i) => { return [new Date(n), gData.valuef[i]] })
}

function blockFeeFunc (gData) {
  return map(gData.count, (n, i) => { return [n, gData.sizef[i]] })
}

function ticketSpendTypeFunc (gData) {
  return map(gData.height, (n, i) => { return [n, gData.unspent[i], gData.revoked[i]] })
}

function ticketByOutputCountFunc (gData) {
  return map(gData.height, (n, i) => { return [n, gData.solo[i], gData.pooled[i]] })
}

function chainWorkFunc (gData) {
  return map(gData.time, (n, i) => { return [new Date(n), gData.chainwork[i]] })
}

function hashrateFunc (gData) {
  return map(gData.time, (n, i) => { return [new Date(n), gData.nethash[i]] })
}

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
    plotter: Dygraph.Plotters.linePlotter
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
      'rollPeriodInput'
    ]
  }

  connect () {
    ensureDygraph(() => {
      Dygraph = window.Dygraph
      this.drawInitialGraph()
      $(document).on('nightMode', (event, params) => {
        this.chartsView.updateOptions(
          nightModeOptions(params.nightMode)
        )
      })
    })
  }

  disconnect () {
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
      highlightCircleSize: 4
    }

    this.chartsView = new Dygraph(
      this.chartsViewTarget,
      [[1, 1]],
      options
    )
    var val = this.chartSelectTarget.namedItem(window.location.hash.replace('#', ''))
    $(this.chartSelectTarget).val((val ? val.value : 'ticket-price'))
    this.selectChart()
  }

  plotGraph (chartName, data) {
    var d = []
    Turbolinks.controller.replaceHistoryWithLocationAndRestorationIdentifier(Turbolinks.Location.wrap(`#${chartName}`), Turbolinks.uuid())
    var gOptions = {
      rollPeriod: 1
    }
    switch (chartName) {
      case 'ticket-price': // price graph
        d = ticketsFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Price'], true, 'Price (DCR)', 'Date', undefined, false, false))
        break

      case 'ticket-pool-size': // pool size graph
        d = poolSizeFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Pool Size'], false, 'Ticket Pool Size', 'Date',
          undefined, true, false))
        break

      case 'ticket-pool-value': // pool value graph
        d = poolValueFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Ticket Pool Value'], true, 'Ticket Pool Value', 'Date',
          undefined, true, false))
        break

      case 'avg-block-size': // block size graph
        d = blockSizeFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Block Size'], false, 'Block Size', 'Date', undefined, true, false))
        break

      case 'blockchain-size': // blockchain size graph
        d = blockChainSizeFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'BlockChain Size'], true, 'BlockChain Size', 'Date', undefined, false, true))
        break

      case 'tx-per-block': // tx per block graph
        d = txPerBlockFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Number of Transactions Per Block'], false, '# of Transactions', 'Date',
          undefined, false, false))
        break

      case 'tx-per-day': // tx per day graph
        d = txPerDayFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Number of Transactions Per Day'], true, '# of Transactions', 'Date',
          undefined, true, false))
        break

      case 'pow-difficulty': // difficulty graph
        d = difficultyFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Difficulty'], true, 'Difficulty', 'Date', undefined, true, false))
        break

      case 'coin-supply': // supply graph
        d = supplyFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Coin Supply'], true, 'Coin Supply (DCR)', 'Date', undefined, true, false))
        break

      case 'fee-per-block': // block fee graph
        d = blockFeeFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Total Fee'], false, 'Total Fee (DCR)', 'Block Height',
          undefined, true, false))
        break

      case 'duration-btw-blocks': // Duration between blocks graph
        d = timeBtwBlocksFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Duration Between Block'], false, 'Duration Between Block (seconds)', 'Block Height',
          undefined, false, false))
        break

      case 'ticket-spend-type': // Tickets spendtype per block graph
        d = ticketSpendTypeFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Block Height', 'Unspent', 'Revoked'], false, '# of Tickets Spend Type', 'Block Height',
          undefined, false, false), {
          fillGraph: true,
          stackedGraph: true,
          plotter: barChartPlotter
        })
        break

      case 'ticket-by-outputs-windows': // Tickets by output count graph for ticket windows
        d = ticketByOutputCountFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Window', '3 Outputs (likely Solo)', '5 Outputs (likely Pooled)'], false, '# of Tickets By Output Count', 'Ticket Price Window',
          undefined, false, false), {
          fillGraph: true,
          stackedGraph: true,
          plotter: barChartPlotter
        })
        break
      case 'ticket-by-outputs-blocks': // Tickets by output count graph for all blocks
        d = ticketByOutputCountFunc(data)
        assign(gOptions, mapDygraphOptions(
          d,
          ['Block Height', 'Solo', 'Pooled'],
          false,
          '# of Tickets By Output Count',
          'Block Height',
          undefined,
          false,
          false
        ), {
          fillGraph: true,
          stackedGraph: true,
          plotter: barChartPlotter
        })
        break

      case 'chainwork': // Total chainwork over time
        d = chainWorkFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Cumulative Chainwork (exahash)'],
          false, 'Cumulative Chainwork (exahash)', 'Date', undefined, true, false))
        break

      case 'hashrate': // Total chainwork over time
        d = hashrateFunc(data)
        assign(gOptions, mapDygraphOptions(d, ['Date', 'Network Hashrate (terahash/s)'],
          false, 'Network Hashrate (terahash/s)', 'Date', undefined, true, false))
        break
    }
    this.chartsView.updateOptions(gOptions, false)
    this.chartsView.resetZoom()
    $(this.chartWrapperTarget).removeClass('loading')
  }

  selectChart () {
    var selection = this.chartSelectTarget.value
    $(this.rollPeriodInputTarget).val(undefined)
    $(this.chartWrapperTarget).addClass('loading')
    if (selectedChart !== selection) {
      ajax('/api/chart/' + selection, (data) => {
        console.log('got api data', data, this, selection)
        this.plotGraph(selection, data)
      })
      selectedChart = selection
    } else {
      $(this.chartWrapperTarget).removeClass('loading')
    }
    this.zoomOptionTargets.forEach((el) => {
      el.classList.toggle('active', $(el).data('zoom') === 0)
    })
  }

  async onZoom (event) {
    var selectedZoom = parseInt($(event.srcElement).data('zoom'))
    await animationFrame()
    $(this.chartWrapperTarget).addClass('loading')
    await animationFrame()
    this.xVal = this.chartsView.xAxisExtremes()
    if (selectedZoom > 0) {
      let normalizedZoom = selectedZoom
      if (this.xVal[0] >= 1400000000000) {
        normalizedZoom = selectedZoom * 1000 * 300
      } else if (selectedChart === 'ticket-by-outputs-windows') {
        normalizedZoom = selectedZoom / 144
      }
      this.chartsView.updateOptions({
        dateWindow: [(this.xVal[1] - normalizedZoom), this.xVal[1]]
      })
    } else {
      this.chartsView.resetZoom()
    }
    this.zoomOptionTargets.forEach((el) => {
      el.classList.toggle('active', $(el).data('zoom') === selectedZoom)
    })
    await animationFrame()
    $(this.chartWrapperTarget).removeClass('loading')
  }

  async onRollPeriodChange (event) {
    var rollPeriod = parseInt(event.target.value) || 1
    await animationFrame()
    $(this.chartWrapperTarget).addClass('loading')
    await animationFrame()
    this.chartsView.updateOptions({
      rollPeriod: rollPeriod
    })
    await animationFrame()
    $(this.chartWrapperTarget).removeClass('loading')
  }
}
