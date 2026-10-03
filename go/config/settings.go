// Package config resolves named, typed settings before runtime construction.
// Loading does not create directories, discover .env files, or mutate os.Environ.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/luoyjx/mini-loop/go/workspace"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type OptimizationMode string
type ResponseStyle string
type DecisionMode string

const (
	OptimizationOff     OptimizationMode = "off"
	OptimizationShadow  OptimizationMode = "shadow"
	OptimizationEnforce OptimizationMode = "enforce"
	ResponseNormal      ResponseStyle    = "normal"
	ResponseConcise     ResponseStyle    = "concise"
	DecisionsOff        DecisionMode     = "off"
	DecisionsLLM        DecisionMode     = "llm"
	DecisionsJev        DecisionMode     = "jev"
	BuiltinSkills                        = "<builtin>"
)

// Seconds is finite, positive elapsed time after validation, with checked
// conversion to Go's nanosecond duration range.
type Seconds float64

func (s Seconds) Duration() time.Duration { return time.Duration(float64(s) * float64(time.Second)) }

// Secret formats as presence only; Reveal is the explicit credential boundary.
type Secret struct{ value string }

func (s Secret) Reveal() string { return s.value }
func (s Secret) Present() bool  { return s.value != "" }
func (s Secret) String() string {
	if s.Present() {
		return "<set>"
	}
	return "<absent>"
}
func (s Secret) GoString() string             { return s.String() }
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

type Settings struct {
	Model                             string           `json:"model"`
	BaseURL                           *string          `json:"base_url"`
	APIKey                            Secret           `json:"-"`
	MaxTokens                         int              `json:"max_tokens"`
	TokenThreshold                    int              `json:"token_threshold"`
	TokenEfficiencyMode               OptimizationMode `json:"token_efficiency_mode"`
	TokenEfficiencyResponseStyle      ResponseStyle    `json:"token_efficiency_response_style"`
	TokenEfficiencyPersistRaw         bool             `json:"token_efficiency_persist_raw"`
	TokenEfficiencyRawMinBytes        int              `json:"token_efficiency_raw_min_bytes"`
	TokenEfficiencyArtifactTTLSeconds Seconds          `json:"token_efficiency_artifact_ttl_seconds"`
	TokenEfficiencyMaxArtifactBytes   int              `json:"token_efficiency_max_artifact_bytes"`
	TokenEfficiencyMaxTotalBytes      int              `json:"token_efficiency_max_total_bytes"`
	ASTOutlineEnabled                 bool             `json:"ast_outline_enabled"`
	ASTOutlineBinary                  string           `json:"ast_outline_binary"`
	ASTOutlineSHA256                  *string          `json:"ast_outline_sha256"`
	ASTOutlineTimeout                 Seconds          `json:"ast_outline_timeout"`
	ASTOutlineMaxOutputBytes          int              `json:"ast_outline_max_output_bytes"`
	MaxConcurrentLLM                  int              `json:"max_concurrent_llm"`
	MaxConcurrentTools                int              `json:"max_concurrent_tools"`
	MaxTurns                          int              `json:"max_turns"`
	SubagentMaxRounds                 int              `json:"subagent_max_rounds"`
	SubagentMaxDepth                  int              `json:"subagent_max_depth"`
	BashTimeout                       int              `json:"bash_timeout"`
	FallbackModel                     *string          `json:"fallback_model"`
	RateLimitPerMinute                int              `json:"rate_limit_per_minute"`
	ApprovalTimeout                   Seconds          `json:"approval_timeout"`
	WorkspaceRoot                     string           `json:"workspace_root"`
	BindableRoots                     []string         `json:"bindable_roots"`
	SpillDir                          *string          `json:"spill_dir"`
	SkillsDir                         string           `json:"skills_dir"`
	UserResourcesRoot                 *string          `json:"user_resources_root"`
	MemoryRoot                        *string          `json:"memory_root"`
	RepoRoot                          *string          `json:"repo_root"`
	TrajectoryRoot                    *string          `json:"trajectory_root"`
	TrajectoryEnabled                 bool             `json:"trajectory_enabled"`
	TrajectoryCaptureContent          bool             `json:"trajectory_capture_content"`
	TeamIdlePoll                      Seconds          `json:"team_idle_poll"`
	TeamIdleTimeout                   Seconds          `json:"team_idle_timeout"`
	FakeLLM                           bool             `json:"fake_llm"`
	EnableFeatures                    bool             `json:"enable_features"`
	GuardianEnabled                   bool             `json:"guardian_enabled"`
	DecisionMode                      DecisionMode     `json:"decision_mode"`
	DecisionModel                     string           `json:"decision_model"`
	TypesafeAPIKey                    Secret           `json:"-"`
	EnableWorkflows                   bool             `json:"enable_workflows"`
	WorkflowMaxConcurrentAgents       int              `json:"workflow_max_concurrent_agents"`
	WorkflowMaxAgents                 int              `json:"workflow_max_agents"`
	WorkflowMaxRounds                 int              `json:"workflow_max_rounds"`
	WorkflowWallTimeSeconds           Seconds          `json:"workflow_wall_time_seconds"`
}

// Snapshot owns no live credentials, and all pointer/slice fields are detached.
type Snapshot struct {
	Settings
	APIKey         *string `json:"api_key"`
	TypesafeAPIKey *string `json:"typesafe_api_key"`
}

func ptr[T any](v T) *T { return &v }
func clone[T any](v *T) *T {
	if v == nil {
		return nil
	}
	return ptr(*v)
}
func (s Settings) Snapshot() Snapshot {
	result := Snapshot{Settings: s}
	if s.APIKey.Present() {
		result.APIKey = ptr("<set>")
	}
	if s.TypesafeAPIKey.Present() {
		result.TypesafeAPIKey = ptr("<set>")
	}
	result.Settings.APIKey, result.Settings.TypesafeAPIKey = Secret{}, Secret{}
	result.BindableRoots = append([]string{}, s.BindableRoots...)
	if s.BaseURL != nil {
		result.Settings.BaseURL = ptr(RedactEndpoint(*s.BaseURL))
	}
	result.Settings.ASTOutlineSHA256 = clone(s.ASTOutlineSHA256)
	result.Settings.FallbackModel = clone(s.FallbackModel)
	result.Settings.SpillDir = clone(s.SpillDir)
	result.Settings.UserResourcesRoot = clone(s.UserResourcesRoot)
	result.Settings.MemoryRoot = clone(s.MemoryRoot)
	result.Settings.RepoRoot = clone(s.RepoRoot)
	result.Settings.TrajectoryRoot = clone(s.TrajectoryRoot)
	return result
}
func (s Settings) String() string {
	data, err := json.Marshal(s.Snapshot())
	if err != nil {
		return "Settings (unprintable)"
	}
	return string(data)
}
func (s Settings) GoString() string { return s.String() }

type LoadOptions struct{ Directory, DefaultSkillsDir string }

func Load(env map[string]string, options LoadOptions) (Settings, error) {
	var s Settings
	get := func(name string) (string, bool) {
		if env == nil {
			return os.LookupEnv(name)
		}
		v, ok := env[name]
		return v, ok
	}
	text := func(name, fallback string) string {
		v, ok := get(name)
		if !ok {
			return fallback
		}
		return v
	}
	if options.Directory == "" {
		directory, err := os.Getwd()
		if err != nil {
			return s, err
		}
		options.Directory = directory
	}
	directory, err := filepath.Abs(options.Directory)
	if err != nil {
		return s, err
	}
	path := func(raw string) (string, error) {
		if !filepath.IsAbs(raw) {
			raw = filepath.Join(directory, raw)
		}
		return workspace.ResolvePath(raw)
	}
	fail := func(name string, expected string) error {
		return fmt.Errorf("%s must be %s; refusing to guess", name, expected)
	}
	integer := func(name string, fallback int) (int, error) {
		raw := text(name, "")
		if strings.TrimSpace(raw) == "" {
			return fallback, nil
		}
		normalized, err := numberText(raw)
		if err != nil {
			return 0, fail(name, "an integer")
		}
		n, err := strconv.ParseInt(normalized, 10, strconv.IntSize)
		if err != nil {
			return 0, fail(name, "an integer within the Go int range")
		}
		return int(n), nil
	}
	real := func(name string, fallback float64) (Seconds, error) {
		raw := text(name, "")
		if strings.TrimSpace(raw) == "" {
			return Seconds(fallback), nil
		}
		normalized, err := numberText(raw)
		if err != nil {
			return 0, fail(name, "a finite number")
		}
		n, err := decimalFloat(normalized)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fail(name, "a finite number")
		}
		return Seconds(n), nil
	}
	boolean := func(name string, fallback bool) (bool, error) {
		raw, ok := get(name)
		if !ok {
			return fallback, nil
		}
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes", "on":
			return true, nil
		case "", "0", "false", "no", "off":
			return false, nil
		}
		return false, fail(name, "1/true/yes/on or empty/0/false/no/off")
	}
	optional := func(raw string) *string {
		if raw == "" {
			return nil
		}
		return ptr(raw)
	}
	var n int
	s.Model = text("MODEL_ID", "claude-sonnet-4-6")
	s.BaseURL = optional(text("ANTHROPIC_BASE_URL", ""))
	s.APIKey = Secret{text("ANTHROPIC_API_KEY", "")}
	s.MaxTokens, err = integer("MINILOOP_MAX_TOKENS", 8000)
	if err != nil {
		return s, err
	}
	s.TokenThreshold, err = integer("MINILOOP_TOKEN_THRESHOLD", 100000)
	if err != nil {
		return s, err
	}
	s.TokenEfficiencyMode = OptimizationMode(strings.ToLower(strings.TrimSpace(text("MINILOOP_TOKEN_EFFICIENCY_MODE", "off"))))
	s.TokenEfficiencyResponseStyle = ResponseStyle(strings.ToLower(strings.TrimSpace(text("MINILOOP_TOKEN_EFFICIENCY_RESPONSE_STYLE", "normal"))))
	s.TokenEfficiencyPersistRaw, err = boolean("MINILOOP_TOKEN_EFFICIENCY_PERSIST_RAW", true)
	if err != nil {
		return s, err
	}
	s.TokenEfficiencyRawMinBytes, err = integer("MINILOOP_TOKEN_EFFICIENCY_RAW_MIN_BYTES", 16384)
	if err != nil {
		return s, err
	}
	s.TokenEfficiencyArtifactTTLSeconds, err = real("MINILOOP_TOKEN_EFFICIENCY_ARTIFACT_TTL_SECONDS", 3600)
	if err != nil {
		return s, err
	}
	s.TokenEfficiencyMaxArtifactBytes, err = integer("MINILOOP_TOKEN_EFFICIENCY_MAX_ARTIFACT_BYTES", 2000000)
	if err != nil {
		return s, err
	}
	s.TokenEfficiencyMaxTotalBytes, err = integer("MINILOOP_TOKEN_EFFICIENCY_MAX_TOTAL_BYTES", 20000000)
	if err != nil {
		return s, err
	}
	s.ASTOutlineEnabled, err = boolean("MINILOOP_AST_OUTLINE_ENABLED", false)
	if err != nil {
		return s, err
	}
	s.ASTOutlineBinary = strings.TrimSpace(text("MINILOOP_AST_OUTLINE_BINARY", "ast-outline"))
	s.ASTOutlineSHA256 = optional(strings.ToLower(strings.TrimSpace(text("MINILOOP_AST_OUTLINE_SHA256", ""))))
	s.ASTOutlineTimeout, err = real("MINILOOP_AST_OUTLINE_TIMEOUT", 10)
	if err != nil {
		return s, err
	}
	s.ASTOutlineMaxOutputBytes, err = integer("MINILOOP_AST_OUTLINE_MAX_OUTPUT_BYTES", 1000000)
	if err != nil {
		return s, err
	}
	s.MaxConcurrentLLM, err = integer("MINILOOP_MAX_CONCURRENT_LLM", 8)
	if err != nil {
		return s, err
	}
	s.MaxConcurrentTools, err = integer("MINILOOP_MAX_CONCURRENT_TOOLS", 8)
	if err != nil {
		return s, err
	}
	s.MaxTurns, err = integer("MINILOOP_MAX_TURNS", 50)
	if err != nil {
		return s, err
	}
	s.SubagentMaxRounds, err = integer("MINILOOP_SUBAGENT_MAX_ROUNDS", 30)
	if err != nil {
		return s, err
	}
	s.SubagentMaxDepth, err = integer("MINILOOP_SUBAGENT_MAX_DEPTH", 2)
	if err != nil {
		return s, err
	}
	s.BashTimeout, err = integer("MINILOOP_BASH_TIMEOUT", 120)
	if err != nil {
		return s, err
	}
	s.FallbackModel = optional(text("MINILOOP_FALLBACK_MODEL", ""))
	s.RateLimitPerMinute, err = integer("MINILOOP_RATE_LIMIT_PER_MINUTE", 0)
	if err != nil {
		return s, err
	}
	n, err = integer("MINILOOP_APPROVAL_TIMEOUT", 300)
	if err != nil {
		return s, err
	}
	s.ApprovalTimeout = Seconds(n)
	s.WorkspaceRoot, err = path(text("MINILOOP_WORKSPACE_ROOT", "./workspaces"))
	if err != nil {
		return s, err
	}
	s.BindableRoots = []string{}
	for _, raw := range strings.Split(text("MINILOOP_BINDABLE_ROOTS", ""), string(os.PathListSeparator)) {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		value, err := path(raw)
		if err != nil {
			return s, err
		}
		s.BindableRoots = append(s.BindableRoots, value)
	}
	rawSpillDir := text("MINILOOP_SPILL_DIR", "./var/spill")
	if strings.TrimSpace(rawSpillDir) != "" {
		value, err := path(rawSpillDir)
		if err != nil {
			return s, err
		}
		s.SpillDir = &value
	}
	s.SkillsDir = options.DefaultSkillsDir
	if s.SkillsDir == "" {
		s.SkillsDir = BuiltinSkills
	}
	if raw, ok := get("MINILOOP_SKILLS_DIR"); ok {
		s.SkillsDir, err = path(raw)
		if err != nil {
			return s, err
		}
	}
	if raw := text("MINILOOP_USER_RESOURCES_ROOT", ""); raw != "" {
		value, err := path(raw)
		if err != nil {
			return s, err
		}
		s.UserResourcesRoot = &value
	}
	if raw := text("MINILOOP_MEMORY_ROOT", ""); raw != "" {
		value, err := path(raw)
		if err != nil {
			return s, err
		}
		s.MemoryRoot = &value
	}
	if raw := text("MINILOOP_REPO_ROOT", ""); raw != "" {
		value, err := path(raw)
		if err != nil {
			return s, err
		}
		s.RepoRoot = &value
	}
	if raw := text("MINILOOP_TRAJECTORY_ROOT", ""); raw != "" {
		value, err := path(raw)
		if err != nil {
			return s, err
		}
		s.TrajectoryRoot = &value
	}
	s.TrajectoryEnabled, err = boolean("MINILOOP_TRAJECTORIES", true)
	if err != nil {
		return s, err
	}
	s.TrajectoryCaptureContent, err = boolean("MINILOOP_TRAJECTORY_CAPTURE_CONTENT", true)
	if err != nil {
		return s, err
	}
	s.TeamIdlePoll, err = real("MINILOOP_TEAM_IDLE_POLL", 1)
	if err != nil {
		return s, err
	}
	s.TeamIdleTimeout, err = real("MINILOOP_TEAM_IDLE_TIMEOUT", 60)
	if err != nil {
		return s, err
	}
	rawFakeLLM := text("MINILOOP_FAKE_LLM", "")
	s.FakeLLM = rawFakeLLM != "" && rawFakeLLM != "0" && rawFakeLLM != "false"
	rawEnableFeatures := text("MINILOOP_FEATURES", "")
	s.EnableFeatures = rawEnableFeatures != "" && rawEnableFeatures != "0" && rawEnableFeatures != "false"
	s.GuardianEnabled, err = boolean("MINILOOP_GUARDIAN", false)
	if err != nil {
		return s, err
	}
	s.DecisionMode = DecisionMode(strings.ToLower(strings.TrimSpace(text("MINILOOP_DECISIONS", "off"))))
	s.DecisionModel = text("MINILOOP_DECISION_MODEL", "jev-latest")
	s.TypesafeAPIKey = Secret{text("TYPESAFE_API_KEY", "")}
	s.EnableWorkflows, err = boolean("MINILOOP_EXPERIMENTAL_WORKFLOWS", false)
	if err != nil {
		return s, err
	}
	s.WorkflowMaxConcurrentAgents, err = integer("MINILOOP_WORKFLOW_MAX_CONCURRENT_AGENTS", 4)
	if err != nil {
		return s, err
	}
	s.WorkflowMaxAgents, err = integer("MINILOOP_WORKFLOW_MAX_AGENTS", 32)
	if err != nil {
		return s, err
	}
	s.WorkflowMaxRounds, err = integer("MINILOOP_WORKFLOW_MAX_ROUNDS", 4)
	if err != nil {
		return s, err
	}
	s.WorkflowWallTimeSeconds, err = real("MINILOOP_WORKFLOW_WALL_TIME_SECONDS", 900)
	if err != nil {
		return s, err
	}
	return s, s.Validate()
}

// Python accepts decimal Unicode digits and underscores between digits. The
// normalized spelling still goes through integer/float parsers; nothing is guessed.
func numberText(raw string) (string, error) {
	runes := []rune(strings.TrimSpace(raw))
	for i, r := range runes {
		for _, entry := range unicode.Nd.R16 {
			if r >= rune(entry.Lo) && r <= rune(entry.Hi) && (r-rune(entry.Lo))%rune(entry.Stride) == 0 {
				runes[i] = '0' + ((r-rune(entry.Lo))/rune(entry.Stride))%10
				break
			}
		}
		if r > 0xffff {
			for _, entry := range unicode.Nd.R32 {
				if r >= rune(entry.Lo) && r <= rune(entry.Hi) && (r-rune(entry.Lo))%rune(entry.Stride) == 0 {
					runes[i] = '0' + ((r-rune(entry.Lo))/rune(entry.Stride))%10
					break
				}
			}
		}
	}
	out := strings.Builder{}
	for i, r := range runes {
		if r == '_' {
			if i == 0 || i == len(runes)-1 || runes[i-1] < '0' || runes[i-1] > '9' || runes[i+1] < '0' || runes[i+1] > '9' {
				return "", errors.New("invalid numeric separator")
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String(), nil
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var decimalFloatPattern = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)

// strconv also accepts hexadecimal floats, which Python float(environment) refuses.
func decimalFloat(normalized string) (float64, error) {
	if !decimalFloatPattern.MatchString(normalized) {
		return 0, errors.New("expected decimal float")
	}
	return strconv.ParseFloat(normalized, 64)
}

// RedactEndpoint keeps diagnostic URLs free of userinfo, query and fragment data.
func RedactEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<invalid endpoint>"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

func validSeconds(s Seconds) bool {
	n := float64(s)
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 1/float64(time.Second) && n < float64(math.MaxInt64)/float64(time.Second)
}

func (s Settings) Validate() error {
	if s.DecisionMode != DecisionsOff && s.DecisionMode != DecisionsLLM && s.DecisionMode != DecisionsJev {
		return errors.New("decision_mode must be one of: off, llm, jev")
	}
	if strings.TrimSpace(s.DecisionModel) == "" {
		return errors.New("decision_model must not be empty")
	}
	if s.DecisionMode == DecisionsJev && strings.TrimSpace(s.TypesafeAPIKey.Reveal()) == "" {
		return errors.New("Jev decisions require TYPESAFE_API_KEY")
	}
	if s.TokenEfficiencyMode != OptimizationOff && s.TokenEfficiencyMode != OptimizationShadow && s.TokenEfficiencyMode != OptimizationEnforce {
		return errors.New("token_efficiency_mode must be one of: off, shadow, enforce")
	}
	if s.TokenEfficiencyResponseStyle != ResponseNormal && s.TokenEfficiencyResponseStyle != ResponseConcise {
		return errors.New("token_efficiency_response_style must be one of: normal, concise")
	}
	if s.ASTOutlineBinary == "" {
		return errors.New("ast_outline_binary must not be empty")
	}
	if s.ASTOutlineSHA256 != nil && !digestPattern.MatchString(*s.ASTOutlineSHA256) {
		return errors.New("ast_outline_sha256 must be 64 lowercase hex characters")
	}
	if s.ASTOutlineEnabled {
		if !filepath.IsAbs(s.ASTOutlineBinary) && !strings.HasPrefix(s.ASTOutlineBinary, "~/") {
			return errors.New("enabled ast-outline requires an operator-pinned absolute binary path")
		}
		if s.ASTOutlineSHA256 == nil {
			return errors.New("enabled ast-outline requires ast_outline_sha256")
		}
	}
	if s.TokenEfficiencyRawMinBytes < 1 {
		return errors.New("token_efficiency_raw_min_bytes must be at least 1")
	}
	if s.TokenEfficiencyMaxArtifactBytes < 1 {
		return errors.New("token_efficiency_max_artifact_bytes must be at least 1")
	}
	if s.TokenEfficiencyMaxTotalBytes < 1 {
		return errors.New("token_efficiency_max_total_bytes must be at least 1")
	}
	if s.ASTOutlineMaxOutputBytes < 1 {
		return errors.New("ast_outline_max_output_bytes must be at least 1")
	}
	if s.MaxConcurrentLLM < 1 {
		return errors.New("max_concurrent_llm must be at least 1")
	}
	if s.MaxConcurrentTools < 1 {
		return errors.New("max_concurrent_tools must be at least 1")
	}
	if s.MaxTurns < 1 {
		return errors.New("max_turns must be at least 1")
	}
	if s.SubagentMaxRounds < 1 {
		return errors.New("subagent_max_rounds must be at least 1")
	}
	if s.SubagentMaxDepth < 1 {
		return errors.New("subagent_max_depth must be at least 1")
	}
	if s.MaxTokens < 1 {
		return errors.New("max_tokens must be at least 1")
	}
	if s.TokenThreshold < 1 {
		return errors.New("token_threshold must be at least 1")
	}
	if s.BashTimeout < 1 {
		return errors.New("bash_timeout must be at least 1")
	}
	if uint64(s.BashTimeout) > uint64(math.MaxInt64)/uint64(time.Second) {
		return errors.New("bash_timeout exceeds Go duration range")
	}
	if s.WorkflowMaxConcurrentAgents < 1 {
		return errors.New("workflow_max_concurrent_agents must be at least 1")
	}
	if s.WorkflowMaxRounds < 1 {
		return errors.New("workflow_max_rounds must be at least 1")
	}
	if !validSeconds(s.TokenEfficiencyArtifactTTLSeconds) {
		return errors.New("token_efficiency_artifact_ttl_seconds must be finite seconds from one nanosecond through Go duration range")
	}
	if !validSeconds(s.ASTOutlineTimeout) {
		return errors.New("ast_outline_timeout must be finite seconds from one nanosecond through Go duration range")
	}
	if !validSeconds(s.ApprovalTimeout) {
		return errors.New("approval_timeout must be finite seconds from one nanosecond through Go duration range")
	}
	if !validSeconds(s.TeamIdlePoll) {
		return errors.New("team_idle_poll must be finite seconds from one nanosecond through Go duration range")
	}
	if !validSeconds(s.TeamIdleTimeout) {
		return errors.New("team_idle_timeout must be finite seconds from one nanosecond through Go duration range")
	}
	if !validSeconds(s.WorkflowWallTimeSeconds) {
		return errors.New("workflow_wall_time_seconds must be finite seconds from one nanosecond through Go duration range")
	}
	if s.TokenEfficiencyRawMinBytes > s.TokenEfficiencyMaxArtifactBytes {
		return errors.New("token_efficiency_raw_min_bytes must not exceed token_efficiency_max_artifact_bytes")
	}
	if s.TokenEfficiencyMaxArtifactBytes > s.TokenEfficiencyMaxTotalBytes {
		return errors.New("token_efficiency_max_artifact_bytes must not exceed token_efficiency_max_total_bytes")
	}
	if s.RateLimitPerMinute < 0 {
		return errors.New("rate_limit_per_minute must be 0 (off) or positive")
	}
	if s.WorkflowMaxConcurrentAgents > 4 {
		return errors.New("workflow_max_concurrent_agents must not exceed 4")
	}
	if s.WorkflowMaxAgents < s.WorkflowMaxConcurrentAgents || s.WorkflowMaxAgents > 32 {
		return errors.New("workflow_max_agents must cover concurrency and be at most 32")
	}
	return nil
}
