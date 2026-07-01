package interp

import (
	"math"
	"sort"
	"strconv"
)

// nan returns a quiet NaN.
func nan() float64 { return math.NaN() }

// This file holds small internal helpers shared across the interpreter.

// sortInts sorts a slice of ints ascending, in place.
func sortInts(a []int) { sort.Ints(a) }

// intToStr formats an int in base 10.
func intToStr(i int) string { return strconv.Itoa(i) }
