package pytext

// IsWord matches Python's pinned Unicode regex word boundary: alphanumeric or _.
func IsWord(value rune) bool { return caseContains(value, pythonWord[:]) }
