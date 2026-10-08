// Package trajectory implements private append-only per-run JSONL evidence.
// This is file evidence, not a session restart store or a cross-process lease.
package trajectory

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/workspace"
)

const MaxRecordBytes = 64 * 1024 * 1024

var ErrMissing = errors.New("trajectory not found")
var ErrInvalid = errors.New("invalid trajectory")
var ErrTooLarge = errors.New("trajectory exceeds JSON size limit")
var idPattern = regexp.MustCompile(`^traj_[0-9a-f]{24}$`)

type Config struct {
	Root           string
	CaptureContent bool
}
type Store struct {
	root           string
	capture        bool
	mu             sync.Mutex
	active         map[agent.TrajectoryID]bool
	locks          [32]sync.Mutex
	problems       []Problem
	problemCount   uint64
	problemDropped uint64
	now            func() float64
	newID          func() (agent.TrajectoryID, error)
}
type header struct {
	RecordType string                   `json:"record_type"`
	Schema     string                   `json:"schema_version"`
	ID         agent.TrajectoryID       `json:"trajectory_id"`
	Trace      agent.TrajectoryID       `json:"trace_id"`
	Group      agent.SessionID          `json:"group_id"`
	Session    agent.SessionID          `json:"session"`
	Owner      *agent.OwnerID           `json:"owner"`
	RunIndex   int                      `json:"run_index"`
	StartedAt  float64                  `json:"started_at"`
	Input      *string                  `json:"input"`
	Metadata   agent.TrajectoryMetadata `json:"metadata"`
}
type terminal struct {
	RecordType string                   `json:"record_type"`
	ID         agent.TrajectoryID       `json:"trajectory_id"`
	Trace      agent.TrajectoryID       `json:"trace_id"`
	Group      agent.SessionID          `json:"group_id"`
	Session    agent.SessionID          `json:"session"`
	Owner      *agent.OwnerID           `json:"owner"`
	Status     agent.TrajectoryStatus   `json:"status"`
	EndedAt    *float64                 `json:"ended_at"`
	DurationMS *float64                 `json:"duration_ms"`
	Output     *string                  `json:"output"`
	Error      *string                  `json:"error"`
	Metrics    *agent.TrajectoryMetrics `json:"metrics"`
}

func New(config Config) (*Store, error) {
	root, err := workspace.ResolvePath(config.Root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Store{root: root, capture: config.CaptureContent, active: make(map[agent.TrajectoryID]bool), now: func() float64 { return float64(time.Now().UnixMicro()) / 1e6 }, newID: randomID}, nil
}
func randomID() (agent.TrajectoryID, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	data[6] = (data[6] & 15) | 64
	data[8] = (data[8] & 63) | 128
	return agent.TrajectoryID("traj_" + hex.EncodeToString(data[:])[:24]), nil
}
func (s *Store) Root() string { return s.root }
func (s *Store) path(id agent.TrajectoryID) (string, error) {
	if !idPattern.MatchString(string(id)) {
		return "", ErrInvalid
	}
	return filepath.Join(s.root, string(id)+".jsonl"), nil
}
func (s *Store) lock(id agent.TrajectoryID) *sync.Mutex {
	hash := fnv.New32a()
	hash.Write([]byte(id))
	return &s.locks[hash.Sum32()%32]
}

type Problem struct {
	Message string
	Count   uint64
}

func (s *Store) report(problem string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problemCount++
	for i := range s.problems {
		if s.problems[i].Message == problem {
			s.problems[i].Count++
			return
		}
	}
	if len(s.problems) == 50 {
		copy(s.problems, s.problems[1:])
		s.problems = s.problems[:49]
		s.problemDropped++
	}
	s.problems = append(s.problems, Problem{Message: problem, Count: 1})
}
func (s *Store) Problems() ([]string, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	messages := make([]string, len(s.problems))
	for i, problem := range s.problems {
		messages[i] = problem.Message
	}
	return messages, s.problemCount
}

// ProblemDiagnostics returns retained distinct messages, lifetime occurrences and evictions.
func (s *Store) ProblemDiagnostics() ([]Problem, uint64, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Problem(nil), s.problems...), s.problemCount, s.problemDropped
}
func (s *Store) write(id agent.TrajectoryID, payload []byte) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if !utf8.Valid(payload) {
		return ErrInvalid
	}
	value, err := jsonvalue.Decode(string(payload))
	if err != nil {
		return ErrInvalid
	}
	if !s.capture {
		value = protectValue(value, "")
	}
	payload, err = value.MarshalLegacyUTF8()
	if err != nil {
		return err
	}
	if len(payload) > MaxRecordBytes {
		return fmt.Errorf("record exceeds %d bytes", MaxRecordBytes)
	}
	lock := s.lock(id)
	lock.Lock()
	defer lock.Unlock()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	line := append(payload, '\n')
	n, err := file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return err
	}
	return file.Close()
}
func (s *Store) Start(start agent.TrajectoryStart) (agent.TrajectoryID, error) {
	id, err := s.newID()
	if err != nil {
		return "", err
	}
	owner := start.Owner
	input := start.Input
	payload, err := json.Marshal(header{"trajectory_start", agent.TrajectorySchema, id, id, start.Session, start.Session, &owner, start.RunIndex, s.now(), &input, start.Metadata})
	if err != nil {
		return "", err
	}
	if err = s.write(id, payload); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.active[id] = true
	s.mu.Unlock()
	return id, nil
}
func (s *Store) Append(id agent.TrajectoryID, event agent.TrajectoryRecord) error {
	payload, err := event.MarshalArchiveJSON()
	if err != nil {
		return err
	}
	return s.write(id, payload)
}
func (s *Store) Finish(id agent.TrajectoryID, finish agent.TrajectoryFinish) error {
	start, _, counts, _, err := s.scan(id, nil)
	if err != nil {
		return err
	}
	defer func() { s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }()
	now := s.now()
	duration := finish.DurationMS
	if duration != nil {
		// Fixed decimal formatting rounds the original binary float, including
		// negative values and ties, like Python round(value, 3).
		rounded, err := strconv.ParseFloat(strconv.FormatFloat(*duration, 'f', 3, 64), 64)
		if err != nil {
			return err
		}
		duration = &rounded
	}
	payload, err := json.Marshal(terminal{"trajectory_end", id, id, start.Group, start.Session, start.Owner, finish.Status, &now, duration, finish.Output, finish.Error, &counts})
	if err != nil {
		return err
	}
	return s.write(id, payload)
}

// The scanner retains one capped record plus header/terminal. Body readers run
// under this process's stripe lock; streaming export never holds it for clients.
func (s *Store) scan(id agent.TrajectoryID, visit func([]byte, string) error) (header, *terminal, agent.TrajectoryMetrics, bool, error) {
	return s.scanBounded(id, visit, -1)
}

func (s *Store) scanBounded(id agent.TrajectoryID, visit func([]byte, string) error, limit int64) (header, *terminal, agent.TrajectoryMetrics, bool, error) {
	var start header
	var end *terminal
	var counts agent.TrajectoryMetrics
	partial := false
	path, err := s.path(id)
	if err != nil {
		return start, end, counts, partial, err
	}
	lock := s.lock(id)
	lock.Lock()
	defer lock.Unlock()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return start, end, counts, partial, ErrMissing
	}
	if err != nil {
		return start, end, counts, partial, err
	}
	if !info.Mode().IsRegular() {
		return start, end, counts, partial, ErrMissing
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return start, end, counts, partial, ErrMissing
	}
	if err != nil {
		return start, end, counts, partial, err
	}
	defer file.Close()
	var input io.Reader = file
	var bounded *io.LimitedReader
	// Reserve one byte to detect growth without overflowing an unlimited int64 cap.
	if limit >= 0 && limit < 1<<63-1 {
		bounded = &io.LimitedReader{R: file, N: limit + 1}
		input = bounded
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), MaxRecordBytes+1)
	found := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if !utf8.Valid(line) {
			return start, end, counts, partial, ErrInvalid
		}
		classification, decodeErr := scanEnvelope(line)
		if decodeErr != nil {
			if errors.Is(decodeErr, ErrInvalid) {
				return start, end, counts, partial, ErrInvalid
			}
			partial = true
			continue
		}
		// Concrete wire projection for classification; arbitrary JSON is local only.
		var envelope struct {
			RecordType string          `json:"record_type"`
			Type       string          `json:"type"`
			Error      json.RawMessage `json:"error"`
			Denied     bool            `json:"denied"`
		}
		if err = json.Unmarshal(classification, &envelope); err != nil {
			return start, end, counts, partial, ErrInvalid
		}
		if !found {
			if envelope.RecordType != "trajectory_start" {
				return start, end, counts, partial, ErrInvalid
			}
			if err = json.Unmarshal(line, &start); err != nil {
				return start, end, counts, partial, ErrInvalid
			}
			var members map[string]json.RawMessage
			json.Unmarshal(line, &members)
			if _, ok := members["owner"]; !ok {
				owner := agent.OwnerID("anonymous")
				start.Owner = &owner
			}
			if start.Trace == "" {
				start.Trace = id
			}
			if start.Group == "" {
				start.Group = start.Session
			}
			if start.Schema == "" {
				start.Schema = agent.TrajectorySchema
			}
			found = true
		} else if envelope.RecordType == "trajectory_end" {
			var value terminal
			if err = json.Unmarshal(line, &value); err != nil {
				return start, end, counts, partial, ErrInvalid
			}
			if value.Status == "" {
				value.Status = agent.TrajectoryCompleted
			}
			end = &value
		} else if envelope.RecordType == "event" {
			counts.EventCount++
			switch envelope.Type {
			case "model_start":
				counts.ModelCalls++
			case "tool_use":
				counts.ToolCalls++
			case "error":
				counts.Errors++
			case "tool_result":
				if envelope.Denied || truth(envelope.Error) {
					counts.ToolErrors++
				}
			}
		}
		if visit != nil {
			if err = visit(line, envelope.RecordType); err != nil {
				return start, end, counts, partial, err
			}
		}
	}
	if bounded != nil && bounded.N == 0 {
		return start, end, counts, partial, ErrTooLarge
	}
	if err = scanner.Err(); err != nil {
		return start, end, counts, partial, err
	}
	if !found {
		return start, end, counts, partial, ErrInvalid
	}
	return start, end, counts, partial, nil
}
func truth(raw []byte) bool {
	value := strings.TrimSpace(string(raw))
	switch value {
	case "", "null", "false", "0", "0.0", `""`, "[]", "{}":
		return false
	}
	return true
}
func (s *Store) summary(id agent.TrajectoryID, start header, end *terminal, counts agent.TrajectoryMetrics, partial bool) agent.TrajectorySummary {
	s.mu.Lock()
	active := s.active[id]
	s.mu.Unlock()
	status := agent.TrajectoryInterrupted
	if active {
		status = agent.TrajectoryRunning
	}
	var ended, duration *float64
	if end != nil {
		status = end.Status
		ended = end.EndedAt
		duration = end.DurationMS
		if end.Metrics != nil {
			counts = *end.Metrics
		}
	}
	preview := start.Input
	if preview != nil {
		runes := []rune(*preview)
		if len(runes) > 160 {
			text := string(runes[:159]) + "…"
			preview = &text
		}
	}
	return agent.TrajectorySummary{ID: id, TrajectoryID: id, TraceID: start.Trace, GroupID: start.Group, Session: start.Session, Owner: start.Owner, RunIndex: start.RunIndex, Status: status, StartedAt: start.StartedAt, EndedAt: ended, DurationMS: duration, Metrics: counts, Partial: partial || end == nil, InputPreview: preview, Model: start.Metadata.Model, Workspace: start.Metadata.Workspace, Build: start.Metadata.Build}
}
func (s *Store) Summary(id agent.TrajectoryID) (agent.TrajectorySummary, error) {
	start, end, counts, partial, err := s.scan(id, nil)
	if err != nil {
		return agent.TrajectorySummary{}, err
	}
	return s.summary(id, start, end, counts, partial), nil
}
func (s *Store) List(query agent.TrajectoryQuery) ([]agent.TrajectorySummary, error) {
	paths, err := filepath.Glob(filepath.Join(s.root, "traj_*.jsonl"))
	if err != nil {
		return nil, err
	}
	summaries := make([]agent.TrajectorySummary, 0)
	for _, path := range paths {
		id := agent.TrajectoryID(strings.TrimSuffix(filepath.Base(path), ".jsonl"))
		row, err := s.Summary(id)
		if errors.Is(err, ErrMissing) {
			continue
		}
		if err != nil {
			s.report(filepath.Base(path) + ": unreadable; dropped from the trajectory listing")
			continue
		}
		if query.Session == nil || row.Session == *query.Session {
			summaries = append(summaries, row)
		}
	}
	sort.SliceStable(summaries, func(i, j int) bool { return summaries[i].StartedAt > summaries[j].StartedAt })
	limit := query.Limit
	if limit < 0 {
		limit = 0
	}
	if len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return summaries, nil
}
func (s *Store) Count(session agent.SessionID) (int, error) {
	rows, err := s.List(agent.TrajectoryQuery{Session: &session, Limit: 1_000_000})
	return len(rows), err
}
func (s *Store) ByteSize(id agent.TrajectoryID) (int64, error) {
	path, err := s.path(id)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0, ErrMissing
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, ErrInvalid
	}
	return info.Size(), nil
}
func (s *Store) JSON(id agent.TrajectoryID, limit int64) ([]byte, error) {
	size, err := s.ByteSize(id)
	if err != nil {
		return nil, err
	}
	if limit < 0 || size > limit {
		return nil, ErrTooLarge
	}
	metadataJSON := json.RawMessage(`{}`)
	events := make([]jsonvalue.Value, 0)
	seen := int64(0)
	start, end, counts, partial, err := s.scanBounded(id, func(line []byte, kind string) error {
		seen += int64(len(line) + 1)
		if seen > limit {
			return ErrTooLarge
		}
		if kind == "trajectory_start" {
			var members map[string]json.RawMessage
			if json.Unmarshal(line, &members) == nil {
				if value, ok := members["metadata"]; ok {
					metadataJSON = append(json.RawMessage(nil), value...)
				}
			}
		}
		if kind == "event" {
			value, err := jsonvalue.Decode(string(line))
			if err != nil {
				return err
			}
			events = append(events, value)
		}
		return nil
	}, limit)
	if err != nil {
		return nil, err
	}
	summary := s.summary(id, start, end, counts, partial)
	var output, detail *string
	if end != nil {
		output = end.Output
		detail = end.Error
	}
	// This DTO only encodes an HTTP/file JSON document, never live event state.
	encoded, err := json.Marshal(struct {
		Schema       string                  `json:"schema_version"`
		ID           agent.TrajectoryID      `json:"id"`
		TrajectoryID agent.TrajectoryID      `json:"trajectory_id"`
		Trace        agent.TrajectoryID      `json:"trace_id"`
		Group        agent.SessionID         `json:"group_id"`
		Session      agent.SessionID         `json:"session"`
		Owner        *agent.OwnerID          `json:"owner"`
		RunIndex     int                     `json:"run_index"`
		Status       agent.TrajectoryStatus  `json:"status"`
		Started      float64                 `json:"started_at"`
		Ended        *float64                `json:"ended_at"`
		Duration     *float64                `json:"duration_ms"`
		Input        *string                 `json:"input"`
		Output       *string                 `json:"output"`
		Error        *string                 `json:"error"`
		Metadata     json.RawMessage         `json:"metadata"`
		Metrics      agent.TrajectoryMetrics `json:"metrics"`
		Events       []jsonvalue.Value       `json:"events"`
		Partial      bool                    `json:"partial"`
	}{start.Schema, id, id, start.Trace, start.Group, start.Session, start.Owner, start.RunIndex, summary.Status, start.StartedAt, summary.EndedAt, summary.DurationMS, start.Input, output, detail, metadataJSON, summary.Metrics, []jsonvalue.Value{}, summary.Partial})
	if err != nil {
		return nil, err
	}
	document, err := jsonvalue.Decode(string(encoded))
	if err != nil {
		return nil, err
	}
	fields := make([]jsonvalue.Field, 0, len(document.Keys()))
	for _, name := range document.Keys() {
		child, _ := document.Lookup(name)
		if name == "events" {
			child = jsonvalue.ArrayValue(events)
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: child})
	}
	return jsonvalue.ObjectValue(fields).MarshalLegacyUTF8()
}
func (s *Store) Stream(ctx context.Context, id agent.TrajectoryID, out io.Writer) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return ErrMissing
	}
	if err != nil {
		return err
	}
	defer file.Close()
	buffer := make([]byte, 65536)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			written, err := out.Write(buffer[:n])
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
func (s *Store) DeleteForSession(session agent.SessionID) (int, error) {
	paths, err := filepath.Glob(filepath.Join(s.root, "traj_*.jsonl"))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			s.report(filepath.Base(path) + ": header unreadable; left in place")
			continue
		}
		reader := bufio.NewScanner(file)
		reader.Buffer(make([]byte, 4096), MaxRecordBytes+1)
		read := reader.Scan()
		line := append([]byte(nil), reader.Bytes()...)
		err = reader.Err()
		file.Close()
		var value struct {
			Session *agent.SessionID `json:"session"`
		}
		if !read || err != nil || !utf8.Valid(line) || len(bytes.TrimSpace(line)) == 0 || bytes.TrimSpace(line)[0] != '{' || json.Unmarshal(line, &value) != nil {
			s.report(filepath.Base(path) + ": header unreadable; left in place")
			continue
		}
		if value.Session == nil || *value.Session != session {
			continue
		}
		id := agent.TrajectoryID(strings.TrimSuffix(filepath.Base(path), ".jsonl"))
		lock := s.lock(id)
		lock.Lock()
		err = os.Remove(path)
		lock.Unlock()
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			s.report(filepath.Base(path) + ": deletion failed; recording retained")
			continue
		}
		s.mu.Lock()
		delete(s.active, id)
		s.mu.Unlock()
		removed++
	}
	return removed, nil
}
