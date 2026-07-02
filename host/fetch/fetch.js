// WHATWG Fetch prelude for gojs.
//
// This script defines the public web API surface — fetch, Headers, Request,
// Response, AbortController, AbortSignal, DOMException — on globalThis, in terms
// of a small set of hidden native primitives installed by the host under
// globalThis.__gojs_fetch:
//
//   __gojs_fetch.send(method, url, headerPairs, bodyU8OrNull, redirect)
//       -> { promise, cancel }
//       promise resolves to { status, statusText, url, redirected,
//                             headers: [[name,value],...], body: Uint8Array }
//       or rejects with a TypeError (network/parse error).
//       cancel(): aborts the in-flight request.
//   __gojs_fetch.utf8Encode(string) -> Uint8Array
//   __gojs_fetch.utf8Decode(Uint8Array|ArrayBuffer) -> string
//
// All WHATWG semantics (header normalization, body extraction, consumption
// guards, promise-returning body accessors) live here; the round-trip and the
// UTF-8 codec live in Go.
(function () {
  "use strict";

  var native = globalThis.__gojs_fetch;

  // ---- DOMException (minimal) ---------------------------------------------
  class DOMException extends Error {
    constructor(message, name) {
      super(message === undefined ? "" : String(message));
      this.name = name === undefined ? "Error" : String(name);
    }
  }

  function abortError(reason) {
    if (reason !== undefined) return reason;
    return new DOMException("The operation was aborted.", "AbortError");
  }

  // ---- Headers -------------------------------------------------------------
  // Backed by a Map from a lowercased name to the combined value. Multiple
  // appends of the same name are joined with ", " (WHATWG "combined value").
  const HMAP = Symbol("headers");

  function normalizeName(name) {
    name = String(name);
    if (name === "" || /[^!#$%&'*+\-.^_`|~0-9A-Za-z]/.test(name)) {
      throw new TypeError("Invalid header name: " + JSON.stringify(name));
    }
    return name.toLowerCase();
  }

  function normalizeValue(value) {
    // Strip leading/trailing HTTP whitespace, matching the WHATWG algorithm.
    return String(value).replace(/^[\t\n\r ]+|[\t\n\r ]+$/g, "");
  }

  class Headers {
    constructor(init) {
      this[HMAP] = new Map();
      if (init === undefined || init === null) return;
      if (init instanceof Headers) {
        init.forEach((value, name) => this.append(name, value));
      } else if (Array.isArray(init)) {
        for (const pair of init) {
          if (pair.length !== 2) {
            throw new TypeError("Header init pair must have two elements");
          }
          this.append(pair[0], pair[1]);
        }
      } else if (typeof init === "object") {
        for (const key of Object.keys(init)) this.append(key, init[key]);
      } else {
        throw new TypeError("Invalid Headers init");
      }
    }

    append(name, value) {
      name = normalizeName(name);
      value = normalizeValue(value);
      const map = this[HMAP];
      map.set(name, map.has(name) ? map.get(name) + ", " + value : value);
    }

    set(name, value) {
      this[HMAP].set(normalizeName(name), normalizeValue(value));
    }

    get(name) {
      name = normalizeName(name);
      return this[HMAP].has(name) ? this[HMAP].get(name) : null;
    }

    has(name) {
      return this[HMAP].has(normalizeName(name));
    }

    delete(name) {
      this[HMAP].delete(normalizeName(name));
    }

    // Iteration is over a sorted, lowercased view of the name/value pairs.
    _sorted() {
      const names = [...this[HMAP].keys()].sort();
      return names.map((n) => [n, this[HMAP].get(n)]);
    }

    forEach(callback, thisArg) {
      for (const [name, value] of this._sorted()) {
        callback.call(thisArg, value, name, this);
      }
    }

    *entries() {
      for (const pair of this._sorted()) yield pair;
    }

    *keys() {
      for (const [name] of this._sorted()) yield name;
    }

    *values() {
      for (const [, value] of this._sorted()) yield value;
    }

    [Symbol.iterator]() {
      return this.entries();
    }

    // Internal: the wire form the native layer expects.
    _pairs() {
      return this._sorted();
    }
  }

  // ---- Body mixin ----------------------------------------------------------
  // Bodies are stored as a Uint8Array (or null). Reading a body consumes it:
  // a second read rejects, matching the "bodyUsed" guard.
  const BODY = Symbol("body");
  const USED = Symbol("bodyUsed");

  function extractBody(body) {
    if (body === undefined || body === null) {
      return { bytes: null, contentType: null };
    }
    if (body instanceof Uint8Array) {
      return { bytes: body, contentType: null };
    }
    if (body instanceof ArrayBuffer) {
      return { bytes: new Uint8Array(body), contentType: null };
    }
    // Any other value is stringified (URLSearchParams/FormData/Blob are not
    // implemented — see README).
    const text = String(body);
    return {
      bytes: native.utf8Encode(text),
      contentType: "text/plain;charset=UTF-8",
    };
  }

  function consume(target) {
    if (target[USED]) {
      return Promise.reject(new TypeError("Body has already been consumed"));
    }
    target[USED] = true;
    const bytes = target[BODY] === null ? new Uint8Array(0) : target[BODY];
    return Promise.resolve(bytes);
  }

  function defineBody(proto) {
    Object.defineProperty(proto, "bodyUsed", {
      configurable: true,
      get() {
        return this[USED] === true;
      },
    });
    proto.arrayBuffer = function () {
      return consume(this).then((b) => {
        // Return a copy so the caller owns the buffer.
        const out = new Uint8Array(b.length);
        out.set(b);
        return out.buffer;
      });
    };
    proto.bytes = function () {
      return consume(this).then((b) => {
        const out = new Uint8Array(b.length);
        out.set(b);
        return out;
      });
    };
    proto.text = function () {
      return consume(this).then((b) => native.utf8Decode(b));
    };
    proto.json = function () {
      return consume(this).then((b) => JSON.parse(native.utf8Decode(b)));
    };
  }

  // ---- Request -------------------------------------------------------------
  const NO_BODY_METHODS = new Set(["GET", "HEAD"]);

  class Request {
    constructor(input, init) {
      init = init || {};
      let url, baseHeaders, baseBody, baseMethod, baseRedirect, baseSignal;
      if (input instanceof Request) {
        url = input.url;
        baseHeaders = input.headers;
        baseBody = input[BODY];
        baseMethod = input.method;
        baseRedirect = input.redirect;
        baseSignal = input.signal;
      } else {
        url = String(input);
      }

      this.url = url;
      let method = (init.method !== undefined ? init.method : baseMethod) || "GET";
      this.method = String(method).toUpperCase();
      this.redirect = init.redirect !== undefined ? init.redirect : baseRedirect || "follow";
      this.signal = init.signal !== undefined ? init.signal : baseSignal || null;

      this.headers = new Headers(
        init.headers !== undefined ? init.headers : baseHeaders
      );

      let bodySource = init.body !== undefined ? init.body : undefined;
      let bytes = baseBody !== undefined ? baseBody : null;
      if (bodySource !== undefined) {
        const extracted = extractBody(bodySource);
        bytes = extracted.bytes;
        if (extracted.contentType && !this.headers.has("content-type")) {
          this.headers.set("content-type", extracted.contentType);
        }
      }
      if (bytes !== null && NO_BODY_METHODS.has(this.method)) {
        throw new TypeError(
          "Request with GET/HEAD method cannot have a body"
        );
      }
      this[BODY] = bytes;
      this[USED] = false;
    }

    clone() {
      const r = new Request(this.url, {
        method: this.method,
        headers: this.headers,
        redirect: this.redirect,
        signal: this.signal,
      });
      r[BODY] = this[BODY];
      return r;
    }
  }
  defineBody(Request.prototype);

  // ---- Response ------------------------------------------------------------
  class Response {
    constructor(body, init) {
      init = init || {};
      const status = init.status !== undefined ? init.status : 200;
      if (status < 200 || status > 599) {
        throw new RangeError("Response status " + status + " is out of range");
      }
      this.status = status;
      this.statusText = init.statusText !== undefined ? String(init.statusText) : "";
      this.headers = new Headers(init.headers);
      this.url = init.url !== undefined ? String(init.url) : "";
      this.redirected = init.redirected === true;
      this.type = init.type !== undefined ? String(init.type) : "default";

      const extracted = extractBody(body);
      this[BODY] = extracted.bytes;
      this[USED] = false;
      if (extracted.contentType && !this.headers.has("content-type")) {
        this.headers.set("content-type", extracted.contentType);
      }
    }

    get ok() {
      return this.status >= 200 && this.status < 300;
    }

    clone() {
      const r = new Response(null, {
        status: this.status,
        statusText: this.statusText,
        headers: this.headers,
        url: this.url,
        redirected: this.redirected,
        type: this.type,
      });
      r[BODY] = this[BODY];
      return r;
    }

    static json(data, init) {
      init = init || {};
      const headers = new Headers(init.headers);
      if (!headers.has("content-type")) {
        headers.set("content-type", "application/json");
      }
      return new Response(JSON.stringify(data), {
        status: init.status,
        statusText: init.statusText,
        headers,
      });
    }

    static error() {
      const r = new Response(null, { status: 200, type: "error" });
      return r;
    }
  }
  defineBody(Response.prototype);

  // Build a Response from the native round-trip result.
  function responseFromNative(result) {
    const headers = new Headers();
    for (const pair of result.headers) headers.append(pair[0], pair[1]);
    const resp = new Response(result.body, {
      status: result.status,
      statusText: result.statusText,
      headers,
      url: result.url,
      redirected: result.redirected,
    });
    return resp;
  }

  // ---- AbortController / AbortSignal ---------------------------------------
  const LISTENERS = Symbol("listeners");

  class AbortSignal {
    constructor() {
      this.aborted = false;
      this.reason = undefined;
      this.onabort = null;
      this[LISTENERS] = [];
    }

    throwIfAborted() {
      if (this.aborted) throw this.reason;
    }

    addEventListener(type, listener) {
      if (type === "abort") this[LISTENERS].push(listener);
    }

    removeEventListener(type, listener) {
      if (type !== "abort") return;
      const l = this[LISTENERS];
      const idx = l.indexOf(listener);
      if (idx >= 0) l.splice(idx, 1);
    }

    dispatchEvent(event) {
      if (event && event.type === "abort") this._signalAbort(this.reason);
      return true;
    }

    _signalAbort(reason) {
      if (this.aborted) return;
      this.aborted = true;
      this.reason = abortError(reason);
      const event = { type: "abort", target: this };
      if (typeof this.onabort === "function") {
        try {
          this.onabort(event);
        } catch (e) {}
      }
      for (const listener of this[LISTENERS].slice()) {
        try {
          listener.call(this, event);
        } catch (e) {}
      }
    }

    static abort(reason) {
      const s = new AbortSignal();
      s._signalAbort(reason);
      return s;
    }

    static timeout(ms) {
      const s = new AbortSignal();
      if (typeof setTimeout === "function") {
        setTimeout(() => {
          s._signalAbort(
            new DOMException("The operation timed out.", "TimeoutError")
          );
        }, ms);
      }
      return s;
    }
  }

  class AbortController {
    constructor() {
      this.signal = new AbortSignal();
    }

    abort(reason) {
      this.signal._signalAbort(reason);
    }
  }

  // ---- fetch ---------------------------------------------------------------
  function fetch(input, init) {
    return new Promise((resolve, reject) => {
      let request;
      try {
        request = new Request(input, init);
      } catch (e) {
        reject(e);
        return;
      }

      const signal = request.signal;
      if (signal && signal.aborted) {
        reject(abortError(signal.reason));
        return;
      }

      const body = request[BODY];
      let call;
      try {
        call = native.send(
          request.method,
          request.url,
          request.headers._pairs(),
          body,
          request.redirect
        );
      } catch (e) {
        reject(e);
        return;
      }

      let settled = false;
      let onAbort = null;
      if (signal) {
        onAbort = () => {
          if (settled) return;
          settled = true;
          try {
            call.cancel();
          } catch (e) {}
          reject(abortError(signal.reason));
        };
        signal.addEventListener("abort", onAbort);
      }

      call.promise.then(
        (result) => {
          if (settled) return;
          settled = true;
          if (signal && onAbort) signal.removeEventListener("abort", onAbort);
          try {
            resolve(responseFromNative(result));
          } catch (e) {
            reject(e);
          }
        },
        (err) => {
          if (settled) return;
          settled = true;
          if (signal && onAbort) signal.removeEventListener("abort", onAbort);
          reject(err);
        }
      );
    });
  }

  // ---- Publish -------------------------------------------------------------
  globalThis.fetch = fetch;
  globalThis.Headers = Headers;
  globalThis.Request = Request;
  globalThis.Response = Response;
  globalThis.AbortController = AbortController;
  globalThis.AbortSignal = AbortSignal;
  if (typeof globalThis.DOMException !== "function") {
    globalThis.DOMException = DOMException;
  }

  // Hide the native bridge from casual enumeration.
  try {
    delete globalThis.__gojs_fetch;
  } catch (e) {}
  native.__hidden = true;
})();
