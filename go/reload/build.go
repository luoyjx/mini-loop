package reload

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// BuildConfig is trusted development configuration, never model tool input.
type BuildConfig struct {
	ModuleDirectory string
	// Package selects one local package; empty selects ./cmd/miniloop.
	Package string
	// Toolchain selects an existing Go executable; empty resolves go once.
	Toolchain string
	// Nil captures the current environment once; empty means an explicit empty base.
	Environment     []string
	TemporaryParent string
}

type Builder struct {
	module, target, toolchain, temporaryParent string
	environment                                []string
}

func (*Builder) String() string           { return "<native reload builder>" }
func (builder *Builder) GoString() string { return builder.String() }

// Artifact owns exactly one private temporary directory. The supervisor must
// join its child before Close. Closing never removes the source module.
type Artifact struct {
	mu         sync.Mutex
	root, path string
	digest     [sha256.Size]byte
	closed     bool
}

func NewBuilder(config BuildConfig) (*Builder, error) {
	if config.ModuleDirectory == "" {
		return nil, errors.New("reload build requires an explicit module directory")
	}
	module, err := filepath.Abs(config.ModuleDirectory)
	if err != nil {
		return nil, err
	}
	module, err = filepath.EvalSymlinks(module)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(filepath.Join(module, "go.mod"))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("reload module requires a regular go.mod")
	}
	target := config.Package
	if target == "" {
		target = "./cmd/miniloop"
	}
	if target != "." && !strings.HasPrefix(target, "./") || strings.Contains(target, "...") || strings.ContainsAny(target, "\\\x00") {
		return nil, errors.New("reload build requires one local package")
	}
	clean := filepath.Clean(target)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return nil, errors.New("reload package must remain in the module")
	}
	if clean == "." {
		target = "."
	} else {
		target = "./" + filepath.ToSlash(clean)
	}
	toolchain := config.Toolchain
	if toolchain == "" {
		toolchain = "go"
	}
	toolchain, err = exec.LookPath(toolchain)
	if err != nil {
		return nil, err
	}
	toolchain, err = filepath.Abs(toolchain)
	if err != nil {
		return nil, err
	}
	parent := config.TemporaryParent
	if parent != "" {
		parent, err = filepath.Abs(parent)
		if err != nil {
			return nil, err
		}
	}
	env := config.Environment
	if env == nil {
		env = os.Environ()
	}
	env = append([]string{}, env...)
	// A development child must run on this host. Use the installed toolchain;
	// an edited go.mod cannot cause an automatic toolchain download.
	env = append(env, "GOTOOLCHAIN=local", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
	return &Builder{module: module, target: target, toolchain: toolchain, temporaryParent: parent, environment: env}, nil
}

// Build streams diagnostics to the caller and returns only a validated Go
// executable. Each attempt owns a fresh output directory; failed attempts clean
// it before returning. Module edits are compiled anew on subsequent attempts.
func (builder *Builder) Build(ctx context.Context, diagnostics io.Writer) (artifact *Artifact, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp(builder.temporaryParent, "miniloop-reload-build-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if artifact == nil {
			err = errors.Join(err, os.RemoveAll(root))
		}
	}()
	path := filepath.Join(root, "miniloop")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if diagnostics == nil {
		diagnostics = io.Discard
	}
	command := exec.CommandContext(ctx, builder.toolchain, "build", "-mod=readonly", "-buildmode=exe", "-o", path, builder.target)
	command.Dir, command.Env = builder.module, append([]string{}, builder.environment...)
	command.Stdout, command.Stderr = diagnostics, diagnostics
	configureBuildGroup(command)
	command.Cancel = func() error {
		killBuildGroup(command.Process)
		return nil
	}
	// Drain inherited diagnostic pipes even if a misbehaving compiler exits while
	// leaving descendants alive. The owned group is killed after Wait returns.
	command.WaitDelay = 2 * time.Second
	err = command.Run()
	if command.Process != nil {
		killBuildGroup(command.Process)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("native reload build failed: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(errors.New("reload compiler did not produce a regular executable"), err)
	}
	if _, err := buildinfo.ReadFile(path); err != nil {
		return nil, fmt.Errorf("reload output is not a Go executable: %w", err)
	}
	if err := os.Chmod(path, 0700); err != nil {
		return nil, err
	}
	digest, err := artifactDigest(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Artifact{root: root, path: path, digest: digest}, nil
}

func artifactDigest(ctx context.Context, path string) ([sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return [sha256.Size]byte{}, err
		}
		n, err := file.Read(buffer)
		hash.Write(buffer[:n])
		if err == io.EOF {
			var digest [sha256.Size]byte
			copy(digest[:], hash.Sum(nil))
			return digest, nil
		}
		if err != nil {
			return [sha256.Size]byte{}, err
		}
	}
}

// Executable checks retained bytes before returning the launch path. This is an
// integrity check within the operator's filesystem, not an OS execution sandbox.
func (artifact *Artifact) Executable(ctx context.Context) (string, error) {
	artifact.mu.Lock()
	defer artifact.mu.Unlock()
	if artifact.closed {
		return "", errors.New("reload artifact is closed")
	}
	info, err := os.Lstat(artifact.path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.Join(errors.New("reload artifact is not a regular file"), err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0100 == 0 {
		return "", errors.New("reload artifact is not executable")
	}
	digest, err := artifactDigest(ctx, artifact.path)
	if err != nil {
		return "", err
	}
	if digest != artifact.digest {
		return "", errors.New("reload artifact changed after build")
	}
	return artifact.path, nil
}

func (artifact *Artifact) Close() error {
	artifact.mu.Lock()
	defer artifact.mu.Unlock()
	if artifact.closed {
		return nil
	}
	if err := os.RemoveAll(artifact.root); err != nil {
		return err
	}
	artifact.closed = true
	return nil
}
