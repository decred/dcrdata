import 'regenerator-runtime/runtime'
/* global require */
/* global $ */
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
