import 'regenerator-runtime/runtime'
/* global require */
/* global $ */
/* global Turbolinks */
import ws from './js/services/messagesocket_service'
import humanize from './js/helpers/humanize_helper'
import './js/services/desktop_notification_service'
import { Application } from 'stimulus'
import { definitionsFromContext } from 'stimulus/webpack-helpers'
import { darkEnabled } from './js/services/theme_service'
import globalEventBus from './js/services/event_bus_service'

require('./scss/application.scss')

window.darkEnabled = darkEnabled

const application = Application.start()
const context = require.context('./js/controllers', true, /\.js$/)
application.load(definitionsFromContext(context))

document.addEventListener('turbolinks:load', function (e) {
  document.querySelectorAll('.jsonly').forEach((el) => {
    el.classList.remove('jsonly')
  })
})

{
  let navBar = document.getElementById('navBar')
  window.DCRThings = {}
  window.DCRThings.targetBlockTime = navBar.dataset.blocktime
  window.DCRThings.ticketPoolSize = navBar.dataset.poolsize
}

function getSocketURI (loc) {
  var protocol = (loc.protocol === 'https:') ? 'wss' : 'ws'
  return protocol + '://' + loc.host + '/ws'
}

function sleep (ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

async function createWebSocket (loc) {
  // wait a bit to prevent websocket churn from drive by page loads
  var uri = getSocketURI(loc)
  await sleep(3000)
  ws.connect(uri)

  var updateBlockData = function (event) {
    console.log('Received newblock message', event)
    var newBlock = JSON.parse(event)
    var b = newBlock.block
    b.unixStamp = (new Date(b.time)).getTime() / 1000

    globalEventBus.publish('BLOCK_RECEIVED', newBlock)

    // Move to blocklist controller
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

    // Create homepage controller. Move this there.
    if (window.location.pathname === '/') {
      var ex = newBlock.extra
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

  // This appears to be unused.
  ws.registerEvtHandler('mempool', function (event) {
    var mempool = JSON.parse(event)
    $('#mempool-total-sent').html(humanize.decimalParts(mempool.total, false, 2, false, 2))
    $('#mempool-tx-count').text('sent in ' + mempool.num_all + ' transactions')
    $('#mempool-votes').text(mempool.num_votes)
    $('#mempool-tickets').text(mempool.num_tickets)
  })
}

createWebSocket(window.location)
