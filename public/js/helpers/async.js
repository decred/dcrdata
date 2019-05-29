// General async functions
import { animationFrame } from './animation_helper'

const FPS = 30

export function sleep (ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

var nonces = {}

// Kill all Jobs who have the given groupID, After killJobs is called, all
// currently running Jobs match the groupID will return the value true for
// Job.expired().
export function killJobs (groupID) { if (nonces[groupID]) nonces[groupID]++ }

// Nonces are set by groupID, which enables the killing of some jobs and not
// others.
function getNonce (groupID) {
  if (nonces[groupID]) return nonces[groupID]
  nonces[groupID] = 1
  return nonces[groupID]
}

// A Job is a context for work and a set of useful methods for  performing long
// and/or sequential tasks without blocking. Running Jobs can be cancelled by
// using the module-level killJobs function. The Job can store values using
// the set and get methods.
export class Job {
  constructor (groupID) {
    // The groupID (string) can later be provided to killJobs, which stop the
    // execution of all jobs with that groupID at the next frame.
    this.groupID = groupID
    // When killJobs is called with this job's groupID, the nonce returned by
    // getNonce will change.
    this.nonce = getNonce(groupID)
    // if set, frameFunc is called during irun
    this.frameFunc = null
    this._resolve = null
    this.completion = new Promise(resolve => {
      this._resolve = resolve
    })
    this._dict = {}
    this.queue = []
    this._running = false
  }

  set (k, v) { this._dict[k] = v }

  get (k) { return this._dict[k] }

  done () {
    this._resolve()
  }

  expired () {
    return this.nonce !== getNonce(this.groupID)
  }

  // irun asynchronously runs a mapping function of single parameter i,
  // 0 < i < num An animationFrame is requested at a frequency that targets a
  // frame rate as defined in FPS.
  async irun (num, mapper) {
    var translated = []
    var frameDuration = 1000 / FPS
    var takeBreak = true
    var tick = () => {
      takeBreak = false
      setTimeout(() => { takeBreak = true }, frameDuration)
    }
    for (let i = 0; i < num; i++) {
      if (takeBreak) {
        await animationFrame()
        if (this.frameFunc) this.frameFunc()
        if (this.expired()) break
        tick()
      }
      translated.push(mapper(i))
    }
    return translated
  }

  // run queues a function and starts the processing loop
  async run (f, ...args) {
    return new Promise(resolve => {
      this.queue.push([f, resolve, args])
      if (!this._running) this.processQueue()
    })
  }

  async processQueue () {
    this._running = true
    while (this.queue.length) {
      if (this.expired()) return
      let f, resolve, args
      [f, resolve, args] = this.queue.shift()
      await f(this, ...args)
      resolve()
    }
    this._running = false
  }
}
