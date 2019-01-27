import { Controller } from 'stimulus'
import { each } from 'lodash-es'
import dompurify from 'dompurify'
import humanize from '../helpers/humanize_helper'
import ws from '../services/messagesocket_service'
import { keyNav } from '../services/keyboard_navigation_service'
import globalEventBus from '../services/event_bus_service'
import { fadeIn } from '../helpers/animation_helper'
import Mempool from '../helpers/mempool_helper'

function incrementValue (element) {
  if (element) {
    element.textContent = parseInt(element.textContent) + 1
  }
}

function voteSpans (tallys) {
  var spans = []
  var joiner
  for (let hash in tallys) {
    if (joiner) spans.push(joiner)
    let count = tallys[hash]
    let span = document.createElement('span')
    span.dataset.tooltip = `For block ${hash}`
    span.className = 'position-relative d-inline-block'
    span.textContent = count.affirm + count.reject
    spans.push(span)
    joiner = document.createElement('span')
    joiner.textContent = ' + '
  }
  return spans
}

function mempoolTableRow (tx) {
  var tbody = document.createElement('tbody')
  var link = `/tx/${tx.hash}`
  tbody.innerHTML = `<tr>
    <td class="text-left pl-1">
      <div class="hash-box">
        <div class="hash-fill">${humanize.hashElide(tx.hash, link)}</div>
      </div>
    </td>
    <td>${tx.Type}</td>
    <td>${humanize.threeSigFigs(tx.total || 0, false, 8)}</td>
    <td class="d-none d-sm-table-cell d-md-none d-lg-table-cell">${tx.size} B</td>
    <td class="text-right pr-3 home-bl-age" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
  </tr>`
  dompurify.sanitize(tbody, { IN_PLACE: true })
  return tbody.firstChild
}

export default class extends Controller {
  static get targets () {
    return ['transactions', 'difficulty',
      'bsubsidyTotal', 'bsubsidyPow', 'bsubsidyPos', 'bsubsidyDev',
      'coinSupply', 'blocksdiff', 'devFund', 'windowIndex', 'posBar',
      'rewardIdx', 'powBar', 'poolSize', 'poolValue', 'ticketReward',
<<<<<<< df338e482277d26437e4ac6fcc5c82911678ec8a
      'targetPct', 'poolSizePct', 'hashrate', 'hashrateDelta',
      'nextExpectedSdiff', 'nextExpectedMin', 'nextExpectedMax'
=======
      'targetPct', 'poolSizePct', 'mempool', 'mpRegTotal', 'mpRegCount',
      'mpTicketTotal', 'mpTicketCount', 'mpVoteTotal', 'mpVoteCount',
      'mpRevTotal', 'mpRevCount', 'mpRegBar', 'mpVoteBar', 'mpTicketBar',
      'mpRevBar', 'voteTally', 'blockVotes', 'blockHeight', 'blockSize',
      'blockTotal'
>>>>>>> add live data summaries to the homepage
    ]
  }

  connect () {
    this.ticketsPerBlock = parseInt(this.mpVoteCountTarget.dataset.ticketsPerBlock)
    this.mempool = new Mempool(this.mempoolTarget.dataset, this.voteTallyTargets)
    this.setBars(this.mempool.totals())
    ws.registerEvtHandler('newtx', (evt) => {
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
    ws.deregisterEvtHandlers('newtx')
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
    voteSpans(counts.vote).forEach((span) => { ct.appendChild(span) })

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
    var votes = this.mempool.votes(hash)
    this.blockVotesTarget.querySelectorAll('span').forEach((span, i) => {
      if (i < votes.affirm) {
        span.className = 'd-inline-block dcricon-affirm'
      } else if (i < votes.affirm + votes.reject) {
        span.className = 'd-inline-block dcricon-reject'
      } else {
        span.className = 'd-inline-block dcricon-missing'
      }
    })
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
    this.targetPctTarget.textContent = parseFloat(ex.pool_info.percent_target).toFixed(2)
    this.rewardIdxTarget.textContent = ex.reward_idx
    this.powBarTarget.style.width = `${(ex.reward_idx / ex.params.reward_window_size) * 100}%`
    this.poolValueTarget.innerHTML = humanize.decimalParts(ex.pool_info.value, true, 0)
    this.ticketRewardTarget.innerHTML = humanize.fmtPercentage(ex.reward)
    this.poolSizePctTarget.textContent = parseFloat(ex.pool_info.percent).toFixed(2)
    if (this.hasDevFundTarget) this.devFundTarget.innerHTML = humanize.decimalParts(ex.dev_fund / 100000000, true, 0)
    this.hashrateTarget.innerHTML = humanize.decimalParts(ex.hash_rate, false, 8, 2)
    this.hashrateDeltaTarget.innerHTML = humanize.fmtPercentage(ex.hash_rate_change)
    this.blockVotesTarget.dataset.hash = blockData.block.hash
    this.setVotes()
    let block = blockData.block
    this.blockHeightTarget.textContent = block.height
    this.blockHeightTarget.href = `/block/${block.hash}`
    this.blockSizeTarget.textContent = humanize.bytes(block.size)
    this.blockTotalTarget.textContent = humanize.threeSigFigs(block.total)
  }
}
