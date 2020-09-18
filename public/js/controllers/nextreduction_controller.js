import { Controller } from 'stimulus'
import globalEventBus from '../services/event_bus_service'
import humanize from '../helpers/humanize_helper'
var remainingSeconds, nextReductionBlockHeight

export default class extends Controller {
  static get targets () {
    return [
      'bsubsidyPos', 'nextReductionTimer', 'hashrate', 'hashrateDelta', 'bsubsidyPow', 'powConverted',
      'poolValue', 'ticketReward', 'poolSizePct', 'blockHeight', 'bsubsidyDev', 'convertedDevSub',
      'devFund', 'convertedDev', 'nextRewardReductionBlockHeight', 'remainingBlocksTillReduction'
    ]
  }

  connect () {
    remainingSeconds = parseInt(this.data.get('remainingSeconds'))
    nextReductionBlockHeight = parseInt(this.nextRewardReductionBlockHeightTarget.dataset.nextReductionBlock)
    setInterval(() => {
      this.countDownTimer(remainingSeconds)
    }, 1000)
    this.processBlock = this._processBlock.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
  }
  disconnect () {
    globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
  }
  setAllValues (targets, data) {
    targets.forEach((n) => { n.innerHTML = data })
  }
  _processBlock (blockData) {
    var ex = blockData.extra
    this.bsubsidyPowTarget.innerHTML = humanize.decimalParts(ex.subsidy.pow / 100000000, false, 8, 2)
    this.bsubsidyPosTarget.innerHTML = humanize.decimalParts((ex.subsidy.pos / 500000000), false, 8, 2) // 5 votes per block (usually)
    this.bsubsidyDevTarget.innerHTML = humanize.decimalParts(ex.subsidy.dev / 100000000, false, 8, 2)
    this.poolValueTarget.innerHTML = humanize.decimalParts(ex.pool_info.value, true, 0)
    this.ticketRewardTarget.innerHTML = `${ex.reward.toFixed(2)}%`
    this.poolSizePctTarget.textContent = parseFloat(ex.pool_info.percent).toFixed(2)
    this.setAllValues(this.devFundTargets, humanize.decimalParts(ex.dev_fund / 100000000, true, 0))
    this.hashrateTarget.innerHTML = humanize.decimalParts(ex.hash_rate, false, 8, 2)
    this.hashrateDeltaTarget.innerHTML = humanize.fmtPercentage(ex.hash_rate_change_month)
    let block = blockData.block
    this.blockHeightTarget.textContent = block.height
    this.blockHeightTarget.href = `/block/${block.hash}`
    this.remainingBlocksTillReductionTarget.innerHTML = nextReductionBlockHeight - block.height
    if (ex.exchange_rate) {
      let xcRate = ex.exchange_rate.value
      let btcIndex = ex.exchange_rate.index
      this.powConvertedTarget.textContent = `${humanize.twoDecimals(ex.subsidy.pow / 1e8 * xcRate)} ${btcIndex}`
      this.setAllValues(this.convertedDevTargets, `${humanize.threeSigFigs(ex.dev_fund / 1e8 * xcRate)} ${btcIndex}`)
      this.convertedDevSubTarget.textContent = `${humanize.twoDecimals(ex.subsidy.dev / 1e8 * xcRate)} ${btcIndex}`
    }
  }
  countDownTimer (allsecs) {
    let str = ''
    if (allsecs > 604799) {
      let weeks = allsecs / 604800
      allsecs %= 604800
      str += Math.floor(weeks) + 'w '
    }
    if (allsecs > 86399) {
      let days = allsecs / 86400
      allsecs %= 86400
      str += Math.floor(days) + 'd '
    }
    if (allsecs > 3599) {
      let hours = allsecs / 3600
      allsecs %= 3600
      str += Math.floor(hours) + 'h '
    }
    if (allsecs > 59) {
      let mins = allsecs / 60
      allsecs %= 60
      str += Math.floor(mins) + 'm '
    }
    if (allsecs >= 0) {
      str += Math.floor(allsecs) + 's '
    }
    this.nextReductionTimerTarget.innerHTML = str
    remainingSeconds--
  }
}
