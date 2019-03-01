/* global Turbolinks */
import { Controller } from 'stimulus'
import Url from 'url-parse'

export default class extends Controller {
  static get targets () {
    return ['pagesize', 'votestatus', 'listview']
  }

  setPageSize () {
    var url = Url(window.location.href)
    var q = Url.qs.parse(url.query)
    delete q.offset
    delete q.height
    q[this.pagesizeTarget.dataset.offsetkey] = this.pagesizeTarget.dataset.offset
    q['rows'] = this.pagesizeTarget.selectedOptions[0].value
    if (this.hasVotestatusTarget) {
      q['byvotestatus'] = this.votestatusTarget.selectedOptions[0].value
    }
    url.set('query', q)
    Turbolinks.visit(url.toString())
  }

  setFilterbyVoteStatus () {
    var url = Url(window.location.href)
    var q = {}
    q['byvotestatus'] = this.votestatusTarget.selectedOptions[0].value
    url.set('query', q)
    Turbolinks.visit(url.toString())
  }

  setListView () {
    var url = Url(window.location.href, true)
    var newPeriod = this.listviewTarget.selectedOptions[0].value
    if (url.pathname !== newPeriod) {
      url.set('query', {})
    }
    url.set('pathname', newPeriod)
    Turbolinks.visit(url.href)
  }
}
