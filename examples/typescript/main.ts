// main.ts — the entry module.
//
// Exercises a spread of TypeScript-only syntax (import, interface, class with
// visibility modifiers and parameter properties, generics, typed arrow) — all
// of which is stripped/lowered to JavaScript before gojs runs it.

import { Vector, distance } from "./geometry";

enum Axis {
  X,
  Y,
}

interface Named {
  name: string;
}

class Point implements Named {
  constructor(public name: string, private v: Vector) {}

  from(origin: Vector): number {
    return distance(this.v, origin);
  }

  vector(): Vector {
    return this.v;
  }
}

// A generic helper: pick the item whose vector is nearest a reference point.
function nearest<T extends Named>(items: T[], vec: (t: T) => Vector, ref: Vector): T {
  return items.reduce((best, it) =>
    distance(vec(it), ref) < distance(vec(best), ref) ? it : best,
  );
}

const origin: Vector = { x: 0, y: 0 };
const points: Point[] = [
  new Point("a", { x: 3, y: 4 }),
  new Point("b", { x: 6, y: 8 }),
  new Point("c", { x: 1, y: 1 }),
];

for (const p of points) {
  console.log(`${p.name}: distance ${p.from(origin)}`);
}

console.log("nearest to origin:", nearest(points, (p) => p.vector(), origin).name);
console.log("axes:", Axis[Axis.X], Axis[Axis.Y]); // enum reverse mapping
