/* global Turbolinks */
import { Controller } from 'stimulus'
import dompurify from 'dompurify'
import ws from '../services/messagesocket_service'
import Notify from 'notifyjs'
import globalEventBus from '../services/event_bus_service'

function buildProgressBar (data) {
  var clean = dompurify.sanitize
  var progressVal = data.percentage_complete
  var timeRemaining = humanizeTime(data.seconds_to_complete)
  var htmlString = data.bar_msg.length > 0 ? clean(`<p style="font-size:14px;">${data.bar_msg}</p>`, {FORBID_TAGS: ['svg', 'math']} ) : ''
  var subtitle = data.subtitle.trim()
  var notifStr = subtitle.length > 0 ? clean(`<span style="font-size:11px;">notification : <i>${subtitle}</i></span>`, {FORBID_TAGS: ['svg', 'math']} ) : ''

  var remainingStr = 'pending'
  if (progressVal > 0) {
    remainingStr = data.seconds_to_complete > 0 ? 'remaining approx.  ' + timeRemaining : '0sec'
  }

  if (progressVal === 100) {
    remainingStr = 'done'
  }

  if (subtitle === 'sync complete') {
    notifStr = ''
  }

  return htmlString + clean(`<div class="progress" style="height:30px;border-radius:5px;">
                <div class="progress-bar sync-progress-bar" role="progressbar" style="height:auto; width:` + progressVal + `%;">
                <span class="nowrap pl-1 font-weight-bold">Progress ` + progressVal + '% (' + remainingStr + `)</span>
                </div>
            </div>`, {FORBID_TAGS: ['svg', 'math']} ) + notifStr
}

function humanizeTime (secs) {
  var years = Math.floor(secs / 31536000) % 10
  var months = Math.floor(secs / 2628000) % 12
  var weeks = Math.floor(secs / 604800) % 4
  var days = Math.floor(secs / 86400) % 7
  var hours = Math.floor(secs / 3600) % 24
  var minutes = Math.floor(secs / 60) % 60
  var seconds = secs % 60
  var timeUnit = ['yr', 'mo', 'wk', 'd', 'hr', 'min', 'sec']

  return [years, months, weeks, days, hours, minutes, seconds]
    .map((v, i) => v !== 0 ? v + '' + timeUnit[i] : '')
    .join('  ')
}

function doNotification () {
  var newBlockNtfn = new Notify('Blockchain Sync Complete', {
    body: 'Redirecting to home in 20 secs.',
    tag: 'blockheight',
    image: '/images/dcrdata144x128.png',
    icon: '/images/dcrdata144x128.png',
    notifyShow: Notify.onShowNotification,
    notifyClose: Notify.onCloseNotification,
    notifyClick: Notify.onClickNotification,
    notifyError: Notify.onErrorNotification,
    timeout: 10
  })
  newBlockNtfn.show()
}

let hasRedirected = false

export default class extends Controller {
  static get targets () {
    return ['statusSyncing', 'futureBlock', 'init', 'address', 'message']
  }

  connect () {
    this.progressBars = {
      'initial-load': this.initTarget,
      'addresses-sync': this.addressTarget
    }
    ws.registerEvtHandler('blockchainSync', (evt) => {
      var d = JSON.parse(evt)
      var i

      for (i = 0; i < d.length; i++) {
        var v = d[i]

        var bar = this.progressBars[v.progress_bar_id]
        while (bar.firstChild) bar.removeChild(bar.firstChild)
        bar.innerHTML = buildProgressBar(v)

        if (v.subtitle === 'sync complete' && !hasRedirected) {
          hasRedirected = true // block consecutive calls.
          if (!Notify.needsPermission) doNotification()

          this.messageTarget.querySelector('h5').textContent = 'Blockchain sync is complete. Redirecting to home in 20 secs.'
          setTimeout(() => Turbolinks.visit('/'), 20000)
          return
        }
      }
    })
    this.processBlock = this._processBlock.bind(this)
    globalEventBus.on('BLOCK_RECEIVED', this.processBlock)
  }

  disconnect () {
    ws.deregisterEvtHandlers('blockchainSync')
    globalEventBus.off('BLOCK_RECEIVED', this.processBlock)
  }

  _processBlock (blockData) {
    if (this.hasFutureBlockTarget) {
      Turbolinks.visit(window.location, { action: 'replace' })
    }
  }
}
