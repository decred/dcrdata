import txInBlock from '../helpers/block_helper'
import globalEventBus from '../services/event_bus_service'
import { Controller } from 'stimulus'
import humanize from '../helpers/humanize_helper'

export default class extends Controller {
  static get targets () {
    return ['unconfirmed', 'confirmations', 'formattedAge', 'age', 'progressBar',
      'ticketStage', 'poolStatus', 'expiryBar']
  }

  connect () {
    this.advanceTicketProgress = this._advanceTicketProgress.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.advanceTicketProgress)
  }

  disconnect () {
    globalEventBus.off('BLOCK_RECEIVED', this.advanceTicketProgress)
  }

  _advanceTicketProgress (blockData) {
    var block = blockData.block
    // If this is a transaction in mempool, it will have an unconfirmedTarget.
    if (this.hasUnconfirmedTarget) {
      let txid = this.unconfirmedTarget.dataset.txid
      if (txInBlock(txid, block)) {
        this.confirmationsTarget.dataset.confirmationBlockHeight = block.height
        this.confirmationsTarget.textContent = '(1 confirmation)'
        let link = this.unconfirmedTarget.querySelector('a')
        link.href = '/block/' + block.hash
        link.textContent = block.height
        let span = this.unconfirmedTarget.querySelector('span')
        this.unconfirmedTarget.removeChild(span)
        this.formattedAgeTarget.textContent = humanize.formatTxDate(block.time, true)
        this.ageTarget.textContent = '( ago)'
        this.ageTarget.dataset.age = block.time
        this.ageTarget.textContent = `(${humanize.timeSince(block.unixStamp)} ago)`
        this.ageTarget.dataset.target = 'time.age'
        if (this.hasProgressBarTarget) {
          this.progressBarTarget.dataset.confirmHeight = block.height
        }
        delete this.unconfirmedTarget.dataset.target
      }
    }
    // Advance the progress bars.
    if (!this.hasProgressBarTarget) {
      return
    }
    var bar = this.progressBarTarget
    var txBlockHeight = parseInt(bar.dataset.confirmHeight)
    if (txBlockHeight === 0) {
      return
    }
    var confirmations = block.height - txBlockHeight + 1
    var txType = bar.dataset.txType
    var complete = parseInt(bar.getAttribute('aria-valuemax'))
    if (confirmations === complete + 1) {
      this.ticketStageTarget.innerHTML = txType === 'LiveTicket' ? 'Expired' : 'Mature'
      return
    }
    var ratio = confirmations / complete
    if (confirmations === complete) {
      bar.querySelector('span').textContent = txType === 'Ticket' ? 'Mature. Eligible to vote on next block.'
        : txType === 'LiveTicket' ? 'Ticket has expired' : 'Mature. Ready to spend.'
      if (this.hasPoolStatusTarget) {
        this.poolStatusTarget.textContent = 'live / unspent'
      }
    } else {
      let blocksLeft = complete + 1 - confirmations
      let remainingTime = blocksLeft * window.DCRThings.targetBlockTime
      if (txType === 'LiveTicket') {
        bar.querySelector('span').textContent = `block ${confirmations} of ${complete} (${(remainingTime / 86400.0).toFixed(1)} days remaining)`
        // Chance of expiring is (1-P)^N where P := single-block probability of being picked, N := blocks remaining.
        // This probability is using the network parameter rather than the calculated value. So far, the ticket pool size seems stable enough to do that.
        let pctChance = Math.pow(1 - 1 / window.DCRThings.ticketPoolSize, blocksLeft) * 100
        this.expiryBarTarget.querySelector('span').textContent = pctChance.toFixed(2) + '% chance of expiry'
      } else {
        let typeStub = txType === 'Ticket' ? 'eligible to vote' : 'spendable'
        bar.querySelector('span').textContent = `Immature, ${typeStub} in ${blocksLeft} blocks (${(remainingTime / 3600.0).toFixed(1)} hours remaining)`
      }
      bar.setAttribute('aria-valuenow', confirmations)
      bar.style.width = `${(ratio * 100).toString()}%`
    }
  }

  toggleScriptData (e) {
    var target = e.srcElement || e.target
    var scriptData = target.querySelector('div.script-data')
    if (!scriptData) return
    scriptData.classList.toggle('d-hide')
  }
}
