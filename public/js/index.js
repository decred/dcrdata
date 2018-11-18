/* global require */
/* global $ */
/* global Turbolinks */
import ws from './services/messagesocket_service'
import humanize from './helpers/humanize_helper'
import Notify from 'notifyjs'

import { Application } from 'stimulus'
import { definitionsFromContext } from 'stimulus/webpack-helpers'
import { darkEnabled } from './services/theme_service'
import { handleNextHomeBlockUpdate, handleMempoolUpdate } from './controllers/nexthome_blocks_controller'
import globalEventBus from './services/event_bus_service'

window.darkEnabled = darkEnabled

const application = Application.start()
const context = require.context('./controllers', true, /\.js$/)
application.load(definitionsFromContext(context))

$('.jsonly').show()
$.ajaxSetup({
  cache: true
})

function updateConnectionStatus (msg, connected) {
  var el = $('#connection')
  el.removeClass('hidden')
  if (connected) {
    el.addClass('connected')
    el.removeClass('disconnected')
  } else {
    el.removeClass('connected')
    el.addClass('disconnected')
  }
  el.html(msg + '<div></div>')
}

function getSocketURI (loc) {
  var protocol = (loc.protocol === 'https:') ? 'wss' : 'ws'
  return protocol + '://' + loc.host + '/ws'
}

function sleep (ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

// `formatTxDate`: Format a string to match the format `(UTC) YYYY-MM-DD HH:MM:SS` used by explorer.TxPage and tx.tmpl
function formatTxDate (stamp, withTimezone) {
  var d = new Date(stamp * 1000)
  var zone = withTimezone ? '(' + d.toLocaleTimeString('en-us', { timeZoneName: 'short' }).split(' ')[2] + ') ' : ''
  return zone + String(d.getFullYear()) + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0') + ' ' +
        String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0') + ':' + String(d.getSeconds()).padStart(2, '0')
}

async function createWebSocket (loc) {
  // wait a bit to prevent websocket churn from drive by page loads
  $('#connection').removeClass('hidden')
  var uri = getSocketURI(loc)
  await sleep(3000)
  ws.connect(uri)

  ws.registerEvtHandler('open', function () {
    console.log('Connected')
    updateConnectionStatus('Connected', true)
  })

  ws.registerEvtHandler('close', function () {
    console.log('Disconnected')
    updateConnectionStatus('Disconnected', false)
  })

  ws.registerEvtHandler('error', function (evt) {
    console.log('WebSocket error:', evt)
    updateConnectionStatus('Disconnected', false)
  })

  ws.registerEvtHandler('ping', function (evt) {
    console.debug('ping. users online: ', evt)
  })

  var updateBlockData = function (event) {
    console.log('Received newblock message', event)
    var newBlock = JSON.parse(event)
    var confirms = $('[data-confirmation-block-height]')
    $.each(confirms, function (i, v) {
      if (isNaN($(v).text())) {
        $(v).text('0')
      }
      var confirmations = newBlock.block.height - $(v).data('confirmation-block-height') + 1
      $(v).text('(' + confirmations + (confirmations > 1 ? ' confirmations' : ' confirmation') + ')')
    })

    globalEventBus.publish("BLOCK_RECEIVED", newBlock)

    // block summary data
    var b = newBlock.block
    b.unixStamp = (new Date(b.time)).getTime() / 1000
    desktopNotifyNewBlock(b)

    var currentTxHash = $('#spanTxID').data('tx-page-txid');
    if (currentTxHash) { // we are on the tx page
      // it is important to check that the current transaction is contained in the received block before any
      // partial update. but since we are silently reloading the page, we will not have the issue of the view
      // being wrongly updated that happens when the current transaction is not contained in the received block
      Turbolinks.reload()
    }

    // Update the blocktime counter.
    window.DCRThings.counter.data('time-lastblocktime', b.unixStamp).removeClass('text-danger')
    window.DCRThings.counter.html(humanize.timeSince(b.unixStamp))

    advanceTicketProgress(b)
    confirmAddrMempool(b)

    var expTableRows = $('#explorertable tbody tr')
    // var CurrentHeight = parseInt($('#explorertable tbody tr td').first().text());
    if (expTableRows) {
      var indexVal = ''
      if (window.location.pathname === '/blocks') {
        indexVal = '<td>' + b.windowIndex + '</td>'
      }
      expTableRows.last().remove()
      var newRow = '<tr id="' + b.height + '">' +
                indexVal +
                '<td><a href="/block/' + b.height + '" class="fs18">' + b.height + '</a></td>' +
                '<td>' + b.tx + '</td>' +
                '<td>' + b.votes + '</td>' +
                '<td>' + b.tickets + '</td>' +
                '<td>' + b.revocations + '</td>' +
                '<td>' + humanize.bytes(b.size) + '</td>' +
                '<td data-target="time.age"  data-age=' + b.unixStamp + '>' + humanize.timeSince(b.unixStamp) + '</td>' +
                '<td>' + b.time + '</td>' +
            '</tr>'
      var newRowHtml = $.parseHTML(newRow)
      $(newRowHtml).insertBefore(expTableRows.first())
    }

    var ex = newBlock.extra
    if (window.location.pathname === '/nexthome') {
      $('#coin-supply').html(humanize.decimalParts(ex.coin_supply / 100000000, true, 0, false))
      $('#bsubsidy-dev').html(humanize.decimalParts(ex.subsidy.dev / 100000000, false, 8, false, 2))
      $('#dev-fund').html(humanize.decimalParts(ex.dev_fund / 100000000, true, 0, false))
      $('#pool-value').html(humanize.decimalParts(ex.pool_info.value, true, 0, false))
      $('#pool-info-percentage').html('(' + parseFloat(ex.pool_info.percent).toFixed(2) + ' % of total supply)')
      $('#bsubsidy-pos').html(humanize.decimalParts((ex.subsidy.pos / 500000000), false, 8, false, 2))
      $('#ticket-reward').html('(' + humanize.fmtPercentage(ex.reward) + ' per ~29.07 days)')
      $('#ticket-price').html(humanize.decimalParts(ex.sdiff, false, 8, false, 2))
      $('#diff').html(humanize.decimalParts(ex.difficulty / 1000000, true, 0, false))
      $('#bsubsidy-pow').html(humanize.decimalParts(ex.subsidy.pow / 100000000, false, 8, false, 2))
      $('#hashrate').html(humanize.decimalParts(ex.hash_rate, false, 8, false, 2))
      $('#hashrate-subdata').html('(' + humanize.fmtPercentage(ex.hash_rate_change) + ' in 24hr)')
      handleNextHomeBlockUpdate(newBlock.block)
    }

    if (window.location.pathname === '/') {
      $('#difficulty').html(humanize.decimalParts(ex.difficulty, true, 8))
      $('#bsubsidy_total').html(humanize.decimalParts(ex.subsidy.total / 100000000, false, 8))
      $('#bsubsidy_pow').html(humanize.decimalParts(ex.subsidy.pow / 100000000, false, 8))
      $('#bsubsidy_pos').html(humanize.decimalParts((ex.subsidy.pos / 500000000), false, 8)) // 5 votes per block (usually)
      $('#bsubsidy_dev').html(humanize.decimalParts(ex.subsidy.dev / 100000000, false, 8))
      $('#coin_supply').html(humanize.decimalParts(ex.coin_supply / 100000000, true, 8))
      $('#blocksdiff').html(humanize.decimalParts(ex.sdiff, false, 8))
      $('#dev_fund').html(humanize.decimalParts(ex.dev_fund / 100000000, true, 8))
      $('#window_block_index').text(ex.window_idx)
      $('#pos-window-progess-bar').css({ width: (ex.window_idx / ex.params.window_size) * 100 + '%' })
      $('#reward_block_index').text(ex.reward_idx)
      $('#pow-window-progess-bar').css({ width: (ex.reward_idx / ex.params.reward_window_size) * 100 + '%' })
      $('#pool_size').text(ex.pool_info.size)
      $('#pool_value').html(humanize.decimalParts(ex.pool_info.value, true, 8))
      $('#ticket_reward').html(parseFloat(ex.reward).toFixed(2))
      $('#target_percent').html(parseFloat(ex.pool_info.percent_target).toFixed(2))
      $('#pool_size_percentage').html(parseFloat(ex.pool_info.percent).toFixed(2))
    }

    // handling status page for a future block
    if ($('#futureblock').length) {
      Turbolinks.visit(window.location, { action: 'replace' })
    }
  }
  ws.registerEvtHandler('newblock', updateBlockData)

  ws.registerEvtHandler('mempool', function (event) {
    if (window.location.pathname === '/nexthome') {
      ws.send('getmempooltxs', '')
    } else {
      var mempool = JSON.parse(event)
      $('#mempool-total-sent').html(humanize.decimalParts(mempool.total, false, 2, false, 2))
      $('#mempool-tx-count').text('sent in ' + mempool.num_all + ' transactions')
      $('#mempool-votes').text(mempool.num_votes)
      $('#mempool-tickets').text(mempool.num_tickets)
    }
  })

  ws.registerEvtHandler('getmempooltxsResp', function (event) {
    console.log('received mempooltx response', event)
    if (window.location.pathname === '/nexthome') {
      handleMempoolUpdate(event)
    }
  })
}

document.getElementById('search').onkeypress = function (e) {
  if (!e) e = window.event
  var keyCode = e.keyCode || e.which
  if (keyCode === '13') {
    Turbolinks.visit('/search?search=' + this.value)
    return false
  }
}

// desktop notifications
function onShowNotification () {
  console.log('block ntfn shown')
}
function onCloseNotification () {
  console.log('block ntfn closed')
}
function onClickNotification () {
  console.log('block ntfn clicked')
}
function onErrorNotification () {
  console.error('Error showing notification. You may need to request permission.')
}
function onPermissionGranted () {
  console.log('Permission has been granted by the user')
}
function onPermissionDenied () {
  console.warn('Permission has been denied by the user')
}

function doNotification (block) {
  var newBlockNtfn = new Notify('New Decred Block Mined', {
    body: 'Block mined at height ' + block.height,
    tag: 'blockheight',
    image: '/images/dcrdata144x128.png',
    icon: '/images/dcrdata144x128.png',
    notifyShow: onShowNotification,
    notifyClose: onCloseNotification,
    notifyClick: onClickNotification,
    notifyError: onErrorNotification,
    timeout: 10
  })
  newBlockNtfn.show()
}

function desktopNotifyNewBlock (block) {
  if (!Notify.needsPermission) {
    doNotification(block)
  }
}

// Check for the txid in the given block
function txInBlock (txid, block) {
  var txTypes = [block.Tx, block.Tickets, block.Revs, block.Votes]
  for (let txIdx in txTypes) {
    let txs = txTypes[txIdx]
    for (let idx in txs) {
      if (txs[idx].TxID === txid) {
        return true
      }
    }
  }
  return false
}

// Advance various progress bars on /tx.
function advanceTicketProgress (block) {
  // Check for confirmations on mempool transactions.
  var needsConfirmation = $('[data-txid-mempool-display]')
  if (needsConfirmation.length && needsConfirmation.text() === 'mempool') {
    var txid = needsConfirmation.data('txid-mempool-display')
    if (txInBlock(txid, block)) {
      needsConfirmation.remove()
      $('[data-confirmation-block-height]').data('confirmation-block-height', block.height).html('(1 confirmation)')
      $('#txMempoolLink').html(block.height).attr('href', '/block/' + block.hash)
      $('#txFmtTime').html(formatTxDate(block.time, true))
      $('#txAge').html(humanize.timeSince(block.time)).before('(').after(' ago)').attr('data-age', block.time)
      var progressBar = $('#txMaturityProgress')
      progressBar.attr('data-confirm-height', block.height)
    }
  }
  // Advance the progress bars.
  var ticketProgress = $('#txMaturityProgress')
  if (!ticketProgress.length) {
    return
  }
  var txBlockHeight = ticketProgress.data('confirm-height')
  if (txBlockHeight === 0) {
    return
  }
  var confirmations = block.height - txBlockHeight + 1
  var txType = ticketProgress.data('tx-type')
  var complete = parseInt(ticketProgress.attr('aria-valuemax'))
  if (confirmations === complete + 1) {
    ticketProgress.closest('.row').replaceWith(txType === 'LiveTicket' ? 'Expired' : 'Mature')
    return
  }
  var ratio = confirmations / complete
  if (confirmations === complete) {
    ticketProgress.children('span').html(txType === 'Ticket' ? 'Mature. Eligible to vote on next block.'
      : txType === 'LiveTicket' ? 'Ticket has expired' : 'Mature. Ready to spend.')
    var status = $('#txPoolStatus')
    if (status.length) {
      status.html('live / unspent')
    }
  } else {
    var blocksLeft = complete + 1 - confirmations
    var remainingTime = blocksLeft * window.DCRThings.targetBlockTime
    if (txType === 'LiveTicket') {
      ticketProgress.children('span').html('block ' + confirmations + ' of ' + complete + ' (' + (remainingTime / 86400.0).toFixed(1) + ' days remaining)')
      // Chance of expiring is (1-P)^N where P := single-block probability of being picked, N := blocks remaining.
      // This probability is using the network parameter rather than the calculated value. So far, the ticket pool size seems stable enough to do that.
      var pctChance = Math.pow(1 - 1 / window.DCRThings.ticketPoolSize, blocksLeft) * 100
      $('#ticketExpiryChance').children('span').html(pctChance.toFixed(2) + '% chance of expiry')
    } else {
      var typeStub = txType === 'Ticket' ? 'eligible to vote' : 'spendable'
      ticketProgress.children('span').html('Immature, ' + typeStub + ' in ' + blocksLeft + ' blocks (' + (remainingTime / 3600.0).toFixed(1) + ' hours remaining)')
    }
    ticketProgress.attr('aria-valuenow', confirmations).css({ 'width': (ratio * 100).toString() + '%' })
  }
}

// Check the block for mempool transactions on the address page.
function confirmAddrMempool (block) {
  var mempoolRows = $('[data-addr-tx-pending]')
  mempoolRows.each(function (idx, tr) {
    var row = $(tr)
    var txid = row.data('addr-tx-pending')
    if (txInBlock(txid, block)) {
      var confirms = row.children('.addr-tx-confirms')
      confirms.attr('data-tx-block-height', block.height)
      row.removeAttr('data-addr-tx-pending')
      row.children('.addr-tx-time').html(formatTxDate(block.time, false))
      row.children('.addr-tx-age').children('span').attr('data-age', block.time).html(humanize.timeSince(block.time))
      confirms.html('1')
    }
  })
  var confirmTds = $('[data-tx-block-height]')
  confirmTds.each(function (idx, td) {
    var confirms = $(td)
    confirms.html(block.height - parseInt(confirms.data('tx-block-height')) + 1)
  })
  var pendingCounters = $('[data-tx-confirmations-pending]')
  pendingCounters.each(function (i, c) {
    var counter = $(c)
    var txid = counter.data('tx-confirmations-pending')
    if (txInBlock(txid, block)) {
      counter.removeAttr('data-tx-confirmations-pending')
      counter.attr('data-confirmation-block-height', block.height).html('(1 confirmation)')
    }
  })
}

createWebSocket(window.location)

$('.scriptDataStar').on('click', function () {
  $(this).next('.scriptData').slideToggle()
})
$('#connection').on('click', function () {
  if (Notify.needsPermission) {
    Notify.requestPermission(onPermissionGranted, onPermissionDenied)
  }
})
$('#listing-view').change(function () {
  Turbolinks.visit('/' +
        $('#listing-view option:selected').val()
  )
})
window.DCRThings.counter = $('[data-time-lastblocktime]')

$('.scriptDataStar').on('click', function () {
  $(this).next('.scriptData').slideToggle()
})
$('#connection').on('click', function () {
  if (Notify.needsPermission) {
    Notify.requestPermission(onPermissionGranted, onPermissionDenied)
  }
})
$('#listing-view').change(function () {
  Turbolinks.visit('/' +
        $('#listing-view option:selected').val()
  )
})
