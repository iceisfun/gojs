package interp

import (
	"context"
	"strings"
)

// initConsole installs the console global. All output is routed through the
// interpreter's PrintProvider; when none is configured, console methods are
// inert (this keeps the default sandbox from writing to the host's stdout).
func (i *Interpreter) initConsole() {
	console := NewObject(i.objectProto)

	logFn := func(warn bool) CallFn {
		return func(ctx context.Context, this Value, args []Value) (Value, error) {
			if i.printer == nil {
				return Undef, nil
			}
			msg, err := i.formatConsole(ctx, args)
			if err != nil {
				return nil, err
			}
			if warn {
				i.printer.Warn(ctx, msg)
			} else {
				i.printer.Print(ctx, msg)
			}
			return Undef, nil
		}
	}

	for _, name := range []string{"log", "info", "debug", "trace"} {
		i.defineMethod(console, name, 0, logFn(false))
	}
	for _, name := range []string{"warn", "error"} {
		i.defineMethod(console, name, 0, logFn(true))
	}
	// dir/assert/group are thin aliases for a first pass.
	i.defineMethod(console, "dir", 0, logFn(false))
	i.defineMethod(console, "assert", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if len(args) > 0 && ToBoolean(args[0]) {
			return Undef, nil
		}
		if i.printer != nil {
			msg, err := i.formatConsole(ctx, args[min(1, len(args)):])
			if err != nil {
				return nil, err
			}
			i.printer.Warn(ctx, "Assertion failed"+ifNonEmpty(": ", msg))
		}
		return Undef, nil
	})

	i.setGlobalHidden("console", console)
}

// formatConsole space-joins arguments using the display representation used by
// console.log (strings verbatim, everything else via inspect).
func (i *Interpreter) formatConsole(ctx context.Context, args []Value) (string, error) {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		s, err := i.inspect(ctx, a, map[*Object]bool{}, false)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " "), nil
}

// ifNonEmpty returns sep+s when s is non-empty, else "".
func ifNonEmpty(sep, s string) string {
	if s == "" {
		return ""
	}
	return sep + s
}
