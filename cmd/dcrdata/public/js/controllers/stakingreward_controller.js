import { Controller } from '@hotwired/stimulus'
import TurboQuery from '../helpers/turbolinks_helper'
import { requestJSON } from '../helpers/http'

const responseCache = {}
let requestCounter = 0

function hasCache (k) {
  if (!responseCache[k]) return false
  const expiration = new Date(responseCache[k].expiration)
  return expiration > new Date()
}

export default class extends Controller {
  static get targets () {
    return [
      'blockHeight',
      'startDate', 'endDate',
      'priceDCR', 'dayText', 'amount', 'days', 'daysText',
      'amountRoi', 'percentageRoi',
      'table', 'tableBody', 'rowTemplate', 'amountError', 'startDateErr'
    ]
  }

  async initialize () {
    this.rewardPeriod = parseInt(this.data.get('rewardPeriod'))
    // default, startDate is 3 month ago
    this.last3Months = new Date()
    this.last3Months.setMonth(this.last3Months.getMonth() - 3)
    this.startDateTarget.value = this.formatDateToString(this.last3Months)
    this.endDateTarget.value = this.formatDateToString(new Date())
    this.amountTarget.value = 1000

    this.query = new TurboQuery()
    this.settings = TurboQuery.nullTemplate([
      'amount', 'start', 'end'
    ])

    this.defaultSettings = {
      amount: 1000,
      start: this.startDateTarget.value,
      end: this.endDateTarget.value
    }

    this.query.update(this.settings)
    if (this.settings.amount) {
      this.amountTarget.value = this.settings.amount
    }
    if (this.settings.start) {
      this.startDateTarget.value = this.settings.start
    }
    if (this.settings.end) {
      this.endDateTarget.value = this.settings.end
    }

    this.calculate()
  }

  // Amount input event
  amountKeypress (e) {
    if (e.keyCode === 13) {
      this.amountChanged()
    }
  }

  // convert date to strin display (YYYY-MM-DD)
  formatDateToString (date) {
    return [
      date.getFullYear(),
      ('0' + (date.getMonth() + 1)).slice(-2),
      ('0' + date.getDate()).slice(-2)
    ].join('-')
  }

  updateQueryString () {
    const [query, settings, defaults] = [{}, this.settings, this.defaultSettings]
    for (const k in settings) {
      if (!settings[k] || settings[k].toString() === defaults[k].toString()) continue
      query[k] = settings[k]
    }
    this.query.replace(query)
  }

  // When amount was changed
  amountChanged () {
    this.settings.amount = parseInt(this.amountTarget.value)
    this.calculate()
  }

  // StartDate type event
  startDateKeypress (e) {
    if (e.keyCode !== 13) {
      return
    }
    if (!this.validateDate()) {
      return
    }
    this.startDateChanged()
  }

  // When startDate was changed
  startDateChanged () {
    if (!this.validateDate()) {
      return
    }
    this.settings.start = this.startDateTarget.value
    this.calculate()
  }

  // EndDate type event
  endDateKeypress (e) {
    if (e.keyCode !== 13) {
      return
    }
    if (!this.validateDate()) {
      return
    }
    this.endDateChanged()
  }

  // When EndDate was changed
  endDateChanged () {
    if (!this.validateDate()) {
      return
    }
    this.settings.end = this.endDateTarget.value
    this.calculate()
  }

  // Validate Date range
  validateDate () {
    const startDate = new Date(this.startDateTarget.value)
    const endDate = new Date(this.endDateTarget.value)

    if (startDate > endDate) {
      console.log('Invalid date range')
      this.startDateErrTarget.textContent = 'Invalid date range'
      return
    }
    const days = this.getDaysOfRange(startDate, endDate)
    if (days < this.rewardPeriod) {
      console.log(`You must stake for more than ${this.rewardPeriod} days`)
      this.startDateErrTarget.textContent = `You must stake for more than ${this.rewardPeriod} days`
      return false
    }

    this.startDateErrTarget.textContent = ''
    return true
  }

  hideAll (targets) {
    targets.classList.add('d-none')
  }

  showAll (targets) {
    targets.classList.remove('d-none')
  }

  // Get days between startDate and endDate
  getDaysOfRange (startDate, endDate) {
    const differenceInTime = endDate.getTime() - startDate.getTime()
    return differenceInTime / (1000 * 3600 * 24)
  }

  // Get Date from Days start from startDay
  getDateFromDays (startDate, days) {
    const tmpDate = new Date()
    tmpDate.setFullYear(startDate.getFullYear())
    tmpDate.setMonth(startDate.getMonth())
    tmpDate.setDate(startDate.getDate() + Number(days))
    return this.formatDateToString(tmpDate)
  }

  // Calculate and response
  async calculate () {
    const _this = this
    requestCounter++
    const thisRequest = requestCounter
    const amount = parseFloat(this.amountTarget.value)
    if (!(amount > 0)) {
      console.log('Amount must be greater than 0')
      _this.amountErrorTarget.textContent = 'Amount must be greater than 0'
      return
    }
    _this.amountErrorTarget.textContent = ''

    const startDate = new Date(this.startDateTarget.value)
    const endDate = new Date(this.endDateTarget.value)
    const days = this.getDaysOfRange(startDate, endDate)

    if (days < this.rewardPeriod) {
      console.log(`You must stake for more than ${this.rewardPeriod} days`)
      _this.startDateErrTarget.textContent = `You must stake for more than ${this.rewardPeriod} days`
      return
    }
    _this.startDateErrTarget.textContent = ''
    this.updateQueryString()
    const startDateUnix = startDate.getTime()
    const endDateUnix = endDate.getTime()
    const url = `/api/stakingcalc/get-future-reward?startDate=${startDateUnix}&endDate=${endDateUnix}&startingBalance=${amount}`
    let response
    if (hasCache(url)) {
      response = responseCache[url]
    } else {
      // response = await axios.get(url)
      response = await requestJSON(url)
      responseCache[url] = response
      if (thisRequest !== requestCounter) {
        // new request was issued while waiting.
        this.startDateTarget.classList.remove('loading')
        return
      }
    }
    _this.daysTextTarget.textContent = parseInt(days)

    // number of periods
    const totalPercentage = response.reward
    const totalAmount = totalPercentage * amount * 1 / 100
    _this.percentageRoiTarget.textContent = totalPercentage.toFixed(2)
    _this.amountRoiTarget.textContent = totalAmount.toFixed(2)

    if (!response.simulation_table || response.simulation_table.length === 0) {
      this.hideAll(_this.tableTarget)
    } else {
      this.showAll(_this.tableTarget)
    }

    _this.tableBodyTarget.innerHTML = ''
    response.simulation_table.forEach(item => {
      const exRow = document.importNode(_this.rowTemplateTarget.content, true)
      const fields = exRow.querySelectorAll('td')

      fields[0].innerText = this.getDateFromDays(startDate, item.day)
      fields[1].innerText = item.height
      fields[2].innerText = item.ticket_price.toFixed(2)
      fields[3].innerText = item.returned_fund.toFixed(2)
      fields[4].innerText = item.reward.toFixed(2)
      fields[5].innerText = item.dcr_balance.toFixed(2)
      fields[6].innerText = (100 * (item.dcr_balance - amount) / amount).toFixed(2)
      fields[7].innerText = item.tickets_purchased
      _this.tableBodyTarget.appendChild(exRow)
    })
  }
}
