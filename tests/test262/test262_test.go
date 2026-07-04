package test262

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestT262 runs a curated slice of the Test262 suite and reports a pass rate.
// It is skipped under `go test -short` (and thus in the normal suite) because
// it reads thousands of files; run it explicitly with:
//
//	go test ./tests/test262/ -run TestT262 -v
//
// Override the directories with the GOJS_T262_DIRS env var (comma-separated,
// relative to reference/test262/test), e.g.:
//
//	GOJS_T262_DIRS=language/expressions/addition,built-ins/Math go test ./tests/test262 -run TestT262 -v
func TestT262(t *testing.T) {
	// This mining harness reads thousands of files and is opt-in: it runs only
	// when GOJS_T262=1 (or GOJS_T262_DIRS) is set, so a plain `go test ./...`
	// stays fast and does not execute untrusted third-party fixtures.
	if os.Getenv("GOJS_T262") == "" && os.Getenv("GOJS_T262_DIRS") == "" {
		t.Skip("set GOJS_T262=1 (and optionally GOJS_T262_DIRS) to run Test262 mining")
	}
	if testing.Short() {
		t.Skip("skipping Test262 mining under -short")
	}
	if _, err := os.Stat(root); err != nil {
		t.Skipf("test262 checkout not found at %s", root)
	}

	dirs := defaultDirs
	if env := os.Getenv("GOJS_T262_DIRS"); env != "" {
		dirs = strings.Split(env, ",")
	}

	// skips holds test-path substrings to exclude. By default it disables the
	// heavy string-building suites (see slowSkip / wontfix/perf-slow-tests.md);
	// GOJS_T262_SLOW=1 runs them anyway. GOJS_T262_SKIP adds ad-hoc substrings.
	var skips []string
	if os.Getenv("GOJS_T262_SLOW") == "" {
		skips = append(skips, slowSkip...)
	}
	if env := os.Getenv("GOJS_T262_SKIP"); env != "" {
		for _, s := range strings.Split(env, ",") {
			if s = strings.TrimSpace(s); s != "" {
				skips = append(skips, s)
			}
		}
	}
	skipped := 0

	var files []string
	for _, d := range dirs {
		base := filepath.Join(root, "test", strings.TrimSpace(d))
		_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			name := entry.Name()
			// Skip fixture modules (_FIXTURE.js) and non-JS files.
			if !strings.HasSuffix(name, ".js") || strings.Contains(name, "_FIXTURE") {
				return nil
			}
			for _, s := range skips {
				if strings.Contains(path, s) {
					skipped++
					return nil
				}
			}
			files = append(files, path)
			return nil
		})
	}
	sort.Strings(files)
	if skipped > 0 {
		t.Logf("Test262: skipped %d disabled test file(s) (set GOJS_T262_SLOW=1 to include)", skipped)
	}
	if len(files) == 0 {
		t.Skipf("no test files under %v", dirs)
	}

	var pass, fail, skip int
	var failures []Result
	for _, f := range files {
		for _, r := range Run(f) {
			switch r.Outcome {
			case Pass:
				pass++
			case Skip:
				skip++
			case Fail:
				fail++
				if failureSampleSize < 0 || len(failures) < failureSampleSize {
					failures = append(failures, r)
				}
			}
		}
	}

	total := pass + fail
	rate := 0.0
	if total > 0 {
		rate = 100 * float64(pass) / float64(total)
	}
	t.Logf("Test262 [%s]: %d pass, %d fail, %d skip  (%.1f%% of runnable)",
		strings.Join(dirs, ","), pass, fail, skip, rate)

	for _, fr := range failures {
		rel := strings.TrimPrefix(fr.Path, root+"/test/")
		t.Logf("  FAIL [%s] %s: %s", fr.Mode, rel, fr.Reason)
	}
}

// failureSampleSize bounds how many failure lines are printed.
var failureSampleSize = func() int {
	if v := os.Getenv("GOJS_T262_SAMPLES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 40
}()

// slowSkip lists test-path substrings for suites disabled by default because
// their wall-clock is dominated by the tree-walking interpreter building and
// matching very large strings (a performance limit, not a conformance gap — the
// results are correct, they just take ~25 min). This keeps general mining passes
// moving. GOJS_T262_SLOW=1 includes them. See wontfix/perf-slow-tests.md.
var slowSkip = []string{
	"built-ins/RegExp/property-escapes/generated/",
}

// defaultDirs is a focused, high-signal slice of the suite covering areas gojs
// implements. It intentionally avoids sprawling built-in coverage until the
// fundamentals stabilize.
var defaultDirs = []string{
	"language/expressions/addition",
	"language/expressions/subtraction",
	"language/expressions/multiplication",
	"language/expressions/strict-equals",
	"language/expressions/logical-and",
	"language/expressions/logical-or",
	"language/expressions/conditional",
	"language/expressions/typeof",
	"language/statements/if",
	"language/statements/for",
	"language/statements/while",
	"language/statements/switch",
	"built-ins/Math",
	"built-ins/JSON",
	"built-ins/Boolean",
}
