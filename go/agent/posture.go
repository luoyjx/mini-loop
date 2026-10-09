package agent

import (
	"errors"
	"reflect"

	"github.com/luoyjx/mini-loop/go/secrets"
)

// RuntimePosture is an observation, never an authorization or durability grant.
// Component labels identify native implementations; absent components are None.
type RuntimePosture struct {
	Authenticated       bool    `json:"authenticated"`
	StateStore          string  `json:"state_store"`
	StateStoreError     *string `json:"state_store_error"`
	ActionJournal       string  `json:"action_journal"`
	Secrets             string  `json:"secrets"`
	SecretsUnregistered int     `json:"secrets_unregistered"`
	Sandbox             string  `json:"sandbox"`
	SandboxReason       *string `json:"sandbox_reason"`
	CachePolicy         string  `json:"cache_policy"`
	StuckDetector       string  `json:"stuck_detector"`
	UserResources       bool    `json:"user_resources"`
	Workflows           bool    `json:"workflows"`
	Trajectories        bool    `json:"trajectories"`
}

// componentLabel inspects type metadata only, without calling an implementation.
func componentLabel(t reflect.Type) string {
	if t == nil {
		return "None"
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() != "" {
		return t.Name()
	}
	return truncateRunes(t.String(), 100)
}

// ObserveRuntimePosture reads the first live Agent when available, matching
// Source insertion order. It creates no session, resolves no owner resources and
// calls no model/tool/cache/detector. Nil environment means the current process.
// Snapshot locks never span injected registry callbacks or persistence reads.
func (manager *SessionManager) ObserveRuntimePosture(authenticated bool, environment secrets.Environment) (report RuntimePosture, err error) {
	defer func() {
		if recover() != nil {
			report = RuntimePosture{}
			err = errors.New("runtime posture observation failed")
		}
	}()
	manager.mu.Lock()
	services := manager.config.Services
	workflow := manager.workflows != nil
	var live *Session
	handles := make([]*ManagedSession, 0, len(manager.sessions))
	for _, id := range manager.order {
		if handle := manager.sessions[id]; handle != nil {
			handles = append(handles, handle)
			if live == nil {
				live = handle.core
			}
		}
	}
	manager.mu.Unlock()
	var mask TextMasker = services.Secrets
	var cache CachePolicy = services.CachePolicy
	var detector StuckDetector = services.StuckDetector
	if live != nil {
		// These bindings are immutable after session publication. Registry contents
		// and persistence errors own their separate concurrent snapshot methods.
		mask, cache, detector = live.secrets, live.cachePolicy, live.stuckDetector
	} else {
		if cache == nil {
			cache = NewDefaultCachePolicy()
		}
		if detector == nil {
			detector = NewDefaultStuckDetector()
		}
	}
	report = RuntimePosture{Authenticated: authenticated,
		StateStore:    componentLabel(reflect.TypeOf(services.StateStore)),
		ActionJournal: componentLabel(reflect.TypeOf(services.ActionJournal)),
		Secrets:       componentLabel(reflect.TypeOf(mask)),
		// The native runtime currently executes host commands without a sandbox seam.
		Sandbox: "None", CachePolicy: componentLabel(reflect.TypeOf(cache)),
		StuckDetector: componentLabel(reflect.TypeOf(detector)),
		UserResources: services.UserResources != nil, Workflows: workflow,
		Trajectories: services.Trajectories != nil}
	for _, handle := range handles {
		if fault := handle.PersistenceStatus().Error; fault != nil && *fault != "" {
			report.StateStoreError = fault
			break
		}
	}
	switch mask.(type) {
	case nil:
	case secrets.Null, *secrets.Null:
		report.Secrets = "NullSecretRegistry"
		// Source's no-registry path does not double-count missing masking.
	default:
		var names []secrets.Name
		if registry, ok := mask.(interface{ Names() []secrets.Name }); ok {
			names = registry.Names()
		}
		report.SecretsUnregistered = secrets.CountUnregistered(environment, names)
	}
	return report, nil
}
