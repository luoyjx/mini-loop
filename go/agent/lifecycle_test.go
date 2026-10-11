package agent

import "testing"

func eventRecordsOfKind(records []SessionEventRecord, kind SessionEventKind) []SessionEventRecord {
	result := []SessionEventRecord{}
	for _, record := range records {
		if record.Event.Kind() == kind {
			result = append(result, record)
		}
	}
	return result
}

func assertToolResultTelemetry(t *testing.T, session *Session, id string, failed, denied bool) {
	t.Helper()
	for _, record := range session.Events() {
		if result, ok := record.Event.ToolResult(); ok && result.ID == id {
			if result.Failed != failed || result.Denied != denied {
				t.Fatalf("tool flags: %+v", result)
			}
			return
		}
	}
	t.Fatalf("no tool_result telemetry for %s", id)
}
