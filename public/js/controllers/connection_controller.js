import { Controller } from 'stimulus'
import ws from '../services/messagesocket_service'
import Notify from 'notifyjs'

export default class extends Controller {
  static get targets () {
    return ['indicator', 'status']
  }

  connect () {
    this.indicatorTarget.classList.remove('hidden')

    ws.registerEvtHandler('open', () => {
      console.log('Connected')
      this.updateConnectionStatus('Connected', true)
    })

    ws.registerEvtHandler('close', () => {
      console.log('Disconnected')
      this.updateConnectionStatus('Disconnected', false)
    })

    ws.registerEvtHandler('error', (evt) => {
      console.log('WebSocket error:', evt)
      this.updateConnectionStatus('Disconnected', false)
    })

    ws.registerEvtHandler('ping', (evt) => {
      console.debug('ping. users online: ', evt)
    })
  }

  updateConnectionStatus (msg, connected) {
    if (connected) {
      this.indicatorTarget.classList.add('connected')
      this.indicatorTarget.classList.remove('disconnected')
    } else {
      this.indicatorTarget.classList.remove('connected')
      this.indicatorTarget.classList.add('disconnected')
    }
    this.statusTarget.textContent = `${msg} `
  }

  requestNotifyPermission () {
    if (Notify.needsPermission) {
      Notify.requestPermission(onPermissionGranted, onPermissionDenied)
    }
  }
}

function onPermissionGranted () {
  console.log('Permission has been granted by the user')
}

function onPermissionDenied () {
  console.warn('Permission has been denied by the user')
}
