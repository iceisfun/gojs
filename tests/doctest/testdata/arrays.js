// expect: 2,4,6
// expect: 12
// expect: [ 1, 2, 3 ]
const xs = [1, 2, 3];
console.log(xs.map(x => x * 2).join(","));
console.log(xs.map(x => x * 2).reduce((a, b) => a + b, 0));
console.log([...xs]);
