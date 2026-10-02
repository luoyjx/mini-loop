package agent

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const CacheMaxBreakpoints = 4
const CacheLookbackBlocks = 20
const CacheStride = 15

type CachePolicy interface {
	Annotate(protocol.ModelRequest) (protocol.ModelRequest, error)
}
type NullCachePolicy struct{}

func (NullCachePolicy) Annotate(request protocol.ModelRequest) (protocol.ModelRequest, error) {
	return request.Clone(), nil
}

type CacheConfig struct {
	TTL            protocol.CacheTTL
	MaxBreakpoints int
	Stride         int
}

func DefaultCacheConfig() CacheConfig {
	return CacheConfig{MaxBreakpoints: CacheMaxBreakpoints, Stride: CacheStride}
}

type DefaultCachePolicy struct{ config CacheConfig }

func NewDefaultCachePolicy() DefaultCachePolicy {
	policy, _ := NewCachePolicy(DefaultCacheConfig())
	return policy
}
func NewCachePolicy(config CacheConfig) (DefaultCachePolicy, error) {
	if config.MaxBreakpoints < 1 || config.Stride < 1 || config.Stride > CacheLookbackBlocks {
		return DefaultCachePolicy{}, errors.New("cache budget must be positive and stride between 1 and 20")
	}
	return DefaultCachePolicy{config}, nil
}
func (policy DefaultCachePolicy) Annotate(request protocol.ModelRequest) (protocol.ModelRequest, error) {
	if policy.config == (CacheConfig{}) {
		policy.config = DefaultCacheConfig()
	}
	request = request.Clone()
	control := protocol.CacheControl{Type: protocol.CacheEphemeral, TTL: policy.config.TTL}
	budget := policy.config.MaxBreakpoints
	request.Cache = protocol.CacheAnnotations{}
	if request.System != nil && *request.System != "" {
		request.Cache.System = &control
		budget--
	}
	run := 0
	chosen := make([]protocol.CacheBreakpoint, 0)
	for mi := len(request.Messages) - 1; mi >= 0 && len(chosen) < budget; mi-- {
		message := request.Messages[mi]
		blocks, ok := message.Content.Blocks()
		if message.Role != protocol.RoleUser || !ok || len(blocks) == 0 {
			if ok {
				run += len(blocks)
			} else {
				run++
			}
			continue
		}
		for bi := len(blocks) - 1; bi >= 0 && len(chosen) < budget; bi-- {
			if len(chosen) == 0 || run >= policy.config.Stride {
				chosen = append(chosen, protocol.CacheBreakpoint{MessageIndex: mi, BlockIndex: bi, Control: control})
				run = 0
			} else {
				run++
			}
		}
	}
	for i, j := 0, len(chosen)-1; i < j; i, j = i+1, j-1 {
		chosen[i], chosen[j] = chosen[j], chosen[i]
	}
	request.Cache.Messages = chosen
	return request, nil
}
