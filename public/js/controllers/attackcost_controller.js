import { Controller } from 'stimulus'
import TurboQuery from '../helpers/turbolinks_helper'
import { getDefault } from '../helpers/module_helper'
import globalEventBus from '../services/event_bus_service'
import dompurify from 'dompurify'
import axios from 'axios'

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
var height, currentDcrPrice, dcrPrice, btcPrice, marketAvgDcrPrice, totalObUnits, totalObCost, hashrate, tpSize, tpValue, tpPrice, graphData, currentPoint, coinSupply

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

const externalAttackType = 'external'
const internalAttackType = 'internal'

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
      'kwhRate', 'kwhRateLabel', 'otherCosts', 'otherCostsValue', 'priceDCR', 'priceDCRWrapper',
      'internalAttackText', 'targetHashRate', 'externalAttackText',
      'externalAttackPosText', 'additionalDcr', 'newTicketPoolValue', 'internalAttackPosText',
      'additionalHashRate', 'newHashRate', 'targetPos', 'targetPow',
      'ticketAttackSize', 'ticketPoolAttack', 'ticketPoolSize', 'ticketPoolSizeLabel',
      'ticketPoolValue', 'ticketPrice', 'tickets', 'ticketSizeAttack', 'durationLongDesc',
      'total', 'totalDCRPos', 'totalDeviceCost', 'totalElectricity', 'totalExtraCostRate', 'totalKwh',
      'totalPos', 'totalPow', 'graph', 'labels', 'projectedTicketPrice', 'projectedTicketPriceIncrease', 'attackType',
      'marketVolume', 'marketValue', 'marketAvgDcrPrice', 'priceType', 'projectedDcrPriceDiv', 'projectedDcrPrice',
      'projectedDcrPriceIncrease', 'dcrPriceIncrease', 'acquiredDcrCost', 'acquiredDcrValue', 'lowOrderBookWarning',
      'attackPosPercentAmountLabel', 'dcrPriceLabel', 'totalDCRPosLabel', 'projectedPriceDiv', 'attackNotPossibleWrapperDiv',
      'coinSupply', 'totalAttackCostContainer', 'predictedTooltip', 'priceTypeCurrent'
    ]
  }

  async connect () {
    this.query = new TurboQuery()
    this.settings = TurboQuery.nullTemplate([
      'attack_time', 'target_pow', 'kwh_rate', 'other_costs', 'target_pos', 'device', 'attack_type', 'price_type'
    ])

    // Get initial view settings from the url
    this.query.update(this.settings)

    height = parseInt(this.data.get('height'))
    hashrate = parseInt(this.data.get('hashrate'))
    currentDcrPrice = parseFloat(this.data.get('dcrprice'))
    btcPrice = parseFloat(this.data.get('btcprice'))
    tpPrice = parseFloat(this.data.get('ticketPrice'))
    tpValue = parseFloat(this.data.get('ticketPoolValue'))
    tpSize = parseInt(this.data.get('ticketPoolSize'))
    coinSupply = parseInt(this.data.get('coinSupply'))

    this.defaultSettings = {
      attack_time: 1,
      target_pow: 100,
      kwh_rate: 0.1,
      other_costs: 5,
      target_pos: 51,
      device: 0,
      attack_type: externalAttackType,
      price_type: 'current'
    }

    if (this.settings.attack_time) this.attackPeriodTarget.value = parseInt(this.settings.attack_time)
    if (this.settings.target_pow) this.targetPowTarget.value = parseFloat(this.settings.target_pow)
    if (this.settings.kwh_rate) this.kwhRateTarget.value = parseFloat(this.settings.kwh_rate)
    if (this.settings.other_costs) this.otherCostsTarget.value = parseFloat(this.settings.other_cost)
    if (this.settings.target_pos) this.setAllInputs(this.targetPosTargets, parseFloat(this.settings.target_pos))
    if (this.settings.device) this.setDevice(this.settings.device)
    if (this.settings.attack_type) this.attackTypeTarget.value = this.settings.attack_type
    if (this.settings.target_pos) this.attackPercentTarget.value = parseInt(this.targetPosTarget.value) / 100

    if (this.settings.attack_type !== internalAttackType) {
      this.settings.attack_type = externalAttackType
    }
    switch (this.settings.price_type === null) {
      case 'predicted':
        this.hideAll(this.priceDCRWrapperTargets)
        this.showAll(this.projectedDcrPriceDivTargets)
        this.showAll(this.predictedTooltipTargets)
        break
      default:
        this.showAll(this.priceDCRWrapperTargets)
        this.hideAll(this.predictedTooltipTargets)
        this.settings.price_type = 'current'
        break
    }
    switch (this.settings.attack_type) {
      case externalAttackType:
        this.priceTypeTarget.disabled = false
        this.hideAll(this.priceTypeCurrentTargets)
        this.showAll(this.priceTypeTargets)
        break
      default:
        this.settings.price_type = 'current'
        this.priceTypeTarget.value = 'current'
        this.showAll(this.priceTypeCurrentTargets)
        this.hideAll(this.priceTypeTargets)
        this.priceTypeTarget.disabled = true
        break
    }
    this.priceTypeTarget.value = this.settings.price_type
    await this.refreshMarketData()
    this.setDevicesDesc()
    this.updateSliderData()

    Dygraph = await getDefault(
      import(/* webpackChunkName: "dygraphs" */ '../vendor/dygraphs.min.js')
    )

    // dygraph does not provide a way to disable zoom on y-axis https://code.google.com/archive/p/dygraphs/issues/384
    // this is a hack as doZoomY_ is marked as private
    Dygraph.prototype.doZoomY_ = function (lowY, highY) { }

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
    this.ratioTable = new Map()

    // populate graphData
    // to avoid javascript decimal math issue, the iteration is done over whole number and reduced to the expected decimal value within the loop
    for (let i = 10; i <= 1000; i += 5) {
      const y = i / 1000
      const x = rateCalculation(y)
      this.ratioTable.set(x, y)
      graphData.push([y * tpSize, x])
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

  updateQueryString () {
    const [query, settings, defaults] = [{}, this.settings, this.defaultSettings]
    for (const k in settings) {
      if (!settings[k] || settings[k].toString() === defaults[k].toString()) continue
      query[k] = settings[k]
    }
    this.query.replace(query)
  }

  updateAttackTime () {
    this.settings.attack_time = this.attackPeriodTarget.value
    this.updateSliderData()
  }

  updateTargetPow (e) {
    this.preserveTargetPow = true
    var targetPercentage = parseFloat(e.currentTarget.value) / 100
    var target = this.ratioTable.get(targetPercentage)
    if (target === undefined) {
      let previousKey = 0
      let previousValue = 0
      this.ratioTable.forEach((value, key) => {
        if ((previousKey <= targetPercentage && targetPercentage <= key) || (previousKey >= targetPercentage && targetPercentage >= key)) {
          const gap = Math.abs(key - targetPercentage)
          const preGap = Math.abs(previousKey - targetPercentage)
          if (gap < preGap) {
            target = value
          } else {
            target = previousValue
          }
        }
        previousKey = key
        previousValue = value
      })
    }
    this.attackPercentTarget.value = target
    this.updateSliderData()
  }

  chooseDevice () {
    this.settings.device = this.selectedDevice()
    this.updateSliderData()
  }

  chooseAttackType () {
    this.settings.attack_type = this.selectedAttackType()
    switch (this.settings.attack_type) {
      case externalAttackType:
        this.priceTypeTarget.disabled = false
        this.hideAll(this.priceTypeCurrentTargets)
        this.showAll(this.priceTypeTargets)
        break
      default:
        this.settings.price_type = 'current'
        this.priceTypeTarget.value = 'current'
        this.showAll(this.priceTypeCurrentTargets)
        this.hideAll(this.priceTypeTargets)
        this.priceTypeTarget.disabled = true
        break
    }
    if (this.settings.price_type !== 'current') {
      this.hideAll(this.priceDCRWrapperTargets)
    } else {
      this.showAll(this.priceDCRWrapperTargets)
    }
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

  updateTargetPos (e) {
    this.settings.target_pos = e.currentTarget.value
    this.preserveTargetPoS = true
    this.setAllInputs(this.targetPosTargets, e.currentTarget.value)
    this.updateQueryString()
    this.attackPercentTarget.value = parseFloat(this.targetPosTarget.value) / 100
    this.updateSliderData()
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

  setActivePoint () {
    // Shows point whose details appear on the legend.
    if (this.chartsView !== undefined) {
      let val = Math.min(parseFloat(this.attackPercentTarget.value) || 0, 0.99)
      let row = this.chartsView.getRowForX(val * tpSize)
      this.chartsView.setSelection(row)
    }
  }

  updateTargetHashRate () {
    let ticketPercentage = parseFloat(this.targetPosTarget.value)
    this.targetHashRate = hashrate * rateCalculation(ticketPercentage / 100)
    const powPercentage = 100 * this.targetHashRate / hashrate
    if (!this.preserveTargetPow) {
      this.targetPowTarget.value = digitformat(powPercentage, 2, true)
    } else {
      this.preserveTargetPow = false
    }
    this.setAllValues(this.internalHashTargets, digitformat((this.targetHashRate), 4) + ' Ph/s ')
    switch (this.settings.attack_type) {
      case externalAttackType:
        this.setAllValues(this.newHashRateTargets, digitformat(this.targetHashRate + hashrate, 4))
        this.setAllValues(this.additionalHashRateTargets, digitformat(this.targetHashRate, 4))
        this.projectedPriceDivTarget.style.display = 'block'
        if (this.settings.price_type === 'predicted') {
          this.showAll(this.projectedDcrPriceDivTargets)
          this.showAll(this.predictedTooltipTargets)
        }
        this.internalAttackTextTarget.classList.add('d-none')
        this.internalAttackPosTextTarget.classList.add('d-none')
        this.externalAttackTextTarget.classList.remove('d-none')
        this.externalAttackPosTextTarget.classList.remove('d-none')
        break
      case internalAttackType:
      default:
        this.projectedPriceDivTarget.style.display = 'none'
        this.hideAll(this.projectedDcrPriceDivTargets)
        this.hideAll(this.predictedTooltipTargets)
        this.externalAttackTextTarget.classList.add('d-none')
        this.externalAttackPosTextTarget.classList.add('d-none')
        this.internalAttackTextTarget.classList.remove('d-none')
        this.internalAttackPosTextTarget.classList.remove('d-none')
        break
    }
  }

  setPriceType (e) {
    this.settings.price_type = e.currentTarget.value
    if (this.settings.price_type === 'predicted') {
      this.hideAll(this.priceDCRWrapperTargets)
      this.showAll(this.projectedDcrPriceDivTargets)
      this.showAll(this.predictedTooltipTargets)
    } else {
      this.showAll(this.priceDCRWrapperTargets)
      this.hideAll(this.projectedDcrPriceDivTargets)
      this.hideAll(this.predictedTooltipTargets)
    }
    this.calculate()
  }

  async refreshMarketData () {
    const orderDeptUrl = '/api/chart/market/aggregated/depth'
    let response = await axios.get(orderDeptUrl)
    const aggMarket = response.data
    let totalVolume = 0
    let totalCost = 0
    const orderCount = aggMarket.data.asks.length
    for (let i = 0; i <= 0.95 * orderCount; i++) {
      let ask = aggMarket.data.asks[i]
      if (ask === undefined) continue
      if (Array.isArray(ask.volumes)) {
        let volume = ask.volumes.reduce((a, b) => { return a + b }, 0)
        totalVolume += volume
        totalCost += (volume * ask.price)
      }
    }
    totalObCost = totalCost
    totalObUnits = totalVolume
    marketAvgDcrPrice = totalCost / totalVolume
  }

  calcAcquiredDcrCost (currentDcrPrice, averageIncreaseValue, buyVolume) {
    let commutator = 0
    for (let v = 1; v <= buyVolume; v++) {
      commutator += currentDcrPrice + (averageIncreaseValue * v)
    }
    return commutator
  }

  updateSliderData () {
    var val = Math.min(parseFloat(this.attackPercentTarget.value) || 0, 0.99)
    // Makes PoS to be affected by the slider
    // Target PoS value increases when slider moves to the right
    if (!this.preserveTargetPoS) {
      this.setAllInputs(this.targetPosTargets, val * 100)
    } else {
      this.preserveTargetPoS = false
    }

    this.updateTargetHashRate()
    this.setActivePoint()

    this.ticketsTarget.innerHTML = digitformat(val * tpSize) + ' tickets '
    switch (this.settings.attack_type) {
      case externalAttackType:
        this.hideAll(this.internalAttackPosTextTargets)
        this.showAll(this.externalAttackPosTextTargets)
        break
      case internalAttackType:
      default:
        this.hideAll(this.externalAttackPosTextTargets)
        this.showAll(this.internalAttackPosTextTargets)
    }

    this.calculate(true)
  }

  async calculate (disableHashRateUpdate) {
    if (!disableHashRateUpdate) this.updateTargetHashRate()

    this.updateQueryString()
    var deviceInfo = deviceList[this.selectedDevice()]
    var deviceCount = Math.ceil((this.targetHashRate * 1000) / deviceInfo.hashrate)
    var totalDeviceCost = deviceCount * deviceInfo.cost
    var totalKwh = deviceCount * deviceInfo.power * parseFloat(this.attackPeriodTarget.value) / 1000
    var totalElectricity = totalKwh * parseFloat(this.kwhRateTarget.value)
    var extraCost = parseFloat(this.otherCostsTarget.value) / 100 * (totalDeviceCost + totalElectricity)
    var totalPow = extraCost + totalDeviceCost + totalElectricity
    var ticketAttackSize, DCRNeed
    if (this.settings.attack_type === externalAttackType) {
      DCRNeed = tpValue / (1 - parseFloat(this.targetPosTarget.value) / 100)
      this.setAllValues(this.newTicketPoolValueTargets, digitformat(DCRNeed, 2))
      this.setAllValues(this.additionalDcrTargets, digitformat(DCRNeed - tpValue, 2))
    } else {
      ticketAttackSize = (tpSize * parseFloat(this.targetPosTarget.value)) / 100
      DCRNeed = tpValue * (parseFloat(this.targetPosTarget.value) / 100)
      this.setAllValues(this.ticketPoolAttackTargets, digitformat(DCRNeed))
    }
    var projectedTicketPrice = DCRNeed / tpSize
    this.projectedTicketPriceIncreaseTarget.innerHTML = digitformat(100 * (projectedTicketPrice - tpPrice) / tpPrice, 2)
    this.ticketPoolValueTarget.innerHTML = digitformat(hashrate, 3)

    var totalDCRPos = this.settings.attack_type === externalAttackType
      ? DCRNeed - tpValue : ticketAttackSize * projectedTicketPrice
    var totalPos = totalDCRPos * currentDcrPrice

    if (totalDCRPos > totalObUnits) {
      this.showAll(this.lowOrderBookWarningTargets)
    } else {
      this.hideAll(this.lowOrderBookWarningTargets)
    }

    // Since the nature of the markets order book is
    // compounding, based on the available data, the cumulative annual growth
    // rate(CAGR) model is used to measure the growth rate of the market.
    // https://en.wikipedia.org/wiki/Compound_annual_growth_rate

    // Although, CAGR is usually used in business and investment, it also has a
    // reputation in prediction (forecasting future values).
    // Here, CAGR is used to get the slope between the current price and the
    // cumulative market price after purchasing the entire order book (95% to remove outliers).
    // The slope is then used to calculate the estimated cumulative price of the total DCR needed for the attack.
    //
    // increaseRate = (Ravg / Rspot)^(1/(Vask - 1)) - 1
    // Ravg = Volume-averaged rate of aggregated asks
    // Rspot = Mid market rate, the currentDcrPrice
    // Vask = Total volume of aggregated asks, the totalObUnits
    let increaseRate = Math.pow(((totalObCost / totalObUnits) * btcPrice) / currentDcrPrice, 1 / (totalObUnits - 1)) - 1
    const averageIncreaseValue = increaseRate * currentDcrPrice
    let projectedDcrPrice = currentDcrPrice + (averageIncreaseValue * totalDCRPos)
    const acquiredDcrCost = this.calcAcquiredDcrCost(currentDcrPrice, averageIncreaseValue, totalDCRPos)

    const increasePercentage = (projectedDcrPrice - currentDcrPrice) / currentDcrPrice
    this.projectedDcrPriceIncreaseTarget.innerHTML = digitformat(increasePercentage, 0)
    this.setAllValues(this.projectedDcrPriceTargets, digitformat(projectedDcrPrice, 0))
    this.setAllValues(this.marketVolumeTargets, digitformat(totalObUnits, 2))
    this.setAllValues(this.dcrPriceIncreaseTargets, digitformat(increaseRate, 10))
    this.setAllValues(this.marketAvgDcrPriceTargets, digitformat(marketAvgDcrPrice * btcPrice - currentDcrPrice))
    this.setAllValues(this.marketValueTargets, digitformat(totalObCost * btcPrice, 0))
    this.setAllValues(this.acquiredDcrCostTargets, digitformat(acquiredDcrCost, 0))
    this.setAllValues(this.acquiredDcrValueTargets, digitformat(totalDCRPos * projectedDcrPrice, 0))

    if (this.settings.price_type === 'predicted' && this.settings.attack_type === externalAttackType) {
      dcrPrice = projectedDcrPrice
      totalPos = acquiredDcrCost
    } else {
      dcrPrice = currentDcrPrice
    }

    var timeStr = this.attackPeriodTarget.value
    timeStr = this.attackPeriodTarget.value > 1 ? timeStr + ' hours' : timeStr + ' hour'
    this.ticketPoolSizeLabelTarget.innerHTML = digitformat(tpSize, 2)
    this.setAllValues(this.actualHashRateTargets, digitformat(hashrate, 4))
    this.priceDCRTarget.textContent = digitformat(currentDcrPrice, 2)
    this.setAllInputs(this.targetPosTargets, digitformat(parseFloat(this.targetPosTarget.value), 2))
    this.ticketPriceTarget.innerHTML = digitformat(tpPrice, 4)
    this.setAllValues(this.targetHashRateTargets, digitformat(this.targetHashRate, 4))
    this.setAllValues(this.additionalHashRateTargets, digitformat(this.targetHashRate, 4))
    this.setAllValues(this.durationLongDescTargets, timeStr)
    this.setAllValues(this.countDeviceTargets, digitformat(deviceCount))
    this.setAllValues(this.deviceNameTargets, `<a href="${deviceInfo.link}">${deviceInfo.name}</a>s`)
    this.setAllValues(this.totalDeviceCostTargets, digitformat(totalDeviceCost))
    this.setAllValues(this.totalKwhTargets, digitformat(totalKwh, 2))
    this.setAllValues(this.totalElectricityTargets, digitformat(totalElectricity, 2))
    this.setAllValues(this.otherCostsValueTargets, digitformat(extraCost, 2))
    this.setAllValues(this.totalPowTargets, digitformat(totalPow, 2))
    this.setAllValues(this.ticketSizeAttackTargets, digitformat(ticketAttackSize))
    this.setAllValues(this.totalDCRPosTargets, digitformat(totalDCRPos, 2))
    this.setAllValues(this.totalPosTargets, digitformat(totalPos))
    this.setAllValues(this.ticketPoolValueTargets, digitformat(tpValue))
    this.setAllValues(this.ticketPoolSizeTargets, digitformat(tpSize))
    this.blockHeightTarget.innerHTML = digitformat(height)
    this.totalTarget.innerHTML = digitformat(totalPow + totalPos, 2)
    this.projectedTicketPriceTarget.innerHTML = digitformat(projectedTicketPrice, 2)
    // this.attackPosPercentNeededLabelTarget.innerHTML = digitformat(this.targetPosTarget.value, 2)
    this.attackPosPercentAmountLabelTarget.innerHTML = digitformat(this.targetPosTarget.value, 2)
    this.setAllValues(this.totalDCRPosLabelTargets, digitformat(totalDCRPos, 2))
    this.setAllValues(this.dcrPriceLabelTargets, digitformat(dcrPrice, 2))
    this.showPosCostWarning(DCRNeed)
  }

  setAllValues (targets, data) {
    targets.forEach((n) => { n.innerHTML = data })
  }

  setAllInputs (targets, data) {
    targets.forEach((n) => { n.value = data })
  }

  hideAll (targets) {
    targets.forEach(el => el.classList.add('d-none'))
  }

  showAll (targets) {
    targets.forEach(el => el.classList.remove('d-none'))
  }

  showPosCostWarning (DCRNeed) {
    var totalDCRInCirculation = coinSupply / 100000000
    if (DCRNeed > totalDCRInCirculation) {
      this.coinSupplyTarget.textContent = digitformat(totalDCRInCirculation, 2)
      this.totalAttackCostContainerTarget.style.cssText = 'color: #f12222 !important'
      this.showAll(this.attackNotPossibleWrapperDivTargets)
    } else {
      this.totalAttackCostContainerTarget.style.cssText = 'color: #6c757d !important'
      this.hideAll(this.attackNotPossibleWrapperDivTargets)
    }
  }
}
