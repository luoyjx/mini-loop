package astcontext

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type probeWire struct {
	Status, Version, Homepage, Message string
	SchemaVersion                      int `json:"schema_version"`
}

type sourceFixture struct {
	Configurations []struct {
		Name          string
		Input, Output json.RawMessage
		Error         string
	}
	Probes []struct {
		Name, Stdout, Stderr string
		ExitCode             *int `json:"exit_code"`
		Output               probeWire
		Executed             bool
	}
	Projections []struct {
		Status, Error string
		Probe         probeWire
	}
}

func fixture(t *testing.T) sourceFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-ast-context.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows sourceFixture
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestConfigurationMatchesSource(t *testing.T) {
	for _, row := range fixture(t).Configurations {
		t.Run(row.Name, func(t *testing.T) {
			parameters := DefaultParameters()
			err := json.Unmarshal(row.Input, &parameters)
			if row.Name == "numeric-env" {
				if err == nil {
					t.Fatal("native typed admission accepted numeric environment name")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			config, err := NewConfig(parameters)
			if row.Error != "" {
				if err == nil || err.Error() != row.Error {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var expected Parameters
			if err := json.Unmarshal(row.Output, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(config.Parameters(), expected) {
				t.Fatal(config.Parameters(), expected)
			}
		})
	}
}

func TestVersionOutputMatchesActualSourceProbes(t *testing.T) {
	config, err := NewConfig(DefaultParameters())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range fixture(t).Probes {
		// Admission fixtures intentionally have no output recipe: parsing cannot
		// establish executable integrity or pretend to have launched a process.
		if row.ExitCode == nil {
			continue
		}
		count++
		t.Run(row.Name, func(t *testing.T) {
			probe, err := ParseVersion(config, VersionOutput{row.Stdout, row.Stderr, *row.ExitCode})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(probe)
			if err != nil {
				t.Fatal(err)
			}
			var actual probeWire
			if err := json.Unmarshal(data, &actual); err != nil {
				t.Fatal(err)
			}
			if actual != row.Output {
				t.Fatal(actual, row.Output)
			}
		})
	}
	if count != 12 {
		t.Fatal("missing actual version-output recipes", count)
	}
}

func TestProbeStatusAndProjection(t *testing.T) {
	for _, row := range fixture(t).Projections {
		status, err := ParseStatus(row.Status)
		if row.Error != "" {
			if err == nil || err.Error() != row.Error {
				t.Fatal(err, row.Error)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		probe, err := NewProbe(status, row.Probe.Version, row.Probe.Homepage, row.Probe.Message)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(probe)
		if err != nil {
			t.Fatal(err)
		}
		var actual probeWire
		if err := json.Unmarshal(data, &actual); err != nil {
			t.Fatal(err)
		}
		if actual != row.Probe {
			t.Fatal(actual, row.Probe)
		}
	}
	if _, err := json.Marshal(Probe{}); err == nil {
		t.Fatal("zero probe serialized as valid")
	}
	if _, err := NewProbe(Status(255), "", "", ""); err == nil {
		t.Fatal("invalid status accepted")
	}
	if _, err := ParseVersion(Config{}, VersionOutput{}); err == nil {
		t.Fatal("zero configuration accepted")
	}
}

func TestDetachedConfigurationAndDurationAdmission(t *testing.T) {
	p := DefaultParameters()
	digest := strings.Repeat("a", 64)
	p.ExpectedSHA256 = &digest
	p.EnvPassthrough = []string{"LANG", "CUSTOM", "LANG"}
	config, err := NewConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	p.EnvPassthrough[0] = "changed"
	digest = "changed"
	snapshot := config.Parameters()
	snapshot.EnvPassthrough[0] = "changed again"
	*snapshot.ExpectedSHA256 = "changed again"
	actual := config.Parameters()
	if !reflect.DeepEqual(actual.EnvPassthrough, []string{"LANG", "CUSTOM"}) || *actual.ExpectedSHA256 != strings.Repeat("a", 64) {
		t.Fatal(actual)
	}
	if config.Timeout() != 15*time.Second {
		t.Fatal(config.Timeout())
	}
	for _, timeout := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e30, 1e-12} {
		p := DefaultParameters()
		p.TimeoutSeconds = timeout
		if _, err := NewConfig(p); err == nil {
			t.Fatal("unrepresentable timeout accepted", timeout)
		}
	}
	// Semantic components must not overflow a machine integer during comparison.
	p = DefaultParameters()
	p.MaxVersionExclusive = "1.9." + strings.Repeat("9", 100)
	config, err = NewConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := ParseVersion(config, VersionOutput{Stdout: "ast-outline 1.9.99999999999999999999\nhomepage: " + Homepage})
	if err != nil || probe.Snapshot().Status != Applied {
		t.Fatal(probe, err)
	}
}
