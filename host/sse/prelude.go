package sse

// prelude is the JavaScript that defines globalThis.EventSource. It is loaded by
// Install via vm.RunString and delegates networking to the hidden natives under
// globalThis.__gojs_sse (installed by Install). Keeping the web-facing API in
// script lets us model the DOM EventTarget semantics (handler properties plus
// addEventListener/removeEventListener/dispatchEvent) without a native
// constructor factory.
const prelude = `
(function () {
  "use strict";
  const native = globalThis.__gojs_sse;

  const CONNECTING = 0;
  const OPEN = 1;
  const CLOSED = 2;

  class EventSource {
    constructor(url, config) {
      if (url === undefined || url === null) {
        throw new TypeError("Failed to construct 'EventSource': url is required");
      }
      this.url = String(url);
      this.withCredentials = !!(config && config.withCredentials);
      this.readyState = CONNECTING;
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      this._listeners = Object.create(null);
      const self = this;
      this._handle = native.connect(
        this.url,
        { withCredentials: this.withCredentials },
        function (kind, payload) { self._onNative(kind, payload); }
      );
    }

    addEventListener(type, listener) {
      if (typeof listener !== "function") return;
      type = String(type);
      const list = this._listeners[type] || (this._listeners[type] = []);
      if (list.indexOf(listener) === -1) list.push(listener);
    }

    removeEventListener(type, listener) {
      type = String(type);
      const list = this._listeners[type];
      if (!list) return;
      const i = list.indexOf(listener);
      if (i !== -1) list.splice(i, 1);
    }

    dispatchEvent(event) {
      const type = event && event.type;
      const handler = this["on" + type];
      if (typeof handler === "function") handler.call(this, event);
      const list = this._listeners[type];
      if (list) {
        const copy = list.slice();
        for (let i = 0; i < copy.length; i++) copy[i].call(this, event);
      }
      return true;
    }

    close() {
      this.readyState = CLOSED;
      native.close(this._handle);
    }

    _onNative(kind, payload) {
      if (this.readyState === CLOSED) return;
      if (kind === "open") {
        this.readyState = OPEN;
        this.dispatchEvent({ type: "open", target: this });
      } else if (kind === "event") {
        this.dispatchEvent({
          type: payload.type || "message",
          data: payload.data,
          lastEventId: payload.lastEventId || "",
          origin: payload.origin || "",
          target: this,
        });
      } else if (kind === "error") {
        this.readyState = (payload && payload.reconnecting) ? CONNECTING : CLOSED;
        this.dispatchEvent({ type: "error", target: this });
      }
    }
  }

  EventSource.CONNECTING = CONNECTING;
  EventSource.OPEN = OPEN;
  EventSource.CLOSED = CLOSED;
  EventSource.prototype.CONNECTING = CONNECTING;
  EventSource.prototype.OPEN = OPEN;
  EventSource.prototype.CLOSED = CLOSED;

  globalThis.EventSource = EventSource;
})();
`
