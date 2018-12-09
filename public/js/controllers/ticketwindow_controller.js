/* global Turbolinks */
import { Controller } from 'stimulus'

export default class extends Controller {
  static get targets () {
    return ['pagesize']
  }

  setPageSize () {
    Turbolinks.visit(
      window.location.pathname + '?offset=' + this.pagesizeTarget.dataset.offset +
      '&rows=' + this.pagesizeTarget.selectedOptions[0].value
    )
  }
}
