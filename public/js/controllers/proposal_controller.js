import { Controller } from 'stimulus'
import { getDefault } from '../helpers/module_helper'

export default class extends Controller {
  static get targets () {
    return ['chartdata']
  }

  async connect () {
    this.setChartData()

    let Chart = await getDefault(
      import('../vendor/charts.min.js')
    )

    this.chart = new Chart(
      document.getElementById('donutgraph').getContext('2d'), {
        options: {
          width: 300,
          height: 300,
          responsive: true,
          animation: { animateScale: true },
          legend: { position: 'bottom' },
          title: {
            display: true,
            text: 'Proposal Vote Results'
          },
          tooltips: {
            callbacks: {
              label: (tooltipItem, data) => {
                var sum = 0
                var currentValue = data.datasets[tooltipItem.datasetIndex].data[tooltipItem.index]
                this.graphData.map((u) => { sum += u })
                return currentValue + ' Votes ( ' + ((currentValue / sum) * 100).toFixed(2) + '% )'
              }
            }
          }
        },
        type: 'doughnut',
        data: {
          labels: this.graphLabels,
          datasets: [{
            data: this.graphData,
            label: 'Yes',
            backgroundColor: this.graphColors,
            borderColor: ['grey', 'grey'],
            borderWidth: 0.5
          }]
        }
      }
    )
  }

  setChartData () {
    this.graphLabels = []
    this.graphData = []
    this.graphColors = []

    this.graphLabels.push(this.chartdataTarget.dataset.firstvotesid)
    this.graphLabels.push(this.chartdataTarget.dataset.secondvotesid)

    this.graphData.push(parseInt(this.chartdataTarget.dataset.firstvotescount))
    this.graphData.push(parseInt(this.chartdataTarget.dataset.secondvotescount))

    this.graphColors.push(this.chartColor(this.chartdataTarget.dataset.firstvotesid))
    this.graphColors.push(this.chartColor(this.chartdataTarget.dataset.secondvotesid))
  }

  chartColor (id) {
    if (id === 'Yes') {
      return '#2DD8A3' // green
    } else if (id === 'No') {
      return '#ED6D47' // red
    } else {
      return '#2970FF' // blue
    }
  }
}
