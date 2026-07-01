// Package corpus runs external regular-expression corpora (rust-regex, RE2,
// PCRE) through the jsregexp engine as a robustness sweep. These corpora test
// other regex dialects, so they are NOT a conformance oracle for ECMAScript
// (Test262 is). What they are good for is dialect-agnostic safety: a regex
// engine must never panic, hang, or run unbounded on any input — valid or not.
//
// The sweep feeds every extracted pattern to jsregexp.Compile and, when it
// compiles, matches it against a set of adversarial subject strings under a step
// budget and a wall-clock deadline. It records panics (real bugs) and matches
// that fail to return within the deadline (unbounded/uncancellable work — the
// class of defect behind the earlier OOM). Compile errors are expected and
// benign (mostly non-ECMAScript syntax).
//
// Opt-in: set GOJS_CORPUS=1. Corpora live under reference/regexp2/testdata/corpus.
package corpus

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/iceisfun/gojs/jsregexp"
)

const corpusRoot = "../../reference/regexp2/testdata/corpus"

// subjects are the strings every compiled pattern is matched against.
var subjects = []string{
	"", "a", "abc", "aaaaaaaaaaaaaaaaaaaa!",
	strings.Repeat("a", 200) + "!",
	"The quick brown fox\njumps over\r\nthe lazy dog",
	"café é́ 🎉 \U0001F600 test",
	"()[]{}\\^$.|?*+", "\x00\x01\x02", strings.Repeat("ab", 500),
}

// matchOutcome runs one match in a goroutine bounded by a wall deadline so an
// unbounded/uncancellable engine bug surfaces as a hang rather than freezing the
// test. Returns "" on success, or a description of a panic/hang.
func matchOutcome(re *jsregexp.Regexp, subject string) string {
	type res struct {
		panicked any
	}
	done := make(chan res, 1)
	go func() {
		var r res
		defer func() {
			if p := recover(); p != nil {
				r.panicked = p
			}
			done <- r
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = re.FindStringSubmatchIndex(ctx, subject, 0)
	}()
	select {
	case r := <-done:
		if r.panicked != nil {
			return "PANIC: " + trunc(sprint(r.panicked))
		}
		return ""
	case <-time.After(4 * time.Second):
		return "HANG: match did not return within 4s (budget/ctx ignored)"
	}
}

func compileOutcome(pattern, flags string) (re *jsregexp.Regexp, panicMsg string) {
	defer func() {
		if p := recover(); p != nil {
			panicMsg = "COMPILE-PANIC: " + trunc(sprint(p))
		}
	}()
	r, err := jsregexp.Compile(pattern, flags)
	if err != nil {
		return nil, "" // expected: mostly non-ECMAScript syntax
	}
	r.SetStepBudget(5_000_000)
	return r, ""
}

func TestCorpusRobustness(t *testing.T) {
	if os.Getenv("GOJS_CORPUS") == "" {
		t.Skip("set GOJS_CORPUS=1 to run the external-corpus robustness sweep")
	}
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("corpus not present at %s", corpusRoot)
	}

	patterns := collectPatterns()
	t.Logf("collected %d patterns across corpora", len(patterns))

	var compiled, compileErr, badCompile, badMatch int
	var problems []string
	for _, p := range patterns {
		for _, flags := range []string{"", "u"} {
			re, pm := compileOutcome(p.pat, flags)
			if pm != "" {
				badCompile++
				if len(problems) < 60 {
					problems = append(problems, pm+"  /"+trunc(p.pat)+"/"+flags+"  ("+p.src+")")
				}
				continue
			}
			if re == nil {
				compileErr++
				continue
			}
			compiled++
			for _, s := range subjects {
				if bad := matchOutcome(re, s); bad != "" {
					badMatch++
					if len(problems) < 60 {
						problems = append(problems, bad+"  /"+trunc(p.pat)+"/"+flags+"  on "+trunc(s)+"  ("+p.src+")")
					}
					break
				}
			}
		}
	}

	t.Logf("compiled ok: %d   compile-error (expected, dialect): %d", compiled, compileErr)
	t.Logf("PROBLEMS  compile-panics: %d   match panics/hangs: %d", badCompile, badMatch)
	for _, pr := range problems {
		t.Logf("  %s", pr)
	}
	if badCompile > 0 || badMatch > 0 {
		t.Errorf("engine crashed/hung on %d pattern(s) — a regex engine must never panic or hang", badCompile+badMatch)
	}
}

type patSrc struct {
	pat string
	src string
}

func collectPatterns() []patSrc {
	var out []patSrc
	// rust-regex TOML: regex = '...'  or  regex = "..."
	reSingle := regexp.MustCompile(`^regex = '(.*)'\s*$`)
	reDouble := regexp.MustCompile(`^regex = "(.*)"\s*$`)
	walk := func(dir, tag string, fn func(line string) (string, bool)) {
		_ = filepath.Walk(filepath.Join(corpusRoot, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, line := range strings.Split(string(data), "\n") {
				if pat, ok := fn(line); ok {
					out = append(out, patSrc{pat: pat, src: tag + ":" + filepath.Base(path)})
				}
			}
			return nil
		})
	}

	walk("rust-regex", "rust", func(line string) (string, bool) {
		if m := reSingle.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
		if m := reDouble.FindStringSubmatch(line); m != nil {
			return tomlUnescape(m[1]), true
		}
		return "", false
	})
	// RE2 basic.dat: FLAGS<tab>PATTERN<tab>INPUT<tab>CAPTURES
	walk("re2", "re2", func(line string) (string, bool) {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "NOTE") {
			return "", false
		}
		f := strings.Split(line, "\t")
		if len(f) >= 2 && f[1] != "" {
			return f[1], true
		}
		return "", false
	})
	// PCRE testoutput: /pattern/flags on its own line.
	rePcre := regexp.MustCompile(`^/(.*)/[a-zA-Z]*\s*$`)
	walk("pcre", "pcre", func(line string) (string, bool) {
		if m := rePcre.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
		return "", false
	})
	return out
}

func tomlUnescape(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\r`, "\r", `\\`, `\`, `\"`, `"`)
	return r.Replace(s)
}

func trunc(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

func sprint(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return "non-string panic"
}
