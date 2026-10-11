package workflows

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

var workflowID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)

func ValidateDefinition(definition Definition) error {
	data, err := definition.MarshalJSON()
	if err != nil {
		return err
	}
	var d definitionWire
	if err = decodeStrict(data, &d); err != nil {
		return err
	}
	require := func(ok bool, message string) error {
		if !ok {
			return invalid(DefinitionFailure, message)
		}
		return nil
	}
	checks := []struct {
		ok      bool
		message string
	}{
		{d.SchemaVersion == SchemaVersion, fmt.Sprintf("unsupported workflow schema_version %d", d.SchemaVersion)},
		{workflowID.MatchString(d.Name), "workflow name is invalid"}, {len(d.Nodes) > 0, "workflow must contain at least one node"},
		{d.Budget.MaxConcurrentAgents > 0 && d.Budget.MaxConcurrentAgents <= HardMaxConcurrentAgents, "max_concurrent_agents must be between 1 and 4"},
		{d.Budget.MaxAgents > 0 && d.Budget.MaxAgents <= HardMaxAgentsPerRun, "max_agents must be between 1 and 32"},
		{d.Budget.MaxRounds > 0, "max_rounds must be positive"}, {d.Budget.WallTimeSeconds > 0, "wall_time_seconds must be positive"},
		{d.Budget.TokenBudget == nil, "token_budget is not implemented by the MVP"},
		{len(d.Policy.AllowedTools) > 0, "allowed_tools must not be empty"},
	}
	for _, check := range checks {
		if err := require(check.ok, check.message); err != nil {
			return err
		}
	}
	tools := map[ToolName]bool{}
	for _, name := range d.Policy.AllowedTools {
		tools[name] = true
	}
	if err := require(len(tools) == len(d.Policy.AllowedTools), "allowed_tools must not contain duplicates"); err != nil {
		return err
	}
	forbidden := []string{}
	for name := range tools {
		if name != "read_file" && name != "glob" {
			forbidden = append(forbidden, string(name))
		}
	}
	sort.Strings(forbidden)
	if err := require(len(forbidden) == 0, "workflow tools are not read-only: "+reprNames(forbidden)); err != nil {
		return err
	}
	if err := require(len(tools) == 2, "MVP allowed_tools must be exactly read_file and glob"); err != nil {
		return err
	}
	if err := require(d.Policy.AgentProfile == "workflow-readonly", "agent_profile must be workflow-readonly"); err != nil {
		return err
	}
	if err := validateSchemaAt(d.InputSchema, "input_schema"); err != nil {
		return err
	}
	if err := validateSchemaAt(d.OutputSchema, "output_schema"); err != nil {
		return err
	}
	nodes := map[NodeID]Node{}
	for _, node := range d.Nodes {
		repr := jsonvalue.ArrayValue([]Value{jsonvalue.TextValue(string(node.ID))}).PythonString()
		if err := require(workflowID.MatchString(string(node.ID)), "invalid node id "+repr[1:len(repr)-1]); err != nil {
			return err
		}
		if _, exists := nodes[node.ID]; exists {
			return invalid(DefinitionFailure, "duplicate node id "+string(node.ID))
		}
		if err := require(node.Kind == Agent || node.Kind == Verify || node.Kind == Reduce, "node kind "+string(node.Kind)+" is not implemented by the MVP engine"); err != nil {
			return err
		}
		if err := require(node.MaxRounds == nil || *node.MaxRounds > 0, string(node.ID)+".max_rounds"); err != nil {
			return err
		}
		if err := require(node.MaxRounds == nil || *node.MaxRounds <= d.Budget.MaxRounds, string(node.ID)+".max_rounds exceeds workflow budget"); err != nil {
			return err
		}
		if err := require(node.ItemsFrom == nil, string(node.ID)+".items_from is not implemented"); err != nil {
			return err
		}
		if err := validateSchemaAt(node.OutputSchema, "nodes."+string(node.ID)+".output_schema"); err != nil {
			return err
		}
		nodes[node.ID] = node
	}
	if err := require(len(d.Nodes) <= d.Budget.MaxAgents, "definition has more nodes than max_agents"); err != nil {
		return err
	}
	returned, exists := nodes[d.ReturnFrom]
	if err := require(exists, "return_from references an unknown node"); err != nil {
		return err
	}
	if err := require(pythonEqual(returned.OutputSchema, d.OutputSchema), "return node output_schema must match workflow output_schema"); err != nil {
		return err
	}
	degrees := map[NodeID]int{}
	outgoing := map[NodeID][]NodeID{}
	for _, node := range d.Nodes {
		degrees[node.ID] = 0
	}
	for _, node := range d.Nodes {
		seen := map[NodeID]bool{}
		for _, dep := range node.Needs {
			seen[dep] = true
		}
		if err := require(len(seen) == len(node.Needs), string(node.ID)+" has duplicate needs"); err != nil {
			return err
		}
		for _, dep := range node.Needs {
			if _, exists := nodes[dep]; !exists {
				return invalid(DefinitionFailure, string(node.ID)+" needs unknown node "+string(dep))
			}
			if err := require(dep != node.ID, string(node.ID)+" cannot depend on itself"); err != nil {
				return err
			}
			degrees[node.ID]++
			outgoing[dep] = append(outgoing[dep], node.ID)
		}
	}
	queue := []NodeID{}
	for _, node := range d.Nodes {
		if degrees[node.ID] == 0 {
			queue = append(queue, node.ID)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, child := range outgoing[id] {
			degrees[child]--
			if degrees[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	return require(visited == len(nodes), "workflow graph must be acyclic")
}
