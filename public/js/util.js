// a place for handy functions utility that can be shared across the client side application

function animationFrame() {
    let resolve = null
    const promise = new Promise(r => resolve = r)
    window.requestAnimationFrame(resolve)
    return promise
}