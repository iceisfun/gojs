package jsregexp

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func hexVal(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	default:
		return int(r-'A') + 10
	}
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isSyntaxChar reports whether r is a SyntaxCharacter (§22.2.1). In Unicode mode
// only these (and '/') may be the target of an IdentityEscape.
func isSyntaxChar(r rune) bool {
	switch r {
	case '^', '$', '\\', '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|':
		return true
	}
	return false
}

func classEscKind(c rune) ClassEscKind {
	switch c {
	case 'd':
		return EscDigit
	case 'D':
		return EscNotDigit
	case 'w':
		return EscWord
	case 'W':
		return EscNotWord
	case 's':
		return EscSpace
	default: // 'S'
		return EscNotSpace
	}
}
