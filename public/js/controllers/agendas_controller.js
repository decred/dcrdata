import { Controller } from 'stimulus'
import globalEventBus from '../services/event_bus_service'
import { darkEnabled } from '../services/theme_service'
import VoteMeter from '../helpers/vote_meter.js'

export default class extends Controller {
  static get targets () {
    return [
      'minerMeter', 'voterMeter', 'quorumMeter', 'approvalMeter'
    ]
  }

  connect () {
    this.minerMeter = this.voterMeter = null
    this.meters = []
    var opts = {
      darkMode: darkEnabled()
    }
    if (this.hasMinerMeterTarget) {
      this.minerMeter = new VoteMeter(this.minerMeterTarget, opts)
      this.meters.push(this.minerMeter)
    }
    if (this.hasVoterMeterTarget) {
      this.voterMeter = new VoteMeter(this.voterMeterTarget, Object.assign({
        meterColor: '#2970ff'
      }, opts))
      this.meters.push(this.voterMeter)
    }
    if (this.hasQuorumMeterTarget) {
      this.quorumMeter = new VoteMeter(this.quorumMeterTarget, Object.assign({
        meterColor: '#2970ff'
      }, opts))
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
