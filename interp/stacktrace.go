package interp

import (
	"fmt"
	"strings"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// maxStackFrames bounds how many frames a captured stack renders.
const maxStackFrames = 32

// stackFrame is one entry in a captured call stack: the function's name and the
// position of the statement currently executing within it.
type stackFrame struct {
	name string
	pos  token.Pos
}

// enterFrame pushes a call frame named name and returns a function that pops it.
// Callers defer the returned function around a function-body execution, so the
// call stack mirrors the active JS call chain.
func (i *Interpreter) enterFrame(name string) func() {
	i.callStack = append(i.callStack, stackFrame{name: name})
	return func() {
		if n := len(i.callStack); n > 0 {
			i.callStack = i.callStack[:n-1]
		}
	}
}

// captureFrames snapshots the current call stack, innermost first, with a final
// <module> frame for top-level code. Frames whose position was never recorded
// (e.g. a throw during parameter binding) are skipped.
func (i *Interpreter) captureFrames() []stackFrame {
	frames := make([]stackFrame, 0, len(i.callStack)+1)
	for j := len(i.callStack) - 1; j >= 0 && len(frames) < maxStackFrames; j-- {
		if i.callStack[j].pos.Line > 0 {
			frames = append(frames, i.callStack[j])
		}
	}
	if i.curPos.Line > 0 && len(frames) < maxStackFrames {
		frames = append(frames, stackFrame{name: "<module>", pos: i.curPos})
	}
	return frames
}

// mapPos applies the SourceMapper to p (when installed and it maps), returning
// the effective source name and 1-based line/column.
func (i *Interpreter) mapPos(p token.Pos) (source string, line, col int) {
	source, line, col = p.Source, p.Line, p.Column
	if i.sourceMapper != nil {
		if os, ol, oc, ok := i.sourceMapper.MapPosition(source, line, col); ok {
			source, line, col = os, ol, oc
		}
	}
	return
}

// funcFrameName returns the name to record for a function call: the function
// object's resolved name (which reflects name inference, e.g. const f = () =>…),
// falling back to the declaration name.
func funcFrameName(fnObj *Object, defName string) string {
	if fnObj != nil && fnObj.fn != nil && fnObj.fn.name != "" {
		return fnObj.fn.name
	}
	return defName
}

// defName returns a function definition's declared name, or "".
func defName(def *ast.FuncDef) string {
	if def != nil && def.Name != nil {
		return def.Name.Name
	}
	return ""
}

// frameName returns a frame's display name, substituting <anonymous> for unnamed
// functions.
func frameName(name string) string {
	if name == "" {
		return "<anonymous>"
	}
	return name
}

// registerSource retains a source's text by name for code-frame rendering.
func (i *Interpreter) registerSource(name, src string) {
	if i.sources == nil {
		i.sources = make(map[string]string)
	}
	i.sources[name] = src
}

// sourceText returns the source text for a (possibly mapped) source name: from
// the SourceMapper for an original/mapped source, else from the parsed-source
// registry.
func (i *Interpreter) sourceText(name string) (string, bool) {
	if i.sourceMapper != nil {
		if s, ok := i.sourceMapper.SourceText(name); ok {
			return s, true
		}
	}
	if s, ok := i.sources[name]; ok {
		return s, true
	}
	return "", false
}

// captureError records an error's stack at construction: the plain-text .stack
// string plus the structured frames (for FormatError's rich rendering).
func (i *Interpreter) captureError(obj *Object, name, message string) {
	frames := i.captureFrames()
	setErrorStack(obj, plainStack(i, name, message, frames))
	if obj.internal == nil {
		obj.internal = make(map[string]any)
	}
	obj.internal["errorFrames"] = frames
	obj.internal["errorName"] = name
	obj.internal["errorMessage"] = message
}

// plainStack builds the plain-text stack string: "Name: message" followed by one
// "    at fn (source:line:column)" line per captured frame.
func plainStack(i *Interpreter, name, message string, frames []stackFrame) string {
	var b strings.Builder
	b.WriteString(name)
	if message != "" {
		b.WriteString(": ")
		b.WriteString(message)
	}
	for _, f := range frames {
		src, line, col := i.mapPos(f.pos)
		fmt.Fprintf(&b, "\n    at %s (%s:%d:%d)", frameName(f.name), src, line, col)
	}
	return b.String()
}

// ANSI palette used by the rich renderer.
const (
	ansiReset   = "\x1b[0m"
	ansiBoldRed = "\x1b[1;31m"
	ansiRed     = "\x1b[31m"
	ansiCyan    = "\x1b[36m"
	ansiGray    = "\x1b[90m"
	ansiYellow  = "\x1b[33m"
)

// FormatError renders v as a rich, developer-facing stack trace: a header, the
// full call stack (source-mapped), and a code frame pointing at the throw site.
// It is colorized with ANSI unless WithErrorColor(false) was set. For a non-error
// value it falls back to a brief rendering.
func (i *Interpreter) FormatError(v Value) string {
	o, ok := v.(*Object)
	if !ok || o.internal == nil {
		return briefValue(v)
	}
	frames, hasFrames := o.internal["errorFrames"].([]stackFrame)
	if !hasFrames {
		return briefValue(v)
	}
	name, _ := o.internal["errorName"].(string)
	message, _ := o.internal["errorMessage"].(string)

	c := func(color, s string) string {
		if !i.errorColor {
			return s
		}
		return color + s + ansiReset
	}

	var b strings.Builder
	b.WriteString(c(ansiBoldRed, name))
	if message != "" {
		b.WriteString(": ")
		b.WriteString(message)
	}
	for _, f := range frames {
		src, line, col := i.mapPos(f.pos)
		b.WriteString("\n    ")
		b.WriteString(c(ansiGray, "at "))
		b.WriteString(c(ansiCyan, frameName(f.name)))
		b.WriteString(" ")
		b.WriteString(c(ansiGray, fmt.Sprintf("(%s:%d:%d)", src, line, col)))
	}
	// Code frame for the throw site (the innermost frame).
	if len(frames) > 0 {
		src, line, col := i.mapPos(frames[0].pos)
		if frame := i.codeFrame(src, line, col); frame != "" {
			b.WriteString("\n\n")
			b.WriteString(frame)
		}
	}
	return b.String()
}

// codeFrame renders the source line at (line,col) with a caret underneath and a
// line of context on each side, 1-based positions. Returns "" if the source text
// is unavailable.
func (i *Interpreter) codeFrame(source string, line, col int) string {
	text, ok := i.sourceText(source)
	if !ok || line <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if line > len(lines) {
		return ""
	}
	color := func(cc, s string) string {
		if !i.errorColor {
			return s
		}
		return cc + s + ansiReset
	}
	var b strings.Builder
	start := line - 1
	if start < 1 {
		start = 1
	}
	end := line + 1
	if end > len(lines) {
		end = len(lines)
	}
	for n := start; n <= end; n++ {
		src := strings.TrimRight(lines[n-1], "\r")
		if n == line {
			fmt.Fprintf(&b, "%s %s | %s\n", color(ansiRed, ">"), color(ansiYellow, fmt.Sprintf("%d", n)), src)
			// caret line
			pad := strings.Repeat(" ", len(fmt.Sprintf("%d", n)))
			gutter := col - 1
			if gutter < 0 {
				gutter = 0
			}
			fmt.Fprintf(&b, "  %s | %s%s\n", pad, strings.Repeat(" ", gutter), color(ansiRed, "^"))
		} else {
			fmt.Fprintf(&b, "  %s | %s\n", color(ansiGray, fmt.Sprintf("%d", n)), src)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
