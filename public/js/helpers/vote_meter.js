const PIPI = 2 * Math.PI

export default class VoteMeter {
  constructor (parent, opts) {
    opts = opts || {}
    this.parent = parent
    var d = parent.dataset
    this.data = {
      progress: parseFloat(d.progress),
      type: d.type
    }
    this.data.thresholds = d.threshold ? [parseFloat(d.threshold)] : null
    if (d.type === 'vote') this.data.thresholds.splice(0, 0, 1 - this.data.thresholds[0])
    this.centralFontSize = 15
    var l = parent.classList
    this.meterSpace = 0.5 // radians
    if (l.contains('large-gap')) this.meterSpace = Math.PI / 2
    if (l.contains('arch')) this.meterSpace = Math.PI
    this.startAngle = Math.PI / 2
    this.meterColor = '#2dd8a3'
    this.meterWidth = 8
    this.approveColor = '#2dd8a3' // '#41be53'
    this.revoteColor = '#ffe4a7'
    this.rejectColor = '#ed6d47'
    this.radius = 0.4
    this.darkTheme = {
      text: 'white',
      tray: '#777'
    }
    this.lightTheme = {
      text: 'black',
      tray: '#999'
    }
    this.activeTheme = opts.darkMode ? this.darkTheme : this.lightTheme
    this.showIndicator = true
    for (var k in opts) {
      this[k] = opts[k]
    }
    this.meterRange = {
      start: this.startAngle + this.meterSpace / 2,
      end: this.startAngle + PIPI - this.meterSpace / 2
    }
    this.meterRange.range = this.meterRange.end - this.meterRange.start
    this.middle = {
      x: 50,
      y: 50
    }
    this.checkmark = String.fromCharCode(10004)
    this.failmark = String.fromCharCode(10008)
    this.canvas = document.createElement('canvas')
    this.canvas.width = 100
    this.canvas.height = 100
    this.ctx = this.canvas.getContext('2d')
    this.ctx.textAlign = 'center'
    this.ctx.textBaseline = 'middle'
    while (parent.firstChild) parent.removeChild(parent.firstChild)
    this.parent.appendChild(this.canvas)
    this.draw()
  }

  denorm (x) {
    return x * this.canvas.width
  }

  polarToCartesian (r, theta) {
    return {
      y: this.middle.x - r * Math.cos(theta + this.startAngle),
      x: this.middle.y + r * Math.sin(theta + this.startAngle)
    }
  }

  thetaRange (start, end) {
    var range = this.meterRange
    return {
      start: range.start + start / PIPI * range.range,
      end: range.start + end / PIPI * range.range
    }
  }

  draw () {
    this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)
    if (this.data.thresholds) this.drawThresholds()
    if (this.showIndicator) this.drawIndicator()
    this.drawCentralValue()
    this.drawMeter()
  }

  drawThresholds () {
    this.data.thresholds.forEach((threshold) => {
      this.drawThreshold(threshold)
    })
  }

  drawThreshold (threshold) {
    var angle = this.thetaRange(threshold * PIPI).start
    var start = this.polarToCartesian(this.denorm(0.32), angle)
    var end = this.polarToCartesian(this.denorm(0.48), angle)
    var ctx = this.ctx
    ctx.lineWidth = 3.0
    ctx.strokeStyle = this.activeTheme.tray
    ctx.beginPath()
    ctx.moveTo(start.x, start.y)
    ctx.lineTo(end.x, end.y)
    ctx.stroke()
  }

  drawIndicator () {
    switch (this.data.type) {
      case 'vote':
        this.drawVoteIndicator()
        break
      case 'progress':
        this.drawProgressIndicator()
    }
  }

  drawProgressIndicator () {
    var ctx = this.ctx
    var threshold = 1
    if (this.data.thresholds) {
      threshold = this.data.thresholds[0]
    }
    if (this.data.progress >= threshold) {
      this.ctx.fillStyle = this.approveColor
      ctx.font = `${this.denorm(0.2)}px sans-serif`
      ctx.fillText(this.checkmark, this.middle.x, this.denorm(0.35))
    } else {
      this.ctx.fillStyle = this.activeTheme.tray
      ctx.beginPath()
      ctx.arc(this.middle.x, this.denorm(0.35), this.denorm(0.03), 0, PIPI)
      ctx.fill()
    }
  }

  drawVoteIndicator () {
    var ctx = this.ctx
    var fail, succeed
    [fail, succeed] = this.data.thresholds
    if (this.data.progress < fail) {
      this.ctx.fillStyle = this.rejectColor
      ctx.font = `${this.denorm(0.2)}px sans-serif`
      ctx.fillText(this.failmark, this.middle.x, this.denorm(0.35))
    } else if (this.data.progress >= succeed) {
      this.ctx.fillStyle = this.approveColor
      ctx.font = `${this.denorm(0.2)}px sans-serif`
      ctx.fillText(this.checkmark, this.middle.x, this.denorm(0.35))
    } else {
      this.ctx.fillStyle = this.activeTheme.tray
      ctx.beginPath()
      ctx.arc(this.middle.x, this.denorm(0.35), this.denorm(0.03), 0, PIPI)
      ctx.fill()
    }
  }

  drawCentralValue () {
    var ctx = this.ctx
    var offset = this.showIndicator ? this.denorm(0.05) : 0
    ctx.fillStyle = this.activeTheme.text
    ctx.font = `bold ${this.centralFontSize}px sans-serif`
    ctx.fillText(`${parseInt(this.data.progress * 100)}%`, this.middle.x, this.middle.y + offset, this.denorm(0.5))
  }

  drawMeter () {
    var ctx = this.ctx
    var theme = this.activeTheme
    ctx.lineWidth = this.meterWidth
    ctx.lineCap = 'round'
    this.ctx.strokeStyle = theme.tray
    var range = this.thetaRange(0, PIPI)
    ctx.beginPath()
    ctx.arc(this.middle.x, this.middle.y, this.denorm(this.radius), range.start, range.end)
    ctx.stroke()
    ctx.lineCap = 'round'
    if (this.data.type === 'vote') {
      let progress = this.data.progress
      var fail, succeed
      [fail, succeed] = this.data.thresholds
      if (progress >= succeed) {
        this.ctx.strokeStyle = this.approveColor
      } else if (progress < fail) {
        this.ctx.strokeStyle = this.rejectColor
      } else {
        this.ctx.strokeStyle = this.revoteColor
      }
    } else {
      this.ctx.strokeStyle = this.meterColor
    }

    range = this.thetaRange(0, PIPI * this.data.progress)
    ctx.beginPath()
    ctx.arc(this.middle.x, this.middle.y, this.denorm(this.radius), range.start, range.end)
    ctx.stroke()
  }

  setDarkMode (nightMode) {
    this.activeTheme = nightMode ? this.darkTheme : this.lightTheme
    this.draw()
  }
}
