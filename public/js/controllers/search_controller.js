/* global Turbolinks */
import { Controller } from 'stimulus'

export default class extends Controller {
  execute (e) {
    e.preventDefault()
    Turbolinks.visit('/search?search=' + e.target[0].value)
  }
}
