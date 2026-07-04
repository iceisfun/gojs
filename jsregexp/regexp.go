package jsregexp

// Regexp is a compiled ECMAScript regular expression. In this phase it holds the
// parsed pattern and flags; the compiled matcher program is attached by a later
// phase. The public API is intentionally small and idiomatic (Compile now;
// Exec/FindSubmatchIndex once the VM lands).
type Regexp struct {
	source    string
	flags     Flags
	pattern   *Pattern
	prog      *prog
	stepLimit int
}

// Compile parses source under the given flags string into a Regexp and compiles
// it to an executable matcher program, returning a *SyntaxError if either the
// flags or the pattern is invalid (compilation can also surface not-yet-resolved
// constructs, e.g. certain Unicode property escapes).
func Compile(source, flags string) (*Regexp, error) {
	f, err := ParseFlags(flags)
	if err != nil {
		return nil, err
	}
	pat, err := Parse(source, f)
	if err != nil {
		return nil, err
	}
	re := &Regexp{source: source, flags: f, pattern: pat, stepLimit: DefaultStepBudget}
	if err := re.compile(); err != nil {
		return nil, err
	}
	return re, nil
}

// SetStepBudget overrides the per-attempt step budget (the ReDoS bound). A value
// <= 0 disables the budget (not recommended for untrusted patterns).
func (re *Regexp) SetStepBudget(n int) { re.stepLimit = n }

// MustCompile is like Compile but panics on error. Intended for tests and
// package-internal known-good patterns.
func MustCompile(source, flags string) *Regexp {
	re, err := Compile(source, flags)
	if err != nil {
		panic(err)
	}
	return re
}

// Source returns the original pattern text.
func (re *Regexp) Source() string { return re.source }

// Flags returns the parsed flags.
func (re *Regexp) Flags() Flags { return re.flags }

// NumSubexp returns the number of capturing groups.
func (re *Regexp) NumSubexp() int { return re.pattern.GroupCount }

// GroupNames returns the name→index map for named capturing groups (empty when
// there are none). The returned map must not be mutated.
func (re *Regexp) GroupNames() map[string][]int { return re.pattern.GroupNames }

// AST exposes the parsed pattern tree, primarily for tests and tooling.
func (re *Regexp) AST() *Pattern { return re.pattern }
