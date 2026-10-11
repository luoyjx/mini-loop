// Package improvement contains the acceptance-instrument checks used by the
// proposal flow. It does not install or execute a proposal service.
package improvement

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

// InstrumentFingerprint retains the first 16 bytes of the source SHA-256
// digest. It is a detached, comparable value with a fixed 32-hex projection.
type InstrumentFingerprint [16]byte

func (fingerprint InstrumentFingerprint) String() string { return hex.EncodeToString(fingerprint[:]) }
func (fingerprint InstrumentFingerprint) MarshalText() ([]byte, error) {
	return []byte(fingerprint.String()), nil
}

// VerifierTouches preserves source substring matching, case, order and repeats.
// It classifies paths without normalizing them or consulting the filesystem.
func VerifierTouches(paths []string) []string {
	result := []string{}
	for _, path := range paths {
		if strings.Contains(path, "tools/verify_") || strings.Contains(path, ".github/workflows/") || strings.Contains(path, "conftest.py") {
			result = append(result, path)
		}
	}
	return result
}

type instrumentFiles interface {
	ReadDir(string) ([]fs.DirEntry, error)
	Stat(string) (fs.FileInfo, error)
	ReadFile(string) ([]byte, error)
}
type localInstrumentFiles struct{}

func (localInstrumentFiles) ReadDir(path string) ([]fs.DirEntry, error) { return os.ReadDir(path) }
func (localInstrumentFiles) Stat(path string) (fs.FileInfo, error)      { return os.Stat(path) }
func (localInstrumentFiles) ReadFile(path string) ([]byte, error)       { return os.ReadFile(path) }

// VerifierFingerprint follows the four source globs in their declared order,
// sorting within each glob, following file symlinks and excluding directories.
// Paths and bytes enter the digest without separators, as in the source.
func VerifierFingerprint(workspace string) (InstrumentFingerprint, error) {
	return verifierFingerprint(workspace, localInstrumentFiles{})
}

func absentInstrument(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.EBADF) || errors.Is(err, syscall.ELOOP)
}

func verifierFingerprint(root string, files instrumentFiles) (InstrumentFingerprint, error) {
	digest := sha256.New()
	var result InstrumentFingerprint
	if root == "" {
		root = "."
	}
	// pathlib treats non-encodable/NUL paths as nonexistent for these probes.
	if strings.ContainsRune(root, 0) {
		copy(result[:], digest.Sum(nil))
		return result, nil
	}
	for _, pattern := range []struct{ directory, prefix, exact string }{
		{directory: "tools", prefix: "verify_"},
		{directory: ".github/workflows"},
		{exact: "conftest.py"},
		{exact: "tests/conftest.py"},
	} {
		var paths []string
		if pattern.exact != "" {
			paths = []string{pattern.exact}
		} else {
			entries, err := files.ReadDir(root + "/" + pattern.directory)
			if err != nil {
				if absentInstrument(err) || errors.Is(err, fs.ErrPermission) {
					continue
				}
				return result, err
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), pattern.prefix) {
					paths = append(paths, pattern.directory+"/"+entry.Name())
				}
			}
			sort.Strings(paths)
		}
		for _, relative := range paths {
			path := root + "/" + relative
			info, err := files.Stat(path)
			if err != nil {
				if absentInstrument(err) {
					continue
				}
				return result, err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			if !utf8.ValidString(relative) {
				return result, errors.New("acceptance instrument path is not UTF-8")
			}
			digest.Write([]byte(relative))
			body, err := files.ReadFile(path)
			if err != nil {
				body = []byte("<unreadable>")
			}
			digest.Write(body)
		}
	}
	copy(result[:], digest.Sum(nil))
	return result, nil
}
