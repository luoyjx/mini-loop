package reload

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestObserverProfileActualSourceEnvironment(t *testing.T) {
	var fixture struct {
		Platform    string `json:"platform"`
		Environment struct {
			Force []struct {
				Value         *string `json:"value"`
				Result        bool
				ExplicitTrue  bool `json:"explicit_true"`
				ExplicitFalse bool `json:"explicit_false"`
			} `json:"force_polling"`
			Delay []struct {
				Value  *string     `json:"value"`
				Result json.Number `json:"result"`
				Error  string      `json:"error"`
			} `json:"poll_delay"`
			Auto []struct {
				System, Release string
				Result          bool
			} `json:"auto_polling"`
			Debug []struct {
				Value  *string `json:"value"`
				Result bool
			} `json:"debug"`
			Constructors []struct {
				Milliseconds string
				Accepted     bool
			} `json:"delay_constructor"`
		} `json:"environment"`
	}
	data, err := os.ReadFile("../testdata/python-watchfiles.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	host := ObserverHost{System: fixture.Platform}
	if len(fixture.Environment.Force) != 10 || len(fixture.Environment.Delay) != 11 || len(fixture.Environment.Auto) != 6 || len(fixture.Environment.Debug) != 5 || len(fixture.Environment.Constructors) != 2 {
		t.Fatal("missing actual Source environment cases")
	}
	for _, row := range fixture.Environment.Force {
		env := map[string]string{}
		if row.Value != nil {
			env["WATCHFILES_FORCE_POLLING"] = *row.Value
		}
		for _, override := range []*bool{nil, boolPointer(true), boolPointer(false)} {
			want := row.Result
			if override != nil {
				if *override {
					want = row.ExplicitTrue
				} else {
					want = row.ExplicitFalse
				}
			}
			profile, err := SelectObserverProfile(env, host, override)
			if err != nil {
				t.Fatal(err)
			}
			if (profile.Backend() == PollingBackend) != want {
				t.Fatalf("force %v override %v: %s", row.Value, override, profile.Backend())
			}
		}
	}
	for _, row := range fixture.Environment.Delay {
		env := map[string]string{}
		if row.Value != nil {
			env["WATCHFILES_POLL_DELAY_MS"] = *row.Value
		}
		want, conversionErr := strconv.ParseUint(row.Result.String(), 10, 64)
		profile, err := SelectObserverProfile(env, host, nil)
		if row.Error != "" || conversionErr != nil {
			if err == nil {
				t.Fatal("Source-rejected delay accepted")
			}
			continue
		}
		if err != nil || profile.PollDelayMilliseconds() != want {
			t.Fatal(profile, err, "want", want)
		}
	}
	for _, row := range fixture.Environment.Auto {
		profile, err := SelectObserverProfile(nil, ObserverHost{System: row.System, Release: row.Release}, nil)
		if err != nil || (profile.Backend() == PollingBackend) != row.Result {
			t.Fatal(row, profile, err)
		}
	}
	for _, row := range fixture.Environment.Debug {
		env := map[string]string{}
		if row.Value != nil {
			env["WATCHFILES_DEBUG"] = *row.Value
		}
		profile, err := SelectObserverProfile(env, host, nil)
		if err != nil || profile.Debug() != row.Result {
			t.Fatal(row, profile, err)
		}
	}
	for _, row := range fixture.Environment.Constructors {
		_, err := SelectObserverProfile(map[string]string{"WATCHFILES_POLL_DELAY_MS": row.Milliseconds}, host, nil)
		if (err == nil) != row.Accepted {
			t.Fatal(row, err)
		}
	}
}
func boolPointer(value bool) *bool { return &value }
