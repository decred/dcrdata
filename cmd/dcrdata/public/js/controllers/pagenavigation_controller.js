import { Controller } from 'stimulus'
import Url from 'url-parse'
import * as Turbo from '@hotwired/turbo'

export default class extends Controller {
  static get targets () {
    return ['pagesize', 'votestatus', 'listview']
  }

  setPageSize () {
    const url = Url(window.location.href)
    const q = Url.qs.parse(url.query)
    delete q.offset
    delete q.height
    q[this.pagesizeTarget.dataset.offsetkey] = this.pagesizeTarget.dataset.offset
    q.rows = this.pagesizeTarget.selectedOptions[0].value
    if (this.hasVotestatusTarget) {
      q.byvotestatus = this.votestatusTarget.selectedOptions[0].value
    }
    url.set('query', q)
    Turbo.visit(url.toString())
  }

  setFilterbyVoteStatus () {
    const url = Url(window.location.href)
    const q = {}
    q.byvotestatus = this.votestatusTarget.selectedOptions[0].value
    url.set('query', q)
    Turbo.visit(url.toString())
  }

  setListView () {
    const url = Url(window.location.href, true)
    const newPeriod = this.listviewTarget.selectedOptions[0].value
    if (url.pathname !== newPeriod) {
      url.set('query', {})
    }
    url.set('pathname', newPeriod)
    Turbo.visit(url.href)
  }
}
