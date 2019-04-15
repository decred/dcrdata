// shared functions for related to charts

export function barChartPlotter (e) {
  plotChart(e, fitWidth(e.points))
}

function fitWidth (points) {
  return Math.floor(2.0 / 3 * findMinSep(points, Infinity))
}

function findMinSep (points, minSep) {
  for (var i = 1; i < points.length; i++) {
    var sep = points[i].canvasx - points[i - 1].canvasx
    if (sep < minSep) minSep = sep
  }
  return minSep
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

export function multiColumnBarPlotter (e) {
  if (e.seriesIndex !== 0) return

  var g = e.dygraph
  var ctx = e.drawingContext
  var sets = e.allSeriesPoints
  var yBottom = e.dygraph.toDomYCoord(0)

  var minSep = Infinity
  sets.map((bar) => { minSep = findMinSep(bar, minSep) })
  var barWidth = Math.floor(2.0 / 3 * minSep)
  var strokeColors = g.getColors()
  var fillColors = g.getOption('fillColors')

  sets.map((bar, i) => {
    ctx.fillStyle = fillColors[i]
    ctx.strokeStyle = strokeColors[i]

    bar.map((p) => {
      var xLeft = p.canvasx - (barWidth / 2) * (1 - i / (sets.length - 1))
      var height = yBottom - p.canvasy
      var width = barWidth / sets.length

      ctx.fillRect(xLeft, p.canvasy, width, height)
      ctx.strokeRect(xLeft, p.canvasy, width, height)
    })
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

function isEqual (a, b) {
  if (!Array.isArray(a) || !Array.isArray(b)) return false
  var i = a.length
  if (i !== b.length) return false
  while (i--) {
    if (a[i] !== b[i]) return false
  }
  return true
}

function minViewValueRange (chartIndex) {
  if (chartIndex === 0) {
    return 70
  } else if (chartIndex === 1) {
    return 8400
  }
  return 0
}

function attachZoomHandlers (gs, prevCallbacks) {
  var block = false
  gs.map((g) => {
    g.updateOptions({
      drawCallback: function (me, initial) {
        if (block || initial) return
        block = true
        var opts = { dateWindow: me.xAxisRange() }

        gs.map((gCopy, j) => {
          if (gCopy === me) {
            if (prevCallbacks[j] && prevCallbacks[j].drawCallback) {
              prevCallbacks[j].drawCallback.apply(this, arguments)
            }
            return
          }

          if (isEqual(opts.dateWindow, gCopy.getOption('dateWindow'))) {
            return
          }

          var yMinRange = minViewValueRange(j)
          if (yMinRange > 0) {
            var yRange = gCopy.yAxisRange()
            if (yRange && yMinRange > yRange[1]) {
              opts.valueRange = [yRange[0], yMinRange]
            } else if (yRange && yMinRange < yRange[1]) {
              opts.valueRange = yRange
            }
          }

          gCopy.updateOptions(opts)

          delete opts.valueRange
        })
        block = false
      }
    }, true)
  })
}

function attachSelectionHandlers (gs, prevCallbacks) {
  var block = false
  gs.map((g) => {
    g.updateOptions({
      highlightCallback: function (event, x, points, row, seriesName) {
        if (block) return
        block = true
        var me = this
        gs.map((gCopy, i) => {
          if (me === gCopy) {
            if (prevCallbacks[i] && prevCallbacks[i].highlightCallback) {
              prevCallbacks[i].highlightCallback.apply(this, arguments)
            }
            return
          }
          var idx = gCopy.getRowForX(x)
          if (idx !== null) {
            gCopy.setSelection(idx, seriesName)
          }
        })
        block = false
      },
      unhighlightCallback: function (event) {
        if (block) return
        block = true
        var me = this
        gs.map((gCopy, i) => {
          if (me === gCopy) {
            if (prevCallbacks[i] && prevCallbacks[i].unhighlightCallback) {
              prevCallbacks[i].unhighlightCallback.apply(this, arguments)
            }
            return
          }
          gCopy.clearSelection()
        })
        block = false
      }
    }, true)
  })
}

export function synchronize (dygraphs, syncOptions) {
  let prevCallbacks = []

  dygraphs.map((g, index) => {
    g.ready(() => {
      var callBackTypes = ['drawCallback', 'highlightCallback', 'unhighlightCallback']
      for (var j = 0; j < dygraphs.length; j++) {
        if (!prevCallbacks[j]) {
          prevCallbacks[j] = {}
        }
        for (var k = callBackTypes.length - 1; k >= 0; k--) {
          prevCallbacks[j][callBackTypes[k]] = dygraphs[j].getFunctionOption(callBackTypes[k])
        }
      }

      if (syncOptions.zoom) {
        attachZoomHandlers(dygraphs, prevCallbacks)
      }

      if (syncOptions.selection) {
        attachSelectionHandlers(dygraphs, prevCallbacks)
      }

      var yMinRange = minViewValueRange(index)
      if (yMinRange === 0) {
        return
      }
      var yRange = g.yAxisRange()
      if (yRange && yMinRange > yRange[1]) {
        g.updateOptions({ valueRange: [yRange[0], yMinRange] })
      }
    })
  })
}
