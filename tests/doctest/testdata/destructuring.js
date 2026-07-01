// expect: 1 2 3
// expect: 10 20
const [a, b, c] = [1, 2, 3];
console.log(a, b, c);
const { x = 10, y = 20 } = { x: 10 };
console.log(x, y);
