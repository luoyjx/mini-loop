package agent

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const MaxToolPayload = 60_000
const DefaultModel = "claude-sonnet-4-6"
const DefaultMaxTokens = 8_000
const DefaultTokenThreshold = 100_000

// ToolCatalogSnapshot pins exactly the fitted schema list consumed by both the
// prompt builder and provider. Accessors detach its recursively typed schemas.
type ToolCatalogSnapshot struct {
	schemas        []protocol.ToolSchema
	sent           []protocol.ToolName
	omitted        []protocol.ToolName
	fingerprint    string
	trimmedTo      int
	inventoryCount int
}

func (snapshot ToolCatalogSnapshot) Schemas() []protocol.ToolSchema {
	result := make([]protocol.ToolSchema, len(snapshot.schemas))
	for i, schema := range snapshot.schemas {
		result[i] = schema.Clone()
	}
	return result
}
func (snapshot ToolCatalogSnapshot) SentNames() []protocol.ToolName {
	return append([]protocol.ToolName(nil), snapshot.sent...)
}
func (snapshot ToolCatalogSnapshot) OmittedNames() []protocol.ToolName {
	return append([]protocol.ToolName(nil), snapshot.omitted...)
}
func (snapshot ToolCatalogSnapshot) Fingerprint() string { return snapshot.fingerprint }
func (snapshot ToolCatalogSnapshot) TrimmedTo() int      { return snapshot.trimmedTo }
func (snapshot ToolCatalogSnapshot) InventoryCount() int { return snapshot.inventoryCount }

func schemaPayloadSize(schemas []protocol.ToolSchema) (int, error) {
	text, err := protocol.PythonJSON(schemas, true, false)
	return len(text), err // ensure_ascii makes bytes and Python characters identical
}

func (catalog *ToolCatalog) Snapshot() (ToolCatalogSnapshot, error) {
	schemas := make([]protocol.ToolSchema, len(catalog.ordered))
	for i, definition := range catalog.ordered {
		schemas[i] = definition.schema.Clone()
	}
	snapshot := ToolCatalogSnapshot{inventoryCount: len(schemas)}
	size, err := schemaPayloadSize(schemas)
	if err != nil {
		return snapshot, err
	}
	if size > MaxToolPayload {
		for _, limit := range []int{1000, 400, 200, 80} {
			for i := range schemas {
				if utf8.RuneCountInString(schemas[i].Description) > limit {
					schemas[i].Description = string([]rune(schemas[i].Description)[:limit]) + "..."
				}
			}
			snapshot.trimmedTo = limit
			size, err = schemaPayloadSize(schemas)
			if err != nil {
				return snapshot, err
			}
			if size <= MaxToolPayload {
				break
			}
		}
	}
	if size > MaxToolPayload {
		kept := make([]protocol.ToolSchema, 0, len(schemas))
		used := 0
		for _, schema := range schemas {
			weight, err := schemaPayloadSize([]protocol.ToolSchema{schema})
			if err != nil {
				return snapshot, err
			}
			if used+weight <= MaxToolPayload {
				kept = append(kept, schema)
				used += weight
			} else {
				snapshot.omitted = append(snapshot.omitted, schema.Name)
			}
		}
		schemas = kept
	}
	snapshot.schemas = schemas
	for _, schema := range schemas {
		snapshot.sent = append(snapshot.sent, schema.Name)
	}
	canonical, err := protocol.PythonJSON(schemas, false, true)
	if err != nil {
		return snapshot, err
	}
	snapshot.fingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	return snapshot, nil
}

type SystemContext struct {
	Workspace string
	Catalog   ToolCatalogSnapshot
	Skills    string
}
type SystemBuilder interface {
	BuildSystem(SystemContext) (string, error)
}
type DefaultSystemBuilder struct{}

func (DefaultSystemBuilder) BuildSystem(value SystemContext) (string, error) {
	names := make([]string, len(value.Catalog.sent))
	for i, name := range value.Catalog.sent {
		names[i] = string(name)
	}
	core := fmt.Sprintf("You are a coding agent working in %s.\nUse the provided tools to act; prefer doing over explaining.\nBefore calling tools, send one concise user-facing progress update; it is commentary, not the final answer. After the work is complete, send a concise final answer.\nFor multi-step work, lay out a plan with TodoWrite and keep it updated.\nDelegate large side-quests to a subagent via `task` to keep your context clean.\nPull in specialized knowledge with `load_skill` only when you need it.\nTools available: %s\nSkills available:\n%s", value.Workspace, strings.Join(names, ", "), value.Skills)
	if len(value.Catalog.omitted) != 0 {
		omitted := value.Catalog.omitted
		limit := min(20, len(omitted))
		names := make([]string, limit)
		for i, name := range omitted[:limit] {
			names[i] = string(name)
		}
		tail := ""
		if len(omitted) > limit {
			tail = fmt.Sprintf(", and %d more", len(omitted)-limit)
		}
		core += fmt.Sprintf("\n\n%d registered tool(s) are NOT included in this request because the combined tool definitions exceed the per-request budget: %s%s. Their definitions were not sent, so they cannot be called reliably; if one is needed, it is unavailable.", len(omitted), strings.Join(names, ", "), tail)
	}
	return core, nil
}

func requestEnvelope(snapshot ToolCatalogSnapshot, system string) string {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(system)))
	return snapshot.fingerprint + ":" + hash[:16]
}

func (s *Session) buildRequest() (protocol.ModelRequest, string, error) {
	snapshot, err := s.gate.catalog.Snapshot()
	if err != nil {
		return protocol.ModelRequest{}, "", err
	}
	s.requestCatalog = snapshot
	descriptions := ""
	if s.skills != nil {
		descriptions = s.skills.Descriptions()
	}
	system, err := s.systemBuilder.BuildSystem(SystemContext{s.executionRoot(), snapshot, descriptions})
	if err != nil {
		return protocol.ModelRequest{}, "", err
	}
	request := protocol.ModelRequest{Model: s.model, MaxTokens: s.maxTokens, Messages: append([]protocol.Message(nil), s.messages...), System: &system, Tools: snapshot.Schemas(), Purpose: protocol.PurposeAgentTurn}
	return request, requestEnvelope(snapshot, system), nil
}

func (s *Session) injectRuntimeFacts(envelope string) {
	var parts []string
	if s.todos != nil && len(s.todos.Snapshot()) != 0 {
		parts = append(parts, "Current TodoWrite state:\n"+s.todos.Render())
	}
	used := s.meter.UsedFor(s.messages, envelope)
	for _, band := range []struct {
		fraction float64
		label    string
	}{{.90, "over 90%"}, {.75, "over 75%"}} {
		if float64(used) >= float64(s.tokenThreshold)*band.fraction {
			parts = append(parts, fmt.Sprintf("Context is %s full; older tool output may be cleared automatically to make room. The `compress` tool frees space deliberately.", band.label))
			break
		}
	}
	facts := strings.Join(parts, "\n\n")
	changed := facts != s.runtimeFacts
	s.runtimeFacts = facts
	if changed && facts != "" {
		s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("<runtime-state>\n" + facts + "\n</runtime-state>")})
	}
}
