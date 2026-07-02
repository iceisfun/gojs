// Fetch API demo. `BASE` is the URL of the local server started by main.go.
//
// Everything runs inside one async IIFE so we can use await; RunString on the
// Go side drains the event loop until all of this completes.
(async () => {
  // 1. GET, read the body as text.
  const hello = await fetch(BASE + "/hello");
  console.log("GET /hello ->", hello.status, hello.ok ? "OK" : "FAIL");
  console.log("  body:", await hello.text());

  // 2. GET, read the body as JSON.
  const info = await fetch(BASE + "/time");
  const data = await info.json();
  console.log("GET /time ->", "service=" + data.service, "version=" + data.version, "tags=" + data.tags.join(","));
  console.log("  content-type:", info.headers.get("content-type"));

  // 3. POST a JSON body with a custom header; read the echoed JSON back.
  const res = await fetch(BASE + "/echo", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Api-Key": "secret-123",
    },
    body: JSON.stringify({ hello: "world", items: [1, 2, 3] }),
  });
  const echoed = await res.json();
  console.log("POST /echo ->", res.status);
  console.log("  method seen by server:", echoed.method);
  console.log("  api key seen by server:", echoed.apiKey);
  console.log("  echoed payload:", JSON.stringify(echoed.received));

  // 4. Inspect and iterate response headers (case-insensitive, combined values).
  const h = new Headers();
  h.append("Accept", "text/plain");
  h.append("Accept", "application/json");
  console.log("Headers demo -> accept:", h.get("accept"));

  // 5. Error handling: a bad URL rejects with a TypeError.
  try {
    await fetch("http://127.0.0.1:1/definitely-refused");
    console.log("unexpected success");
  } catch (err) {
    console.log("Error handling -> caught", err.name + ":", err.message);
  }

  // 6. Abort: an AbortController rejects the fetch with an AbortError.
  const controller = new AbortController();
  const pending = fetch(BASE + "/hello", { signal: controller.signal });
  controller.abort();
  try {
    await pending;
  } catch (err) {
    console.log("Abort demo -> caught", err.name);
  }

  console.log("Done.");
})();
