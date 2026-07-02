// WebSocket web-API surface for gojs. This runs once at Install time and
// defines globalThis.WebSocket, delegating all I/O to the hidden native bridge
// globalThis.__gojs_ws (connect/send/close).
//
// Divergences from browsers: there is no Blob, so binary messages always arrive
// as an ArrayBuffer and binaryType defaults to "arraybuffer" (browsers default
// to "blob"). Everything else follows the WHATWG WebSocket interface.
(function () {
  "use strict";

  var WS = globalThis.__gojs_ws;

  var CONNECTING = 0;
  var OPEN = 1;
  var CLOSING = 2;
  var CLOSED = 3;

  var EVENT_TYPES = ["open", "message", "error", "close"];

  // A minimal Event-like object. MessageEvent carries `data`; CloseEvent carries
  // `code`, `reason`, `wasClean`; the error event carries `message`.
  function makeEvent(type, props) {
    var ev = { type: type, target: null };
    if (props) {
      for (var k in props) {
        if (Object.prototype.hasOwnProperty.call(props, k)) ev[k] = props[k];
      }
    }
    return ev;
  }

  class WebSocket {
    constructor(url, protocols) {
      if (url === undefined) {
        throw new TypeError("Failed to construct 'WebSocket': 1 argument required");
      }
      this.url = String(url);
      this._readyState = CONNECTING;
      this.protocol = "";
      this.extensions = "";
      this.bufferedAmount = 0;
      this.binaryType = "arraybuffer";
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      this.onclose = null;
      this._listeners = { open: [], message: [], error: [], close: [] };

      var protoArg = "";
      if (protocols !== undefined && protocols !== null) {
        if (Array.isArray(protocols)) protoArg = protocols.join(", ");
        else protoArg = String(protocols);
      }

      var self = this;
      this._handle = WS.connect(this.url, protoArg, function (type, a, b, c) {
        self._dispatch(type, a, b, c);
      });
    }

    get readyState() {
      return this._readyState;
    }

    _dispatch(type, a, b, c) {
      switch (type) {
        case "open":
          this._readyState = OPEN;
          this.protocol = a || "";
          this.extensions = b || "";
          this._fire("open", makeEvent("open"));
          break;
        case "message":
          this._fire("message", makeEvent("message", { data: a }));
          break;
        case "error":
          this._fire("error", makeEvent("error", { message: a || "" }));
          break;
        case "close":
          this._readyState = CLOSED;
          this._fire(
            "close",
            makeEvent("close", { code: a, reason: b || "", wasClean: !!c })
          );
          break;
      }
    }

    _fire(type, ev) {
      ev.target = this;
      var handler = this["on" + type];
      if (typeof handler === "function") {
        try {
          handler.call(this, ev);
        } catch (e) {
          // Listener exceptions must not break the dispatch, mirroring the DOM.
        }
      }
      var list = this._listeners[type];
      if (list) {
        var copy = list.slice();
        for (var i = 0; i < copy.length; i++) {
          try {
            copy[i].call(this, ev);
          } catch (e) {
            // swallow, as above
          }
        }
      }
    }

    send(data) {
      if (this._readyState === CONNECTING) {
        var err = new Error("Failed to execute 'send' on 'WebSocket': still in CONNECTING state");
        err.name = "InvalidStateError";
        throw err;
      }
      if (this._readyState !== OPEN) {
        // CLOSING or CLOSED: the spec drops the data silently.
        return;
      }
      WS.send(this._handle, data);
    }

    close(code, reason) {
      if (code !== undefined) {
        code = code | 0;
        if (code !== 1000 && !(code >= 3000 && code <= 4999)) {
          var err = new Error("Failed to execute 'close' on 'WebSocket': invalid code " + code);
          err.name = "InvalidAccessError";
          throw err;
        }
      }
      if (reason !== undefined && reason !== null) {
        reason = String(reason);
        // A close reason is limited to 123 bytes of UTF-8 (spec).
        if (reason.length > 123) {
          var e2 = new Error("Failed to execute 'close' on 'WebSocket': reason too long");
          e2.name = "SyntaxError";
          throw e2;
        }
      }
      if (this._readyState === CLOSING || this._readyState === CLOSED) {
        return;
      }
      this._readyState = CLOSING;
      WS.close(this._handle, code, reason);
    }

    addEventListener(type, listener) {
      if (EVENT_TYPES.indexOf(type) < 0 || typeof listener !== "function") return;
      var list = this._listeners[type];
      if (list.indexOf(listener) < 0) list.push(listener);
    }

    removeEventListener(type, listener) {
      var list = this._listeners[type];
      if (!list) return;
      var idx = list.indexOf(listener);
      if (idx >= 0) list.splice(idx, 1);
    }

    dispatchEvent(event) {
      if (!event || typeof event.type !== "string") return true;
      this._fire(event.type, event);
      return true;
    }
  }

  // readyState constants as static and instance (prototype) properties.
  WebSocket.CONNECTING = CONNECTING;
  WebSocket.OPEN = OPEN;
  WebSocket.CLOSING = CLOSING;
  WebSocket.CLOSED = CLOSED;
  WebSocket.prototype.CONNECTING = CONNECTING;
  WebSocket.prototype.OPEN = OPEN;
  WebSocket.prototype.CLOSING = CLOSING;
  WebSocket.prototype.CLOSED = CLOSED;

  globalThis.WebSocket = WebSocket;
})();
