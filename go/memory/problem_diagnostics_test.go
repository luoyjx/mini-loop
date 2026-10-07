package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedSelfAuditNeverReadsSharedDiagnosticNames(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	alice, _ := Bind(store, "alice")
	bob, _ := Bind(store, "bob")
	input := Input{Name: "alice-private", Body: strings.Repeat("界", MaxBody+1)}
	for i := 0; i < 2; i++ {
		if _, err := alice.Write(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if err := alice.ReplaceAll(ctx, []Input{input}, Imported); err != nil {
		t.Fatal(err)
	}
	// An unreadable filename cannot establish an owner, even during an owned read.
	secret := "bob-secret-unattributed.md"
	if err := os.WriteFile(filepath.Join(root, secret), []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.List(ctx); err != nil {
		t.Fatal(err)
	}
	a := alice.SelfAuditProblems()
	b := bob.SelfAuditProblems()
	fleet := store.SelfAuditProblems()
	if a.Total.String() != "3" || len(a.Entries) != 1 || !strings.HasSuffix((*a.Summary)[0], "(x3)") {
		t.Fatalf("owned: %+v", a)
	}
	if b.Total.String() != "0" || len(b.Entries) != 0 {
		t.Fatalf("bob sees alice diagnostics: %+v", b)
	}
	if !strings.Contains(strings.Join(fleet.Entries, "\n"), secret) {
		t.Fatalf("fleet lost unreadable diagnostic: %+v", fleet)
	}
	if strings.Contains(strings.Join(a.Entries, "\n"), secret) {
		t.Fatal("unattributed filename leaked")
	}
	a.Entries[0] = "changed"
	(*a.Summary)[0] = "changed"
	a.Total.BigInt().SetInt64(0)
	if alice.SelfAuditProblems().Total.String() != "3" || strings.Contains(alice.SelfAuditProblems().Entries[0], "changed") {
		t.Fatal("snapshot aliases binding")
	}
	// A new binding starts its own ledger, keeping retention bounded per holder.
	again, _ := Bind(store, "alice")
	if again.SelfAuditProblems().Total.String() != "0" {
		t.Fatal("binding unexpectedly reused diagnostics")
	}
}
