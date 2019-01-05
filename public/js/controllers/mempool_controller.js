import { Controller } from 'stimulus'
import { map, each } from 'lodash-es'
import stripJs from 'strip-js'
import humanize from '../helpers/humanize_helper'
import ws from '../services/messagesocket_service'
import { keyNav } from '../services/keyboard_navigation_service'

function incrementValue (el) {
  if (!el) return
  el.textContent = parseInt(el.textContent) + 1
}

function txTableRow (tx) {
  return stripJs(`<tr class="flash">
        <td class="break-word"><span><a class="hash" href="/tx/${tx.hash}" title="${tx.hash}">${tx.hash}</a></span></td>
        <td class="mono fs15 text-right">${humanize.decimalParts(tx.total, false, 8)}</td>
        <td class="mono fs15 text-right">${tx.size} B</td>
        <td class="mono fs15 text-right" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
    </tr>`)
}

function voteTxTableRow (tx) {
  return stripJs(`<tr class="flash" data-height="${tx.vote_info.block_validation.height}" data-blockhash="${tx.vote_info.block_validation.hash}">
        <td class="break-word"><span><a class="hash" href="/tx/${tx.hash}">${tx.hash}</a></span></td>
        <td class="mono fs15"><span><a href="/block/${tx.vote_info.block_validation.hash}">${tx.vote_info.block_validation.height}</a></span></td>
        <td class="mono fs15 last_block">${tx.vote_info.last_block ? 'True' : 'False'}</td>
        <td class="mono fs15"><a href="/tx/${tx.vote_info.ticket_spent}">${tx.vote_info.mempool_ticket_index}<a/></td>
        <td class="mono fs15">${tx.vote_info.vote_version}</td>
        <td class="mono fs15 text-right">${humanize.decimalParts(tx.total, false, 8)}</td>
        <td class="mono fs15">${tx.size} B</td>
        <td class="mono fs15 text-right" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
    </tr>`)
}

function buildTable (target, txType, txns, rowFn) {
  let tableBody
  if (txns && txns.length > 0) {
    tableBody = map(txns, rowFn).join('')
  } else {
    tableBody = `<tr><td colspan="${(txType === 'votes' ? 8 : 4)}">No ${txType} in mempool.</td></tr>`
  }
  target.innerHTML = tableBody
}

function addTxRow (tx, target, rowFn) {
  target.insertAdjacentHTML('beforebegin', rowFn(tx))
}

export default class extends Controller {
  static get targets () {
    return [
      'bestBlock',
      'bestBlockTime',
      'mempoolSize',
      'numVote',
      'numTicket',
      'numRevoke',
      'numRegular',
      'voteTransactions',
      'ticketTransactions',
      'revokeTransactions',
      'regularTransactions',
      'ticketsVoted',
      'maxVotesPerBlock',
      'totalOut'
    ]
  }

  connect () {
    ws.registerEvtHandler('newtx', (evt) => {
      this.renderNewTxns(evt)
      this.labelVotes()
      this.sortVotesTable()
      keyNav(evt, false, true)
    })
    ws.registerEvtHandler('mempool', (evt) => {
      this.updateMempool(evt)
      ws.send('getmempooltxs', '')
    })
    ws.registerEvtHandler('getmempooltxsResp', (evt) => {
      this.handleTxsResp(evt)
      this.labelVotes()
      this.sortVotesTable()
      keyNav(evt, false, true)
    })
  }

  disconnect () {
    ws.deregisterEvtHandlers('newtx')
    ws.deregisterEvtHandlers('mempool')
    ws.deregisterEvtHandlers('getmempooltxsResp')
  }

  updateMempool (e) {
    var m = JSON.parse(e)
    this.numTicketTarget.textContent = m.num_tickets
    this.numVoteTarget.textContent = m.num_votes
    this.numRegularTarget.textContent = m.num_regular
    this.numRevokeTarget.textContent = m.num_revokes
    this.bestBlockTarget.textContent = m.block_height
    this.bestBlockTarget.dataset.hash = m.block_hash
    this.bestBlockTarget.href = `/block/${m.block_hash}`
    this.bestBlockTimeTarget.dataset.age = m.block_time
    this.mempoolSizeTarget.textContent = m.formatted_size
    this.ticketsVotedTarget.textContent = m.voting_info.tickets_voted
    this.maxVotesPerBlockTarget.textContent = m.voting_info.max_votes_per_block
    this.totalOutTarget.innerHTML = humanize.decimalParts(m.total, false, 8)
    this.mempoolSizeTarget.textContent = m.formatted_size
    this.labelVotes()
  }

  handleTxsResp (event) {
    var m = JSON.parse(event)
    buildTable(this.regularTransactionsTarget, 'regular transactions', m.tx, txTableRow)
    buildTable(this.revokeTransactionsTarget, 'revocations', m.revokes, txTableRow)
    buildTable(this.voteTransactionsTarget, 'votes', m.votes, voteTxTableRow)
    buildTable(this.ticketTransactionsTarget, 'tickets', m.tickets, txTableRow)
  }

  renderNewTxns (evt) {
    var txs = JSON.parse(evt)
    each(txs, (tx) => {
      incrementValue(this['num' + tx.Type + 'Target'])
      var rowFn = tx.Type === 'Vote' ? voteTxTableRow : txTableRow
      addTxRow(tx, this[tx.Type.toLowerCase() + 'TransactionsTarget'], rowFn)
    })
  }

  labelVotes () {
    var bestBlockHash = this.bestBlockTarget.dataset.hash
    var bestBlockHeight = parseInt(this.bestBlockTarget.textContent)
    this.voteTransactionsTarget.querySelectorAll('tr').forEach((tr) => {
      var voteValidationHash = tr.dataset.blockhash
      var voteBlockHeight = tr.dataset.height
      if (voteBlockHeight > bestBlockHeight) {
        tr.classList.add('upcoming-vote')
        tr.classList.remove('old-vote')
      } else if (voteValidationHash !== bestBlockHash) {
        tr.classList.add('old-vote')
        tr.classList.remove('upcoming-vote')
        if (tr.classList.contains('last_block')) {
          tr.textContent = 'False'
        }
      } else {
        tr.classList.remove('old-vote')
        tr.classList.remove('upcoming-vote')
        if (tr.classList.contains('last_block')) {
          tr.textContent = 'True'
        }
      }
    })
  }

  sortVotesTable () {
    var rows = Array.from(this.voteTransactionsTarget.querySelectorAll('tr'))
    rows.sort(function (a, b) {
      if (a.dataset.height === b.dataset.height) {
        var indexA = parseInt(a.dataset.ticketIndex)
        var indexB = parseInt(b.dataset.ticketIndex)
        return (indexA - indexB)
      } else {
        return (b.dataset.height - a.dataset.height)
      }
    })
    this.voteTransactionsTarget.innerHTML = ''
    rows.forEach((row) => { this.voteTransactionsTarget.appendChild(row) })
  }
}
