package traceview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/agent"
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
		if !json.Valid(line) {
			partial = true
			continue
		}
		var record struct {
			RecordType string `json:"record_type"`
		}
		if json.Unmarshal(line, &record) != nil {
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
	var header struct {
		ID        *agent.TrajectoryID `json:"trajectory_id"`
		Session   agent.SessionID
		RunIndex  int      `json:"run_index"`
		StartedAt *float64 `json:"started_at"`
		Input     *string
	}
	var terminal struct {
		Status        *agent.TrajectoryStatus
		EndedAt       *float64 `json:"ended_at"`
		DurationMS    *float64 `json:"duration_ms"`
		Output, Error *string
		Metrics       Metrics
	}
	if json.Unmarshal(start, &header) != nil {
		return Ledger{}, ErrDocument
	}
	if end != nil && json.Unmarshal(end, &terminal) != nil {
		return Ledger{}, ErrDocument
	}
	status := agent.TrajectoryInterrupted
	if end != nil {
		status = agent.TrajectoryCompleted
		if terminal.Status != nil {
			status = *terminal.Status
		}
	}
	id := agent.TrajectoryID("")
	if header.ID != nil {
		id = *header.ID
	} else {
		var members map[string]json.RawMessage
		json.Unmarshal(start, &members)
		if _, ok := members["trajectory_id"]; !ok {
			id = agent.TrajectoryID(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		}
	}
	document, err := json.Marshal(struct {
		ID                   agent.TrajectoryID     `json:"trajectory_id"`
		Session              agent.SessionID        `json:"session"`
		RunIndex             int                    `json:"run_index"`
		StartedAt            *float64               `json:"started_at"`
		EndedAt              *float64               `json:"ended_at"`
		DurationMS           *float64               `json:"duration_ms"`
		Status               agent.TrajectoryStatus `json:"status"`
		Input, Output, Error *string
		Metrics              Metrics
		Events               []json.RawMessage
		Partial              bool
	}{id, header.Session, header.RunIndex, header.StartedAt, terminal.EndedAt, terminal.DurationMS, status, header.Input, terminal.Output, terminal.Error, terminal.Metrics, events, partial || end == nil})
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
