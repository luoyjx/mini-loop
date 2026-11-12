package assistant

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A tenant whose owner has been absent for ArchiveAfter is compressed into
// one file beside where its directory was. Its databases are closed while it
// sleeps, so the archive is a consistent copy. The rule that keeps archiving
// safe across crashes: when the directory exists it wins, and an archive
// beside it is a leftover.

// maxArchiveBytes bounds what one restore unpacks.
const maxArchiveBytes = 4 << 30

func archivePath(dir string) string { return dir + ".tar.gz" }

// archiveAbsent archives a batch of sleeping tenants whose owners were last
// seen before cutoff. Each holds its tenant's slot meanwhile, so a request
// for it waits and then restores it.
func (f *Fleet) archiveAbsent(ctx context.Context, cutoff time.Time) {
	ids, err := f.registry.archivable(ctx, cutoff, 32)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(f.log, "fleet: list archivable tenants: %v\n", err)
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		f.mu.Lock()
		if _, open := f.tenants[id]; open || f.closed {
			f.mu.Unlock()
			continue
		}
		hold := &tenant{id: id, closing: true, gone: make(chan struct{})}
		f.tenants[id] = hold
		f.mu.Unlock()
		if err := f.archive(ctx, id); err != nil {
			fmt.Fprintf(f.log, "fleet: archive %s: %v\n", id, err)
		}
		f.mu.Lock()
		delete(f.tenants, id)
		f.mu.Unlock()
		close(hold.gone)
	}
}

func (f *Fleet) archive(ctx context.Context, id TenantID) error {
	dir := f.dir(id)
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	target := archivePath(dir)
	temp := target + ".tmp"
	if err := writeArchive(dir, temp); err != nil {
		return errors.Join(err, removeIfExists(temp))
	}
	if err := os.Rename(temp, target); err != nil {
		return errors.Join(err, removeIfExists(temp))
	}
	marked, err := f.registry.archived(ctx, id)
	if err != nil || !marked {
		return errors.Join(err, removeIfExists(target))
	}
	f.counters.archives.Add(1)
	return os.RemoveAll(dir)
}

// writeArchive writes dir's directories and regular files to a gzip tar.
func writeArchive(dir, path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	zipper, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return errors.Join(err, file.Close())
	}
	archive := tar.NewWriter(zipper)
	walk := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return err
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name, header.Uname, header.Gname = filepath.ToSlash(rel), "", ""
		if entry.IsDir() {
			header.Name += "/"
		}
		if err := archive.WriteHeader(header); err != nil || entry.IsDir() {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(archive, source)
		return errors.Join(err, source.Close())
	})
	err = errors.Join(walk, archive.Close(), zipper.Close())
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

// restore brings back the directory of an archived tenant before it opens.
func (f *Fleet) restore(record TenantRecord, dir string) error {
	archive := archivePath(dir)
	if _, err := os.Stat(dir); err == nil {
		return removeIfExists(archive)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(archive); errors.Is(err, os.ErrNotExist) {
		if record.State == TenantCold {
			return fmt.Errorf("archive %s is missing", archive)
		}
		return nil
	}
	temp := dir + ".restoring"
	if err := os.RemoveAll(temp); err != nil {
		return err
	}
	if err := readArchive(archive, temp); err != nil {
		return errors.Join(err, os.RemoveAll(temp))
	}
	if err := os.Rename(temp, dir); err != nil {
		return errors.Join(err, os.RemoveAll(temp))
	}
	f.counters.restores.Add(1)
	return removeIfExists(archive)
}

// readArchive unpacks only directories and regular files, and only inside
// dir.
func readArchive(path, dir string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	unzipper, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer unzipper.Close()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	archive := tar.NewReader(unzipper)
	var total int64
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || name == "." {
			return fmt.Errorf("archive entry %q escapes the tenant directory", header.Name)
		}
		target := filepath.Join(dir, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			total += header.Size
			if total > maxArchiveBytes {
				return errors.New("archive is larger than a tenant may be")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, err = io.CopyN(out, archive, header.Size)
			if err := errors.Join(err, out.Close()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive entry %q has unsupported type %c", header.Name, header.Typeflag)
		}
	}
}
