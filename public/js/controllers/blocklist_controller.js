/* global Turbolinks */
import { Controller } from 'stimulus'
import Url from 'url-parse'
import globalEventBus from '../services/event_bus_service'
import humanize from '../helpers/humanize_helper'

export default class extends Controller {
  static get targets () {
    return ['pagesize', 'listview', 'table']
  }

  connect () {
    this.processBlock = this._processBlock.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
    this.pageOffset = this.data.get('initialOffset')
  }

  disconnect () {
    globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
  }

  setPageSize () {
    var controller = this
    var queryData = {}
    queryData[controller.data.get('offsetKey')] = controller.pageOffset
    queryData.rows = controller.pagesizeTarget.selectedOptions[0].value
    var url = Url(window.location.href, true)
    url.set('query', queryData)
    Turbolinks.visit(url.href)
  }

  setListView () {
    var controller = this
    var url = Url(window.location.href)
    url.set('pathname', controller.listviewTarget.selectedOptions[0].value)
    Turbolinks.visit(url.href)
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
      let dataType = td.dataset.type
      newTd.dataset.type = dataType
      switch (dataType) {
        case 'index':
          newTd.textContent = block.windowIndex
          break
        case 'height':
          newTd.textContent = block.Height
          let link = document.createElement('a')
          link.href = `/block/${block.height}`
          link.textContent = block.height
          link.classList.add(tr.dataset.linkClass)
          newTd.appendChild(link)
          break
        case 'txs':
          newTd.textContent = block.tx
          break
        case 'voters':
          newTd.textContent = block.votes
          break
        case 'stake':
          newTd.textContent = block.tickets
          break
        case 'revokes':
          newTd.textContent = block.revocations
          break
        case 'size':
          newTd.textContent = humanize.bytes(block.size)
          break
        case 'age':
          newTd.dataset.age = block.unixStamp
          newTd.dataset.target = 'time.age'
          newTd.textContent = humanize.timeSince(block.unixStamp)
          break
        case 'time':
          newTd.textContent = block.time
          break
        default:
          return
      }
      newRow.appendChild(newTd)
    })
    this.tableTarget.insertBefore(newRow, this.tableTarget.firstChild)
  }
}
