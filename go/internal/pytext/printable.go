package pytext

// Printable uses the pinned Python Unicode database rather than the newer Go
// runtime tables. Newly assigned Go characters can still be escaped by Python.
func Printable(value rune) bool { return caseContains(value, pythonPrintable[:]) }
