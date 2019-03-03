import { Controller } from 'stimulus'
import globalEventBus from '../services/event_bus_service'
import humanize from '../helpers/humanize_helper'

export default class extends Controller {
  static get targets () {
    return ['table']
  }

  connect () {
    this.processBlock = this._processBlock.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
    this.pageOffset = this.data.get('initialOffset')
  }

  disconnect () {
    globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
  }

  _processBlock (blockData) {
    if (!this.hasTableTarget) return
    var block = blockData.block
    // Grab a copy of the first row.
    var rows = this.tableTarget.querySelectorAll('tr')
    if (rows.length === 0) return
    var tr = rows[0]
    var lastHeight = parseInt(tr.dataset.height)
    // Make sure this block belongs on the top of this table.
    if (block.height === lastHeight) {
      this.tableTarget.removeChild(tr)
    } else if (block.height === lastHeight + 1) {
      this.tableTarget.removeChild(rows[rows.length - 1])
    } else return
    // Set the td contents based on the order of the existing row.
    var newRow = document.createElement('tr')
    newRow.dataset.height = block.height
    newRow.dataset.linkClass = tr.dataset.linkClass
    var tds = tr.querySelectorAll('td')
    tds.forEach((td) => {
      let newTd = document.createElement('td')
      newTd.className = td.className
      let dataType = td.dataset.type
      newTd.dataset.type = dataType
      switch (dataType) {
        case 'age':
          newTd.dataset.age = block.unixStamp
          newTd.dataset.target = 'time.age'
          newTd.textContent = humanize.timeSince(block.unixStamp)
          break
        case 'height':
          let link = document.createElement('a')
          link.href = `/block/${block.height}`
          link.textContent = block.height
          link.classList.add(tr.dataset.linkClass)
          newTd.appendChild(link)
          break
        case 'size':
          newTd.textContent = humanize.bytes(block.size)
          break
        case 'value':
          newTd.textContent = humanize.threeSigFigs(block.TotalSent)
          break
        case 'time':
          newTd.textContent = humanize.date(block.time, true)
          break
        default:
          newTd.textContent = block[dataType]
      }
      newRow.appendChild(newTd)
    })
    this.tableTarget.insertBefore(newRow, this.tableTarget.firstChild)
  }
}
