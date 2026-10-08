package sessionquery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type reconstructionEpoch struct {
	Epoch    ArchiveValue
	Messages []ArchiveValue
}
type reconstructionFixtureReader struct {
	events               []ArchiveValue
	epochs               []reconstructionEpoch
	id                   SessionID
	eventErr, messageErr error
	onEvents             func()
	onMessages           func()
	reads                int
}

func (reader *reconstructionFixtureReader) LoadRequestEvents(_ context.Context, id SessionID) ([]RequestEvent, error) {
	if id != reader.id {
		return nil, errors.New("foreign session read")
	}
	if reader.onEvents != nil {
		reader.onEvents()
	}
	if reader.eventErr != nil {
		return nil, reader.eventErr
	}
	events := make([]RequestEvent, len(reader.events))
	for index, raw := range reader.events {
		event, err := DecodeRequestEvent(raw)
		if err != nil {
			return nil, err
		}
		events[index] = event
	}
	return events, nil
}
func (reader *reconstructionFixtureReader) LoadRequestMessages(_ context.Context, id SessionID, epoch ArchiveValue) ([]ArchiveValue, error) {
	reader.reads++
	if id != reader.id {
		return nil, errors.New("foreign session read")
	}
	if reader.onMessages != nil {
		reader.onMessages()
	}
	if reader.messageErr != nil {
		return nil, reader.messageErr
	}
	for _, stored := range reader.epochs {
		if stored.Epoch.PythonEqual(epoch) {
			return stored.Messages, nil
		}
	}
	return []ArchiveValue{}, nil
}
func reconstructionJSON(t *testing.T, result Reconstruction) ArchiveValue {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestReconstructAllActualSourceHistoricalAndRuntimeRecipes(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Name   string
			Seq    int64
			Events []ArchiveValue
			Epochs []reconstructionEpoch
			Output ArchiveValue
			Error  *string
		}
		NullOutput ArchiveValue `json:"null_output"`
		Runtime    struct {
			Seq           int64
			Events        []ArchiveValue
			Epochs        []reconstructionEpoch
			Before, After ArchiveValue
		}
	}
	data, err := os.ReadFile("../testdata/python-request-reconstruction.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			reader := &reconstructionFixtureReader{id: SessionID(row.Name), events: row.Events, epochs: row.Epochs}
			result, err := Reconstruct(context.Background(), reader, reader.id, NewRequestSequence(row.Seq))
			if row.Error != nil {
				var fault ReconstructionFault
				if !errors.As(err, &fault) || string(fault.Class) != *row.Error || result.Kind() != ReconstructionInvalid || reader.reads != 0 {
					t.Fatal(result, err, reader.reads)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reconstructionJSON(t, result).PythonEqual(row.Output) {
				t.Fatal("Source projection differs", result)
			}
			if _, present := row.Output.Lookup("error"); present && reader.reads != 0 {
				t.Fatal("early notice read transcript")
			}
		})
	}
	reader := &reconstructionFixtureReader{id: "null"}
	result, err := Reconstruct(context.Background(), reader, reader.id, NewRequestSequence(10))
	if err != nil || !reconstructionJSON(t, result).PythonEqual(fixture.NullOutput) {
		t.Fatal(result, err)
	}
	runtime := fixture.Runtime
	reader = &reconstructionFixtureReader{id: "runtime", events: runtime.Events, epochs: runtime.Epochs}
	result, err = Reconstruct(context.Background(), reader, reader.id, NewRequestSequence(runtime.Seq))
	if err != nil || !reconstructionJSON(t, result).PythonEqual(runtime.Before) || !reconstructionJSON(t, result).PythonEqual(runtime.After) {
		t.Fatal(result, err)
	}
	// Projection containers cannot mutate the sealed result or the backing rows.
	before := reconstructionJSON(t, result)
	projection, ok := result.Request()
	if !ok || len(projection.Messages) == 0 || projection.Capability == nil {
		t.Fatal("missing runtime projection")
	}
	projection.Messages[0] = jsonvalue.NullValue()
	projection.Capability.PermissionMode = jsonvalue.TextValue("auto")
	if !reconstructionJSON(t, result).PythonEqual(before) {
		t.Fatal("projection aliases result")
	}
}

func TestReconstructionCancellationFaultsAndSequenceBoundary(t *testing.T) {
	start, err := jsonvalue.Decode(`{"seq":1,"type":"model_start","transcript_epoch":1,"message_count":1}`)
	if err != nil {
		t.Fatal(err)
	}
	reader := &reconstructionFixtureReader{id: "own", events: []ArchiveValue{start}}
	failure := errors.New("read failed")
	reader.eventErr = failure
	if _, err := Reconstruct(context.Background(), reader, reader.id, NewRequestSequence(1)); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	reader.eventErr = nil
	reader.messageErr = failure
	if _, err := Reconstruct(context.Background(), reader, reader.id, NewRequestSequence(1)); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	reader.messageErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	reader.onEvents = cancel
	reader.reads = 0
	if _, err := Reconstruct(ctx, reader, reader.id, NewRequestSequence(1)); !errors.Is(err, context.Canceled) || reader.reads != 0 {
		t.Fatal(err, reader.reads)
	}
	reader.onEvents = nil
	ctx, cancel = context.WithCancel(context.Background())
	reader.onMessages = cancel
	if _, err := Reconstruct(ctx, reader, reader.id, NewRequestSequence(1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	reader.onMessages = nil
	if _, err := Reconstruct(context.Background(), reader, "foreign", NewRequestSequence(1)); err == nil {
		t.Fatal("foreign read accepted by backing")
	}
	if _, err := Reconstruct(context.Background(), nil, "own", NewRequestSequence(1)); err == nil {
		t.Fatal("missing reader")
	}
	for _, raw := range []string{`1.0`, `null`, `true`, `"1"`} {
		if _, err := ParseRequestSequence(raw); err == nil {
			t.Fatal("noninteger sequence", raw)
		}
	}
	large, err := ParseRequestSequence("99999999999999999999999999999999999999")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Reconstruct(context.Background(), reader, reader.id, large)
	if err != nil || result.Kind() != ReconstructionMissingStart {
		t.Fatal(result, err)
	}
}
