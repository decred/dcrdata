import txInBlock from '../helpers/block_helper'
import globalEventBus from '../services/event_bus_service'
import { Controller } from 'stimulus'
import humanize from '../helpers/humanize_helper'

export default class extends Controller {
  static get targets () {
    return ['unconfirmed', 'confirmations', 'formattedAge', 'age', 'progressBar',
      'ticketStage', 'expiryChance', 'mempoolTd', 'ticketMsg',
      'expiryMsg', 'statusMsg', 'spendingTx']
  }

  connect () {
    this.txid = this.data.get('txid')
    this.processBlock = this._processBlock.bind(this)
    this.targetBlockTime = parseInt(document.getElementById('navBar').dataset.blocktime)
    globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
  }

  disconnect () {
    globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
  }

  _processBlock (blockData) {
    const block = blockData.block
    const extra = blockData.extra
    // If this is a transaction in mempool, it will have an unconfirmedTarget.
    if (this.hasUnconfirmedTarget) {
      const txid = this.unconfirmedTarget.dataset.txid
      if (txInBlock(txid, block)) {
        this.confirmationsTarget.textContent = this.confirmationsTarget.dataset.yes.replace('#', '1').replace('@', '')
        this.confirmationsTarget.classList.add('confirmed')
        // Set the block link
        const link = this.unconfirmedTarget.querySelector('.mp-unconfirmed-link')
        link.href = '/block/' + block.hash
        link.textContent = block.height
        this.unconfirmedTarget.querySelector('.mp-unconfirmed-msg').classList.add('d-none')
        // Reset the age and time to be based off of the block time.
        const age = this.unconfirmedTarget.querySelector('.mp-unconfirmed-time')
        age.dataset.age = block.time
        age.textContent = humanize.timeSince(block.unixStamp)
        this.formattedAgeTarget.textContent = humanize.date(block.time, true)
        this.ageTarget.dataset.age = block.unixStamp
        this.ageTarget.textContent = humanize.timeSince(block.unixStamp)
        this.ageTarget.dataset.target = 'time.age'
        // Prepare the progress bar for updating
        if (this.hasProgressBarTarget) {
          this.progressBarTarget.dataset.confirmHeight = block.height
        }
        delete this.unconfirmedTarget.dataset.target
      }
    }

    // Look for any unconfirmed matching tx hashes in the table.
    if (this.hasMempoolTdTarget) {
      this.mempoolTdTargets.forEach((td) => {
        const txid = td.dataset.txid
        if (txInBlock(txid, block)) {
          const link = document.createElement('a')
          link.textContent = block.height
          link.href = `/block/${block.height}`
          while (td.firstChild) td.removeChild(td.firstChild)
          td.appendChild(link)
          delete td.dataset.target
        }
      })
    }

    // Advance the progress bars.
    if (!this.hasProgressBarTarget) {
      return
    }
    const bar = this.progressBarTarget
    let txBlockHeight = parseInt(bar.dataset.confirmHeight)
    if (txBlockHeight === 0) {
      return
    }
    let confirmations = block.height - txBlockHeight + 1
    let txType = bar.dataset.txType
    let complete = parseInt(bar.getAttribute('aria-valuemax'))

    if (txType === 'LiveTicket') {
      // Check for a spending vote
      const votes = block.Votes || []
      for (const idx in votes) {
        const vote = votes[idx]
        if (this.txid === vote.VoteInfo.ticket_spent) {
          const link = document.createElement('a')
          link.href = `/tx/${vote.TxID}`
          link.textContent = 'vote'
          const msg = this.spendingTxTarget
          while (msg.firstChild) msg.removeChild(msg.firstChild)
          msg.appendChild(link)
          this.ticketStageTarget.innerHTML = 'Voted'
          return
        }
      }
    }

    if (confirmations === complete + 1) {
      // Hide bars after completion, or change ticket to live ticket
      if (txType === 'Ticket') {
        txType = bar.dataset.txType = 'LiveTicket'
        const expiry = parseInt(bar.dataset.expiry)
        bar.setAttribute('aria-valuemax', expiry)
        txBlockHeight = bar.dataset.confirmHeight = block.height
        this.ticketMsgTarget.classList.add('d-none')
        this.expiryMsgTarget.classList.remove('d-none')
        confirmations = 1
        complete = expiry
      } else {
        this.ticketStageTarget.innerHTML = txType === 'LiveTicket' ? 'Expired' : 'Mature'
        return
      }
    }

    const barMsg = bar.querySelector('span')
    if (confirmations === complete) {
      // Special case: progress reaching max
      switch (txType) {
        case 'Ticket':
          barMsg.textContent = 'Mature. Eligible to vote on next block.'
          this.statusMsgTarget.textContent = 'live'
          break
        case 'LiveTicket':
          barMsg.textContent = 'Ticket has expired'
          delete bar.dataset.target
          this.statusMsgTarget.textContent = 'expired'
          break
        default: // Vote
          barMsg.textContent = 'Mature. Ready to spend.'
          this.statusMsgTarget.textContent = 'mature'
      }
      return
    }

    // Otherwise, set the bar appropriately
    const blocksLeft = complete + 1 - confirmations
    const remainingTime = blocksLeft * this.targetBlockTime
    switch (txType) {
      case 'LiveTicket': {
        barMsg.textContent = `block ${confirmations} of ${complete} (${(remainingTime / 86400.0).toFixed(1)} days remaining)`
        // Chance of expiring is (1-P)^N where P := single-block probability of being picked, N := blocks remaining.
        const pctChance = Math.pow(1 - parseFloat(bar.dataset.ticketsPerBlock) / extra.pool_info.size, blocksLeft) * 100
        this.expiryChanceTarget.textContent = `${pctChance.toFixed(2)}%`
        break
      }
      case 'Ticket':
        barMsg.textContent = `Immature, eligible to vote in ${blocksLeft} blocks (${(remainingTime / 3600.0).toFixed(1)} hours remaining)`
        break
      default: // Vote
        barMsg.textContent = `Immature, spendable in ${blocksLeft} blocks (${(remainingTime / 3600.0).toFixed(1)} hours remaining)`
    }
    bar.setAttribute('aria-valuenow', confirmations)
    bar.style.width = `${(confirmations / complete * 100).toString()}%`
  }

  toggleScriptData (e) {
    const target = e.srcElement || e.target
    const scriptData = target.querySelector('div.script-data')
    if (!scriptData) return
    scriptData.classList.toggle('d-hide')
  }
}
