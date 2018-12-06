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

function safeStartTurbolinksProgress () {
  if (!Turbolinks.supported) { return }
  Turbolinks.controller.adapter.progressBar.setValue(0)
  Turbolinks.controller.adapter.progressBar.show()
}

function safeStopTurbolinksProgress () {
  if (!Turbolinks.supported) { return }
  Turbolinks.controller.adapter.progressBar.hide()
  Turbolinks.controller.adapter.progressBar.setValue(100)
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
      'pagesize', 'txntype', 'txnCount', 'qron', 'qroff', 'paginator',
      'pageplus', 'pageminus', 'listbox', 'table', 'range']
  }

  initialize () {
    var ctrl = this
    ctrl.retrievedData = {}
    // Bind functions passed as callbacks to the controller
    ctrl.updateView = ctrl._updateView.bind(ctrl)
    ctrl.zoomCallback = ctrl._zoomCallback.bind(ctrl)
    ctrl.drawCallback = ctrl._drawCallback.bind(ctrl)
    ctrl.zoomMap = {
      all: 0,
      year: 3.154e+10,
      month: 2.628e+9,
      week: 6.048e+8,
      day: 8.64e+7
    }
    ctrl.query = new TurboQuery()

    // These two are templates for query parameter sets
    ctrl.chartSettings = {
      view: null,
      zoom: null,
      bin: null,
      flow: null
    }
    ctrl.listSettings = {
      n: null,
      start: null,
      txntype: null
    }

    // A master settings object
    var settings = ctrl.viewSettings = Object.assign({}, ctrl.chartSettings)
    Object.assign(settings, ctrl.listSettings)

    ctrl.currentChartSettings = Object.assign({}, ctrl.chartSettings)
    // Set initial viewSettings from the url
    ctrl.query.update(settings)
    settings.view = settings.view || 'list'
    settings.flow = settings.flow ? settings.flow : null
    TurboQuery.project(ctrl.chartSettings, settings)
    TurboQuery.project(ctrl.listSettings, settings)
    // Set the initial view based on the url
    ctrl.setViewButton(settings.view === 'list' ? 'list' : 'chart')
    ctrl.setChartType()
    $.getScript('/js/vendor/dygraphs.min.js', () => {
      ctrl.typesGraphOptions = {
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

      ctrl.amountFlowGraphOptions = {
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

      ctrl.unspentGraphOptions = {
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
    var ctrl = this
    var cdata = ctrl.data
    ctrl.bindStuff()
    ctrl.chartElements = $('.chart-display')
    ctrl.listElements = $('.list-display')
    ctrl.zoomButtons = $(ctrl.zoomTarget).children('input')
    ctrl.binputs = $(ctrl.intervalTarget).children('input')
    ctrl.flowBoxes = ctrl.flowTarget.querySelectorAll('input[type=checkbox]')
    if (ctrl.viewSettings.flow) ctrl.setFlowChecks()
    ctrl.qrOff = $(ctrl.qroffTarget)
    ctrl.qrOn = $(ctrl.qronTarget)
    ctrl.qrMade = false
    ctrl.dcrAddress = cdata.get('dcraddress')
    ctrl.paginationParams = {
      'offset': parseInt(cdata.get('offset')),
      'all': parseInt(cdata.get('fundingCount')) + parseInt(cdata.get('spendingCount')),
      'credit': parseInt(cdata.get('fundingCount')),
      'debit': parseInt(cdata.get('spendingCount')),
      'merged_debit': parseInt(cdata.get('mergedCount'))
    }
    if (ctrl.chartSettings.zoom !== null) {
      ctrl.zoomButtons.removeClass('btn-active')
    }
    // ctrl.formattedTimeTargets.forEach((el, i) => {
    //   el.textContent = 'Unconfirmed'
    // })
    // ctrl.txnCountTargets.forEach((el, i) => {
    //   ctrl.setTxnCountText(el, parseInt(el.dataset.txnCount))
    // })
    // ctrl.disableBtnsIfNotApplicable()
    setTimeout(ctrl.updateView, 0)
  }

  disconnect () {
    if (this.graph !== undefined) {
      this.graph.destroy()
    }
  }

  bindStuff () {
    var ctrl = this
    let isFirstFire = true
    globalEventBus.on('BLOCK_RECEIVED', function (data) {
      // The update of the Time UTC and transactions count will only happen during the first confirmation
      if (!isFirstFire) {
        return
      }
      isFirstFire = false
      ctrl.numUnconfirmedTargets.forEach((el, i) => {
        el.classList.add('hidden')
      })
      let numConfirmed = 0
      ctrl.formattedTimeTargets.forEach((el, i) => {
        el.textContent = data.block.formatted_time
        numConfirmed++
      })
      ctrl.txnCountTargets.forEach((el, i) => {
        let transactions = numConfirmed + parseInt(el.dataset.txnCount)
        ctrl.setTxnCountText(el, transactions)
      })
    })
    $('.jsonly').show()
    $('.matchhash').hover(function () {
      hashHighLight($(this).attr('href'), true)
    }, function () {
      hashHighLight($(this).attr('href'), false)
    })
    ctrl.paginatorTargets.forEach(function (link) {
      link.addEventListener('click', function (e) {
        e.preventDefault()
      })
    })
  }

  showQRCode () {
    var ctrl = this
    function setMargin () {
      ctrl.qrOff.css({
        margin: '0px 0px 12px',
        opacity: 1,
        height: 'auto'
      }).show()
    }
    if (ctrl.qrMade) {
      setMargin()
    } else {
      $.getScript(
        '/js/vendor/qrcode.min.js',
        function () {
          ctrl.qrMade = new QRCode(ctrl.qroffTarget, ctrl.dcrAddress)
          setMargin()
        }
      )
    }
    ctrl.qrOn.hide()
  }

  hideQRCode () {
    this.qrOn.show()
    this.qrOff.hide().css({
      margin: '0',
      opacity: 0,
      height: 0
    })
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
    if (requestedOffset > params[txType]) return
    if (requestedOffset < 0) requestedOffset = 0
    ctrl.fetchTable(txType, count, requestedOffset)
  }

  fetchTable (txType, count, offset) {
    var ctrl = this
    ctrl.listboxTarget.classList.add('loading')
    safeStartTurbolinksProgress()
    $.ajax({
      type: 'GET',
      url: ctrl.makeTableUrl(txType, count, offset),
      complete: function () {
        ctrl.listboxTarget.classList.remove('loading')
        safeStopTurbolinksProgress()
      },
      success: function (html) {
        ctrl.tableTarget.innerHTML = html
        var settings = ctrl.viewSettings
        settings.n = count
        settings.start = offset
        settings.txntype = txType
        ctrl.query.replace(TurboQuery.project(ctrl.listSettings, settings))
        ctrl.paginationParams.offset = offset
        ctrl.setPageability()
      }
    })
  }

  setPageability () {
    var ctrl = this
    var params = ctrl.paginationParams
    var rowMax = params[ctrl.txnType]
    var count = ctrl.pageSize
    if (params.offset + count > rowMax) {
      ctrl.pageplusTarget.classList.add('disabled')
    } else {
      ctrl.pageplusTarget.classList.remove('disabled')
    }
    if (params.offset - count < 0) {
      ctrl.pageminusTarget.classList.add('disabled')
    } else {
      ctrl.pageminusTarget.classList.remove('disabled')
    }
    if (ctrl.pageSize < 20) {
      ctrl.pagesizeTarget.classList.add('disabled')
    } else {
      ctrl.pagesizeTarget.classList.remove('disabled')
    }
    var suffix = rowMax > 1 ? 's' : ''
    var rangeEnd = params.offset + count
    if (rangeEnd > rowMax) rangeEnd = rowMax
    ctrl.rangeTarget.innerHTML = (params.offset + 1) + ' &mdash; ' + rangeEnd + ' of ' +
      rowMax + ' transaction' + suffix
  }

  drawGraph () {
    var ctrl = this
    var settings = ctrl.chartSettings

    $('#no-bal').addClass('d-hide')
    $('#history-chart').removeClass('d-hide')
    $('#toggle-charts').addClass('d-hide')

    if (ctrl.unspent === '0' && settings.view === 'unspent') {
      $('#no-bal').removeClass('d-hide')
      $('#history-chart').addClass('d-hide')
      $('body').removeClass('loading')
      return
    }

    // If the view parameters aren't valid, go to default view.
    if (!ctrl.validGraphView() || !ctrl.validGraphInterval()) return ctrl.showList()

    if (settings.view === ctrl.currentChartSettings.view && settings.bin === ctrl.currentChartSettings.bin) {
      // Only the zoom has changed.
      var zoom = ctrl.decodeZoom(settings.zoom)
      if (zoom) {
        ctrl.setZoom(zoom.start.getTime(), zoom.end.getTime())
      }
      return
    }

    // Set the current view to prevent uneccesary reloads.
    TurboQuery.project(ctrl.currentChartSettings, ctrl.viewSettings)

    $('body').addClass('loading')

    // Check for cached data
    var queue = ctrl.retrievedData
    if (queue[settings.view] && queue[settings.view][settings.bin]) {
      // Queue the function to allow the loading animation to start.
      setTimeout(function () {
        ctrl.popChartQueue(settings.view, settings.bin)
        $('body').removeClass('loading')
      }, 10) // 0 should work but doesn't always
      return
    }
    $.ajax({
      type: 'GET',
      url: '/api/address/' + ctrl.dcrAddress + '/' + settings.view + '/' + settings.bin,
      complete: function () { $('body').removeClass('loading') },
      success: function (data) {
        ctrl.processData(settings.view, settings.bin, data)
      }
    })
  }

  processData (chart, bin, data) {
    var ctrl = this
    if (!ctrl.retrievedData[chart]) {
      ctrl.retrievedData[chart] = {}
    }
    if (!isEmpty(data)) {
      var processor = null
      switch (chart) {
        case 'types':
          processor = txTypesFunc
          break

        case 'amountflow':
          processor = amountFlowFunc
          break

        case 'unspent':
          processor = unspentAmountFunc
          break
      }
      if (!processor) {
        return
      }
      ctrl.retrievedData[chart][bin] = processor(data)
      setTimeout(function () {
        ctrl.popChartQueue(chart, bin)
      }, 0)
    } else {
      $('#no-bal').removeClass('d-hide')
      $('#history-chart').addClass('d-hide')
      $('#toggle-charts').removeClass('d-hide')
    }
  }

  popChartQueue (chart, bin) {
    var ctrl = this
    if (!ctrl.retrievedData[chart] || !ctrl.retrievedData[chart][bin]) {
      return
    }
    var data = ctrl.retrievedData[chart][bin]
    var options = null
    switch (chart) {
      case 'types':
        options = ctrl.typesGraphOptions
        break

      case 'amountflow':
        options = ctrl.amountFlowGraphOptions
        $('#toggle-charts').removeClass('d-hide')
        break

      case 'unspent':
        options = ctrl.unspentGraphOptions
        break
    }
    options.zoomCallback = ctrl.zoomCallback
    options.drawCallback = ctrl.drawCallback
    if (ctrl.graph === undefined) {
      ctrl.graph = plotGraph(data, options)
    } else {
      ctrl.graph.updateOptions({
        ...{ 'file': data },
        ...options })
    }
    ctrl.xVal = ctrl.graph.xAxisExtremes()
    ctrl.setVisibleButtons()
    var zoom = ctrl.decodeZoom(ctrl.viewSettings.zoom)
    if (zoom) ctrl.setZoom(zoom.start.getTime(), zoom.end.getTime())
  }

  _updateView () {
    var ctrl = this
    if (ctrl.query.count === 0 || ctrl.viewSettings.view === 'list') {
      ctrl.showList()
      return
    }
    ctrl.showGraph()
    ctrl.drawGraph()
  }

  showList () {
    var ctrl = this
    ctrl.viewSettings.view = 'list'
    ctrl.query.replace(TurboQuery.project(ctrl.listSettings, ctrl.viewSettings))
    ctrl.chartElements.addClass('d-hide')
    ctrl.listElements.removeClass('d-hide')
  }

  showGraph () {
    var ctrl = this
    var settings = ctrl.viewSettings
    settings.bin = ctrl.getBin()
    settings.flow = settings.view === 'amountflow' ? ctrl.flow : null
    ctrl.query.replace(TurboQuery.project(ctrl.chartSettings, settings))
    ctrl.chartElements.removeClass('d-hide')
    ctrl.listElements.addClass('d-hide')
  }

  validGraphView (view) {
    view = view || this.chartSettings.view
    return this.optionsTarget.namedItem(view) || false
  }

  validGraphInterval (interval) {
    interval = interval || this.chartSettings.bin || this.activeIntervalButton
    return this.binputs.filter("[name='" + interval + "']") || false
  }

  changeView (e) {
    var ctrl = this
    $('.addr-btn').removeClass('btn-active')
    $(e ? e.srcElement : '.chart').addClass('btn-active')
    var view = ctrl.activeViewButton
    if (view !== 'list') {
      ctrl.viewSettings.view = ctrl.chartType
      ctrl.setGraphQuery() // Triggers chart draw
      this.updateView()
    } else {
      ctrl.showList()
    }
  }

  changeGraph (e) {
    this.viewSettings.view = this.chartType
    this.setGraphQuery()
    this.updateView()
  }

  changeBin (e) {
    var ctrl = this
    ctrl.viewSettings.bin = e.target.name
    ctrl.setIntervalButton(e.target.name)
    this.setGraphQuery()
    this.updateView()
  }

  setGraphQuery () {
    this.query.replace(TurboQuery.project(this.chartSettings, this.viewSettings))
  }

  updateFlow () {
    var ctrl = this
    var bitmap = ctrl.flow
    if (bitmap === 0) {
      // If all boxes are unchecked, just leave the last view
      // in place to prevent chart errors with zero visible datasets
      return
    }
    ctrl.viewSettings.flow = bitmap
    ctrl.setGraphQuery()
    // Set the graph dataset visibility based on the bitmap
    // Dygraph dataset indices: 0 received, 1 sent, 2 & 3 net
    var visibility = {}
    visibility[0] = bitmap & 1
    visibility[1] = bitmap & 2
    visibility[2] = visibility[3] = bitmap & 4
    Object.keys(visibility).forEach(function (idx) {
      ctrl.graph.setVisibility(idx, visibility[idx])
    })
  }

  setFlowChecks () {
    var bitmap = this.viewSettings.flow
    this.flowBoxes.forEach(function (box) {
      box.checked = bitmap & parseInt(box.value)
    })
  }

  onZoom (e) {
    var ctrl = this
    ctrl.zoomButtons.removeClass('btn-active')
    $(e.srcElement).addClass('btn-active')
    if (ctrl.graph === undefined) {
      return
    }
    var duration = ctrl.zoom

    var end = ctrl.xVal[1]
    var start = duration === 0 ? ctrl.xVal[0] : end - duration
    ctrl.setZoom(start, end)
  }

  setZoom (start, end) {
    var ctrl = this
    $('body').addClass('loading')
    ctrl.graph.updateOptions({
      dateWindow: [start, end]
    })
    ctrl.viewSettings.zoom = ctrl.encodeZoomStamps(start, end)
    ctrl.query.replace(TurboQuery.project(ctrl.chartSettings, ctrl.viewSettings))
    $('body').removeClass('loading')
  }

  encodeZoomDates (start, end) {
    return this.encodeZoomStamps(start.getTime(), end.getTime())
  }

  encodeZoomStamps (start, end) {
    return parseInt(start / 1000).toString(36) + '-' + parseInt(end / 1000).toString(36)
  }

  getBin () {
    var ctrl = this
    var bin = ctrl.query.get('bin')
    if (!ctrl.setIntervalButton(bin)) {
      bin = ctrl.activeIntervalButton
    }
    return bin
  }

  setIntervalButton (interval) {
    var ctrl = this
    var button = ctrl.validGraphInterval(interval)
    if (!button) return false
    ctrl.binputs.removeClass('btn-active')
    button.addClass('btn-active')
  }

  setViewButton (view) {
    var viewForm = $(this.btnsTarget)
    viewForm.children('input').removeClass('btn-active')
    viewForm.children("input[name='" + view + "']").addClass('btn-active')
  }

  setChartType () {
    var view = this.viewSettings.view
    if (this.validGraphView(view)) {
      this.optionsTarget.value = view
    }
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

  _drawCallback (graph, first) {
    var ctrl = this
    if (first) return
    var start, end
    [start, end] = ctrl.graph.xAxisRange()
    ctrl.viewSettings.zoom = ctrl.encodeZoomStamps(start, end)
    ctrl.query.replace(TurboQuery.project(ctrl.chartSettings, ctrl.viewSettings))
  }

  _zoomCallback (start, end) {
    var ctrl = this
    ctrl.zoomButtons.removeClass('btn-active')
    ctrl.viewSettings.zoom = ctrl.encodeZoomStamps(start, end)
    ctrl.query.replace(TurboQuery.project(ctrl.chartSettings, ctrl.viewSettings))
  }

  setVisibleButtons () {
    var ctrl = this
    var duration = ctrl.xVal[1] - ctrl.xVal[0]
    var buttonSets = [ctrl.zoomButtons, ctrl.binputs]
    buttonSets.forEach(function (buttonSet) {
      buttonSet.each(function (i, button) {
        if (duration > ctrl.zoomMap[button.name]) {
          button.classList.remove('d-hide')
        } else {
          button.classList.add('d-hide')
        }
      })
    })
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
    var base10 = 0
    this.flowBoxes.forEach(function (box) {
      if (box.checked) base10 += parseInt(box.value)
    })
    return base10
  }

  get txnType () {
    return this.txntypeTarget.selectedOptions[0].value
  }

  get pageSize () {
    return parseInt(this.pagesizeTarget.selectedOptions[0].value)
  }
}
