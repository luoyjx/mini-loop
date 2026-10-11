package sessionquery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type fixtureEpoch struct {
	Epoch    Epoch    `json:"epoch"`
	Messages []string `json:"messages_json"`
}
type fixtureReader struct {
	id      SessionID
	epochs  []fixtureEpoch
	current Epoch
	loads   []Epoch
	loadErr error
	onLoad  func()
}

func (reader *fixtureReader) TranscriptEpoch(context.Context, SessionID) (Epoch, error) {
	return reader.current, nil
}
func (reader *fixtureReader) LoadMessages(_ context.Context, id SessionID, epoch Epoch) ([]Message, error) {
	reader.loads = append(reader.loads, epoch)
	if reader.onLoad != nil {
		reader.onLoad()
	}
	if reader.loadErr != nil {
		return nil, reader.loadErr
	}
	if id != reader.id {
		return nil, errors.New("foreign session read")
	}
	for _, stored := range reader.epochs {
		if stored.Epoch != epoch {
			continue
		}
		messages := make([]Message, len(stored.Messages))
		for index, raw := range stored.Messages {
			row, err := jsonvalue.Decode(raw)
			if err != nil {
				return nil, err
			}
			role := jsonvalue.TextValue("?")
			if storedRole, present := row.Lookup("role"); present {
				role = storedRole
			}
			content, _ := row.Lookup("content")
			messages[index] = Message{role, content}
		}
		return messages, nil
	}
	return nil, nil
}

func TestSearchAndToolRenderingMatchActualPython(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Name   SessionID      `json:"name"`
			Epochs []fixtureEpoch `json:"epochs"`
			Query  string         `json:"query"`
			Search Result         `json:"search"`
			Output string         `json:"output"`
		} `json:"rows"`
		Surfaces []struct{ Name, Output string } `json:"surfaces"`
	}
	data, err := os.ReadFile("../testdata/python-transcript-search.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(string(row.Name), func(t *testing.T) {
			reader := &fixtureReader{id: row.Name, epochs: row.Epochs}
			for _, epoch := range row.Epochs {
				reader.current = max(reader.current, epoch.Epoch)
			}
			// ToolSearch trims; Search's direct API retains empty-needle behavior.
			result, err := Search(context.Background(), reader, row.Name, pytext.Strip(row.Query))
			if err != nil || !reflect.DeepEqual(result, row.Search) {
				t.Fatalf("result %+v want %+v: %v", result, row.Search, err)
			}
			output, err := ToolSearch(context.Background(), reader, row.Name, row.Query)
			if err != nil || output != row.Output {
				t.Fatalf("output %q want %q: %v", output, row.Output, err)
			}
		})
	}
	for _, row := range fixture.Surfaces {
		var reader Reader
		id := SessionID("own")
		if row.Name == "null-store" || row.Name == "no-id" {
			reader = &fixtureReader{id: id}
		}
		if row.Name == "no-id" {
			id = ""
		}
		output, err := ToolSearch(context.Background(), reader, id, "needle")
		if err != nil || output != row.Output {
			t.Fatal(row.Name, output, err)
		}
	}
}

func TestSearchBoundsCancellationAndReadFailure(t *testing.T) {
	reader := &fixtureReader{id: "own", current: 1000}
	result, err := Search(context.Background(), reader, "own", "needle")
	if err != nil || result.FirstEpoch != 981 || len(reader.loads) != MaxEpochsScanned || reader.loads[19] != 1000 {
		t.Fatal(result, reader.loads, err)
	}
	_, err = Search(context.Background(), reader, "foreign", "needle")
	if err == nil {
		t.Fatal("foreign selection accepted")
	}
	readFailure := errors.New("read failure")
	reader.loadErr = readFailure
	if _, err := Search(context.Background(), reader, "own", "needle"); !errors.Is(err, readFailure) {
		t.Fatal(err)
	}
	reader.loadErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	reader.onLoad = cancel
	reader.loads = nil
	if _, err := Search(ctx, reader, "own", "needle"); !errors.Is(err, context.Canceled) || len(reader.loads) != 1 {
		t.Fatal(reader.loads, err)
	}
	reader.loads = nil
	if _, err := Search(ctx, reader, "own", "needle"); !errors.Is(err, context.Canceled) || len(reader.loads) != 0 {
		t.Fatal(reader.loads, err)
	}
	if _, err := Search(context.Background(), nil, "own", "needle"); err == nil {
		t.Fatal("missing reader accepted")
	}
}
