package agent

import "github.com/luoyjx/mini-loop/go/internal/jsonsyntax"

// Caller selection is native-only metadata. It neither comes from a model
// reply nor appears in persisted run snapshots or delegated peer provenance.
type memorySelectionScope uint8

const (
	memorySelectionRun memorySelectionScope = iota
	memorySelectionManaged
	memorySelectionHTTPPost
	memorySelectionHTTPStream
	memorySelectionDirect
)

func (scope memorySelectionScope) limits() jsonsyntax.Limits {
	containers := 980
	switch scope {
	case memorySelectionManaged:
		containers = 978
	case memorySelectionHTTPPost:
		containers = 972
	case memorySelectionHTTPStream:
		containers = 976
	case memorySelectionDirect:
		containers = 982
	}
	return jsonsyntax.Limits{Containers: containers, IntegerDigits: 4300}
}
