import { Controller } from 'stimulus'
import { isEmpty } from 'lodash-es'
import dompurify from 'dompurify'
import { getDefault } from '../helpers/module_helper'
import { padPoints, sizedBarPlotter } from '../helpers/chart_helper'
import Zoom from '../helpers/zoom_helper'
import globalEventBus from '../services/event_bus_service'
import TurboQuery from '../helpers/turbolinks_helper'
import axios from 'axios'
import humanize from '../helpers/humanize_helper'
import txInBlock from '../helpers/block_helper'
import { fadeIn, animationFrame } from '../helpers/animation_helper'

const blockDuration = 5 * 60000
const maxAddrRows = 160
let Dygraph // lazy loaded on connect

function txTypesFunc (d, binSize) {
  const p = []

  d.time.map((n, i) => {
    p.push([new Date(n), d.sentRtx[i], d.receivedRtx[i], d.tickets[i], d.votes[i], d.revokeTx[i]])
  })

  padPoints(p, binSize)

  return p
}

function amountFlowProcessor (d, binSize) {
  const flowData = []
  const balanceData = []
  let balance = 0

  d.time.map((n, i) => {
    const v = d.net[i]
    let netReceived = 0
    let netSent = 0

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
  let xHTML = ''
  if (data.xHTML !== undefined) {
    xHTML = humanize.date(data.x, false, true)
  }
  let html = this.getLabels()[0] + ': ' + xHTML
  data.series.map((series) => {
    if (series.color === undefined) return ''
    // Skip display of zeros
    if (series.y === 0) return ''
    const l = '<span style="color: ' + series.color + ';"> ' + series.labelHTML
    html = '<span style="color:#2d2d2d;">' + html + '</span>'
    html += '<br>' + series.dashHTML + l + ': ' + (isNaN(series.y) ? '' : series.y) + '</span>'
  })
  return html
}

function customizedFormatter (data) {
  let xHTML = ''
  if (data.xHTML !== undefined) {
    xHTML = humanize.date(data.x, false, true)
  }
  let html = this.getLabels()[0] + ': ' + xHTML
  data.series.map((series) => {
    if (series.color === undefined) return ''
    // Skip display of zeros
    if (series.y === 0) return ''
    const l = '<span style="color: ' + series.color + ';"> ' + series.labelHTML
    html = '<span style="color:#2d2d2d;">' + html + '</span>'
    html += '<br>' + series.dashHTML + l + ': ' + (isNaN(series.y) ? '' : series.y + ' DCR') + '</span> '
  })
  return html
}

function setTxnCountText (el, count) {
  if (el.dataset.formatted) {
    el.textContent = count + ' transaction' + (count > 1 ? 's' : '')
  } else {
    el.textContent = count
  }
}

let commonOptions, typesGraphOptions, amountFlowGraphOptions, balanceGraphOptions
// Cannot set these until DyGraph is fetched.
function createOptions () {
  commonOptions = {
    digitsAfterDecimal: 8,
    showRangeSelector: true,
    rangeSelectorHeight: 20,
    rangeSelectorForegroundStrokeColor: '#999',
    rangeSelectorBackgroundStrokeColor: '#777',
    legend: 'follow',
    fillAlpha: 0.9,
    labelsKMB: true,
    labelsUTC: true,
    stepPlot: false,
    rangeSelectorPlotFillColor: 'rgba(128, 128, 128, 0.3)',
    rangeSelectorPlotFillGradientColor: 'transparent',
    rangeSelectorPlotStrokeColor: 'rgba(128, 128, 128, 0.7)',
    rangeSelectorPlotLineWidth: 2
  }

  typesGraphOptions = {
    labels: ['Date', 'Sending (regular)', 'Receiving (regular)', 'Tickets', 'Votes', 'Revocations'],
    colors: ['#69D3F5', '#2971FF', '#41BF53', 'darkorange', '#FF0090'],
    ylabel: 'Tx Count',
    visibility: [true, true, true, true, true],
    legendFormatter: formatter,
    stackedGraph: true,
    fillGraph: false
  }

  amountFlowGraphOptions = {
    labels: ['Date', 'Received', 'Spent', 'Net Received', 'Net Spent'],
    colors: ['#2971FF', '#2ED6A1', '#41BF53', '#FF0090'],
    ylabel: 'Total (DCR)',
    visibility: [true, false, false, false],
    legendFormatter: customizedFormatter,
    stackedGraph: true,
    fillGraph: false
  }

  balanceGraphOptions = {
    labels: ['Date', 'Balance'],
    colors: ['#41BF53'],
    ylabel: 'Balance (DCR)',
    plotter: [Dygraph.Plotters.linePlotter, Dygraph.Plotters.fillPlotter],
    legendFormatter: customizedFormatter,
    stackedGraph: false,
    visibility: [true],
    fillGraph: true,
    stepPlot: true
  }
}

let ctrl = null

export default class extends Controller {
  static get targets () {
    return ['options', 'addr', 'balance',
      'flow', 'zoom', 'interval', 'numUnconfirmed',
      'pagesize', 'txntype', 'txnCount', 'qricon', 'qrimg', 'qrbox',
      'paginator', 'pageplus', 'pageminus', 'listbox', 'table',
      'range', 'chartbox', 'noconfirms', 'chart', 'pagebuttons',
      'pending', 'hash', 'matchhash', 'view', 'mergedMsg',
      'chartLoader', 'listLoader', 'expando', 'littlechart', 'bigchart',
      'fullscreen', 'tablePagination']
  }

  async connect () {
    ctrl = this
    ctrl.retrievedData = {}
    ctrl.ajaxing = false
    ctrl.qrCode = false
    ctrl.requestedChart = false
    // Bind functions that are passed as callbacks
    ctrl.zoomCallback = ctrl._zoomCallback.bind(ctrl)
    ctrl.drawCallback = ctrl._drawCallback.bind(ctrl)
    ctrl.lastEnd = 0
    ctrl.confirmMempoolTxs = ctrl._confirmMempoolTxs.bind(ctrl)
    ctrl.bindElements()
    ctrl.bindEvents()
    ctrl.query = new TurboQuery()

    // These two are templates for query parameter sets.
    // When url query parameters are set, these will also be updated.
    const settings = ctrl.settings = TurboQuery.nullTemplate(['chart', 'zoom', 'bin', 'flow',
      'n', 'start', 'txntype'])

    ctrl.state = Object.assign({}, settings)

    // Get initial view settings from the url
    ctrl.query.update(settings)
    ctrl.setChartType()
    if (settings.flow) ctrl.setFlowChecks()
    if (settings.zoom !== null) {
      ctrl.zoomButtons.forEach((button) => {
        button.classList.remove('btn-selected')
      })
    }
    if (settings.bin == null) {
      settings.bin = ctrl.getBin()
    }
    if (settings.chart == null || !ctrl.validChartType(settings.chart)) {
      settings.chart = ctrl.chartType
    }

    // Parse stimulus data
    const cdata = ctrl.data
    ctrl.dcrAddress = cdata.get('dcraddress')
    ctrl.paginationParams = {
      offset: parseInt(cdata.get('offset')),
      count: parseInt(cdata.get('txnCount'))
    }
    ctrl.balance = cdata.get('balance')

    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )
    ctrl.initializeChart()
    ctrl.drawGraph()
  }

  disconnect () {
    if (this.graph !== undefined) {
      this.graph.destroy()
    }
    globalEventBus.off('BLOCK_RECEIVED', this.confirmMempoolTxs)
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
    this.flowBoxes = this.flowTarget.querySelectorAll('input')
    // pagesizeTarget is not available for dummy addresses
    this.pageSizeOptions = this.hasPagesizeTarget ? this.pagesizeTarget.querySelectorAll('option') : []
    this.zoomButtons = this.zoomTarget.querySelectorAll('button')
    this.binputs = this.intervalTarget.querySelectorAll('button')
  }

  bindEvents () {
    globalEventBus.on('BLOCK_RECEIVED', this.confirmMempoolTxs)
    ctrl.paginatorTargets.forEach((link) => {
      link.addEventListener('click', (e) => {
        e.preventDefault()
      })
    })
  }

  async showQRCode () {
    this.qrboxTarget.classList.remove('d-hide')
    if (this.qrCode) {
      await fadeIn(this.qrimgTarget)
    } else {
      const QRCode = await getDefault(
        import(/* webpackChunkName: "qrcode" */ 'qrcode')
      )
      const qrCodeImg = await QRCode.toDataURL(this.dcrAddress,
        {
          errorCorrectionLevel: 'H',
          scale: 6,
          margin: 0
        }
      )
      this.qrimgTarget.innerHTML = `<img src="${qrCodeImg}"/>`
      await fadeIn(this.qrimgTarget)
      if (this.graph) this.graph.resize()
    }
    this.qriconTarget.classList.add('d-hide')
  }

  async hideQRCode () {
    this.qriconTarget.classList.remove('d-hide')
    this.qrboxTarget.classList.add('d-hide')
    this.qrimgTarget.style.opacity = 0
    await animationFrame()
    if (this.graph) this.graph.resize()
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

  pageNumberLink (e) {
    e.preventDefault()
    const url = e.target.href
    const parser = new URL(url)
    const start = parser.searchParams.get('start')
    const pagesize = parser.searchParams.get('n')
    const txntype = parser.searchParams.get('txntype')
    this.fetchTable(txntype, pagesize, start)
  }

  toPage (direction) {
    const params = ctrl.paginationParams
    const count = ctrl.pageSize
    const txType = ctrl.txnType
    let requestedOffset = params.offset + count * direction
    if (requestedOffset >= params.count) return
    if (requestedOffset < 0) requestedOffset = 0
    ctrl.fetchTable(txType, count, requestedOffset)
  }

  async fetchTable (txType, count, offset) {
    ctrl.listLoaderTarget.classList.add('loading')
    const requestCount = count > 20 ? count : 20
    const tableResponse = await axios.get(ctrl.makeTableUrl(txType, requestCount, offset))
    const data = tableResponse.data
    ctrl.tableTarget.innerHTML = dompurify.sanitize(data.html)
    const settings = ctrl.settings
    settings.n = count
    settings.start = offset
    settings.txntype = txType
    ctrl.paginationParams.count = data.tx_count
    ctrl.query.replace(settings)
    ctrl.paginationParams.offset = offset
    ctrl.paginationParams.pagesize = count
    ctrl.paginationParams.txntype = txType
    ctrl.setPageability()
    if (txType.indexOf('merged') === -1) {
      this.mergedMsgTarget.classList.add('d-hide')
    } else {
      this.mergedMsgTarget.classList.remove('d-hide')
    }
    ctrl.tablePaginationParams = data.pages
    ctrl.setTablePaginationLinks()
    ctrl.listLoaderTarget.classList.remove('loading')
  }

  setPageability () {
    const params = ctrl.paginationParams
    const rowMax = params.count
    const count = ctrl.pageSize
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
          option.text = option.value = Math.min(rowMax, maxAddrRows)
        } else {
          option.disabled = true
          option.text = option.value = maxAddrRows
        }
      } else {
        option.disabled = rowMax <= option.value
      }
    })
    setAbility(ctrl.pagesizeTarget, rowMax > 20)
    const suffix = rowMax > 1 ? 's' : ''
    let rangeEnd = params.offset + count
    if (rangeEnd > rowMax) rangeEnd = rowMax
    ctrl.rangeTarget.innerHTML = 'showing ' + (params.offset + 1) + ' &ndash; ' +
    rangeEnd + ' of ' + rowMax.toLocaleString() + ' transaction' + suffix
  }

  createGraph (processedData, otherOptions) {
    return new Dygraph(
      this.chartTarget,
      processedData,
      { ...commonOptions, ...otherOptions }
    )
  }

  setTablePaginationLinks () {
    const tablePagesLink = ctrl.tablePaginationParams
    const txCount = parseInt(ctrl.paginationParams.count)
    const offset = parseInt(ctrl.paginationParams.offset)
    const pageSize = parseInt(ctrl.paginationParams.pagesize)
    const txnType = ctrl.paginationParams.txntype
    let links = ''

    if (typeof offset !== 'undefined' && offset > 0) {
      links = `<a href="/address/${this.dcrAddress}?start=${offset - pageSize}&n=${pageSize}&txntype=${txnType}" ` +
      'class="d-inline-block dcricon-arrow-left m-1 fz20" data-action="click->address#pageNumberLink"></a>' + '\n'
    }

    links += tablePagesLink.map(d => {
      if (!d.link) return `<span>${d.str}</span>`
      return `<a href="${d.link}" class="fs18 pager px-1${d.active ? ' active' : ''}" data-action="click->address#pageNumberLink">${d.str}</a>`
    }).join('\n')

    if ((txCount - offset) > pageSize) {
      links += '\n' + `<a href="/address/${this.dcrAddress}?start=${(offset + pageSize)}&n=${pageSize}&txntype=${txnType}" ` +
      'class="d-inline-block dcricon-arrow-right m-1 fs20" data-action="click->address#pageNumberLink"></a>'
    }

    ctrl.tablePaginationTarget.innerHTML = dompurify.sanitize(links)
  }

  drawGraph () {
    const settings = ctrl.settings

    ctrl.noconfirmsTarget.classList.add('d-hide')
    ctrl.chartTarget.classList.remove('d-hide')

    // Check for invalid view parameters
    if (!ctrl.validChartType(settings.chart) || !ctrl.validGraphInterval()) return

    if (settings.chart === ctrl.state.chart && settings.bin === ctrl.state.bin) {
      // Only the zoom has changed.
      const zoom = Zoom.decode(settings.zoom)
      if (zoom) {
        ctrl.setZoom(zoom.start, zoom.end)
      }
      return
    }

    // Set the current view to prevent unnecessary reloads.
    Object.assign(ctrl.state, settings)
    ctrl.fetchGraphData(settings.chart, settings.bin)
  }

  async fetchGraphData (chart, bin) {
    const cacheKey = chart + '-' + bin
    if (ctrl.ajaxing === cacheKey) {
      return
    }
    ctrl.requestedChart = cacheKey
    ctrl.ajaxing = cacheKey

    ctrl.chartLoaderTarget.classList.add('loading')

    // Check for cached data
    if (ctrl.retrievedData[cacheKey]) {
      // Queue the function to allow the loader to display.
      setTimeout(() => {
        ctrl.popChartCache(chart, bin)
        ctrl.chartLoaderTarget.classList.remove('loading')
        ctrl.ajaxing = false
      }, 10) // 0 should work but doesn't always
      return
    }

    const chartKey = chart === 'balance' ? 'amountflow' : chart
    const url = '/api/address/' + ctrl.dcrAddress + '/' + chartKey + '/' + bin
    const graphDataResponse = await axios.get(url)
    ctrl.processData(chart, bin, graphDataResponse.data)
    ctrl.ajaxing = false
    ctrl.chartLoaderTarget.classList.remove('loading')
  }

  processData (chart, bin, data) {
    if (isEmpty(data)) {
      ctrl.noDataAvailable()
      return
    }

    const binSize = Zoom.mapValue(bin) || blockDuration
    if (chart === 'types') {
      ctrl.retrievedData['types-' + bin] = txTypesFunc(data, binSize)
    } else if (chart === 'amountflow' || chart === 'balance') {
      const processed = amountFlowProcessor(data, binSize)
      ctrl.retrievedData['amountflow-' + bin] = processed.flow
      ctrl.retrievedData['balance-' + bin] = processed.balance
    } else return
    setTimeout(() => {
      ctrl.popChartCache(chart, bin)
    }, 0)
  }

  popChartCache (chart, bin) {
    const cacheKey = chart + '-' + bin
    const binSize = Zoom.mapValue(bin) || blockDuration
    if (!ctrl.retrievedData[cacheKey] ||
        ctrl.requestedChart !== cacheKey
    ) {
      return
    }
    const data = ctrl.retrievedData[cacheKey]
    let options = null
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
    options.zoomCallback = null
    options.drawCallback = null
    if (ctrl.graph === undefined) {
      ctrl.graph = ctrl.createGraph(data, options)
    } else {
      ctrl.graph.updateOptions({
        ...{ file: data },
        ...options
      })
    }
    if (chart === 'amountflow') {
      ctrl.updateFlow()
    }
    ctrl.chartLoaderTarget.classList.remove('loading')
    ctrl.xRange = ctrl.graph.xAxisExtremes()
    ctrl.validateZoom(binSize)
  }

  noDataAvailable () {
    this.noconfirmsTarget.classList.remove('d-hide')
    this.chartTarget.classList.add('d-hide')
    this.chartLoaderTarget.classList.remove('loading')
  }

  validChartType (chart) {
    return this.optionsTarget.namedItem(chart) || false
  }

  validGraphInterval (interval) {
    const bin = interval || this.settings.bin || this.activeBin
    let b = false
    this.binputs.forEach((button) => {
      if (button.name === bin) b = button
    })
    return b
  }

  validateZoom (binSize) {
    ctrl.setButtonVisibility()
    const zoom = Zoom.validate(ctrl.activeZoomKey || ctrl.settings.zoom, ctrl.xRange, binSize)
    ctrl.setZoom(zoom.start, zoom.end)
    ctrl.graph.updateOptions({
      zoomCallback: ctrl.zoomCallback,
      drawCallback: ctrl.drawCallback
    })
  }

  changeGraph (e) {
    this.settings.chart = this.chartType
    this.setGraphQuery()
    this.drawGraph()
  }

  changeBin (e) {
    const target = e.srcElement || e.target
    if (target.nodeName !== 'BUTTON') return
    ctrl.settings.bin = target.name
    ctrl.setIntervalButton(target.name)
    this.setGraphQuery()
    this.drawGraph()
  }

  setGraphQuery () {
    this.query.replace(this.settings)
  }

  updateFlow () {
    const bitmap = ctrl.flow
    if (bitmap === 0) {
      // If all boxes are unchecked, just leave the last view
      // in place to prevent chart errors with zero visible datasets
      return
    }
    ctrl.settings.flow = bitmap
    ctrl.setGraphQuery()
    // Set the graph dataset visibility based on the bitmap
    // Dygraph dataset indices: 0 received, 1 sent, 2 & 3 net
    const visibility = {}
    visibility[0] = bitmap & 1
    visibility[1] = bitmap & 2
    visibility[2] = visibility[3] = bitmap & 4
    Object.keys(visibility).forEach((idx) => {
      ctrl.graph.setVisibility(idx, visibility[idx])
    })
  }

  setFlowChecks () {
    const bitmap = this.settings.flow
    this.flowBoxes.forEach((box) => {
      box.checked = bitmap & parseInt(box.value)
    })
  }

  onZoom (e) {
    const target = e.srcElement || e.target
    if (target.nodeName !== 'BUTTON') return
    ctrl.zoomButtons.forEach((button) => {
      button.classList.remove('btn-selected')
    })
    target.classList.add('btn-selected')
    if (ctrl.graph === undefined) {
      return
    }
    const duration = ctrl.activeZoomDuration

    const end = ctrl.xRange[1]
    const start = duration === 0 ? ctrl.xRange[0] : end - duration
    ctrl.setZoom(start, end)
  }

  setZoom (start, end) {
    ctrl.chartLoaderTarget.classList.add('loading')
    ctrl.graph.updateOptions({
      dateWindow: [start, end]
    })
    ctrl.settings.zoom = Zoom.encode(start, end)
    ctrl.lastEnd = end
    ctrl.query.replace(ctrl.settings)
    ctrl.chartLoaderTarget.classList.remove('loading')
  }

  getBin () {
    let bin = ctrl.query.get('bin')
    if (!ctrl.setIntervalButton(bin)) {
      bin = ctrl.activeBin
    }
    return bin
  }

  setIntervalButton (interval) {
    const button = ctrl.validGraphInterval(interval)
    if (!button) return false
    ctrl.binputs.forEach((button) => {
      button.classList.remove('btn-selected')
    })
    button.classList.add('btn-selected')
  }

  setViewButton (view) {
    this.viewTargets.forEach((button) => {
      if (button.name === view) {
        button.classList.add('btn-active')
      } else {
        button.classList.remove('btn-active')
      }
    })
  }

  setChartType () {
    const chart = this.settings.chart
    if (this.validChartType(chart)) {
      this.optionsTarget.value = chart
    }
  }

  setSelectedZoom (zoomKey) {
    this.zoomButtons.forEach(function (button) {
      if (button.name === zoomKey) {
        button.classList.add('btn-selected')
      } else {
        button.classList.remove('btn-selected')
      }
    })
  }

  _drawCallback (graph, first) {
    if (first) return
    const [start, end] = ctrl.graph.xAxisRange()
    if (start === end) return
    if (end === this.lastEnd) return // Only handle slide event.
    this.lastEnd = end
    ctrl.settings.zoom = Zoom.encode(start, end)
    ctrl.query.replace(ctrl.settings)
    ctrl.setSelectedZoom(Zoom.mapKey(ctrl.settings.zoom, ctrl.graph.xAxisExtremes()))
  }

  _zoomCallback (start, end) {
    ctrl.zoomButtons.forEach((button) => {
      button.classList.remove('btn-selected')
    })
    ctrl.settings.zoom = Zoom.encode(start, end)
    ctrl.query.replace(ctrl.settings)
    ctrl.setSelectedZoom(Zoom.mapKey(ctrl.settings.zoom, ctrl.graph.xAxisExtremes()))
  }

  setButtonVisibility () {
    const duration = ctrl.chartDuration
    const buttonSets = [ctrl.zoomButtons, ctrl.binputs]
    buttonSets.forEach((buttonSet) => {
      buttonSet.forEach((button) => {
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

  _confirmMempoolTxs (blockData) {
    const block = blockData.block
    if (this.hasPendingTarget) {
      this.pendingTargets.forEach((row) => {
        if (txInBlock(row.dataset.txid, block)) {
          const confirms = row.querySelector('td.addr-tx-confirms')
          confirms.textContent = '1'
          confirms.dataset.confirmationBlockHeight = block.height
          row.querySelector('td.addr-tx-time').textContent = humanize.date(block.time, true)
          const age = row.querySelector('td.addr-tx-age > span')
          age.dataset.age = block.time
          age.textContent = humanize.timeSince(block.unixStamp)
          delete row.dataset.target
          // Increment the displayed tx count
          const count = this.txnCountTarget
          count.dataset.txnCount++
          setTxnCountText(count, count.dataset.txnCount)
          this.numUnconfirmedTargets.forEach((tr, i) => {
            const td = tr.querySelector('td.addr-unconfirmed-count')
            let count = parseInt(tr.dataset.count)
            if (count) count--
            tr.dataset.count = count
            if (count === 0) {
              tr.classList.add('.d-hide')
              delete tr.dataset.target
            } else {
              td.textContent = count
            }
          })
        }
      })
    }
  }

  hashOver (e) {
    const target = e.srcElement || e.target
    const href = target.href
    this.hashTargets.forEach((link) => {
      if (link.href === href) {
        link.classList.add('blue-row')
      } else {
        link.classList.remove('blue-row')
      }
    })
  }

  hashOut (e) {
    const target = e.srcElement || e.target
    const href = target.href
    this.hashTargets.forEach((link) => {
      if (link.href === href) {
        link.classList.remove('blue-row')
      }
    })
  }

  toggleExpand (e) {
    const btn = this.expandoTarget
    if (btn.classList.contains('dcricon-expand')) {
      btn.classList.remove('dcricon-expand')
      btn.classList.add('dcricon-collapse')
      this.bigchartTarget.appendChild(this.chartboxTarget)
      this.fullscreenTarget.classList.remove('d-none')
    } else {
      this.putChartBack()
    }
    if (this.graph) this.graph.resize()
  }

  putChartBack () {
    const btn = this.expandoTarget
    btn.classList.add('dcricon-expand')
    btn.classList.remove('dcricon-collapse')
    this.littlechartTarget.appendChild(this.chartboxTarget)
    this.fullscreenTarget.classList.add('d-none')
    if (this.graph) this.graph.resize()
  }

  exitFullscreen (e) {
    if (e.target !== this.fullscreenTarget) return
    this.putChartBack()
  }

  get chartType () {
    return this.optionsTarget.value
  }

  get activeView () {
    let view = null
    this.viewTargets.forEach((button) => {
      if (button.classList.contains('btn-active')) view = button.name
    })
    return view
  }

  get activeZoomDuration () {
    return this.activeZoomKey ? Zoom.mapValue(this.activeZoomKey) : false
  }

  get activeZoomKey () {
    const activeButtons = this.zoomTarget.getElementsByClassName('btn-selected')
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
    let base10 = 0
    this.flowBoxes.forEach((box) => {
      if (box.checked) base10 += parseInt(box.value)
    })
    return base10
  }

  get txnType () {
    return this.txntypeTarget.selectedOptions[0].value
  }

  get pageSize () {
    const selected = this.pagesizeTarget.selectedOptions
    return selected.length ? parseInt(selected[0].value) : 20
  }
}
