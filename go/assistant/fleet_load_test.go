package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// FleetLoadReport compares keeping every assistant resident with the fleet.
// Sizes are per tenant unless named otherwise.
type FleetLoadReport struct {
	Resident struct {
		Tenants         int     `json:"tenants"`
		OpenMS          float64 `json:"open_ms"`
		Goroutines      float64 `json:"goroutines"`
		HeapKB          int64   `json:"heap_kb"`
		RSSKB           int64   `json:"rss_kb"`
		DiskOpenKB      int64   `json:"disk_open_kb"`
		IdleCPUPerSecMS float64 `json:"idle_cpu_ms_per_s_per_tenant"`
	} `json:"resident"`
	Fleet struct {
		Tenants        int     `json:"tenants"`
		MaxActive      int     `json:"max_active"`
		Clients        int     `json:"clients"`
		Requests       int64   `json:"requests"`
		Rejected       int64   `json:"rejected_503"`
		RequestsPerSec float64 `json:"requests_per_s"`
		ColdP50MS      float64 `json:"cold_open_p50_ms"`
		ColdP99MS      float64 `json:"cold_open_p99_ms"`
		WarmP50MS      float64 `json:"warm_get_p50_ms"`
		WarmP99MS      float64 `json:"warm_get_p99_ms"`
		ChatP50MS      float64 `json:"chat_done_p50_ms"`
		ChatP99MS      float64 `json:"chat_done_p99_ms"`
		PeakActive     int     `json:"peak_active"`
		PeakTurns      int     `json:"peak_turns_in_use"`
		ModelCalls     int64   `json:"model_calls"`
		Evictions      int64   `json:"evictions"`
		// After every owner left and the reaper ran.
		ActiveAfter        int     `json:"active_after"`
		GoroutinesAfter    int     `json:"goroutines_over_start_after"`
		RSSAfterKB         int64   `json:"rss_over_start_after_kb"`
		IdleCPUPerSecMS    float64 `json:"idle_cpu_ms_per_s_all_tenants"`
		DiskSleepingKB     int64   `json:"disk_sleeping_kb"`
		DiskArchivedKB     int64   `json:"disk_archived_kb"`
		RestoreOpenP50MS   float64 `json:"restore_open_p50_ms"`
		RegistryKB         int64   `json:"registry_kb"`
		UntouchedDiskBytes int64   `json:"untouched_tenant_disk_bytes"`
	} `json:"fleet"`
}

// TestFleetLoad measures the fleet against resident assistants. It is off by
// default; MINILOOP_FLEET_LOAD=<tenants> runs it and MINILOOP_FLEET_LOAD_OUT
// writes the JSON report. Run it alone, with the repository's resource
// limits.
func TestFleetLoad(t *testing.T) {
	tenants, _ := strconv.Atoi(os.Getenv("MINILOOP_FLEET_LOAD"))
	if tenants <= 0 {
		t.Skip("set MINILOOP_FLEET_LOAD=<tenants> to run the fleet load measurement")
	}
	var report FleetLoadReport
	measureResident(t, &report, 50)
	measureFleet(t, &report, tenants)
	out, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("%s", out)
	if path := os.Getenv("MINILOOP_FLEET_LOAD_OUT"); path != "" {
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func processRSS(t *testing.T) int64 {
	t.Helper()
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Fatal(err)
	}
	kb, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return kb
}

func cpuTime() time.Duration {
	var usage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
}

func idleCPU(window time.Duration) float64 {
	before := cpuTime()
	time.Sleep(window)
	return float64(cpuTime()-before) / float64(time.Millisecond) / window.Seconds()
}

func heapInUse() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapInuse
}

func loadPercentile(samples []time.Duration, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	index := int(float64(len(samples)-1) * p)
	return float64(samples[index]) / float64(time.Millisecond)
}

// measureResident opens n assistants the way n daemons would run them.
func measureResident(t *testing.T, report *FleetLoadReport, n int) {
	root := t.TempDir()
	ctx := context.Background()
	heap, goroutines, rss := heapInUse(), runtime.NumGoroutine(), processRSS(t)
	type resident struct {
		app    *App
		close  func() error
		cancel context.CancelFunc
	}
	residents := []resident{}
	started := time.Now()
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, strconv.Itoa(i))
		connector, extra, err := MockTenantConnect(ctx, "", dir, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		app, err := NewApp(ctx, Config{DataDir: dir, Connector: connector, Provider: &ScriptedModel{}, Model: ScriptedModelID})
		if err != nil {
			t.Fatal(err)
		}
		run, cancel := context.WithCancel(ctx)
		app.Start(run)
		residents = append(residents, resident{app, extra, cancel})
	}
	r := &report.Resident
	r.Tenants = n
	r.OpenMS = float64(time.Since(started)) / float64(time.Millisecond) / float64(n)
	time.Sleep(3 * time.Second)
	r.Goroutines = float64(runtime.NumGoroutine()-goroutines) / float64(n)
	r.HeapKB = int64(heapInUse()-heap) / int64(n) / 1024
	r.RSSKB = (processRSS(t) - rss) / int64(n)
	r.DiskOpenKB = dirSize(t, root) / int64(n) / 1024
	r.IdleCPUPerSecMS = idleCPU(10*time.Second) / float64(n)
	for _, resident := range residents {
		resident.cancel()
		if err := resident.app.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if err := resident.close(); err != nil {
			t.Fatal(err)
		}
	}
}

func dirSize(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// measureFleet registers n tenants and lets each owner open the page, chat
// once and wait for the reply, with a bounded number of concurrent owners.
func measureFleet(t *testing.T, report *FleetLoadReport, n int) {
	const clients, maxActive = 32, 64
	ctx := context.Background()
	root := t.TempDir()
	goroutines, rss := runtime.NumGoroutine(), processRSS(t)
	fleet, err := OpenFleet(ctx, FleetConfig{
		DataDir: root, Provider: &ScriptedModel{}, Model: ScriptedModelID,
		MaxActive: maxActive, IdleTTL: 500 * time.Millisecond, Turns: 8,
		WakeTick: 250 * time.Millisecond, ReapTick: 250 * time.Millisecond, JanitorTick: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fleet.Close()
	fleet.Start()
	tokens := make([]string, n)
	ids := make([]TenantID, n)
	for i := range n {
		ids[i] = TenantID(fmt.Sprintf("tenant-%05d", i))
		if _, tokens[i], err = fleet.CreateTenant(ctx, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	f := &report.Fleet
	f.Tenants, f.MaxActive, f.Clients = n, maxActive, clients
	f.UntouchedDiskBytes = dirSize(t, filepath.Join(root, "tenants"))
	handler := fleet.Handler("")
	var requests, rejected atomic.Int64
	call := func(method, path, token, payload string) (int, string) {
		requests.Add(1)
		code, body := tenantCall(handler, method, path, token, payload)
		if code == http.StatusServiceUnavailable {
			rejected.Add(1)
		}
		return code, body
	}
	var mu sync.Mutex
	var cold, warm, chat []time.Duration
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
			stats, err := fleet.Stats(ctx)
			if err == nil {
				f.PeakActive = max(f.PeakActive, stats.Active)
				f.PeakTurns = max(f.PeakTurns, stats.TurnsInUse)
			}
		}
	}()
	work := make(chan int)
	var wg sync.WaitGroup
	started := time.Now()
	for range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				base := "/t/" + string(ids[i]) + "/api/"
				for {
					begin := time.Now()
					code, _ := call(http.MethodGet, base+"state", tokens[i], "")
					if code == http.StatusOK {
						mu.Lock()
						cold = append(cold, time.Since(begin))
						mu.Unlock()
						break
					}
					if code != http.StatusServiceUnavailable {
						t.Errorf("%s: state %d", ids[i], code)
						return
					}
					time.Sleep(100 * time.Millisecond)
				}
				begin := time.Now()
				if code, body := call(http.MethodPost, base+"chat", tokens[i], `{"text":"今天日程"}`); code >= 300 {
					t.Errorf("%s: chat %d %s", ids[i], code, body)
					return
				}
				for {
					polled := time.Now()
					code, body := call(http.MethodGet, base+"state", tokens[i], "")
					if code == http.StatusOK {
						mu.Lock()
						warm = append(warm, time.Since(polled))
						mu.Unlock()
						if strings.Contains(body, "今明两天") {
							break
						}
					}
					time.Sleep(50 * time.Millisecond)
				}
				mu.Lock()
				chat = append(chat, time.Since(begin))
				mu.Unlock()
			}
		}()
	}
	for i := range n {
		work <- i
	}
	close(work)
	wg.Wait()
	elapsed := time.Since(started)
	close(stop)
	<-sampled
	f.Requests, f.Rejected = requests.Load(), rejected.Load()
	f.RequestsPerSec = float64(f.Requests) / elapsed.Seconds()
	f.ColdP50MS, f.ColdP99MS = loadPercentile(cold, 0.5), loadPercentile(cold, 0.99)
	f.WarmP50MS, f.WarmP99MS = loadPercentile(warm, 0.5), loadPercentile(warm, 0.99)
	f.ChatP50MS, f.ChatP99MS = loadPercentile(chat, 0.5), loadPercentile(chat, 0.99)

	deadline := time.Now().Add(2 * time.Minute)
	for {
		stats, err := fleet.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("assistants did not go to sleep: %+v", stats)
		}
		time.Sleep(100 * time.Millisecond)
	}
	stats, _ := fleet.Stats(ctx)
	f.ModelCalls, f.Evictions, f.ActiveAfter = stats.ModelCalls, stats.Evictions, stats.Active
	runtime.GC()
	time.Sleep(time.Second)
	f.GoroutinesAfter = runtime.NumGoroutine() - goroutines
	f.RSSAfterKB = processRSS(t) - rss
	f.IdleCPUPerSecMS = idleCPU(10 * time.Second)
	f.DiskSleepingKB = dirSize(t, filepath.Join(root, "tenants")) / int64(n) / 1024
	if info, err := os.Stat(filepath.Join(root, "fleet.db")); err == nil {
		f.RegistryKB = info.Size() / 1024
	}
	fleet.archiveAbsent(ctx, time.Now().Add(time.Hour))
	for archived := int64(32); archived < int64(n); archived += 32 {
		fleet.archiveAbsent(ctx, time.Now().Add(time.Hour))
	}
	f.DiskArchivedKB = dirSize(t, filepath.Join(root, "tenants")) / int64(n) / 1024
	restored := []time.Duration{}
	for i := 0; i < min(n, 50); i++ {
		begin := time.Now()
		if code, _ := call(http.MethodGet, "/t/"+string(ids[i])+"/api/state", tokens[i], ""); code != http.StatusOK {
			t.Fatalf("restore %s: %d", ids[i], code)
		}
		restored = append(restored, time.Since(begin))
	}
	f.RestoreOpenP50MS = loadPercentile(restored, 0.5)
}
