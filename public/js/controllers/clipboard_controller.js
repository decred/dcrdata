import { Controller } from 'stimulus'

export var copyIcon = () => {
  var copyIcon = document.createElement('span')
  copyIcon.classList.add('dcricon-copy')
  copyIcon.classList.add('clickable')
  copyIcon.dataset.controller = 'clipboard'
  copyIcon.dataset.action = 'click->clipboard#copyTextToClipboard this'
  return copyIcon.outerHTML
}

export var alertArea = () => {
  var alertArea = document.createElement('span')
  alertArea.classList.add('alert')
  alertArea.classList.add('alert-success')
  alertArea.classList.add('alert-copy')
  return alertArea.outerHTML
}

export default class extends Controller {
  connect () {
    var copySupported = document.queryCommandSupported('copy')
    if (copySupported === false) {
      for (const classname of ['dcricon-copy', 'alert-copy']) {
        var icons = document.getElementsByClassName(classname)
        while (icons.length > 0) icons[0].remove()
      }
    }
  }

  copyTextToClipboard (clickEvent) {
    var parentNode = clickEvent.srcElement.parentNode
    var textContent = parentNode.textContent.trim().split(' ')[0]
    var copyTextArea = document.createElement('textarea')
    copyTextArea.value = textContent
    document.body.appendChild(copyTextArea)
    copyTextArea.select()
    try {
      document.execCommand('copy')
      var alertCopy = parentNode.getElementsByClassName('alert-copy')[0]
      alertCopy.textContent = 'Copied!'
      alertCopy.style.display = 'inline-table'
      setTimeout(function () {
        alertCopy.textContent = ''
        alertCopy.style.display = 'none'
      }, 1000)
    } catch (err) {
      console.log('Unable to copy: you can Ctrl+C or Command+C selected area')
    }
    document.body.removeChild(copyTextArea)
  }
}
