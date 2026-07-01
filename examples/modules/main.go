// Example: intercepting require() with a ModuleProvider.
//
// A host serves module source from its own storage — here an in-memory map,
// standing in for a game's data files, a pak archive, or a virtual filesystem.
// The engine never touches the real filesystem; every require() goes through the
// provider. Modules use CommonJS conventions (module.exports / exports /
// require), including relative specifiers and caching (a module runs once).
package main

import (
	"log"

	"github.com/iceisfun/gojs"
)

func main() {
	// The host's "data files": module id -> JavaScript source. In a real game
	// these would be loaded from disk/assets on demand inside a custom
	// ModuleProvider; MapModuleProvider is the in-memory shortcut.
	assets := map[string]string{
		"lib/vec.js": `
			exports.add = (a, b) => ({ x: a.x + b.x, y: a.y + b.y });
			exports.len = (v) => Math.hypot(v.x, v.y);
		`,
		"entity.js": `
			const vec = require("./lib/vec.js");
			// module.exports = ... replaces the whole exports object.
			module.exports = function makeEntity(x, y) {
				return {
					pos: { x, y },
					moveBy(dx, dy) { this.pos = vec.add(this.pos, { x: dx, y: dy }); },
					distanceFromOrigin() { return vec.len(this.pos); },
				};
			};
		`,
	}

	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
		gojs.WithModuleProvider(gojs.NewMapModuleProvider(assets)),
	)
	defer vm.Close()

	_, err := vm.RunString("main.js", `
		const makeEntity = require("entity.js");
		const e = makeEntity(3, 4);
		console.log("start distance:", e.distanceFromOrigin());   // 5
		e.moveBy(3, 0);
		console.log("after move:", e.pos.x, e.pos.y, "->", e.distanceFromOrigin());

		// require() is cached: the same module object comes back each time.
		const vecA = require("./lib/vec.js");
		const vecB = require("lib/vec.js");
		console.log("cached module identity:", vecA === vecB);
	`)
	if err != nil {
		log.Fatalf("run error: %v", err)
	}
}
