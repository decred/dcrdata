/* global Turbolinks */
import { Controller } from 'stimulus'
import Url from 'url-parse'

export default class extends Controller {
  static get targets () {
    return ['pagesize', 'votestatus']
  }

  setPageSize () {
    let url = Url(window.location.href)
    let q = Url.qs.parse(url.query)
    q['offset'] = this.pagesizeTarget.dataset.offset
    q['rows'] = this.pagesizeTarget.selectedOptions[0].value
    url.set('query', q)
    Turbolinks.visit(url.toString())
  }

  setFilterbyVoteStatus () {
    let url = Url(window.location.href)
    let q = {}
    q['byvotestatus'] = this.votestatusTarget.selectedOptions[0].value
    url.set('query', q)
    Turbolinks.visit(url.toString())
  }
}
