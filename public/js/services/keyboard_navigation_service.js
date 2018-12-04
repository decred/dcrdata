/* global $ */
/* global Turbolinks */
import { toggleMenu, toggleSun, closeMenu } from '../services/theme_service'
import { setCookie } from './cookie_service'
import Mousetrap from 'mousetrap'
import { addPauseToMousetrap } from '../vendor/mousetrap-pause'

addPauseToMousetrap(Mousetrap)

// Keyboard Navigation
var targets
var targetsLength
var currentIndex = 0
var jumpToIndexOnLoad
var keyNavCookieName = 'dcrdataKeyNav'

function keyNavEnabled () {
  return document.cookie.includes(keyNavCookieName)
}

function clearTargets () {
  $('.keynav-target').each((i, el) => {
    $(el).removeClass('keynav-target').removeClass('pulsate')
  })
}

function enableKeyNav () {
  setCookie(keyNavCookieName, 1, 525600)
  Mousetrap.unpause()
  $('#keynav-toggle .text').text('Disable Hot Keys')
  keyNav()
}

function disableKeyNav () {
  setCookie(keyNavCookieName, '', 0)
  clearTargets()
  $('#keynav-toggle .text').text('Enable Hot Keys')
  Mousetrap.pause()
}

function toggleKeyNav () {
  if (keyNavEnabled()) {
    disableKeyNav()
  } else {
    enableKeyNav()
  }
}

export function keyNav (event, pulsate, preserveIndex) {
  if ($('#menuToggle input').prop('checked')) {
    targets = $('#hamburger-menu a')
    currentIndex = 0
  } else {
    targets = $.merge($('a:not([data-keynav-skip])'), ($('.top-search')))
    if (jumpToIndexOnLoad > 0) {
      currentIndex = jumpToIndexOnLoad
      jumpToIndexOnLoad = undefined
    } else if (!preserveIndex) {
      var priorityLink = $('[data-keynav-priority]')[0]
      var i = $.inArray(priorityLink, targets)
      currentIndex = i > 0 ? i : 0
    }
  }
  targetsLength = targets.length
  clearTargets()
  $(targets[currentIndex]).addClass('keynav-target').focus().blur() // refocuses keyboard context
  if (pulsate) {
    $(targets[currentIndex]).addClass('pulsate')
  }
}

Mousetrap.bind(['left', '['], function () {
  clearTargets()
  currentIndex--
  if (currentIndex < 0) {
    currentIndex = targetsLength - 1
  }
  $(targets[currentIndex]).addClass('keynav-target')
})

Mousetrap.bind(['right', ']'], function () {
  clearTargets()
  currentIndex++
  if (currentIndex >= targetsLength) {
    currentIndex = 0
  }
  $(targets[currentIndex]).addClass('keynav-target')
})

Mousetrap.bind('enter', function (e) {
  if (targets.length < currentIndex) {
    return
  }
  var currentTarget = $(targets[currentIndex])
  if (currentTarget.is('input')) {
    currentTarget.focus()
    e.stopPropagation()
    e.preventDefault()
    return
  }
  if (currentTarget[0].id === 'keynav-toggle') {
    toggleKeyNav()
    return
  }
  var location = currentTarget.attr('href')
  if (location !== undefined) {
    var preserveKeyNavIndex = currentTarget.data('preserveKeynavIndex')
    if (preserveKeyNavIndex) {
      jumpToIndexOnLoad = currentIndex
    }
    currentTarget.addClass('activated')
    Turbolinks.visit(location)
  }
})

Mousetrap.bind('\\', function (e) {
  e.preventDefault()
  var $topSearch = $('.top-search')
  if ($topSearch.hasClass('keynav-target')) {
    $topSearch.blur()
    clearTargets()
    keyNav(e, true, 0)
  } else {
    clearTargets()
    $topSearch.addClass('keynav-target').focus()
  }
})

Mousetrap.bind('`', function () {
  toggleSun()
})

Mousetrap.bind('=', function (e) {
  toggleMenu(e)
  keyNav(e, true)
})

Mousetrap.bind('q', function () {
  clearTargets()
})

if (keyNavEnabled()) Mousetrap.unpause()

$('#keynav-toggle .text').text(keyNavEnabled() ? 'Disable Hot Keys' : 'Enable Hot Keys')
document.addEventListener('turbolinks:load', function (e) {
  closeMenu(e)
  if (keyNavEnabled()) {
    $('.top-search').removeAttr('autofocus')
    keyNav(e, true)
  } else {
    $('.top-search').focus()
  }
})

$('#keynav-toggle').on('click', function (e) {
  if (e.offsetX === 0) {
    // prevent duplicate click handling when turbolinks re-attaches handlers
    // TODO find a more semantic way to deal with this
    return
  }
  toggleKeyNav()
})

$('#menuToggle input').change(function (e) {
  if (keyNavEnabled()) {
    keyNav(e, true)
  }
})
