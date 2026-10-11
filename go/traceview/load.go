package traceview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/trajectory"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// AssembleFile reads an operator-selected export. Missing ends are interrupted;
// blank lines are ignored, malformed JSON marks partial, and the last end wins.
func AssembleFile(ctx context.Context, path string) (Ledger, error) {
	file, err := os.Open(path)
	if err != nil {
		return Ledger{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), trajectory.MaxRecordBytes+1)
	var start, end json.RawMessage
	events := make([]json.RawMessage, 0)
	partial := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return Ledger{}, err
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if !utf8.Valid(line) {
			return Ledger{}, ErrDocument
		}
		value, decodeErr := jsonvalue.Decode(string(line))
		if decodeErr != nil {
			partial = true
			continue
		}
		var record struct {
			RecordType string `json:"record_type"`
		}
		kind, _ := value.Lookup("record_type")
		projection, err := kind.MarshalJSON()
		if err != nil || json.Unmarshal(projection, &record.RecordType) != nil {
			return Ledger{}, ErrDocument
		}
		if start == nil {
			if record.RecordType != "trajectory_start" {
				return Ledger{}, ErrDocument
			}
			start = append(json.RawMessage(nil), line...)
		}
		if record.RecordType == "event" {
			events = append(events, append(json.RawMessage(nil), line...))
		}
		if record.RecordType == "trajectory_end" {
			end = append(json.RawMessage(nil), line...)
		}
	}
	if err := scanner.Err(); err != nil {
		return Ledger{}, err
	}
	if start == nil {
		return Ledger{}, ErrDocument
	}
	startValue, err := jsonvalue.Decode(string(start))
	if err != nil {
		return Ledger{}, err
	}
	endValue := jsonvalue.NullValue()
	if end != nil {
		endValue, err = jsonvalue.Decode(string(end))
		if err != nil {
			return Ledger{}, err
		}
	}
	lookup := func(value jsonvalue.Value, name string) jsonvalue.Value {
		child, _ := value.Lookup(name)
		return child
	}
	id, present := startValue.Lookup("trajectory_id")
	if !present {
		id = jsonvalue.TextValue(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	}
	status := jsonvalue.TextValue("interrupted")
	if end != nil {
		status, present = endValue.Lookup("status")
		if !present {
			status = jsonvalue.TextValue("completed")
		}
	}
	decodedEvents := make([]jsonvalue.Value, 0, len(events))
	for _, event := range events {
		v, err := jsonvalue.Decode(string(event))
		if err != nil {
			return Ledger{}, err
		}
		decodedEvents = append(decodedEvents, v)
	}
	document, err := jsonvalue.AppendLegacy(nil, jsonvalue.ObjectValue([]jsonvalue.Field{
		{Name: "trajectory_id", Value: id},
		{Name: "session", Value: lookup(startValue, "session")},
		{Name: "run_index", Value: lookup(startValue, "run_index")},
		{Name: "started_at", Value: lookup(startValue, "started_at")},
		{Name: "input", Value: lookup(startValue, "input")},
		{Name: "status", Value: status},
		{Name: "ended_at", Value: lookup(endValue, "ended_at")},
		{Name: "duration_ms", Value: lookup(endValue, "duration_ms")},
		{Name: "output", Value: lookup(endValue, "output")},
		{Name: "error", Value: lookup(endValue, "error")},
		{Name: "metrics", Value: lookup(endValue, "metrics")},
		{Name: "events", Value: jsonvalue.ArrayValue(decodedEvents)},
		{Name: "partial", Value: jsonvalue.BoolValue(partial || end == nil)},
	}))
	if err != nil {
		return Ledger{}, err
	}
	return Build(document)
}

// Load accepts an export path, trajectory ID or session ID. Store reads are an
// operator capability; the HTTP route performs its own recorded-owner admission.
func Load(ctx context.Context, target, root string) ([]Ledger, error) {
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
		ledger, err := AssembleFile(ctx, target)
		if err != nil {
			return nil, err
		}
		return []Ledger{ledger}, nil
	}
	store, err := trajectory.New(trajectory.Config{Root: root, CaptureContent: true})
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(target, "traj_") {
		data, err := store.JSON(agent.TrajectoryID(target), 1<<63-1)
		if err != nil {
			return nil, err
		}
		ledger, err := Build(data)
		if err != nil {
			return nil, err
		}
		return []Ledger{ledger}, nil
	}
	session := agent.SessionID(target)
	rows, err := store.List(agent.TrajectoryQuery{Session: &session, Limit: 500})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no trajectory file and no recorded session '%s' under %s", target, root)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].StartedAt < rows[j].StartedAt })
	ledgers := make([]Ledger, 0, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := store.JSON(row.ID, 1<<63-1)
		if err != nil {
			return nil, err
		}
		ledger, err := Build(data)
		if err != nil {
			return nil, err
		}
		ledgers = append(ledgers, ledger)
	}
	return ledgers, nil
}
