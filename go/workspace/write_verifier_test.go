package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestWriteVerificationUsesFullStrictTextAndUniversalNewlines(t *testing.T) {
	files, err := NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name            string
		data            []byte
		expected        string
		matches, failed bool
	}{
		{"exact", []byte("汉字😀"), "汉字😀", true, false},
		{"empty", []byte{}, "", true, false},
		{"newlines", []byte("a\r\nb\rc"), "a\nb\nc", true, false},
		{"raw-intent", []byte("a\r\nb"), "a\r\nb", false, false},
		{"different", []byte("other"), "wanted", false, false},
		{"extra", []byte("wanted extra"), "wanted", false, false},
		{"strict-suffix", []byte{'x', 0xff}, "wanted", false, true},
		{"strict-truncated", []byte{0xe6, 0x96}, "", false, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(files.Root(), "proof")
			if err := os.WriteFile(path, fixture.data, 0600); err != nil {
				t.Fatal(err)
			}
			actual, err := files.WriteTookEffect(context.Background(), protocol.WriteFileInput{Path: "proof", Content: fixture.expected})
			if actual != fixture.matches || (err != nil) != fixture.failed {
				t.Fatalf("match %v, error %v", actual, err)
			}
		})
	}
	missing, err := files.WriteTookEffect(context.Background(), protocol.WriteFileInput{Path: "missing", Content: ""})
	if missing || err != nil {
		t.Fatal(missing, err)
	}
	if _, err := files.WriteTookEffect(context.Background(), protocol.WriteFileInput{Path: "../escape"}); err == nil {
		t.Fatal("unbound verification")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := files.WriteTookEffect(ctx, protocol.WriteFileInput{Path: "proof"}); err == nil {
		t.Fatal("cancellation ignored")
	}
}
