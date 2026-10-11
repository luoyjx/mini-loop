package shell

import "testing"

type unavailableTestSandbox struct {
	testSandbox
	reason string
}

func (s unavailableTestSandbox) ForWorkspace(root string) (Sandbox, error) {
	s.root = root
	return s, nil
}
func (s unavailableTestSandbox) SandboxReason() *string { return &s.reason }

func TestSandboxPostureReportsBoundWrapperWithoutRunningIt(t *testing.T) {
	plain := makeExecutor(t, Config{})
	if got := plain.SandboxPosture(); got.Name != "None" || got.Reason != nil {
		t.Fatal(got)
	}
	bound := makeExecutor(t, Config{Sandbox: unavailableTestSandbox{testSandbox: testSandbox{program: "/nonexistent/never-run"}, reason: "fixture backend unavailable"}})
	got := bound.SandboxPosture()
	if got.Name != "unavailableTestSandbox" || got.Reason == nil || *got.Reason != "fixture backend unavailable" {
		t.Fatal(got)
	}
	*got.Reason = "mutated"
	next, err := bound.WithWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := next.SandboxPosture(); got.Name != "unavailableTestSandbox" || *got.Reason != "fixture backend unavailable" {
		t.Fatal(got)
	}
}
