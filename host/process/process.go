// Package process installs a Node-like `process` global on a gojs VM. It is a
// host capability — like the other host/* packages, it is opt-in — intended for
// standalone runners and scripts that expect a minimal Node environment
// (process.argv, process.env, process.stdout.write, process.exit, …).
//
// It is a compatibility shim, not a Node runtime: only a small, commonly used
// surface is provided, and time/exit/IO are host-controlled through options so
// an embedder can sandbox them.
package process

import (
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/iceisfun/gojs"
)

type config struct {
	argv   []string
	env    map[string]string
	stdout io.Writer
	stderr io.Writer
	exit   func(int)
	cwd    func() (string, error)
}

// Option configures the installed process object.
type Option func(*config)

// WithArgs sets process.argv (the full vector, including argv[0]).
func WithArgs(argv ...string) Option { return func(c *config) { c.argv = argv } }

// WithEnv sets process.env. A nil map means the OS environment.
func WithEnv(env map[string]string) Option { return func(c *config) { c.env = env } }

// WithStdout redirects process.stdout.write.
func WithStdout(w io.Writer) Option { return func(c *config) { c.stdout = w } }

// WithStderr redirects process.stderr.write.
func WithStderr(w io.Writer) Option { return func(c *config) { c.stderr = w } }

// WithExit overrides process.exit. The default calls os.Exit; an embedder that
// must not let a script kill the host should override it (e.g. to record the
// code and cancel the VM's context).
func WithExit(fn func(int)) Option { return func(c *config) { c.exit = fn } }

// WithCwd overrides process.cwd().
func WithCwd(fn func() (string, error)) Option { return func(c *config) { c.cwd = fn } }

// Install adds the process global to vm.
func Install(vm *gojs.VM, opts ...Option) error {
	cfg := &config{
		stdout: os.Stdout,
		stderr: os.Stderr,
		exit:   os.Exit,
		cwd:    os.Getwd,
	}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.argv == nil {
		cfg.argv = []string{"gojs"}
	}
	if cfg.env == nil {
		cfg.env = osEnv()
	}

	proc := vm.NewPlainObject()

	// argv / argv0
	argv := make([]gojs.Value, len(cfg.argv))
	for i, a := range cfg.argv {
		argv[i] = gojs.String(a)
	}
	proc.SetData("argv", vm.NewArray(argv...))
	if len(cfg.argv) > 0 {
		proc.SetData("argv0", gojs.String(cfg.argv[0]))
	}

	// env
	env := vm.NewPlainObject()
	for k, v := range cfg.env {
		env.SetData(k, gojs.String(v))
	}
	proc.SetData("env", env)

	// Static host facts.
	proc.SetData("platform", gojs.String(goosToPlatform(runtime.GOOS)))
	proc.SetData("arch", gojs.String(goarchToArch(runtime.GOARCH)))
	proc.SetData("pid", gojs.Number(float64(os.Getpid())))
	proc.SetData("version", gojs.String("v22.0.0")) // Node-compat version string
	versions := vm.NewPlainObject()
	versions.SetData("node", gojs.String("22.0.0"))
	proc.SetData("versions", versions)

	// stdout / stderr: { write(chunk) -> true }
	proc.SetData("stdout", writable(vm, cfg.stdout))
	proc.SetData("stderr", writable(vm, cfg.stderr))

	// exit([code])
	proc.SetData("exit", vm.NewFunction("exit", func(args []gojs.Value) (gojs.Value, error) {
		code := 0
		if len(args) > 0 {
			if n, ok := args[0].(gojs.Number); ok {
				code = int(n)
			}
		}
		cfg.exit(code)
		return gojs.Undefined, nil
	}))

	// cwd()
	proc.SetData("cwd", vm.NewFunction("cwd", func([]gojs.Value) (gojs.Value, error) {
		dir, err := cfg.cwd()
		if err != nil {
			return gojs.String(""), nil
		}
		return gojs.String(dir), nil
	}))

	// nextTick(fn, ...args) — schedule fn on the microtask queue.
	proc.SetData("nextTick", vm.NewFunction("nextTick", func(args []gojs.Value) (gojs.Value, error) {
		if len(args) == 0 {
			return gojs.Undefined, nil
		}
		fn := args[0]
		extra := append([]gojs.Value(nil), args[1:]...)
		vm.QueueMicrotask(func() error {
			_, err := vm.Call(fn, gojs.Undefined, extra...)
			return err
		})
		return gojs.Undefined, nil
	}))

	// hrtime([prev]) -> [seconds, nanoseconds], high-resolution monotonic time.
	start := time.Now()
	proc.SetData("hrtime", vm.NewFunction("hrtime", func(args []gojs.Value) (gojs.Value, error) {
		ns := time.Since(start).Nanoseconds()
		if len(args) > 0 {
			if arr, ok := vm.ToGo(args[0]).([]any); ok && len(arr) == 2 {
				ps, _ := arr[0].(float64)
				pn, _ := arr[1].(float64)
				ns -= int64(ps)*1e9 + int64(pn)
			}
		}
		return vm.NewArray(gojs.Number(float64(ns/1e9)), gojs.Number(float64(ns%1e9))), nil
	}))

	vm.SetGlobal("process", proc)
	return nil
}

func writable(vm *gojs.VM, w io.Writer) *gojs.Object {
	o := vm.NewPlainObject()
	o.SetData("write", vm.NewFunction("write", func(args []gojs.Value) (gojs.Value, error) {
		if len(args) > 0 {
			s, err := vm.ToString(args[0])
			if err != nil {
				return nil, err
			}
			io.WriteString(w, s)
		}
		return gojs.True, nil
	}))
	return o
}

func osEnv() map[string]string {
	m := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func goosToPlatform(goos string) string {
	switch goos {
	case "windows":
		return "win32"
	default:
		return goos // linux, darwin, freebsd, … match Node
	}
}

func goarchToArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	default:
		return goarch // arm64, arm, … match Node
	}
}
