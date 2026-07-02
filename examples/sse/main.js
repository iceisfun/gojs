// SSE client demo. SERVER_URL is injected by main.go and points at a localhost
// text/event-stream endpoint. We exercise:
//   - onopen / onmessage handler properties
//   - addEventListener for a named "tick" event
//   - onerror, which fires when the server drops the connection and the client
//     transparently reconnects (readyState goes back to CONNECTING)
//   - Last-Event-ID: the server resumes ids across the reconnect, visible here
//     as a monotonically increasing e.lastEventId

console.log("connecting to", SERVER_URL);
const es = new EventSource(SERVER_URL);

es.onopen = () => {
  console.log("[open] readyState =", es.readyState, "(OPEN =", EventSource.OPEN + ")");
};

es.onmessage = (e) => {
  console.log("[message]", e.data, "| lastEventId =", e.lastEventId, "| origin =", e.origin);
};

es.addEventListener("tick", (e) => {
  console.log("[tick] value =", e.data);
});

es.onerror = () => {
  // Non-fatal errors mean the client is reconnecting (readyState CONNECTING).
  console.log("[error] readyState =", es.readyState, "-> reconnecting");
};

// Let the stream run (including one automatic reconnect), then close so the
// process can exit. Requires a timer provider (installed by main.go).
setTimeout(() => {
  console.log("closing EventSource");
  es.close();
  console.log("[closed] readyState =", es.readyState, "(CLOSED =", EventSource.CLOSED + ")");
}, 2500);
