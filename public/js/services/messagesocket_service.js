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

let handlers = {}

function forward (event, message) {
  if (typeof handlers[event] === 'undefined') return
  // call each handler
  for (var i = 0; i < handlers[event].length; i++) {
    handlers[event][i](message)
  }
}

class MessageSocket {
  registerEvtHandler (eventID, handler) {
    handlers[eventID] = handlers[eventID] || []
    handlers[eventID].push(handler)
  }

  deregisterEvtHandlers (eventID) {
    handlers[eventID] = []
  }

  connect (uri) {
    this.uri = uri

    var connection = new window.WebSocket(uri)
    this.connection = connection

    this.close = function (reason) {
      console.log('close, reason:', reason, handlers)
      clearTimeout(pinger)
      handlers = {}
      connection.close()
    }

    // send a message back to the server
    var send = function (eventID, message) {
      var payload = JSON.stringify({
        event: eventID,
        message: message
      })
      console.log('send', payload)
      connection.send(payload)
      return this
    }
    this.send = send

    // unmarshall message, and forward the message to registered handlers
    connection.onmessage = function (evt) {
      var json = JSON.parse(evt.data)
      forward(json.event, json.message)
    }

    // Stub out standard functions
    connection.onclose = function () {
      forward('close', null)
    }
    connection.onopen = function () {
      forward('open', null)
    }
    connection.onerror = function (evt) {
      forward('error', evt)
    }

    // Start ping pong
    var pinger = setInterval(function () {
      send('ping', 'sup')
    }, 7000)
  }
}

let ws = new MessageSocket()
export default ws
