package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type ServerSettings struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	FakeDelay       time.Duration `json:"-"`
	ShutdownTimeout time.Duration `json:"-"`
}

func (s ServerSettings) Address() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }
func LoadServer(env map[string]string) (ServerSettings, error) {
	get := func(name, fallback string) string {
		if env == nil {
			if v, ok := os.LookupEnv(name); ok {
				return v
			}
		} else if v, ok := env[name]; ok {
			return v
		}
		return fallback
	}
	s := ServerSettings{Host: get("HOST", "127.0.0.1"), Port: 8000, ShutdownTimeout: 10 * time.Second}
	// RefuseOpenBind's historical empty host exception cannot own a listener:
	// net.Listen(":port") binds every interface, so reject it at this boundary.
	if strings.TrimSpace(s.Host) == "" {
		return s, errors.New("HOST must not be empty; use 127.0.0.1 for an unauthenticated loopback listener")
	}
	if raw := get("PORT", "8000"); raw != "8000" {
		normalized, err := numberText(raw)
		if err != nil {
			return s, errors.New("PORT must be an integer")
		}
		n, err := strconv.Atoi(normalized)
		if err != nil {
			return s, errors.New("PORT must be an integer")
		}
		s.Port = n
	}
	if s.Port < 0 || s.Port > 65535 {
		return s, errors.New("PORT must be between 0 and 65535")
	}
	if get("MINILOOP_RELOAD", "") != "" {
		return s, errors.New("MINILOOP_RELOAD is not implemented in the Go launcher")
	}
	if raw := get("MINILOOP_FAKE_DELAY", "0"); raw != "" {
		normalized, err := numberText(raw)
		if err != nil {
			return s, errors.New("MINILOOP_FAKE_DELAY must be finite seconds")
		}
		n, err := decimalFloat(normalized)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) >= float64(math.MaxInt64)/float64(time.Second) {
			return s, errors.New("MINILOOP_FAKE_DELAY must be finite seconds within Go duration range")
		}
		s.FakeDelay = time.Duration(n * float64(time.Second))
	}
	return s, nil
}

type UnsupportedSetting struct {
	Variable string `json:"variable"`
	Reason   string `json:"reason"`
}

func (s Settings) Unsupported() []UnsupportedSetting {
	result := []UnsupportedSetting{}
	add := func(enabled bool, variable, reason string) {
		if enabled {
			result = append(result, UnsupportedSetting{variable, reason})
		}
	}
	add(s.EnableFeatures, "MINILOOP_FEATURES", "comprehensive feature services are not implemented")
	add(s.EnableWorkflows, "MINILOOP_EXPERIMENTAL_WORKFLOWS", "workflow services are not implemented")
	add(s.GuardianEnabled, "MINILOOP_GUARDIAN", "guardian implementation is not available")
	add(s.TokenEfficiencyMode != OptimizationOff, "MINILOOP_TOKEN_EFFICIENCY_MODE", "protected request/observation projections are not implemented")
	add(s.TokenEfficiencyResponseStyle != ResponseNormal, "MINILOOP_TOKEN_EFFICIENCY_RESPONSE_STYLE", "response policies are not implemented")
	add(s.ASTOutlineEnabled, "MINILOOP_AST_OUTLINE_ENABLED", "pinned AST tool integration is not implemented")
	add(s.UserResourcesRoot != nil, "MINILOOP_USER_RESOURCES_ROOT", "owner resources are not implemented")
	add(s.MemoryRoot != nil, "MINILOOP_MEMORY_ROOT", "memory services are not implemented")
	return result
}
func (s Settings) RequireSupported() error {
	missing := s.Unsupported()
	if len(missing) == 0 {
		return nil
	}
	var lines []string
	for _, entry := range missing {
		lines = append(lines, entry.Variable+": "+entry.Reason)
	}
	return fmt.Errorf("Go launcher cannot activate configured settings: %s", strings.Join(lines, "; "))
}
