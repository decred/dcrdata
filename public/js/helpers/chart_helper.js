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
