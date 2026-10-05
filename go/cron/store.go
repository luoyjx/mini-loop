package cron

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxStoreBytes = 8 << 20

func claimsPath(path string) string { return strings.TrimSuffix(path, filepath.Ext(path)) + ".claims" }
func claimName(id ID, marker string) string {
	var b strings.Builder
	for _, r := range string(id) + "." + marker {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}
func (s *Scheduler) claim(id ID, marker string) (bool, error) {
	if s.claims == "" {
		return true, nil
	}
	if err := os.MkdirAll(s.claims, 0700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(s.claims, claimName(id, marker)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, f.Close()
}
func (s *Scheduler) unclaim(id ID, marker string) {
	if s.claims != "" && marker != "" {
		_ = os.Remove(filepath.Join(s.claims, claimName(id, marker)))
	}
}

// save is called while state is locked. Failure preserves source's already
// mutated memory and occurrence claim; a lost occurrence is never dispatched.
func (s *Scheduler) save() (err error) {
	if s.path == "" {
		return nil
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("cron persistence callback panicked (%v)", p)
		}
	}()
	jobs := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		if !j.Durable {
			continue
		}
		if s.secrets != nil {
			masked := s.secrets.Mask(j.Prompt)
			if masked != j.Prompt {
				s.problem(fmt.Sprintf("%s: prompt held a registered secret; the stored copy is masked and will fire masked after a restart", j.ID))
				j.Prompt = masked
			}
		}
		jobs = append(jobs, j)
	}
	payload, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	if len(payload) > maxStoreBytes {
		return errors.New("cron store exceeds eight MiB")
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err = f.Write(payload); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// wireJob is a concrete boundary DTO: required strings have presence bits and
// optional fields retain dataclass defaults. Raw JSON exists only while reading
// this array and never enters scheduler state or invocation payloads.
type wireJob struct {
	ID        *ID        `json:"id"`
	Cron      *string    `json:"cron"`
	Prompt    *string    `json:"prompt"`
	Session   *SessionID `json:"session_id"`
	Recurring *bool      `json:"recurring"`
	Durable   *bool      `json:"durable"`
	LastFired *string    `json:"last_fired"`
}

func decodeJob(raw []byte) (Job, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var w wireJob
	if err := d.Decode(&w); err != nil {
		return Job{}, err
	}
	if w.ID == nil || w.Cron == nil || w.Prompt == nil || w.Session == nil {
		return Job{}, errors.New("missing or null required job field")
	}
	j := Job{ID: *w.ID, Cron: *w.Cron, Prompt: *w.Prompt, Session: *w.Session, Recurring: true}
	if w.Recurring != nil {
		j.Recurring = *w.Recurring
	}
	if w.Durable != nil {
		j.Durable = *w.Durable
	}
	if w.LastFired != nil {
		j.LastFired = *w.LastFired
	}
	return j, nil
}
func (s *Scheduler) load() {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.problem(fmt.Sprintf("%s: unreadable (%v); all durable jobs were dropped", s.path, err))
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxStoreBytes+1))
	if err == nil && len(b) > maxStoreBytes {
		err = errors.New("cron store exceeds eight MiB")
	}
	var rows []json.RawMessage
	if err == nil && len(bytes.TrimSpace(b)) > 0 && bytes.TrimSpace(b)[0] != '[' {
		if json.Valid(b) {
			return
		}
		err = errors.New("invalid JSON")
	}
	if err == nil {
		err = json.Unmarshal(b, &rows)
	}
	if err != nil {
		s.problem(fmt.Sprintf("%s: unreadable (%v); all durable jobs were dropped", s.path, err))
		return
	}
	for _, row := range rows {
		j, err := decodeJob(row)
		if err != nil {
			s.problem(fmt.Sprintf("a stored job was unreadable: %v", err))
			continue
		}
		if _, err := Parse(j.Cron); err != nil {
			s.problem(fmt.Sprintf("%s: dropped, %s is not a valid cron expression", j.ID, pythonRepr(j.Cron)))
			continue
		}
		if n := s.index(j.ID, nil); n >= 0 {
			s.jobs[n] = j
		} else {
			s.jobs = append(s.jobs, j)
		}
	}
}
