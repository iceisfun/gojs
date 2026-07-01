package jsregexp

// dupCheckNames enforces the duplicate-named-capturing-group early error
// (§22.2.1.1 GroupSpecifiersThatMatch / ES2025 duplicate names). A capture name
// may be reused only when the two occurrences lie in different alternatives of a
// single disjunction — mutually exclusive branches that can never both match.
// Any other repetition (in sequence, nested, or through a quantifier/group/
// lookaround) is a SyntaxError.
//
// It returns the set of names declared anywhere in n's subtree so callers can
// combine sibling sets with the appropriate rule (exclusive for sequential
// composition, permissive across disjunction branches).
func dupCheckNames(n Node) (map[string]struct{}, *SyntaxError) {
	switch t := n.(type) {
	case *Disjunction:
		acc := map[string]struct{}{}
		for _, alt := range t.Alternatives {
			s, err := dupCheckNames(alt)
			if err != nil {
				return nil, err
			}
			for k := range s { // permissive: branches are mutually exclusive
				acc[k] = struct{}{}
			}
		}
		return acc, nil

	case *Concat:
		acc := map[string]struct{}{}
		for _, term := range t.Terms {
			s, err := dupCheckNames(term)
			if err != nil {
				return nil, err
			}
			if err := mergeExclusive(acc, s); err != nil {
				return nil, err
			}
		}
		return acc, nil

	case *Capture:
		s, err := dupCheckNames(t.Body)
		if err != nil {
			return nil, err
		}
		if t.Name != "" {
			if _, dup := s[t.Name]; dup {
				return nil, &SyntaxError{Msg: "duplicate capture group name " + t.Name, Pos: -1}
			}
			s[t.Name] = struct{}{}
		}
		return s, nil

	case *Group:
		return dupCheckNames(t.Body)
	case *Quantifier:
		return dupCheckNames(t.Body)
	case *Lookaround:
		return dupCheckNames(t.Body)

	default:
		return map[string]struct{}{}, nil
	}
}

// mergeExclusive adds every name in b into a, reporting an error on any name
// already present (a name shared across a sequential path).
func mergeExclusive(a, b map[string]struct{}) *SyntaxError {
	for k := range b {
		if _, dup := a[k]; dup {
			return &SyntaxError{Msg: "duplicate capture group name " + k, Pos: -1}
		}
		a[k] = struct{}{}
	}
	return nil
}
