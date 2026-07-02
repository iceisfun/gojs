package process

import (
	"context"
	"strings"
	"testing"

	"github.com/iceisfun/gojs"
)

// capturePrinter records everything written through the PrintProvider, proving
// process.stdout/stderr route through the same sink as console.
type capturePrinter struct{ out *strings.Builder }

func (c capturePrinter) Print(_ context.Context, msg string) { c.out.WriteString(msg + "\n") }
func (c capturePrinter) Warn(_ context.Context, msg string)  { c.out.WriteString("ERR:" + msg + "\n") }

// fakeOs is an OsProvider that records exit and serves a fixed environment,
// standing in for a host that walls off and redirects OS access.
type fakeOs struct {
	env      map[string]string
	exitCode *int
}

func (f *fakeOs) Getenv(_ context.Context, name string) (string, bool) { v, ok := f.env[name]; return v, ok }
func (f *fakeOs) Environ(context.Context) map[string]string           { return f.env }
func (f *fakeOs) Cwd(context.Context) (string, error)                 { return "/app", nil }
func (f *fakeOs) Exit(_ context.Context, code int)                    { *f.exitCode = code }
func (f *fakeOs) Platform() string                                    { return "testos" }
func (f *fakeOs) Arch() string                                        { return "testarch" }
func (f *fakeOs) Pid() int                                            { return 4242 }

func TestProcess(t *testing.T) {
	var out strings.Builder
	exitCode := -1
	fos := &fakeOs{env: map[string]string{"FOO": "bar"}, exitCode: &exitCode}

	vm := gojs.New(
		gojs.WithPrintProvider(capturePrinter{&out}),
		gojs.WithOsProvider(fos),
	)
	defer vm.Close()
	if err := Install(vm, WithArgs("gojs", "app.js", "--flag")); err != nil {
		t.Fatal(err)
	}

	_, err := vm.RunString("t.js", `
		process.stdout.write("hello ");
		process.stdout.write("world\n");   // line-buffered into one "hello world"
		process.stdout.write("argv=" + process.argv.join(",") + "\n");
		process.stdout.write("env.FOO=" + process.env.FOO + "\n");
		process.stdout.write("platform=" + process.platform + " pid=" + process.pid + "\n");
		process.stdout.write("cwd=" + process.cwd() + "\n");
		process.nextTick(() => process.stdout.write("tick\n"));
		process.exit(3);
	`)
	if err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{
		"hello world", // partial writes joined into one line
		"argv=gojs,app.js,--flag",
		"env.FOO=bar",
		"platform=testos pid=4242",
		"cwd=/app",
		"tick",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if exitCode != 3 {
		t.Errorf("exit code = %d, want 3", exitCode)
	}
}

// TestProcessSandbox verifies OS facilities are absent without an OsProvider —
// a script cannot exit, read env, or learn the platform.
func TestProcessSandbox(t *testing.T) {
	vm := gojs.New() // no providers at all
	defer vm.Close()
	if err := Install(vm); err != nil {
		t.Fatal(err)
	}
	v, err := vm.RunString("t.js", `[
		typeof process.exit,
		typeof process.env,
		typeof process.platform,
		typeof process.cwd,
		Array.isArray(process.argv),
	].join(",")`)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := vm.ToString(v)
	if want := "undefined,undefined,undefined,undefined,true"; s != want {
		t.Errorf("sandbox surface = %q, want %q", s, want)
	}
}
