// For all your value formatting needs...

function logn (n, b) {
  return Math.log(n) / Math.log(b)
}
function round (value, precision) {
  const multiplier = Math.pow(10, precision || 0)
  return Math.round(value * multiplier) / multiplier
}

function hashParts (hash) {
  const clipLen = 6
  const hashLen = hash.length - clipLen
  if (hashLen < 1) {
    return ['', hash]
  }
  return [hash.substring(0, hashLen), hash.substring(hashLen)]
}

const humanize = {
  fmtPercentage: function (val) {
    let sign = '+'
    let cssClass = 'text-green'
    if (val < 1) {
      sign = ''
      cssClass = 'text-danger'
    }
    sign = sign + val.toFixed(2)
    return `<span class="${cssClass}">${sign} % </span>`
  },
  decimalParts: function (v, useCommas, precision, lgDecimals) {
    if (isNaN(precision) || precision > 8) {
      precision = 8
    }
    const formattedVal = parseFloat(v).toFixed(precision)
    const chunks = formattedVal.split('.')
    const int = useCommas ? parseInt(chunks[0]).toLocaleString() : chunks[0]
    const decimal = (chunks[1] || '')
    let i = decimal.length
    let numTrailingZeros = 0
    while (i--) {
      if (decimal[i] === '0') {
        numTrailingZeros++
      } else {
        break
      }
    }
    const decimalVals = decimal.slice(0, decimal.length - numTrailingZeros)
    const trailingZeros = (numTrailingZeros === 0) ? '' : decimal.slice(-(numTrailingZeros))

    let htmlString = '<div class="decimal-parts d-inline-block">'

    if (!isNaN(lgDecimals) && lgDecimals > 0) {
      htmlString += `<span class="int">${int}.${decimalVals.substring(0, lgDecimals)}</span>` +
      `<span class="decimal">${decimalVals.substring(lgDecimals, decimalVals.length)}</span>` +
      `<span class="decimal trailing-zeroes">${trailingZeros}</span>`
    } else if (precision !== 0) {
      htmlString += `<span class="int">${int}</span>` +
      '<span class="decimal dot">.</span>' +
      `<span class="decimal">${decimalVals}</span>` +
      `<span class="decimal trailing-zeroes">${trailingZeros}</span>`
    } else {
      htmlString += `<span class="int">${int}</span>`
    }

    htmlString += '</div>'

    return htmlString
  },
  threeSigFigs: function (v) {
    const sign = v >= 0 ? '' : '-'
    v = Math.abs(v)
    if (v === 0) return '0'
    if (v >= 1e11) return `${sign}${Math.round(v / 1e9)}B`
    if (v >= 1e10) return `${sign}${(v / 1e9).toFixed(1)}B`
    if (v >= 1e9) return `${sign}${(v / 1e9).toFixed(2)}B`
    if (v >= 1e8) return `${sign}${Math.round(v / 1e6)}M`
    if (v >= 1e7) return `${sign}${(v / 1e6).toFixed(1)}M`
    if (v >= 1e6) return `${sign}${(v / 1e6).toFixed(2)}M`
    if (v >= 1e5) return `${sign}${Math.round(v / 1e3)}k`
    if (v >= 1e4) return `${sign}${(v / 1e3).toFixed(1)}k`
    if (v >= 1e3) return `${sign}${(v / 1e3).toFixed(2)}k`
    if (v >= 1e2) return `${sign}${Math.round(v)}`
    if (v >= 10) return `${sign}${v.toFixed(1)}`
    if (v >= 1) return `${sign}${v.toFixed(2)}`
    if (v >= 1e-1) return `${sign}${v.toFixed(3)}`
    if (v >= 1e-2) return `${sign}${v.toFixed(4)}`
    if (v >= 1e-3) return `${sign}${v.toFixed(5)}`
    if (v >= 1e-4) return `${sign}${v.toFixed(6)}`
    if (v >= 1e-5) return `${sign}${v.toFixed(7)}`
    return `${sign}${v.toFixed(8)}`
  },
  twoDecimals: function (v) {
    if (v === 0.0) return '0.00'
    if (Math.abs(v) < 1.0) return parseFloat(v).toPrecision(3)
    return parseFloat(v).toFixed(2)
  },
  subsidyToString: function (x, y = 1) {
    return (x / 100000000 / y) + ' DCR'
  },
  bytes: function (s) { // from go-humanize
    const sizes = ['B', 'kB', 'MB', 'GB', 'TB', 'PB', 'EB']
    if (s < 10) {
      return s + 'B'
    }
    const e = Math.floor(logn(s, 1000))
    const suffix = sizes[e]
    const val = Math.floor(s / Math.pow(1000, e) * 10 + 0.5) / 10
    const precision = (val < 10) ? 1 : 0
    return round(val, precision) + ' ' + suffix
  },
  timeSince: function (unixTime, keepOnly) {
    const seconds = Math.floor(((new Date().getTime() / 1000) - unixTime))
    let interval = Math.floor(seconds / 31536000)
    if (interval >= 1) {
      const extra = Math.floor((seconds - interval * 31536000) / 2628000)
      let result = interval + 'y'
      if (extra > 0 && keepOnly !== 'years') {
        result = result + ' ' + extra + 'mo'
      }
      return result
    }
    interval = Math.floor(seconds / 2628000)
    if (interval >= 1) {
      const extra = Math.floor((seconds - interval * 2628000) / 86400)
      let result = interval + 'mo'
      if (extra > 0 && keepOnly !== 'months') {
        result = result + ' ' + extra + 'd'
      }
      return result
    }
    interval = Math.floor(seconds / 86400)
    if (interval >= 1) {
      const extra = Math.floor((seconds - interval * 86400) / 3600)
      let result = interval + 'd'
      if (extra > 0 && keepOnly !== 'days') {
        result = result + ' ' + extra + 'h'
      }
      return result
    }
    const pad = function (x) {
      return x.toString().padStart(2, '\u00a0')
    }
    interval = Math.floor(seconds / 3600)
    if (interval >= 1) {
      const extra = Math.floor((seconds - interval * 3600) / 60)
      let result = interval + 'h'
      if (extra > 0) {
        result = result + ' ' + pad(extra) + 'm'
      }
      return result
    }
    interval = Math.floor(seconds / 60)
    if (interval >= 1) {
      const extra = seconds - interval * 60
      let result = pad(interval) + 'm'
      if (extra > 0) {
        result = result + ' ' + pad(extra) + 's'
      }
      return result
    }
    return pad(Math.floor(seconds)) + 's'
  },
  date: function (stamp, withTimezone, hideHisForMidnight) {
    const d = new Date(stamp)
    let dateStr = `${String(d.getUTCFullYear())}-${String(d.getUTCMonth() + 1).padStart(2, '0')}-${String(d.getUTCDate()).padStart(2, '0')}`
    if (!hideHisForMidnight) {
      dateStr += ` ${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}:${String(d.getUTCSeconds()).padStart(2, '0')}`
    }
    if (withTimezone) dateStr += ' (UTC)'
    return dateStr
  },
  hashElide: function (hash, link, asNode) {
    const div = document.createElement(link ? 'a' : 'div')
    if (link) div.href = link
    div.dataset.keynavePriority = 1
    div.classList.add('elidedhash')
    div.classList.add('mono')
    const [head, tail] = hashParts(hash)
    div.dataset.head = head
    div.dataset.tail = tail
    div.textContent = hash
    if (asNode) {
      return div
    }
    return div.outerHTML
  },
  capitalize: function (s) {
    if (typeof s !== 'string') return ''
    return s.charAt(0).toUpperCase() + s.slice(1)
  }
}

export default humanize
