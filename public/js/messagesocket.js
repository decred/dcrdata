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

var MessageSocket = function(uri) {
  // create a new WebSocket connection
  console.log("new MessageSocket", uri);
  var ws = new WebSocket(uri);

  console.log("WS",ws);
  this.ws = ws

  // register an event handler
  var handlers = {};
  this.handlers = handlers;
  this.registerEvtHandler = function(eventID, handler) {
    handlers[eventID] = handlers[eventID] || [];
    handlers[eventID].push(handler);
    return this;
  };

  this.close = function(reason) {
    console.log("close, reason:", reason, handlers)
    clearTimeout(pinger);
    handlers = {};
    ws.close()
  }

  // send a message back to the server
  var send = function(eventID, message) {
    var payload = JSON.stringify({
      event: eventID,
      message: message
    });
    console.log("send", payload)
    ws.send(payload);
    return this;
  };
  this.send = send;

  // unmarshall message, and forward the message to registered handlers
  ws.onmessage = function(evt) {
    var json = JSON.parse(evt.data);
    forward(json.event, json.message);
  };

  // forward a message for the named event to the right handlers
  var forward = function(event, message) {

    // handlers registered for this event type
    var eventHandlers = handlers[event];
    if (typeof eventHandlers == "undefined") return;

    // call each handler
    for (var i = 0; i < eventHandlers.length; i++) {
      eventHandlers[i](message);
    }
  };

  // Stub out standard functions
  ws.onclose = function() {
    forward("close", null);
  };
  ws.onopen = function() {
    forward("open", null);
  };
  ws.onerror = function(evt) {
    forward("error", evt);
  };

  // Start ping pong
  var pinger = setInterval(function () {
    send("ping","sup");
  }, 7000);

};
