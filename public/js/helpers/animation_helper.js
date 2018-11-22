// this place to put animation helper functions

export function animationFrame () {
  let _resolve = null
  const promise = new Promise(function (resolve) {
    _resolve = resolve
  })
  window.requestAnimationFrame(_resolve)
  return promise
}
