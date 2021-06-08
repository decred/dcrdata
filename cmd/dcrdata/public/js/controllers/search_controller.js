import { Controller } from 'stimulus'
import * as Turbo from '@hotwired/turbo'

export default class extends Controller {
  execute (e) {
    e.preventDefault()
    const search = e.target[0].value.trim()
    if (search === '') {
      return
    }
    Turbo.visit('/search?search=' + search)
  }
}
