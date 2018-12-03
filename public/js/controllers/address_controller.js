/* global Dygraph */
/* global QRCode */
/* global $ */
/* global Turbolinks */
import { Controller } from 'stimulus'
import { isEmpty } from 'lodash-es'
import { barChartPlotter } from '../helpers/chart_helper'
import globalEventBus from '../services/event_bus_service'
import Url from 'url-parse'

function txTypesFunc (d) {
  var p = []

  d.time.map((n, i) => {
    p.push([new Date(n), d.sentRtx[i], d.receivedRtx[i], d.tickets[i], d.votes[i], d.revokeTx[i]])
  })
  return p
}

function amountFlowFunc (d) {
  var p = []

  d.time.map((n, i) => {
    var v = d.net[i]
    var netReceived = 0
    var netSent = 0

    v > 0 ? (netReceived = v) : (netSent = (v * -1))
    p.push([new Date(n), d.received[i], d.sent[i], netReceived, netSent])
  })
  return p
}

function unspentAmountFunc (d) {
  var p = []
  // start plotting 6 days before the actual day
  if (d.length > 0) {
    let v = new Date(d.time[0])
    p.push([new Date().setDate(v.getDate() - 6), 0])
  }

  d.time.map((n, i) => p.push([new Date(n), d.amount[i]]))
  return p
}

function formatter (data) {
  var html = this.getLabels()[0] + ': ' + ((data.xHTML === undefined) ? '' : data.xHTML)
  data.series.map(function (series) {
    if (series.color === undefined) return ''
    var l = `<span style="color: ` + series.color + ';"> ' + series.labelHTML
    html = `<span style="color:#2d2d2d;">` + html + `</span>`
    html += '<br>' + series.dashHTML + l + ': ' + (isNaN(series.y) ? '' : series.y) + '</span>'
  })
  return html
}

function customizedFormatter (data) {
  var html = this.getLabels()[0] + ': ' + ((data.xHTML === undefined) ? '' : data.xHTML)
  data.series.map(function (series) {
    if (series.color === undefined) return ''
    if (series.y === 0 && series.labelHTML.includes('Net')) return ''
    var l = `<span style="color: ` + series.color + ';"> ' + series.labelHTML
    html = `<span style="color:#2d2d2d;">` + html + `</span>`
    html += '<br>' + series.dashHTML + l + ': ' + (isNaN(series.y) ? '' : series.y + ' DCR') + '</span> '
  })
  return html
}

function plotGraph (processedData, otherOptions) {
  var commonOptions = {
    digitsAfterDecimal: 8,
    showRangeSelector: true,
    legend: 'follow',
    xlabel: 'Date',
    fillAlpha: 0.9,
    labelsKMB: true
  }

  return new Dygraph(
    document.getElementById('history-chart'),
    processedData,
    { ...commonOptions, ...otherOptions }
  )
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

class TurboQuery {
  constructor (turbolinks) {
    var ta = this
    ta.replaceTimer = 0
    ta.appendTimer = 0
    ta.turbolinks = turbolinks || Turbolinks || false
    if (!ta.turbolinks) {
      console.error('No passed or global Turbolinks instance detected. TurboQuery requires Turbolinks.')
      return
    }
    // These are timer callbacks. Bind them to the TurboQuery instance.
    ta.replaceHistory = ta._replaceHistory.bind(ta)
    ta.appendHistory = ta._appendHistory.bind(ta)
    ta.url = Url(window.location.href, true)
  }

  replaceHref () {
    // Rerouting through timer to prevent spamming.
    // Turbolinks blocks replacement if frequency too high.
    if (this.replaceTimer === 0) {
      this.replaceTimer = setTimeout(this.replaceHistory, 250)
    }
  }

  toHref () {
    if (this.appendTimer === 0) {
      this.appendTimer = setTimeout(this.appendHistory, 250)
    }
  }

  _replaceHistory () {
    // see https://github.com/turbolinks/turbolinks/issues/219. This also works:
    // window.history.replaceState(window.history.state, this.addr, this.url.href)
    this.turbolinks.controller.replaceHistoryWithLocationAndRestorationIdentifier(this.turbolinks.Location.wrap(this.url.href), this.turbolinks.uuid())
    this.replaceTimer = 0
  }

  _appendHistory () {
    // same as replaceHref, but creates a new entry in history for navigating
    // with the browsers forward and back buttons. May still not work because of
    // TurboLinks caching behavior, I think.
    this.turbolinks.controller.pushHistoryWithLocationAndRestorationIdentifier(this.turbolinks.Location.wrap(this.url.href), this.turbolinks.uuid())
    this.appendTimer = 0
  }

  replace (query) {
    this.url.set('query', this.filteredQuery(query))
    this.replaceHref()
  }

  to (query) {
    this.url.set('query', this.filteredQuery(query))
    this.toHref()
  }

  filteredQuery (query) {
    var filtered = {}
    Object.keys(query).forEach(function (key) {
      var v = query[key]
      if (typeof v === 'undefined' || v === null) return
      filtered[key] = v
    })
    return filtered
  }

  update (target) {
    // Update projects the current query parameters onto the given template.
    return this.constructor.project(target, this.parsed)
  }

  get parsed () {
    return this.url.query
  }

  get count () {
    return Object.keys(this.url.query).length
  }

  get (key) {
    if (this.url.query.hasOwnProperty(key)) {
      return TurboQuery.parseValue(this.url.query[key])
    }
    return null
  }

  static parseValue (v) {
    switch (v) {
      case 'null':
        return null
      case '':
        return null
      case 'undefined':
        return null
      case 'false':
        return false
      case 'true':
        return true
    }
    if (!isNaN(parseFloat(v)) && isFinite(v)) {
      if (String(v).includes('.')) {
        return parseFloat(v)
      } else {
        return parseInt(v)
      }
    }
    return v
  }

  static project (target, source) {
    // project fills in the properties of the given template, if they exist in
    // the source. Extraneous source properties are not added to the template.
    var keys = Object.keys(target)
    var idx
    for (idx in keys) {
      var k = keys[idx]
      if (source.hasOwnProperty(k)) {
        target[k] = this.parseValue(source[k])
      }
    }
    return target
  }
}

export default class extends Controller {
  static get targets () {
    return ['options', 'addr', 'btns', 'unspent',
      'flow', 'zoom', 'interval', 'numUnconfirmed', 'formattedTime',
      'pagesize', 'txntype', 'txnCount', 'qron', 'qroff']
  }

  initialize () {
    var controller = this
    controller.retrievedData = {}
    // Bind functions passed as callbacks to the controller
    controller.updateView = controller._updateView.bind(controller)
    controller.zoomCallback = controller._zoomCallback.bind(controller)
    controller.zoomMap = {
      all: 0,
      year: 3.154e+10,
      month: 2.628e+9,
      week: 6.048e+8,
      day: 8.64e+7
    }
    controller.query = new TurboQuery()
    controller.viewSettings = controller.makeViewSettings('list', null, null, null, null, null)
    controller.currentView = controller.makeViewSettings(null, null, null, null, null, null)
    // Set initial viewSettings from the url
    controller.query.update(controller.viewSettings)
    // Set the initial view based on the url
    controller.setViewButton(controller.viewSettings.view === 'list' ? 'list' : 'chart')
    let isFirstFire = true
    globalEventBus.on('BLOCK_RECEIVED', function (data) {
      // The update of the Time UTC and transactions count will only happen during the first confirmation
      if (!isFirstFire) {
        return
      }
      isFirstFire = false
      controller.numUnconfirmedTargets.forEach((el, i) => {
        el.classList.add('hidden')
      })
      let numConfirmed = 0
      controller.formattedTimeTargets.forEach((el, i) => {
        el.textContent = data.block.formatted_time
        numConfirmed++
      })
      controller.txnCountTargets.forEach((el, i) => {
        let transactions = numConfirmed + parseInt(el.dataset.txnCount)
        controller.setTxnCountText(el, transactions)
      })
    })
    $.getScript('/js/vendor/dygraphs.min.js', () => {
      controller.typesGraphOptions = {
        labels: ['Date', 'Sending (regular)', 'Receiving (regular)', 'Tickets', 'Votes', 'Revocations'],
        colors: ['#69D3F5', '#2971FF', '#41BF53', 'darkorange', '#FF0090'],
        ylabel: 'Number of Transactions by Type',
        title: 'Transactions Types',
        visibility: [true, true, true, true, true],
        legendFormatter: formatter,
        plotter: barChartPlotter,
        stackedGraph: true,
        fillGraph: false
      }

      controller.amountFlowGraphOptions = {
        labels: ['Date', 'Received', 'Spent', 'Net Received', 'Net Spent'],
        colors: ['#2971FF', '#2ED6A1', '#41BF53', '#FF0090'],
        ylabel: 'Total Amount (DCR)',
        title: 'Sent And Received',
        visibility: [true, false, false, false],
        legendFormatter: customizedFormatter,
        plotter: barChartPlotter,
        stackedGraph: true,
        fillGraph: false
      }

      controller.unspentGraphOptions = {
        labels: ['Date', 'Unspent'],
        colors: ['#41BF53'],
        ylabel: 'Cummulative Unspent Amount (DCR)',
        title: 'Total Unspent',
        plotter: [Dygraph.Plotters.linePlotter, Dygraph.Plotters.fillPlotter],
        legendFormatter: customizedFormatter,
        stackedGraph: false,
        visibility: [true],
        fillGraph: true
      }
    })
  }

  setTxnCountText (el, count) {
    if (el.dataset.formatted) {
      el.textContent = count + ' transaction' + (count > 1 ? 's' : '')
    } else {
      el.textContent = count
    }
  }

  connect () {
    var controller = this
    controller.bindStuff()
    controller.chartElements = $('.chart-display')
    controller.listElements = $('.list-display')
    controller.zoomButtons = $(controller.zoomTarget).children('input')
    controller.binputs = $(controller.intervalTarget).children('input')
    controller.qrOff = $(controller.qroffTarget)
    controller.qrOn = $(controller.qronTarget)
    controller.qrMade = false
    controller.dcrAddress = controller.data.get('dcraddress')
    if (controller.query.get('zoom') != null) {
      controller.zoomButtons.removeClass('btn-active')
    }
    controller.formattedTimeTargets.forEach((el, i) => {
      el.textContent = 'Unconfirmed'
    })
    controller.txnCountTargets.forEach((el, i) => {
      controller.setTxnCountText(el, parseInt(el.dataset.txnCount))
    })
    controller.disableBtnsIfNotApplicable()
    setTimeout(controller.updateView, 0)
  }

  disconnect () {
    if (this.graph !== undefined) {
      this.graph.destroy()
    }
  }

  bindStuff () {
    $('.jsonly').show()
    $('.matchhash').hover(function () {
      hashHighLight($(this).attr('href'), true)
    }, function () {
      hashHighLight($(this).attr('href'), false)
    })
  }

  showQRCode () {
    var controller = this
    function setMargin () {
      controller.qrOff.css({
        margin: '0px 0px 12px',
        opacity: 1,
        height: 'auto'
      }).show()
    }
    if (controller.qrMade) {
      setMargin()
    } else {
      $.getScript(
        '/js/vendor/qrcode.min.js',
        function () {
          controller.qrMade = new QRCode(controller.qroffTarget, controller.dcrAddress)
          setMargin()
        }
      )
    }
    controller.qrOn.hide()
  }

  hideQRCode () {
    this.qrOn.show()
    this.qrOff.hide().css({
      margin: '0',
      opacity: 0,
      height: 0
    })
  }

  makeViewSettings (view, n, start, txntype, zoom, bin) {
    return {
      view: view,
      n: n,
      start: start,
      txntype: txntype,
      zoom: zoom,
      bin: bin
    }
  }

  paginate () {
    Turbolinks.visit(
      window.location.pathname +
      '?txntype=' + this.txntypeTarget.selectedOptions[0].value +
      '&n=' + this.pagesizeTarget.selectedOptions[0].value +
      '&start=' + this.data.get('offset')
    )
  }

  drawGraph () {
    var controller = this
    var settings = controller.viewSettings

    $('#no-bal').addClass('d-hide')
    $('#history-chart').removeClass('d-hide')
    $('#toggle-charts').addClass('d-hide')

    if (controller.unspent === '0' && settings.view === 'unspent') {
      $('#no-bal').removeClass('d-hide')
      $('#history-chart').addClass('d-hide')
      $('body').removeClass('loading')
      return
    }

    // If the view parameters aren't valid, go to default view.
    if (!controller.validGraphView() || !controller.validGraphInterval()) return controller.showList()
    if (settings.view === controller.currentView.view && settings.bin === controller.currentView.bin) {
      // Only the zoom has changed.
      var zoom = controller.decodeZoom(settings.zoom)
      if (zoom) {
        controller.setZoom(zoom.start.getTime(), zoom.end.getTime())
      } else {
        controller.showList()
      }
      return
    }

    // Set the current view to prevent uneccesary reloads.
    TurboQuery.project(controller.currentView, controller.viewSettings)

    // Check for cached data
    if (controller.retrievedData[settings.view]) {
      var viewData = controller.retrievedData[settings.view]
      if (viewData[settings.bin]) {
        controller.processData(settings.view, settings.bin, viewData[settings.bin])
        return
      }
    }

    $('body').addClass('loading')

    $.ajax({
      type: 'GET',
      url: '/api/address/' + controller.dcrAddress + '/' + settings.view + '/' + settings.bin,
      beforeSend: function () {},
      complete: function () { $('body').removeClass('loading') },
      success: function (data) {
        controller.processData(settings.view, settings.bin, data)
      }
    })
  }

  processData (chart, bin, data) {
    var controller = this
    if (!controller.retrievedData[chart]) {
      controller.retrievedData[chart] = {}
    }
    controller.retrievedData[chart][bin] = data
    if (!isEmpty(data)) {
      var newData = []
      var options = {}

      switch (chart) {
        case 'types':
          newData = txTypesFunc(data)
          options = controller.typesGraphOptions
          break

        case 'amountflow':
          newData = amountFlowFunc(data)
          options = controller.amountFlowGraphOptions
          $('#toggle-charts').removeClass('d-hide')
          break

        case 'unspent':
          newData = unspentAmountFunc(data)
          options = controller.unspentGraphOptions
          break
      }

      options.zoomCallback = controller.zoomCallback

      if (controller.graph === undefined) {
        controller.graph = plotGraph(newData, options)
      } else {
        controller.graph.updateOptions({
          ...{ 'file': newData },
          ...options })
      }
      controller.updateFlow()
      controller.xVal = controller.graph.xAxisExtremes()
      var zoom = controller.decodeZoom(controller.viewSettings.zoom)
      if (zoom) controller.setZoom(zoom.start.getTime(), zoom.end.getTime())
    } else {
      $('#no-bal').removeClass('d-hide')
      $('#history-chart').addClass('d-hide')
      $('#toggle-charts').removeClass('d-hide')
    }
  }

  _updateView () {
    var controller = this
    if (controller.query.count === 0 || controller.query.get('view') === 'list') {
      controller.showList()
      return
    }
    controller.showGraph()
    controller.drawGraph()
  }

  showList () {
    var controller = this
    controller.viewSettings.view = 'list'
    TurboQuery.project(controller.currentView, controller.viewSettings)
    controller.query.replace(controller.viewSettings)
    controller.chartElements.addClass('d-hide')
    controller.listElements.removeClass('d-hide')
  }

  showGraph () {
    var controller = this
    controller.viewSettings.bin = controller.getBin()
    controller.query.replace(controller.viewSettings)
    controller.chartElements.removeClass('d-hide')
    controller.listElements.addClass('d-hide')
  }

  validGraphView (view) {
    view = view || this.viewSettings.view
    return this.optionsTarget.namedItem(view) || false
  }

  validGraphInterval (interval) {
    interval = interval || this.viewSettings.bin || this.activeIntervalButton
    return this.binputs.filter("[name='" + interval + "']") || false
  }

  changeView (e) {
    var controller = this
    var settings = controller.viewSettings
    $('.addr-btn').removeClass('btn-active')
    $(e ? e.srcElement : '.chart').addClass('btn-active')
    var view = controller.activeViewButton
    if (view !== 'list') {
      settings.view = controller.chartType
      controller.setGraphQuery() // Triggers chart draw
    } else {
      controller.showList()
    }
  }

  changeGraph (e) {
    this.viewSettings.view = this.chartType
    this.setGraphQuery()
  }

  changeBin (e) {
    var controller = this
    controller.viewSettings.bin = e.target.name
    controller.setIntervalButton(e.target.name)
    this.setGraphQuery()
  }

  setGraphQuery () {
    this.query.replace(this.viewSettings)
    this.updateView()
  }

  updateFlow () {
    if (this.chartType !== 'amountflow') return ''
    for (var i = 0; i < this.flow.length; i++) {
      var d = this.flow[i]
      this.graph.setVisibility(d[0], d[1])
    }
  }

  onZoom (e) {
    var controller = this
    controller.zoomButtons.removeClass('btn-active')
    $(e.srcElement).addClass('btn-active')
    if (controller.graph === undefined) {
      return
    }
    var duration = controller.zoom

    var end = controller.xVal[1]
    var start = duration === 0 ? controller.xVal[0] : end - duration
    controller.setZoom(start, end)
  }

  setZoom (start, end) {
    var controller = this
    $('body').addClass('loading')
    controller.graph.updateOptions({
      dateWindow: [start, end]
    })
    controller.viewSettings.zoom = controller.encodeZoomStamps(start, end)
    controller.query.replace(controller.viewSettings)
    $('body').removeClass('loading')
  }

  encodeZoomDates (start, end) {
    return this.encodeZoomStamps(start.getTime(), end.getTime())
  }

  encodeZoomStamps (start, end) {
    return parseInt(start / 1000).toString(36) + '-' + parseInt(end / 1000).toString(36)
  }

  getBin () {
    var controller = this
    var bin = controller.query.get('bin')
    if (!controller.setIntervalButton(bin)) {
      bin = controller.activeIntervalButton
    }
    return bin
  }

  setIntervalButton (interval) {
    var controller = this
    var button = controller.validGraphInterval(interval)
    if (!button) return false
    controller.binputs.removeClass('btn-active')
    button.addClass('btn-active')
  }

  setViewButton (view) {
    var viewForm = $(this.btnsTarget)
    viewForm.children('input').removeClass('btn-active')
    viewForm.children("input[name='" + view + "']").addClass('btn-active')
  }

  decodeZoom (encoded) {
    if (!encoded) return false
    var range = encoded.split('-')
    if (range.length !== 2) {
      return false
    }
    var start = parseInt(range[0], 36)
    var end = parseInt(range[1], 36)
    if (isNaN(start) || isNaN(end) || end - start <= 0) {
      return false
    }
    return {
      start: new Date(start * 1000),
      end: new Date(end * 1000)
    }
  }

  _zoomCallback (start, end, yRanges) {
    var controller = this
    controller.zoomButtons.removeClass('btn-active')
    controller.viewSettings.zoom = controller.encodeZoomStamps(start, end)
    controller.query.replace(controller.viewSettings)
  }

  disableBtnsIfNotApplicable () {
    var val = new Date(this.addrTarget.dataset.oldestblocktime)
    var d = new Date()

    var pastYear = d.getFullYear() - 1
    var pastMonth = d.getMonth() - 1
    var pastWeek = d.getDate() - 7
    var pastDay = d.getDate() - 1

    this.enabledButtons = []
    var setApplicableBtns = (className, ts, numIntervals) => {
      var isDisabled = (val > new Date(ts)) ||
                    (this.chartType === 'unspent' && this.unspent === '0') ||
                    numIntervals < 2

      if (isDisabled) {
        this.zoomTarget.getElementsByClassName(className)[0].setAttribute('disabled', isDisabled)
        this.intervalTarget.getElementsByClassName(className)[0].setAttribute('disabled', isDisabled)
      }

      if (className !== 'year' && !isDisabled) {
        this.enabledButtons.push(className)
      }
    }

    setApplicableBtns('year', new Date().setFullYear(pastYear), this.intervalTarget.dataset.year)
    setApplicableBtns('month', new Date().setMonth(pastMonth), this.intervalTarget.dataset.month)
    setApplicableBtns('week', new Date().setDate(pastWeek), this.intervalTarget.dataset.week)
    setApplicableBtns('day', new Date().setDate(pastDay), this.intervalTarget.dataset.day)

    if (parseInt(this.intervalTarget.dataset.txcount) < 20 || this.enabledButtons.length === 0) {
      this.enabledButtons[0] = 'all'
    }

    $('input.chart-size').removeClass('btn-active')
    $('input.chart-size.' + this.enabledButtons[0]).addClass('btn-active')
  }

  get chartType () {
    return this.optionsTarget.value
  }

  get unspent () {
    return this.unspentTarget.id
  }

  get activeViewButton () {
    return this.btnsTarget.getElementsByClassName('btn-active')[0].name
  }

  get zoom () {
    var v = this.zoomTarget.getElementsByClassName('btn-active')[0].name
    return this.zoomMap[v]
  }

  get activeIntervalButton () {
    return this.intervalTarget.getElementsByClassName('btn-active')[0].name
  }

  get flow () {
    var ar = []
    var boxes = this.flowTarget.querySelectorAll('input[type=checkbox]')
    boxes.forEach((n) => {
      var intVal = parseFloat(n.value)
      ar.push([isNaN(intVal) ? 0 : intVal, n.checked])
      if (intVal === 2) {
        ar.push([3, n.checked])
      }
    })
    return ar
  }
}
