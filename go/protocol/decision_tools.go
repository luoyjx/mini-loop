package protocol

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/decisions"
)

func DecisionToolInput(request decisions.Request) ToolInput {
	return ToolInput{name: ToolDecision, decision: request.Clone()}
}
func (input ToolInput) Decision() (decisions.Request, bool) {
	return input.decision.Clone(), input.name == ToolDecision && input.decisionProjection == nil
}
func DecisionSchema() ToolSchema {
	var schema ToolSchema
	if err := json.Unmarshal([]byte(decisionSchemaJSON), &schema); err != nil {
		panic("invalid compiled decision schema")
	}
	return schema
}

const decisionSchemaJSON = `{"description":"Evaluate explicit state with typed questions: choice returns an option distribution, score returns a probability-weighted 0-based rubric score, noul returns P(yes). Batch independent questions. Uses the configured decision provider and may incur a separate model/API charge. Returns judgments only; confidence never authorizes actions. Inspect probability_source: llm_estimate is uncalibrated.","input_schema":{"additionalProperties":false,"properties":{"questions":{"additionalProperties":{"oneOf":[{"additionalProperties":false,"properties":{"criteria":{"additionalProperties":{"type":["string","object","array","null"]},"maxProperties":255,"minProperties":1,"type":"object"},"instructions":{"type":["string","object","array"]},"type":{"const":"choice"}},"required":["type","instructions","criteria"],"type":"object"},{"additionalProperties":false,"properties":{"criteria":{"items":{"type":["string","object","array"]},"maxItems":10,"minItems":2,"type":"array"},"instructions":{"type":["string","object","array"]},"type":{"const":"score"}},"required":["type","instructions","criteria"],"type":"object"},{"additionalProperties":false,"properties":{"criteria":{"additionalProperties":false,"properties":{"false":{"type":["string","object","array"]},"true":{"type":["string","object","array"]}},"type":"object"},"instructions":{"type":["string","object","array"]},"type":{"const":"noul"}},"required":["type","instructions"],"type":"object"}]},"description":"Named independent questions evaluated against the same state.","maxProperties":32,"minProperties":1,"type":"object"},"state":{"description":"Explicit evidence to evaluate; no session history is added.","type":["string","object","array"]}},"required":["state","questions"],"type":"object"},"name":"decision"}`
