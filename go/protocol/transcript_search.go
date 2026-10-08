package protocol

const ToolTranscriptSearch ToolName = "transcript_search"

// TranscriptSearchInput cannot choose a session, owner or storage backend.
type TranscriptSearchInput struct {
	Query string `json:"query"`
}

func TranscriptSearchToolInput(value TranscriptSearchInput) ToolInput {
	return ToolInput{name: ToolTranscriptSearch, transcriptSearch: value}
}
func (input ToolInput) TranscriptSearch() (TranscriptSearchInput, bool) {
	return input.transcriptSearch, input.name == ToolTranscriptSearch
}
func TranscriptSearchSchema() ToolSchema {
	properties := SchemaProperties{"query": {Type: SchemaString, Description: "Substring to find (case-insensitive)."}}
	return ToolSchema{Name: ToolTranscriptSearch,
		Description: "Search the durable transcript of THIS session (the most recent 20 epochs), including history that compaction summarized away. States when older epochs fall outside the scan.",
		InputSchema: InputSchema{Type: SchemaObject, Properties: &properties, Required: []string{"query"}},
	}
}
