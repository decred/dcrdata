/* global Turbolinks */
import { Controller } from 'stimulus'
import UrlParse from 'url-parse'

export default class extends Controller {
  static get targets () {
    return ['pagesize', 'votestatus']
  }

  setPageSize () {
    Turbolinks.visit(
      window.location.pathname + '?offset=' + this.pagesizeTarget.dataset.offset +
      '&rows=' + this.pagesizeTarget.selectedOptions[0].value
    )
  }

  setFilterbyVoteStatus () {
    let url = UrlParse(window.location.href)
    url.query.byvotestatus = this.votestatusTarget.selectedOptions[0].value
    Turbolinks.visit(window.location.href)
  }
}
