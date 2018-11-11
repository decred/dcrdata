/* global $ */
import { Controller } from 'stimulus'
import { toggleMenu, closeMenu, toggleSun } from '../services/theme_service'

export default class extends Controller {
  static get targets () {
    return ['toggle', 'darkModeToggle']
  }

  connect () {
    $(document).click((e) => {
      if (e.target === this.toggleTarget) {
        return
      }
      if ($(e.target).parents('#hamburger-menu').size() > 0) {
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
