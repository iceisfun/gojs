// expect: 1
// expect: 2
// expect: 3
function counter() {
  let n = 0;
  return () => ++n;
}
const c = counter();
console.log(c());
console.log(c());
console.log(c());
