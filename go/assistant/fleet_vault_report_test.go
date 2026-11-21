package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"
)

// VaultReport measures what keeping one tenant in the vault costs under three
// checkpoint policies, for the same work: sizes in bytes, times in
// milliseconds.
type VaultReport struct {
	Engine string `json:"engine"`
	Turns  int    `json:"turns"`
	// A tenant that was opened once and has done nothing.
	FreshBytes int64 `json:"fresh_tenant_bytes"`
	FreshFiles int   `json:"fresh_tenant_files"`
	FreshPages int64 `json:"fresh_tenant_pages"`
	// The same tenant after the turns.
	FinalBytes   int64 `json:"final_tenant_bytes"`
	GrowthPerRun int64 `json:"tenant_growth_per_turn_bytes"`
	Policies     []struct {
		Name        string  `json:"policy"`
		Checkpoints int64   `json:"checkpoints"`
		Pages       int64   `json:"pages_written"`
		Bytes       int64   `json:"bytes_written"`
		PerTurn     int64   `json:"bytes_written_per_turn"`
		P50MS       float64 `json:"checkpoint_p50_ms"`
		P99MS       float64 `json:"checkpoint_p99_ms"`
	} `json:"policies"`
	PullMS  float64 `json:"open_from_vault_ms"`
	ReuseMS float64 `json:"open_from_cache_ms"`
	// PostgreSQL only: what the vault's tables take on disk for this tenant
	// set, and the write-ahead log the whole run produced.
	TableBytes int64 `json:"postgres_vault_table_bytes,omitempty"`
	WALBytes   int64 `json:"postgres_wal_bytes,omitempty"`
}

// TestVaultReport prints the report when MINILOOP_VAULT_REPORT is set. It
// asserts nothing: it measures.
func TestVaultReport(t *testing.T) {
	if os.Getenv("MINILOOP_VAULT_REPORT") == "" {
		t.Skip("set MINILOOP_VAULT_REPORT=1 to measure")
	}
	const turns = 12
	ctx := context.Background()
	clock := newClock()
	database := sharedDatabase(t)
	fleet := openFleet(t, clock, func(c *FleetConfig) { database(c); c.IdleTTL = time.Second })
	handler := fleet.Handler("", "")
	report := VaultReport{Engine: fleet.registry.db.Engine(), Turns: turns}
	walStart := int64(0)
	if fleet.registry.db.postgres {
		_ = fleet.registry.db.QueryRowContext(ctx, `SELECT CAST(pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0') AS BIGINT)`).Scan(&walStart)
	}
	size := func(id TenantID) (bytes int64, files int, pages int64) {
		_ = fleet.registry.db.QueryRowContext(ctx, `SELECT CAST(COALESCE(SUM(size),0) AS BIGINT), COUNT(*) FROM vault_files WHERE tenant_id=?`, string(id)).Scan(&bytes, &files)
		_ = fleet.registry.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vault_pages WHERE tenant_id=?`, string(id)).Scan(&pages)
		return
	}
	work := func(id TenantID, token string, turn int, each func()) {
		app := touch(t, fleet, id)
		switch turn % 3 {
		case 0:
			deliver(t, app, []Preset{PresetManager, PresetFriend, PresetManagerFollow}[(turn/3)%3])
		default:
			text := []string{"今天日程", "收件箱", "记住 周三下午不开会", "现在有哪些任务"}[turn%4]
			if code, body := tenantCall(handler, http.MethodPost, "/t/"+string(id)+"/api/chat", token, fmt.Sprintf(`{"text":%q}`, text)); code != http.StatusAccepted {
				t.Fatalf("chat: %d %s", code, body)
			}
		}
		deadline := time.Now().Add(20 * time.Second)
		for {
			busy := false
			for _, trigger := range mustTriggers(t, app.Store) {
				busy = busy || trigger.Status == TriggerPending || trigger.Status == TriggerRunning
			}
			if each != nil {
				each()
			}
			if !busy {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the turn did not settle")
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	quantile := func(samples []float64, q float64) float64 {
		if len(samples) == 0 {
			return 0
		}
		sort.Float64s(samples)
		return samples[min(len(samples)-1, int(q*float64(len(samples))))]
	}
	for i, policy := range []string{"at an owner's write and at sleep", "and after every turn", "and during the turn, every few ms"} {
		id := TenantID(fmt.Sprintf("tenant-%d", i))
		token := createTenant(t, fleet, id)
		touch(t, fleet, id)
		for checkpoint(t, fleet, id) {
			time.Sleep(20 * time.Millisecond)
		}
		if i == 0 {
			report.FreshBytes, report.FreshFiles, report.FreshPages = size(id)
		}
		before := stats(t, fleet)
		samples := []float64{}
		timed := func() {
			start := time.Now()
			if checkpoint(t, fleet, id) {
				samples = append(samples, float64(time.Since(start).Microseconds())/1000)
			}
		}
		for turn := 0; turn < turns; turn++ {
			switch i {
			case 0:
				work(id, token, turn, nil)
			case 1:
				work(id, token, turn, nil)
				timed()
			default:
				work(id, token, turn, timed)
			}
		}
		timed()
		after := stats(t, fleet)
		entry := struct {
			Name        string  `json:"policy"`
			Checkpoints int64   `json:"checkpoints"`
			Pages       int64   `json:"pages_written"`
			Bytes       int64   `json:"bytes_written"`
			PerTurn     int64   `json:"bytes_written_per_turn"`
			P50MS       float64 `json:"checkpoint_p50_ms"`
			P99MS       float64 `json:"checkpoint_p99_ms"`
		}{policy, after.Checkpoints - before.Checkpoints, after.CheckpointPages - before.CheckpointPages, after.CheckpointBytes - before.CheckpointBytes, 0, quantile(samples, 0.5), quantile(samples, 0.99)}
		entry.PerTurn = entry.Bytes / turns
		report.Policies = append(report.Policies, entry)
		if i == 0 {
			report.FinalBytes, _, _ = size(id)
			report.GrowthPerRun = (report.FinalBytes - report.FreshBytes) / turns
		}
	}
	// Opening a sleeping tenant from this node's copy, and from the vault.
	clock.Set(clock.Now().Add(2 * time.Second))
	for fleet.held() > 0 {
		fleet.reap(ctx)
		time.Sleep(20 * time.Millisecond)
	}
	start := time.Now()
	touch(t, fleet, "tenant-0")
	report.ReuseMS = float64(time.Since(start).Microseconds()) / 1000
	if err := os.RemoveAll(fleet.dir("tenant-1")); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	touch(t, fleet, "tenant-1")
	report.PullMS = float64(time.Since(start).Microseconds()) / 1000
	if fleet.registry.db.postgres {
		_ = fleet.registry.db.QueryRowContext(ctx, `SELECT CAST(pg_total_relation_size('vault_pages')+pg_total_relation_size('vault_files')+pg_total_relation_size('vault_heads') AS BIGINT)`).Scan(&report.TableBytes)
		var walEnd int64
		_ = fleet.registry.db.QueryRowContext(ctx, `SELECT CAST(pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0') AS BIGINT)`).Scan(&walEnd)
		report.WALBytes = walEnd - walStart
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("\n%s", encoded)
}
