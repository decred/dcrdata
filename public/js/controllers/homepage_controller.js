import { Controller } from 'stimulus'
import humanize from '../helpers/humanize_helper'
import ws from '../services/messagesocket_service'
import globalEventBus from '../services/event_bus_service'
import Mempool from '../helpers/mempool_helper'
import { PieChart } from '../helpers/meters.js'
import { getDefault } from '../helpers/module_helper'
import axios from 'axios'
import { darkEnabled } from '../services/theme_service'

const atomsToDCR = 1e-8
var ticketsPerBlock, ticketWindowSize, targetBlockTime
var Dygraph

var chartOpts = {
  'ticket-price': {
    url: '/api/chart/ticket-price?bin=window&axis=time',
    zip: (d) => {
      let prices = d.price
      return d.t.map((t, i) => {
        return [new Date(t * 1000), prices[i] * atomsToDCR]
      })
    },
    label: 'price'
  },
  'hashrate': {
    url: '/api/chart/hashrate?bin=day&axis=time',
    zip: (d) => {
      let rates = d.rate
      return d.t.map((t, i) => {
        return [new Date(t * 1000), rates[i] * 1e12]
      })
    },
    label: 'hashrate'
  }
}

export default class extends Controller {
  static get targets () {
    return ['difficulty', 'bsubsidyTotal', 'bsubsidyPow', 'bsubsidyPos',
      'bsubsidyDev', 'coinSupply', 'ticketPrice', 'devFund', 'windowIndex',
      'posBar', 'rewardIdx', 'powBar', 'poolSize', 'poolValue', 'targetPct',
      'stakedPct', 'hashrate', 'hashrateDelta', 'nextExpectedSdiff',
      'nextExpectedMin', 'nextExpectedMax', 'mempool', 'mpTicketCount',
      'mpVoteCount', 'mpRegCount', 'mpRevCount', 'voteTally', 'blockVotes',
      'blockHeight', 'blockSize', 'blockTotal', 'consensusMsg', 'powConverted',
      'convertedDev', 'convertedSupply', 'convertedDevSub', 'exchangeRate',
      'convertedStake', 'sizePerTx', 'sizePrefix', 'valuePerTx', 'mpSizePrefix',
      'mempoolSize', 'blockTime', 'diffDelta', 'tpsOverUnder', 'chartLegend',
      'sdiffWindowLeft', 'mppie', 'chart', 'chartZoom'
    ]
  }

  connect () {
    ticketsPerBlock = parseInt(this.mpVoteCountTarget.dataset.ticketsPerBlock)
    ticketWindowSize = parseInt(this.data.get('windowsize'))
    targetBlockTime = parseInt(this.data.get('blocksecs'))
    var mempoolData = this.mempoolTarget.dataset
    ws.send('getmempooltxs', mempoolData.id)
    this.mempool = new Mempool(mempoolData, this.voteTallyTargets)
    var segments = [
      {
        label: 'tx',
        value: parseFloat(mempoolData.regSize),
        color: '#2970FF'
      },
      {
        label: 'tkt',
        value: parseFloat(mempoolData.ticketSize),
        color: '#2ED6A1'
      },
      {
        label: 'vote',
        value: parseFloat(mempoolData.voteSize),
        color: '#c600c0'
      },
      {
        label: 'rev',
        value: parseFloat(mempoolData.revSize),
        color: '#ED6D47'
      }
    ]

    this.mempoolPie = new PieChart(this.mppieTarget, {
      segments: segments,
      valFormatter: segment => {
        let tsf = humanize.threeSFG(segment.value)
        return `${segment.label}: ${tsf.value} ${tsf.prefix}B`
      }
    })

    ws.registerEvtHandler('newtxs', (evt) => {
      var txs = JSON.parse(evt)
      this.mempool.mergeTxs(txs)
      this.setMempoolFigures()
    })
    ws.registerEvtHandler('mempool', (evt) => {
      var m = JSON.parse(evt)
      this.mempool.replace(m)
      this.setMempoolFigures()
      ws.send('getmempooltxs', '')
    })
    ws.registerEvtHandler('getmempooltxsResp', (evt) => {
      var m = JSON.parse(evt)
      this.mempool.mergeMempool(m)
      this.setMempoolFigures()
    })
    this.processNightMode = this._processNightMode.bind(this)
    globalEventBus.on('NIGHT_MODE', this.processNightMode)
    this.processBlock = this._processBlock.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
    window.addEventListener('resize', () => { this.mempoolPie.resize() })
    this.setupPlot()
  }

  disconnect () {
    ws.deregisterEvtHandlers('newtxs')
    ws.deregisterEvtHandlers('mempool')
    ws.deregisterEvtHandlers('getmempooltxsResp')
    globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
    window.removeEventListener('resize', this.resize)
    globalEventBus.off('NIGHT_MODE', this.processNightMode)
  }

  async setupPlot () {
    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )
    var dark = darkEnabled()
    this.graph = new Dygraph(this.chartTarget, [[0, 0], [0, 1]], {
      axisLineWidth: 0.3,
      colors: [dark ? 'white' : 'black'],
      axes: {
        x: {
          axisLineColor: '#aaa'
        },
        y: {
          axisLineColor: '#555',
          axisLabelWidth: 30,
          axisLabelFormatter: humanize.threeSFV
        }
      },
      strokeWidth: 2,
      gridLineColor: dark ? '#555' : '#ccc',
      labelsDiv: this.chartLegendTarget
    })
    this.plot('ticket-price')
  }

  async plot (chart) {
    var opts = chartOpts[chart]
    let chartResponse = await axios.get(opts.url)
    let data = opts.zip(chartResponse.data)
    let dateWindow = this.graph.xAxisRange()
    if (dateWindow[1] === 0) {
      let now = new Date()
      let lastYear = new Date()
      lastYear.setMonth(now.getMonth() - 12)
      dateWindow = [lastYear, now]
    }
    this.graph.updateOptions({
      file: data,
      dateWindow: dateWindow,
      labels: ['date', opts.label]
    })
  }

  setMempoolFigures () {
    var totals = this.mempool.totals()
    var counts = this.mempool.counts()
    // this.mpRegTotalTarget.textContent = humanize.threeSFV(totals.regular)
    this.mpRegCountTarget.textContent = counts.regular
    this.mpTicketCountTarget.textContent = counts.ticket

    var ct = this.mpVoteCountTarget
    while (ct.firstChild) ct.removeChild(ct.firstChild)
    this.mempool.voteSpans(counts.vote).forEach((span) => { ct.appendChild(span) })

    this.mpRevCountTarget.textContent = counts.rev

    this.mempoolTarget.textContent = humanize.threeSFV(totals.total)
    var tsf = humanize.threeSFG(totals.size)
    this.mempoolSizeTarget.textContent = tsf.value
    this.mpSizePrefixTarget.textContent = tsf.prefix
    this.setPie()
    this.setVotes()
  }

  setPie () {
    var v = this.mempool.sizes()
    this.mempoolPie.animate([v.regular, v.ticket, v.vote, v.rev])
  }

  setVotes () {
    var hash = this.blockVotesTarget.dataset.hash
    var votes = this.mempool.blockVoteTally(hash)
    this.blockVotesTarget.querySelectorAll('div').forEach((div, i) => {
      let span = div.firstChild
      if (i < votes.affirm) {
        span.className = 'dcricon-affirm'
        div.dataset.tooltip = 'the stakeholder has voted to accept this block'
      } else if (i < votes.affirm + votes.reject) {
        span.className = 'dcricon-reject'
        div.dataset.tooltip = 'the stakeholder has voted to reject this block'
      } else {
        span.className = 'dcricon-missing'
        div.dataset.tooltip = 'this vote has not been received yet'
      }
    })
    var threshold = ticketsPerBlock / 2
    if (votes.affirm > threshold) {
      this.consensusMsgTarget.textContent = 'approved'
      this.consensusMsgTarget.classList.add('text-green')
      this.consensusMsgTarget.classList.remove('text-danger')
    } else if (votes.reject > threshold) {
      this.consensusMsgTarget.textContent = 'rejected'
      this.consensusMsgTarget.classList.remove('text-green')
      this.consensusMsgTarget.classList.add('text-danger')
    } else {
      this.consensusMsgTarget.classList.remove('text-green')
      this.consensusMsgTarget.classList.remove('text-danger')
      this.consensusMsgTarget.textContent = 'collecting votes'
    }
  }

  _processBlock (blockData) {
    var ex = blockData.extra
    let block = blockData.block
    this.difficultyTarget.textContent = humanize.threeSFV(ex.difficulty)
    this.diffDeltaTarget.textContent = humanize.fmtPercent(ex.pow_diff_change_month)
    this.bsubsidyPowTarget.textContent = humanize.threeSFV(ex.subsidy.pow * atomsToDCR)
    this.bsubsidyPosTarget.textContent = humanize.threeSFV((ex.subsidy.pos * atomsToDCR / ticketsPerBlock)) // 5 votes per block (usually)
    this.bsubsidyDevTarget.textContent = humanize.threeSFV(ex.subsidy.dev * atomsToDCR)
    this.coinSupplyTarget.textContent = humanize.threeSFV(ex.coin_supply * atomsToDCR)
    this.ticketPriceTarget.textContent = humanize.threeSFV(ex.sdiff)
    this.nextExpectedSdiffTarget.innerHTML = ex.next_expected_sdiff.toFixed(2)
    this.nextExpectedMinTarget.innerHTML = ex.next_expected_min.toFixed(1)
    this.nextExpectedMaxTarget.innerHTML = ex.next_expected_max.toFixed(1)
    this.windowIndexTarget.textContent = ex.window_idx
    var fauxSince = (new Date().getTime()) / 1000 - ((ticketWindowSize - block.height % ticketWindowSize) * targetBlockTime)
    this.sdiffWindowLeftTarget.textContent = `${humanize.timeSince(fauxSince)} remaining`
    this.posBarTarget.style.width = `${(ex.window_idx / ex.params.window_size) * 100}%`
    this.poolSizeTarget.innerHTML = humanize.decimalParts(ex.pool_info.size, true, 0)
    var overUnder = ex.pool_info.percent_target - 100
    this.targetPctTarget.textContent = overUnder.toFixed(2)
    this.tpsOverUnderTarget.textContent = overUnder >= 0 ? 'over' : 'under'
    this.rewardIdxTarget.textContent = ex.reward_idx
    this.powBarTarget.style.width = `${(ex.reward_idx / ex.params.reward_window_size) * 100}%`
    this.poolValueTarget.innerHTML = humanize.threeSFV(ex.pool_info.value)
    this.stakedPctTarget.textContent = ex.pool_info.percent.toFixed(2)
    this.devFundTarget.innerHTML = humanize.threeSFV(ex.dev_fund * atomsToDCR)
    this.hashrateTarget.innerHTML = humanize.threeSFV(ex.hash_rate)
    this.hashrateDeltaTarget.innerHTML = humanize.fmtPercent(ex.hash_rate_change_month)
    this.blockVotesTarget.dataset.hash = blockData.block.hash
    this.setVotes()
    this.blockHeightTarget.textContent = block.height
    this.blockHeightTarget.href = `/block/${block.hash}`
    this.blockTimeTarget.dataset.age = block.time
    this.blockTimeTarget.textContent = `${humanize.timeSince(block.time)} ago`
    this.blockSizeTarget.textContent = humanize.bytes(block.size)
    this.blockTotalTarget.textContent = humanize.threeSFV(block.total)

    if (ex.exchange_rate) {
      let xcRate = ex.exchange_rate.value
      let btcIndex = ex.exchange_rate.index
      if (this.hasPowConvertedTarget) {
        this.powConvertedTarget.textContent = `${humanize.twoDecimals(ex.subsidy.pow / 1e8 * xcRate)} ${btcIndex}`
      }
      if (this.hasConvertedDevTarget) {
        this.convertedDevTarget.textContent = `${humanize.threeSFV(ex.dev_fund / 1e8 * xcRate)} ${btcIndex}`
      }
      if (this.hasConvertedSupplyTarget) {
        this.convertedSupplyTarget.textContent = `${humanize.threeSFV(ex.coin_supply / 1e8 * xcRate)} ${btcIndex}`
      }
      if (this.hasConvertedDevSubTarget) {
        this.convertedDevSubTarget.textContent = `${humanize.twoDecimals(ex.subsidy.dev / 1e8 * xcRate)} ${btcIndex}`
      }
      if (this.hasExchangeRateTarget) {
        this.exchangeRateTarget.textContent = humanize.twoDecimals(xcRate)
      }
      if (this.hasConvertedStakeTarget) {
        this.convertedStakeTarget.textContent = `${humanize.twoDecimals(ex.sdiff * xcRate)} ${btcIndex}`
      }
    }
  }

  selectChart (e) {
    var target = e.target || e.srcElement
    this.plot(target.value)
  }

  _processNightMode (data) {
    this.graph.updateOptions({
      colors: [data.nightMode ? 'white' : 'black'],
      gridLineColor: data.darkMode ? '#555' : '#ccc'
    })
  }

  zoomClicked (e) {
    var target = e.target || e.srcElement
    var v = target.value
    this.chartZoomTargets.forEach(btn => {
      if (btn.value === v) btn.dataset.selected = 1
      else btn.removeAttribute('data-selected')
    })
    var now = (new Date()).getTime()
    var then = new Date()
    switch (v) {
      case 'all':
        this.graph.resetZoom()
        return
      case '1y':
        then.setMonth(then.getMonth() - 12)
        break
      case '1mo':
        then.setMonth(then.getMonth() - 1)
    }
    this.graph.updateOptions({ dateWindow: [then, now] })
  }
}
