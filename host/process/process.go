// Package process installs a Node-like `process` global on a gojs VM. It is a
// thin shim over the VM's capability providers — nothing here touches the host
// directly:
//
//   - process.stdout.write / process.stderr.write route through the VM's
//     PrintProvider (the same sink as console), so an embedder that redirects
//     console output to a log or web app captures process output too. Writes are
//     line-buffered (the PrintProvider is line-oriented); a trailing partial
//     line is flushed on process.exit.
//   - process.exit / cwd / env / platform / arch / pid come from the VM's
//     OsProvider. Without one, those facilities are simply absent — a sandboxed
//     VM cannot exit the host, read its environment, or learn what it runs on.
//   - process.hrtime uses the TimeProvider; process.nextTick uses the microtask
//     queue.
//
// Only argv is supplied here (the invocation); everything else is a capability
// the host grants and can intercept.
package process

import (
	"context"
	"strings"

	"github.com/iceisfun/gojs"
)

type config struct {
	argv []string
}

// Option configures the installed process object.
type Option func(*config)

// WithArgs sets process.argv (the full vector, including argv[0]).
func WithArgs(argv ...string) Option { return func(c *config) { c.argv = argv } }

// Install adds the process global to vm, backed by vm's providers.
func Install(vm *gojs.VM, opts ...Option) error {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.argv == nil {
		cfg.argv = []string{"gojs"}
	}

	ctx := context.Background()
	proc := vm.NewPlainObject()

	// argv / argv0 — the invocation, always present.
	argv := make([]gojs.Value, len(cfg.argv))
	for i, a := range cfg.argv {
		argv[i] = gojs.String(a)
	}
	proc.SetData("argv", vm.NewArray(argv...))
	proc.SetData("argv0", gojs.String(cfg.argv[0]))

	// Node-compat version strings (constants, not host facts).
	proc.SetData("version", gojs.String("v22.0.0"))
	versions := vm.NewPlainObject()
	versions.SetData("node", gojs.String("22.0.0"))
	proc.SetData("versions", versions)

	// stdout / stderr → PrintProvider (line-buffered). Silent without a printer.
	stdout := &lineWriter{}
	stderr := &lineWriter{}
	if printer := vm.PrintProvider(); printer != nil {
		stdout.emit = func(line string) { printer.Print(ctx, line) }
		stderr.emit = func(line string) { printer.Warn(ctx, line) }
	}
	proc.SetData("stdout", writable(vm, stdout))
	proc.SetData("stderr", writable(vm, stderr))

	// nextTick(fn, ...args) → microtask queue (a core VM capability).
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

	// hrtime([prev]) → TimeProvider. Present only when a clock is granted.
	if clock := vm.TimeProvider(); clock != nil {
		base := clock.Monotonic(ctx)
		proc.SetData("hrtime", vm.NewFunction("hrtime", func(args []gojs.Value) (gojs.Value, error) {
			ns := int64((clock.Monotonic(ctx) - base) * 1e6)
			if len(args) > 0 {
				if prev, ok := vm.ToGo(args[0]).([]any); ok && len(prev) == 2 {
					ps, _ := prev[0].(float64)
					pn, _ := prev[1].(float64)
					ns -= int64(ps)*1e9 + int64(pn)
				}
			}
			return vm.NewArray(gojs.Number(float64(ns/1e9)), gojs.Number(float64(ns%1e9))), nil
		}))
	}

	// OS facilities → OsProvider. Present only when granted; a sandbox omits them.
	if osp := vm.OsProvider(); osp != nil {
		env := vm.NewPlainObject()
		for k, v := range osp.Environ(ctx) {
			env.SetData(k, gojs.String(v))
		}
		proc.SetData("env", env)
		proc.SetData("platform", gojs.String(osp.Platform()))
		proc.SetData("arch", gojs.String(osp.Arch()))
		proc.SetData("pid", gojs.Number(float64(osp.Pid())))

		proc.SetData("exit", vm.NewFunction("exit", func(args []gojs.Value) (gojs.Value, error) {
			code := 0
			if len(args) > 0 {
				if n, ok := args[0].(gojs.Number); ok {
					code = int(n)
				}
			}
			stdout.flush()
			stderr.flush()
			osp.Exit(ctx, code)
			return gojs.Undefined, nil
		}))
		proc.SetData("cwd", vm.NewFunction("cwd", func([]gojs.Value) (gojs.Value, error) {
			dir, err := osp.Cwd(ctx)
			if err != nil {
				return gojs.String(""), nil
			}
			return gojs.String(dir), nil
		}))
	}

	vm.SetGlobal("process", proc)
	return nil
}

// lineWriter adapts a byte-stream write() to the line-oriented PrintProvider: it
// emits each complete line (without the newline) and holds a trailing partial
// line until the next newline or an explicit flush.
type lineWriter struct {
	buf  strings.Builder
	emit func(line string) // nil = discard
}

func (w *lineWriter) write(s string) {
	if w.emit == nil {
		return
	}
	w.buf.WriteString(s)
	content := w.buf.String()
	for {
		i := strings.IndexByte(content, '\n')
		if i < 0 {
			break
		}
		w.emit(content[:i])
		content = content[i+1:]
	}
	w.buf.Reset()
	w.buf.WriteString(content)
}

func (w *lineWriter) flush() {
	if w.emit == nil || w.buf.Len() == 0 {
		return
	}
	w.emit(w.buf.String())
	w.buf.Reset()
}

func writable(vm *gojs.VM, w *lineWriter) *gojs.Object {
	o := vm.NewPlainObject()
	o.SetData("write", vm.NewFunction("write", func(args []gojs.Value) (gojs.Value, error) {
		if len(args) > 0 {
			s, err := vm.ToString(args[0])
			if err != nil {
				return nil, err
			}
			w.write(s)
		}
		return gojs.True, nil
	}))
	return o
}
