/* global $ */
import { Controller } from 'stimulus'
import { map, each } from 'lodash-es'
import humanize from '../helpers/humanize_helper'
import ws from '../services/messagesocket_service'
import { keyNav } from '../services/keyboard_navigation_service'

function incrementValue ($el) {
  if ($el.length > 0) {
    $el.text(
      parseInt($el.text()) + 1
    )
  }
}

function txTableRow (tx) {
  return `<tr class="flash">
        <td class="break-word"><span><a class="hash" href="/tx/${tx.hash}" title="${tx.hash}">${tx.hash}</a></span></td>
        <td class="mono fs15 text-right">${humanize.decimalParts(tx.total, false, 8, true)}</td>
        <td class="mono fs15 text-right">${tx.size} B</td>
        <td class="mono fs15 text-right" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
    </tr>`
}

function voteTxTableRow (tx) {
  return `<tr class="flash" data-height="${tx.vote_info.block_validation.height}" data-blockhash="${tx.vote_info.block_validation.hash}">
        <td class="break-word"><span><a class="hash" href="/tx/${tx.hash}">${tx.hash}</a></span></td>
        <td class="mono fs15"><span><a href="/block/${tx.vote_info.block_validation.hash}">${tx.vote_info.block_validation.height}</a></span></td>
        <td class="mono fs15 last_block">${tx.vote_info.last_block ? 'True' : 'False'}</td>
        <td class="mono fs15"><a href="/tx/${tx.vote_info.ticket_spent}">${tx.vote_info.mempool_ticket_index}<a/></td>
        <td class="mono fs15">${tx.vote_info.vote_version}</td>
        <td class="mono fs15 text-right">${humanize.decimalParts(tx.total, false, 8, true)}</td>
        <td class="mono fs15">${tx.size} B</td>
        <td class="mono fs15 text-right" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</td>
    </tr>`
}

function buildTable (target, txType, txns, rowFn) {
  let tableBody
  if (txns && txns.length > 0) {
    tableBody = map(txns, rowFn).join('')
  } else {
    tableBody = `<tr><td colspan="${(txType === 'votes' ? 8 : 4)}">No ${txType} in mempool.</td></tr>`
  }
  $(target).html($.parseHTML(tableBody))
}

function addTxRow (tx, target, rowFn) {
  var rows = $(target).find('tr')
  var newRowHtml = $.parseHTML(rowFn(tx))
  $(newRowHtml).insertBefore(rows.first())
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
    $(this.numTicketTarget).text(m.num_tickets)
    $(this.numVoteTarget).text(m.num_votes)
    $(this.numRegularTarget).text(m.num_regular)
    $(this.numRevokeTarget).text(m.num_revokes)
    $(this.bestBlockTarget).text(m.block_height)
    $(this.bestBlockTarget).data('hash', m.block_hash)
    $(this.bestBlockTarget).attr('data-hash', m.block_hash)
    $(this.bestBlockTarget).attr('href', '/block/' + m.block_hash)
    $(this.bestBlockTimeTarget).data('age', m.block_time)
    $(this.bestBlockTimeTarget).attr('data-age', m.block_time)
    $(this.mempoolSizeTarget).text(m.formatted_size)
    $(this.ticketsVoted).text(m.voting_info.tickets_voted)
    $(this.maxVotesPerBlock).text(m.voting_info.max_votes_per_block)
    $(this.totalOutTarget).html(`${humanize.decimalParts(m.total, false, 8, true)}`)
    $(this.mempoolSizeTarget).text(m.formatted_size)
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
      incrementValue($(this['num' + tx.Type + 'Target']))
      var rowFn = tx.Type === 'Vote' ? voteTxTableRow : txTableRow
      addTxRow(tx, this[tx.Type.toLowerCase() + 'TransactionsTarget'], rowFn)
    })
  }

  labelVotes () {
    var bestBlockHash = $(this.bestBlockTarget).data('hash')
    var bestBlockHeight = $(this.bestBlockTarget).text()
    $(this.voteTransactionsTarget).children('tr').each(function () {
      var voteValidationHash = $(this).data('blockhash')
      var voteBlockHeight = $(this).data('height')
      if (voteBlockHeight > bestBlockHeight) {
        $(this).closest('tr').addClass('upcoming-vote')
        $(this).closest('tr').removeClass('old-vote')
      } else if (voteValidationHash !== bestBlockHash) {
        $(this).closest('tr').addClass('old-vote')
        $(this).closest('tr').removeClass('upcoming-vote')
        $(this).find('td.last_block').text('False')
      } else {
        $(this).closest('tr').removeClass('old-vote')
        $(this).closest('tr').removeClass('upcoming-vote')
        $(this).find('td.last_block').text('True')
      }
    })
  }

  sortVotesTable () {
    var $rows = $(this.voteTransactionsTarget).children('tr')
    $rows.sort(function (a, b) {
      var heightA = parseInt($('td:nth-child(2)', a).text())
      var heightB = parseInt($('td:nth-child(2)', b).text())
      if (heightA === heightB) {
        var indexA = parseInt($('td:nth-child(4)', a).text())
        var indexB = parseInt($('td:nth-child(4)', b).text())
        return (indexA - indexB)
      } else {
        return (heightB - heightA)
      }
    })
    $(this.voteTransactionsTarget).html($rows)
  }
}
