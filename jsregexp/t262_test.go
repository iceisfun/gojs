package jsregexp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This harness scores the parser against the parse-phase slice of Test262's
// regular-expression tests. It targets language/literals/regexp, whose tests
// carry a bare RegExp literal (/pattern/flags) that can be extracted without
// executing JavaScript, and whose frontmatter declares whether the literal must
// parse or must be a SyntaxError. It measures the parser only; runtime matching
// is scored once the VM and interpreter integration land.
//
// Set GOJS_REGEXP_T262_VERBOSE=1 to print each disagreement.

const t262Root = "../reference/test262"

var (
	// A regex literal on its own line: /pattern/flags; (optional trailing ';').
	reLiteralLine = regexp.MustCompile(`^\s*/(.+)/([dgimsuvy]*)\s*;?\s*$`)
	reNegative    = regexp.MustCompile(`(?m)^negative:`)
	rePhase       = regexp.MustCompile(`(?m)^\s*phase:\s*(\w+)`)
	reType        = regexp.MustCompile(`(?m)^\s*type:\s*(\w+)`)
)

func TestT262RegexpLiterals(t *testing.T) {
	dir := filepath.Join(t262Root, "test/language/literals/regexp")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("test262 not present at %s", t262Root)
	}

	var pass, fail, skip int
	var fails []string
	verbose := os.Getenv("GOJS_REGEXP_T262_VERBOSE") != ""

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".js") ||
			strings.HasSuffix(path, "_FIXTURE.js") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(src)

		front := frontmatter(text)
		wantErr, ok := parsePhaseExpectation(front)
		if !ok {
			skip++ // not a clean parse-phase test (e.g. runtime negative)
			return nil
		}
		pat, flags, found := extractLiteral(text)
		if !found {
			skip++ // literal not extractable without executing JS
			return nil
		}

		_, cerr := Compile(pat, flags)
		gotErr := cerr != nil
		switch {
		case gotErr == wantErr:
			pass++
		default:
			fail++
			if verbose || len(fails) < 40 {
				verb := "should reject"
				if !wantErr {
					verb = "should accept"
				}
				fails = append(fails, filepath.Base(path)+": "+verb+"  /"+pat+"/"+flags)
			}
		}
		return nil
	})

	total := pass + fail
	rate := 0.0
	if total > 0 {
		rate = 100 * float64(pass) / float64(total)
	}
	t.Logf("Test262 regexp literals: %d pass, %d fail, %d skip  (%.1f%% of runnable)", pass, fail, skip, rate)
	for _, f := range fails {
		t.Logf("  MISS %s", f)
	}
}

// frontmatter returns the YAML block between /*--- and ---*/, or "".
func frontmatter(text string) string {
	i := strings.Index(text, "/*---")
	if i < 0 {
		return ""
	}
	j := strings.Index(text[i:], "---*/")
	if j < 0 {
		return ""
	}
	return text[i : i+j]
}

// parsePhaseExpectation reports whether the test expects a parse-phase
// SyntaxError (wantErr=true) or expects the literal to parse (wantErr=false).
// ok=false means the test is not a clean parse-phase case (skip it).
func parsePhaseExpectation(front string) (wantErr bool, ok bool) {
	if !reNegative.MatchString(front) {
		return false, true // positive test: literal must parse
	}
	phase := submatch(rePhase, front)
	typ := submatch(reType, front)
	if phase != "parse" || typ != "SyntaxError" {
		return false, false // runtime/resolution negative — not a parser test
	}
	return true, true
}

// extractLiteral returns the pattern and flags of the last standalone regex
// literal line (after the frontmatter), if any.
func extractLiteral(text string) (pat, flags string, ok bool) {
	// Start scanning after the frontmatter close to avoid slashes in comments.
	if k := strings.Index(text, "---*/"); k >= 0 {
		text = text[k+len("---*/"):]
	}
	for _, line := range strings.Split(text, "\n") {
		if m := reLiteralLine.FindStringSubmatch(line); m != nil {
			pat, flags, ok = m[1], m[2], true // keep last match
		}
	}
	return pat, flags, ok
}

func submatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}
