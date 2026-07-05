package interp

import "testing"

// A rope (the value the + operator produces) is a string primitive, so every
// boundary that accepts a String must also accept it. These tests pin the two
// boundaries that were missing or hand-rolled: the Go-interop ToGo conversion
// and Array.prototype.join.

func TestRopeIsStringPrimitive(t *testing.T) {
	r := concatStrings(String("foo"), String("bar"))
	if _, ok := r.(*vmString); !ok {
		t.Fatalf("concatStrings of two non-empty strings should build a rope, got %T", r)
	}
	if r.Typeof() != "string" {
		t.Errorf("rope Typeof = %q, want \"string\"", r.Typeof())
	}
	// An empty operand must collapse rather than wrap "" in a rope node.
	if v := concatStrings(String(""), String("x")); v != Value(String("x")) {
		t.Errorf("concatStrings(\"\",\"x\") = %#v, want String(\"x\")", v)
	}
}

func TestRopeToGo(t *testing.T) {
	i := New()
	// Directly: a rope handed to the host boundary must flatten, not become nil.
	r := concatStrings(concatStrings(String("a"), String("b")), String("c"))
	if got := i.ToGo(r); got != "abc" {
		t.Errorf("ToGo(rope) = %#v, want \"abc\"", got)
	}
	// Through the engine: `+` yields a rope; converting the result to Go must
	// give the string value, not nil (regression for the missing *strRope arm).
	v, err := i.RunString("main", `"foo" + "bar" + "baz"`)
	if err != nil {
		t.Fatal(err)
	}
	if got := i.ToGo(v); got != "foobarbaz" {
		t.Errorf("ToGo(RunString \"+\") = %#v, want \"foobarbaz\"", got)
	}
}

func TestArrayJoinCorrect(t *testing.T) {
	i := New()
	cases := map[string]string{
		`[1,2,3].join("-")`:             "1-2-3",
		`["a","b","c"].join("")`:        "abc",
		`[].join(",")`:                  "",
		`[1,null,2,undefined,3].join()`: "1,,2,,3",
		`["x"].join("SEP")`:             "x",
		// A high surrogate at the end of one element and a low surrogate at the
		// start of the next must coalesce to a canonical astral scalar.
		`["\uD83D","\uDE00"].join("")`: "\U0001F600",
	}
	for src, want := range cases {
		v, err := i.RunString("main", src)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got := string(v.(String)); got != want {
			t.Errorf("%s = %q, want %q", src, got, want)
		}
	}
}
