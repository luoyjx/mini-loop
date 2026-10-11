package httpapi

import (
	"net/http"
	"os"
	"strings"

	"github.com/luoyjx/mini-loop/go/benchmark"
	"github.com/luoyjx/mini-loop/go/config"
)

func (s *Server) benchmark(w http.ResponseWriter, r *http.Request) {
	if !s.rate(w, principal(r.Context())) {
		return
	}
	// Source Settings() reads environment at request time. Only deployment skills
	// are captured from the app; no main-manager provider or owner snapshot leaks.
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	env["MINILOOP_FAKE_LLM"] = "1"
	env["MINILOOP_SPILL_DIR"] = ""
	// The source supplies workspace_root explicitly, ignoring this env field.
	env["MINILOOP_WORKSPACE_ROOT"] = os.TempDir()
	env["MINILOOP_SKILLS_DIR"] = s.benchmarkSkillsDir
	settings, err := config.Load(env, config.LoadOptions{})
	if err == nil {
		// The builtin sentinel is compiled data, not a filesystem path.
		if s.benchmarkSkillsDir == config.BuiltinSkills {
			settings.SkillsDir = config.BuiltinSkills
		}
		var timing config.ServerSettings
		timing, err = config.LoadServer(map[string]string{"MINILOOP_FAKE_DELAY": env["MINILOOP_FAKE_DELAY"]})
		if err == nil {
			var report benchmark.FakeReport
			report, err = benchmark.RunFakeComparison(r.Context(), settings, timing.FakeDelay)
			if err == nil {
				writeJSON(s, w, http.StatusOK, report)
				return
			}
		}
	}
	// Configuration, filesystem and cancellation errors stay private.
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
