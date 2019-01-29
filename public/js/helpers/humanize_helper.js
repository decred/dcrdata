// For all your value formatting needs...

function logn (n, b) {
  return Math.log(n) / Math.log(b)
}
function round (value, precision) {
  var multiplier = Math.pow(10, precision || 0)
  return Math.round(value * multiplier) / multiplier
}

var humanize = {
  fmtPercentage: function (val) {
    var sign = '+'
    var cssClass = 'text-success'
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
  }
}

export default humanize
