/* global $ */
import { Controller } from 'stimulus'
import { isEmpty } from 'lodash-es'
import { getDefault } from '../helpers/module_helper'
import { padPoints, sizedBarPlotter, Zoom } from '../helpers/chart_helper'
import globalEventBus from '../services/event_bus_service'
import TurboQuery from '../helpers/turbolinks_helper'
import axios from 'axios'

const blockDuration = 5 * 60000
let Dygraph // lazy loaded on connect

function txTypesFunc (d, binSize) {
  var p = []

  d.time.map((n, i) => {
    p.push([new Date(n), d.sentRtx[i], d.receivedRtx[i], d.tickets[i], d.votes[i], d.revokeTx[i]])
  })

  padPoints(p, binSize)

  return p
}

function amountFlowProcessor (d, binSize) {
  var flowData = []
  var balanceData = []
  var balance = 0

  d.time.map((n, i) => {
    var v = d.net[i]
    var netReceived = 0
    var netSent = 0

    v > 0 ? (netReceived = v) : (netSent = (v * -1))
    flowData.push([new Date(n), d.received[i], d.sent[i], netReceived, netSent])
    balance += v
    balanceData.push([new Date(n), balance])
  })

  padPoints(flowData, binSize)
  padPoints(balanceData, binSize, true)

  return {
    flow: flowData,
    balance: balanceData
  }
}

function formatter (data) {
  var html = this.getLabels()[0] + ': ' + ((data.xHTML === undefined) ? '' : data.xHTML)
  data.series.map((series) => {
    if (series.color === undefined) return ''
    // Skip display of zeros
    if (series.y === 0) return ''
    var l = `<span style="color: ` + series.color + ';"> ' + series.labelHTML
    html = `<span style="color:#2d2d2d;">` + html + `</span>`
    html += '<br>' + series.dashHTML + l + ': ' + (isNaN(series.y) ? '' : series.y) + '</span>'
  })
  return html
}

function customizedFormatter (data) {
  var html = this.getLabels()[0] + ': ' + ((data.xHTML === undefined) ? '' : data.xHTML)
  data.series.map((series) => {
    if (series.color === undefined) return ''
    // Skip display of zeros
    if (series.y === 0) return ''
    var l = `<span style="color: ` + series.color + ';"> ' + series.labelHTML
    html = `<span style="color:#2d2d2d;">` + html + `</span>`
    html += '<br>' + series.dashHTML + l + ': ' + (isNaN(series.y) ? '' : series.y + ' DCR') + '</span> '
  })
  return html
}

function hashHighLight (matchHash, hoverOn) {
  $('.hash').each(function () {
    var thisHash = $(this).attr('href')
    if (thisHash === matchHash && hoverOn) {
      $(this).addClass('matching-hash')
    } else {
      $(this).removeClass('matching-hash')
    }
  })
}

function setTxnCountText (el, count) {
  if (el.dataset.formatted) {
    el.textContent = count + ' transaction' + (count > 1 ? 's' : '')
  } else {
    el.textContent = count
  }
}

var commonOptions, typesGraphOptions, amountFlowGraphOptions, balanceGraphOptions
// Cannot set these until DyGraph is fetched.
function createOptions () {
  commonOptions = {
    digitsAfterDecimal: 8,
    showRangeSelector: true,
    rangeSelectorHeight: 20,
    rangeSelectorForegroundStrokeColor: '#999',
    rangeSelectorBackgroundStrokeColor: '#777',
    legend: 'follow',
    xlabel: 'Date',
    fillAlpha: 0.9,
    labelsKMB: true
  }

  typesGraphOptions = {
    labels: ['Date', 'Sending (regular)', 'Receiving (regular)', 'Tickets', 'Votes', 'Revocations'],
    colors: ['#69D3F5', '#2971FF', '#41BF53', 'darkorange', '#FF0090'],
    ylabel: 'Number of Transactions by Type',
    title: 'Transactions Types',
    visibility: [true, true, true, true, true],
    legendFormatter: formatter,
    stackedGraph: true,
    fillGraph: false
  }

  amountFlowGraphOptions = {
    labels: ['Date', 'Received', 'Spent', 'Net Received', 'Net Spent'],
    colors: ['#2971FF', '#2ED6A1', '#41BF53', '#FF0090'],
    ylabel: 'Total Amount (DCR)',
    title: 'Sent And Received',
    visibility: [true, false, false, false],
    legendFormatter: customizedFormatter,
    stackedGraph: true,
    fillGraph: false
  }

  balanceGraphOptions = {
    labels: ['Date', 'Balance'],
    colors: ['#41BF53'],
    ylabel: 'Balance (DCR)',
    title: 'Balance',
    plotter: [Dygraph.Plotters.linePlotter, Dygraph.Plotters.fillPlotter],
    legendFormatter: customizedFormatter,
    stackedGraph: false,
    visibility: [true],
    fillGraph: true
  }
}

export default class extends Controller {
  static get targets () {
    return ['options', 'addr', 'btns', 'balance',
      'flow', 'zoom', 'interval', 'numUnconfirmed', 'formattedTime',
      'pagesize', 'txntype', 'txnCount', 'qricon', 'qrimg', 'qrbox',
      'paginator', 'pageplus', 'pageminus', 'listbox', 'table',
      'range', 'chartbox', 'noconfirms', 'chart', 'pagebuttons']
  }

  async connect () {
    var ctrl = this
    ctrl.retrievedData = {}
    ctrl.ajaxing = false
    ctrl.qrCode = false
    ctrl.requestedChart = false
    // Bind functions that are passed as callbacks
    ctrl.updateView = ctrl._updateView.bind(ctrl)
    ctrl.zoomCallback = ctrl._zoomCallback.bind(ctrl)
    ctrl.drawCallback = ctrl._drawCallback.bind(ctrl)
    ctrl.bindElements()
    ctrl.bindEvents()
    ctrl.query = new TurboQuery()

    // These two are templates for query parameter sets.
    // When url query parameters are set, these will also be updated.
    ctrl.chartSettings = TurboQuery.nullTemplate(['chart', 'zoom', 'bin', 'flow'])
    ctrl.listSettings = TurboQuery.nullTemplate(['n', 'start', 'txntype'])

    ctrl.chartState = Object.assign({}, ctrl.chartSettings)

    // Get initial view settings from the url
    ctrl.query.update(ctrl.chartSettings)
    ctrl.query.update(ctrl.listSettings)
    ctrl.currentTab = ctrl.query.get('chart') ? 'chart' : 'list'
    ctrl.setViewButton(ctrl.currentTab === 'list' ? 'list' : 'chart')
    ctrl.setChartType()
    if (ctrl.chartSettings.flow) ctrl.setFlowChecks()
    if (ctrl.chartSettings.zoom !== null) {
      ctrl.zoomButtons.removeClass('btn-selected')
    }

    // Parse stimulus data
    var cdata = ctrl.data
    ctrl.dcrAddress = cdata.get('dcraddress')
    ctrl.paginationParams = {
      'offset': parseInt(cdata.get('offset')),
      'all': parseInt(cdata.get('fundingCount')) + parseInt(cdata.get('spendingCount')),
      'credit': parseInt(cdata.get('fundingCount')),
      'debit': parseInt(cdata.get('spendingCount')),
      'merged_debit': parseInt(cdata.get('mergedCount'))
    }
    ctrl.balance = cdata.get('balance')

    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )
    ctrl.initializeChart()
    setTimeout(ctrl.updateView, 0)
  }

  disconnect () {
    if (this.graph !== undefined) {
      this.graph.destroy()
    }
    this.retrievedData = {}
  }

  // Request the initial chart data, grabbing the Dygraph script if necessary.
  initializeChart () {
    createOptions()
    // If no chart data has been requested, e.g. when initially on the
    // list tab, then fetch the initial chart data.
    if (!this.requestedChart) {
      this.fetchGraphData(this.chartType, this.getBin())
    }
  }

  bindElements () {
    var ctrl = this
    ctrl.chartElements = $('.chart-display')
    ctrl.listElements = $('.list-display')
    ctrl.zoomButtons = $(ctrl.zoomTarget).children('button')
    ctrl.binputs = $(ctrl.intervalTarget).children('button')
    ctrl.flowBoxes = ctrl.flowTarget.querySelectorAll('input[type=checkbox]')
    ctrl.pageSizeOptions = ctrl.pagesizeTarget.querySelectorAll('option')
    ctrl.qrImg = $(ctrl.qrimgTarget)
    ctrl.qrIcon = $(ctrl.qriconTarget)
    ctrl.qrBox = $(ctrl.qrboxTarget)
  }

  bindEvents () {
    var ctrl = this
    var isFirstFire = true
    globalEventBus.on('BLOCK_RECEIVED', (data) => {
      // The update of the Time UTC and transactions count will only happen during the first confirmation
      if (!isFirstFire) {
        return
      }
      isFirstFire = false
      ctrl.numUnconfirmedTargets.forEach((el, i) => {
        el.classList.add('hidden')
      })
      var numConfirmed = 0
      ctrl.formattedTimeTargets.forEach((el, i) => {
        el.textContent = data.block.formatted_time
        numConfirmed++
      })
      ctrl.txnCountTargets.forEach((el, i) => {
        var transactions = numConfirmed + parseInt(el.dataset.txnCount)
        setTxnCountText(el, transactions)
      })
    })
    $('.matchhash').hover(function () {
      hashHighLight($(this).attr('href'), true)
    }, function () {
      hashHighLight($(this).attr('href'), false)
    })
    ctrl.paginatorTargets.forEach((link) => {
      link.addEventListener('click', (e) => {
        e.preventDefault()
      })
    })
  }

  async showQRCode () {
    this.qrBox.show()
    if (this.qrCode) {
      this.qrImg.css({ opacity: 1 })
    } else {
      let QRCode = await getDefault(
        import(/* webpackChunkName: "qrcode" */ 'qrcode')
      )
      let qrCodeImg = await QRCode.toDataURL(this.dcrAddress,
        {
          errorCorrectionLevel: 'H',
          scale: 8,
          margin: 0
        }
      )
      this.qrimgTarget.innerHTML = `<img src="${qrCodeImg}"/>`
      this.qrimgTarget.style.opacity = '1'
    }
    this.qrIcon.hide()
  }

  hideQRCode () {
    this.qrIcon.show()
    this.qrBox.hide()
    this.qrImg.css({ opacity: 0 })
  }

  makeTableUrl (txType, count, offset) {
    return '/addresstable/' + this.dcrAddress + '?txntype=' +
    txType + '&n=' + count + '&start=' + offset
  }

  changePageSize () {
    this.fetchTable(this.txnType, this.pageSize, this.paginationParams.offset)
  }

  changeTxType () {
    this.fetchTable(this.txnType, this.pageSize, 0)
  }

  nextPage () {
    this.toPage(1)
  }

  prevPage () {
    this.toPage(-1)
  }

  toPage (direction) {
    var ctrl = this
    var params = ctrl.paginationParams
    var count = ctrl.pageSize
    var txType = ctrl.txnType
    var requestedOffset = params.offset + count * direction
    if (requestedOffset >= params[txType]) return
    if (requestedOffset < 0) requestedOffset = 0
    ctrl.fetchTable(txType, count, requestedOffset)
  }

  async fetchTable (txType, count, offset) {
    var ctrl = this
    ctrl.listboxTarget.classList.add('loading')
    let tableResponse = await axios.get(ctrl.makeTableUrl(txType, count, offset))
    let html = tableResponse.data
    ctrl.tableTarget.innerHTML = html
    var settings = ctrl.listSettings
    settings.n = count
    settings.start = offset
    settings.txntype = txType
    ctrl.query.replace(settings)
    ctrl.paginationParams.offset = offset
    ctrl.setPageability()
    ctrl.listboxTarget.classList.remove('loading')
  }

  setPageability () {
    var ctrl = this
    var params = ctrl.paginationParams
    var rowMax = params[ctrl.txnType]
    var count = ctrl.pageSize
    if (rowMax > count) {
      ctrl.pagebuttonsTarget.classList.remove('d-hide')
    } else {
      ctrl.pagebuttonsTarget.classList.add('d-hide')
    }
    const setAbility = (el, state) => {
      if (state) {
        el.classList.remove('disabled')
      } else {
        el.classList.add('disabled')
      }
    }
    setAbility(ctrl.pageplusTarget, params.offset + count < rowMax)
    setAbility(ctrl.pageminusTarget, params.offset - count >= 0)
    ctrl.pageSizeOptions.forEach((option) => {
      if (option.value > 100) {
        if (rowMax > 100) {
          option.disabled = false
          option.text = option.value = Math.min(rowMax, 1000)
        } else {
          option.disabled = true
          option.text = option.value = 1000
        }
      } else {
        option.disabled = rowMax <= option.value
      }
    })
    setAbility(ctrl.pagesizeTarget, rowMax > 20)
    var suffix = rowMax > 1 ? 's' : ''
    var rangeEnd = params.offset + count
    if (rangeEnd > rowMax) rangeEnd = rowMax
    ctrl.rangeTarget.innerHTML = 'showing ' + (params.offset + 1) + ' &ndash; ' +
    rangeEnd + ' of ' + rowMax + ' transaction' + suffix
  }

  createGraph (processedData, otherOptions) {
    return new Dygraph(
      this.chartTarget,
      processedData,
      { ...commonOptions, ...otherOptions }
    )
  }

  drawGraph () {
    var ctrl = this
    var settings = ctrl.chartSettings

    ctrl.noconfirmsTarget.classList.add('d-hide')
    ctrl.chartTarget.classList.remove('d-hide')

    // If the view parameters aren't valid, go to default (list) view.
    if (!ctrl.validChartType() || !ctrl.validGraphInterval()) return ctrl.showList()

    if (settings.chart === ctrl.chartState.chart && settings.bin === ctrl.chartState.bin) {
      // Only the zoom has changed.
      let zoom = Zoom.decode(settings.zoom)
      if (zoom) {
        ctrl.setZoom(zoom.start, zoom.end)
      }
      return
    }

    // Set the current view to prevent uneccesary reloads.
    Object.assign(ctrl.chartState, settings)
    ctrl.fetchGraphData(settings.chart, settings.bin)
  }

  async fetchGraphData (chart, bin) {
    var ctrl = this
    var cacheKey = chart + '-' + bin
    if (ctrl.ajaxing === cacheKey) {
      return
    }
    ctrl.requestedChart = cacheKey
    ctrl.ajaxing = cacheKey

    ctrl.chartboxTarget.classList.add('loading')

    // Check for cached data
    if (ctrl.retrievedData[cacheKey]) {
      // Queue the function to allow the loader to display.
      setTimeout(() => {
        ctrl.popChartCache(chart, bin)
        ctrl.chartboxTarget.classList.remove('loading')
        ctrl.ajaxing = false
      }, 10) // 0 should work but doesn't always
      return
    }
    var chartKey = chart === 'balance' ? 'amountflow' : chart
    let url = '/api/address/' + ctrl.dcrAddress + '/' + chartKey + '/' + bin
    let graphDataResponse = await axios.get(url)
    ctrl.processData(chart, bin, graphDataResponse.data)
    ctrl.ajaxing = false
    ctrl.chartboxTarget.classList.remove('loading')
  }

  processData (chart, bin, data) {
    var ctrl = this
    if (isEmpty(data)) {
      ctrl.noDataAvailable()
      return
    }
    var binSize = Zoom.mapValue(bin) || blockDuration
    if (chart === 'types') {
      ctrl.retrievedData['types-' + bin] = txTypesFunc(data, binSize)
    } else if (chart === 'amountflow' || chart === 'balance') {
      let processed = amountFlowProcessor(data, binSize)
      ctrl.retrievedData['amountflow-' + bin] = processed.flow
      ctrl.retrievedData['balance-' + bin] = processed.balance
    } else return
    setTimeout(() => {
      ctrl.popChartCache(chart, bin)
    }, 0)
  }

  popChartCache (chart, bin) {
    var ctrl = this
    var cacheKey = chart + '-' + bin
    var binSize = Zoom.mapValue(bin) || blockDuration
    if (!ctrl.retrievedData[cacheKey] ||
        ctrl.requestedChart !== cacheKey ||
        ctrl.currentTab === 'list'
    ) {
      return
    }
    var data = ctrl.retrievedData[cacheKey]
    var options = null
    ctrl.flowTarget.classList.add('d-hide')
    switch (chart) {
      case 'types':
        options = typesGraphOptions
        options.plotter = sizedBarPlotter(binSize)
        break

      case 'amountflow':
        options = amountFlowGraphOptions
        options.plotter = sizedBarPlotter(binSize)
        ctrl.flowTarget.classList.remove('d-hide')
        break

      case 'balance':
        options = balanceGraphOptions
        break
    }
    options.zoomCallback = ctrl.zoomCallback
    options.drawCallback = ctrl.drawCallback
    if (ctrl.graph === undefined) {
      ctrl.graph = ctrl.createGraph(data, options)
    } else {
      ctrl.graph.updateOptions({
        ...{ 'file': data },
        ...options })
    }
    if (chart === 'amountflow') {
      ctrl.updateFlow()
    }
    ctrl.chartboxTarget.classList.remove('loading')
    ctrl.xRange = ctrl.graph.xAxisExtremes()
    ctrl.validateZoom(binSize)
  }

  noDataAvailable () {
    this.noconfirmsTarget.classList.remove('d-hide')
    this.chartTarget.classList.add('d-hide')
    this.chartboxTarget.classList.remove('loading')
    this.flowTarget.classList.add('d-hide')
  }

  _updateView () {
    var ctrl = this
    if (ctrl.query.count === 0 || ctrl.currentTab === 'list') {
      ctrl.showList()
      return
    }
    ctrl.showGraph()
    ctrl.drawGraph()
  }

  showList () {
    var ctrl = this
    ctrl.currentTab = 'list'
    ctrl.query.replace(ctrl.listSettings)
    ctrl.chartElements.addClass('d-hide')
    ctrl.listElements.removeClass('d-hide')
  }

  showGraph () {
    var ctrl = this
    var settings = ctrl.chartSettings
    settings.bin = ctrl.getBin()
    settings.flow = settings.chart === 'amountflow' ? ctrl.flow : null
    ctrl.query.replace(settings)
    ctrl.chartElements.removeClass('d-hide')
    ctrl.listElements.addClass('d-hide')
  }

  validChartType (chart) {
    chart = chart || this.chartSettings.chart
    return this.optionsTarget.namedItem(chart) || false
  }

  validGraphInterval (interval) {
    interval = interval || this.chartSettings.bin || this.activeBin
    return this.binputs.filter("[name='" + interval + "']") || false
  }

  validateZoom (binSize) {
    var ctrl = this
    ctrl.setButtonVisibility()
    var zoom = Zoom.validate(ctrl.chartSettings.zoom || ctrl.activeZoomKey, ctrl.xRange, binSize)
    ctrl.setZoom(zoom.start, zoom.end)
    if (Zoom.equals(ctrl.chartSettings.zoom, ctrl.graph.xAxisExtremes())) ctrl.setSelectedZoom('all')
  }

  changeView (e) {
    var ctrl = this
    var target = e.srcElement || e.target
    if (target.nodeName !== 'INPUT') return
    $('.addr-btn').removeClass('btn-active')
    target.classList.add('btn-active')
    var view = ctrl.activeView
    if (view !== 'list') {
      ctrl.currentTab = 'chart'
      ctrl.chartSettings.chart = ctrl.chartType
      ctrl.setGraphQuery() // Triggers chart draw
      this.updateView()
    } else {
      ctrl.showList()
    }
  }

  changeGraph (e) {
    this.chartSettings.chart = this.chartType
    this.setGraphQuery()
    this.updateView()
  }

  changeBin (e) {
    var ctrl = this
    var target = e.srcElement || e.target
    if (target.nodeName !== 'BUTTON') return
    ctrl.chartSettings.bin = target.name
    ctrl.setIntervalButton(target.name)
    this.setGraphQuery()
    this.updateView()
  }

  setGraphQuery () {
    this.query.replace(this.chartSettings)
  }

  updateFlow () {
    var ctrl = this
    var bitmap = ctrl.flow
    if (bitmap === 0) {
      // If all boxes are unchecked, just leave the last view
      // in place to prevent chart errors with zero visible datasets
      return
    }
    ctrl.chartSettings.flow = bitmap
    ctrl.setGraphQuery()
    // Set the graph dataset visibility based on the bitmap
    // Dygraph dataset indices: 0 received, 1 sent, 2 & 3 net
    var visibility = {}
    visibility[0] = bitmap & 1
    visibility[1] = bitmap & 2
    visibility[2] = visibility[3] = bitmap & 4
    Object.keys(visibility).forEach((idx) => {
      ctrl.graph.setVisibility(idx, visibility[idx])
    })
  }

  setFlowChecks () {
    var bitmap = this.chartSettings.flow
    this.flowBoxes.forEach((box) => {
      box.checked = bitmap & parseInt(box.value)
    })
  }

  onZoom (e) {
    var ctrl = this
    var target = e.srcElement || e.target
    if (target.nodeName !== 'BUTTON') return
    ctrl.zoomButtons.removeClass('btn-selected')
    target.classList.add('btn-selected')
    if (ctrl.graph === undefined) {
      return
    }
    var duration = ctrl.activeZoomDuration

    var end = ctrl.xRange[1]
    var start = duration === 0 ? ctrl.xRange[0] : end - duration
    ctrl.setZoom(start, end)
  }

  setZoom (start, end) {
    var ctrl = this
    ctrl.chartboxTarget.classList.add('loading')
    ctrl.graph.updateOptions({
      dateWindow: [start, end]
    })
    ctrl.chartSettings.zoom = Zoom.encode(start, end)
    ctrl.query.replace(ctrl.chartSettings)
    ctrl.chartboxTarget.classList.remove('loading')
  }

  getBin () {
    var ctrl = this
    var bin = ctrl.query.get('bin')
    if (!ctrl.setIntervalButton(bin)) {
      bin = ctrl.activeBin
    }
    return bin
  }

  setIntervalButton (interval) {
    var ctrl = this
    var button = ctrl.validGraphInterval(interval)
    if (!button) return false
    ctrl.binputs.removeClass('btn-selected')
    button.addClass('btn-selected')
  }

  setViewButton (view) {
    var viewForm = $(this.btnsTarget)
    viewForm.children('input').removeClass('btn-active')
    viewForm.children("input[name='" + view + "']").addClass('btn-active')
  }

  setChartType () {
    var chart = this.chartSettings.chart
    if (this.validChartType(chart)) {
      this.optionsTarget.value = chart
    }
  }

  setSelectedZoom (zoomKey) {
    this.zoomButtons.each(function (i, button) {
      if (button.name === zoomKey) {
        button.classList.add('btn-selected')
      } else {
        button.classList.remove('btn-selected')
      }
    })
  }

  _drawCallback (graph, first) {
    var ctrl = this
    if (first) return
    var start, end
    [start, end] = ctrl.graph.xAxisRange()
    if (start === end) return
    ctrl.chartSettings.zoom = Zoom.encode(start, end)
    ctrl.query.replace(ctrl.chartSettings)
    if (Zoom.equals(ctrl.chartSettings.zoom, ctrl.graph.xAxisExtremes())) ctrl.setSelectedZoom('all')
  }

  _zoomCallback (start, end) {
    var ctrl = this
    ctrl.zoomButtons.removeClass('btn-selected')
    ctrl.chartSettings.zoom = Zoom.encode(start, end)
    ctrl.query.replace(ctrl.chartSettings)
    if (Zoom.equals(ctrl.chartSettings.zoom, ctrl.graph.xAxisExtremes())) ctrl.setSelectedZoom('all')
  }

  setButtonVisibility () {
    var ctrl = this
    var duration = ctrl.chartDuration
    var buttonSets = [ctrl.zoomButtons, ctrl.binputs]
    buttonSets.forEach((buttonSet) => {
      buttonSet.each((i, button) => {
        if (button.dataset.fixed) return
        if (duration > Zoom.mapValue(button.name)) {
          button.classList.remove('d-hide')
        } else {
          button.classList.remove('btn-selected')
          button.classList.add('d-hide')
        }
      })
    })
  }

  get chartType () {
    return this.optionsTarget.value
  }

  get activeView () {
    return this.btnsTarget.getElementsByClassName('btn-active')[0].name
  }

  get activeZoomDuration () {
    return this.activeZoomKey ? Zoom.mapValue(this.activeZoomKey) : false
  }

  get activeZoomKey () {
    var activeButtons = this.zoomTarget.getElementsByClassName('btn-selected')
    if (activeButtons.length === 0) return null
    return activeButtons[0].name
  }

  get chartDuration () {
    return this.xRange[1] - this.xRange[0]
  }

  get activeBin () {
    return this.intervalTarget.getElementsByClassName('btn-selected')[0].name
  }

  get flow () {
    var base10 = 0
    this.flowBoxes.forEach((box) => {
      if (box.checked) base10 += parseInt(box.value)
    })
    return base10
  }

  get txnType () {
    return this.txntypeTarget.selectedOptions[0].value
  }

  get pageSize () {
    var selected = this.pagesizeTarget.selectedOptions
    return selected.length ? parseInt(selected[0].value) : 20
  }
}
