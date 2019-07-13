import { Controller } from 'stimulus'
import TurboQuery from '../helpers/turbolinks_helper'

function digitformat (amount, decimalPlaces) {
  decimalPlaces = decimalPlaces || 0
  return amount.toLocaleString(undefined, { maximumFractionDigits: decimalPlaces })
}

var height, dcrPrice, hashrate, tpSize, tpValue, tpPrice

const deviceList = {
  '0': {
    hashrate: 34, // Th/s
    units: 'Th/s',
    power: 1610, // W
    cost: 1282, // $
    name: 'DCR5',
    link: 'https://www.cryptocompare.com/mining/bitmain/antminer-dr5-blake256r14-34ths/'
  },
  '1': {
    hashrate: 44, // Th/s
    units: 'Th/s',
    power: 2200, // W
    cost: 4199, // $
    name: 'D1',
    link: 'https://www.cryptocompare.com/mining/crypto-drilling/microbt-whatsminer-d1-plus-psu-dcr-44ths/'
  }
}

export default class extends Controller {
  static get targets () {
    return [
      'actualHashRate', 'attackPercent', 'attackPeriod', 'blockHeight', 'countDevice', 'device',
      'deviceCost', 'deviceDesc', 'deviceName', 'devicePower', 'external', 'internal', 'internalHash',
      'kwhRate', 'kwhRateLabel', 'otherCosts', 'priceDCR', 'priceDCRLabel', 'targetHashRate', 'targetPos',
      'targetPosLabel', 'targetPow', 'ticketAttackSize', 'ticketPoolAttack', 'ticketPoolSize',
      'ticketPoolValue', 'ticketPrice', 'tickets', 'ticketSizeAttach', 'durationLongDesc', 'durationShortDesc',
      'total', 'totalDCRPos', 'totalDeviceCost', 'totalElectricity', 'totalExtraCostRate', 'totalKwh',
      'totalPos', 'totalPow', 'typeAttack'
    ]
  }

  initialize () {
    this.query = new TurboQuery()
    this.settings = TurboQuery.nullTemplate([
      'attack_time', 'target_pow', 'kwh_rate', 'other_costs', 'target_pos', 'price', 'device', 'attack_type'
    ])

    height = parseInt(this.data.get('height'))
    hashrate = parseInt(this.data.get('hashrate'))
    dcrPrice = parseFloat(this.data.get('dcrprice'))
    tpPrice = parseFloat(this.data.get('ticketPrice'))
    tpValue = parseFloat(this.data.get('ticketPoolValue'))
    tpSize = parseInt(this.data.get('ticketPoolSize'))

    if (this.settings.attack_time) this.attackPeriodTarget.value = parseInt(this.settings.attack_time)
    if (this.settings.target_pow) this.targetPowTarget.value = parseFloat(this.settings.target_pow)
    if (this.settings.kwh_rate) this.kwhRateTarget.value = parseFloat(this.settings.kwh_rate)
    if (this.settings.other_costs) this.otherCostsTarget.value = parseFloat(this.settings.other_cost)
    if (this.settings.target_pos) this.targetPosTarget.value = parseFloat(this.settings.target_pos)
    if (this.settings.price) this.priceDCRTarget.value = parseFloat(this.settings.price)
    if (this.settings.device) this.setDevice(this.settings.device)
    if (this.settings.attack_type) this.setAttackType(this.settings.attack_type)

    this.setDevicesDesc()
    this.updateAttackType()
    this.updateSliderData()
  }

  updateAttackTime () {
    this.settings.attack_time = this.attackPeriodTarget.value
    this.calculate()
  }

  updateTargetPow () {
    this.settings.target_pow = this.targetPowTargetTarget.value
    this.attackPercentTarget.value = this.targetPowTargetTarget.value / 100

    this.updateSliderData()
  }

  chooseDevice () {
    this.settings.device = this.selectedDevice()
    this.calculate()
  }

  updateKwhRate () {
    this.settings.kwh_rate = this.kwhRateTarget.value
    this.calculate()
  }

  updateOtherCosts () {
    this.settings.other_costs = this.otherCostsTarget.value
    this.calculate()
  }

  updateTargetPos () {
    this.settings.target_pos = this.targetPos.value
    this.calculate()
  }

  updatePrice () {
    this.settings.price = this.priceDCRTarget.value
    dcrPrice = this.priceDCRTarget.value
    this.calculate()
  }

  updateAttackType () {
    if (this.selectedAttackType() === '0') {
      this.externalTarget.classList.add('d-hide')
      this.internalTarget.classList.remove('d-hide')
    } else {
      this.externalTarget.classList.remove('d-hide')
      this.internalTarget.classList.add('d-hide')
    }
  }

  chooseAttackType () {
    this.settings.attack_type = this.selectedAttackType()
    this.updateAttackType()
    this.calculate()
  }

  selectedDevice () { return this.deviceTarget.value }

  selectedAttackType () { return this.typeAttackTarget.value }

  selectOption (options) {
    let val = '0'
    options.map((n) => { if (n.selected) val = n.value })
    return val
  }

  setDevicesDesc () {
    this.deviceDescTargets.map((n) => {
      let info = deviceList[n.value]
      if (!info) return
      n.innerHTML = `${info.name}, ${info.hashrate} ${info.units}, ${info.power}w, $${digitformat(info.cost)} per unit`
    })
  }

  setDevice (selectedVal) { return this.setOption(this.deviceTargets, selectedVal) }

  setAttackType (selectedVal) { return this.setOption(this.typeAttackTargets, selectedVal) }

  setOption (options, selectedVal) {
    options.map((n) => { n.selected = n.value === selectedVal })
  }

  updateTargetHashRate (newTargetPow) {
    this.targetPowTarget.value = newTargetPow || this.targetPowTarget.value
    this.targetHashRate = hashrate * this.targetPowTarget.value / 100
  }

  updateSliderData () {
    // equation to determine hashpower requirement based on percentage of live stake:
    // (6 (1-f_s)⁵ -15(1-f_s) + 10(1-f_s)³) / (6f_s⁵-15f_s⁴ + 10f_s³)
    // (6x⁵-15x⁴ +10x³) / (6y⁵-15y⁴ +10y³) where y = this.value and x = 1-y
    var y = this.attackPercentTarget.value
    var x = 1 - y
    this.updateTargetHashRate(y * 100)

    var rate = ((6 * Math.pow(x, 5)) - (15 * Math.pow(x, 4)) + (10 * Math.pow(x, 3))) / ((6 * Math.pow(y, 5)) - (15 * Math.pow(y, 4)) + (10 * Math.pow(y, 3)))
    this.internalHashTarget.innerHTML = digitformat((rate * this.targetHashRate), 4) + ' Ph/s '
    this.ticketsTarget.innerHTML = digitformat((y * tpSize), 0) + ' tickets '
    this.calculate(true)
  }

  calculate (disableHashRateUpdate) {
    if (!disableHashRateUpdate) this.updateTargetHashRate()

    this.query.replace(this.settings)
    var deviceInfo = deviceList[this.selectedDevice()]
    var deviceCount = Math.ceil((this.targetHashRate * 1000) / deviceInfo.hashrate)
    var totalDeviceCost = deviceCount * deviceInfo.cost
    var totalKwh = deviceCount * deviceInfo.power * this.attackPeriodTarget.value / 1000
    var totalElectricity = totalKwh * this.kwhRateTarget.value
    var extraCostsRate = 1 + this.otherCostsTarget.value / 100
    var totalPow = extraCostsRate * totalDeviceCost + totalElectricity
    var ticketAttackSize = Math.ceil((tpSize * this.targetPosTarget.value) / 100)
    var ticketPrice = tpPrice
    // extend attack
    if (this.selectedAttackType() === '1') {
      var DCRNeed = hashrate / 0.6
      ticketPrice = DCRNeed / tpSize
      this.setAllValues(this.ticketPoolAttackTargets, digitformat(DCRNeed, 3))
      this.ticketPoolValueTarget.innerHTML = digitformat(hashrate, 3)
    }

    var totalDCRPos = ticketAttackSize * ticketPrice
    var totalPos = totalDCRPos * dcrPrice
    var timeStr = this.attackPeriodTarget.value
    timeStr = this.attackPeriodTarget.value > 1 ? timeStr + ' hours' : timeStr + ' hour'

    this.actualHashRateTarget.innerHTML = digitformat(hashrate, 4)
    this.priceDCRTarget.value = digitformat(dcrPrice, 2)
    this.priceDCRLabelTarget.innerHTML = digitformat(dcrPrice, 2)
    this.setAllValues(this.ticketPriceTargets, digitformat(ticketPrice, 4))
    this.setAllValues(this.targetHashRateTargets, digitformat(this.targetHashRate, 4))
    this.setAllValues(this.durationLongDescTargets, timeStr)
    this.durationShortDescTarget.innerHTML = this.attackPeriodTarget.value + 'h'
    this.setAllValues(this.countDeviceTargets, digitformat(deviceCount))
    this.setAllValues(this.deviceNameTargets, `<a href="${deviceInfo.link}">${deviceInfo.name}</a>s`)
    this.devicePowerTarget.innerHTML = digitformat(deviceInfo.power)
    this.totalExtraCostRateTarget.innerHTML = digitformat(extraCostsRate, 4)
    this.setAllValues(this.totalDeviceCostTargets, digitformat(totalDeviceCost))
    this.setAllValues(this.totalKwhTargets, digitformat(totalKwh, 2))
    this.kwhRateLabelTarget.innerHTML = digitformat(parseFloat(this.kwhRateTarget.value), 2)
    this.setAllValues(this.totalElectricityTargets, digitformat(totalElectricity, 2))
    this.setAllValues(this.totalPowTargets, digitformat(totalPow, 2))
    this.targetPosLabelTarget.innerHTML = this.targetPosTarget.value + '%'
    this.setAllValues(this.ticketSizeAttachTargets, digitformat(ticketAttackSize))
    this.setAllValues(this.totalDCRPosTargets, digitformat(totalDCRPos, 2))
    this.setAllValues(this.totalPosTargets, digitformat(totalPos, 2))
    this.setAllValues(this.ticketPoolValueTargets, digitformat(tpValue))
    this.setAllValues(this.ticketPoolSizeTargets, digitformat(tpSize))
    this.blockHeightTarget.innerHTML = digitformat(height)
    this.totalTarget.innerHTML = digitformat(totalPow + totalPos, 2)
  }

  setAllValues (targets, data) {
    targets.map((n) => { n.innerHTML = data })
  }
}
