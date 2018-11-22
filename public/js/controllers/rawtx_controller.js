/* global $ */
import { Controller } from 'stimulus'
import ws from '../services/messagesocket_service'

export default class extends Controller {
  static get targets () {
    return [
      'decode',
      'broadcast',
      'rawTransaction',
      'decodedTransaction',
      'decodeHeader'
    ]
  }

  connect () {
    ws.registerEvtHandler('decodetxResp', (evt) => {
      $(this.decodeHeaderTarget).text('Decoded tx')
      $(this.decodedTransactionTarget).text(evt)
    })
    ws.registerEvtHandler('sendtxResp', (evt) => {
      $(this.decodeHeaderTarget).text('Sent tx')
      $(this.decodedTransactionTarget).text(evt)
    })
  }

  disconnect () {
    ws.deregisterEvtHandlers('decodetxResp')
    ws.deregisterEvtHandlers('sendtxResp')
  }

  send (e) {
    if (e.type === 'keypress' && e.keyCode !== 13) {
      return
    }
    if (this.rawTransactionTarget.value !== '') {
      ws.send(e.target.dataset.eventId, this.rawTransactionTarget.value)
      $(this.rawTransactionTarget).text('')
      $(this.decodedTransactionTarget).text('')
      $(this.decodedTransactionTarget).fadeTo(0, 0.3, (el) => {
        $(this.decodedTransactionTarget).fadeTo(500, 1.0)
      })
    }
  }
}
