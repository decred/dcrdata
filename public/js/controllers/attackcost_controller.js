import { Controller } from 'stimulus'
import TurboQuery from '../helpers/turbolinks_helper'
import { getDefault } from '../helpers/module_helper'
import globalEventBus from '../services/event_bus_service'
import dompurify from 'dompurify'

function digitformat (amount, decimalPlaces, noComma) {
  if (!amount) return 0

  if (noComma) return amount.toFixed(decimalPlaces)

  decimalPlaces = decimalPlaces || 0
  let result = parseFloat(amount).toLocaleString(undefined, { minimumFractionDigits: decimalPlaces, maximumFractionDigits: decimalPlaces }).replace(/\.0*$/, '')
  if (result.indexOf('.') > -1 && result.endsWith('0')) {
    return removeTrailingZeros(result)
  }

  return result
}

function removeTrailingZeros (value) {
  value = value.toString()
  if (value.indexOf('.') === -1) {
    return value
  }

  let cutFrom = value.length - 1
  do {
    if (value[cutFrom] === '0') {
      cutFrom--
    }
  } while (value[cutFrom] === '0')

  if (value[cutFrom] === '.') {
    cutFrom--
  }

  return value.substr(0, cutFrom + 1)
}

let Dygraph // lazy loaded on connect
var height, dcrPrice, hashrate, tpSize, tpValue, tpPrice, graphData, currentPoint

function rateCalculation (y) {
  y = y || 0.99 // 0.99 TODO confirm why 0.99 is used as default instead of 1
  let x = 1 - y

  // equation to determine hashpower requirement based on percentage of live stake
  // https://medium.com/decred/decreds-hybrid-protocol-a-superior-deterrent-to-majority-attacks-9421bf486292
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
      return `${nodes} <span>${series.labelHTML}</span>`
    }, '')
    html = `<span>${this.getLabels()[0]}: N/A</span>${dashLabels}`
  } else {
    let yVals = data.series.reduce((nodes, series) => {
      if (!series.isVisible) return nodes
      let precession = series.y >= 1 ? 2 : 6
      return `${nodes} <span class="ml-3">${series.labelHTML}:</span> &nbsp;${digitformat(series.y, precession)}x`
    }, '<br>')

    html = `<span>${this.getLabels()[0]}: ${digitformat(data.x, 1)}</span>${yVals}`
  }
  dompurify.sanitize(html)
  return html
}

function nightModeOptions (nightModeOn) {
  if (nightModeOn) {
    return {
      rangeSelectorAlpha: 0.3,
      gridLineColor: '#596D81',
      colors: ['#2DD8A3', '#2970FF', '#FFC84E']
    }
  }
  return {
    rangeSelectorAlpha: 0.4,
    gridLineColor: '#C4CBD2',
    colors: ['#2970FF', '#006600', '#FF0090']
  }
}

export default class extends Controller {
  static get targets () {
    return [
      'actualHashRate', 'attackPercent', 'attackPeriod', 'blockHeight', 'countDevice', 'device',
      'deviceCost', 'deviceDesc', 'deviceName', 'external', 'internal', 'internalHash',
      'kwhRate', 'kwhRateLabel', 'otherCosts', 'priceDCR', 'internalAttackText', 'targetHashRate', 'externalAttackText',
      'externalAttackPosText', 'additionalTickets', 'internalAttackPosText',
      'additionalHashRate', 'targetPos', 'targetPow', 'operatorSign',
      'ticketAttackSize', 'ticketPoolAttack', 'ticketPoolSize', 'ticketPoolSizeLabel',
      'ticketPoolValue', 'ticketPrice', 'tickets', 'ticketSizeAttack', 'durationLongDesc',
      'total', 'totalDCRPos', 'totalDeviceCost', 'totalElectricity', 'totalExtraCostRate', 'totalKwh',
      'totalPos', 'totalPow', 'graph', 'labels', 'projectedTicketPrice', 'attackType',
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

    // dygraph does not provide a way to disable zoom on y-axis https://code.google.com/archive/p/dygraphs/issues/384
    // this is a hack as doZoomY_ is marked as private
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
    const that = this
    graphData = []

    // populate graphData
    // to avoid javascript decimal math issue, the iteration is done over whole number and reduced to the expected decimal value within the loop
    for (let i = 10; i <= 1000; i += 5) {
      const y = i / 1000
      graphData.push([y * tpSize, rateCalculation(y)])
    }

    let options = {
      ...nightModeOptions(false),
      labels: ['Attackers Tickets', 'Hash Power Multiplier'],
      ylabel: 'Hash Power Multiplier',
      xlabel: 'Attackers Tickets',
      axes: {
        y: {
          axisLabelWidth: 70
        }
      },
      highlightSeriesOpts: { strokeWidth: 2 },
      legendFormatter: legendFormatter,
      hideOverlayOnMouseOut: false,
      labelsDiv: this.labelsTarget,
      labelsSeparateLines: true,
      showRangeSelector: false,
      labelsKMB: true,
      legend: 'always',
      logscale: true,
      interactionModel: {
        'click': function (e) {
          that.attackPercentTarget.value = currentPoint.x
          that.updateSliderData()
        }
      },
      highlightCallback: function (event, x, p) {
        currentPoint = p[0]
      }
    }

    this.chartsView = new Dygraph(this.graphTarget, graphData, options)
    this.chartsView.setAnnotations([{
      series: 'Hashpower multiplier',
      x: 0.51,
      shortText: 'L',
      text: '51% Attack'
    }])
    this.setActivePoint()
  }

  updateAttackTime () {
    this.settings.attack_time = this.attackPeriodTarget.value
    this.updateSliderData()
  }

  updateTargetPow () {
    // todo use the inverse of (6x⁵-15x⁴ +10x³) / (6y⁵-15y⁴ +10y³) where y = this.value and x = 1-y to get the
    //  appropriate multiplier for the selected attack percentage
    // this.updateSliderData()
  }

  chooseDevice () {
    this.settings.device = this.selectedDevice()
    this.updateSliderData()
  }

  chooseAttackType () {
    this.settings.attack_type = this.selectedAttackType()
    this.updateSliderData()
  }

  updateKwhRate () {
    this.settings.kwh_rate = this.kwhRateTarget.value
    this.updateSliderData()
  }

  updateOtherCosts () {
    this.settings.other_costs = this.otherCostsTarget.value
    this.updateSliderData()
  }

  updateTargetPos () {
    this.settings.target_pos = this.targetPosTarget.value
    this.attackPercentTarget.value = parseFloat(this.targetPosTarget.value) / 100
    this.updateSliderData()
  }

  updatePrice () {
    this.settings.price = this.priceDCRTarget.value
    dcrPrice = this.priceDCRTarget.value
    this.updateSliderData()
  }

  selectedDevice () { return this.deviceTarget.value }

  selectedAttackType () {
    // TurboQuery parses the attack_type from the url as int
    return parseInt(this.attackTypeTarget.value)
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

  setActivePoint () {
    // Shows point whose details appear on the legend.
    if (this.chartsView !== undefined) {
      let val = Math.min(parseFloat(this.attackPercentTarget.value) || 0, 0.99)
      let row = this.chartsView.getRowForX(val * tpSize)
      this.chartsView.setSelection(row)
    }
  }

  updateTargetHashRate (newTargetPow) {
    this.targetPowTarget.value = newTargetPow || this.targetPowTarget.value

    switch (this.settings.attack_type) {
      case 1:
        this.targetHashRate = hashrate / (1 - parseFloat(this.targetPowTarget.value) / 100) - hashrate
        this.projectedPriceDivTarget.style.display = 'block'
        this.internalAttackTextTarget.classList.add('d-none')
        this.internalAttackPosTextTarget.classList.add('d-none')
        this.externalAttackTextTarget.classList.remove('d-none')
        this.externalAttackPosTextTarget.classList.remove('d-node')
        console.log(this.externalAttackPosTextTarget)
        return
      case 0:
      default:
        this.targetHashRate = hashrate * parseFloat(this.targetPowTarget.value) / 100
        this.projectedPriceDivTarget.style.display = 'none'
        this.externalAttackTextTarget.classList.add('d-none')
        this.externalAttackPosTextTarget.classList.add('d-node')
        this.internalAttackTextTarget.classList.remove('d-none')
        this.internalAttackPosTextTarget.classList.remove('d-none')
    }
  }

  updateSliderData () {
    var val = Math.min(parseFloat(this.attackPercentTarget.value) || 0, 0.99)
    // Makes PoS to be affected by the slider
    // Target PoS value increases when slider moves to the right
    this.targetPosTarget.value = val * 100

    this.updateTargetHashRate(val * 100)
    this.setActivePoint()

    var rate = rateCalculation(val)
    var randomVal = (rate * this.targetHashRate) / parseFloat(hashrate) * 100

    this.targetPowTarget.value = Math.floor(randomVal * 100) / 100
    this.internalHashTarget.innerHTML = digitformat((rate * this.targetHashRate), 4) + ' Ph/s '
    this.ticketsTarget.innerHTML = digitformat(val * tpSize) + ' tickets '
    switch (this.settings.attack_type) {
      case 1:
        this.hideAll(this.internalAttackPosTextTargets)
        this.showAll(this.externalAttackPosTextTargets)
        break
      case 0:
      default:
        this.hideAll(this.externalAttackPosTextTargets)
        this.showAll(this.internalAttackPosTextTargets)
    }

    this.calculate(true)
  }

  calculate (disableHashRateUpdate) {
    if (!disableHashRateUpdate) this.updateTargetHashRate()

    this.targetHashRate *= rateCalculation(this.targetPosTarget.value / 100)
    this.query.replace(this.settings)
    var deviceInfo = deviceList[this.selectedDevice()]
    var deviceCount = Math.ceil((this.targetHashRate * 1000) / deviceInfo.hashrate)
    var totalDeviceCost = deviceCount * deviceInfo.cost
    var totalKwh = deviceCount * deviceInfo.power * parseFloat(this.attackPeriodTarget.value) / 1000
    var totalElectricity = totalKwh * parseFloat(this.kwhRateTarget.value)
    var extraCostsRate = 1 + parseFloat(this.otherCostsTarget.value) / 100
    var totalPow = extraCostsRate * totalDeviceCost + totalElectricity
    var ticketAttackSize, DCRNeed
    if (this.settings.attack_type === 1) {
      ticketAttackSize = tpSize / (1 - parseFloat(this.targetPosTarget.value) / 100)
      DCRNeed = tpValue / (1 - parseFloat(this.targetPosTarget.value) / 100)
      this.setAllValues(this.additionalTicketsTargets, digitformat(ticketAttackSize - tpSize, 0))
      // this.setAllValues(this.newTicketPoolValueTargets, digitformat(ticketAttackSize, 0))
      this.setAllValues(this.operatorSignTargets, '+')
    } else {
      ticketAttackSize = (tpSize * parseFloat(this.targetPosTarget.value)) / 100
      DCRNeed = tpValue * (parseFloat(this.targetPosTarget.value) / 100)
      this.setAllValues(this.operatorSignTargets, '*')
    }
    var projectedTicketPrice = DCRNeed / tpSize
    this.setAllValues(this.ticketPoolAttackTargets, digitformat(DCRNeed))
    this.ticketPoolValueTarget.innerHTML = digitformat(hashrate, 3)

    var totalDCRPos = ticketAttackSize * projectedTicketPrice
    var totalPos = totalDCRPos * dcrPrice
    var timeStr = this.attackPeriodTarget.value
    timeStr = this.attackPeriodTarget.value > 1 ? timeStr + ' hours' : timeStr + ' hour'
    this.ticketPoolSizeLabelTarget.innerHTML = digitformat(tpSize, 2)

    this.setAllValues(this.actualHashRateTargets, digitformat(hashrate, 4))
    this.priceDCRTarget.value = digitformat(dcrPrice, 2)
    this.targetPowTarget.value = digitformat(parseFloat(this.targetPowTarget.value), 2, true)
    this.targetPosTarget.value = digitformat(parseFloat(this.targetPosTarget.value), 2)
    this.ticketPriceTarget.innerHTML = digitformat(tpPrice, 4)
    this.setAllValues(this.targetHashRateTargets, digitformat(this.targetHashRate, 4))
    this.setAllValues(this.additionalHashRateTargets, digitformat(this.targetHashRate - hashrate, 4))
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
    this.projectedTicketPriceTarget.innerHTML = digitformat(projectedTicketPrice, 2)
    this.attackPosPercentNeededLabelTarget.innerHTML = digitformat(this.targetPosTarget.value, 2)
    this.attackPosPercentAmountLabelTarget.innerHTML = digitformat(this.targetPosTarget.value, 2)
    this.totalDCRPosLabelTarget.innerHTML = digitformat(totalDCRPos, 2)
    this.dcrPriceLabelTarget.innerHTML = digitformat(dcrPrice, 2)
  }

  setAllValues (targets, data) {
    targets.forEach((n) => { n.innerHTML = data })
  }

  hideAll (targets) {
    targets.forEach(el => el.classList.add('d-none'))
  }

  showAll (targets) {
    targets.forEach(el => el.classList.remove('d-none'))
  }
}
