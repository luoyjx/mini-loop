package assistant

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/memory"
)

//go:embed web/index.html
var indexHTML []byte

const maxBody = 64 << 10

// IntegrationView tells the page which MCP server backs mail and calendar
// and whether it currently answers.
type IntegrationView struct {
	Server     string   `json:"server"`
	Transport  string   `json:"transport"`
	AgentTools []string `json:"agent_tools"`
	DaemonOnly []string `json:"daemon_only"`
	Error      string   `json:"error,omitempty"`
}

// WorkersView shows how many background turns may run at once, how many
// more slots are kept for the user's chat, and how many turns run now.
type WorkersView struct {
	Capacity int `json:"capacity"`
	Reserved int `json:"reserved"`
	Running  int `json:"running"`
}

// StateView is everything the simulated channel page renders. Attention
// comes first on the page; Paused is set while the user paused processing.
// Candidates are observed inferred candidates; Router counts decisions since
// the process started.
type StateView struct {
	Model         string           `json:"model"`
	Now           time.Time        `json:"now"`
	Paused        *time.Time       `json:"paused,omitempty"`
	Attention     []AttentionItem  `json:"attention"`
	CandidateMode CandidateMode    `json:"candidate_mode"`
	Candidates    []Trigger        `json:"candidates"`
	Router        RouterStats      `json:"router"`
	Jobs          []Job            `json:"jobs"`
	Contacts      []Contact        `json:"contacts"`
	Profile       []ProfileEntry   `json:"profile"`
	Memories      []MemoryView     `json:"memories"`
	Retrieval     string           `json:"retrieval"`
	Vectors       *VectorStatus    `json:"vectors,omitempty"`
	Integration   IntegrationView  `json:"integration"`
	Workers       WorkersView      `json:"workers"`
	Channel       []ChannelMessage `json:"channel"`
	Proposals     []Proposal       `json:"proposals"`
	Tasks         []Task           `json:"tasks"`
	Inbox         []Email          `json:"inbox"`
	Sent          []Email          `json:"sent"`
	Calendar      []CalendarEvent  `json:"calendar"`
	Ledger        []LedgerEntry    `json:"ledger"`
	Triggers      []Trigger        `json:"triggers"`
	Routines      []Routine        `json:"routines"`
	Presets       []PresetInfo     `json:"presets"`
}

type chatRequest struct {
	Text string `json:"text"`
}

type simulateRequest struct {
	Preset Preset `json:"preset"`
}

type answerRequest struct {
	Text string `json:"text"`
}

type modeRequest struct {
	Mode CandidateMode `json:"mode"`
}

type pauseRequest struct {
	Paused bool `json:"paused"`
}

type pauseResponse struct {
	Paused  bool `json:"paused"`
	Changed bool `json:"changed"`
}

type acceptedResponse struct {
	TriggerID int64 `json:"trigger_id"`
}

type deliveredResponse struct {
	MailID string `json:"mail_id"`
}

type bookedResponse struct {
	EventID string    `json:"event_id"`
	Start   time.Time `json:"start"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Handler serves the simulated channel. It is meant for a loopback listener:
// requests must name a loopback host (DNS-rebinding guard), and writes must be
// same-origin JSON (simple cross-site forms cannot set that content type).
func (app *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("GET /api/state", app.handleState)
	mux.HandleFunc("POST /api/chat", app.handleChat)
	mux.HandleFunc("POST /api/simulate", app.handleSimulate)
	mux.HandleFunc("POST /api/simulate/custom", app.handleCustom)
	mux.HandleFunc("POST /api/simulate/event", app.handleEvent)
	mux.HandleFunc("POST /api/proposals/{id}/{decision}", app.handleDecision)
	mux.HandleFunc("POST /api/tasks/{id}/{action}", app.handleTask)
	mux.HandleFunc("POST /api/triggers/{id}/retry", app.handleTriggerRetry)
	mux.HandleFunc("POST /api/attention/{ref}/dismiss", app.handleDismiss)
	mux.HandleFunc("POST /api/questions/{id}/answer", app.handleAnswer)
	mux.HandleFunc("POST /api/candidates/{id}/{decision}", app.handleCandidate)
	mux.HandleFunc("POST /api/candidates/mode", app.handleCandidateMode)
	mux.HandleFunc("POST /api/jobs/{id}/stop", app.handleJobStop)
	mux.HandleFunc("POST /api/pause", app.handlePause)
	mux.HandleFunc("POST /api/routines/{id}/run", app.handleRoutine)
	mux.HandleFunc("GET /api/memory/search", app.handleMemorySearch)
	return guard(mux)
}

func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if ip := net.ParseIP(host); !(host == "localhost" || (ip != nil && ip.IsLoopback())) {
			writeJSON(w, http.StatusForbidden, errorResponse{"loopback host required"})
			return
		}
		if r.Method == http.MethodPost {
			media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if media != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeJSON(w, http.StatusForbidden, errorResponse{"same-origin JSON required"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decode[T any](r *http.Request) (T, error) {
	var value T
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return value, errors.New("request body must contain one JSON object")
	}
	return value, nil
}

func (app *App) handleState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := app.now()
	view := StateView{Model: app.ModelLabel, Now: now, Presets: Presets, Integration: IntegrationView{
		Server: app.Connector.Server(), Transport: app.Connector.Transport(),
		AgentTools: []string{MCPMailList, MCPMailRead, MCPCalendarList, MCPCalendarGet},
		DaemonOnly: []string{MCPMailPoll + "（poller）", MCPCalendarList + "（scheduler：日程临近）", MCPMailSend + "（outbox）", MCPCalendarRespond + "（outbox）", MCPSimDeliver + " / " + MCPSimAddEvent + "（模拟器）"},
	}, Workers: WorkersView{Capacity: app.Router.Capacity(), Reserved: app.Router.Reserved()}, Router: app.Router.Stats()}
	if since, paused := app.Store.Paused(); paused {
		view.Paused = &since
	}
	var err error
	steps := []func() error{
		func() error { view.Attention, err = app.Attention(ctx); return err },
		func() error { view.Channel, err = app.Store.Channel(ctx, 200); return err },
		func() error { view.Proposals, err = app.Store.Proposals(ctx, 50); return err },
		func() error { view.Tasks, err = app.Store.Tasks(ctx, 40); return err },
		func() error { view.Workers.Running, err = app.Store.CountTriggers(ctx, TriggerRunning); return err },
		func() error { view.Ledger, err = app.Store.Ledger(ctx, 150); return err },
		func() error { view.Triggers, err = app.Store.Triggers(ctx, 50); return err },
		func() error { view.Routines, err = app.Store.Routines(ctx); return err },
		func() error { view.CandidateMode, err = app.Store.CandidateMode(ctx); return err },
		func() error { view.Candidates, err = app.Store.Candidates(ctx, 30); return err },
		func() error { view.Jobs, err = app.Store.Jobs(ctx, 20); return err },
		func() error { view.Contacts, err = app.Store.Contacts(ctx, 20); return err },
		func() error { view.Profile, err = app.Store.Profile(ctx); return err },
		func() error { view.Memories, err = app.memoryView(ctx); return err },
	}
	view.Retrieval = app.Search.Mode()
	if app.Search.vectors != nil {
		status := app.Search.vectors.Status()
		view.Vectors = &status
	}
	for _, step := range steps {
		if err := step(); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
			return
		}
	}
	// The mailbox and calendar panes are read through MCP like everything
	// else; a connector outage degrades them instead of failing the page.
	view.Inbox, view.Sent, view.Calendar = []Email{}, []Email{}, []CalendarEvent{}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	remote := []func() error{
		func() error { view.Inbox, err = app.Connector.ListMail(ctx, FolderInbox, 50); return err },
		func() error { view.Sent, err = app.Connector.ListMail(ctx, FolderSent, 50); return err },
		func() error { view.Calendar, err = app.Connector.Events(ctx, day, day.AddDate(0, 0, 7)); return err },
	}
	for _, step := range remote {
		if err := step(); err != nil {
			view.Integration.Error = err.Error()
			break
		}
	}
	if view.Integration.Error == "" {
		view.Integration.Error = app.Poller.LastError()
	}
	writeJSON(w, http.StatusOK, view)
}

func (app *App) handleChat(w http.ResponseWriter, r *http.Request) {
	request, err := decode[chatRequest](r)
	text := strings.TrimSpace(request.Text)
	if err != nil || text == "" || len([]rune(text)) > MaxChatRunes {
		writeJSON(w, http.StatusBadRequest, errorResponse{fmt.Sprintf("text must be 1-%d characters", MaxChatRunes)})
		return
	}
	id, err := app.Store.ChatFromUser(r.Context(), text, app.now())
	app.accepted(w, id, err)
}

func (app *App) handleSimulate(w http.ResponseWriter, r *http.Request) {
	request, err := decode[simulateRequest](r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	delivered, err := app.Connector.DeliverPreset(r.Context(), request.Preset)
	app.delivered(w, delivered, err)
}

func (app *App) handleCustom(w http.ResponseWriter, r *http.Request) {
	request, err := decode[CustomMail](r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	delivered, err := app.Connector.DeliverCustom(r.Context(), request)
	app.delivered(w, delivered, err)
}

// delivered answers a simulator call. The mail is now in the external inbox;
// the daemon finds it on the poll this wake-up triggers, not through this
// request.
func (app *App) delivered(w http.ResponseWriter, mail DeliveredMail, err error) {
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	app.Poller.Wake()
	writeJSON(w, http.StatusAccepted, deliveredResponse{mail.ID})
}

// handleEvent books a meeting that starts soon in the simulated calendar.
// The scheduler finds it through calendar_list on the tick this wakes.
func (app *App) handleEvent(w http.ResponseWriter, r *http.Request) {
	request, err := decode[SimEvent](r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	event, err := app.Connector.AddEvent(r.Context(), request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	app.Scheduler.Wake()
	writeJSON(w, http.StatusAccepted, bookedResponse{event.ID, event.Start})
}

// handleTask lets the user check a task now, cancel it, or resume one that
// was handed back after its last automatic check. Workers complete tasks;
// only the user cancels or resumes them.
func (app *App) handleTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	action := r.PathValue("action")
	if err != nil || (action != "wake" && action != "cancel" && action != "resume") {
		writeJSON(w, http.StatusNotFound, errorResponse{"unknown task action"})
		return
	}
	if _, err := decode[struct{}](r); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	now := app.now()
	var task Task
	switch action {
	case "wake":
		task, err = app.Store.WakeTask(r.Context(), id, "用户点了立即检查", "me（信道）", now)
	case "resume":
		task, err = app.Store.ResumeTask(r.Context(), id, "me（信道）", now)
	default:
		task, err = app.Store.CloseTask(r.Context(), id, TaskCancelled, "用户取消", "me（信道）", now)
	}
	if !writeActionError(w, err) {
		return
	}
	switch action {
	case "wake":
		app.Scheduler.Wake()
	case "resume":
		_, _ = app.Store.Post(r.Context(), ChannelMessage{Role: RoleSystem, Text: fmt.Sprintf("继续跟进任务 #%d「%s」，再自动检查最多 %d 次。", task.ID, task.Title, task.MaxChecks), TriggerID: task.LastTrigger}, now)
		app.Scheduler.Wake()
	default:
		_, _ = app.Store.Post(r.Context(), ChannelMessage{Role: RoleSystem, Text: fmt.Sprintf("已取消任务 #%d「%s」，不再自动检查。", task.ID, task.Title), TriggerID: task.LastTrigger}, now)
	}
	writeJSON(w, http.StatusOK, task)
}

// writeActionError answers a failed page action and reports whether err was
// nil: a state conflict is 409, an unknown item 404, anything else 400.
func writeActionError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, ErrTaskClosed), errors.Is(err, ErrProposalDecided), errors.Is(err, ErrNotActionable), errors.Is(err, ErrTooManyTasks):
		writeJSON(w, http.StatusConflict, errorResponse{err.Error()})
	case errors.Is(err, ErrUnknownRef):
		writeJSON(w, http.StatusNotFound, errorResponse{err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
	}
	return false
}

// handleTriggerRetry queues a failed trigger again.
func (app *App) handleTriggerRetry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResponse{"unknown trigger"})
		return
	}
	if _, err := decode[struct{}](r); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	trigger, err := app.Store.ReprocessTrigger(r.Context(), id, "me（信道）", app.now())
	if !writeActionError(w, err) {
		return
	}
	app.Router.Wake()
	writeJSON(w, http.StatusOK, trigger)
}

// handleDismiss takes a failed proposal, a handed-back task or a failed
// trigger off the attention list without acting on it.
func (app *App) handleDismiss(w http.ResponseWriter, r *http.Request) {
	if _, err := decode[struct{}](r); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	if !writeActionError(w, app.Store.Dismiss(r.Context(), r.PathValue("ref"), "me（信道）", app.now())) {
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// handleAnswer takes the user's answer to a worker's question and hands it
// back to the question's thread as a user.answer trigger.
func (app *App) handleAnswer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResponse{"unknown question"})
		return
	}
	request, err := decode[answerRequest](r)
	text := strings.TrimSpace(request.Text)
	if err != nil || text == "" || len([]rune(text)) > MaxChatRunes {
		writeJSON(w, http.StatusBadRequest, errorResponse{fmt.Sprintf("text must be 1-%d characters", MaxChatRunes)})
		return
	}
	_, trigger, err := app.Store.AnswerQuestion(r.Context(), id, text, "me（信道）", app.now())
	if !writeActionError(w, err) {
		return
	}
	app.Router.Wake()
	writeJSON(w, http.StatusAccepted, acceptedResponse{trigger})
}

// MemoryView is one owner memory as the page lists it, with the search
// aliases written for its current text. Legs, and Similarity when the dense
// leg found it, are set on search results.
type MemoryView struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Origin      string   `json:"origin"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	Aliases     []string `json:"aliases,omitempty"`
	Legs        []string `json:"legs,omitempty"`
	Similarity  float64  `json:"similarity,omitempty"`
}

func (app *App) memoryView(ctx context.Context) ([]MemoryView, error) {
	records, err := app.Memory.List(ctx)
	if err != nil {
		return nil, err
	}
	aliases, err := app.Store.MemoryAliases(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]MemoryView, 0, len(records))
	for _, record := range records {
		result = append(result, memoryRow(record, aliases))
	}
	return result, nil
}

func memoryRow(record memory.Record, aliases map[string]MemoryAlias) MemoryView {
	view := MemoryView{Name: record.Name, Type: string(record.Type), Origin: string(record.Origin), Description: record.Description, Body: truncateRunes(strings.TrimSpace(record.Body), 120)}
	if alias, ok := aliases[record.Name]; ok && alias.Hash == aliasHash(record) {
		view.Aliases = alias.Aliases
	}
	return view
}

// handleMemorySearch runs the workers' memory search for the page.
func (app *App) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if len([]rune(query)) > 200 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"query exceeds 200 characters"})
		return
	}
	hits, err := app.Search.Search(r.Context(), query, maxSearchLimit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
		return
	}
	aliases, err := app.Store.MemoryAliases(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
		return
	}
	result := make([]MemoryView, 0, len(hits))
	for _, hit := range hits {
		row := memoryRow(hit.Record, aliases)
		row.Legs, row.Similarity = hit.Legs, hit.Dense
		result = append(result, row)
	}
	writeJSON(w, http.StatusOK, result)
}

// handleCandidate hands an observed candidate to a worker or ignores it.
func (app *App) handleCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	decision := r.PathValue("decision")
	if err != nil || (decision != "handle" && decision != "ignore") {
		writeJSON(w, http.StatusNotFound, errorResponse{"unknown candidate decision"})
		return
	}
	if _, err := decode[struct{}](r); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	trigger, err := app.Store.DecideCandidate(r.Context(), id, decision == "handle", "me（信道）", app.now())
	if !writeActionError(w, err) {
		return
	}
	app.Router.Wake()
	writeJSON(w, http.StatusOK, trigger)
}

// handleCandidateMode switches inferred candidates between observe and auto.
func (app *App) handleCandidateMode(w http.ResponseWriter, r *http.Request) {
	request, err := decode[modeRequest](r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	if err := app.Store.SetCandidateMode(r.Context(), request.Mode, "me（信道）", app.now()); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, request)
}

// handleJobStop cancels a queued or running preparation job.
func (app *App) handleJobStop(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResponse{"unknown job"})
		return
	}
	if _, err := decode[struct{}](r); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	job, err := app.Store.CancelJob(r.Context(), id, "me（信道）", app.now())
	if !writeActionError(w, err) {
		return
	}
	app.Preparer.Stop(id)
	writeJSON(w, http.StatusOK, job)
}

// handlePause pauses or resumes all processing.
func (app *App) handlePause(w http.ResponseWriter, r *http.Request) {
	request, err := decode[pauseRequest](r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	changed, err := app.SetPaused(r.Context(), request.Paused)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pauseResponse{Paused: request.Paused, Changed: changed})
}

func (app *App) handleRoutine(w http.ResponseWriter, r *http.Request) {
	id, err := app.Scheduler.RunNow(r.Context(), RoutineID(r.PathValue("id")))
	app.accepted(w, id, err)
}

func (app *App) accepted(w http.ResponseWriter, id int64, err error) {
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	app.Router.Wake()
	writeJSON(w, http.StatusAccepted, acceptedResponse{id})
}

// handleDecision is the approval gate's human side: only this endpoint can
// move a proposal out of pending. Approval hands it to the outbox, which
// executes it through MCP and reports the outcome on the channel. Retry hands
// a finally failed proposal back to the outbox under the same idempotency key.
func (app *App) handleDecision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	decision := r.PathValue("decision")
	if err != nil || (decision != "approve" && decision != "deny" && decision != "retry") {
		writeJSON(w, http.StatusNotFound, errorResponse{"unknown proposal decision"})
		return
	}
	now := app.now()
	if decision == "retry" {
		proposal, err := app.Store.RetryProposal(r.Context(), id, "me（信道）", now)
		if !writeActionError(w, err) {
			return
		}
		app.Outbox.Wake()
		writeJSON(w, http.StatusOK, proposal)
		return
	}
	approve := decision == "approve"
	proposal, err := app.Store.DecideProposal(r.Context(), id, approve, "me（信道）", now)
	if !writeActionError(w, err) {
		return
	}
	if approve {
		app.Outbox.Wake()
	} else {
		_, _ = app.Store.Post(r.Context(), ChannelMessage{Role: RoleSystem, Text: fmt.Sprintf("已取消提案 #%d，什么都没有发出。", proposal.ID), TriggerID: proposal.TriggerID}, now)
		// A denial brings the thread's tasks forward.
		app.Scheduler.Wake()
	}
	writeJSON(w, http.StatusOK, proposal)
}
