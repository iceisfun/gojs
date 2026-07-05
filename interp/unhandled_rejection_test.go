package interp

import "testing"

// TestUnhandledRejectionTracking covers HostPromiseRejectionTracker-style
// bookkeeping exposed via TakeUnhandledRejections: a promise that rejects with no
// handler is reported; one that gains a handler (synchronously or in a later job)
// is not; and a fulfilled promise never is.
func TestUnhandledRejectionTracking(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"bare reject", `Promise.reject(new Error("boom"));`, 1},
		{"sync catch", `Promise.reject(new Error("h")).catch(function () {});`, 0},
		{"sync then with reject handler", `Promise.reject(1).then(null, function () {});`, 0},
		{
			"handled in a later job",
			`var p = Promise.reject(2); Promise.resolve().then(function () { p.catch(function () {}); });`,
			0,
		},
		{"fulfilled never reports", `Promise.resolve(1).then(function () {});`, 0},
		{"two independent unhandled", `Promise.reject(1); Promise.reject(2);`, 2},
		{
			"then without reject handler leaves derived unhandled",
			// The source is handled (a then is attached), but the rejection
			// propagates to the derived promise, which is itself unhandled.
			`Promise.reject(new Error("x")).then(function () {});`,
			1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := New()
			defer vm.Close()
			if _, err := vm.RunString("t.js", tc.src); err != nil {
				t.Fatalf("run: %v", err)
			}
			got := vm.TakeUnhandledRejections()
			if len(got) != tc.want {
				t.Errorf("unhandled = %d, want %d", len(got), tc.want)
			}
			// The log is cleared after collection.
			if again := vm.TakeUnhandledRejections(); len(again) != 0 {
				t.Errorf("second take = %d, want 0 (log cleared)", len(again))
			}
		})
	}
}

// TestUnhandledRejectionReason checks that the collected value is the rejection
// reason itself (so a host can render it).
func TestUnhandledRejectionReason(t *testing.T) {
	vm := New()
	defer vm.Close()
	if _, err := vm.RunString("t.js", `Promise.reject(new TypeError("nope"));`); err != nil {
		t.Fatal(err)
	}
	got := vm.TakeUnhandledRejections()
	if len(got) != 1 {
		t.Fatalf("want 1 rejection, got %d", len(got))
	}
	s, err := vm.ToString(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if s != "TypeError: nope" {
		t.Errorf("reason = %q, want %q", s, "TypeError: nope")
	}
}
