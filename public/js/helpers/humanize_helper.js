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

var humanize = {
  fmtPercentage: function (val) {
    var sign = '+'
    var cssClass = 'text-green'
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
  threeSigFigs: function (v) {
    if (v >= 1e11) return `${Math.round(v / 1e9)}B`
    if (v >= 1e10) return `${(v / 1e9).toFixed(1)}B`
    if (v >= 1e9) return `${(v / 1e9).toFixed(2)}B`
    if (v >= 1e8) return `${Math.round(v / 1e6)}M`
    if (v >= 1e7) return `${(v / 1e6).toFixed(1)}M`
    if (v >= 1e6) return `${(v / 1e6).toFixed(2)}M`
    if (v >= 1e5) return `${Math.round(v / 1e3)}k`
    if (v >= 1e4) return `${(v / 1e3).toFixed(1)}k`
    if (v >= 1e3) return `${(v / 1e3).toFixed(2)}k`
    if (v >= 1e2) return `${Math.round(v)}`
    if (v >= 10) return `${v.toFixed(1)}`
    if (v >= 1) return `${v.toFixed(2)}`
    if (v >= 1e-1) return `${v.toFixed(3)}`
    if (v >= 1e-2) return `${v.toFixed(4)}`
    if (v >= 1e-3) return `${v.toFixed(5)}`
    if (v >= 1e-4) return `${v.toFixed(6)}`
    if (v >= 1e-5) return `${v.toFixed(7)}`
    if (v === 0) return '0'
    console.log(`tiny v = ${v}`)
    return v.toFixed(8)
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
  formatTxDate: function (stamp, withTimezone) {
    var d = new Date(stamp)
    var zone = withTimezone ? '(' + d.toLocaleTimeString('en-us', { timeZoneName: 'short' }).split(' ')[2] + ') ' : ''
    return zone + String(d.getFullYear()) + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0') + ' ' +
          String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0') + ':' + String(d.getSeconds()).padStart(2, '0')
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
  }
}

export default humanize
