package protocol

import "github.com/luoyjx/mini-loop/go/internal/jsonvalue"

// BashBackgroundRequest retains a model observation. Execution receives
// only its explicit Python-truth projection; the observed tree is never arguments.
type BashBackgroundRequest struct{ observed jsonvalue.Value }

func (request BashBackgroundRequest) requested() bool { return request.observed.Truth() }
func (request BashBackgroundRequest) MarshalJSON() ([]byte, error) {
	return request.observed.MarshalJSON()
}

// BashApprovalProposal retains optional approval metadata. Only an array of
// strings can reach candidate validation; other observed shapes propose nothing.
// A proposal does not supply a grant or bypass the existing permission gate.
type BashApprovalProposal struct{ observed jsonvalue.Value }

func (proposal BashApprovalProposal) tokens() *[]string {
	items, ok := proposal.observed.Array()
	if !ok {
		return nil
	}
	tokens := make([]string, len(items))
	for i, item := range items {
		text, ok := item.Text()
		if !ok {
			return nil
		}
		tokens[i] = text
	}
	return &tokens
}
func (proposal BashApprovalProposal) MarshalJSON() ([]byte, error) {
	return proposal.observed.MarshalJSON()
}

func (input ToolInput) validateBashOptions() error {
	if input.bashBackground != nil {
		if _, err := input.bashBackground.observed.MarshalLegacyUTF8(); err != nil {
			return err
		}
	}
	if input.bashApproval != nil {
		if _, err := input.bashApproval.observed.MarshalLegacyUTF8(); err != nil {
			return err
		}
	}
	return nil
}
