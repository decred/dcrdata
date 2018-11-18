/* global $ */
import { Controller } from 'stimulus'
import { each } from 'lodash-es'
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

function txFlexTableRow (tx) {
  return `<div class="d-flex flex-table-row flash">
        <a class="hash truncate-hash" style="flex: 1 1 auto" href="/tx/${tx.hash}" title="${tx.hash}">${tx.hash}</a>
        <span style="flex: 0 0 60px" class="mono text-right ml-1">${tx.Type}</span>
        <span style="flex: 0 0 105px" class="mono text-right ml-1">${humanize.decimalParts(tx.total, false, 8, true)}</span>
        <span style="flex: 0 0 50px" class="mono text-right ml-1">${tx.size} B</span>
        <span style="flex: 0 0 65px" class="mono text-right ml-1" data-target="time.age" data-age="${tx.time}">${humanize.timeSince(tx.time)}</span>
    </div>`
}

export default class extends Controller {
  static get targets () {
    return [
      'transactions',
      'numVote',
      'numTicket'
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
      $(this.numTicketTarget).text(m.num_tickets)
      $(this.numVoteTarget).text(m.num_votes)
      keyNav(evt, false, true)
      ws.send('getmempooltxs', '')
    })
    ws.registerEvtHandler('getmempooltxsResp', (evt) => {
      var m = JSON.parse(evt)
      this.renderLatestTransactions(m.latest, true)
      keyNav(evt, false, true)
    })
  }

  disconnect () {
    ws.deregisterEvtHandlers('newtx')
    ws.deregisterEvtHandlers('mempool')
    ws.deregisterEvtHandlers('getmempooltxsResp')
  }

  renderLatestTransactions (txs, incremental) {
    console.log('render', txs)
    each(txs, (tx) => {
      if (incremental) {
        incrementValue($(this['num' + tx.Type + 'Target']))
      }
      var rows = $(this.transactionsTarget).find('.flex-table-row')
      rows.last().remove()
      $($.parseHTML(txFlexTableRow(tx))).insertBefore(rows.first())
    })
  }
}
