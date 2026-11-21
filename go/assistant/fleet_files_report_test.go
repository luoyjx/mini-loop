package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// LargeFileReport measures what one tenant with two large files costs with
// large files kept in the shard and with them kept in the archive store, for
// the same work: sizes in bytes, times in milliseconds.
type LargeFileReport struct {
	Engine string `json:"engine"`
	Store  string `json:"store"`
	// The work: a database of DatabaseBytes changed a few rows at a time,
	// Updates times, and an attachment of AttachmentBytes rewritten whole
	// Rewrites times.
	DatabaseBytes   int64             `json:"database_bytes"`
	AttachmentBytes int64             `json:"attachment_bytes"`
	Updates         int               `json:"database_updates"`
	Rewrites        int               `json:"attachment_rewrites"`
	Policies        []LargeFilePolicy `json:"policies"`
}

type LargeFilePolicy struct {
	Name string `json:"policy"`
	// What the shard holds for the tenant at the end.
	PageRows   int64 `json:"shard_page_rows"`
	PageBytes  int64 `json:"shard_page_bytes"`
	TableBytes int64 `json:"postgres_vault_pages_table_bytes,omitempty"`
	WALBytes   int64 `json:"postgres_wal_bytes,omitempty"`
	// What went to the shard and to the store during the work.
	WrittenPageBytes    int64   `json:"page_bytes_written"`
	ObjectsStored       int64   `json:"objects_stored"`
	ObjectBytesStored   int64   `json:"object_bytes_stored"`
	ObjectsHeld         int     `json:"objects_held_at_end"`
	ObjectBytesHeld     int64   `json:"object_bytes_held_at_end"`
	UpdateP50MS         float64 `json:"update_checkpoint_p50_ms"`
	UpdateP99MS         float64 `json:"update_checkpoint_p99_ms"`
	RewriteP50MS        float64 `json:"rewrite_checkpoint_p50_ms"`
	FirstCheckpointMS   float64 `json:"first_checkpoint_ms"`
	OpenOnAnotherNodeMS float64 `json:"open_on_another_node_ms"`
}

// TestLargeFileReport prints the report when MINILOOP_LARGE_FILE_REPORT is
// set, on PostgreSQL when MINILOOP_TEST_POSTGRES is set and on an S3 bucket
// when MINILOOP_TEST_S3_URL is. It asserts only that the files are read back
// whole: it measures.
func TestLargeFileReport(t *testing.T) {
	if os.Getenv("MINILOOP_LARGE_FILE_REPORT") == "" {
		t.Skip("set MINILOOP_LARGE_FILE_REPORT=1 to measure")
	}
	const (
		rows       = 2048 // of 2 KiB, each on a page of its own: about 8 MiB
		updates    = 24
		rewrites   = 4
		attachment = 2 << 20
	)
	ctx := context.Background()
	store := ArchiveStore{URL: "file://" + t.TempDir()}
	if address := os.Getenv("MINILOOP_TEST_S3_URL"); address != "" {
		store = ArchiveStore{URL: address, Region: os.Getenv("MINILOOP_TEST_S3_REGION"), AccessKeyID: os.Getenv("MINILOOP_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("MINILOOP_TEST_S3_SECRET_ACCESS_KEY")}
	}
	report := LargeFileReport{Updates: updates, Rewrites: rewrites, AttachmentBytes: attachment}
	quantile := func(samples []float64, q float64) float64 {
		if len(samples) == 0 {
			return 0
		}
		sort.Float64s(samples)
		return samples[min(len(samples)-1, int(q*float64(len(samples))))]
	}
	for _, large := range []int64{0, 1 << 20} {
		clock := newClock()
		database := sharedDatabase(t)
		tune := func(c *FleetConfig) {
			database(c)
			c.IdleTTL, c.Archive, c.LargeFile = time.Second, store, large
		}
		a := storedNode(t, clock, tune, nil)
		b := storedNode(t, clock, tune, nil)
		report.Engine, report.Store = a.registry.db.Engine(), a.vault.cold.String()
		id := TenantID(fmt.Sprintf("large-%d", large))
		createTenant(t, a.Fleet, id)
		touch(t, a.Fleet, id)
		walStart := int64(0)
		if a.registry.db.postgres {
			_ = a.registry.db.QueryRowContext(ctx, `SELECT CAST(pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0') AS BIGINT)`).Scan(&walStart)
		}
		notes := filepath.Join(a.dir(id), "notes.db")
		db, err := sql.Open("sqlite3", "file:"+notes+"?_journal_mode=WAL")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `CREATE TABLE notes(id INTEGER PRIMARY KEY, body BLOB)`); err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := range rows {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notes(id,body) VALUES(?,?)`, i, noise(2048, fmt.Sprint("row", i))); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			t.Fatal(err)
		}
		writeTenantFile(t, a, id, "files/attachment.bin", noise(attachment, "attachment 0"))
		before := stats(t, a.Fleet)
		start := time.Now()
		checkpoint(t, a.Fleet, id)
		policy := LargeFilePolicy{Name: "large files in the shard", FirstCheckpointMS: float64(time.Since(start).Microseconds()) / 1000}
		if large > 0 {
			policy.Name = fmt.Sprintf("files of %d KiB or more in the archive store", large>>10)
		}
		timed := func(samples *[]float64) {
			start := time.Now()
			if !checkpoint(t, a.Fleet, id) {
				t.Fatal("the change was not stored")
			}
			*samples = append(*samples, float64(time.Since(start).Microseconds())/1000)
		}
		var updated, rewritten []float64
		for round := range updates {
			for j := range 3 {
				if _, err := db.ExecContext(ctx, `UPDATE notes SET body=? WHERE id=?`, noise(2048, fmt.Sprint("update", round, j)), (round*97+j*31)%rows); err != nil {
					t.Fatal(err)
				}
			}
			timed(&updated)
			if round%(updates/rewrites) == updates/rewrites-1 {
				writeTenantFile(t, a, id, "files/attachment.bin", noise(attachment, fmt.Sprint("attachment ", round)))
				timed(&rewritten)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		checkpoint(t, a.Fleet, id)
		after := stats(t, a.Fleet)
		info, err := os.Stat(notes)
		if err != nil {
			t.Fatal(err)
		}
		report.DatabaseBytes = info.Size()
		policy.WrittenPageBytes = after.CheckpointBytes - before.CheckpointBytes
		policy.ObjectsStored, policy.ObjectBytesStored = after.ObjectsStored-before.ObjectsStored, after.ObjectBytes-before.ObjectBytes
		policy.UpdateP50MS, policy.UpdateP99MS, policy.RewriteP50MS = quantile(updated, 0.5), quantile(updated, 0.99), quantile(rewritten, 0.5)
		shard := shardOf(t, a.Fleet, id)
		s, _ := a.vault.shard(shard)
		sdb, err := s.use(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := sdb.QueryRowContext(ctx, `SELECT COUNT(*), CAST(COALESCE(SUM(LENGTH(data)),0) AS BIGINT) FROM vault_pages WHERE tenant_id=?`, string(id)).Scan(&policy.PageRows, &policy.PageBytes); err != nil {
			t.Fatal(err)
		}
		for _, file := range layoutIn(t, a.Fleet, shard, id) {
			if file.base != "" {
				policy.ObjectsHeld++
				policy.ObjectBytesHeld += file.baseSize
			}
		}
		if a.registry.db.postgres {
			_ = sdb.QueryRowContext(ctx, `SELECT pg_total_relation_size('vault_pages')`).Scan(&policy.TableBytes)
			var walEnd int64
			_ = a.registry.db.QueryRowContext(ctx, `SELECT CAST(pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0') AS BIGINT)`).Scan(&walEnd)
			policy.WALBytes = walEnd - walStart
		}
		want, err := os.ReadFile(filepath.Join(a.dir(id), "files", "attachment.bin"))
		if err != nil {
			t.Fatal(err)
		}
		sleep(t, clock, a.Fleet)
		start = time.Now()
		touch(t, b.Fleet, id)
		policy.OpenOnAnotherNodeMS = float64(time.Since(start).Microseconds()) / 1000
		if got := readTenantFile(t, b, id, "files/attachment.bin"); string(got) != string(want) {
			t.Fatal("the attachment is read back whole")
		}
		sleep(t, clock, b.Fleet)
		if err := b.DeleteTenant(ctx, id); err != nil {
			t.Fatal(err)
		}
		report.Policies = append(report.Policies, policy)
	}
	out, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(out))
}
