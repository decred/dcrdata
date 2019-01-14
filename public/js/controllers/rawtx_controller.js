import { Controller } from 'stimulus'
import ws from '../services/messagesocket_service'
import { fadeIn } from '../helpers/animation_helper'

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
      this.decodeHeaderTarget.textContent = 'Decoded tx'
      fadeIn(this.decodedTransactionTarget)
      this.decodedTransactionTarget.textContent = evt
    })
    ws.registerEvtHandler('sendtxResp', (evt) => {
      this.decodeHeaderTarget.textContent = 'Sent tx'
      fadeIn(this.decodedTransactionTarget)
      this.decodedTransactionTarget.textContent = evt
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
      this.rawTransactionTarget.textContent = ''
      this.decodedTransactionTarget.textContent = ''
    }
  }
}
