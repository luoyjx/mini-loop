// Package astcontext models the operator-controlled AST adapter contract.
// Version parsing is inert: it neither executes a binary nor grants trust in one.
package astcontext

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const Homepage = "https://github.com/ast-outline/ast-outline"
const SchemaVersion = 1

type Status uint8

const (
	Invalid Status = iota
	Applied
	NoMatch
	Partial
	Ambiguous
	Error
	Missing
	Incompatible
)

func (s Status) String() string {
	switch s {
	case Applied:
		return "applied"
	case NoMatch:
		return "no_match"
	case Partial:
		return "partial"
	case Ambiguous:
		return "ambiguous"
	case Error:
		return "error"
	case Missing:
		return "missing"
	case Incompatible:
		return "incompatible"
	default:
		return "invalid"
	}
}

func ParseStatus(value string) (Status, error) {
	for s := Applied; s <= Incompatible; s++ {
		if s.String() == value {
			return s, nil
		}
	}
	return Invalid, fmt.Errorf("unknown ast context status: %s", value)
}

func (s Status) MarshalJSON() ([]byte, error) {
	if s < Applied || s > Incompatible {
		return nil, errors.New("invalid AST context status")
	}
	return json.Marshal(s.String())
}

type Parameters struct {
	Binary              string   `json:"binary"`
	TimeoutSeconds      float64  `json:"timeout_seconds"`
	MaxOutputBytes      int      `json:"max_output_bytes"`
	MinVersion          string   `json:"min_version"`
	MaxVersionExclusive string   `json:"max_version_exclusive"`
	Homepage            string   `json:"homepage"`
	SchemaVersion       int      `json:"schema_version"`
	EnvPassthrough      []string `json:"env_passthrough"`
	ExpectedSHA256      *string  `json:"expected_sha256"`
}

func DefaultParameters() Parameters {
	return Parameters{Binary: "ast-outline", TimeoutSeconds: 15, MaxOutputBytes: 2_000_000,
		MinVersion: "1.9.0", MaxVersionExclusive: "1.10.0", Homepage: Homepage,
		SchemaVersion: SchemaVersion, EnvPassthrough: []string{}}
}

type Config struct {
	parameters       Parameters
	minimum, maximum [3]*big.Int
	timeout          time.Duration
	valid            bool
}

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func NewConfig(p Parameters) (Config, error) {
	if p.Binary == "" {
		return Config{}, errors.New("ast-outline binary must not be empty")
	}
	if p.TimeoutSeconds <= 0 {
		return Config{}, errors.New("ast-outline timeout_seconds must be positive")
	}
	if math.IsNaN(p.TimeoutSeconds) || math.IsInf(p.TimeoutSeconds, 0) || p.TimeoutSeconds*float64(time.Second) >= float64(math.MaxInt64) || p.TimeoutSeconds*float64(time.Second) < 1 {
		return Config{}, errors.New("ast-outline timeout_seconds must fit a finite positive duration")
	}
	if p.MaxOutputBytes <= 0 {
		return Config{}, errors.New("ast-outline max_output_bytes must be positive")
	}
	names := []string{}
	seen := make(map[string]bool)
	for _, name := range p.EnvPassthrough {
		if !environmentName.MatchString(name) {
			return Config{}, errors.New("env_passthrough must contain environment names")
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	p.EnvPassthrough = names
	if p.ExpectedSHA256 != nil {
		digest := strings.ToLower(pytext.Strip(*p.ExpectedSHA256))
		if !digestPattern.MatchString(digest) {
			return Config{}, errors.New("expected_sha256 must be 64 lowercase hex characters")
		}
		p.ExpectedSHA256 = &digest
	}
	if p.SchemaVersion != SchemaVersion {
		return Config{}, errors.New("this adapter supports ast-outline JSON schema 1 only")
	}
	minimum, err := versionTuple(p.MinVersion)
	if err != nil {
		return Config{}, err
	}
	maximum, err := versionTuple(p.MaxVersionExclusive)
	if err != nil {
		return Config{}, err
	}
	if compareVersion(minimum, maximum) >= 0 {
		return Config{}, errors.New("ast-outline version range is empty")
	}
	return Config{parameters: p, minimum: minimum, maximum: maximum, timeout: time.Duration(p.TimeoutSeconds * float64(time.Second)), valid: true}, nil
}

// Parameters returns a detached snapshot, including optional digest and names.
func (c Config) Parameters() Parameters {
	p := c.parameters
	p.EnvPassthrough = append([]string{}, p.EnvPassthrough...)
	if p.ExpectedSHA256 != nil {
		digest := *p.ExpectedSHA256
		p.ExpectedSHA256 = &digest
	}
	return p
}
func (c Config) Timeout() time.Duration { return c.timeout }

func versionPrefix(runes []rune) (int, [3]*big.Int, bool) {
	var tuple [3]*big.Int
	index := 0
	for part := range tuple {
		start := index
		for index < len(runes) {
			if _, ok := pytext.DecimalDigit(runes[index]); !ok {
				break
			}
			index++
		}
		if start == index {
			return 0, tuple, false
		}
		tuple[part], _ = pytext.Decimal(string(runes[start:index]))
		if part < 2 {
			if index >= len(runes) || runes[index] != '.' {
				return 0, tuple, false
			}
			index++
		}
	}
	return index, tuple, true
}

func versionTuple(value string) ([3]*big.Int, error) {
	_, tuple, ok := versionPrefix([]rune(value))
	if !ok {
		return tuple, fmt.Errorf("invalid semantic version: %s", pytext.Repr(value))
	}
	return tuple, nil
}

func compareVersion(a, b [3]*big.Int) int {
	for i := range a {
		if order := a[i].Cmp(b[i]); order != 0 {
			return order
		}
	}
	return 0
}

type Probe struct {
	status                     Status
	version, homepage, message string
}

type ProbeSnapshot struct {
	Status        Status `json:"status"`
	Version       string `json:"version"`
	Homepage      string `json:"homepage"`
	SchemaVersion int    `json:"schema_version"`
	Message       string `json:"message,omitempty"`
}

func NewProbe(status Status, version, homepage, message string) (Probe, error) {
	if status < Applied || status > Incompatible {
		return Probe{}, errors.New("invalid AST context status")
	}
	return Probe{status: status, version: version, homepage: homepage, message: message}, nil
}
func (p Probe) Snapshot() ProbeSnapshot {
	return ProbeSnapshot{p.status, p.version, p.homepage, SchemaVersion, p.message}
}
func (p Probe) MarshalJSON() ([]byte, error) { return json.Marshal(p.Snapshot()) }

type VersionOutput struct {
	Stdout, Stderr string
	ExitCode       int
}

// ParseVersion interprets captured --version output. Execution, executable
// integrity and workspace containment belong to the future process adapter.
func ParseVersion(c Config, output VersionOutput) (Probe, error) {
	if !c.valid {
		return Probe{}, errors.New("AST context configuration is not initialized")
	}
	if output.ExitCode != 0 {
		return NewProbe(Error, "", "", fmt.Sprintf("ast-outline --version exited %d: %s", output.ExitCode, pytext.Strip(output.Stderr)))
	}
	version := bannerField(output.Stdout, "ast-outline", true)
	homepage := bannerField(output.Stdout, "homepage:", false)
	if version == "" || homepage == "" {
		return NewProbe(Incompatible, "", "", "ast-outline --version did not report the expected version and homepage")
	}
	homepage = strings.TrimRight(homepage, "/")
	expected := strings.TrimRight(c.parameters.Homepage, "/")
	tuple, err := versionTuple(version)
	if err != nil {
		return Probe{}, err
	}
	if homepage != expected || compareVersion(c.minimum, tuple) > 0 || compareVersion(tuple, c.maximum) >= 0 {
		return NewProbe(Incompatible, version, homepage, fmt.Sprintf("expected %s version >= %s and < %s", expected, c.parameters.MinVersion, c.parameters.MaxVersionExclusive))
	}
	return NewProbe(Applied, version, homepage, "")
}

// Python multiline anchors permit whitespace across lines after the prefix,
// but a value must end at its line boundary. Decimal blocks are Python-pinned.
func bannerField(text, prefix string, isVersion bool) string {
	runes := []rune(text)
	p := []rune(prefix)
	for start := range runes {
		if start != 0 && runes[start-1] != '\n' {
			continue
		}
		if len(runes)-start < len(p) || string(runes[start:start+len(p)]) != prefix {
			continue
		}
		i := start + len(p)
		before := i
		for i < len(runes) && pytext.IsSpace(runes[i]) {
			i++
		}
		if isVersion && i == before {
			continue
		}
		begin := i
		if isVersion {
			length, _, ok := versionPrefix(runes[i:])
			if !ok {
				continue
			}
			i += length
			if i < len(runes) && (runes[i] == '-' || runes[i] == '+') {
				i++
				suffix := i
				for i < len(runes) && !pytext.IsSpace(runes[i]) {
					i++
				}
				if i == suffix {
					continue
				}
			}
		} else {
			for i < len(runes) && !pytext.IsSpace(runes[i]) {
				i++
			}
		}
		end := i
		for i < len(runes) && runes[i] != '\n' && pytext.IsSpace(runes[i]) {
			i++
		}
		if begin != end && (i == len(runes) || runes[i] == '\n') {
			return string(runes[begin:end])
		}
	}
	return ""
}
