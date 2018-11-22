/* global $ */
import { Controller } from 'stimulus'
import ws from '../services/messagesocket_service'
import Turbolinks from 'turbolinks'
import Notify from 'notifyjs'

function buildProgressBar (data) {
  var progressVal = data.percentage_complete
  var timeRemaining = humanizeTime(data.seconds_to_complete)
  var htmlString = data.bar_msg.length > 0 ? '<p style="font-size:14px;">' + data.bar_msg + '</p>' : ''
  var subtitle = data.subtitle.trim()
  var notifStr = subtitle.length > 0 ? '<span style="font-size:11px;">notification : <i>' + subtitle + '</i></span>' : ''

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

  return (htmlString + `<div class="progress" style="height:30px;border-radius:5px;">
                <div class="progress-bar sync-progress-bar" role="progressbar" style="height:auto; width:` + progressVal + `%;">
                <span class="nowrap pl-1 font-weight-bold">Progress ` + progressVal + '% (' + remainingStr + `)</span>
                </div>
            </div>` + notifStr)
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

export default class extends Controller {
  static get targets () {
    return [ 'statusSyncing' ]
  }

  connect () {
    ws.registerEvtHandler('blockchainSync', (evt) => {
      var d = JSON.parse(evt)
      var i

      for (i = 0; i < d.length; i++) {
        var v = d[i]

        $('#' + v.progress_bar_id).html(buildProgressBar(v))

        if (v.subtitle === 'sync complete') {
          if (!Notify.needsPermission) {
            doNotification()
          }

          $('.alert.alert-info h5').html('Blockchain sync is complete. Redirecting to home in 20 secs.')
          setInterval(() => Turbolinks.visit('/'), 20000)
          return
        }
      }
    })
  }

  disconnect () {
    ws.deregisterEvtHandlers('blockchainSync')
  }
}
