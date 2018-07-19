// For all your client side value formatting needs...
var humanize = (function() {
  function logn(n, b) {
    return Math.log(n) / Math.log(b)
  }
  function round(value, precision) {
    var multiplier = Math.pow(10, precision || 0)
    return Math.round(value * multiplier) / multiplier
  }
  return {
    decimalParts: function(v, useCommas, precision) {
      if (!precision || precision > 8) {
        precision = 8
      }
      var formattedVal = parseFloat(v).toFixed(precision)
      var chunks = formattedVal.split(".")
      var int = useCommas ? parseInt(chunks[0]).toLocaleString() : chunks[0]
      var decimal = (chunks[1] || "")
      var i = decimal.length
      var numTrailingZeros = 0
      while (i--) {
        if (decimal[i] == "0") {
          numTrailingZeros++
        } else {
          break
        }
      }
      var decimalVals = decimal.slice(0,decimal.length - numTrailingZeros)
      var trailingZeros = (numTrailingZeros == 0) ? "" : decimal.slice(-(numTrailingZeros))
      return $.parseHTML("<span class='int'>"+int+"</span>"+
             "<span class='dot'>.</span>"+
             "<span class='decimal'>"+decimalVals+
                "<span class='trailing-zeroes'>"+trailingZeros+"</span>"+
              "</span>")
    },
    subsidyToString:  function(x, y = 1) {
      return (x / 100000000 / y) + " DCR"
    },
    bytes: function(s) { // from go-humanize
      var sizes = ["B", "kB", "MB", "GB", "TB", "PB", "EB"]
      if (s < 10) {
        return s + "B"
      }
      var e = Math.floor(logn(s,1000))
      var suffix = sizes[e]
      var val = Math.floor(s / Math.pow(1000, e) * 10 +0.5) / 10
      var precision = (val < 10) ? 1 : 0
      return round(val,precision) + " " + suffix
    },
    timeSince: function(unixTime) {
      var seconds = Math.floor(((new Date().getTime()/1000) - unixTime))
      var interval = Math.floor(seconds / 31536000);
      if (interval >= 1) {
        var extra = Math.floor((seconds - interval * 31536000) / 2592000)
        var result = interval + "y"
        if (extra > 0) {
          result = result + " " + extra + "mo"
        }
        return result
      }
      interval = Math.floor(seconds / 2592000);
      if (interval >= 1) {
        var extra = Math.floor((seconds - interval * 2592000) / 86400)
        var result = interval + "mo"
        if (extra > 0) {
          result = result + " " + extra + "d"
        }
        return result
      }
      interval = Math.floor(seconds / 86400);
      if (interval >= 1) {
        var extra = Math.floor((seconds - interval * 86400) / 3600)
        var result = interval + "d"
        if (extra > 0) {
          result = result + " " + extra + "h"
        }
        return result
      }
      interval = Math.floor(seconds / 3600);
      if (interval >= 1) {
        var extra = Math.floor((seconds - interval * 3600) / 60)
        var result = interval + "h"
        if (extra > 0) {
          result = result + " " + extra + "m"
        }
        return result
      }
      interval = Math.floor(seconds / 60);
      if (interval >= 1) {
        var extra = seconds - interval * 60
        var result = interval + "m"
        if (extra > 0) {
          result = result + " " + extra + "s"
        }
        return result
      }
      return Math.floor(seconds) + "s";
    }
  }
}())
