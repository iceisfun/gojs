// geometry.ts — a TypeScript module imported by main.ts.
//
// Types and interfaces are erased during transpilation; the exported function
// survives as plain JavaScript.

export interface Vector {
  x: number;
  y: number;
}

export function distance(a: Vector, b: Vector): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}
