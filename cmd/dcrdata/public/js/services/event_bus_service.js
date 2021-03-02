import { remove } from 'lodash-es'

const eventCallbacksPairs = []

function findEventCallbacksPair (eventType) {
  return eventCallbacksPairs.find(eventObject => eventObject.eventType === eventType)
}

function EventCallbacksPair (eventType, callback) {
  this.eventType = eventType
  this.callbacks = [callback]
}

class EventBus {
  on (eventType, callback) {
    const eventCallbacksPair = findEventCallbacksPair(eventType)
    if (eventCallbacksPair) {
      eventCallbacksPair.callbacks.push(callback)
    } else {
      eventCallbacksPairs.push(new EventCallbacksPair(eventType, callback))
    }
  }

  off (eventType, callback) {
    const eventCallbacksPair = findEventCallbacksPair(eventType)
    if (eventCallbacksPair) {
      remove(eventCallbacksPair.callbacks, (cb) => {
        return cb === callback
      })
    }
  }

  publish (eventType, args) {
    const eventCallbacksPair = findEventCallbacksPair(eventType)
    if (!eventCallbacksPair) return
    eventCallbacksPair.callbacks.forEach(callback => callback(args))
  }
}

const eventBusInstance = new EventBus()
export default eventBusInstance
