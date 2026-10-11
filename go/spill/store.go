// Package spill preserves already-masked oversized text in private local files.
// A namespace groups artifacts; it is not an access-control identity.
package spill

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/workspace"
)

const MaxBytes = 8_000_000

type Namespace string

type Request struct {
	Namespace     Namespace
	ToolName      string
	Label         string
	SuggestedName string
	Content       string // Caller-owned masking boundary; the store has no credential registry.
}

type Ref struct {
	Locator       string `json:"locator"`
	Bytes         int    `json:"bytes"`
	RetrievalHint string `json:"retrieval_hint"`
}

type Store interface {
	SaveText(context.Context, Request) (Ref, error)
}

// LocalStore has no background workers or open handles between saves. Construction
// tightens the resolved root. Exclusive create protects the artifact leaf; it does
// not confine parent-component races or establish isolation between host-shell users.
type LocalStore struct {
	root  string
	token func() (string, error)
}

func NewLocalStore(root string) (*LocalStore, error) {
	resolved, err := workspace.ResolvePath(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resolved, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(resolved, 0700); err != nil {
		return nil, err
	}
	return &LocalStore{root: resolved, token: randomToken}, nil
}
func (s *LocalStore) Root() string { return s.root }
func randomToken() (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
var repeatedDots = regexp.MustCompile(`\.{2,}`)

func safeName(suggested string) string {
	name := unsafeName.ReplaceAllString(suggested, "_")
	name = strings.TrimLeft(repeatedDots.ReplaceAllString(name, "."), ".")
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		return "spill.txt"
	}
	return name
}
func comma(n int) string {
	digits := fmt.Sprint(n)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}

func (s *LocalStore) SaveText(ctx context.Context, request Request) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	if !utf8.ValidString(string(request.Namespace)) || !utf8.ValidString(request.Content) {
		return Ref{}, errors.New("spill namespace and content must be UTF-8")
	}
	if len(request.Content) > MaxBytes {
		return Ref{}, fmt.Errorf("spill artifact is %s bytes; limit %s", comma(len(request.Content)), comma(MaxBytes))
	}
	digest := sha256.Sum256([]byte(request.Namespace))
	directory := filepath.Join(s.root, "session-"+hex.EncodeToString(digest[:8]))
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return Ref{}, err
	}
	token, err := s.token()
	if err != nil {
		return Ref{}, err
	}
	path := filepath.Join(directory, token+"-"+safeName(request.SuggestedName))
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Ref{}, err
	}
	owned, ownErr := file.Stat()
	complete := false
	defer func() {
		file.Close()
		// Failed/cancelled writes never advertise an artifact. Cleanup is best-
		// effort after checking the created inode. Check/unlink is not atomic
		// against concurrent host tampering; this is not a filesystem sandbox.
		if !complete {
			current, currentErr := os.Lstat(path)
			if ownErr == nil && currentErr == nil && os.SameFile(owned, current) {
				os.Remove(path)
			}
		}
	}()
	written, err := file.Write([]byte(request.Content))
	if err == nil && written != len(request.Content) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		return Ref{}, err
	}
	if err = file.Close(); err != nil {
		return Ref{}, err
	}
	complete = true
	return Ref{Locator: path, Bytes: len(request.Content), RetrievalHint: "outside the workspace; read it with bash, e.g. sed -n '1,200p' " + path + " or grep <pattern> " + path}, nil
}
