package main

import "testing"

// TestParseRunArgs covers the runner's argument parsing: --permissive/-p is a
// runner flag ONLY before the entry file; anything after the file (including a
// literal "--permissive") is a script argument.
func TestParseRunArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantFile   string
		wantPerm   bool
		wantScript []string
	}{
		{"flag before file", []string{"--permissive", "app.js"}, "app.js", true, nil},
		{"short flag before file", []string{"-p", "app.js"}, "app.js", true, nil},
		{"no flag", []string{"app.js", "a", "b"}, "app.js", false, []string{"a", "b"}},
		{
			"flag after file is a script arg",
			[]string{"app.js", "--permissive", "x"},
			"app.js", false, []string{"--permissive", "x"},
		},
		{
			"flag before file, more script args after",
			[]string{"--permissive", "app.js", "--permissive", "y"},
			"app.js", true, []string{"--permissive", "y"},
		},
		{"empty", nil, "", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, perm, script := parseRunArgs(tc.args)
			if file != tc.wantFile {
				t.Errorf("file = %q, want %q", file, tc.wantFile)
			}
			if perm != tc.wantPerm {
				t.Errorf("permissive = %v, want %v", perm, tc.wantPerm)
			}
			if len(script) != len(tc.wantScript) {
				t.Fatalf("scriptArgs = %v, want %v", script, tc.wantScript)
			}
			for i := range script {
				if script[i] != tc.wantScript[i] {
					t.Errorf("scriptArgs[%d] = %q, want %q", i, script[i], tc.wantScript[i])
				}
			}
		})
	}
}

// TestEntryRequireSource covers that the generated bootstrap require() is a
// well-formed JS string literal even when the filename contains characters that
// would otherwise break the source (quotes, backslashes).
func TestEntryRequireSource(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"app.js", `require("./app.js")`},
		{"odd'name.js", `require("./odd'name.js")`},
		{`we"ird.js`, `require("./we\"ird.js")`},
		{`back\slash.js`, `require("./back\\slash.js")`},
	}
	for _, tc := range cases {
		if got := entryRequireSource(tc.base); got != tc.want {
			t.Errorf("entryRequireSource(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}
