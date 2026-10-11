package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type managerRawBinding struct {
	users    map[SessionID]bool
	template bool
}
type managerEfficiencyState struct {
	mu                          sync.Mutex
	stores                      map[*efficiency.RawStore]*managerRawBinding
	started                     bool
	initialize, close           *efficiency.LifecycleReport
	initializeError, closeError *string
}

// ManagerEfficiencyStatus exposes detached reports without granting runtime authority.
type ManagerEfficiencyStatus struct {
	Initialize      *efficiency.LifecycleReportProjection `json:"initialize"`
	Close           *efficiency.LifecycleReportProjection `json:"close"`
	InitializeError *string                               `json:"initialize_error"`
	CloseError      *string                               `json:"close_error"`
}

func (m *SessionManager) EfficiencyStatus() ManagerEfficiencyStatus {
	s := &m.efficiencyState
	s.mu.Lock()
	defer s.mu.Unlock()
	result := ManagerEfficiencyStatus{InitializeError: clonePointer(s.initializeError), CloseError: clonePointer(s.closeError)}
	if s.initialize != nil {
		projection, _ := s.initialize.Projection()
		result.Initialize = &projection
	}
	if s.close != nil {
		projection, _ := s.close.Projection()
		result.Close = &projection
	}
	return result
}

func (m *SessionManager) initializeEfficiency(ctx context.Context) error {
	s := &m.efficiencyState
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()
	report, err := m.config.Services.TokenEfficiency.Initialize(ctx, efficiency.LifecycleServices{})
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.initialize = &report
	} else {
		fault := "RuntimeError"
		s.initializeError = &fault
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func (m *SessionManager) bindEfficiencySession(config RuntimeConfig, catalog *ToolCatalog) (*efficiency.Runtime, *efficiency.RawStore, error) {
	runtime := config.TokenEfficiency
	if runtime == nil {
		return nil, nil, nil
	}
	store := runtime.RawStore()
	c := *m.config.Efficiency
	_, recovery := catalog.Lookup(protocol.ToolReadTokenArtifact)
	if store == nil && runtime.ObservationEnforced() && c.PersistRaw && config.Mode != ModeReadonly && recovery {
		rawConfig := efficiency.DefaultRawStoreConfig(config.Workspace)
		rawConfig.TTLSeconds, rawConfig.MaxArtifactBytes, rawConfig.MaxTotalBytes = c.ArtifactTTLSeconds, c.MaxArtifactBytes, c.MaxTotalBytes
		var err error
		store, err = efficiency.NewRawStore(rawConfig)
		if err != nil {
			return nil, nil, err
		}
		runtime = runtime.WithRawStore(store)
	}
	if store != nil {
		s := &m.efficiencyState
		s.mu.Lock()
		binding := s.stores[store]
		if binding == nil {
			binding = &managerRawBinding{users: make(map[SessionID]bool)}
			s.stores[store] = binding
		}
		binding.users[config.ID] = true
		s.mu.Unlock()
	}
	return runtime, store, nil
}

func (m *SessionManager) releaseEfficiencySession(id SessionID) {
	s := &m.efficiencyState
	s.mu.Lock()
	defer s.mu.Unlock()
	for store, binding := range s.stores {
		if !binding.users[id] {
			continue
		}
		delete(binding.users, id)
		if len(binding.users) == 0 {
			store.Close()
			if !binding.template {
				delete(s.stores, store)
			}
		}
	}
}

func (m *SessionManager) closeEfficiency() {
	s := &m.efficiencyState
	s.mu.Lock()
	for store := range s.stores {
		store.Close()
	}
	s.stores = make(map[*efficiency.RawStore]*managerRawBinding)
	s.mu.Unlock()
	report, err := m.config.Services.TokenEfficiency.Close(context.Background(), efficiency.LifecycleCloseOptions{})
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.close = &report
	} else {
		fault := "RuntimeError"
		s.closeError = &fault
	}
}
