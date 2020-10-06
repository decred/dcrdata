import { Controller } from 'stimulus'

export default class extends Controller {
  rewardPeriod
  ticketReward

  static get targets () {
    return [
      'amount', 'initialCapital', 'endDate', 'infoBox', 'dateInfo',
      'profit', 'finalAmount', 'reinvestReward'
    ]
  }

  connect () {
    this.rewardPeriod = parseFloat(this.data.get('period'))
    this.ticketReward = parseFloat(this.data.get('reward'))
  }

  calculate () {
    let initialCapital = parseFloat(this.amountTarget.value)
    if (initialCapital <= 0) {
      window.alert('Amount must not be empty and should be greater than zero')
      return
    }
    let now = new Date()
    let endDate = new Date(this.endDateTarget.value)
    let duration = (endDate - now) / 1000
    let numberOfPeriods = (duration - (duration % this.rewardPeriod)) / this.rewardPeriod
    let finalAmount
    if (this.reinvestRewardTarget.checked) {
      finalAmount = initialCapital * Math.pow((1 + this.ticketReward / numberOfPeriods), numberOfPeriods)
    } else {
      finalAmount = initialCapital * (1 + this.ticketReward * numberOfPeriods)
    }
    this.dateInfoTarget.textContent = endDate
    let interest = finalAmount - initialCapital
    this.initialCapitalTarget.textContent = initialCapital
    this.profitTarget.textContent = interest
    this.finalAmountTarget.textContent = finalAmount
    this.infoBoxTarget.classList.remove('d-hide')
  }
}
