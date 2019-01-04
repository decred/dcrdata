import { setCookie } from './cookie_service'
import globalEventBus from './event_bus_service'
/* global $ */

var sunIcon = document.getElementById('sun-icon')
var darkBGCookieName = 'dcrdataDarkBG'

export function darkEnabled () {
  return document.cookie.includes(darkBGCookieName)
}

if (darkEnabled()) {
  toggleToDarkClasses(document.body)
} else {
  toggleToLightClasses(document.body)
}
function toggleToDarkClasses (body) {
  $(sunIcon).removeClass('dcricon-sun-fill')
  $(sunIcon).addClass('dcricon-sun-stroke')
  $(body).addClass('darkBG')
}
function toggleToLightClasses (body) {
  $(body).removeClass('darkBG')
  $(sunIcon).removeClass('dcricon-sun-stroke')
  $(sunIcon).addClass('dcricon-sun-fill')
}
export function toggleSun () {
  if (darkEnabled()) {
    setCookie(darkBGCookieName, '', 0)
    toggleToLightClasses(document.body)
    globalEventBus.publish('NIGHT_MODE', { nightMode: false })
  } else {
    setCookie(darkBGCookieName, 1, 525600)
    toggleToDarkClasses(document.body)
    globalEventBus.publish('NIGHT_MODE', { nightMode: true })
  }
}

document.addEventListener('turbolinks:before-render', function (event) {
  if (darkEnabled()) {
    toggleToDarkClasses(event.data.newBody)
  } else {
    toggleToLightClasses(event.data.newBody)
  }
})

export function toggleMenu () {
  $('#menuToggle input').prop('checked', !$('#menuToggle input').prop('checked'))
}

export function closeMenu () {
  $('#menuToggle input').prop('checked', false)
}
