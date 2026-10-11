package agent

import (
	"encoding/json"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// TrajectorySummaryArchive retains observational values from historical files.
// Producer metadata and scan counters keep their concrete native types. These
// closed JSON variants never become model settings, workspaces or build authority.
type TrajectorySummaryArchive struct {
	Model, Workspace, Build        jsonvalue.Value
	Metrics                        jsonvalue.Value
	StartedAt, EndedAt, DurationMS jsonvalue.Value
}

// ArchiveValue is the complete inert summary wire projection. Readers select
// their final encoding boundary after optional recording masks.
func (s TrajectorySummary) ArchiveValue() (jsonvalue.Value, error) {
	type nativeSummary TrajectorySummary
	data, err := json.Marshal(nativeSummary(s))
	if err != nil {
		return jsonvalue.Value{}, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil || s.Archive == nil {
		return value, err
	}
	fields := make([]jsonvalue.Field, 0, len(value.Keys()))
	for _, name := range value.Keys() {
		child, _ := value.Lookup(name)
		switch name {
		case "model":
			child = s.Archive.Model
		case "workspace":
			child = s.Archive.Workspace
		case "build":
			child = s.Archive.Build
		case "metrics":
			child = s.Archive.Metrics
		case "started_at":
			child = s.Archive.StartedAt
		case "ended_at":
			child = s.Archive.EndedAt
		case "duration_ms":
			child = s.Archive.DurationMS
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: child})
	}
	return jsonvalue.ObjectValue(fields), nil
}

func (s TrajectorySummary) MarshalJSON() ([]byte, error) {
	value, err := s.ArchiveValue()
	if err != nil {
		return nil, err
	}
	return value.MarshalUTF8()
}
