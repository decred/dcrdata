import { Controller } from 'stimulus'
import { closeMenu, toggleSun } from '../services/theme_service'

function closest (el, id) {
  // https://stackoverflow.com/a/48726873/1124661
  if (el.id === id) {
    return el
  }
  if (el.parentNode && el.parentNode.nodeName !== 'BODY') {
    return closest(el.parentNode, id)
  }
  return null
}

export default class extends Controller {
  static get targets () {
    return ['toggle', 'darkModeToggle']
  }

  connect () {
    this.clickout = this._clickout.bind(this)
    document.querySelectorAll('.menu-submit').forEach(button => { button.disabled = true })
  }

  _clickout (e) {
    var target = e.target || e.srcElement
    if (!closest(target, 'hamburger-menu')) {
      document.removeEventListener('click', this.clickout)
      closeMenu()
    }
  }

  toggle (e) {
    if (this.toggleTarget.checked) {
      document.addEventListener('click', this.clickout)
    }
  }

  onSunClick () {
    toggleSun()
  }
}
