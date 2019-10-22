import { Controller } from 'stimulus'
import { each } from 'lodash-es'
import dompurify from 'dompurify'
import humanize from '../helpers/humanize_helper'
import ws from '../services/messagesocket_service'
import { keyNav } from '../services/keyboard_navigation_service'
import globalEventBus from '../services/event_bus_service'
import { fadeIn } from '../helpers/animation_helper'
import Mempool from '../helpers/mempool_helper'
import { copyIcon, alertArea } from './clipboard_controller'

function incrementValue (element) {
  if (element) {
    element.textContent = parseInt(element.textContent) + 1
  }
}

function mempoolTableRow (tx) {
  var tbody = document.createElement('tbody')
  var link = `/tx/${tx.hash}`
  tbody.innerHTML = `<tr>
    <td class="text-left pl-1 clipboard">
      ${humanize.hashElide(tx.hash, link)}
      ${copyIcon()}
      ${alertArea()}
    </td>
    <td class="text-left">${tx.Type}</td>
    <td class="text-right">${humanize.threeSigFigs(tx.total || 0, false, 8)}</td>
    <td class="text-nowrap text-right">${tx.size} B</td>
    <td class="text-right pr-1 text-nowrap" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
  </tr>`
  dompurify.sanitize(tbody, { IN_PLACE: true, FORBID_TAGS: ['svg', 'math']} )
  return tbody.firstChild
}

export default class extends Controller {
  static get targets () {
    return ['transactions', 'difficulty',
      'bsubsidyTotal', 'bsubsidyPow', 'bsubsidyPos', 'bsubsidyDev',
      'coinSupply', 'blocksdiff', 'devFund', 'windowIndex', 'posBar',
      'rewardIdx', 'powBar', 'poolSize', 'poolValue', 'ticketReward',
      'targetPct', 'poolSizePct', 'hashrate', 'hashrateDelta',
      'nextExpectedSdiff', 'nextExpectedMin', 'nextExpectedMax', 'mempool',
      'mpRegTotal', 'mpRegCount', 'mpTicketTotal', 'mpTicketCount', 'mpVoteTotal', 'mpVoteCount',
      'mpRevTotal', 'mpRevCount', 'mpRegBar', 'mpVoteBar', 'mpTicketBar',
      'mpRevBar', 'voteTally', 'blockVotes', 'blockHeight', 'blockSize',
      'blockTotal', 'consensusMsg', 'powConverted', 'convertedDev',
      'convertedSupply', 'convertedDevSub', 'exchangeRate', 'convertedStake'
    ]
  }

  connect () {
    this.ticketsPerBlock = parseInt(this.mpVoteCountTarget.dataset.ticketsPerBlock)
    var mempoolData = this.mempoolTarget.dataset
    ws.send('getmempooltxs', mempoolData.id)
    this.mempool = new Mempool(mempoolData, this.voteTallyTargets)
    this.setBars(this.mempool.totals())
    ws.registerEvtHandler('newtxs', (evt) => {
      var txs = JSON.parse(evt)
      this.mempool.mergeTxs(txs)
      this.setMempoolFigures()
      this.renderLatestTransactions(txs, true)
      keyNav(evt, false, true)
    })
    ws.registerEvtHandler('mempool', (evt) => {
      var m = JSON.parse(evt)
      this.renderLatestTransactions(m.latest, false)
      this.mempool.replace(m)
      this.setMempoolFigures()
      keyNav(evt, false, true)
      ws.send('getmempooltxs', '')
    })
    ws.registerEvtHandler('getmempooltxsResp', (evt) => {
      var m = JSON.parse(evt)
      this.mempool.mergeMempool(m)
      this.setMempoolFigures()
      this.renderLatestTransactions(m.latest, true)
      keyNav(evt, false, true)
    })
    this.processBlock = this._processBlock.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
  }

  disconnect () {
    ws.deregisterEvtHandlers('newtxs')
    ws.deregisterEvtHandlers('mempool')
    ws.deregisterEvtHandlers('getmempooltxsResp')
    globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
  }

  setMempoolFigures () {
    var totals = this.mempool.totals()
    var counts = this.mempool.counts()
    this.mpRegTotalTarget.textContent = humanize.threeSigFigs(totals.regular)
    this.mpRegCountTarget.textContent = counts.regular

    this.mpTicketTotalTarget.textContent = humanize.threeSigFigs(totals.ticket)
    this.mpTicketCountTarget.textContent = counts.ticket

    this.mpVoteTotalTarget.textContent = humanize.threeSigFigs(totals.vote)

    var ct = this.mpVoteCountTarget
    while (ct.firstChild) ct.removeChild(ct.firstChild)
    this.mempool.voteSpans(counts.vote).forEach((span) => { ct.appendChild(span) })

    this.mpRevTotalTarget.textContent = humanize.threeSigFigs(totals.rev)
    this.mpRevCountTarget.textContent = counts.rev

    this.mempoolTarget.textContent = humanize.threeSigFigs(totals.total)
    this.setBars(totals)
    this.setVotes()
  }

  setBars (totals) {
    this.mpRegBarTarget.style.width = `${totals.regular / totals.total * 100}%`
    this.mpVoteBarTarget.style.width = `${totals.vote / totals.total * 100}%`
    this.mpTicketBarTarget.style.width = `${totals.ticket / totals.total * 100}%`
    this.mpRevBarTarget.style.width = `${totals.rev / totals.total * 100}%`
  }

  setVotes () {
    var hash = this.blockVotesTarget.dataset.hash
    var votes = this.mempool.blockVoteTally(hash)
    this.blockVotesTarget.querySelectorAll('div').forEach((div, i) => {
      let span = div.firstChild
      if (i < votes.affirm) {
        span.className = 'd-inline-block dcricon-affirm'
        div.dataset.tooltip = 'the stakeholder has voted to accept this block'
      } else if (i < votes.affirm + votes.reject) {
        span.className = 'd-inline-block dcricon-reject'
        div.dataset.tooltip = 'the stakeholder has voted to reject this block'
      } else {
        span.className = 'd-inline-block dcricon-missing'
        div.dataset.tooltip = 'this vote has not been received yet'
      }
    })
    var threshold = this.ticketsPerBlock / 2
    if (votes.affirm > threshold) {
      this.consensusMsgTarget.textContent = 'approved'
      this.consensusMsgTarget.className = 'small text-green'
    } else if (votes.reject > threshold) {
      this.consensusMsgTarget.textContent = 'rejected'
      this.consensusMsgTarget.className = 'small text-danger'
    } else {
      this.consensusMsgTarget.textContent = ''
    }
  }

  renderLatestTransactions (txs, incremental) {
    each(txs, (tx) => {
      if (incremental) {
        let targetKey = `num${tx.Type}Target`
        incrementValue(this[targetKey])
      }
      let rows = this.transactionsTarget.querySelectorAll('tr')
      if (rows.length) {
        let lastRow = rows[rows.length - 1]
        this.transactionsTarget.removeChild(lastRow)
      }
      let row = mempoolTableRow(tx)
      row.style.opacity = 0.05
      this.transactionsTarget.insertBefore(row, this.transactionsTarget.firstChild)
      fadeIn(row)
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
    this.targetPctTarget.textContent = parseFloat(ex.pool_info.percent_target - 100).toFixed(2)
    this.rewardIdxTarget.textContent = ex.reward_idx
    this.powBarTarget.style.width = `${(ex.reward_idx / ex.params.reward_window_size) * 100}%`
    this.poolValueTarget.innerHTML = humanize.decimalParts(ex.pool_info.value, true, 0)
    this.ticketRewardTarget.innerHTML = `${ex.reward.toFixed(2)}%`
    this.poolSizePctTarget.textContent = parseFloat(ex.pool_info.percent).toFixed(2)
    if (this.hasDevFundTarget) this.devFundTarget.innerHTML = humanize.decimalParts(ex.dev_fund / 100000000, true, 0)
    this.hashrateTarget.innerHTML = humanize.decimalParts(ex.hash_rate, false, 8, 2)
    this.hashrateDeltaTarget.innerHTML = humanize.fmtPercentage(ex.hash_rate_change_month)
    this.blockVotesTarget.dataset.hash = blockData.block.hash
    this.setVotes()
    let block = blockData.block
    this.blockHeightTarget.textContent = block.height
    this.blockHeightTarget.href = `/block/${block.hash}`
    this.blockSizeTarget.textContent = humanize.bytes(block.size)
    this.blockTotalTarget.textContent = humanize.threeSigFigs(block.total)

    if (ex.exchange_rate) {
      let xcRate = ex.exchange_rate.value
      let btcIndex = ex.exchange_rate.index
      if (this.hasPowConvertedTarget) {
        this.powConvertedTarget.textContent = `${humanize.twoDecimals(ex.subsidy.pow / 1e8 * xcRate)} ${btcIndex}`
      }
      if (this.hasConvertedDevTarget) {
        this.convertedDevTarget.textContent = `${humanize.threeSigFigs(ex.dev_fund / 1e8 * xcRate)} ${btcIndex}`
      }
      if (this.hasConvertedSupplyTarget) {
        this.convertedSupplyTarget.textContent = `${humanize.threeSigFigs(ex.coin_supply / 1e8 * xcRate)} ${btcIndex}`
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
}
