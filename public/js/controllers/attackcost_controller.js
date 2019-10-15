import { Controller } from 'stimulus'
import TurboQuery from '../helpers/turbolinks_helper'
import { getDefault } from '../helpers/module_helper'
import globalEventBus from '../services/event_bus_service'
import { nightModeOptions } from '../helpers/chart_helper'
import dompurify from 'dompurify'

function digitformat (amount, decimalPlaces) {
  if (!amount) return 0
  decimalPlaces = decimalPlaces || 0
  return parseFloat(amount).toLocaleString(undefined, { minimumFractionDigits: decimalPlaces, maximumFractionDigits: decimalPlaces }).replace(/\.0*$/, '')
}

let Dygraph // lazy loaded on connect
var height, dcrPrice, hashrate, tpSize, tpValue, tpPrice, graphData

function rateCalculation (y) {
  y = y || 0.99
  var x = 1 - y

  // equation to determine hashpower requirement based on percentage of live stake:
  // (6 (1-f_s)⁵ -15(1-f_s) + 10(1-f_s)³) / (6f_s⁵-15f_s⁴ + 10f_s³)
  // (6x⁵-15x⁴ +10x³) / (6y⁵-15y⁴ +10y³) where y = this.value and x = 1-y
  return ((6 * Math.pow(x, 5)) - (15 * Math.pow(x, 4)) + (10 * Math.pow(x, 3))) / ((6 * Math.pow(y, 5)) - (15 * Math.pow(y, 4)) + (10 * Math.pow(y, 3)))
}

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

function legendFormatter (data) {
  var html = ''
  if (data.x == null) {
    let dashLabels = data.series.reduce((nodes, series) => {
      return `${nodes} <span style="color:${series.color};">${series.dashHTML} ${series.labelHTML}</span>`
    }, '')
    html = `<span>${this.getLabels()[0]}: N/A</span>${dashLabels}`
  } else {
    let yVals = data.series.reduce((nodes, series) => {
      if (!series.isVisible) return nodes
      return `${nodes} <span class="ml-3" style="color:${series.color};">${series.dashHTML} ${series.labelHTML}: </span>${digitformat(series.y, 4)}`
    }, '<br>')

    html = `<span>${this.getLabels()[0]}: ${digitformat(data.x)}</span>${yVals}`
  }
  dompurify.sanitize(html)
  return html
}

export default class extends Controller {
  static get targets () {
    return [
      'actualHashRate', 'attackPercent', 'attackPeriod', 'blockHeight', 'countDevice', 'device',
      'deviceCost', 'deviceDesc', 'deviceName', 'external', 'internal', 'internalHash',
      'kwhRate', 'kwhRateLabel', 'otherCosts', 'priceDCR', 'targetHashRate', 'targetPos', 'targetPow',
      'ticketAttackSize', 'ticketPoolAttack', 'ticketPoolSize', 'ticketPoolSizeLabel',
      'ticketPoolValue', 'ticketPrice', 'tickets', 'ticketSizeAttack', 'durationLongDesc',
      'total', 'totalDCRPos', 'totalDeviceCost', 'totalElectricity', 'totalExtraCostRate', 'totalKwh',
      'totalPos', 'totalPow', 'graph', 'labels', 'attackPercentLabel', 'projectedTicketPrice', 'attackType',
      'attackPosPercentNeededLabel', 'attackPosPercentAmountLabel', 'dcrPriceLabel', 'totalDCRPosLabel',
      'projectedPriceDiv'
    ]
  }

  async connect () {
    this.query = new TurboQuery()
    this.settings = TurboQuery.nullTemplate([
      'attack_time', 'target_pow', 'kwh_rate', 'other_costs', 'target_pos', 'price', 'device', 'attack_type'
    ])

    // Get initial view settings from the url
    this.query.update(this.settings)

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
    if (this.settings.attack_type) this.attackTypeTarget.value = parseInt(this.settings.attack_type)
    if (this.settings.target_pos || this.settings.target_pow) this.attackPercentTarget.value = (this.targetPowTarget.value || this.targetPosTarget.value) / 100

    this.setDevicesDesc()
    this.updateSliderData()

    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )

    Dygraph.prototype.doZoomY_ = function (lowY, highY) {}

    this.plotGraph()
    this.processNightMode = (params) => {
      this.chartsView.updateOptions(
        nightModeOptions(params.nightMode)
      )
    }
    globalEventBus.on('NIGHT_MODE', this.processNightMode)
  }

  disconnect () {
    globalEventBus.off('NIGHT_MODE', this.processNightMode)
    if (this.chartsView !== undefined) {
      this.chartsView.destroy()
    }
  }

  plotGraph () {
    graphData = []

    // populate graphData
    for (var i = 0.01; i <= 0.99; i += 0.005) {
      graphData.push([i * tpSize, rateCalculation(i)])
    }

    let options = {
      ...nightModeOptions(false),
      labels: ['Attackers Tickets', 'Hashpower multiplier'],
      ylabel: 'Hash Power Multiplier',
      xlabel: 'Attackers Tickets',
      highlightSeriesOpts: { strokeWidth: 2 },
      legendFormatter: legendFormatter,
      hideOverlayOnMouseOut: false,
      labelsDiv: this.labelsTarget,
      labelsSeparateLines: true,
      showRangeSelector: false,
      labelsKMB: true,
      legend: 'always',
      logscale: true,
      interactionModel: {}
      // clickCallback: this.updateFromChart
    }

    this.chartsView = new Dygraph(this.graphTarget, graphData, options)
    this.chartsView.setAnnotations([{
      series: 'Hashpower multiplier',
      x: 0.51,
      shortText: 'L',
      text: '51% Attack'
    }])
    this.chartsView.updateOptions({
      clickCallback: this.updateFromChart
    })
    this.setActivePoint()
  }

  updateAttackTime () {
    this.settings.attack_time = this.attackPeriodTarget.value
    this.calculate()
  }

  updateTargetPow () {
    this.settings.target_pow = this.targetPowTarget.value
    this.attackPercentTarget.value = parseFloat(this.targetPowTarget.value) / 100

    this.updateSliderData()
  }

  chooseDevice () {
    this.settings.device = this.selectedDevice()
    this.calculate()
  }

  chooseAttackType () {
    this.settings.attack_type = this.selectedAttackType()
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
    this.settings.target_pos = this.targetPosTarget.value
    this.attackPercentTarget.value = parseFloat(this.targetPosTarget.value) / 100
    this.updateSliderData()
  }

  updatePrice () {
    this.settings.price = this.priceDCRTarget.value
    dcrPrice = this.priceDCRTarget.value
    this.calculate()
  }

  selectedDevice () { return this.deviceTarget.value }

  selectedAttackType () {
    return this.attackTypeTarget.value
  }

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

  setOption (options, selectedVal) {
    options.map((n) => { n.selected = n.value === selectedVal })
  }

  updateTargetHashRate (newTargetPow) {
    this.targetPowTarget.value = newTargetPow || this.targetPowTarget.value

    switch (this.settings.attack_type) {
      case '1':
        this.targetHashRate = hashrate / (1 - parseFloat(this.targetPowTarget.value) / 100)
        this.projectedPriceDivTarget.style.display = 'block'
        return
      case '0':
      default:
        this.targetHashRate = hashrate * parseFloat(this.targetPowTarget.value) / 100
        this.projectedPriceDivTarget.style.display = 'none'
    }
  }

  setActivePoint () {
    // Shows point whose details appear on the legend.
    if (this.chartsView !== undefined) {
      let row = Math.round(parseFloat(this.attackPercentTarget.value) / 0.005)
      this.chartsView.setSelection(row)
    }
  }

  updateSliderData () {
    var val = parseFloat(this.attackPercentTarget.value) || 0

    // Makes PoS to be affected by the slider
    // Target PoS value increases when slider moves to the right
    this.targetPosTarget.value = val * 100

    this.updateTargetHashRate(val * 100)
    this.setActivePoint()

    var rate = rateCalculation(val)
    this.internalHashTarget.innerHTML = digitformat((rate * this.targetHashRate), 4) + ' Ph/s '
    this.ticketsTarget.innerHTML = digitformat(val * tpSize) + ' tickets '
    this.calculate(true)
  }

  updateFromChart (e, attackTickets, points) {
    console.log(`Attack tickets: ${attackTickets}`)
    // this.targetPosTarget.innerHTML = parseFloat((attackTickets / tpSize) * 100)
    // console.log(`Target PoS: ${parseFloat((attackTickets / tpSize) * 100)}`)
    // this.calculate()
  }

  calculate (disableHashRateUpdate) {
    if (!disableHashRateUpdate) this.updateTargetHashRate()

    this.settings.target_pow = digitformat(parseFloat(this.targetPowTarget.value), 2)
    // this.settings.target_pos = digitformat(parseFloat(this.targetPosTarget.value), 2)
    this.query.replace(this.settings)
    var deviceInfo = deviceList[this.selectedDevice()]
    var deviceCount = Math.ceil((this.targetHashRate * 1000) / deviceInfo.hashrate)
    var totalDeviceCost = deviceCount * deviceInfo.cost
    var totalKwh = deviceCount * deviceInfo.power * parseFloat(this.attackPeriodTarget.value) / 1000
    var totalElectricity = totalKwh * parseFloat(this.kwhRateTarget.value)
    var extraCostsRate = 1 + parseFloat(this.otherCostsTarget.value) / 100
    var totalPow = extraCostsRate * totalDeviceCost + totalElectricity
    var ticketAttackSize = (tpSize * parseFloat(this.targetPosTarget.value)) / 100
    // var DCRNeed = tpValue / (parseFloat(this.targetPosTarget.value) / 100)
    var DCRNeed = tpValue * (parseFloat(this.targetPosTarget.value) / 100)
    var projectedTicketPrice = DCRNeed / tpSize
    this.setAllValues(this.ticketPoolAttackTargets, digitformat(DCRNeed))
    this.ticketPoolValueTarget.innerHTML = digitformat(hashrate, 3)

    var totalDCRPos = ticketAttackSize * projectedTicketPrice
    var totalPos = totalDCRPos * dcrPrice
    var timeStr = this.attackPeriodTarget.value
    timeStr = this.attackPeriodTarget.value > 1 ? timeStr + ' hours' : timeStr + ' hour'
    this.ticketPoolSizeLabelTarget.innerHTML = digitformat(tpSize, 2)

    this.actualHashRateTarget.innerHTML = digitformat(hashrate, 4)
    this.priceDCRTarget.value = digitformat(dcrPrice, 2)
    this.targetPowTarget.value = digitformat(parseFloat(this.targetPowTarget.value), 2)
    this.targetPosTarget.value = digitformat(parseFloat(this.targetPosTarget.value), 2)
    this.ticketPriceTarget.innerHTML = digitformat(tpPrice, 4)
    this.setAllValues(this.targetHashRateTargets, digitformat(this.targetHashRate, 4))
    this.setAllValues(this.durationLongDescTargets, timeStr)
    this.setAllValues(this.countDeviceTargets, digitformat(deviceCount))
    this.setAllValues(this.deviceNameTargets, `<a href="${deviceInfo.link}">${deviceInfo.name}</a>s`)
    this.setAllValues(this.totalDeviceCostTargets, digitformat(totalDeviceCost))
    this.setAllValues(this.totalKwhTargets, digitformat(totalKwh, 2))
    this.setAllValues(this.totalElectricityTargets, digitformat(totalElectricity, 2))
    this.setAllValues(this.totalPowTargets, digitformat(totalPow, 2))
    this.setAllValues(this.ticketSizeAttackTargets, digitformat(ticketAttackSize))
    this.setAllValues(this.totalDCRPosTargets, digitformat(totalDCRPos, 2))
    this.setAllValues(this.totalPosTargets, digitformat(totalPos))
    this.setAllValues(this.ticketPoolValueTargets, digitformat(tpValue))
    this.setAllValues(this.ticketPoolSizeTargets, digitformat(tpSize))
    this.blockHeightTarget.innerHTML = digitformat(height)
    this.totalTarget.innerHTML = digitformat(totalPow + totalPos, 2)
    this.attackPercentLabelTarget.innerHTML = digitformat(this.targetPowTarget.value, 2)
    this.projectedTicketPriceTarget.innerHTML = digitformat(projectedTicketPrice, 2)
    this.attackPosPercentNeededLabelTarget.innerHTML = digitformat(this.targetPosTarget.value, 2)
    this.attackPosPercentAmountLabelTarget.innerHTML = digitformat(this.targetPosTarget.value, 2)
    this.totalDCRPosLabelTarget.innerHTML = digitformat(totalDCRPos, 2)
    this.dcrPriceLabelTarget.innerHTML = digitformat(dcrPrice, 2)
  }

  setAllValues (targets, data) {
    targets.map((n) => { n.innerHTML = data })
  }
}
