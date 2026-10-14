package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildFingerprintContentCacheAndFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native-artifact")
	content := bytes.Repeat([]byte("native artifact fixture\n"), 9000)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	load := cachedBuildID(func() (string, error) { calls.Add(1); return path, nil })
	var wait sync.WaitGroup
	var fingerprints [16]Fingerprint
	for index := range fingerprints {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			var err error
			fingerprints[index], err = load()
			if err != nil {
				t.Error(err)
			}
		}(index)
	}
	wait.Wait()
	digest := sha256.Sum256(content)
	want := Fingerprint(hex.EncodeToString(digest[:])[:12])
	for _, value := range fingerprints {
		if value != want {
			t.Fatal("fingerprint does not cover complete native artifact", value, want)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent callers recomputed identity", calls.Load())
	}
	copyPath := filepath.Join(t.TempDir(), "relocated-native-artifact")
	if err := os.WriteFile(copyPath, content, 0700); err != nil {
		t.Fatal(err)
	}
	if copyID, err := fingerprintFile(copyPath); err != nil || copyID != want {
		t.Fatal("identical bytes changed fingerprint after relocation", copyID, err)
	}
	if err := os.WriteFile(path, append(content, 'x'), 0600); err != nil {
		t.Fatal(err)
	}
	if cached, err := load(); err != nil || cached != want {
		t.Fatal("running identity changed after on-disk edit", cached, err)
	}
	if changed, err := fingerprintFile(path); err != nil || changed == want {
		t.Fatal("uncached artifact identity failed to distinguish changed content", changed, err)
	}
	for _, mode := range []string{"resolve-error", "missing-file", "read-directory"} {
		t.Run(mode, func(t *testing.T) {
			var attempts atomic.Int64
			missing := filepath.Join(t.TempDir(), "missing")
			var repaired bool
			if mode == "read-directory" {
				if err := os.Mkdir(missing, 0700); err != nil {
					t.Fatal(err)
				}
			}
			failed := cachedBuildID(func() (string, error) {
				attempts.Add(1)
				if mode == "resolve-error" && !repaired {
					return "", errors.New("private executable path")
				}
				return missing, nil
			})
			id, err := failed()
			if id != "" || err == nil || err.Error() != "native executable fingerprint unavailable" {
				t.Fatal("failure produced a usable identity or leaked its path", id, err)
			}
			if mode == "read-directory" {
				if err := os.Remove(missing); err != nil {
					t.Fatal(err)
				}
			}
			repaired = true
			if err := os.WriteFile(missing, []byte("repaired"), 0600); err != nil {
				t.Fatal(err)
			}
			recovered, err := failed()
			repairedDigest := sha256.Sum256([]byte("repaired"))
			if err != nil || recovered != Fingerprint(hex.EncodeToString(repairedDigest[:])[:12]) {
				t.Fatal("cache retained an exception instead of retrying repaired input", recovered, err)
			}
			if err := os.WriteFile(missing, []byte("changed-after-success"), 0600); err != nil {
				t.Fatal(err)
			}
			if cached, err := failed(); err != nil || cached != recovered || attempts.Load() != 2 {
				t.Fatal("cache did not retain the successful recovery", cached, recovered, err, attempts.Load())
			}
		})
	}
}

func TestProcessSnapshotMatchesActualPythonWallClock(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-public-identity-audit.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		BuildFailure struct {
			ExceptionClass         string `json:"exception_class"`
			FailureCacheSize       int    `json:"failure_cache_size"`
			RecoveredAfterRepair   bool   `json:"recovered_after_repair"`
			SuccessCachedAfterEdit bool   `json:"success_cached_after_edit"`
			RecoveryCacheMisses    int    `json:"recovery_cache_misses"`
			RecoveryCacheHits      int    `json:"recovery_cache_hits"`
			RecoveryCacheSize      int    `json:"recovery_cache_size"`
		} `json:"build_failure"`
		WallClock []struct {
			Name               string
			StartedAt          float64 `json:"started_at"`
			Now                float64
			ObservedStartedAt  float64 `json:"observed_started_at"`
			Uptime             float64 `json:"uptime_s"`
			UptimeNegativeZero bool    `json:"uptime_negative_zero"`
			BuildMatchesPublic bool    `json:"build_matches_public"`
			PIDMatchesProcess  bool    `json:"pid_matches_process"`
		} `json:"wall_clock"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	failure := fixture.BuildFailure
	if failure.ExceptionClass != "IsADirectoryError" || failure.FailureCacheSize != 0 || !failure.RecoveredAfterRepair || !failure.SuccessCachedAfterEdit || failure.RecoveryCacheMisses != 2 || failure.RecoveryCacheHits != 1 || failure.RecoveryCacheSize != 1 || len(fixture.WallClock) != 8 {
		t.Fatal("actual Source recovery/wall-clock corpus changed", failure)
	}
	for _, row := range fixture.WallClock {
		t.Run(row.Name, func(t *testing.T) {
			if !row.BuildMatchesPublic || !row.PIDMatchesProcess {
				t.Fatal("clock injection changed unrelated identity fields", row)
			}
			got := processSnapshotAt(1234, row.StartedAt, row.Now)
			negativeZero := got.Uptime == 0 && math.Signbit(got.Uptime)
			if got.PID != 1234 || got.StartedAt != row.ObservedStartedAt || got.Uptime != row.Uptime || negativeZero != row.UptimeNegativeZero {
				t.Fatal("native wall-clock snapshot differs from actual Source", got, row)
			}
		})
	}
}

func TestRuntimeIdentityMatchesActualPythonRelationships(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-public-identity-audit.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PublicAliases map[string]bool `json:"public_aliases"`
		Build         struct {
			LowercaseHex                 bool `json:"lowercase_hex"`
			Length                       int
			MatchesPackageContent        bool `json:"matches_package_content"`
			StableWithinProcess          bool `json:"stable_within_process"`
			CorpusCachedAfterEdit        bool `json:"corpus_cached_after_edit"`
			CorpusChangedAfterCacheClear bool `json:"corpus_changed_after_cache_clear"`
			CorpusIgnoresNonSource       bool `json:"corpus_ignores_non_source"`
		}
		Runtime []struct {
			Name                string
			Keys                []string
			BuildMatchesPublic  bool `json:"build_matches_public"`
			PIDMatchesProcess   bool `json:"pid_matches_process"`
			StartedAtStable     bool `json:"started_at_stable"`
			UptimeNonnegative   bool `json:"uptime_nonnegative"`
			UptimeNondecreasing bool `json:"uptime_nondecreasing"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if !fixture.PublicAliases["build_id"] || !fixture.PublicAliases["runtime_identity"] || len(fixture.Runtime) != 3 {
		t.Fatal("incomplete actual public identity corpus")
	}
	properties := fixture.Build
	if !properties.LowercaseHex || properties.Length != 12 || !properties.MatchesPackageContent || !properties.StableWithinProcess || !properties.CorpusCachedAfterEdit || !properties.CorpusChangedAfterCacheClear || !properties.CorpusIgnoresNonSource {
		t.Fatal("Source build identity relationships changed", properties)
	}
	before, err := Runtime()
	if err != nil {
		t.Fatal(err)
	}
	after, err := Runtime()
	if err != nil {
		t.Fatal(err)
	}
	build, err := BuildID()
	if err != nil {
		t.Fatal(err)
	}
	row := fixture.Runtime[0]
	if row.Name != "without-manager" || !row.BuildMatchesPublic || !row.PIDMatchesProcess || !row.StartedAtStable || !row.UptimeNonnegative || !row.UptimeNondecreasing {
		t.Fatal("Source identity relationships changed", row)
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(string(build)) || before.Build != build || after.Build != build || before.PID != os.Getpid() || after.PID != before.PID || before.StartedAt != after.StartedAt || before.StartedAt != Process().StartedAt || before.StartedAt <= 0 || before.StartedAt > float64(time.Now().UnixNano())/1e9 || before.Uptime < 0 || after.Uptime < before.Uptime {
		t.Fatal("native process identity violates actual Source relationships", before, after, build)
	}
	// Decode only a closed key set; no arbitrary value tree is required to
	// verify the public JSON schema of the manager-free native counterpart.
	encoded, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, row.Keys) {
		t.Fatal("runtime identity JSON differs from actual Source", names, row.Keys)
	}
}
