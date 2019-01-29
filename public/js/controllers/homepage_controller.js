import { Controller } from 'stimulus'
import { each } from 'lodash-es'
import dompurify from 'dompurify'
import humanize from '../helpers/humanize_helper'
import ws from '../services/messagesocket_service'
import { keyNav } from '../services/keyboard_navigation_service'
import globalEventBus from '../services/event_bus_service'

function incrementValue (element) {
  if (element) {
    element.textContent = parseInt(element.textContent) + 1
  }
}

function txFlexTableRow (tx) {
  return dompurify.sanitize(`<div class="d-flex flex-table-row flash mempool-row">
        <a class="hash truncate-hash" style="flex: 1 1 auto" href="/tx/${tx.hash}" title="${tx.hash}">${tx.hash}</a>
        <span style="flex: 0 0 60px" class="mono text-right ml-1">${tx.Type}</span>
        <span style="flex: 0 0 105px" class="mono text-right ml-1">${humanize.decimalParts(tx.total, false, 8)}</span>
        <span style="flex: 0 0 50px" class="mono text-right ml-1">${tx.size} B</span>
        <span style="flex: 0 0 65px" class="mono text-right ml-1" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</span>
    </div>`)
}

export default class extends Controller {
  static get targets () {
    return ['transactions', 'difficulty',
      'bsubsidyTotal', 'bsubsidyPow', 'bsubsidyPos', 'bsubsidyDev',
      'coinSupply', 'blocksdiff', 'devFund', 'windowIndex', 'posBar',
      'rewardIdx', 'powBar', 'poolSize', 'poolValue', 'ticketReward',
      'targetPct', 'poolSizePct', 'hashrate', 'hashrateDelta',
      'nextExpectedSdiff', 'nextExpectedMin', 'nextExpectedMax'
    ]
  }

  connect () {
    ws.registerEvtHandler('newtx', (evt) => {
      var txs = JSON.parse(evt)
      this.renderLatestTransactions(txs, true)
      keyNav(evt, false, true)
    })
    ws.registerEvtHandler('mempool', (evt) => {
      var m = JSON.parse(evt)
      this.renderLatestTransactions(m.latest, false)
      keyNav(evt, false, true)
      ws.send('getmempooltxs', '')
    })
    ws.registerEvtHandler('getmempooltxsResp', (evt) => {
      var m = JSON.parse(evt)
      this.renderLatestTransactions(m.latest, true)
      keyNav(evt, false, true)
    })
    this.processBlock = this._processBlock.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
  }

  disconnect () {
    ws.deregisterEvtHandlers('newtx')
    ws.deregisterEvtHandlers('mempool')
    ws.deregisterEvtHandlers('getmempooltxsResp')
    globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
  }

  renderLatestTransactions (txs, incremental) {
    each(txs, (tx) => {
      if (incremental) {
        let targetKey = `num${tx.Type}Target`
        incrementValue(this[targetKey])
      }
      let rows = this.transactionsTarget.querySelectorAll('div.mempool-row')
      if (rows.length) {
        let lastRow = rows[rows.length - 1]
        this.transactionsTarget.removeChild(lastRow)
      }
      this.transactionsTarget.insertAdjacentHTML('afterbegin', txFlexTableRow(tx))
    })
  }

  _processBlock (blockData) {
    var ex = blockData.extra
    this.difficultyTarget.innerHTML = humanize.decimalParts(ex.difficulty / 1000000, true, 0)
    this.bsubsidyPowTarget.innerHTML = humanize.decimalParts(ex.subsidy.pow / 100000000, false, 8, 2)
    this.bsubsidyPosTarget.innerHTML = humanize.decimalParts((ex.subsidy.pos / 500000000), false, 8, 2) // 5 votes per block (usually)
    this.bsubsidyDevTarget.innerHTML = humanize.decimalParts(ex.subsidy.dev / 100000000, false, 8, 2)
    this.coinSupplyTarget.innerHTML = humanize.decimalParts(ex.coin_supply / 100000000, true, 0)
    this.blocksdiffTarget.innerHTML = humanize.decimalParts(ex.sdiff, false, 8, 2)
    this.nextExpectedSdiffTarget.innerHTML = humanize.decimalParts(ex.next_expected_sdiff, false, 2, 2)
    this.nextExpectedMinTarget.innerHTML = humanize.decimalParts(ex.next_expected_min, false, 2, 2)
    this.nextExpectedMaxTarget.innerHTML = humanize.decimalParts(ex.next_expected_max, false, 2, 2)
    this.windowIndexTarget.textContent = ex.window_idx
    this.posBarTarget.style.width = `${(ex.window_idx / ex.params.window_size) * 100}%`
    this.poolSizeTarget.innerHTML = humanize.decimalParts(ex.pool_info.size, true, 0)
    this.targetPctTarget.textContent = parseFloat(ex.pool_info.percent_target).toFixed(2)
    this.rewardIdxTarget.textContent = ex.reward_idx
    this.powBarTarget.style.width = `${(ex.reward_idx / ex.params.reward_window_size) * 100}%`
    this.poolValueTarget.innerHTML = humanize.decimalParts(ex.pool_info.value, true, 0)
    this.ticketRewardTarget.innerHTML = humanize.fmtPercentage(ex.reward)
    this.poolSizePctTarget.textContent = parseFloat(ex.pool_info.percent).toFixed(2)
    if (this.hasDevFundTarget) this.devFundTarget.innerHTML = humanize.decimalParts(ex.dev_fund / 100000000, true, 0)
    this.hashrateTarget.innerHTML = humanize.decimalParts(ex.hash_rate, false, 8, 2)
    this.hashrateDeltaTarget.innerHTML = humanize.fmtPercentage(ex.hash_rate_change)
  }
}
