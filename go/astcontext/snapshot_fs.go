package astcontext

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
)

// These primitives are private to the forthcoming snapshot builder. The root
// must already be canonical and trusted; child names are single components.
type snapshotDirectory struct{ file *os.File }

type snapshotEntryKind uint8

const (
	snapshotSpecial snapshotEntryKind = iota
	snapshotRegular
	snapshotDir
	snapshotSymlink
)

type snapshotMetadata struct {
	kind snapshotEntryKind
	size int64
}

var errSnapshotEntryLimit = errors.New("snapshot directory entry limit exceeded")
var errSnapshotPlatform = errors.New("secure ast-outline snapshots require dir_fd and O_NOFOLLOW support")

func snapshotContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("snapshot context is required")
	}
	return ctx.Err()
}

func snapshotComponent(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return errors.New("snapshot entry must be a single child name")
	}
	return nil
}

func (directory *snapshotDirectory) close() error { return directory.file.Close() }

// An independently opened "." has its own directory offset. A dup would share
// that offset with the anchor and break overlapping/repeated target scans.
func (directory *snapshotDirectory) names(ctx context.Context, limit int) ([]string, error) {
	if err := snapshotContext(ctx); err != nil {
		return nil, err
	}
	scan, err := directory.scanFile()
	if err != nil {
		return nil, err
	}
	defer scan.Close()
	names := []string{}
	for {
		if err := snapshotContext(ctx); err != nil {
			return nil, err
		}
		batch, err := scan.Readdirnames(128)
		for _, name := range batch {
			if err := snapshotContext(ctx); err != nil {
				return nil, err
			}
			if len(names) >= limit {
				return nil, errSnapshotEntryLimit
			}
			names = append(names, name)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(names)
	return names, nil
}

func (directory *snapshotDirectory) parent(ctx context.Context, parts []string) (*snapshotDirectory, error) {
	if err := snapshotContext(ctx); err != nil {
		return nil, err
	}
	file, err := directory.scanFile()
	if err != nil {
		return nil, err
	}
	current := &snapshotDirectory{file: file}
	for _, part := range parts {
		next, err := current.child(ctx, part)
		current.close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}
