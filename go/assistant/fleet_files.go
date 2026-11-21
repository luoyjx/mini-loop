package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// Size decides where a file is kept, as age decides where a tenant is (see
// fleet_cold.go). A shard keeps a file as rows of 4 KiB pages, which is cheap
// to change a little and dear to keep a lot of: every page is a row with an
// index entry, written to the database's log, copied to its standbys and
// backups, and vacuumed. A file of LargeFile or more keeps its bulk as one
// object in the archive store instead, and its shard holds only the pages
// that differ from that object:
//
//   - A database that grows past LargeFile is written to the store once; its
//     checkpoints go on writing only the pages they change.
//   - Once the pages over its object, or the pages one checkpoint changes,
//     hold more than a quarter of the file, the file is written to the store
//     anew and its pages go. A file that is
//     rewritten whole, such as a note or an attachment, so goes to the store
//     as one object each time it changes and leaves no pages behind.
//   - A file that shrinks below half of LargeFile goes back to pages.
//
// An object is never written twice: each has a name of its own, so a node
// that was taken for dead cannot write over what another wrote since. Every
// object that may end up unused is written down in the registry (garbage)
// before the step that could leave it so, and goes once nothing refers to it.
// The janitor looks with the tenant held and after making itself the writer
// of the tenant's shard, so no checkpoint can come to refer to the object
// after it looked.
//
// While the store or the registry does not answer, a large file's changes go
// to its shard as pages. A checkpoint waits for one request to the store at
// most storeWait and a second for each MiB it sends, never more than half the
// time it has, so the shard is still written in the other half; once the
// store failed the node, its checkpoints do not ask it for storeCooldown. A
// node reading a tenant from its shard needs the store for the tenant's large
// files, as it needs it for an archived tenant.

// errInStore marks a failure of the archive store met while reading from a
// shard, which does not count against the shard (see dbTrouble).
var errInStore = errors.New("the archive store failed")

const (
	filePlace = "~file"
	// rebaseShare: a large file is written to the store anew once the pages
	// that differ from its object would hold more than 1/rebaseShare of it.
	rebaseShare = 4
	// storeWait and storeCooldown bound what the store costs a checkpoint
	// that writes to it (see storeContext and storeAway).
	storeWait     = 5 * time.Second
	storeCooldown = 30 * time.Second
)

// storeContext bounds one request to the store made by a checkpoint or the
// janitor: storeWait and a second for each MiB sent, and never more than half
// the time ctx has left.
func storeContext(ctx context.Context, size int64) (context.Context, context.CancelFunc) {
	limit := storeWait + time.Duration(size>>20)*time.Second
	if deadline, ok := ctx.Deadline(); ok {
		limit = min(limit, time.Until(deadline)/2)
	}
	return context.WithTimeout(ctx, limit)
}

// storeAway reports whether the store failed this node within storeCooldown;
// its checkpoints then keep large files' changes as pages without asking it.
func (v *vault) storeAway() bool {
	until := v.storeDown.Load()
	return until != 0 && v.now().UnixNano() < until
}

func (v *vault) storeFailed() {
	v.storeDown.Store(v.now().Add(storeCooldown).UnixNano())
}

// fileAt is the place of a large file's object, as the registry writes it
// down; fileToken reads it back.
func fileAt(token string) string { return filePlace + "/" + token }

func fileToken(place string) (string, bool) { return strings.CutPrefix(place, filePlace+"/") }

var tokenPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// fileKey names the object of one large file of a tenant. Like an archive's
// name it carries born, and a token that no other object has.
func fileKey(id TenantID, born int64, token string) string {
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%s/%s.%d.%s.file", hex.EncodeToString(sum[:1]), id, born, token)
}

// layouts reads how a shard keeps each of a tenant's files.
func layouts(ctx context.Context, q queryer, id TenantID) (map[string]storedFile, error) {
	files, _, err := storedFiles(ctx, q, id)
	if err != nil {
		return nil, err
	}
	found := make(map[string]storedFile, len(files))
	for _, file := range files {
		found[file.path] = file
	}
	return found, nil
}

// fileObjects lists the objects a shard's copy of a tenant refers to.
func fileObjects(ctx context.Context, q queryer, id TenantID, born int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT f.base FROM vault_files f JOIN vault_heads h ON h.tenant_id=f.tenant_id
		WHERE f.tenant_id=? AND f.base<>'' AND (h.born=? OR h.born=0) ORDER BY f.base`, string(id), born)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := []string{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// overlayPages hands the pages of a large file that differ from its object,
// in order. It fails with errCorrupt unless each lies inside the file and
// every one but the file's last page is full.
func overlayPages(ctx context.Context, q queryer, id TenantID, file storedFile, write func(idx int, data []byte) error) error {
	rows, err := q.QueryContext(ctx, `SELECT idx,data FROM vault_pages WHERE tenant_id=? AND path=? ORDER BY idx`, string(id), file.path)
	if err != nil {
		return err
	}
	defer rows.Close()
	last := (file.size - 1) / vaultPage
	for previous := -1; rows.Next(); {
		var idx int
		var data []byte
		if err := rows.Scan(&idx, &data); err != nil {
			return err
		}
		at := int64(idx)
		want := int64(vaultPage)
		if at == last {
			want = file.size - last*vaultPage
		}
		if file.size == 0 || idx <= previous || at > last || int64(len(data)) != want {
			return fmt.Errorf("%w: %s page %d", errCorrupt, file.path, idx)
		}
		if err := write(idx, data); err != nil {
			return err
		}
		previous = idx
	}
	return rows.Err()
}

// writeFile writes one stored file to out: its pages, or for a large file
// its object with its pages over it. It fails with errCorrupt unless what it
// wrote is the file that was stored.
func (v *vault) writeFile(ctx context.Context, q queryer, id TenantID, born int64, file storedFile, out *os.File) error {
	if file.base == "" {
		return storedPages(ctx, q, id, file, func(_ int, data []byte) error {
			_, err := out.Write(data)
			return err
		})
	}
	if v.cold == nil {
		return fmt.Errorf("%s keeps its bulk in the archive store, which this node does not have", file.path)
	}
	digest := sha256.New()
	counted := &countWriter{}
	err := v.cold.get(ctx, fileKey(id, born, file.base), io.MultiWriter(out, digest, counted))
	if errors.Is(err, errNoArchive) {
		return fmt.Errorf("%w: the object of %s is missing", errCorrupt, file.path)
	}
	if err != nil {
		return fmt.Errorf("read the object of %s: %w", file.path, errors.Join(errInStore, err))
	}
	if counted.n != file.baseSize || hex.EncodeToString(digest.Sum(nil)) != file.baseSum {
		return fmt.Errorf("%w: the object of %s is not the one that was stored", errCorrupt, file.path)
	}
	v.counters.objectGets.Add(1)
	err = overlayPages(ctx, q, id, file, func(idx int, data []byte) error {
		_, err := out.WriteAt(data, int64(idx)*vaultPage)
		return err
	})
	if err != nil {
		return err
	}
	if err := out.Truncate(file.size); err != nil {
		return err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return err
	}
	whole := sha256.New()
	if n, err := io.Copy(whole, out); err != nil {
		return err
	} else if n != file.size || hex.EncodeToString(whole.Sum(nil)) != file.sum {
		return fmt.Errorf("%w: %s does not match its checksum", errCorrupt, file.path)
	}
	return nil
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// ---- on the way to the shard ----

// stage decides, for each file a checkpoint changes, whether it goes as
// pages, as a new object, or back to pages from an object, and writes the
// new objects to the store. A file whose object cannot be written down or
// stored goes as pages: the checkpoint does not wait for the store. A file
// that is gone and had an object cannot go without the registry, so then the
// checkpoint fails and the next one tries again.
func (r *replica) stage(ctx context.Context, delta *vaultDelta) error {
	v := r.vault
	large := v.largeFile > 0 && v.cold != nil
	for i := range delta.files {
		file := &delta.files[i]
		before := r.layout[file.path]
		switch {
		case !file.moved:
			// The file itself, which the tenant may be writing: only its
			// pages, read with its checksum, are what was compared. It is
			// copied to an image of its own from the next checkpoint on.
		case large && file.size >= v.largeFile && (before.base == "" || max(before.overlay, int64(len(file.pages)))*vaultPage > file.size/rebaseShare):
			// before.overlay is what the last checkpoint counted in the
			// shard; adding this one's pages to it would count again those
			// it changes once more.
			if v.storeAway() {
				v.counters.fallbacks.Add(1)
				continue
			}
			token := randomHex(8)
			if err := r.store(ctx, file, token, before.base); err != nil {
				v.counters.fallbacks.Add(1)
				continue
			}
			file.object, file.pages = token, nil
			delta.added = append(delta.added, token)
			if before.base != "" {
				delta.dropped = append(delta.dropped, before.base)
			}
		case before.base != "" && (!large || file.size < v.largeFile/2):
			if err := v.registry.intend(ctx, leftover{r.id, r.born, fileAt(before.base)}, v.now()); err != nil {
				v.counters.fallbacks.Add(1)
				continue
			}
			pages, err := allPages(file.image)
			if err != nil {
				return err
			}
			file.pages, file.whole = pages, true
			delta.dropped = append(delta.dropped, before.base)
		}
	}
	for _, rel := range delta.gone {
		if base := r.layout[rel].base; base != "" {
			if err := v.registry.intend(ctx, leftover{r.id, r.born, fileAt(base)}, v.now()); err != nil {
				return fmt.Errorf("write down the object of %s, which is gone: %w", rel, err)
			}
			delta.dropped = append(delta.dropped, base)
		}
	}
	return nil
}

// tiered reports whether a file of this size may be kept as an object, or
// is: its image is then a copy of its own (see pin). From half the size on,
// so a file that grows past it between two looks is copied all the same.
func (r *replica) tiered(rel string, size int64) bool {
	v := r.vault
	return (v.largeFile > 0 && v.cold != nil && size >= v.largeFile/2) || r.layout[rel].base != ""
}

// pin copies a plain file to an image of its own, which the checkpoint reads
// instead of the file: what goes to the store is then what the image's size
// and checksum say, even while the tenant writes the file. A database's image
// is a snapshot already.
func (r *replica) pin(rel, path string) (string, error) {
	source, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	image := r.next(rel)
	return image, replaceFile(image, source)
}

// allPages reads a file as pages.
func allPages(path string) ([]vaultPageData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pages := make([]vaultPageData, 0, (len(data)+vaultPage-1)/vaultPage)
	for idx := 0; idx*vaultPage < len(data); idx++ {
		pages = append(pages, vaultPageData{idx: idx, data: data[idx*vaultPage : min(len(data), (idx+1)*vaultPage)]})
	}
	return pages, nil
}

// store writes a file's image to the archive store as the object token. The
// object, and the one it replaces, are written down first: the new one goes
// unless the checkpoint that refers to it is written, the old one once it is.
func (r *replica) store(ctx context.Context, file *vaultFile, token, replaced string) error {
	v := r.vault
	for _, t := range []string{token, replaced} {
		if t == "" {
			continue
		}
		if err := v.registry.intend(ctx, leftover{r.id, r.born, fileAt(t)}, v.now()); err != nil {
			return err
		}
	}
	image, err := os.Open(file.image)
	if err != nil {
		return err
	}
	defer image.Close()
	sum, err := hex.DecodeString(file.sum)
	if err != nil || len(sum) != sha256.Size {
		return fmt.Errorf("%s has no checksum", file.path)
	}
	v.at("upload")
	put, cancel := storeContext(ctx, file.size)
	err = v.cold.put(put, fileKey(r.id, r.born, token), image, file.size, [sha256.Size]byte(sum))
	cancel()
	if err != nil {
		v.storeFailed()
		return err
	}
	v.counters.objectPuts.Add(1)
	v.counters.objectBytes.Add(file.size)
	v.at("put")
	return nil
}

// unstage writes down again the objects a checkpoint that failed stored:
// a janitor that settled one before it was there may have let its row go.
func (r *replica) unstage(ctx context.Context, delta *vaultDelta) {
	for _, token := range delta.added {
		_ = r.vault.registry.intend(ctx, leftover{r.id, r.born, fileAt(token)}, r.vault.now())
	}
}

// settle removes the objects a written checkpoint no longer refers to and
// the rows of the ones it now does. What fails, or waits while the store is
// away, stays written down and the janitor finishes it.
func (r *replica) settle(ctx context.Context, delta *vaultDelta) {
	v := r.vault
	for _, token := range delta.dropped {
		if v.storeAway() {
			break
		}
		_ = v.dropFile(ctx, leftover{r.id, r.born, fileAt(token)})
	}
	for _, token := range delta.added {
		_ = v.registry.cleaned(ctx, leftover{r.id, r.born, fileAt(token)})
	}
}

// ---- objects that may be unused ----

// take makes node the one writer of a tenant's data in a shard and returns
// the checkpoint the shard holds (see take). The objects the data it removes
// refers to are written down in the registry between its transactions.
func (v *vault) take(ctx context.Context, s *shard, id TenantID, node string, born int64, reset bool) (head held, err error) {
	noted := map[string]bool{}
	for {
		var tokens []string
		var owner int64
		err = s.do(ctx, func(db *fleetDB) (err error) {
			head, tokens, owner, err = take(ctx, db, id, node, born, reset, v.now(), noted)
			return err
		})
		if err != nil || len(tokens) == 0 {
			return head, err
		}
		if err := v.note(ctx, id, owner, tokens); err != nil {
			return held{}, err
		}
		for _, token := range tokens {
			noted[token] = true
		}
	}
}

// note writes down the objects a copy of a tenant refers to before the rows
// that refer to them go.
func (v *vault) note(ctx context.Context, id TenantID, born int64, tokens []string) error {
	for _, token := range tokens {
		if err := v.registry.intend(ctx, leftover{id, born, fileAt(token)}, v.now()); err != nil {
			return err
		}
	}
	return nil
}

// referenced lists the objects a shard's copy of a tenant refers to.
func referenced(ctx context.Context, s *shard, id TenantID, born int64) (map[string]bool, error) {
	var tokens []string
	err := s.do(ctx, func(db *fleetDB) (err error) {
		tokens, err = fileObjects(ctx, db, id, born)
		return err
	})
	found := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		found[token] = true
	}
	return found, err
}

// dropFile removes an object nothing refers to, and its row.
func (v *vault) dropFile(ctx context.Context, l leftover) error {
	token, _ := fileToken(l.place)
	if v.cold == nil {
		return errors.New("this node has no archive store")
	}
	remove, cancel := storeContext(ctx, 0)
	err := v.cold.remove(remove, fileKey(l.id, l.born, token))
	cancel()
	if err != nil {
		v.storeFailed()
		return err
	}
	v.counters.objectRemoves.Add(1)
	return v.registry.cleaned(ctx, l)
}

// letGo settles the objects a removed copy of a tenant referred to. The
// caller holds the tenant, or the tenant is gone. An object goes unless the
// tenant's data, where the registry says it is, refers to it. Nothing can
// come to refer to it later: a checkpoint refers only to objects it wrote
// itself, and a move copies the tenant's data from where the registry says
// it is. What cannot be decided now stays written down for the janitor.
func (v *vault) letGo(ctx context.Context, l leftover, tokens []string) {
	if len(tokens) == 0 {
		return
	}
	record, ok, err := v.registry.get(ctx, l.id)
	if err != nil {
		return
	}
	refs := map[string]bool{}
	if ok && millis(record.CreatedAt) == l.born && record.State != TenantCold && record.Shard != "" {
		if record.Shard == l.place {
			return
		}
		s, err := v.shard(record.Shard)
		if err != nil {
			return
		}
		if refs, err = referenced(ctx, s, l.id, l.born); err != nil {
			return
		}
	}
	for _, token := range tokens {
		row := leftover{l.id, l.born, fileAt(token)}
		if refs[token] {
			_ = v.registry.cleaned(ctx, row)
		} else {
			_ = v.dropFile(ctx, row)
		}
	}
}

// cleanFile settles one object that may be unused: it goes unless the
// tenant's data refers to it. A node that was taken for dead may be about to
// write a checkpoint that refers to it, so this node holds the tenant and
// makes itself the writer of the tenant's shard before it looks; that node's
// checkpoint is refused after.
func (f *Fleet) cleanFile(ctx context.Context, l leftover, token string) error {
	gone := func(record TenantRecord, ok bool) bool { return !ok || millis(record.CreatedAt) != l.born }
	record, ok, err := f.registry.get(ctx, l.id)
	if err != nil {
		return err
	}
	if gone(record, ok) {
		return f.vault.dropFile(ctx, l)
	}
	release, err := f.hold(ctx, l.id)
	if errors.Is(err, ErrTenantUnknown) {
		return f.vault.dropFile(ctx, l)
	}
	if err != nil {
		return err
	}
	defer release()
	if record, ok, err = f.registry.get(ctx, l.id); err != nil {
		return err
	}
	if gone(record, ok) || record.State == TenantCold || record.Shard == "" {
		return f.vault.dropFile(ctx, l)
	}
	s, err := f.vault.shard(record.Shard)
	if err != nil {
		return err
	}
	f.vault.at("seize")
	if err := f.vault.seize(ctx, leftover{l.id, l.born, record.Shard}, f.node); err != nil {
		return err
	}
	refs, err := referenced(ctx, s, l.id, l.born)
	if err != nil {
		return err
	}
	if refs[token] {
		return f.registry.cleaned(ctx, l)
	}
	f.vault.at("clean")
	return f.vault.dropFile(ctx, l)
}

// objectSize and objectSum are what a checkpoint writes as a file's object:
// the file itself when it has a new one, nothing when it left its object.
func objectSize(file *vaultFile) int64 {
	if file.object == "" {
		return 0
	}
	return file.size
}

func objectSum(file *vaultFile) string {
	if file.object == "" {
		return ""
	}
	return file.sum
}
