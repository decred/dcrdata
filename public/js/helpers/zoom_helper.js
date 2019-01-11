// Utilities for working with chart zoom windows.

// The keys in zoomMap can be passed directly to Zoom.validate.
var zoomMap = {
  all: 0,
  year: 3.154e+10,
  month: 2.628e+9,
  week: 6.048e+8,
  day: 8.64e+7
}

function zoomObject (start, end) {
  return {
    start: start,
    end: end
  }
}

// Attempts to decode a string of format start-end where start and end are
// base 36 encoded unix timestamps in seconds.
function decodeZoomString (encoded) {
  var range = encoded.split('-')
  if (range.length !== 2) {
    return false
  }
  var start = parseInt(range[0], 36)
  var end = parseInt(range[1], 36)
  if (isNaN(start) || isNaN(end) || end - start <= 0) {
    return false
  }
  return zoomObject(start, end)
}

function tryDecode (zoom) {
  if (Array.isArray(zoom) && zoom.length === 2) {
    return zoomObject(zoom[0], zoom[1])
  } else if (typeof zoom === 'string' && zoom.indexOf('-') !== -1) {
    return decodeZoomString(zoom)
  }
  return zoom
}

// Zoom is the exported class for dealing with Zoom windows. It is composed
// entirely of static methods used for working with zoom ranges.
export default class Zoom {
  static object (start, end) {
    return zoomObject(start, end)
  }

  static mapValue (key, scale) {
    scale = scale || 1
    return zoomMap[key] / scale
  }

  // encode uses base 36 encoded unix timestamps to store the range in a
  // short string.
  static encode (start, end) {
    // Can supply a single argument of zoomObject type, or two
    // millisecond timestamps.
    if (!end) {
      let range = tryDecode(start)
      end = range.end
      start = range.start
    }
    return parseInt(start).toString(36) + '-' + parseInt(end).toString(36)
  }

  static decode (encoded, limits, scale) {
    // decodes zoomString, such as from this.encode. zoomObjects pass through.
    // If limits are provided, encoded can be a zoomMap key.
    scale = scale || 1
    let decoded = tryDecode(encoded)
    let lims = tryDecode(limits)
    if (lims && zoomMap.hasOwnProperty(decoded)) {
      let duration = zoomMap[decoded] / scale
      if (duration === 0) return lims
      return zoomObject(lims.end - duration, lims.end)
    }
    return decoded
  }

  // validate will shift and clamp the proposed zoom window to accommodate the
  // range limits and minimum size.
  static validate (proposal, limits, minSize, scale) {
    // proposed: encoded zoom string || zoomMap key || zoomObject
    // limits: zoomObject || array
    scale = scale || 1
    let lims = tryDecode(limits)
    let proposed = tryDecode(proposal)
    var zoom = lims
    if (typeof proposed === 'string') {
      zoom = this.decode(proposed, lims, scale)
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

  // mapKey returns the corresponding map key, if the zoom meets the correct
  // range and position within the limits, else null.
  static mapKey (zoom, limits, scale) {
    let lims = tryDecode(limits)
    let decoded = this.decode(zoom, lims)
    if (decoded.end !== lims.end) return null
    if (decoded.start === lims.start) return 'all'
    let keys = Object.keys(zoomMap)
    for (let idx in keys) {
      let k = keys[idx]
      let v = zoomMap[k]
      if (v === 0) continue
      if (decoded.start === lims.end - v / scale) return k
    }
    return null
  }

  // project proportionally translates the zoom from oldWindow to newWindow.
  static project (zoom, oldWindow, newWindow) {
    let decoded = tryDecode(zoom)
    if (!decoded) return
    let ow = tryDecode(oldWindow)
    let nw = tryDecode(newWindow)
    let oldRange = ow.end - ow.start
    let newRange = nw.end - nw.start
    let pStart = (decoded.start - ow.start) / oldRange
    let pEnd = (decoded.end - ow.start) / oldRange
    return zoomObject(nw.start + pStart * newRange, nw.start + pEnd * newRange)
  }
}
