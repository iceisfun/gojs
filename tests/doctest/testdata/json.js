// expect: {"name":"gojs","tags":["a","b"],"n":3}
// expect: hello
const obj = { name: "gojs", tags: ["a", "b"], n: 3 };
console.log(JSON.stringify(obj));
console.log(JSON.parse('{"msg":"hello"}').msg);
