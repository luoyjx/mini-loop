package launcher

import (
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/decisions"
)

// DecisionBackend reports the selected runtime service without credentials or I/O.
type DecisionBackend string

const (
	DecisionOff    DecisionBackend = "off"
	DecisionLLM    DecisionBackend = "llm"
	DecisionJev    DecisionBackend = "jev"
	DecisionCustom DecisionBackend = "custom"
)

func selectedDecisionBackend(settings config.Settings, options Options) DecisionBackend {
	if !options.DecisionTools && settings.DecisionMode == config.DecisionsOff {
		return DecisionOff
	}
	if options.DecisionProvider != nil {
		return DecisionCustom
	}
	if settings.DecisionMode == config.DecisionsJev {
		return DecisionJev
	}
	return DecisionLLM
}

func configuredDecision(settings config.Settings, options Options) (bool, decisions.Provider, error) {
	switch selectedDecisionBackend(settings, options) {
	case DecisionOff:
		return false, nil, nil
	case DecisionCustom:
		return true, options.DecisionProvider, nil
	case DecisionJev:
		cfg := decisions.DefaultJevConfig(settings.TypesafeAPIKey.Reveal())
		cfg.Model = settings.DecisionModel
		backend, err := decisions.NewJev(cfg)
		return true, backend, err
	default:
		return true, nil, nil
	}
}
