// this place to put animation helper functions

export function animationFrame () {
  let _resolve = null
  const promise = new Promise(function (resolve) {
    _resolve = resolve
  })
  window.requestAnimationFrame(_resolve)
  return promise
}

export async function fadeIn (element, duration) {
  element.style.transition = 'none'
  element.style.opacity = 0
  await animationFrame()
  element.style.transition = `opacity ${duration || 0.42}s ease-in-out`
  element.style.opacity = 1
}
