// Utilities for working with chart zoom windows.

// The keys in zoomMap can be passed directly to Zoom.validate.
const zoomMap = {
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
  const range = encoded.split('-')
  if (range.length !== 2) {
    return false
  }
  const start = parseInt(range[0], 36)
  const end = parseInt(range[1], 36)
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
      const range = tryDecode(start)
      end = range.end
      start = range.start
    }
    return parseInt(start).toString(36) + '-' + parseInt(end).toString(36)
  }

  static decode (encoded, limits, scale) {
    // decodes zoomString, such as from this.encode. zoomObjects pass through.
    // If limits are provided, encoded can be a zoomMap key.
    scale = scale || 1
    const decoded = tryDecode(encoded)
    const lims = tryDecode(limits)
    if (lims && Object.prototype.hasOwnProperty.call(zoomMap, decoded)) {
      const duration = zoomMap[decoded] / scale
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
    const lims = tryDecode(limits)
    const proposed = tryDecode(proposal)
    let zoom = lims
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
      const shift = zoom.end - lims.end
      zoom.end -= shift
      zoom.start = Math.max(zoom.start - shift, lims.start)
    } else if (zoom.start < lims.start) {
      const shift = lims.start - zoom.start
      zoom.start += shift
      zoom.end = Math.min(zoom.end + shift, lims.end)
    }
    return zoom
  }

  // mapKey returns the corresponding map key, if the zoom meets the correct
  // range and position within the limits, else null.
  static mapKey (zoom, limits, scale) {
    scale = scale || 1
    const lims = tryDecode(limits)
    const decoded = this.decode(zoom, lims)
    const range = decoded.end - decoded.start
    if (decoded.start === lims.start && range === lims.end - lims.start) return 'all'
    for (const k in zoomMap) {
      const v = zoomMap[k]
      if (v === 0) continue
      // support an error of ±0.99 due to precision loss while dividing.
      if (Math.abs(v / scale - range) < 0.99) return k
    }
    return null
  }

  // project proportionally translates the zoom from oldWindow to newWindow.
  static project (zoom, oldWindow, newWindow) {
    const decoded = tryDecode(zoom)
    if (!decoded) return
    const ow = tryDecode(oldWindow)
    const nw = tryDecode(newWindow)
    const oldRange = ow.end - ow.start
    const newRange = nw.end - nw.start
    const pStart = (decoded.start - ow.start) / oldRange
    const pEnd = (decoded.end - ow.start) / oldRange
    return zoomObject(nw.start + pStart * newRange, nw.start + pEnd * newRange)
  }
}
