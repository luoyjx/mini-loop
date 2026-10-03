package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type managerBindingCase struct {
	Name      string
	Status    int
	Reason    string
	Workspace *string
	Bound     bool
}
type managerContract struct {
	BindingCases      []managerBindingCase `json:"binding_cases"`
	BoundMarker       string               `json:"bound_marker"`
	DefaultModelLimit ConcurrencyLimit     `json:"default_model_limit"`
	DefaultToolLimit  ConcurrencyLimit     `json:"default_tool_limit"`
	DefaultRounds     int                  `json:"default_rounds"`
	Deleted           struct {
		Deleted          bool
		Listing          []OwnerID
		Remembered       OwnerID
		UnknownDelete    bool `json:"unknown_delete"`
		WorkspaceRemoved bool `json:"workspace_removed"`
	}
	IDLength         int `json:"id_length"`
	Initial          SessionInfo
	MaxOwners        int       `json:"max_owners"`
	OwnersBefore     []OwnerID `json:"owners_before"`
	ScratchDistinct  bool      `json:"scratch_distinct"`
	Shared           struct{ Actions, Approvals, Model, Tools bool }
	StopKeepsScratch bool   `json:"stop_keeps_scratch"`
	StoppedCreate    string `json:"stopped_create"`
}

func readManagerContract(t *testing.T) managerContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-manager.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract managerContract
	if err = json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.BindingCases) != 10 || contract.MaxOwners != MaxRememberedOwners {
		t.Fatal("manager inventory drift")
	}
	return contract
}

func TestManagerCreationDeletionAndDefaultsMatchPython(t *testing.T) {
	contract := readManagerContract(t)
	m := makeManager(t, ManagerConfig{WorkspaceRoot: t.TempDir(), Services: ManagerServices{Provider: FakeProvider{}}})
	first := createManaged(t, m, CreateSessionRequest{Owner: "first"})
	second := createManaged(t, m, CreateSessionRequest{Owner: "second"})
	initial := first.Info()
	initial.ID = ""
	initial.CreatedAt = 0
	initial.Workspace = ""
	if !reflect.DeepEqual(initial, contract.Initial) {
		t.Fatalf("initial info differs from Python: %+v != %+v", initial, contract.Initial)
	}
	if len(first.ID()) != contract.IDLength || (first.core.workspace != second.core.workspace) != contract.ScratchDistinct || first.core.maxRounds != contract.DefaultRounds || ConcurrencyLimit(cap(first.core.modelLimiter.slots)) != contract.DefaultModelLimit || ConcurrencyLimit(cap(first.core.toolLimiter.slots)) != contract.DefaultToolLimit {
		t.Fatal("manager default/identity drift")
	}
	if (first.core.gate.journal == second.core.gate.journal) != contract.Shared.Actions || (first.approvals == second.approvals) != contract.Shared.Approvals || (first.core.modelLimiter == second.core.modelLimiter) != contract.Shared.Model || (first.core.toolLimiter == second.core.toolLimiter) != contract.Shared.Tools {
		t.Fatal("shared source services drift")
	}
	var before []OwnerID
	for _, id := range m.order {
		session, _ := m.Get(m.sessions[id].Owner(), id)
		before = append(before, session.Owner())
	}
	if !reflect.DeepEqual(before, contract.OwnersBefore) {
		t.Fatal("creation order drift")
	}
	deleted, err := m.Delete("first", first.ID(), DeleteSessionOptions{})
	if err != nil || deleted != contract.Deleted.Deleted {
		t.Fatal(err)
	}
	_, pathErr := os.Stat(first.core.workspace)
	if os.IsNotExist(pathErr) != contract.Deleted.WorkspaceRemoved || m.RememberedOwners()[first.ID()] != contract.Deleted.Remembered {
		t.Fatal("deletion/attribution drift")
	}
	var after []OwnerID
	for _, id := range m.order {
		after = append(after, m.sessions[id].Owner())
	}
	if !reflect.DeepEqual(after, contract.Deleted.Listing) {
		t.Fatal("deleted listing drift")
	}
	missing, err := m.Delete("first", "missing", DeleteSessionOptions{})
	if missing != contract.Deleted.UnknownDelete || !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("missing delete drift", err)
	}
	m.Stop(context.Background())
	_, err = m.Create(context.Background(), CreateSessionRequest{Owner: "first"})
	if err == nil || err.Error() != contract.StoppedCreate {
		t.Fatal("stopped create drift", err)
	}
	_, pathErr = os.Stat(second.core.workspace)
	if (pathErr == nil) != contract.StopKeepsScratch {
		t.Fatal("stop reclaimed live scratch")
	}
}

func TestManagerBindingCasesMatchPython(t *testing.T) {
	contract := readManagerContract(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(base, "allowed", "repo")
	outside := filepath.Join(base, "outside")
	os.MkdirAll(checkout, 0700)
	os.Mkdir(outside, 0700)
	os.WriteFile(filepath.Join(checkout, "keep"), []byte("source"), 0600)
	os.Symlink(checkout, filepath.Join(base, "allowed", "alias"))
	os.Symlink(outside, filepath.Join(base, "allowed", "escape"))
	narrow := makeManager(t, ManagerConfig{WorkspaceRoot: filepath.Join(base, "narrow"), BindableRoots: []string{filepath.Join(base, "allowed")}, Services: ManagerServices{Provider: FakeProvider{}}})
	broad := makeManager(t, ManagerConfig{WorkspaceRoot: filepath.Join(base, "broad"), BindableRoots: []string{base}, Services: ManagerServices{Provider: FakeProvider{}}})
	off := makeManager(t, ManagerConfig{WorkspaceRoot: filepath.Join(base, "off"), Services: ManagerServices{Provider: FakeProvider{}}})
	for _, entry := range contract.BindingCases {
		t.Run(entry.Name, func(t *testing.T) {
			manager, path := narrow, checkout
			switch entry.Name {
			case "disabled":
				manager = off
			case "alias", "escape":
				path = filepath.Join(base, "allowed", entry.Name)
			case "outside":
				path = outside
			case "outside-missing":
				path = filepath.Join(outside, "absent")
			case "inside-missing":
				path = filepath.Join(base, "allowed", "absent")
			case "file":
				path = filepath.Join(checkout, "keep")
			case "own-root":
				manager, path = broad, broad.WorkspaceRoot()
			case "own-child":
				manager, path = broad, filepath.Join(broad.WorkspaceRoot(), "absent")
			}
			session, err := manager.Create(context.Background(), CreateSessionRequest{Owner: "owner", Workspace: &path})
			if entry.Status == 200 {
				if err != nil {
					t.Fatal(err)
				}
				relative, _ := filepath.Rel(base, session.Info().Workspace)
				if entry.Workspace == nil || relative != *entry.Workspace || session.Info().WorkspaceBound != entry.Bound {
					t.Fatal("resolved binding differs from Python")
				}
				manager.Delete("owner", session.ID(), DeleteSessionOptions{})
				return
			}
			var binding *WorkspaceBindingError
			if !errors.As(err, &binding) || int(binding.Status) != entry.Status {
				t.Fatal("binding status differs from Python", err)
			}
			reason := "not-directory"
			switch {
			case strings.Contains(binding.Detail, "binding is disabled"):
				reason = "disabled"
			case strings.Contains(binding.Detail, "manager's own"):
				reason = "own-root"
			case strings.Contains(binding.Detail, "outside every"):
				reason = "outside"
			}
			if reason != entry.Reason {
				t.Fatal("binding policy differs from Python", err)
			}
		})
	}
	data, err := os.ReadFile(filepath.Join(checkout, "keep"))
	if err != nil || string(data) != contract.BoundMarker {
		t.Fatal("bound marker differed after deletes", err)
	}
}
