// shared functions for related to charts

export function barChartPlotter (e) {
  var ctx = e.drawingContext
  var points = e.points
  var yBottom = e.dygraph.toDomYCoord(0)

  ctx.fillStyle = e.color

  var minSep = Infinity
  for (var i = 1; i < points.length; i++) {
    var sep = points[i].canvasx - points[i - 1].canvasx
    if (sep < minSep) minSep = sep
  }
  var barWidth = Math.floor(2.0 / 3 * minSep)
  points.map((p) => {
    var x = p.canvasx - barWidth / 2
    var height = yBottom - p.canvasy
    ctx.fillRect(x, p.canvasy, barWidth, height)
    ctx.strokeRect(x, p.canvasy, barWidth, height)
  })
}
