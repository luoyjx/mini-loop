package workflows

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Value is a closed immutable JSON sum, not an untyped payload. Schemas are
// constrained to objects here; semantic schema and DAG validation is separate.
type Value = jsonvalue.Value
type AuthorityRequirement string
type AgentProfile string
type ToolName string

var ErrDefinition = errors.New("invalid workflow definition")

type BudgetPolicy struct {
	SizeGuideline       string  `json:"size_guideline"`
	MaxConcurrentAgents int     `json:"max_concurrent_agents"`
	MaxAgents           int     `json:"max_agents"`
	MaxRounds           int     `json:"max_rounds"`
	WallTimeSeconds     float64 `json:"wall_time_seconds"`
	TokenBudget         *int    `json:"token_budget"`
}
type ToolPolicy struct {
	OriginAuthorityRequired AuthorityRequirement `json:"origin_authority_required"`
	AgentProfile            AgentProfile         `json:"agent_profile"`
	AllowedTools            []ToolName           `json:"allowed_tools"`
}
type Node struct {
	ID             NodeID   `json:"id"`
	Kind           NodeKind `json:"kind"`
	Needs          []NodeID `json:"needs"`
	PromptTemplate string   `json:"prompt_template"`
	OutputSchema   Value    `json:"output_schema"`
	ItemsFrom      *NodeID  `json:"items_from"`
	MaxRounds      *int     `json:"max_rounds"`
}
type definitionWire struct {
	Name           string           `json:"name"`
	Nodes          []Node           `json:"nodes"`
	ReturnFrom     NodeID           `json:"return_from"`
	SchemaVersion  int              `json:"schema_version"`
	Description    string           `json:"description"`
	Revision       Revision         `json:"revision"`
	DefinitionID   DefinitionID     `json:"definition_id"`
	ParentRevision *Revision        `json:"parent_revision"`
	Source         DefinitionSource `json:"source"`
	SourceVersion  *string          `json:"source_version"`
	InputSchema    Value            `json:"input_schema"`
	OutputSchema   Value            `json:"output_schema"`
	Budget         BudgetPolicy     `json:"budget"`
	Policy         ToolPolicy       `json:"policy"`
	DefinitionHash Value            `json:"definition_hash"` // source ignores any saved copy
}

// Definition retains only immutable canonical projections. Detached decoded
// views cannot change its identity. A hash is content identity, not authorization.
type Definition struct {
	semantic, data Value
	hash           Digest
	revision       Revision
	id             DefinitionID
}

func (d Definition) Hash() Digest                 { return d.hash }
func (d Definition) Revision() Revision           { return d.revision }
func (d Definition) ID() DefinitionID             { return d.id }
func (d Definition) Semantic() Value              { return d.semantic }
func (d Definition) Data() Value                  { return d.data }
func (d Definition) MarshalJSON() ([]byte, error) { return d.data.MarshalJSON() }

func CanonicalJSON(value Value) ([]byte, error) { return value.Sorted().MarshalUTF8() }
func ContentHash(value Value, prefix string) (Digest, error) {
	data, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return Digest(prefix + ":" + hex.EncodeToString(hash[:])), nil
}
func objectSchema() Value {
	return jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "type", Value: jsonvalue.TextValue("object")}})
}
func defaultBudget() BudgetPolicy {
	return BudgetPolicy{"small", HardMaxConcurrentAgents, HardMaxAgentsPerRun, 4, 900, nil}
}
func defaultPolicy() ToolPolicy {
	return ToolPolicy{"explicit_human", "workflow-readonly", []ToolName{"read_file", "glob"}}
}

func decodeStrict[T any](data []byte, target *T) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(Value)); err != io.EOF {
		return ErrDefinition
	}
	return nil
}
func DecodeDefinition(data []byte) (Definition, error) {
	input, err := jsonvalue.Decode(string(data))
	if err != nil {
		return Definition{}, err
	}
	if input.Kind() != jsonvalue.Object {
		return Definition{}, ErrDefinition
	}
	for _, key := range []string{"name", "return_from"} {
		if v, present := input.Lookup(key); !present || v.Kind() != jsonvalue.Text {
			return Definition{}, ErrDefinition
		}
	}
	// Initialize advertised defaults before lowering the typed wire object.
	w := definitionWire{SchemaVersion: SchemaVersion, Source: Dynamic, InputSchema: objectSchema(), OutputSchema: objectSchema(), Budget: defaultBudget(), Policy: defaultPolicy()}
	if err := decodeStrict(data, &w); err != nil {
		return Definition{}, err
	}
	w.DefinitionHash = jsonvalue.NullValue()
	// Node defaults must distinguish absent schema from explicit null.
	var nodeWire struct {
		Nodes []Value `json:"nodes"`
	}
	if nodes, present := input.Lookup("nodes"); present && nodes.Kind() != jsonvalue.Array {
		return Definition{}, ErrDefinition
	}
	if err := json.Unmarshal(data, &nodeWire); err != nil {
		return Definition{}, err
	}
	for i := range w.Nodes {
		n := &w.Nodes[i]
		if id, present := nodeWire.Nodes[i].Lookup("id"); !present || id.Kind() != jsonvalue.Text {
			return Definition{}, ErrDefinition
		}
		if needs, present := nodeWire.Nodes[i].Lookup("needs"); present && needs.Kind() != jsonvalue.Array {
			return Definition{}, ErrDefinition
		}
		if !n.Kind.Valid() {
			return Definition{}, ErrDefinition
		}
		if _, present := nodeWire.Nodes[i].Lookup("output_schema"); !present {
			n.OutputSchema = objectSchema()
		}
		if n.OutputSchema.Kind() != jsonvalue.Object {
			return Definition{}, ErrDefinition
		}
		if n.Needs == nil {
			n.Needs = []NodeID{}
		}
	}
	if !w.Source.Valid() || w.InputSchema.Kind() != jsonvalue.Object || w.OutputSchema.Kind() != jsonvalue.Object {
		return Definition{}, ErrDefinition
	}
	if policy, present := input.Lookup("policy"); present {
		if tools, present := policy.Lookup("allowed_tools"); present && tools.Kind() != jsonvalue.Array {
			return Definition{}, ErrDefinition
		}
	}
	// Work with a closed projection to preserve Python's float identity (900.0).
	encoded, err := json.Marshal(w)
	if err != nil {
		return Definition{}, err
	}
	value, err := jsonvalue.Decode(string(encoded))
	if err != nil {
		return Definition{}, err
	}
	budget, _ := value.Lookup("budget")
	fields := []jsonvalue.Field{}
	for _, key := range budget.Keys() {
		v, _ := budget.Lookup(key)
		if key == "wall_time_seconds" {
			v = jsonvalue.FloatValue(w.Budget.WallTimeSeconds)
			if rawBudget, present := input.Lookup("budget"); present {
				if supplied, present := rawBudget.Lookup(key); present {
					v = supplied
				}
			}
		}
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	budget = jsonvalue.ObjectValue(fields)
	semanticFields := []jsonvalue.Field{}
	for _, key := range []string{"schema_version", "name", "description", "source", "source_version", "input_schema", "output_schema", "budget", "policy", "nodes", "return_from"} {
		v, _ := value.Lookup(key)
		if key == "budget" {
			v = budget
		}
		if key == "nodes" && w.Nodes == nil {
			v = jsonvalue.ArrayValue(nil)
		}
		semanticFields = append(semanticFields, jsonvalue.Field{Name: key, Value: v})
	}
	semantic := jsonvalue.ObjectValue(semanticFields)
	hash, err := ContentHash(semantic, "wfdef")
	if err != nil {
		return Definition{}, err
	}
	suffix := string(hash)[len("wfdef:") : len("wfdef:")+16]
	if w.Revision == "" {
		w.Revision = Revision("wfdef_" + suffix)
	}
	if w.DefinitionID == "" {
		w.DefinitionID = DefinitionID("wf_" + suffix)
	}
	fields = append([]jsonvalue.Field{}, semanticFields...)
	parent, _ := value.Lookup("parent_revision")
	fields = append(fields, jsonvalue.Field{Name: "definition_id", Value: jsonvalue.TextValue(string(w.DefinitionID))}, jsonvalue.Field{Name: "revision", Value: jsonvalue.TextValue(string(w.Revision))}, jsonvalue.Field{Name: "parent_revision", Value: parent}, jsonvalue.Field{Name: "definition_hash", Value: jsonvalue.TextValue(string(hash))})
	return Definition{semantic, jsonvalue.ObjectValue(fields), hash, w.Revision, w.DefinitionID}, nil
}
