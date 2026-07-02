// WebSocket client demo. Uses only the standard WebSocket API that
// host/websocket installs. Go's main.go supplies SERVER_URL and runs an echo
// server that abruptly drops the socket when it receives "please-drop".

// ---- small binary helpers (gojs has no TypedArray sugar, only DataView) ----
function toArrayBuffer(bytes) {
  var ab = new ArrayBuffer(bytes.length);
  var dv = new DataView(ab);
  for (var i = 0; i < bytes.length; i++) dv.setUint8(i, bytes[i]);
  return ab;
}
function fromArrayBuffer(ab) {
  var dv = new DataView(ab);
  var out = [];
  for (var i = 0; i < ab.byteLength; i++) out.push(dv.getUint8(i));
  return out;
}

var MAX_RECONNECTS = 2;
var attempt = 0;

function connect() {
  attempt++;
  console.log("[client] connecting (attempt " + attempt + ") to " + SERVER_URL);
  var ws = new WebSocket(SERVER_URL, ["echo"]);
  ws.binaryType = "arraybuffer";

  // A tiny per-session script so the demo terminates deterministically.
  var firstSession = attempt === 1;
  var got = 0;

  ws.addEventListener("open", function () {
    console.log("[client] open; protocol=" + JSON.stringify(ws.protocol) +
      " readyState=" + ws.readyState);
    ws.send("hello, server");
    ws.send(toArrayBuffer([0x67, 0x6f, 0x6a, 0x73])); // "gojs"
  });

  ws.onmessage = function (e) {
    got++;
    if (typeof e.data === "string") {
      console.log("[client] text echo: " + JSON.stringify(e.data));
    } else {
      console.log("[client] binary echo: [" + fromArrayBuffer(e.data).join(", ") + "]");
    }
    if (got === 2) {
      if (firstSession) {
        // Force a mid-session disconnect to exercise reconnect.
        console.log("[client] asking server to drop the connection...");
        ws.send("please-drop");
      } else {
        // Second session: graceful shutdown.
        console.log("[client] done; closing cleanly");
        ws.close(1000, "bye");
      }
    }
  };

  ws.onerror = function (e) {
    console.log("[client] error: " + e.message);
  };

  ws.onclose = function (e) {
    console.log("[client] close code=" + e.code + " reason=" +
      JSON.stringify(e.reason) + " wasClean=" + e.wasClean);
    if (!e.wasClean && attempt <= MAX_RECONNECTS) {
      var delay = 100 * attempt; // linear backoff
      console.log("[client] reconnecting in " + delay + "ms");
      setTimeout(connect, delay);
    } else {
      console.log("[client] shutdown complete");
    }
  };
}

connect();
