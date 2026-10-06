package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

type userResourceFrame struct {
	Bound        bool
	Name         string
	Owner        OwnerID
	Descriptions string
	Loads        []struct {
		Input  protocol.LoadSkillInput
		SHA256 string
		Failed bool
	}
}

func ownerResourceResolver(t *testing.T, root string, agent *skills.Catalog) *userresources.Resolver {
	t.Helper()
	value, err := userresources.NewResolver(context.Background(), root, agent, nil)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func publishOwnerSkill(t *testing.T, resolver *userresources.Resolver, owner, name, description, body string) {
	t.Helper()
	if _, err := resolver.PublishSkill(context.Background(), userresources.OwnerID(owner), userresources.SkillFields{Name: name, Description: description, Body: body}); err != nil {
		t.Fatal(err)
	}
}
func resourceDigest(text string) string {
	value := sha256.Sum256([]byte(text))
	return hex.EncodeToString(value[:])
}

func TestManagedUserResourceFramesMatchActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-user-session-resources.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Frames               []userResourceFrame
		MemoryReused         bool `json:"memory_reused"`
		TeammateMemoryPinned bool `json:"teammate_memory_pinned"`
		ResourcesRetained    bool `json:"resources_retained"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Frames) != 9 {
		t.Fatal("incomplete source frames")
	}
	ctx := context.Background()
	base := t.TempDir()
	agentRoot := filepath.Join(base, "agent")
	path := filepath.Join(agentRoot, "first", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	canonical, _ := userresources.NewCanonicalSkill(userresources.SkillFields{Name: "first", Description: "First", Body: "agent"})
	if err := os.WriteFile(path, []byte(canonical.Text()), 0600); err != nil {
		t.Fatal(err)
	}
	agent, err := skills.NewCatalog(ctx, agentRoot)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "users")
	resolver := ownerResourceResolver(t, root, agent)
	publishOwnerSkill(t, resolver, "alice", "first", "First", "alice")
	publishOwnerSkill(t, resolver, "bob", "first", "First", "bob")
	store := newRuntimeStateStore()
	config := managerTestConfig(filepath.Join(base, "workspaces"), &FakeProvider{})
	config.Services.Skills = agent
	config.Services.UserResources = resolver
	config.Services.StateStore = store
	manager := makeManager(t, config)
	alice := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	bob := createManaged(t, manager, CreateSessionRequest{Owner: "bob"})
	anonymous := createManaged(t, manager, CreateSessionRequest{Owner: "anonymous"})
	check := func(name string, core *Session) {
		t.Helper()
		var frame *userResourceFrame
		for i := range fixture.Frames {
			if fixture.Frames[i].Name == name {
				frame = &fixture.Frames[i]
				break
			}
		}
		if frame == nil || len(frame.Loads) != 4 {
			t.Fatal("missing frame", name)
		}
		resources, ok := core.UserResources()
		if ok != frame.Bound || ok && OwnerID(resources.Owner()) != frame.Owner || core.owner != frame.Owner || core.skills.Descriptions() != frame.Descriptions {
			t.Fatal(name, "owner/catalogue differs")
		}
		for _, load := range frame.Loads {
			outcome, err := core.gate.Dispatch(ctx, ToolAuthority{SessionID: core.id, OwnerID: core.owner, Workspace: core.executionRoot(), Mode: core.permissionMode()}, ToolCall{ID: "probe", Input: protocol.LoadSkillToolInput(load.Input)})
			if err != nil || outcome.Failed != load.Failed || resourceDigest(outcome.Output) != load.SHA256 {
				t.Fatalf("%s %s: %+v %v", name, load.Input.Name, outcome, err)
			}
		}
	}
	check("alice-old", alice.core)
	check("bob", bob.core)
	check("anonymous", anonymous.core)
	publishOwnerSkill(t, resolver, "alice", "later", "Later", "new")
	check("alice-live", alice.core)
	fork, err := manager.Fork(ctx, "alice", alice.ID())
	if err != nil {
		t.Fatal(err)
	}
	check("fork", fork.core)
	old, _ := alice.core.UserResources()
	forked, _ := fork.core.UserResources()
	if (old.Memory() == forked.Memory()) != fixture.MemoryReused {
		t.Fatal("fork memory reuse differs")
	}
	// Native teams are pending. This frame checks the same inherited bundle seam
	// used by a source teammate without claiming a native teammate scheduler.
	childConfig := runtimeConfig(t.TempDir(), &FakeProvider{})
	childConfig.Owner = "alice"
	childConfig.UserResources = &old
	child, err := NewRuntimeSession(childConfig)
	if err != nil {
		t.Fatal(err)
	}
	check("teammate", child)
	childResources, _ := child.UserResources()
	if (childResources.Memory() == old.Memory()) != fixture.TeammateMemoryPinned {
		t.Fatal("inherited memory binding differs")
	}
	if _, err := manager.Delete("bob", bob.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	bobKey, _ := userresources.OwnerDirectoryKey("bob")
	_, fileErr := os.Stat(filepath.Join(root, string(bobKey), "skills", "first", "SKILL.md"))
	if (fileErr == nil) != fixture.ResourcesRetained {
		t.Fatal("session deletion removed resources")
	}
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	config.Services.UserResources = ownerResourceResolver(t, root, agent)
	restoredManager := makeManager(t, config)
	restored, err := restoredManager.RestoreSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, session := range restored {
		if session.ID() == alice.ID() {
			check("restored-alice", session.core)
			found = true
		}
	}
	if !found {
		t.Fatal("saved owner not restored")
	}
	scheduled, err := restoredManager.RestoreScheduledSession(ctx, "missing")
	if err != nil {
		t.Fatal(err)
	}
	check("scheduled-anonymous", scheduled.core)
	config.Services.UserResources, config.Services.StateStore = nil, nil
	disabled := makeManager(t, config)
	check("disabled", createManaged(t, disabled, CreateSessionRequest{Owner: "alice"}).core)
}

func TestFailedOwnerResolutionDoesNotPublishHandleOrRetainScratch(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "users")
	resolver := ownerResourceResolver(t, root, skills.EmptyCatalog())
	key, _ := userresources.OwnerDirectoryKey("owner")
	victim := filepath.Join(base, "victim")
	if err := os.Mkdir(victim, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(root, string(key))); err != nil {
		t.Fatal(err)
	}
	config := managerTestConfig(filepath.Join(base, "workspaces"), &FakeProvider{})
	config.Services.UserResources = resolver
	manager := makeManager(t, config)
	if _, err := manager.Create(context.Background(), CreateSessionRequest{Owner: "owner"}); err == nil {
		t.Fatal("unsafe owner resource directory admitted")
	}
	if len(manager.List("owner")) != 0 {
		t.Fatal("failed resource construction published a handle")
	}
	entries, err := os.ReadDir(config.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("failed construction retained scratch", entry.Name())
		}
	}
	entries, err = os.ReadDir(victim)
	if err != nil || len(entries) != 0 {
		t.Fatal("unsafe owner binding touched victim", entries, err)
	}
}

func TestRuntimeUserResourcesValidateOwnerBeforeFilesystemAndPinCallerValue(t *testing.T) {
	resolver := ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	bound, err := resolver.ForOwner(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []userresources.Resources{{}, func() userresources.Resources {
		r, err := resolver.ForOwner(context.Background(), "other")
		if err != nil {
			t.Fatal(err)
		}
		return r
	}()} {
		config := runtimeConfig(filepath.Join(t.TempDir(), "absent"), &FakeProvider{})
		config.UserResources = &wrong
		if _, err := NewRuntimeSession(config); err == nil {
			t.Fatal("wrong/incomplete owner admitted")
		}
		if _, err := os.Stat(config.Workspace); !os.IsNotExist(err) {
			t.Fatal("wrong binding created workspace")
		}
	}
	config := runtimeConfig(t.TempDir(), &FakeProvider{})
	config.UserResources = &bound
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	bound = userresources.Resources{}
	fixed, ok := session.UserResources()
	if !ok || fixed.Owner() != "owner" {
		t.Fatal("caller rebinding changed session")
	}
	disabled, err := NewRuntimeSession(runtimeConfig(t.TempDir(), &FakeProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := disabled.UserResources(); ok {
		t.Fatal("default unexpectedly activated owner resources")
	}
}

type ownerChildCapture struct {
	resource userresources.Resources
	skills   SkillSource
	delegate InProcessSubagents
}

func (p *ownerChildCapture) RunSubagent(ctx context.Context, request SubagentRequest) (string, error) {
	resources, ok := request.Parent.UserResources()
	if !ok {
		return "", errors.New("missing inherited bundle")
	}
	p.resource, p.skills = resources, request.Parent.skills
	return p.delegate.RunSubagent(ctx, request)
}
func TestInProcessChildInheritsPinnedOwnerBundleAndRunsQualifiedSkillThroughGate(t *testing.T) {
	ctx := context.Background()
	resolver := ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	publishOwnerSkill(t, resolver, "owner", "first", "First", "old")
	bound, err := resolver.ForOwner(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	capture := &ownerChildCapture{}
	provider := resourceProvider{tools: []protocol.Block{protocol.NewToolUse("load", protocol.LoadSkillToolInput(protocol.LoadSkillInput{Name: "user:first"}))}}
	config := runtimeConfig(t.TempDir(), provider)
	config.UserResources = &bound
	config.Subagents = capture
	config.RoleToolPolicy = allRoleTools{}
	parent, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	publishOwnerSkill(t, resolver, "owner", "later", "Later", "new")
	if _, err := parent.Delegate(ctx, "read skill", RoleWorker); err != nil {
		t.Fatal(err)
	}
	if capture.resource != bound || capture.skills != bound.Skills() {
		t.Fatal("child silently refreshed resources")
	}
	served := false
	for _, event := range parent.Events() {
		if result, ok := event.Event.ToolResult(); ok && result.Name == protocol.ToolLoadSkill {
			if result.Failed || !strings.Contains(result.Output, "source=\"user\"") || !strings.Contains(result.Output, "old") {
				t.Fatal("qualified child load differs", result)
			}
			served = true
		}
	}
	if !served {
		t.Fatal("child did not execute the skill")
	}
}

type brokenSkillSource struct{}

func (brokenSkillSource) Descriptions() string { return "" }
func (brokenSkillSource) Load(context.Context, protocol.LoadSkillInput) (string, error) {
	return "", errors.New("backend unavailable")
}
func TestSkillRefusalsKeepCustomBackendAndCancellationFaultsDistinct(t *testing.T) {
	config := runtimeConfig(t.TempDir(), &FakeProvider{})
	config.Skills = brokenSkillSource{}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := session.gate.Dispatch(context.Background(), ToolAuthority{SessionID: session.id, OwnerID: session.owner, Workspace: session.executionRoot(), Mode: session.permissionMode()}, ToolCall{ID: "load", Input: protocol.LoadSkillToolInput(protocol.LoadSkillInput{Name: "missing"})})
	if err != nil || !outcome.Failed {
		t.Fatal("backend fault became domain refusal", outcome, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = skills.EmptyCatalog().Load(ctx, protocol.LoadSkillInput{Name: "missing"})
	var refusal *skills.RefusalError
	if !errors.Is(err, context.Canceled) || errors.As(err, &refusal) {
		t.Fatal("cancellation became domain refusal", err)
	}
}
