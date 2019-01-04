import { Controller } from 'stimulus'
import { toggleMenu, closeMenu, toggleSun } from '../services/theme_service'

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
    document.addEventListener('click', (e) => {
      var target = e.srcElement || e.target
      if (target === this.toggleTarget) {
        return
      }
      if (closest(target, 'hamburger-menu')) {
        return
      }
      closeMenu(e)
    })
  }

  toggle (e) {
    toggleMenu(e)
  }

  onSunClick () {
    toggleSun()
  }
}
