// For all your value formatting needs...

function logn (n, b) {
  return Math.log(n) / Math.log(b)
}
function round (value, precision) {
  var multiplier = Math.pow(10, precision || 0)
  return Math.round(value * multiplier) / multiplier
}

function hashParts (hash) {
  var clipLen = 6
  var hashLen = hash.length - clipLen
  if (hashLen < 1) {
    return ['', hash]
  }
  return [hash.substring(0, hashLen), hash.substring(hashLen)]
}

// Polyfill for IE
Math.log10 = Math.log10 || function (x) {
  return Math.log(x) * Math.LOG10E
}

function threeSigFigs (v, billions) {
  var sign = v >= 0 ? '' : '-'
  var tsf = (val, pre) => { return { value: `${sign}${val}`, prefix: pre || '' } }
  v = Math.abs(v)
  if (v === 0) {
    return tsf(0, '')
  }
  var exp = Math.floor(Math.log10(v))
  if (exp > 17) return tsf(Math.round(v / 1e15), 'E') // exa
  let p
  switch (Math.floor(exp / 3)) {
    case 5:
      p = 'P' // peta
      switch (exp) {
        case 17: return tsf(Math.round(v / 1e15), p)
        case 16: return tsf((v / 1e15).toFixed(1), p)
        case 15: return tsf((v / 1e15).toFixed(2), p)
      }
      break
    case 4:
      p = 'T' // tera
      switch (exp) {
        case 14: return tsf(Math.round(v / 1e12), p)
        case 13: return tsf((v / 1e12).toFixed(1), p)
        case 12: return tsf((v / 1e12).toFixed(2), p)
      }
      break
    case 3:
      switch (exp) {
        case 11: return tsf(Math.round(v / 1e9), billions)
        case 10: return tsf((v / 1e9).toFixed(1), billions)
        case 9: return tsf((v / 1e9).toFixed(2), billions)
      }
      break
    case 2:
      p = 'M' // mega
      switch (exp) {
        case 8: return tsf(Math.round(v / 1e6), p)
        case 7: return tsf((v / 1e6).toFixed(1), p)
        case 6: return tsf((v / 1e6).toFixed(2), p)
      }
      break
    case 1:
      p = 'k' // kilo
      switch (exp) {
        case 5: return tsf(Math.round(v / 1e3), p)
        case 4: return tsf((v / 1e3).toFixed(1), p)
        case 3: return tsf((v / 1e3).toFixed(2), p)
      }
      break
    case 0:
      switch (exp) {
        case 2: return tsf(Math.round(v))
        case 1: return tsf(v.toFixed(1))
        case 0: return tsf(v.toFixed(2))
      }
      break
    default:
      switch (exp) {
        case -1: return tsf(v.toFixed(3))
        case -2: return tsf(v.toFixed(4))
        case -3: return tsf(v.toFixed(5))
        case -4: return tsf(v.toFixed(6))
        case -5: return tsf(v.toFixed(7))
        default: return tsf(v.toFixed(8))
      }
  }
}

var humanize = {
  fmtPercent: function (val) {
    var sign = val <= 0 ? '-' : '+'
    return sign + val.toFixed(2)
  },
  decimalParts: function (v, useCommas, precision, lgDecimals) {
    if (isNaN(precision) || precision > 8) {
      precision = 8
    }
    var formattedVal = parseFloat(v).toFixed(precision)
    var chunks = formattedVal.split('.')
    var int = useCommas ? parseInt(chunks[0]).toLocaleString() : chunks[0]
    var decimal = (chunks[1] || '')
    var i = decimal.length
    var numTrailingZeros = 0
    while (i--) {
      if (decimal[i] === '0') {
        numTrailingZeros++
      } else {
        break
      }
    }
    var decimalVals = decimal.slice(0, decimal.length - numTrailingZeros)
    var trailingZeros = (numTrailingZeros === 0) ? '' : decimal.slice(-(numTrailingZeros))

    var htmlString = '<div class="decimal-parts d-inline-block">'

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
  threeSFV: function (v) {
    // Use for DCR values. returns a single string with unit prefix appended
    // with no space. Uses 'B' for billions.
    var tsf = threeSigFigs(v, 'B')
    return tsf.value + tsf.prefix
  },
  threeSFG: function (v) {
    // Returns the threeSigFig object with 'G' for billions
    return threeSigFigs(v, 'G')
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
    var sizes = ['B', 'kB', 'MB', 'GB', 'TB', 'PB', 'EB']
    if (s < 10) {
      return s + 'B'
    }
    var e = Math.floor(logn(s, 1000))
    var suffix = sizes[e]
    var val = Math.floor(s / Math.pow(1000, e) * 10 + 0.5) / 10
    var precision = (val < 10) ? 1 : 0
    return round(val, precision) + ' ' + suffix
  },
  timeSince: function (unixTime, keepOnly) {
    var seconds = Math.floor(((new Date().getTime() / 1000) - unixTime))
    var interval = Math.floor(seconds / 31536000)
    if (interval >= 1) {
      let extra = Math.floor((seconds - interval * 31536000) / 2628000)
      let result = interval + 'y'
      if (extra > 0 && keepOnly !== 'years') {
        result = result + ' ' + extra + 'mo'
      }
      return result
    }
    interval = Math.floor(seconds / 2628000)
    if (interval >= 1) {
      let extra = Math.floor((seconds - interval * 2628000) / 86400)
      let result = interval + 'mo'
      if (extra > 0 && keepOnly !== 'months') {
        result = result + ' ' + extra + 'd'
      }
      return result
    }
    interval = Math.floor(seconds / 86400)
    if (interval >= 1) {
      let extra = Math.floor((seconds - interval * 86400) / 3600)
      let result = interval + 'd'
      if (extra > 0 && keepOnly !== 'days') {
        result = result + ' ' + extra + 'h'
      }
      return result
    }
    interval = Math.floor(seconds / 3600)
    if (interval >= 1) {
      let extra = Math.floor((seconds - interval * 3600) / 60)
      let result = interval + 'h'
      if (extra > 0) {
        result = result + ' ' + extra + 'm'
      }
      return result
    }
    interval = Math.floor(seconds / 60)
    if (interval >= 1) {
      let extra = seconds - interval * 60
      let result = interval + 'm'
      if (extra > 0) {
        result = result + ' ' + extra + 's'
      }
      return result
    }
    return Math.floor(seconds) + 's'
  },
  date: function (stamp, withTimezone) {
    var d = new Date(stamp)
    return `${String(d.getUTCFullYear())}-${String(d.getUTCMonth() + 1).padStart(2, '0')}-${String(d.getUTCDate()).padStart(2, '0')} ` +
          `${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}:${String(d.getUTCSeconds()).padStart(2, '0')} (UTC)`
  },
  hashElide: function (hash, link, asNode) {
    var div = document.createElement(link ? 'a' : 'div')
    if (link) div.href = link
    div.dataset.keynavePriority = 1
    div.classList.add('elidedhash')
    div.classList.add('mono')
    var head, tail
    [head, tail] = hashParts(hash)
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
