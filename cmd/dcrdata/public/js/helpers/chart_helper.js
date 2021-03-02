// shared functions for related to charts

export function barChartPlotter (e) {
  plotChart(e, findWidth(findMinSep(e.points, Infinity)))
}

function findWidth (sep) {
  return Math.floor(2.0 / 3 * sep * 10000) / 10000 // to 4 decimal places
}

function findMinSep (points, minSep) {
  for (let i = 1; i < points.length; i++) {
    const sep = points[i].canvasx - points[i - 1].canvasx
    if (sep < minSep) minSep = sep
  }
  return minSep
}

function findLineWidth (barWidth) {
  const width = 0.2 + (0.08 * barWidth)
  return width > 1 ? 1 : width
}

export function sizedBarPlotter (binSize) {
  return (e) => {
    const canvasBin = e.dygraph.toDomXCoord(binSize) - e.dygraph.toDomXCoord(0)
    plotChart(e, findWidth(canvasBin))
  }
}

function plotChart (e, barWidth) {
  const ctx = e.drawingContext
  const yBottom = e.dygraph.toDomYCoord(0)

  ctx.fillStyle = e.color
  ctx.lineWidth = findLineWidth(barWidth)

  e.points.map((p) => {
    if (p.yval === 0) return
    const x = p.canvasx - barWidth / 2
    const height = yBottom - p.canvasy
    ctx.fillRect(x, p.canvasy, barWidth, height)
    ctx.strokeRect(x, p.canvasy, barWidth, height)
  })
}

export function multiColumnBarPlotter (e) {
  if (e.seriesIndex !== 0) return

  const g = e.dygraph
  const ctx = e.drawingContext
  const sets = e.allSeriesPoints
  const yBottom = e.dygraph.toDomYCoord(0)

  let minSep = Infinity
  sets.map((bar) => { minSep = findMinSep(bar, minSep) })
  const barWidth = findWidth(minSep)
  const strokeColors = g.getColors()
  const fillColors = g.getOption('fillColors')
  ctx.lineWidth = findLineWidth(barWidth)

  sets.map((bar, i) => {
    ctx.fillStyle = fillColors[i]
    ctx.strokeStyle = strokeColors[i]

    bar.map((p) => {
      if (p.yval === 0) return
      const xLeft = p.canvasx - (barWidth / 2) * (1 - i / (sets.length - 1))
      const height = yBottom - p.canvasy
      const width = barWidth / sets.length

      ctx.fillRect(xLeft, p.canvasy, width, height)
      ctx.strokeRect(xLeft, p.canvasy, width, height)
    })
  })
}

export function padPoints (pts, binSize, sustain) {
  let pad = binSize / 2.0
  const lastPt = pts[pts.length - 1]
  const firstPt = pts[0]
  const frontStamp = firstPt[0].getTime()
  const backStamp = lastPt[0].getTime()
  const duration = backStamp - frontStamp
  if (duration < binSize) {
    pad = Math.max(pad, (binSize - duration) / 2.0)
  }
  const front = [new Date(frontStamp - pad)]
  const back = [new Date(backStamp + pad)]
  for (let i = 1; i < firstPt.length; i++) {
    front.push(0)
    back.push(sustain ? lastPt[i] : 0)
  }
  pts.unshift(front)
  pts.push(back)
}

export function isEqual (a, b) {
  if (!Array.isArray(a) || !Array.isArray(b)) return false
  let i = a.length
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
  let block = false
  gs.map((g) => {
    g.updateOptions({
      drawCallback: function (me, initial) {
        if (block || initial) return
        block = true
        const opts = { dateWindow: me.xAxisRange() }

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

          const yMinRange = minViewValueRange(j)
          if (yMinRange > 0) {
            const yRange = gCopy.yAxisRange()
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
  let block = false
  gs.map((g) => {
    g.updateOptions({
      highlightCallback: function (event, x, points, row, seriesName) {
        if (block) return
        block = true
        const me = this
        gs.map((gCopy, i) => {
          if (me === gCopy) {
            if (prevCallbacks[i] && prevCallbacks[i].highlightCallback) {
              prevCallbacks[i].highlightCallback.apply(this, arguments)
            }
            return
          }
          const idx = gCopy.getRowForX(x)
          if (idx !== null) {
            gCopy.setSelection(idx, seriesName)
          }
        })
        block = false
      },
      unhighlightCallback: function (event) {
        if (block) return
        block = true
        const me = this
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
  const prevCallbacks = []

  dygraphs.map((g, index) => {
    g.ready(() => {
      const callBackTypes = ['drawCallback', 'highlightCallback', 'unhighlightCallback']
      for (let j = 0; j < dygraphs.length; j++) {
        if (!prevCallbacks[j]) {
          prevCallbacks[j] = {}
        }
        for (let k = callBackTypes.length - 1; k >= 0; k--) {
          prevCallbacks[j][callBackTypes[k]] = dygraphs[j].getFunctionOption(callBackTypes[k])
        }
      }

      if (syncOptions.zoom) {
        attachZoomHandlers(dygraphs, prevCallbacks)
      }

      if (syncOptions.selection) {
        attachSelectionHandlers(dygraphs, prevCallbacks)
      }

      const yMinRange = minViewValueRange(index)
      if (yMinRange === 0) {
        return
      }
      const yRange = g.yAxisRange()
      if (yRange && yMinRange > yRange[1]) {
        g.updateOptions({ valueRange: [yRange[0], yMinRange] })
      }
    })
  })
}
