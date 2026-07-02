package interp

import "context"

// defineSpeciesGetter installs the well-known get [Symbol.species] accessor on a
// constructor. Per spec the getter simply returns the this value so that
// subclasses inherit the base constructor for SpeciesConstructor, and it is a
// non-enumerable, configurable accessor named "get [Symbol.species]"
// (e.g. §23.1.2.2 for Map, §23.2.2.2 for Set, §27.2.4.7 for Promise).
func (i *Interpreter) defineSpeciesGetter(ctor *Object) {
	get := i.newNativeFunc("get [Symbol.species]", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return this, nil
	})
	ctor.defineOwn(SymKey(i.symSpecies), &Property{Get: get, Accessor: true, Configurable: true})
}
