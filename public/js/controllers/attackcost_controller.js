import { Controller } from 'stimulus'
import TurboQuery from '../helpers/turbolinks_helper'

function currencyFormat (amount, decimalPlaces) {
  return '$ ' + digitformat(amount, decimalPlaces)
}

function digitformat (amount, decimalPlaces) {
  decimalPlaces = decimalPlaces || 0
  return amount.toLocaleString(undefined, { maximumFractionDigits: decimalPlaces })
}

var height, dcrPrice, hashrate, tpSize, tpValue, tpPrice

const deviceList = {
  '0': {
    hashrate: 34, // Th/s
    power: 1610, // W
    cost: 1282, // $
    name: 'DCR5s'
  },
  '1': {
    hashrate: 44, // Th/s
    power: 2200, // W
    cost: 4199, // $
    name: 'D1s'
  }
}

export default class extends Controller {
  static get targets () {
    return [
      'actualHashRate', 'targetPow', 'device', 'targetPos', 'targetPosStr', 'targetHashRate',
      'timeStrLong', 'timeStrShort', 'attackPeriod', 'kwhRate', 'ticketPoolSize',
      'ticketAttackSize', 'price', 'ticketPrice', 'total', 'deviceName', 'deviceCost',
      'devicePower', 'totalKwh', 'totalKwhStr', 'totalElectricity', 'totalDeviceCost',
      'totalPow', 'totalPos', 'ticketSizeAttach', 'blockHeight', 'totalDCRPos', 'otherCosts',
      'TotalFree', 'countDevice', 'kwhRateTwo', 'deviceNameTwo', 'ticketPoolValue',
      'ticketPoolAttack', 'ticketPriceTwo', 'ticketPoolValueTwo', 'ticketPriceExtend',
      'tickets', 'internalhash', 'ticketpool', 'external', 'internal', 'typeAttack', 'priceDCR'
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
    if (this.settings.price) this.priceTarget.value = parseFloat(this.settings.price)
    if (this.settings.device) this.setDevice(this.settings.device)
    if (this.settings.attack_type) this.setAttackType(this.settings.attack_type)

    this.updateSliderData()
    this.updateAttackType()
  }

  updateAttackTime () {
    this.settings.attack_time = this.attackPeriodTarget.value
    this.calculate()
  }

  updateTargetPow () {
    this.settings.target_pow = this.targetPowTargetTarget.value
    this.calculate()
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
    this.settings.price = this.priceTarget.value
    dcrPrice = this.priceTarget.value
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

  setDevice (selectedVal) { return this.setOption(this.deviceTargets, selectedVal) }

  setAttackType (selectedVal) { return this.setOption(this.typeAttackTargets, selectedVal) }

  setOption (options, selectedVal) {
    options.map((n) => { n.selected = n.value === selectedVal })
  }

  updateSliderData () {
    // equation to determine hashpower requirement based on percentage of live stake:
    // (6 (1-f_s)⁵ -15(1-f_s) + 10(1-f_s)³) / (6f_s⁵-15f_s⁴ + 10f_s³)
    // (6x⁵-15x⁴ +10x³) / (6y⁵-15y⁴ +10y³) where y = this.value and x = 1-y
    var y = this.ticketsTarget.value
    var x = 1 - y
    this.targetHashRate = hashrate * this.targetPowTarget.value / 100

    var rate = ((6 * Math.pow(x, 5)) - (15 * Math.pow(x, 4)) + (10 * Math.pow(x, 3))) / ((6 * Math.pow(y, 5)) - (15 * Math.pow(y, 4)) + (10 * Math.pow(y, 3)))
    this.internalhashTarget.innerHTML = digitformat((rate * this.targetHashRate), 4) + ' Ph/s '
    this.ticketpoolTarget.innerHTML = digitformat((y * tpSize), 0) + ' tickets '
    this.calculate()
  }

  calculate () {
    // Update the URL first
    this.query.replace(this.settings)
    var deviceInfo = deviceList[this.selectedDevice()]
    var deviceCount = Math.ceil((this.targetHashRate * 1000) / deviceInfo.hashrate)
    var totalDeviceCost = deviceCount * deviceInfo.cost
    var totalKwh = deviceCount * deviceInfo.power * this.attackPeriodTarget.value / 1000
    var totalElectricity = totalKwh * this.kwhRateTarget.value
    var totalFree = 1 + this.otherCostsTarget.value / 100
    var totalPow = totalDeviceCost * totalFree + totalElectricity
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
    var total = totalPow + totalPos
    var timeStr = this.attackPeriodTarget.value
    timeStr = this.attackPeriodTarget.value > 1 ? timeStr + ' hours' : timeStr + ' hour'

    this.actualHashRateTarget.innerHTML = digitformat(hashrate, 4)
    this.priceTarget.value = digitformat(dcrPrice, 2)
    this.priceDCRTarget.innerHTML = digitformat(dcrPrice, 2)
    this.setAllValues(this.ticketPriceTargets, digitformat(ticketPrice, 3))
    this.setAllValues(this.targetHashRateTargets, digitformat(this.targetHashRate, 3))
    this.setAllValues(this.timeStrLongTargets, timeStr)
    this.timeStrShortTarget.innerHTML = this.attackPeriodTarget.value + 'h'
    this.countDeviceTarget.innerHTML = digitformat(deviceCount)
    this.deviceNameTarget.innerHTML = deviceInfo.name
    this.deviceNameTwoTarget.innerHTML = digitformat(deviceCount) + ' ' + deviceInfo.name
    this.devicePowerTarget.innerHTML = digitformat(deviceInfo.power)
    this.TotalFreeTarget.innerHTML = digitformat(totalFree)
    this.setAllValues(this.totalDeviceCostTargets, currencyFormat(totalDeviceCost))
    this.totalKwhTarget.innerHTML = digitformat(totalKwh, 2)
    this.totalKwhStrTarget.innerHTML = digitformat(totalKwh, 2) + ' kWh'
    this.kwhRateTwoTarget.innerHTML = digitformat(parseFloat(this.kwhRateTarget.value), 2)
    this.setAllValues(this.totalElectricityTargets, currencyFormat(totalElectricity, 2))
    this.setAllValues(this.totalPowTargets, currencyFormat(totalPow, 2))
    this.targetPosStrTarget.innerHTML = this.targetPosTarget.value + '%'
    this.setAllValues(this.ticketSizeAttachTargets, digitformat(ticketAttackSize))
    this.setAllValues(this.totalDCRPosTargets, digitformat(totalDCRPos, 2))
    this.setAllValues(this.totalPosTargets, currencyFormat(totalPos, 2))
    this.setAllValues(this.ticketPoolValueTargets, digitformat(tpValue))
    this.setAllValues(this.ticketPoolSizeTargets, digitformat(tpSize))
    this.blockHeightTarget.innerHTML = digitformat(height)
    this.totalTarget.innerHTML = currencyFormat(total, 2)
  }

  setAllValues (targets, data) {
    targets.map((n) => { n.innerHTML = data })
  }
}
