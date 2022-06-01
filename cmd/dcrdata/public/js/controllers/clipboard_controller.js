import { Controller } from '@hotwired/stimulus'

export const copyIcon = () => {
  const copyIcon = document.createElement('span')
  copyIcon.classList.add('dcricon-copy')
  copyIcon.classList.add('clickable')
  copyIcon.dataset.controller = 'clipboard'
  copyIcon.dataset.action = 'click->clipboard#copyTextToClipboard this'
  return copyIcon.outerHTML
}

export const alertArea = () => {
  const alertArea = document.createElement('span')
  alertArea.classList.add('alert')
  alertArea.classList.add('alert-success')
  alertArea.classList.add('alert-copy')
  return alertArea.outerHTML
}

export default class extends Controller {
  copyTextToClipboard (clickEvent) {
    const parentNode = clickEvent.srcElement.parentNode
    const textContent = parentNode.textContent.trim().split(' ')[0]
    navigator.clipboard.writeText(textContent).then(() => {
      const alertCopy = parentNode.getElementsByClassName('alert-copy')[0]
      alertCopy.textContent = 'Copied'
      alertCopy.style.display = 'inline-table'
      setTimeout(function () {
        alertCopy.textContent = ''
        alertCopy.style.display = 'none'
      }, 1000)
    }, (reason) => {
      console.error('Unable to copy:', reason)
    })
  }
}
