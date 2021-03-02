import { Controller } from 'stimulus'
import humanize from '../helpers/humanize_helper'
import globalEventBus from '../services/event_bus_service'

function isCorrectVal (value) {
  return /^\d+$/.test(value) && value > 0
}

export default class extends Controller {
  static get targets () {
    return ['age', 'blocktime', 'header']
  }

  connect () {
    this.startAgeRefresh()
    this.processBlock = this._processBlock.bind(this)
    this.targetBlockTime = parseInt(document.getElementById('navBar').dataset.blocktime)
    if (this.hasBlocktimeTarget) {
      globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
    }
    if (this.hasHeaderTarget) {
      this.headerTargets.forEach(h => {
        h.textContent = h.dataset.jstitle
      })
    }
  }

  disconnect () {
    this.stopAgeRefresh()
    if (this.hasBlocktimeTarget) {
      globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
    }
  }

  _processBlock (blockData) {
    const block = blockData.block
    this.blocktimeTarget.dataset.stamp = block.unixStamp
    this.blocktimeTarget.classList.remove('text-danger')
    this.blocktimeTarget.textContent = humanize.timeSince(block.unixStamp)
  }

  startAgeRefresh () {
    setTimeout(() => {
      this.setAges()
    })
    this.ageRefreshTimer = setInterval(() => {
      this.setAges()
    }, 10 * 1000)
  }

  stopAgeRefresh () {
    if (this.ageRefreshTimer) {
      clearInterval(this.ageRefreshTimer)
    }
  }

  setAges () {
    if (this.hasBlocktimeTarget) {
      const lbt = this.blocktimeTarget.dataset.stamp
      this.blocktimeTarget.textContent = humanize.timeSince(lbt)
      if ((new Date()).getTime() / 1000 - lbt > 8 * this.targetBlockTime) { // 8*blocktime = 40minutes = 12000 seconds
        this.element.classList.add('text-danger')
      }
    }
    if (!this.hasAgeTarget) return
    this.ageTargets.forEach((el) => {
      if (isCorrectVal(el.dataset.age)) {
        el.textContent = humanize.timeSince(el.dataset.age)
      } else if (el.dataset.age !== '') {
        el.textContent = humanize.timeSince(Date.parse(el.dataset.age) / 1000)
      }
    })
  }
}
