package websocket

import _ "embed"

// preludeJS is the JavaScript that defines globalThis.WebSocket on top of the
// hidden __gojs_ws native bridge. It is loaded once by Install.
//
//go:embed prelude.js
var preludeJS string
