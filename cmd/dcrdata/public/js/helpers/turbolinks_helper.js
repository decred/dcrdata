/* global Turbolinks */
import Url from 'url-parse'

export default class TurboQuery {
  constructor (turbolinks) {
    const tq = this
    tq.replaceTimer = 0
    tq.appendTimer = 0
    tq.turbolinks = turbolinks || Turbolinks || false
    if (!tq.turbolinks || !tq.turbolinks.supported) {
      console.error('No passed or global Turbolinks instance detected. TurboQuery requires Turbolinks.')
      return
    }
    // These are timer callbacks. Bind them to the TurboQuery instance.
    tq.replaceHistory = tq._replaceHistory.bind(tq)
    tq.appendHistory = tq._appendHistory.bind(tq)
    tq.url = Url(window.location.href, true)
  }

  replaceHref () {
    // Rerouting through timer to prevent spamming.
    // Turbolinks blocks replacement if frequency too high.
    if (this.replaceTimer === 0) {
      this.replaceTimer = setTimeout(this.replaceHistory, 250)
    }
  }

  toHref () {
    if (this.appendTimer === 0) {
      this.appendTimer = setTimeout(this.appendHistory, 250)
    }
  }

  _replaceHistory () {
    // see https://github.com/turbolinks/turbolinks/issues/219. This also works:
    // window.history.replaceState(window.history.state, this.addr, this.url.href)
    this.turbolinks.controller.replaceHistoryWithLocationAndRestorationIdentifier(this.turbolinks.Location.wrap(this.url.href), this.turbolinks.uuid())
    this.replaceTimer = 0
  }

  _appendHistory () {
    // same as replaceHref, but creates a new entry in history for navigating
    // with the browsers forward and back buttons. May still not work because of
    // TurboLinks caching behavior, I think.
    this.turbolinks.controller.pushHistoryWithLocationAndRestorationIdentifier(this.turbolinks.Location.wrap(this.url.href), this.turbolinks.uuid())
    this.appendTimer = 0
  }

  replace (query) {
    this.url.set('query', this.filteredQuery(query))
    this.replaceHref()
  }

  to (query) {
    this.url.set('query', this.filteredQuery(query))
    this.toHref()
  }

  filteredQuery (query) {
    const filtered = {}
    Object.keys(query).forEach(function (key) {
      const v = query[key]
      if (typeof v === 'undefined' || v === null) return
      filtered[key] = v
    })
    return filtered
  }

  update (target) {
    // Update projects the current query parameters onto the given template.
    return this.constructor.project(target, this.parsed)
  }

  get parsed () {
    return this.url.query
  }

  // Not an ES5 getter.
  get (key) {
    if (Object.prototype.hasOwnProperty.call(this.url.query, key)) {
      return TurboQuery.parseValue(this.url.query[key])
    }
    return null
  }

  static parseValue (v) {
    switch (v) {
      case 'null':
        return null
      case '':
        return null
      case 'undefined':
        return null
      case 'false':
        return false
      case 'true':
        return true
    }
    if (!isNaN(parseFloat(v)) && isFinite(v)) {
      if (String(v).includes('.')) {
        return parseFloat(v)
      } else {
        return parseInt(v)
      }
    }
    return v
  }

  static project (target, source) {
    // project fills in the properties of the given template, if they exist in
    // the source. Extraneous source properties are not added to the template.
    const keys = Object.keys(target)
    let idx
    for (idx in keys) {
      const k = keys[idx]
      if (Object.prototype.hasOwnProperty.call(source, k)) {
        target[k] = this.parseValue(source[k])
      }
    }
    return target
  }

  static nullTemplate (keys) {
    const d = {}
    keys.forEach((key) => {
      d[key] = null
    })
    return d
  }
}
