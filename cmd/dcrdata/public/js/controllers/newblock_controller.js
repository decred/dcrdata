import { Controller } from 'stimulus'
import globalEventBus from '../services/event_bus_service'

export default class extends Controller {
  static get targets () {
    return ['confirmations']
  }

  initialize () {
    const that = this
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
    if (confirmations > 0) {
      el.textContent = el.dataset.yes.replace('#', confirmations).replace('@', confirmations > 1 ? 's' : '')
      el.classList.add('confirmed')
    } else {
      el.textContent = el.dataset.no
      el.classList.remove('confirmed')
    }
  }

  refreshConfirmations (expHeight) {
    this.confirmationsTargets.forEach((el, i) => {
      const confirmHeight = parseInt(el.dataset.confirmationBlockHeight)
      if (confirmHeight === -1) return // Unconfirmed block
      const confirmations = expHeight - confirmHeight + 1
      this.setConfirmationText(el, confirmations)
      el.dataset.confirmations = confirmations
    })
  }
}
