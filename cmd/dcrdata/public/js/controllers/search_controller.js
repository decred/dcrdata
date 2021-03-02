/* global Turbolinks */
import { Controller } from 'stimulus'

export default class extends Controller {
  execute (e) {
    e.preventDefault()
    const search = e.target[0].value.trim()
    if (search === '') {
      return
    }
    Turbolinks.visit('/search?search=' + search)
  }
}
