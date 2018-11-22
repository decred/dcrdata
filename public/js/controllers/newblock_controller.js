import { Controller } from 'stimulus'
import globalEventBus from '../services/event_bus_service'

export default class extends Controller {
  static get targets () {
    return [ 'confirmations' ]
  }
  initialize () {
    let that = this
    globalEventBus.on('BLOCK_RECEIVED', function (data) {
      that.refreshConfirmations(data.block.height)
    })
  }
  connect () {
    this.confirmationsTargets.forEach((el, i) => {
      if (!el.dataset.confirmations) return
      this.setConfirmationText(el, el.dataset.confirmations)
    })
  }
  setConfirmationText (el, confirmations) {
    if (!el.dataset.formatted) {
      el.textContent = confirmations
      return
    }
    if (confirmations > 0) {
      el.textContent = '(' + confirmations + (confirmations > 1 ? ' confirmations' : ' confirmation') + ')'
    } else {
      el.textContent = '(unconfirmed)'
    }
  }
  refreshConfirmations (expHeight) {
    this.confirmationsTargets.forEach((el, i) => {
      let confirmHeight = parseInt(el.dataset.confirmationBlockHeight)
      if (confirmHeight === -1) return // Unconfirmed block
      let confirmations = expHeight - confirmHeight + 1
      this.setConfirmationText(el, confirmations)
      el.dataset.confirmations = confirmations
    })
  }
}
