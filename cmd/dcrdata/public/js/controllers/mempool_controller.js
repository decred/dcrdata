import { Controller } from 'stimulus'
import { map, each } from 'lodash-es'
import dompurify from 'dompurify'
import humanize from '../helpers/humanize_helper'
import ws from '../services/messagesocket_service'
import { keyNav } from '../services/keyboard_navigation_service'
import Mempool from '../helpers/mempool_helper'
import { copyIcon, alertArea } from './clipboard_controller'

function incrementValue (el) {
  if (!el) return
  el.textContent = parseInt(el.textContent) + 1
}

function rowNode (rowText) {
  const tbody = document.createElement('tbody')
  tbody.innerHTML = rowText
  dompurify.sanitize(tbody, { IN_PLACE: true, FORBID_TAGS: ['svg', 'math'] })
  return tbody.firstChild
}

function txTableRow (tx) {
  return rowNode(`<tr class="flash">
        <td class="break-word clipboard">
          <a class="hash" href="/tx/${tx.hash}" title="${tx.hash}">${tx.hash}</a>
          ${copyIcon()}
          ${alertArea()}
          </td>
        <td class="mono fs15 text-right">${humanize.decimalParts(tx.total, false, 8)}</td>
        <td class="mono fs15 text-right">${tx.size} B</td>
        <td class="mono fs15 text-right">${tx.fee_rate} DCR/kB</td>
        <td class="mono fs15 text-right" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
    </tr>`)
}

function voteTxTableRow (tx) {
  return rowNode(`<tr class="flash" data-height="${tx.vote_info.block_validation.height}" data-blockhash="${tx.vote_info.block_validation.hash}">
        <td class="break-word clipboard">
          <a class="hash" href="/tx/${tx.hash}">${tx.hash}</a>
          ${copyIcon()}
          ${alertArea()}
        </td>
        <td class="mono fs15"><a href="/block/${tx.vote_info.block_validation.hash}">${tx.vote_info.block_validation.height}<span
          class="small">${tx.vote_info.last_block ? ' best' : ''}</span></a></td>
        <td class="mono fs15"><a href="/tx/${tx.vote_info.ticket_spent}">${tx.vote_info.mempool_ticket_index}<a/></td>
        <td class="mono fs15">${tx.vote_info.vote_version}</td>
        <td class="mono fs15 text-right d-none d-sm-table-cell">${humanize.decimalParts(tx.total, false, 8)}</td>
        <td class="mono fs15 text-right">${humanize.bytes(tx.size)}</td>
        <td class="mono fs15 text-right d-none d-sm-table-cell jsonly" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
    </tr>`)
}

function buildTable (target, txType, txns, rowFn) {
  while (target.firstChild) target.removeChild(target.firstChild)
  if (txns && txns.length > 0) {
    map(txns, rowFn).forEach((tr) => {
      target.appendChild(tr)
    })
  } else {
    target.innerHTML = `<tr class="no-tx-tr"><td colspan="${(txType === 'votes' ? 8 : 4)}">No ${txType} in mempool.</td></tr>`
  }
}

function addTxRow (tx, target, rowFn) {
  if (target.childElementCount === 1 && target.firstChild.classList.contains('no-tx-tr')) {
    target.removeChild(target.firstChild)
  }
  target.insertBefore(rowFn(tx), target.firstChild)
}

export default class extends Controller {
  static get targets () {
    return [
      'bestBlock',
      'bestBlockTime',
      'voteTransactions',
      'ticketTransactions',
      'revocationTransactions',
      'regularTransactions',
      'mempool',
      'voteTally',
      'regTotal',
      'regCount',
      'ticketTotal',
      'ticketCount',
      'voteTotal',
      'voteCount',
      'revTotal',
      'revCount',
      'likelyTotal',
      'mempoolSize'
    ]
  }

  connect () {
    // from txhelpers.DetermineTxTypeString
    const mempoolData = this.mempoolTarget.dataset
    ws.send('getmempooltxs', mempoolData.id)
    this.mempool = new Mempool(mempoolData, this.voteTallyTargets)
    this.txTargetMap = {
      Vote: this.voteTransactionsTarget,
      Ticket: this.ticketTransactionsTarget,
      Revocation: this.revocationTransactionsTarget,
      Regular: this.regularTransactionsTarget
    }
    this.countTargetMap = {
      Vote: this.numVoteTarget,
      Ticket: this.numTicketTarget,
      Revocation: this.numRevocationTarget,
      Regular: this.numRegularTarget
    }
    ws.registerEvtHandler('newtxs', (evt) => {
      const txs = JSON.parse(evt)
      this.mempool.mergeTxs(txs)
      this.renderNewTxns(txs)
      this.setMempoolFigures()
      this.labelVotes()
      this.sortVotesTable()
      keyNav(evt, false, true)
    })
    ws.registerEvtHandler('mempool', (evt) => {
      const m = JSON.parse(evt)
      this.mempool.replace(m)
      this.setMempoolFigures()
      this.updateBlock(m)
      ws.send('getmempooltxs', '')
    })
    ws.registerEvtHandler('getmempooltxsResp', (evt) => {
      const m = JSON.parse(evt)
      this.mempool.replace(m)
      this.handleTxsResp(m)
      this.setMempoolFigures()
      this.labelVotes()
      this.sortVotesTable()
      keyNav(evt, false, true)
    })
  }

  disconnect () {
    ws.deregisterEvtHandlers('newtxs')
    ws.deregisterEvtHandlers('mempool')
    ws.deregisterEvtHandlers('getmempooltxsResp')
  }

  updateBlock (m) {
    this.bestBlockTarget.textContent = m.block_height
    this.bestBlockTarget.dataset.hash = m.block_hash
    this.bestBlockTarget.href = `/block/${m.block_hash}`
    this.bestBlockTimeTarget.dataset.age = m.block_time
  }

  setMempoolFigures () {
    const totals = this.mempool.totals()
    const counts = this.mempool.counts()
    this.regTotalTarget.textContent = humanize.threeSigFigs(totals.regular)
    this.regCountTarget.textContent = counts.regular

    this.ticketTotalTarget.textContent = humanize.threeSigFigs(totals.ticket)
    this.ticketCountTarget.textContent = counts.ticket

    this.voteTotalTarget.textContent = humanize.threeSigFigs(totals.vote)

    const ct = this.voteCountTarget
    while (ct.firstChild) ct.removeChild(ct.firstChild)
    this.mempool.voteSpans(counts.vote).forEach((span) => { ct.appendChild(span) })

    this.revTotalTarget.textContent = humanize.threeSigFigs(totals.rev)
    this.revCountTarget.textContent = counts.rev

    this.likelyTotalTarget.textContent = humanize.threeSigFigs(totals.total)
    this.mempoolSizeTarget.textContent = humanize.bytes(totals.size)

    this.labelVotes()
    // this.setVotes()
  }

  handleTxsResp (m) {
    buildTable(this.regularTransactionsTarget, 'regular transactions', m.tx, txTableRow)
    buildTable(this.revocationTransactionsTarget, 'revocations', m.revs, txTableRow)
    buildTable(this.voteTransactionsTarget, 'votes', m.votes, voteTxTableRow)
    buildTable(this.ticketTransactionsTarget, 'tickets', m.tickets, txTableRow)
  }

  renderNewTxns (txs) {
    each(txs, (tx) => {
      incrementValue(this.countTargetMap[tx.Type])
      const rowFn = tx.Type === 'Vote' ? voteTxTableRow : txTableRow
      addTxRow(tx, this.txTargetMap[tx.Type], rowFn)
    })
  }

  labelVotes () {
    const bestBlockHash = this.bestBlockTarget.dataset.hash
    const bestBlockHeight = parseInt(this.bestBlockTarget.textContent)
    this.voteTransactionsTarget.querySelectorAll('tr').forEach((tr) => {
      const voteValidationHash = tr.dataset.blockhash
      const voteBlockHeight = tr.dataset.height
      const best = tr.querySelector('.small')
      best.textContent = ''
      if (voteBlockHeight > bestBlockHeight) {
        tr.classList.add('blue-row')
        tr.classList.remove('disabled-row')
      } else if (voteValidationHash !== bestBlockHash) {
        tr.classList.add('disabled-row')
        tr.classList.remove('blue-row')
        if (tr.classList.contains('last_block')) {
          tr.textContent = 'False'
        }
      } else {
        tr.classList.remove('disabled-row')
        tr.classList.remove('blue-row')
        best.textContent = ' (best)'
      }
    })
  }

  sortVotesTable () {
    const rows = Array.from(this.voteTransactionsTarget.querySelectorAll('tr'))
    rows.sort(function (a, b) {
      if (a.dataset.height === b.dataset.height) {
        const indexA = parseInt(a.dataset.ticketIndex)
        const indexB = parseInt(b.dataset.ticketIndex)
        return (indexA - indexB)
      } else {
        return (b.dataset.height - a.dataset.height)
      }
    })
    this.voteTransactionsTarget.innerHTML = ''
    rows.forEach((row) => { this.voteTransactionsTarget.appendChild(row) })
  }
}
