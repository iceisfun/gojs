// expect: Rex barks
// expect: Generic makes a sound
class Animal {
  constructor(name) { this.name = name; }
  speak() { return `${this.name} makes a sound`; }
}
class Dog extends Animal {
  speak() { return `${this.name} barks`; }
}
console.log(new Dog("Rex").speak());
console.log(new Animal("Generic").speak());
