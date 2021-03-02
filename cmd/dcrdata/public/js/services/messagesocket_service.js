// MessageSocket is a WebSocket manager with an assumed JSON message format.
//
// JSON message format:
// {
//   event: name,
//   message: your message data
// }
//
// Functions for external use:
// register(id, handler_function) -- register a function to handle events of
//     the given type
// send(id, data) -- create a JSON message in the above format and send it
//
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.
//
// Based on ws_events_dispatcher.js by Ismael Celis

function forward (event, message, handlers) {
  if (typeof handlers[event] === 'undefined') return
  // call each handler
  for (let i = 0; i < handlers[event].length; i++) {
    handlers[event][i](message)
  }
}

class MessageSocket {
  constructor () {
    this.uri = undefined
    this.connection = undefined
    this.handlers = {}
    this.queue = []
    this.maxQlength = 5
  }

  registerEvtHandler (eventID, handler) {
    this.handlers[eventID] = this.handlers[eventID] || []
    this.handlers[eventID].push(handler)
  }

  deregisterEvtHandlers (eventID) {
    this.handlers[eventID] = []
  }

  // send a message back to the server
  send (eventID, message) {
    if (this.connection === undefined) {
      while (this.queue.length > this.maxQlength - 1) this.queue.shift()
      this.queue.push([eventID, message])
      return
    }
    const payload = JSON.stringify({
      event: eventID,
      message: message
    })

    if (window.loggingDebug) console.log('send', payload)
    this.connection.send(payload)
  }

  connect (uri) {
    this.uri = uri
    this.connection = new window.WebSocket(uri)

    this.close = (reason) => {
      console.log('close, reason:', reason, this.handlers)
      clearTimeout(pinger)
      this.handlers = {}
      this.connection.close()
    }

    // unmarshal message, and forward the message to registered handlers
    this.connection.onmessage = (evt) => {
      const json = JSON.parse(evt.data)
      forward(json.event, json.message, this.handlers)
    }

    // Stub out standard functions
    this.connection.onclose = () => {
      forward('close', null, this.handlers)
    }
    this.connection.onopen = () => {
      forward('open', null, this.handlers)
      while (this.queue.length) {
        const [eventID, message] = this.queue.shift()
        this.send(eventID, message)
      }
    }
    this.connection.onerror = (evt) => {
      forward('error', evt, this.handlers)
    }

    // Start ping pong
    const pinger = setInterval(() => {
      this.send('ping', 'sup')
    }, 7000)
  }
}

const ws = new MessageSocket()
export default ws
