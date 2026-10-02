package agent

type ExecutionMode string

const (
	ExecutionExclusive ExecutionMode = "exclusive"
	ExecutionParallel  ExecutionMode = "parallel"
)

type ExecutionClassifier interface {
	ClassifyExecution(ToolCall) (ExecutionMode, error)
}

func (definition ToolDefinition) WithExecutionClassifier(classifier ExecutionClassifier) ToolDefinition {
	definition.classifier = classifier
	return definition
}

// Scheduling precedes gate hooks. A failing, panicking or invalid classifier
// always creates an ordered barrier, even for a statically parallel-safe tool.
func (definition ToolDefinition) ExecutionMode(call ToolCall) (mode ExecutionMode) {
	mode = ExecutionExclusive
	defer func() {
		if recover() != nil {
			mode = ExecutionExclusive
		}
	}()
	if definition.classifier != nil {
		selected, err := definition.classifier.ClassifyExecution(call)
		if err == nil && (selected == ExecutionParallel || selected == ExecutionExclusive) {
			return selected
		}
		return mode
	}
	if definition.parallelSafe {
		return ExecutionParallel
	}
	return mode
}
