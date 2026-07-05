// Package test262 provides a runner for the official ECMAScript conformance
// suite (Test262), located under reference/test262. It parses each test's YAML
// frontmatter, assembles the required harness includes, executes the test with
// the engine under a timeout, and classifies the outcome against the test's
// declared expectation (positive or negative).
//
// It is deliberately conservative: tests using features gojs does not implement
// (modules, raw/generated forms, and a configurable feature denylist) are
// reported as skipped rather than failed, so the pass rate reflects real
// conformance of implemented features. Async tests (flags: [async]) are run:
// the runner supplies $DONE and inspects the reported completion.
package test262

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iceisfun/gojs/interp"
	"github.com/iceisfun/gojs/parser"
)

// root is the test262 checkout, relative to this package directory.
const root = "../../reference/test262"

// Outcome classifies a single test run.
type Outcome int

const (
	Pass Outcome = iota
	Fail         // engine behavior disagreed with the test's expectation
	Skip         // test needs a feature/flag gojs does not support
)

// Meta holds the parsed subset of a test's frontmatter that the runner needs.
type Meta struct {
	Includes   []string
	Flags      map[string]bool
	Features   []string
	NegType    string // expected error constructor name (negative tests)
	NegPhase   string // "parse", "resolution", or "runtime"
	IsNegative bool
}

// Result is the outcome of running one test file (one mode).
type Result struct {
	Path    string
	Mode    string // "strict" or "sloppy"
	Outcome Outcome
	Reason  string
}

// unsupportedFeatures lists feature tags whose tests we skip wholesale. These
// are language areas gojs does not implement yet; running them only produces
// noise.
var unsupportedFeatures = map[string]bool{
	"Proxy": false, "Reflect": false, "TypedArray": false, "ArrayBuffer": false,
	// SharedArrayBuffer + the Atomics operations are implemented for a single
	// agent (no worker threads). The multi-agent tests — which need a real
	// $262.agent host to spawn a second thread over the same buffer — are skipped
	// separately by hostFeatureSkip (a $262.agent reference) and by the
	// CanBlock* flags in skipReason.
	"SharedArrayBuffer": false, "Atomics": false, "WeakRef": false,
	"FinalizationRegistry": false, "Temporal": true, "Intl": true,
	"tail-call-optimization": true, "import-assertions": true,
	"decorators": true, "explicit-resource-management": true,
	"IsHTMLDDA": true, "__proto__": false,
	// immutable-arraybuffer (ArrayBuffer.prototype.transferToImmutable) is a
	// separate proposal gojs does not implement; skip its tests rather than fail.
	"immutable-arraybuffer": true,
	// import-defer (import.defer(...)) and source-phase-imports (import.source(...))
	// are separate TC39 proposals with their own import.* syntax gojs does not
	// parse; skip their tests rather than fail on the SyntaxError.
	"import-defer": true, "source-phase-imports": true,
	// import-attributes (import(x, {with:{...}})) requires a host module-type
	// system to honor and validate attributes; gojs evaluates the options
	// argument but does not implement attributes, so skip like import-assertions.
	"import-attributes": true,
}

// ParseMeta extracts the frontmatter metadata from a test's source.
func ParseMeta(src string) Meta {
	m := Meta{Flags: map[string]bool{}}
	start := strings.Index(src, "/*---")
	if start < 0 {
		return m
	}
	end := strings.Index(src[start:], "---*/")
	if end < 0 {
		return m
	}
	block := src[start+len("/*---") : start+end]

	// A test may use CR or CRLF line terminators (the line-terminator-normalisation
	// tests do), so normalize the frontmatter block before splitting — otherwise a
	// CR-only file collapses to one line and includes:/flags:/features: go unseen.
	// Only the metadata block is normalized; the test body keeps its terminators.
	block = strings.ReplaceAll(block, "\r\n", "\n")
	block = strings.ReplaceAll(block, "\r", "\n")
	lines := strings.Split(block, "\n")
	for idx := 0; idx < len(lines); idx++ {
		line := strings.TrimRight(lines[idx], " \t")
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "includes:"):
			m.Includes = append(m.Includes, parseInlineList(trimmed[len("includes:"):])...)
		case strings.HasPrefix(trimmed, "flags:"):
			for _, f := range parseInlineList(trimmed[len("flags:"):]) {
				m.Flags[f] = true
			}
		case strings.HasPrefix(trimmed, "features:"):
			m.Features = append(m.Features, parseInlineList(trimmed[len("features:"):])...)
		case trimmed == "negative:":
			m.IsNegative = true
			// The following indented lines carry type:/phase:.
			for j := idx + 1; j < len(lines); j++ {
				sub := strings.TrimSpace(lines[j])
				if strings.HasPrefix(sub, "type:") {
					m.NegType = strings.TrimSpace(sub[len("type:"):])
				} else if strings.HasPrefix(sub, "phase:") {
					m.NegPhase = strings.TrimSpace(sub[len("phase:"):])
				} else if sub != "" && !strings.HasPrefix(lines[j], " ") && !strings.HasPrefix(lines[j], "\t") {
					break
				}
			}
		}
	}
	return m
}

// hasFeature reports whether the test declares the given feature tag.
func hasFeature(m Meta, name string) bool {
	for _, f := range m.Features {
		if f == name {
			return true
		}
	}
	return false
}

// parseInlineList parses a YAML flow list "[a, b, c]" or a bare scalar.
func parseInlineList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// harnessCache memoizes harness include file contents.
var harnessCache = map[string]string{}

// loadHarness returns the concatenation of the named harness includes plus the
// mandatory assert.js and sta.js.
func loadHarness(includes []string) (string, error) {
	var b strings.Builder
	all := append([]string{"assert.js", "sta.js"}, includes...)
	for _, name := range all {
		src, ok := harnessCache[name]
		if !ok {
			data, err := os.ReadFile(filepath.Join(root, "harness", name))
			if err != nil {
				return "", err
			}
			src = string(data)
			harnessCache[name] = src
		}
		b.WriteString(src)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// skipReason returns a non-empty reason when the test should be skipped based on
// unsupported flags/features.
func skipReason(m Meta) string {
	if m.Flags["module"] {
		return "module"
	}
	if m.Flags["raw"] {
		return "raw"
	}
	if m.Flags["CanBlockIsFalse"] || m.Flags["CanBlockIsTrue"] {
		return "agent"
	}
	for _, f := range m.Features {
		if unsupportedFeatures[f] {
			return "feature:" + f
		}
	}
	return ""
}

// hostFeatureSkip returns a non-empty reason when a test needs a host-environment
// capability the Test262 runner intentionally does not provide, so it is skipped
// rather than counted as a conformance failure. These are host features, not
// language behavior gojs implements:
//
//   - print: the runner exposes `print` only as a bespoke $DONE sentinel sink for
//     async tests (see installAsyncDone); it deliberately does not provide a
//     general `print` host sink. A NON-async test that references the bare `print`
//     global therefore needs a capability we don't provide. Skipping such tests in
//     the classification path (rather than installing a global print) keeps the
//     async sentinel `print` untouched and avoids double-installing it.
//
// $262.createRealm is implemented (installT262Host builds a fresh realm via
// interp.NewChildRealm, a real isolated Interpreter sharing this agent's Symbol
// registry and well-known symbols), and the realm-aware abstract operations the
// cross-realm corpus exercises are implemented too: GetFunctionRealm (§10.2.10)
// so GetPrototypeFromConstructor falls back to the constructor's OWN realm's
// intrinsic default (proto-from-ctor-realm), and current-running-realm selection
// for the Typeerrors/intrinsics a Proxy raises (Proxy internal-method *-realm
// tests). The whole cross-realm / createRealm corpus therefore runs rather than
// being skipped.
func hostFeatureSkip(src string, m Meta) string {
	if !m.Flags["async"] && referencesPrintGlobal(src) {
		return "host:print"
	}
	// The $262.agent API spawns a second agent (worker thread) sharing a
	// SharedArrayBuffer. gojs implements SharedArrayBuffer/Atomics for a single
	// agent only (Phase 1), so a test that drives another agent needs a host
	// capability we don't provide; skip it rather than fail. The single-agent
	// SharedArrayBuffer and Atomics tests (the large majority) still run.
	if strings.Contains(src, "$262.agent") {
		return "host:agent"
	}
	return ""
}

// referencesPrintGlobal reports whether src uses the bare `print` global as a
// value or a call (e.g. `print(x)` or `Array.print = print`). It deliberately
// does NOT match property access (".print"), longer identifiers ("printCodePoint",
// "printShape", "unprintable"), or the word "print" appearing in comments/prose
// ("... should not print a time zone"), so genuinely-supported tests are not
// skipped merely because the substring "print" occurs in text.
func referencesPrintGlobal(src string) bool {
	const p = "print"
	for i := 0; i+len(p) <= len(src); i++ {
		if src[i:i+len(p)] != p {
			continue
		}
		// Reject property access and larger identifiers on the left.
		if i > 0 {
			if c := src[i-1]; isIdentPart(c) || c == '.' || c == '$' {
				continue
			}
		}
		j := i + len(p)
		// Reject larger identifiers on the right (printCodePoint, printShape).
		if j < len(src) && isIdentPart(src[j]) {
			continue
		}
		// Call/value use: the token is immediately followed (ignoring inline
		// whitespace) by code punctuation that never introduces prose.
		k := j
		for k < len(src) && (src[k] == ' ' || src[k] == '\t') {
			k++
		}
		if k < len(src) {
			switch src[k] {
			case '(', ';', ',', ')':
				return true
			}
		}
		// Value use on the right of an assignment/comparison: `= print`.
		h := i - 1
		for h >= 0 && (src[h] == ' ' || src[h] == '\t') {
			h--
		}
		if h >= 0 && src[h] == '=' {
			return true
		}
	}
	return false
}

// isIdentPart reports whether c can appear inside a JS identifier (ASCII subset,
// sufficient for the "print" boundary checks above).
func isIdentPart(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// Run executes a single test file, returning one Result per applicable mode
// (strict/sloppy). It never panics; failures are captured as Fail outcomes.
func Run(path string) []Result {
	data, err := os.ReadFile(path)
	if err != nil {
		return []Result{{Path: path, Outcome: Skip, Reason: "read: " + err.Error()}}
	}
	src := string(data)
	m := ParseMeta(src)

	if reason := skipReason(m); reason != "" {
		return []Result{{Path: path, Outcome: Skip, Reason: reason}}
	}
	if reason := hostFeatureSkip(src, m); reason != "" {
		return []Result{{Path: path, Outcome: Skip, Reason: reason}}
	}

	var modes []string
	switch {
	case m.Flags["onlyStrict"]:
		modes = []string{"strict"}
	case m.Flags["noStrict"], m.Flags["raw"]:
		modes = []string{"sloppy"}
	default:
		modes = []string{"sloppy", "strict"}
	}

	var results []Result
	for _, mode := range modes {
		results = append(results, runMode(path, src, m, mode))
	}
	return results
}

// runMode runs one test in one strictness mode.
func runMode(path, src string, m Meta, mode string) Result {
	res := Result{Path: path, Mode: mode}

	// Negative parse errors must be detected by the parser directly.
	prelude := ""
	if mode == "strict" {
		prelude = "\"use strict\";\n"
	}

	if m.IsNegative && m.NegPhase == "parse" {
		_, perr := parser.Parse(path, prelude+src)
		if perr != nil {
			res.Outcome = Pass
		} else {
			res.Outcome = Fail
			res.Reason = "expected parse error " + m.NegType
		}
		return res
	}

	harness, herr := loadHarness(m.Includes)
	if herr != nil {
		res.Outcome = Skip
		res.Reason = "harness: " + herr.Error()
		return res
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opts := []interp.Option{
		interp.WithContext(ctx),
		interp.WithTimeProvider(interp.NewDefaultTimeProvider()),
		interp.WithTimerProvider(interp.NewDefaultTimerProvider()),
	}
	// The bytecode VM is the default engine (interp.New). GOJS_T262_TREEWALK=1
	// forces the tree-walker instead, so a conformance run doubles as a differential
	// check of the two engines at scale. GOJS_T262_BYTECODE is kept (now redundant)
	// for older invocations.
	if os.Getenv("GOJS_T262_TREEWALK") != "" {
		opts = append(opts, interp.WithTreeWalker())
	} else if os.Getenv("GOJS_T262_BYTECODE") != "" {
		opts = append(opts, interp.WithBytecode())
	}
	// Dynamic import() tests reference sibling fixture modules by relative
	// specifier; serve them from the test's own directory so import() can resolve
	// and evaluate them. Gated on the feature tag so ordinary tests keep no
	// module provider (and thus no `require` global).
	// ShadowRealm.prototype.importValue imports modules in the inner realm, so
	// those tests need a module provider too (they carry only the ShadowRealm tag).
	if hasFeature(m, "dynamic-import") || hasFeature(m, "ShadowRealm") {
		opts = append(opts, interp.WithModuleProvider(interp.NewDirModuleProvider(filepath.Dir(path))))
	}
	vm := interp.New(opts...)
	defer vm.Close()
	installT262Host(vm)

	// An async test (flags: [async]) signals its outcome by calling $DONE — with
	// no/undefined argument on success, or an error on failure. gojs's RunString
	// drains the event loop before returning, so by then every promise reaction
	// and timer the test scheduled has run and $DONE has been called. We install
	// $DONE natively (rather than via harness/doneprintHandle.js) so it is a real
	// own property of globalThis, which asyncHelpers.js's asyncTest requires.
	var sink asyncSink
	if m.Flags["async"] {
		installAsyncDone(vm, &sink)
	}

	full := prelude + harness + "\n" + src
	done := make(chan error, 1)
	go func() {
		_, err := vm.RunString(path, full)
		done <- err
	}()

	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(6 * time.Second):
		res.Outcome = Fail
		res.Reason = "timeout"
		return res
	}

	// Render a readable reason for a thrown value, running its toString so that
	// Test262Error (and other custom errors) report their message rather than
	// "[object Object]".
	describe := func(err error) string {
		if v, ok := interp.ThrownValue(err); ok {
			if s, e := vm.ToString(v); e == nil && s != "" {
				return s
			}
			return interp.BriefValue(v)
		}
		return err.Error()
	}

	if m.IsNegative {
		if runErr == nil {
			res.Outcome = Fail
			res.Reason = "expected " + m.NegType + " but completed"
			return res
		}
		if v, ok := interp.ThrownValue(runErr); ok && negTypeMatches(vm, v, m.NegType) {
			res.Outcome = Pass
		} else {
			res.Outcome = Fail
			res.Reason = "wanted " + m.NegType + ", got " + describe(runErr)
		}
		return res
	}

	if runErr != nil {
		res.Outcome = Fail
		res.Reason = describe(runErr)
		return res
	}
	if m.Flags["async"] {
		switch {
		case sink.failed:
			res.Outcome = Fail
			res.Reason = "async: " + sink.failure
		case sink.done:
			res.Outcome = Pass
		default:
			res.Outcome = Fail
			res.Reason = "async test did not signal completion ($DONE not called)"
		}
		return res
	}
	res.Outcome = Pass
	return res
}

// asyncSink records the completion an async test reports through $DONE (or the
// print-based $DONE from harness/doneprintHandle.js). It is written from the VM
// goroutine and read back only after that goroutine has finished (channel
// receive establishes the happens-before), so no synchronization is needed.
type asyncSink struct {
	done    bool   // $DONE was called
	failed  bool   // it was called with an error argument
	failure string // the rendered error, when failed
}

// installAsyncDone installs the $DONE hook (and a compatible print) that an
// async Test262 test uses to report completion, recording the outcome in sink.
func installAsyncDone(vm *interp.Interpreter, sink *asyncSink) {
	// $DONE(err?): success when called with no argument or a nullish one;
	// otherwise the argument is the failure reason.
	vm.SetGlobal("$DONE", vm.NewFunction("$DONE", func(args []interp.Value) (interp.Value, error) {
		sink.done = true
		if len(args) == 0 {
			return interp.Undef, nil
		}
		v := args[0]
		if v == interp.Undef || v == interp.Nul {
			return interp.Undef, nil
		}
		msg, err := vm.ToString(v)
		if err != nil || msg == "" {
			msg = interp.BriefValue(v)
		}
		sink.failed = true
		sink.failure = msg
		return interp.Undef, nil
	}))
	// harness/doneprintHandle.js defines a $DONE that routes through print with a
	// sentinel message; support it too, so a test that includes that harness
	// explicitly still reports through the same sink.
	vm.SetGlobal("print", vm.NewFunction("print", func(args []interp.Value) (interp.Value, error) {
		if len(args) == 0 {
			return interp.Undef, nil
		}
		s, _ := vm.ToString(args[0])
		switch {
		case s == "Test262:AsyncTestComplete":
			sink.done = true
		case strings.HasPrefix(s, "Test262:AsyncTestFailure:"):
			sink.done = true
			sink.failed = true
			sink.failure = strings.TrimPrefix(s, "Test262:AsyncTestFailure:")
		}
		return interp.Undef, nil
	}))
}

// negTypeMatches reports whether a thrown value matches a negative test's
// expected error constructor (m.NegType).
//
// Built-in errors expose a "name" data property on their prototype chain, which
// BriefValue renders as "Name: message" without needing to run user code — so
// the fast, side-effect-free substring check identifies them.
//
// A user-defined error type may lack a "name" property and instead identify
// itself through a prototype toString (e.g. Test262Error from harness/sta.js,
// whose toString returns "Test262Error: <message>"). BriefValue deliberately
// does not run user toString, so such a value renders as a generic
// "[object <class>]". Only in that case do we fall back to a full ToString,
// which invokes the prototype toString and reveals the type name. Scoping the
// fallback to generic-object renderings keeps built-in error matching strict, so
// a test that expects e.g. TypeError but throws a differently-named error is
// still correctly reported as a failure.
func negTypeMatches(vm *interp.Interpreter, v interp.Value, negType string) bool {
	brief := interp.BriefValue(v)
	if strings.Contains(brief, negType) {
		return true
	}
	if strings.HasPrefix(brief, "[object ") {
		if s, err := vm.ToString(v); err == nil && strings.Contains(s, negType) {
			return true
		}
	}
	return false
}

// installT262Host installs the $262 host object that some Test262 tests use on
// vm's global object and returns it. The members our corpus needs are provided:
// global (the realm's global object), detachArrayBuffer (backing the many
// detached-buffer tests), evalScript (global-code evaluation in this realm), and
// createRealm (a fresh sibling realm with its own $262). Unsupported members are
// left absent so tests that require them fail visibly rather than silently
// misbehaving.
func installT262Host(vm *interp.Interpreter) *interp.Object {
	host := vm.NewPlainObject()
	host.SetData("global", vm.GetGlobal("globalThis"))
	host.SetData("detachArrayBuffer", vm.NewFunction("detachArrayBuffer", func(args []interp.Value) (interp.Value, error) {
		if len(args) > 0 {
			vm.DetachArrayBuffer(args[0])
		}
		return interp.Undef, nil
	}))
	// $262.evalScript(sourceText): evaluate sourceText as a Script (global code,
	// not eval code) in the current realm and return its completion value. It
	// shares this Interpreter's realm/global, so declarations it introduces are
	// observable on globalThis afterwards — the property the language/global-code
	// tests rely on. RunString is the runner's own global-script entry point, so
	// reusing it keeps evalScript's semantics identical to the main test body; a
	// parse error or thrown exception comes back as an error and propagates as a
	// normal throw to the caller.
	host.SetData("evalScript", vm.NewFunction("evalScript", func(args []interp.Value) (interp.Value, error) {
		var src string
		if len(args) > 0 {
			s, err := vm.ToString(args[0])
			if err != nil {
				return interp.Undef, err
			}
			src = s
		}
		return vm.RunString("<evalScript>", src)
	}))
	// $262.createRealm(): create a new realm in the same agent, install this same
	// $262 API on its global, and return that realm's $262 object. The child
	// shares the GlobalSymbolRegistry and is closed with its parent (see
	// NewChildRealm). Objects flow between realms directly as ordinary values, so
	// cross-realm tests observe each realm's distinct intrinsics.
	host.SetData("createRealm", vm.NewFunction("createRealm", func(args []interp.Value) (interp.Value, error) {
		child := vm.NewChildRealm()
		return installT262Host(child), nil
	}))
	vm.SetGlobal("$262", host)
	return host
}
