package improvement

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

type instrumentFaultFiles struct {
	localInstrumentFiles
	unreadable                map[string]bool
	directoryError, statError error
}

func (files instrumentFaultFiles) ReadFile(path string) ([]byte, error) {
	if files.unreadable[path] {
		return nil, fs.ErrPermission
	}
	return files.localInstrumentFiles.ReadFile(path)
}
func (files instrumentFaultFiles) ReadDir(path string) ([]fs.DirEntry, error) {
	if files.directoryError != nil {
		return nil, files.directoryError
	}
	return files.localInstrumentFiles.ReadDir(path)
}
func (files instrumentFaultFiles) Stat(path string) (fs.FileInfo, error) {
	if files.statError != nil {
		return nil, files.statError
	}
	return files.localInstrumentFiles.Stat(path)
}

// APFS refuses malformed UTF-8 filenames. This detached entry exercises the
// source encoding refusal independently of the host filesystem's admission.
type malformedInstrumentEntry struct{ fs.DirEntry }

func (malformedInstrumentEntry) Name() string { return "verify_" + string([]byte{0xff}) }

type malformedInstrumentFiles struct {
	localInstrumentFiles
	entry fs.DirEntry
	info  fs.FileInfo
}

func (files malformedInstrumentFiles) ReadDir(path string) ([]fs.DirEntry, error) {
	if strings.HasSuffix(path, "/tools") {
		return []fs.DirEntry{malformedInstrumentEntry{files.entry}}, nil
	}
	return files.localInstrumentFiles.ReadDir(path)
}
func (files malformedInstrumentFiles) Stat(path string) (fs.FileInfo, error) {
	if strings.HasSuffix(path, string([]byte{0xff})) {
		return files.info, nil
	}
	return files.localInstrumentFiles.Stat(path)
}

func TestActualSourceAcceptanceInstrumentContracts(t *testing.T) {
	var source struct {
		Touches      []struct{ Paths, Touched []string }
		Fingerprints []struct {
			Name, Fingerprint string
			Missing, Nul      bool
			Files             []struct {
				Path, Text, Hex, Target string
				Unreadable              bool
			}
		}
	}
	data, err := os.ReadFile("../testdata/python-improvement-instruments.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	for _, row := range source.Touches {
		if got := VerifierTouches(row.Paths); !reflect.DeepEqual(got, row.Touched) {
			t.Fatal(got, row.Touched)
		}
	}
	for _, row := range source.Fingerprints {
		t.Run(row.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root[literal]*?")
			if row.Nul {
				root += "\x00"
			}
			if !row.Missing && !row.Nul {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			files := instrumentFaultFiles{unreadable: map[string]bool{}}
			for _, file := range row.Files {
				path := root + "/" + file.Path
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if file.Target != "" {
					if err := os.Symlink(file.Target, path); err != nil {
						t.Fatal(err)
					}
				} else {
					body := []byte(file.Text)
					if file.Hex != "" {
						var err error
						body, err = hex.DecodeString(file.Hex)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(path, body, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if file.Unreadable {
					files.unreadable[path] = true
				}
			}
			got, err := verifierFingerprint(root, files)
			if err != nil || got.String() != row.Fingerprint {
				t.Fatal(got, row.Fingerprint, err)
			}
			if len(files.unreadable) == 0 {
				public, err := VerifierFingerprint(root)
				if err != nil || public != got {
					t.Fatal(public, got, err)
				}
			}
			encoded, err := json.Marshal(got)
			if err != nil || string(encoded) != `"`+row.Fingerprint+`"` {
				t.Fatal(string(encoded), err)
			}
		})
	}
}

func TestInstrumentMutationAndRestorationChangeJudgmentDigest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "conftest.py")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := VerifierFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ordinary.txt"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	unchanged, err := VerifierFingerprint(root)
	if err != nil || unchanged != before {
		t.Fatal(unchanged, err)
	}
	if err := os.WriteFile(path, []byte("weakened"), 0600); err != nil {
		t.Fatal(err)
	}
	during, err := VerifierFingerprint(root)
	if err != nil || during == before {
		t.Fatal(during, err)
	}
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := VerifierFingerprint(root)
	if err != nil || restored != before {
		t.Fatal(restored, err)
	}
	paths := []string{"tools/verify_one", "ordinary"}
	touched := VerifierTouches(paths)
	touched[0] = "mutated"
	if paths[0] != "tools/verify_one" {
		t.Fatal("touch output aliased input")
	}
}

func TestInstrumentIOFailuresPreserveSourceDistinctions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/conftest.py", []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []error{fs.ErrNotExist, syscall.ENOTDIR, syscall.EBADF, syscall.ELOOP} {
		if _, err := verifierFingerprint(root, instrumentFaultFiles{statError: fault}); err != nil {
			t.Fatal(fault, err)
		}
	}
	if _, err := verifierFingerprint(root, instrumentFaultFiles{statError: fs.ErrPermission}); !errors.Is(err, fs.ErrPermission) {
		t.Fatal("stat permission swallowed", err)
	}
	if _, err := verifierFingerprint(root, instrumentFaultFiles{directoryError: fs.ErrPermission}); err != nil {
		t.Fatal(err)
	}
	if _, err := verifierFingerprint(root, instrumentFaultFiles{directoryError: syscall.EIO}); !errors.Is(err, syscall.EIO) {
		t.Fatal("scan IO fault swallowed", err)
	}
	if err := os.Mkdir(root+"/tools", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/tools/verify_valid", []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root + "/tools")
	if err != nil {
		t.Fatal(err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifierFingerprint(root, malformedInstrumentFiles{entry: entries[0], info: info}); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatal("invalid path encoding accepted", err)
	}
}
