import { Controller } from 'stimulus'
import humanize from '../helpers/humanize_helper'
import globalEventBus from '../services/event_bus_service'

export default class extends Controller {
  static get targets () {
    return ['coinSupply', 'devSubsidy', 'devFund', 'poolValue', 'stakePct', 'posSubsidy',
      'ticketReward', 'ticketPrice', 'diff', 'powSubsidy', 'hashrate', 'hashrateDelta']
  }

  connect () {
    this.renderNewBlock = this._renderNewBlock.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.renderNewBlock)
  }

  disconnect () {
    globalEventBus.off('BLOCK_RECEIVED', this.renderNewBlock)
  }

  _renderNewBlock (newBlock) {
    var ex = newBlock.extra
    this.coinSupplyTarget.innerHTML = humanize.decimalParts(ex.coin_supply / 100000000, true, 0)
    this.devSubsidyTarget.innerHTML = humanize.decimalParts(ex.subsidy.dev / 100000000, false, 8, 2)
    this.devFundTarget.innerHTML = humanize.decimalParts(ex.dev_fund / 100000000, true, 0)
    this.poolValueTarget.innerHTML = humanize.decimalParts(ex.pool_info.value, true, 0)
    this.stakePctTarget.innerHTML = `(${parseFloat(ex.pool_info.percent).toFixed(2)}% of total supply)`
    this.posSubsidyTarget.innerHTML = humanize.decimalParts((ex.subsidy.pos / 500000000), false, 8, 2)
    this.ticketRewardTarget.innerHTML = `(${humanize.fmtPercentage(ex.reward)} per ~29.07 days)`
    this.ticketPriceTarget.innerHTML = humanize.decimalParts(ex.sdiff, false, 8, 2)
    this.diffTarget.innerHTML = humanize.decimalParts(ex.difficulty / 1000000, true, 0)
    this.powSubsidyTarget.innerHTML = humanize.decimalParts(ex.subsidy.pow / 100000000, false, 8, 2)
    this.hashrateTarget.innerHTML = humanize.decimalParts(ex.hash_rate, false, 8, 2)
    this.hashrateDeltaTarget.innerHTML = `(${humanize.fmtPercentage(ex.hash_rate_change)} in 24hr)`
  }
}
