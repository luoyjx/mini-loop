package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/workflows"
)

const (
	ToolWorkflow       ToolName = "Workflow"
	ToolWorkflowStatus ToolName = "WorkflowStatus"
	ToolWorkflowCancel ToolName = "WorkflowCancel"
)

type WorkflowRunID = workflows.RunID

// WorkflowInput preserves the model's original definition, including supplied
// metadata, for action identity. Dynamic admission normalizes a separate copy.
// The recursive values are closed immutable JSON sums, constrained to objects.
type WorkflowInput struct {
	Definition jsonvalue.Value `json:"definition"`
	Args       jsonvalue.Value `json:"args"`
}

type WorkflowReferenceInput struct {
	RunID WorkflowRunID `json:"run_id"`
}

func WorkflowToolInput(value WorkflowInput) ToolInput {
	return ToolInput{name: ToolWorkflow, workflow: value}
}
func WorkflowStatusToolInput(value WorkflowReferenceInput) ToolInput {
	return ToolInput{name: ToolWorkflowStatus, workflowReference: value}
}
func WorkflowCancelToolInput(value WorkflowReferenceInput) ToolInput {
	return ToolInput{name: ToolWorkflowCancel, workflowReference: value}
}
func (input ToolInput) Workflow() (WorkflowInput, bool) {
	return input.workflow, input.name == ToolWorkflow
}
func (input ToolInput) WorkflowReference() (WorkflowReferenceInput, bool) {
	return input.workflowReference, input.name == ToolWorkflowStatus || input.name == ToolWorkflowCancel
}

func validateWorkflowInput(value WorkflowInput) error {
	if value.Definition.Kind() != jsonvalue.Object || value.Args.Kind() != jsonvalue.Object {
		return errors.New("Workflow requires object definition and args")
	}
	if _, err := value.Definition.MarshalJSON(); err != nil {
		return err
	}
	_, err := value.Args.MarshalJSON()
	return err
}

func decodeWorkflowInput(name ToolName, data []byte) (ToolInput, error) {
	object, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolInput{}, err
	}
	if object.Kind() != jsonvalue.Object {
		return ToolInput{}, errors.New("workflow tool input must be an object")
	}
	if name == ToolWorkflow {
		definition, hasDefinition := object.Lookup("definition")
		args, hasArgs := object.Lookup("args")
		if !hasDefinition || !hasArgs || len(object.Keys()) != 2 {
			return ToolInput{}, errors.New("Workflow requires exactly definition and args")
		}
		input := WorkflowToolInput(WorkflowInput{Definition: definition, Args: args})
		return input, input.Validate()
	}
	runID, present := object.Lookup("run_id")
	if !present || runID.Kind() != jsonvalue.Text || len(object.Keys()) != 1 {
		return ToolInput{}, errors.New("workflow reference requires exactly string run_id")
	}
	var value struct {
		RunID WorkflowRunID `json:"run_id"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return ToolInput{}, err
	}
	reference := WorkflowReferenceInput{RunID: value.RunID}
	if name == ToolWorkflowStatus {
		return WorkflowStatusToolInput(reference), nil
	}
	return WorkflowCancelToolInput(reference), nil
}

// WorkflowToolSchemas advertises source contracts only. It installs no handlers,
// grants no authority, and does not add these optional tools to default catalogues.
func WorkflowToolSchemas() []ToolSchema {
	fields := SchemaProperties{
		"definition": {Type: SchemaObject, Description: "Versioned declarative WorkflowDefinition."},
		"args":       {Type: SchemaObject, Description: "Arguments validated against definition.input_schema."},
	}
	refs := SchemaProperties{"run_id": {Type: SchemaString}}
	launch := InputSchema{Type: SchemaObject, Properties: &fields, Required: []string{"definition", "args"}, Additional: AdditionalAllowed(false)}
	reference := InputSchema{Type: SchemaObject, Properties: &refs, Required: []string{"run_id"}, Additional: AdditionalAllowed(false)}
	return []ToolSchema{
		{Name: ToolWorkflow, Description: "Launch a bounded, read-only declarative workflow. This experimental tool is local-only and requires trusted human origin.", InputSchema: launch},
		{Name: ToolWorkflowStatus, Description: "Inspect a workflow owned by this session.", InputSchema: reference.Clone()},
		{Name: ToolWorkflowCancel, Description: "Cooperatively cancel a workflow owned by this session.", InputSchema: reference.Clone()},
	}
}
