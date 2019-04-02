import 'regenerator-runtime/runtime'
/* global require */
import ws from './js/services/messagesocket_service'
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
  await sleep(1000)
  ws.connect(uri)

  var updateBlockData = function (event) {
    console.log('Received newblock message', event)
    var newBlock = JSON.parse(event)
    newBlock.block.unixStamp = new Date(newBlock.block.time).getTime() / 1000
    globalEventBus.publish('BLOCK_RECEIVED', newBlock)
  }
  ws.registerEvtHandler('newblock', updateBlockData)
  ws.registerEvtHandler('exchange', e => {
    globalEventBus.publish('EXCHANGE_UPDATE', JSON.parse(e))
  })
}

createWebSocket(window.location)
