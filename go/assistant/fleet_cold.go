package assistant

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A fleet's tenants are not all needed alike, and the places that keep them
// do not cost alike. A tenant's data is in one of three:
//
//   - hot: on the disk of the node that has it open, or had. This is a cache
//     and is bounded by age and by size (see sweepCache).
//   - warm: as pages in a shard, where any node can open it in milliseconds.
//     A database is the dearest place to keep a byte.
//   - cold: as one compressed object in the archive store, once its owner has
//     been away for ArchiveAfter and nothing is due to wake it soon. Object
//     storage costs a fraction of a database, and a tenant there costs the
//     shard nothing: no rows, no index entries, nothing to back up or vacuum.
//
// An archived tenant is read back by the first node that opens it. The rule
// that keeps the two-step moves between shard and archive safe: while the
// registry says a tenant is cold, the archive is its data and whatever a
// shard holds of it is a leftover; once it says warm, the shard is.

// coldStore keeps archived tenants as objects, one per tenant.
type coldStore interface {
	// put stores body, whose SHA-256 is sum, replacing what is there.
	put(ctx context.Context, key string, body io.ReadSeeker, size int64, sum [sha256.Size]byte) error
	// get copies the object out; errNoArchive when there is none.
	get(ctx context.Context, key string, into io.Writer) error
	// remove deletes the object; one that is not there is removed.
	remove(ctx context.Context, key string) error
	String() string
}

var (
	errNoArchive = errors.New("the archive is missing")
	// errNothingStored means a tenant has no data in its shard to archive.
	errNothingStored = errors.New("the tenant has nothing stored")
)

// coldMargin is how soon a tenant must not be due for it to be archived.
const coldMargin = 24 * time.Hour

// coldKey names one archive of a tenant. born keeps a deleted tenant's
// archives apart from a later tenant's with the same name, mark one archiving
// from the next, and the two leading characters spread the keys.
func coldKey(id TenantID, born int64, mark string) string {
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%s/%s.%d.%s.tar.gz", hex.EncodeToString(sum[:1]), id, born, mark)
}

// ArchiveStore says where archived tenants go. URL is a directory
// (file:///mnt/archive), the base URL of an S3-compatible bucket, with any
// prefix (https://minio.internal:9000/fleet/archive, or the bucket's own
// host as https://fleet.oss-cn-hangzhou.aliyuncs.com/archive), or
// s3://bucket/prefix for Amazon S3. The keys sign the requests and are not
// part of the URL.
type ArchiveStore struct {
	URL             string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

var s3PathPattern = regexp.MustCompile(`^[A-Za-z0-9._~/-]*$`)

func openColdStore(config ArchiveStore) (coldStore, error) {
	if config.URL == "" {
		return nil, nil
	}
	parsed, err := url.Parse(config.URL)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("the archive store must be file:///directory, https://host/bucket/prefix or s3://bucket/prefix")
	}
	region := config.Region
	if region == "" {
		region = "us-east-1"
	}
	switch parsed.Scheme {
	case "file":
		if parsed.Host != "" || !filepath.IsAbs(parsed.Path) {
			return nil, errors.New("a file archive store needs an absolute path, as in file:///mnt/archive")
		}
		if err := os.MkdirAll(parsed.Path, 0o700); err != nil {
			return nil, err
		}
		return &dirStore{root: filepath.Clean(parsed.Path)}, nil
	case "s3":
		if parsed.Host == "" {
			return nil, errors.New("an s3 archive store names its bucket, as in s3://bucket/prefix")
		}
		parsed = &url.URL{Scheme: "https", Host: parsed.Host + ".s3." + region + ".amazonaws.com", Path: parsed.Path}
	case "http", "https":
	default:
		return nil, errors.New("the archive store must be file:///directory, https://host/bucket/prefix or s3://bucket/prefix")
	}
	if parsed.Host == "" || !s3PathPattern.MatchString(parsed.Path) {
		return nil, errors.New("the archive store's URL may hold only letters, digits and ._~/- in its path")
	}
	if config.AccessKeyID == "" || config.SecretAccessKey == "" {
		return nil, errors.New("an S3 archive store needs an access key ID and a secret access key")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return &s3Store{
		base: parsed, region: region, access: config.AccessKeyID, secret: config.SecretAccessKey, session: config.SessionToken,
		client: &http.Client{Transport: storeTransport()}, now: time.Now,
	}, nil
}

// storeTransport gives up on a store that takes a request and then says
// nothing.
func storeTransport() http.RoundTripper {
	transport := nodeTransport().(*http.Transport)
	transport.ResponseHeaderTimeout = time.Minute
	return transport
}

// thawLimit bounds reading one archive back. Its size is not known before it
// is read, so this is the time the largest tenant takes.
const thawLimit = time.Hour

// ---- a directory ----

// dirStore keeps the archives in a directory, which every node must see: a
// network filesystem, or one host.
type dirStore struct{ root string }

func (d *dirStore) String() string { return "file://" + d.root }

func (d *dirStore) path(key string) string { return filepath.Join(d.root, filepath.FromSlash(key)) }

func (d *dirStore) put(_ context.Context, key string, body io.ReadSeeker, _ int64, _ [sha256.Size]byte) error {
	path := d.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".put-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(temp, body)
	if err == nil {
		err = temp.Sync()
	}
	if err = errors.Join(err, temp.Close()); err != nil {
		return errors.Join(err, os.Remove(temp.Name()))
	}
	return os.Rename(temp.Name(), path)
}

func (d *dirStore) get(_ context.Context, key string, into io.Writer) error {
	file, err := os.Open(d.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return errNoArchive
	}
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(into, file)
	return err
}

func (d *dirStore) remove(_ context.Context, key string) error { return removeIfExists(d.path(key)) }

// ---- S3 ----

// s3Store keeps the archives in a bucket of Amazon S3 or of a store that
// speaks its API. It needs three requests, each signed with AWS Signature
// Version 4: PUT, GET and DELETE of one object.
type s3Store struct {
	base                            *url.URL
	region, access, secret, session string
	client                          *http.Client
	now                             func() time.Time
}

func (s *s3Store) String() string { return s.base.String() }

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// sign adds the headers that authorise a request whose body has the given
// SHA-256. Every header already set is signed, and the host.
func (s *s3Store) sign(request *http.Request, payload string) {
	now := s.now().UTC()
	stamp, day := now.Format("20060102T150405Z"), now.Format("20060102")
	request.Header.Set("X-Amz-Date", stamp)
	request.Header.Set("X-Amz-Content-Sha256", payload)
	if s.session != "" {
		request.Header.Set("X-Amz-Security-Token", s.session)
	}
	names := []string{"host"}
	headers := map[string]string{"host": request.URL.Host}
	for name, values := range request.Header {
		lower := strings.ToLower(name)
		names = append(names, lower)
		headers[lower] = strings.Join(strings.Fields(strings.Join(values, ",")), " ")
	}
	sort.Strings(names)
	canonical := strings.Builder{}
	canonical.WriteString(request.Method + "\n" + request.URL.EscapedPath() + "\n" + request.URL.RawQuery + "\n")
	for _, name := range names {
		canonical.WriteString(name + ":" + headers[name] + "\n")
	}
	signed := strings.Join(names, ";")
	canonical.WriteString("\n" + signed + "\n" + payload)
	scope := day + "/" + s.region + "/s3/aws4_request"
	digest := sha256.Sum256([]byte(canonical.String()))
	text := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(digest[:])
	mac := func(key []byte, message string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(message))
		return h.Sum(nil)
	}
	key := mac(mac(mac(mac([]byte("AWS4"+s.secret), day), s.region), "s3"), "aws4_request")
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.access+"/"+scope+", SignedHeaders="+signed+", Signature="+hex.EncodeToString(mac(key, text)))
}

// send signs and sends one request and returns the response when its status
// is one of those wanted.
func (s *s3Store) send(ctx context.Context, method, key string, body io.ReadSeeker, size int64, payload string, want ...int) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, s.base.JoinPath(key).String(), nil)
	if err != nil {
		return nil, err
	}
	if body != nil {
		// The file stays the caller's to close, and can be sent again when a
		// kept connection turns out to be gone.
		request.Body, request.ContentLength = io.NopCloser(body), size
		request.GetBody = func() (io.ReadCloser, error) {
			_, err := body.Seek(0, io.SeekStart)
			return io.NopCloser(body), err
		}
	}
	s.sign(request, payload)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("archive store: %w", err)
	}
	for _, status := range want {
		if response.StatusCode == status {
			return response, nil
		}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, errNoArchive
	}
	said, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	return nil, fmt.Errorf("archive store: %s %s: %s %s", method, key, response.Status, s.withoutKeys(strings.Join(strings.Fields(string(said)), " ")))
}

// withoutKeys takes the store's keys out of what the store said. S3 names
// the access key ID in the answer to a request it refuses.
func (s *s3Store) withoutKeys(text string) string {
	for _, key := range []string{s.secret, s.session, s.access} {
		if key != "" {
			text = strings.ReplaceAll(text, key, "<key>")
		}
	}
	return text
}

func (s *s3Store) put(ctx context.Context, key string, body io.ReadSeeker, size int64, sum [sha256.Size]byte) error {
	response, err := s.send(ctx, http.MethodPut, key, body, size, hex.EncodeToString(sum[:]), http.StatusOK, http.StatusCreated, http.StatusNoContent)
	if err != nil {
		return err
	}
	return response.Body.Close()
}

func (s *s3Store) get(ctx context.Context, key string, into io.Writer) error {
	response, err := s.send(ctx, http.MethodGet, key, nil, 0, emptySHA256, http.StatusOK)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	copied, err := io.Copy(into, io.LimitReader(response.Body, maxArchiveBytes+1))
	if err == nil && copied > maxArchiveBytes {
		err = errors.New("archive is larger than a tenant may be")
	}
	return err
}

func (s *s3Store) remove(ctx context.Context, key string) error {
	response, err := s.send(ctx, http.MethodDelete, key, nil, 0, emptySHA256, http.StatusOK, http.StatusNoContent, http.StatusAccepted)
	if errors.Is(err, errNoArchive) {
		return nil
	}
	if err != nil {
		return err
	}
	return response.Body.Close()
}

// ---- shard to archive and back ----

// transit makes a file for an archive on its way to or from the store, and
// returns how to be rid of it.
func (v *vault) transit(pattern string) (*os.File, func(), error) {
	if err := os.MkdirAll(v.tmp, 0o700); err != nil {
		return nil, nil, err
	}
	file, err := os.CreateTemp(v.tmp, pattern)
	if err != nil {
		return nil, nil, err
	}
	return file, func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}, nil
}

// freeze moves a sleeping tenant's data from its shard to the archive store.
// The caller holds the tenant's placement. The archive is complete in the
// store before the registry calls the tenant cold, and the shard's copy goes
// only after that.
func (v *vault) freeze(ctx context.Context, record TenantRecord, node string) error {
	shard, err := v.shard(record.Shard)
	if err != nil {
		return err
	}
	id, born := record.ID, millis(record.CreatedAt)
	head, err := v.take(ctx, shard, id, node, born, false)
	if err != nil {
		return err
	}
	if head.seq == 0 {
		return errNothingStored
	}
	// Read from the shard and written to the store: twice its size.
	ctx, cancel := bounded(ctx, 2*head.bytes)
	defer cancel()
	file, discard, err := v.transit("freeze-*.tar.gz")
	if err != nil {
		return err
	}
	defer discard()
	digest := sha256.New()
	var total int64
	err = shard.do(ctx, func(db *fleetDB) (err error) {
		total, err = v.writeStored(ctx, db, id, born, node, head.seq, io.MultiWriter(file, digest))
		return err
	})
	if err != nil {
		return err
	}
	size, err := file.Seek(0, io.SeekCurrent)
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		return err
	}
	// The object is written down as a copy to remove before it exists, and
	// stops being one in the statement that makes it the tenant's data.
	mark := randomHex(8)
	object := leftover{id, born, archiveAt(mark)}
	if err := v.registry.intend(ctx, object, v.now()); err != nil {
		return err
	}
	v.at("store")
	if err := v.cold.put(ctx, coldKey(id, born, mark), file, size, [sha256.Size]byte(digest.Sum(nil))); err != nil {
		return err
	}
	v.at("freeze")
	frozen, err := v.registry.frozen(ctx, id, born, node, shard.name, mark, v.now())
	if err != nil {
		return err
	}
	if !frozen {
		// The tenant was taken from this node meanwhile and the shard is
		// still its data. If the object's row was acted on before the
		// object was there, it is written down again.
		return errors.Join(v.registry.intend(ctx, object, v.now()), fmt.Errorf("%w: tenant %s was taken while it was being archived", errFenced, id))
	}
	v.counters.freezes.Add(1)
	v.counters.frozenBytes.Add(total)
	v.counters.archiveBytes.Add(size)
	v.at("frozen")
	// This node took the shard's copy when the archiving began, and removes
	// it only if no node took it since.
	return v.clean(ctx, leftover{id, born, shard.name}, node)
}

// writeStored writes what a shard holds of a tenant as a gzip tar, from one
// snapshot, and returns the size of the files. A large file is read from its
// object and pages into the archive, which then holds all of the tenant. The
// caller's deadline bounds it.
func (v *vault) writeStored(ctx context.Context, db *fleetDB, id TenantID, born int64, node string, seq int64, into io.Writer) (int64, error) {
	tx, err := db.begin(ctx, true)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var held int64
	if err := tx.QueryRowContext(ctx, `SELECT seq FROM vault_heads WHERE tenant_id=? AND node=?`, string(id), node).Scan(&held); err != nil || held != seq {
		return 0, errors.Join(err, errFenced)
	}
	files, total, err := storedFiles(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	zipper := gzip.NewWriter(into)
	archive := tar.NewWriter(zipper)
	for _, file := range files {
		if err := archive.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: file.path, Size: file.size, Mode: 0o600}); err != nil {
			return 0, err
		}
		if err := v.archiveFile(ctx, tx, id, born, file, archive); err != nil {
			return 0, fmt.Errorf("%s: %w", file.path, err)
		}
	}
	return total, errors.Join(archive.Close(), zipper.Close())
}

// archiveFile writes one stored file into an archive. A large file is put
// together in a file of the node's first, as only that can be written out of
// order.
func (v *vault) archiveFile(ctx context.Context, tx *fleetTx, id TenantID, born int64, file storedFile, archive io.Writer) error {
	if file.base == "" {
		return storedPages(ctx, tx, id, file, func(_ int, data []byte) error {
			_, err := archive.Write(data)
			return err
		})
	}
	whole, discard, err := v.transit("file-*")
	if err != nil {
		return err
	}
	defer discard()
	if err := v.writeFile(ctx, tx, id, born, file, whole); err != nil {
		return err
	}
	if _, err := whole.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = io.Copy(archive, whole)
	return err
}

// thaw reads an archived tenant back: the archive is unpacked as the node's
// copy and stored in the tenant's shard, which the caller has just emptied,
// as the tenant's first checkpoint there. Only then does the registry call
// the tenant warm; a node that stops before that leaves a tenant that is
// still cold and whose archive is still whole.
func (r *replica) thaw(ctx context.Context) error {
	v := r.vault
	if v.cold == nil || r.archive == "" {
		return errors.New("the tenant is archived and this node has no archive store, or the registry does not say which archive")
	}
	if err := os.RemoveAll(r.dir); err != nil {
		return err
	}
	file, discard, err := v.transit("thaw-*.tar.gz")
	if err != nil {
		return err
	}
	defer discard()
	reading, cancel := context.WithTimeout(ctx, thawLimit)
	err = v.cold.get(reading, coldKey(r.id, r.born, r.archive), file)
	cancel()
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := readArchive(file.Name(), r.dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(r.dir, vaultDir), 0o700); err != nil {
		return err
	}
	if _, err := r.ship(ctx); err != nil {
		return err
	}
	v.at("thaw")
	thawed, err := v.registry.thawed(ctx, r.id, r.born, r.node, r.archive, v.now())
	if err != nil {
		return err
	}
	if !thawed {
		return fmt.Errorf("%w: tenant %s was taken while it was being read back", errFenced, r.id)
	}
	v.counters.thaws.Add(1)
	// The archive goes now. If the store does not answer it stays written
	// down as a leftover and goes later.
	_ = v.clean(ctx, leftover{r.id, r.born, archiveAt(r.archive)}, r.node)
	return nil
}

// freezeAbsent archives a batch of sleeping tenants whose owners were last
// seen before cutoff and that nothing wakes soon.
func (f *Fleet) freezeAbsent(ctx context.Context, cutoff time.Time) {
	ids, err := f.registry.freezable(ctx, cutoff, f.now().Add(coldMargin), 32)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(f.log, "fleet: list tenants to archive: %v\n", err)
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		release, err := f.hold(ctx, id)
		if err != nil {
			continue
		}
		record, ok, err := f.registry.get(ctx, id)
		if err == nil && ok && record.State == TenantWarm && !record.Active && record.Shard != "" {
			err = f.vault.freeze(ctx, record, f.node)
			if err == nil {
				f.counters.archives.Add(1)
				// This node's copy is of a checkpoint that is gone.
				err = os.RemoveAll(f.dir(id))
			}
		}
		release()
		if err != nil && !errors.Is(err, errNothingStored) && ctx.Err() == nil {
			fmt.Fprintf(f.log, "fleet: archive %s: %v\n", id, err)
			if errors.Is(err, errShardDown) {
				return
			}
		}
	}
}
