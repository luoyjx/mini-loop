// Package sandbox implements opt-in Source-compatible shell confinement.
// Configuration metadata does not attest to successful OS enforcement.
package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/workspace"
)

const Binary = "/usr/bin/sandbox-exec"
const shellBinary = "/bin/sh"

// Config is copied at construction. Only ExtraWritable survives workspace rebind.
type Config struct {
	WritableRoots   []string
	UnreadableRoots []string
	ExtraWritable   []string
	AllowNetwork    bool
}

type SeatbeltSandbox struct{ config Config }
type NullSandbox struct{}
type UnavailableSandbox struct{ reason string }
type SandboxUnavailable struct{ Reason string }

func (err *SandboxUnavailable) Error() string { return err.Reason }

var ErrNoWritableRoots = errors.New("a sandbox with no writable root cannot run a shell; pass the session workspace")

func (sandbox *NullSandbox) ForWorkspace(string) (shell.Sandbox, error) { return sandbox, nil }
func (NullSandbox) Argv(command string) ([]string, error) {
	return []string{shellBinary, "-c", command}, nil
}
func (NullSandbox) Describe() string { return "none (host execution)" }
func (NullSandbox) Confined() bool   { return false }

func NewUnavailable(reason string) *UnavailableSandbox                         { return &UnavailableSandbox{reason: reason} }
func (sandbox *UnavailableSandbox) ForWorkspace(string) (shell.Sandbox, error) { return sandbox, nil }
func (sandbox *UnavailableSandbox) Argv(command string) ([]string, error) {
	return NullSandbox{}.Argv(command)
}
func (sandbox *UnavailableSandbox) Describe() string       { return "unavailable: " + sandbox.reason }
func (sandbox *UnavailableSandbox) Confined() bool         { return false }
func (sandbox *UnavailableSandbox) SandboxReason() *string { reason := sandbox.reason; return &reason }

func NewSeatbelt(config Config) (*SeatbeltSandbox, error) {
	var err error
	config.WritableRoots, err = resolveRoots(config.WritableRoots)
	if err != nil {
		return nil, err
	}
	config.UnreadableRoots, err = resolveRoots(config.UnreadableRoots)
	if err != nil {
		return nil, err
	}
	config.ExtraWritable, err = resolveRoots(config.ExtraWritable)
	if err != nil {
		return nil, err
	}
	for _, extra := range config.ExtraWritable {
		if !slices.Contains(config.WritableRoots, extra) {
			config.WritableRoots = append(config.WritableRoots, extra)
		}
	}
	for _, root := range slices.Clone(config.WritableRoots) {
		common, err := worktreeCommonDir(root)
		if err != nil {
			return nil, err
		}
		if common != "" && !slices.Contains(config.WritableRoots, common) {
			config.WritableRoots = append(config.WritableRoots, common)
		}
	}
	if len(config.WritableRoots) == 0 {
		return nil, ErrNoWritableRoots
	}
	return &SeatbeltSandbox{config: config}, nil
}

// Configuration returns detached slices; concurrent command construction is read-only.
func (sandbox *SeatbeltSandbox) Configuration() Config {
	config := sandbox.config
	config.WritableRoots = slices.Clone(config.WritableRoots)
	config.UnreadableRoots = slices.Clone(config.UnreadableRoots)
	config.ExtraWritable = slices.Clone(config.ExtraWritable)
	return config
}
func (sandbox *SeatbeltSandbox) Confined() bool { return true }
func (sandbox *SeatbeltSandbox) ForWorkspace(path string) (shell.Sandbox, error) {
	target, err := resolve(path)
	if err != nil {
		return nil, err
	}
	if len(sandbox.config.WritableRoots) == 1 && sandbox.config.WritableRoots[0] == target {
		return sandbox, nil
	}
	config := sandbox.Configuration()
	config.WritableRoots = []string{target}
	return NewSeatbelt(config)
}
func (sandbox *SeatbeltSandbox) Describe() string {
	reads := strings.Join(sandbox.config.UnreadableRoots, ", ")
	if reads == "" {
		reads = "none"
	}
	network := "network denied"
	if sandbox.config.AllowNetwork {
		network = "network allowed"
	}
	return fmt.Sprintf("seatbelt (write: %s; no-read: %s; %s)", strings.Join(sandbox.config.WritableRoots, ", "), reads, network)
}
func (sandbox *SeatbeltSandbox) Preview(command string) string {
	argv, _ := sandbox.Argv(command)
	for index, part := range argv {
		argv[index] = quote(part)
	}
	return strings.Join(argv, " ")
}

func resolveRoots(roots []string) ([]string, error) {
	result := make([]string, 0, len(roots))
	for _, path := range roots {
		root, err := resolve(path)
		if err != nil {
			return nil, err
		}
		result = append(result, root)
	}
	return result, nil
}
func resolve(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		index := strings.IndexByte(path, '/')
		if index < 0 {
			index = len(path)
		}
		var home string
		var err error
		if index == 1 {
			home, err = homeDirectory()
		} else {
			var account *user.User
			account, err = user.Lookup(path[1:index])
			if err == nil {
				home = account.HomeDir
			}
		}
		if err != nil {
			return "", errors.New("Could not determine home directory.")
		}
		path = strings.TrimRight(home, "/") + path[index:]
		if path == "" {
			path = "/"
		}
	}
	return workspace.ResolvePath(path)
}
func homeDirectory() (string, error) {
	if home, exists := os.LookupEnv("HOME"); exists {
		return home, nil
	}
	account, err := user.Current()
	if err != nil {
		return "", err
	}
	return account.HomeDir, nil
}

func worktreeCommonDir(root string) (string, error) {
	marker := filepath.Join(root, ".git")
	info, err := os.Stat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return "", nil
	}
	for offset := 0; offset < len(data); {
		_, width, issue := pytext.DecodeRune(data[offset:])
		if issue != pytext.ValidUTF8 {
			return "", &MarkerEncodingError{Offset: offset, End: offset + width - 1, Byte: data[offset], Issue: issue}
		}
		offset += width
	}
	for _, line := range strings.FieldsFunc(string(data), lineBreak) {
		if !strings.HasPrefix(line, "gitdir:") {
			continue
		}
		gitdir := pytext.Strip(strings.TrimPrefix(line, "gitdir:"))
		if !filepath.IsAbs(gitdir) {
			gitdir = root + string(filepath.Separator) + gitdir
		}
		resolved, err := workspace.ResolvePath(gitdir)
		if err != nil {
			return "", err
		}
		common := filepath.Dir(filepath.Dir(resolved))
		if filepath.Base(common) == ".git" {
			return common, nil
		}
	}
	return "", nil
}
func lineBreak(value rune) bool {
	return value == '\n' || value == '\r' || value == '\v' || value == '\f' || value == '\x1c' || value == '\x1d' || value == '\x1e' || value == '\x85' || value == '\u2028' || value == '\u2029'
}

type MarkerEncodingError struct {
	Offset, End int
	Byte        byte
	Issue       pytext.UTF8Issue
}

func (err *MarkerEncodingError) Error() string {
	if err.End > err.Offset {
		return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: %s", err.Offset, err.End, err.Issue)
	}
	return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", err.Byte, err.Offset, err.Issue)
}

func quote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r))
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// DefaultOptions keeps credential protection enabled when the option is omitted.
type DefaultOptions struct {
	UnreadableRoots    []string
	AllowNetwork       bool
	Require            bool
	ProtectCredentials *bool
}
type hostFacts struct {
	system       string
	binaryExists bool
}

func currentHost() hostFacts {
	system := runtime.GOOS
	switch system {
	case "darwin":
		system = "Darwin"
	case "linux", "android":
		system = "Linux"
	case "windows":
		system = "Windows"
	case "freebsd":
		system = "FreeBSD"
	case "openbsd":
		system = "OpenBSD"
	case "netbsd":
		system = "NetBSD"
	case "dragonfly":
		system = "DragonFly"
	case "solaris", "illumos":
		system = "SunOS"
	case "aix":
		system = "AIX"
	}
	_, err := os.Stat(Binary)
	return hostFacts{system: system, binaryExists: err == nil}
}
func (host hostFacts) reason() *string {
	if host.system == "Darwin" && host.binaryExists {
		return nil
	}
	reason := Binary + " is missing on this macOS host"
	if host.system != "Darwin" {
		reason = fmt.Sprintf("Seatbelt is macOS-only and this host is %s; there is no Linux or Windows backend yet, so confinement needs a container", host.system)
	}
	return &reason
}
func Available() bool            { return currentHost().reason() == nil }
func UnavailableReason() *string { return currentHost().reason() }
func DefaultUnreadableRoots() ([]string, error) {
	home, err := homeDirectory()
	if err != nil {
		return nil, err
	}
	if home == "" {
		home = "/"
	}
	roots := make([]string, 0, 8)
	for _, relative := range []string{".ssh", ".aws", ".gnupg", ".kube", ".config/gcloud", ".netrc", ".docker/config.json", ".config/gh"} {
		roots = append(roots, filepath.Join(home, relative))
	}
	return roots, nil
}
func NewDefault(path string, options DefaultOptions) (shell.Sandbox, error) {
	return defaultForHost(path, options, currentHost())
}
func defaultForHost(path string, options DefaultOptions, host hostFacts) (shell.Sandbox, error) {
	if reason := host.reason(); reason != nil {
		if options.Require {
			return nil, &SandboxUnavailable{Reason: *reason}
		}
		return NewUnavailable(*reason), nil
	}
	roots := slices.Clone(options.UnreadableRoots)
	if options.ProtectCredentials == nil || *options.ProtectCredentials {
		credentials, err := DefaultUnreadableRoots()
		if err != nil {
			return nil, err
		}
		roots = append(credentials, roots...)
	}
	return NewSeatbelt(Config{WritableRoots: []string{path}, UnreadableRoots: roots, AllowNetwork: options.AllowNetwork})
}
