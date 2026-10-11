package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/memory"
)

type memorySurrogateMasker struct{}

func (memorySurrogateMasker) MaskText(text string) string {
	return strings.ReplaceAll(text, string([]byte{0xed, 0xa0, 0x80}), "safe")
}

func memoryBodyMasker(enabled bool) memory.Masker {
	if enabled {
		return memorySurrogateMasker{}
	}
	return nil
}

func assertMemoryProblems(t *testing.T, raw *memory.Store, owner, foreign *memory.ScopedStore, expected []string) {
	t.Helper()
	for label, actual := range map[string][]string{
		"raw":   raw.SelfAuditProblems().Entries,
		"owner": owner.SelfAuditProblems().Entries,
	} {
		if !slices.Equal(actual, expected) {
			t.Fatalf("%s memory problems: %v want %v", label, actual, expected)
		}
	}
	if entries := foreign.SelfAuditProblems().Entries; len(entries) != 0 {
		t.Fatal("foreign binding received body diagnostics", entries)
	}
}
