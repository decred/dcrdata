// this is a handrolled humanize lib designed to mirror the go-humanize package used by backend
var humanize = (function() {
  function logn(n, b) {
    return Math.log(n) / Math.log(b)
  }
  function round(value, precision) {
    var multiplier = Math.pow(10, precision || 0)
    return Math.round(value * multiplier) / multiplier
  }
  return {
    bytes: function(s) {
      var sizes = ["B", "kB", "MB", "GB", "TB", "PB", "EB"]
      if (s < 10) {
        return s + "B"
      }
      var e = Math.floor(logn(s,1000))
      var suffix = sizes[e]
      var val = Math.floor(s / Math.pow(1000, e) * 10 +0.5) / 10
      var precision = (val < 10) ? 1 : 0
      return round(val,precision) + " " + suffix
    }
  }
}())
