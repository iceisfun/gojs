// Package doctest runs end-to-end JavaScript programs through the full gojs
// stack (lexer → parser → interpreter) and asserts on their console output.
//
// Each .js file under testdata/ has a companion "// expect:" prologue: every
// leading line comment beginning with "// expect: " contributes one expected
// output line, in order. The harness runs the script with a capturing
// PrintProvider and compares the captured console.log lines to the expected
// set. This exercises the provider-gated output path in addition to language
// semantics.
package doctest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/iceisfun/gojs"
)

// capturePrinter records console output for assertions.
type capturePrinter struct {
	mu    sync.Mutex
	lines []string
}

func (c *capturePrinter) Print(_ context.Context, msg string) {
	c.mu.Lock()
	c.lines = append(c.lines, msg)
	c.mu.Unlock()
}

func (c *capturePrinter) Warn(ctx context.Context, msg string) { c.Print(ctx, msg) }

// expectedLines extracts the "// expect: X" prologue lines from source.
func expectedLines(src string) []string {
	var want []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "// expect:") {
			want = append(want, strings.TrimSpace(strings.TrimPrefix(trimmed, "// expect:")))
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		break // prologue ends at the first non-comment line
	}
	return want
}

func TestDoctests(t *testing.T) {
	files, err := filepath.Glob("testdata/*.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no doctest fixtures found")
	}
	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			src := string(data)
			want := expectedLines(src)

			printer := &capturePrinter{}
			vm := gojs.New(
				gojs.WithPrintProvider(printer),
				gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
				gojs.WithTimeProvider(gojs.NewDefaultTimeProvider()),
			)
			defer vm.Close()

			if _, err := vm.RunString(filepath.Base(file), src); err != nil {
				t.Fatalf("run error: %v", err)
			}

			if len(printer.lines) != len(want) {
				t.Fatalf("got %d output lines, want %d\n got:  %q\n want: %q",
					len(printer.lines), len(want), printer.lines, want)
			}
			for idx := range want {
				if printer.lines[idx] != want[idx] {
					t.Errorf("line %d = %q, want %q", idx, printer.lines[idx], want[idx])
				}
			}
		})
	}
}
