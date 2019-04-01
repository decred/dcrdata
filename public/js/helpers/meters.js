const PIPI = 2 * Math.PI

function makePt (x, y) {
  return {
    x: x,
    y: y
  }
}

function addOffset (pt, offset) {
  return {
    x: pt.x + offset.x,
    y: pt.y + offset.y
  }
}

function sleep (ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

// Meter provides basic drawing and math utilities for a meter object, which
// has a circular strip shape with a gap. You can draw segments of the strip
// using segment. Child classes should implement a draw method. The parent
// parameter should be a block element with class .meter. CSS classes .large-gap
// and .arch can also be applied for increasingly large gap angle, with .arch
// being a semi-circle. Any contents of parent will be replaced with the Meter.
class Meter {
  constructor (parent, opts) {
    opts = opts || {}
    this.options = opts
    this.radius = opts.radius || 0.4
    this.parent = parent
    this.canvas = document.createElement('canvas')
    opts.padding = opts.padding || 15
    // Add a little padding around the drawing area by offsetting all drawing.
    // This allows element like text to overflow the drawing area slightly
    // without being cut off.
    this.offset = makePt(opts.padding, opts.padding)
    // Meter's API provides an assumed canvas size of 100 x 100
    this.dimension = 100
    this.canvas.width = this.dimension + 2 * opts.padding
    this.canvas.height = this.dimension + 2 * opts.padding

    // Keep track of any properties being animated
    this.animationEnd = {}
    this.animationRunning = {}
    this.animationTarget = {}

    this.ctx = this.canvas.getContext('2d')
    this.ctx.textAlign = 'center'
    this.ctx.textBaseline = 'middle'
    while (parent.firstChild) parent.removeChild(parent.firstChild)
    this.parent.appendChild(this.canvas)
    this.middle = {
      x: 50,
      y: 50
    }
    this.offsetMiddle = addOffset(this.middle, this.offset)
    this.startAngle = Math.PI / 2
    var meterSpace = 0.5 // radians
    if (parent.classList.contains('large-gap')) meterSpace = Math.PI / 2
    if (parent.classList.contains('arch')) meterSpace = Math.PI
    this.meterSpecs = {
      meterSpace: meterSpace,
      startTheta: this.startAngle + meterSpace / 2,
      endTheta: this.startAngle + PIPI - meterSpace / 2
    }
    this.meterSpecs.range = this.meterSpecs.endTheta - this.meterSpecs.startTheta
    this.meterSpecs.start = this.normedPolarToCartesian(this.radius, 0)
    this.meterSpecs.end = this.normedPolarToCartesian(this.radius, 1)

    // animation options
    opts.fps = opts.fps || 60
    opts.animationLength = opts.animationLength || 400

    // A couple unicode characters
    this.checkmark = String.fromCharCode(10004)
    this.failmark = String.fromCharCode(10008)
  }

  roundCap () {
    this.ctx.lineCap = 'round'
  }

  buttCap () {
    this.ctx.lineCap = 'butt'
  }

  denorm (x) {
    return x * this.dimension
  }

  norm (x) {
    return x / this.dimension
  }

  normedPolarToCartesian (normed, normedTheta) {
    // maps radius: [0,1] to [0,width], and angle: [0,1] to [0,2PI]
    var r = this.denorm(normed)
    var theta = this.denormTheta(normedTheta)
    return {
      y: this.middle.x - r * Math.cos(theta + this.startAngle),
      x: this.middle.y + r * Math.sin(theta + this.startAngle)
    }
  }

  denormTheta (theta) {
    // [0, 1] to [this.meterSpecs.startTheta, this.meterSpecs.endTheta]
    return this.meterSpecs.startTheta + theta * this.meterSpecs.range
  }

  denormThetaRange (start, end) {
    return {
      start: this.denormTheta(start),
      end: this.denormTheta(end)
    }
  }

  setDarkMode (nightMode) {
    this.activeTheme = nightMode ? this.darkTheme : this.lightTheme
    this.draw()
  }

  clear () {
    this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)
  }

  dot (pt, color, radius) {
    pt = addOffset(pt, this.offset)
    var ctx = this.ctx
    ctx.fillStyle = color
    ctx.beginPath()
    ctx.arc(pt.x, pt.y, radius, 0, PIPI)
    ctx.fill()
  }

  segment (start, end, color) {
    // A segment of the strip, where 0 <= start < end <= 1.
    var ctx = this.ctx
    ctx.strokeStyle = color
    let range = this.denormThetaRange(start, end)
    ctx.beginPath()
    ctx.arc(this.offsetMiddle.x, this.offsetMiddle.y, this.denorm(this.radius), range.start, range.end)
    ctx.stroke()
  }

  line (start, end) {
    // set ctx.lineWidth and ctx.strokeStyle before drawing.
    start = addOffset(start, this.offset)
    end = addOffset(end, this.offset)
    var ctx = this.ctx
    ctx.beginPath()
    ctx.moveTo(start.x, start.y)
    ctx.lineTo(end.x, end.y)
    ctx.stroke()
  }

  write (text, pt, maxWidth) {
    // Set ctx.textAlign and ctx.textBaseline for additional alignment options.
    pt = addOffset(pt, this.offset)
    this.ctx.fillText(text, pt.x, pt.y, maxWidth)
  }

  async animate (key, target) {
    // key is a string referencing any property of Meter.data.
    var opts = this.options
    this.animationEnd[key] = new Date().getTime() + opts.animationLength
    this.animationTarget[key] = target
    if (this.animationRunning[key]) return
    this.animationRunning[key] = true
    var frameDuration = 1000 / opts.fps
    var now = new Date().getTime()
    while (now < this.animationEnd[key]) {
      let remainingTime = this.animationEnd[key] - now
      let progress = this.data[key]
      let toGo = this.animationTarget[key] - progress
      let step = toGo * frameDuration / remainingTime
      await sleep(frameDuration)
      this.data[key] = progress + step
      this.draw()
      now = new Date().getTime()
    }
    this.data[key] = this.animationTarget[key]
    this.draw()
    this.animationRunning[key] = false
  }
}

// VoteMeter has three regions: reject, revote, and approve. The parent element
// should have property parent.dataset.threshold set to the pass threshold [0,1],
// and parent.dataset.approval set to the current approval rate.
export class VoteMeter extends Meter {
  constructor (parent, opts) {
    super(parent, opts)
    this.buttCap()
    opts = this.options
    var d = parent.dataset
    this.data = {
      approval: parseFloat(d.approval)
    }
    this.approveThreshold = d.threshold
    this.rejectThreshold = 1 - d.threshold

    // Options
    opts.centralFontSize = opts.centralFontSize || 18
    opts.meterColor = opts.meterColor || '#2dd8a3'
    opts.meterWidth = opts.meterWidth || 12
    opts.approveColor = opts.approveColor || '#2dd8a3'
    opts.revoteColor = opts.revoteColor || '#ffe4a7'
    opts.rejectColor = opts.rejectColor || '#ed6d47'
    opts.dotColor = opts.dotColor || '#888'
    opts.showIndicator = opts.showIndicator || true
    this.darkTheme = opts.darkTheme || {
      text: 'white',
      tray: '#999'
    }
    this.lightTheme = opts.lightTheme || {
      text: 'black',
      tray: '#555'
    }
    this.activeTheme = opts.darkMode ? this.darkTheme : this.lightTheme

    // Set up a starting animation
    var progress = this.data.approval
    this.data.approval = 0.5
    this.draw()
    super.animate('approval', progress)
  }

  draw () {
    super.clear()
    this.drawVote()
  }

  drawVote () {
    var ctx = this.ctx
    var opts = this.options
    var indicatorColor
    var strokeColor = this.activeTheme.text
    var trayColor = this.activeTheme.tray

    // Draw the three-color tray with border
    var borderWidth = opts.meterWidth * 1.3
    ctx.lineWidth = borderWidth
    this.segment(0, 1, trayColor)
    ctx.lineWidth = opts.meterWidth
    this.segment(0, this.rejectThreshold, opts.rejectColor)
    this.segment(this.rejectThreshold, this.approveThreshold, opts.revoteColor)
    this.segment(this.approveThreshold, 1, opts.approveColor)
    ctx.strokeStyle = trayColor
    ctx.lineWidth = 2
    super.line(super.normedPolarToCartesian(this.radius + this.norm(borderWidth / 2), 0),
      super.normedPolarToCartesian(this.radius - this.norm(borderWidth / 2), 0))
    super.line(super.normedPolarToCartesian(this.radius + this.norm(borderWidth / 2), 1),
      super.normedPolarToCartesian(this.radius - this.norm(borderWidth / 2), 1))

    // Draw the indicator icon, which is a checkmark if the measure is currently
    // passing.
    if (opts.showIndicator) {
      ctx.font = `${super.denorm(0.2)}px sans-serif`
      if (this.data.approval < this.rejectThreshold) {
        ctx.fillStyle = indicatorColor = opts.rejectColor
        super.write(this.failmark, makePt(this.middle.x, super.denorm(0.35)))
      } else if (this.data.approval >= this.approveThreshold) {
        ctx.fillStyle = indicatorColor = opts.approveColor
        super.write(this.checkmark, makePt(this.middle.x, super.denorm(0.35)))
      } else {
        indicatorColor = opts.revoteColor
        super.dot(makePt(this.middle.x, super.denorm(0.35)), opts.dotColor, super.denorm(0.03))
      }
    }

    // The mark
    var halfLen = this.norm(opts.meterWidth * 0.5)
    var start = super.normedPolarToCartesian(this.radius - halfLen * 1.2, this.data.approval)
    var end = super.normedPolarToCartesian(this.radius + halfLen * 1.6, this.data.approval)
    ctx.lineWidth = 2.5
    ctx.strokeStyle = strokeColor
    super.line(start, end)
    super.dot(start, strokeColor, 3)
    super.dot(end, strokeColor, 5)
    super.dot(end, indicatorColor, 3)

    // The central value
    var offset = opts.showIndicator ? super.denorm(0.05) : 0
    this.ctx.fillStyle = this.activeTheme.text
    this.ctx.font = `bold ${opts.centralFontSize}px sans-serif`
    this.write(`${parseInt(this.data.approval * 100)}%`, makePt(this.middle.x, this.middle.y + offset), super.denorm(0.5))
  }
}

// ProgressMeter has a single threshold. The parent element should have property
// parent.dataset.threshold set to the progress threshold [0,1], and
// parent.dataset.progress set to the current value.
export class ProgressMeter extends Meter {
  constructor (parent, opts) {
    super(parent, opts)
    this.buttCap()
    opts = this.options
    this.threshold = parseFloat(parent.dataset.threshold)
    this.data = {
      progress: parseFloat(parent.dataset.progress)
    }

    opts.meterColor = opts.meterColor || '#2970ff'
    opts.meterWidth = opts.meterWidth || 14
    opts.centralFontSize = opts.centralFontSize || 18
    opts.successColor = opts.successColor = '#2dd8a3'
    opts.showIndicator = opts.showIndicator || true
    this.darkTheme = opts.darkTheme || {
      tray: '#777',
      text: 'white'
    }
    this.lightTheme = opts.lightTheme || {
      tray: '#999',
      text: 'black'
    }
    this.activeTheme = opts.darkMode ? this.darkTheme : this.lightTheme

    var progress = this.data.progress
    this.data.progress = 0
    this.draw()
    this.animate('progress', progress)
  }

  drawIndicator (value, color) {
    var ctx = this.ctx
    var opts = this.options
    var theme = this.activeTheme
    var halfLen = this.norm(opts.meterWidth) * 0.5
    var start = super.normedPolarToCartesian(this.radius - halfLen, value)
    var end = super.normedPolarToCartesian(this.radius + halfLen, value)
    ctx.lineWidth = 1.5
    ctx.strokeStyle = color
    super.dot(start, color, 3)
    super.dot(end, color, 3)
    ctx.strokeStyle = theme.text
    super.line(start, end)
  }

  draw () {
    super.clear()
    var ctx = this.ctx
    var opts = this.options
    var theme = this.activeTheme

    ctx.lineWidth = opts.meterWidth
    var c = this.data.progress >= this.threshold ? opts.successColor : theme.tray
    super.segment(0, 1, c)

    this.drawIndicator(this.threshold, c)

    ctx.lineWidth = opts.meterWidth
    super.segment(0, this.data.progress, opts.meterColor)

    this.drawIndicator(this.data.progress, theme.text)

    if (opts.showIndicator && this.data.progress >= this.threshold) {
      ctx.fillStyle = opts.successColor
      super.write(this.checkmark, makePt(this.middle.x, super.denorm(0.35)))
    }

    var offset = opts.showIndicator ? super.denorm(0.05) : 0
    this.ctx.fillStyle = this.activeTheme.text
    this.ctx.font = `bold ${opts.centralFontSize}px sans-serif`
    this.write(`${parseInt(this.data.progress * 100)}%`, makePt(this.middle.x, this.middle.y + offset), super.denorm(0.5))
  }
}
