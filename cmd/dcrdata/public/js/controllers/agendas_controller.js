import { Controller } from 'stimulus'
import globalEventBus from '../services/event_bus_service'
import { darkEnabled } from '../services/theme_service'
import { VoteMeter, ProgressMeter } from '../helpers/meters.js'

export default class extends Controller {
  static get targets () {
    return [
      'minerMeter', 'voterMeter', 'quorumMeter', 'approvalMeter'
    ]
  }

  connect () {
    this.minerMeter = this.voterMeter = null
    this.meters = []
    const opts = {
      darkMode: darkEnabled()
    }
    if (this.hasMinerMeterTarget) {
      this.minerMeter = new ProgressMeter(this.minerMeterTarget, opts)
      this.meters.push(this.minerMeter)
    }
    if (this.hasVoterMeterTarget) {
      this.voterMeter = new ProgressMeter(this.voterMeterTarget, opts)
      this.meters.push(this.voterMeter)
    }
    if (this.hasQuorumMeterTarget) {
      this.quorumMeter = new ProgressMeter(this.quorumMeterTarget, opts)
      this.meters.push(this.quorumMeter)
    }
    if (this.hasApprovalMeterTarget) {
      this.approvalMeter = new VoteMeter(this.approvalMeterTarget, opts)
      this.meters.push(this.approvalMeter)
    }
    this.setNightMode = this._setNightMode.bind(this)
    globalEventBus.on('NIGHT_MODE', this.setNightMode)
  }

  disconnect () {
    globalEventBus.off('NIGHT_MODE', this.setNightMode)
  }

  _setNightMode (state) {
    this.meters.forEach((meter) => {
      meter.setDarkMode(state.nightMode)
    })
  }
}
