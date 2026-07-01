// This script exercises the console and timer capabilities. Its output is
// captured by the host's custom PrintProvider rather than written to stdout
// directly, and the timers run on the interpreter's event loop.
console.log("starting");

let ticks = 0;
const id = setInterval(() => {
  ticks++;
  console.log("tick", ticks);
  if (ticks >= 3) clearInterval(id);
}, 5);

setTimeout(() => console.log("timeout fired"), 20);

Promise.resolve("promised").then((v) => console.log("microtask:", v));

console.log("synchronous end");
