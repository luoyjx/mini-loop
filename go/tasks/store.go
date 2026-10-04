// Package tasks implements the persistent workspace task graph. A claim marker
// arbitrates between processes; it does not provide leases or general file locks.
package tasks

import (
	"bytes"
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
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type ID string
type Owner string
type Status string

const (
	Pending        Status = "pending"
	InProgress     Status = "in_progress"
	Completed      Status = "completed"
	MaxField              = 16000
	MaxRows               = 50
	MaxSubject            = 200
	MaxRecordBytes        = 64 * 1024 * 1024
	MaxProblems           = 50
)

type Task struct {
	ID          ID      `json:"id"`
	Subject     string  `json:"subject"`
	Description string  `json:"description"`
	Status      Status  `json:"status"`
	Owner       *Owner  `json:"owner"`
	BlockedBy   []ID    `json:"blockedBy"`
	Worktree    *string `json:"worktree"`
}

type Masker interface{ MaskText(string) string }
type Config struct {
	Workspace string
	Secrets   Masker
}
type Problem struct {
	Message string
	Count   uint64
}
type Diagnostics struct {
	Problems       []Problem
	Total, Dropped uint64
}
type Store struct {
	root          string
	lock          *sync.Mutex
	secrets       Masker
	nextID        func() (ID, error)
	diagnosticsMu sync.Mutex
	diagnostics   Diagnostics
}

var locks [32]sync.Mutex
var safeID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
var safeWorktree = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
var ErrID = errors.New("task id must match [A-Za-z0-9._-]{1,128}")
var ErrWorktree = errors.New("worktree name must match [A-Za-z0-9._-]{1,64}")
var ErrRecord = errors.New("invalid task record")

func New(config Config) (*Store, error) {
	if config.Workspace == "" {
		return nil, errors.New("task store requires a workspace")
	}
	root, err := filepath.Abs(filepath.Join(config.Workspace, ".tasks"))
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	h := fnv.New32a()
	h.Write([]byte(root))
	return &Store{root: root, lock: &locks[h.Sum32()%uint32(len(locks))], secrets: config.Secrets, nextID: randomID}, nil
}
func randomID() (ID, error) {
	var data [6]byte
	_, err := rand.Read(data[:])
	return ID("task_" + hex.EncodeToString(data[:])), err
}
func (s *Store) Root() string { return s.root }
func (s *Store) path(id ID) (string, error) {
	if !safeID.MatchString(string(id)) {
		return "", ErrID
	}
	return filepath.Join(s.root, string(id)+".json"), nil
}
func (s *Store) report(text string) {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	d := &s.diagnostics
	d.Total++
	for i := range d.Problems {
		if d.Problems[i].Message == text {
			d.Problems[i].Count++
			return
		}
	}
	if len(d.Problems) == MaxProblems {
		d.Problems = d.Problems[1:]
		d.Dropped++
	}
	d.Problems = append(d.Problems, Problem{text, 1})
}
func (s *Store) Diagnostics() Diagnostics {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	d := s.diagnostics
	d.Problems = append([]Problem(nil), d.Problems...)
	return d
}
func (s *Store) Save(task Task) error {
	path, err := s.path(task.ID)
	if err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value *string
	}{{"subject", &task.Subject}, {"description", &task.Description}} {
		runes := []rune(*field.value)
		if len(runes) > MaxField {
			s.report(fmt.Sprintf("%s: %s truncated from %s to 16,000", task.ID, field.name, comma(len(runes))))
			*field.value = string(runes[:MaxField]) + " [truncated]"
		}
	}
	text, err := protocol.MaskedPythonJSON(task, func(value string) string {
		if s.secrets != nil {
			return s.secrets.MaskText(value)
		}
		return value
	}, true, true)
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err = json.Indent(&pretty, []byte(text), "", "  "); err != nil {
		return err
	}
	if pretty.Len() > MaxRecordBytes {
		return ErrRecord
	}
	temporaryID, err := randomID()
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(s.root, "."+filepath.Base(path)+"."+string(temporaryID)+".tmp"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	// Python replaces through a freshly-created file using the process umask.
	_, err = io.Copy(file, bytes.NewReader(pretty.Bytes()))
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		return err
	}
	dir, err := os.Open(s.root)
	if err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
func (s *Store) Load(id ID) (*Task, error) {
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		s.report("unreadable task " + filepath.Base(path) + ": OSError")
		return nil, nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxRecordBytes+1))
	if err != nil {
		s.report("unreadable task " + filepath.Base(path) + ": OSError")
		return nil, nil
	}
	var task Task
	if len(data) > MaxRecordBytes || !utf8.Valid(data) || !json.Valid(data) {
		s.report("unreadable task " + filepath.Base(path) + ": JSONDecodeError")
		return nil, nil
	}
	// Required fields are admitted separately so missing or null identity cannot
	// enter the typed store as a zero-value task. Unknown fields are rejected.
	var wire struct {
		ID          *ID     `json:"id"`
		Subject     *string `json:"subject"`
		Description *string `json:"description"`
		Status      *Status `json:"status"`
		Owner       *Owner  `json:"owner"`
		BlockedBy   []ID    `json:"blockedBy"`
		Worktree    *string `json:"worktree"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&wire); err != nil || wire.ID == nil || wire.Subject == nil {
		s.report("unreadable task " + filepath.Base(path) + ": TypeError")
		return nil, nil
	}
	task = Task{ID: *wire.ID, Subject: *wire.Subject, Status: Pending, BlockedBy: append([]ID{}, wire.BlockedBy...), Owner: wire.Owner, Worktree: wire.Worktree}
	if wire.Description != nil {
		task.Description = *wire.Description
	}
	if wire.Status != nil {
		task.Status = *wire.Status
	}
	if task.Status != Pending && task.Status != InProgress && task.Status != Completed {
		s.report("unreadable task " + filepath.Base(path) + ": ValueError")
		return nil, nil
	}
	return &task, nil
}
func (s *Store) List() ([]Task, error) {
	paths, err := filepath.Glob(filepath.Join(s.root, "task_*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	result := []Task{}
	for _, path := range paths {
		task, err := s.Load(ID(strings.TrimSuffix(filepath.Base(path), ".json")))
		if err != nil {
			return nil, err
		}
		if task != nil {
			result = append(result, *task)
		}
	}
	return result, nil
}
func (s *Store) Create(subject, description string, dependencies []ID, worktree *string) (Task, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, dep := range dependencies {
		if !safeID.MatchString(string(dep)) {
			return Task{}, errors.New("blockedBy contains an invalid task id")
		}
	}
	if worktree != nil && *worktree != "" && !validWorktree(*worktree) {
		return Task{}, ErrWorktree
	}
	id, err := s.nextID()
	if err != nil {
		return Task{}, err
	}
	task := Task{ID: id, Subject: subject, Description: description, Status: Pending, BlockedBy: append([]ID{}, dependencies...), Worktree: cloneString(worktree)}
	if err = s.Save(task); err != nil {
		return Task{}, err
	}
	return task, nil
}
func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func validWorktree(value string) bool {
	return value != "." && value != ".." && safeWorktree.MatchString(value)
}
func (s *Store) CanStart(id ID) (bool, error) {
	task, err := s.Load(id)
	if err != nil || task == nil {
		return false, err
	}
	for _, dep := range task.BlockedBy {
		blocker, err := s.Load(dep)
		if err != nil {
			return false, err
		}
		if blocker == nil || blocker.Status != Completed {
			return false, nil
		}
	}
	return true, nil
}
func (s *Store) Claim(id ID, owner Owner) (string, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	task, err := s.Load(id)
	if err != nil {
		return "", err
	}
	if task == nil {
		return fmt.Sprintf("Error: task %s not found", id), nil
	}
	if task.Status != Pending {
		return fmt.Sprintf("Error: task %s is %s, not claimable", id, task.Status), nil
	}
	if task.Owner != nil && *task.Owner != "" {
		return fmt.Sprintf("Error: task %s is already owned by %s", id, *task.Owner), nil
	}
	ready, err := s.CanStart(id)
	if err != nil {
		return "", err
	}
	if !ready {
		return fmt.Sprintf("Error: task %s is blocked by incomplete deps %s", id, pythonIDs(task.BlockedBy)), nil
	}
	path, _ := s.path(id)
	marker := strings.TrimSuffix(path, ".json") + ".owner"
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0777)
	if errors.Is(err, os.ErrExist) {
		holder := "an unknown claimer"
		if data, e := os.ReadFile(marker); e == nil && strings.TrimSpace(string(data)) != "" {
			holder = strings.TrimSpace(string(data))
		}
		current, e := s.Load(id)
		if e != nil {
			return "", e
		}
		if current == nil || current.Owner == nil || *current.Owner == "" {
			s.report(fmt.Sprintf("%s: claim marker from %s but the record shows no owner; a claimer may have crashed mid-claim (operator: inspect and remove %s)", id, holder, filepath.Base(marker)))
		}
		return fmt.Sprintf("Error: task %s is already claimed by %s", id, holder), nil
	}
	if err != nil {
		return "", err
	}
	_, err = file.WriteString(string(owner))
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	task.Owner = &owner
	task.Status = InProgress
	if err = s.Save(*task); err != nil {
		return "", err
	}
	return fmt.Sprintf("Claimed %s for %s", id, owner), nil
}
func (s *Store) Complete(id ID, owner *Owner) (string, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	task, err := s.Load(id)
	if err != nil {
		return "", err
	}
	if task == nil {
		return fmt.Sprintf("Error: task %s not found", id), nil
	}
	if task.Status != InProgress {
		return fmt.Sprintf("Error: task %s must be in_progress before completion (is %s)", id, task.Status), nil
	}
	if owner != nil && *owner != "" && task.Owner != nil && *task.Owner != "" && *task.Owner != *owner {
		return fmt.Sprintf("Error: task %s is owned by %s, not %s", id, *task.Owner, *owner), nil
	}
	task.Status = Completed
	if err = s.Save(*task); err != nil {
		return "", err
	}
	path, _ := s.path(id)
	_ = os.Remove(strings.TrimSuffix(path, ".json") + ".owner")
	list, err := s.List()
	if err != nil {
		return "", err
	}
	var unblocked []string
	for _, candidate := range list {
		if candidate.Status == Pending && len(candidate.BlockedBy) > 0 {
			ready, err := s.CanStart(candidate.ID)
			if err != nil {
				return "", err
			}
			if ready {
				unblocked = append(unblocked, string(candidate.ID))
			}
		}
	}
	text := fmt.Sprintf("Completed %s.", id)
	if len(unblocked) > 0 {
		text += " Now runnable: " + strings.Join(unblocked, ", ")
	}
	return text, nil
}
func (s *Store) BindWorktree(id ID, name string) (string, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if !validWorktree(name) {
		return "Error: " + ErrWorktree.Error(), nil
	}
	task, err := s.Load(id)
	if err != nil {
		return "", err
	}
	if task == nil {
		return fmt.Sprintf("Error: task %s not found", id), nil
	}
	task.Worktree = &name
	if err = s.Save(*task); err != nil {
		return "", err
	}
	return fmt.Sprintf("Bound %s to worktree %s", id, name), nil
}
func (s *Store) Runnable() ([]Task, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	result := []Task{}
	for _, task := range list {
		if task.Status == Pending && (task.Owner == nil || *task.Owner == "") {
			ready, err := s.CanStart(task.ID)
			if err != nil {
				return nil, err
			}
			if ready {
				result = append(result, task)
			}
		}
	}
	return result, nil
}
func (s *Store) Render() (string, error) {
	list, err := s.List()
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No tasks.", nil
	}
	existing := map[ID]bool{}
	for _, task := range list {
		existing[task.ID] = true
	}
	var lines []string
	for _, task := range list {
		owner, worktree, blocked := "", "", ""
		if task.Owner != nil && *task.Owner != "" {
			owner = " @" + string(*task.Owner)
		}
		if task.Worktree != nil && *task.Worktree != "" {
			worktree = " (worktree: " + *task.Worktree + ")"
		}
		if len(task.BlockedBy) > 0 {
			missing := []ID{}
			for _, dep := range task.BlockedBy {
				if !existing[dep] {
					missing = append(missing, dep)
				}
			}
			blocked = " (blockedBy: " + pythonIDs(task.BlockedBy)
			if len(missing) > 0 {
				s.report(fmt.Sprintf("%s: blocked by missing task(s) %s; it can never start until they are created", task.ID, pythonIDs(missing)))
				blocked += "; MISSING: " + pythonIDs(missing)
			}
			blocked += ")"
		}
		subject := []rune(task.Subject)
		if len(subject) > MaxSubject {
			subject = append(subject[:MaxSubject], '…')
		}
		glyph := map[Status]string{Pending: "[ ]", InProgress: "[>]", Completed: "[x]"}[task.Status]
		if glyph == "" {
			glyph = "[?]"
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s%s%s%s", glyph, task.ID, string(subject), owner, blocked, worktree))
	}
	if len(lines) > MaxRows {
		hidden := len(lines) - MaxRows
		lines = append([]string{fmt.Sprintf("... (%d older task(s) not shown; reference them by id)", hidden)}, lines[hidden:]...)
	}
	return strings.Join(lines, "\n"), nil
}
func pythonIDs(ids []ID) string {
	values := make([]string, len(ids))
	for i, id := range ids {
		quote := "'"
		if strings.Contains(string(id), "'") && !strings.Contains(string(id), "\"") {
			quote = "\""
		}
		value := strings.ReplaceAll(string(id), "\\", "\\\\")
		value = strings.ReplaceAll(value, quote, "\\"+quote)
		values[i] = quote + value + quote
	}
	return "[" + strings.Join(values, ", ") + "]"
}
func comma(value int) string {
	str := fmt.Sprint(value)
	for pos := len(str) - 3; pos > 0; pos -= 3 {
		str = str[:pos] + "," + str[pos:]
	}
	return str
}
func JSON(task Task) (string, error) {
	text, err := protocol.PythonJSON(task, true, true)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	err = json.Indent(&output, []byte(text), "", "  ")
	return output.String(), err
}
