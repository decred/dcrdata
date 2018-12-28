// shared functions for related to charts

export function barChartPlotter (e) {
  plotChart(e, fitWidth(e.points))
}

function fitWidth (points) {
  var minSep = Infinity
  for (var i = 1; i < points.length; i++) {
    var sep = points[i].canvasx - points[i - 1].canvasx
    if (sep < minSep) minSep = sep
  }
  return Math.floor(2.0 / 3 * minSep)
}

export function sizedBarPlotter (binSize) {
  return (e) => {
    let canvasBin = e.dygraph.toDomXCoord(binSize) - e.dygraph.toDomXCoord(0)
    plotChart(e, Math.floor(2.0 / 3 * canvasBin))
  }
}

function plotChart (e, barWidth) {
  var ctx = e.drawingContext
  var yBottom = e.dygraph.toDomYCoord(0)

  ctx.fillStyle = e.color

  e.points.map((p) => {
    var x = p.canvasx - barWidth / 2
    var height = yBottom - p.canvasy
    ctx.fillRect(x, p.canvasy, barWidth, height)
    ctx.strokeRect(x, p.canvasy, barWidth, height)
  })
}

export function padPoints (pts, binSize, sustain) {
  var pad = binSize / 2.0
  var lastPt = pts[pts.length - 1]
  var firstPt = pts[0]
  var frontStamp = firstPt[0].getTime()
  var backStamp = lastPt[0].getTime()
  var duration = backStamp - frontStamp
  if (duration < binSize) {
    pad = Math.max(pad, (binSize - duration) / 2.0)
  }
  var front = [new Date(frontStamp - pad)]
  var back = [new Date(backStamp + pad)]
  for (var i = 1; i < firstPt.length; i++) {
    front.push(0)
    back.push(sustain ? lastPt[i] : 0)
  }
  pts.unshift(front)
  pts.push(back)
}

var zoomMap = {
  all: 0,
  year: 3.154e+10,
  month: 2.628e+9,
  week: 6.048e+8,
  day: 8.64e+7
}

export class Zoom {
  static object (start, end) {
    return {
      start: start,
      end: end
    }
  }

  static mapValue (key) {
    return zoomMap[key]
  }

  static encode (start, end) {
    if (!end) {
      start = this.tryDecode(start)
      end = start.end
      start = start.start
    }
    return parseInt(start / 1000).toString(36) + '-' + parseInt(end / 1000).toString(36)
  }

  static decode (encoded, limits) {
    // if limits are provided, encoded can be a zoomMap key
    let decoded = this.tryDecode(encoded)
    let lims = this.tryDecode(limits)
    if (lims && zoomMap.hasOwnProperty(decoded)) {
      let duration = zoomMap[decoded]
      if (duration === 0) return lims
      return this.object(lims.end - zoomMap[decoded], lims.end)
    }
    return decoded
  }

  static decodeZoomString (encoded) {
    var range = encoded.split('-')
    if (range.length !== 2) {
      return false
    }
    var start = parseInt(range[0], 36)
    var end = parseInt(range[1], 36)
    if (isNaN(start) || isNaN(end) || end - start <= 0) {
      return false
    }
    return this.object(start * 1000, end * 1000)
  }

  static validate (proposed, lims, minSize) {
    // proposed: encoded zoom string || zoomMap key || zoomObject
    // lims: zoomObject || array
    lims = this.tryDecode(lims)
    proposed = this.tryDecode(proposed)
    var zoom = lims
    if (typeof proposed === 'string') {
      zoom = this.decode(proposed, lims)
      if (!zoom) return false
    } else if (proposed && typeof proposed === 'object') {
      zoom = proposed
    }
    // Shift-Clamp
    if (minSize && zoom.end - zoom.start < minSize) {
      zoom.end = zoom.start + minSize
    }
    if (zoom.end > lims.end) {
      let shift = zoom.end - lims.end
      zoom.end -= shift
      zoom.start = Math.max(zoom.start - shift, lims.start)
    } else if (zoom.start < lims.start) {
      let shift = lims.start - zoom.start
      zoom.start += shift
      zoom.end = Math.min(zoom.end + shift, lims.end)
    }
    return zoom
  }

  static tryDecode (zoom) {
    if (Array.isArray(zoom) && zoom.length === 2) {
      return this.object(zoom[0], zoom[1])
    } else if (typeof zoom === 'string' && zoom.indexOf('-') !== -1) {
      return this.decodeZoomString(zoom)
    }
    return zoom
  }

  static equals (standard, sample) {
    standard = this.tryDecode(standard)
    sample = this.tryDecode(sample)
    return standard.start === sample.start && standard.end === sample.end
  }
}
