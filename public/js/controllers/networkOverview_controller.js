/* global $ */
import { Controller } from 'stimulus'
import humanize from '../helpers/humanize_helper'
import globalEventBus from '../services/event_bus_service'

export default class extends Controller {
  static get targets () {
    return []
  }

  connect () {
    globalEventBus.on('BLOCK_RECEIVED', this.renderNewBlock)
  }

  disconnect () {
    globalEventBus.off('BLOCK_RECEIVED', this.renderNewBlock)
  }

  renderNewBlock (newBlock) {
    var ex = newBlock.extra
    $('#coin-supply').html(humanize.decimalParts(ex.coin_supply / 100000000, true, 0, false))
    $('#bsubsidy-dev').html(humanize.decimalParts(ex.subsidy.dev / 100000000, false, 8, false, 2))
    $('#dev-fund').html(humanize.decimalParts(ex.dev_fund / 100000000, true, 0, false))
    $('#pool-value').html(humanize.decimalParts(ex.pool_info.value, true, 0, false))
    $('#pool-info-percentage').html('(' + parseFloat(ex.pool_info.percent).toFixed(2) + ' % of total supply)')
    $('#bsubsidy-pos').html(humanize.decimalParts((ex.subsidy.pos / 500000000), false, 8, false, 2))
    $('#ticket-reward').html('(' + humanize.fmtPercentage(ex.reward) + ' per ~29.07 days)')
    $('#ticket-price').html(humanize.decimalParts(ex.sdiff, false, 8, false, 2))
    $('#diff').html(humanize.decimalParts(ex.difficulty / 1000000, true, 0, false))
    $('#bsubsidy-pow').html(humanize.decimalParts(ex.subsidy.pow / 100000000, false, 8, false, 2))
    $('#hashrate').html(humanize.decimalParts(ex.hash_rate, false, 8, false, 2))
    $('#hashrate-subdata').html('(' + humanize.fmtPercentage(ex.hash_rate_change) + ' in 24hr)')
  }
}
