/* global Turbolinks */
import { Controller } from 'stimulus'
import Url from 'url-parse'

export default class extends Controller {
  static get targets () {
    return ['pagesize', 'listview']
  }

  connect () {
    var controller = this
    controller.pageOffset = this.data.get('initialOffset')
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
}
