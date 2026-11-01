package assistant

import (
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

// WorkersView shows how many worker turns may run at once and how many do.
type WorkersView struct {
	Capacity int `json:"capacity"`
	Running  int `json:"running"`
}

// StateView is everything the simulated channel page renders.
type StateView struct {
	Model       string           `json:"model"`
	Now         time.Time        `json:"now"`
	Integration IntegrationView  `json:"integration"`
	Workers     WorkersView      `json:"workers"`
	Channel     []ChannelMessage `json:"channel"`
	Proposals   []Proposal       `json:"proposals"`
	Tasks       []Task           `json:"tasks"`
	Inbox       []Email          `json:"inbox"`
	Sent        []Email          `json:"sent"`
	Calendar    []CalendarEvent  `json:"calendar"`
	Ledger      []LedgerEntry    `json:"ledger"`
	Triggers    []Trigger        `json:"triggers"`
	Routines    []Routine        `json:"routines"`
	Presets     []PresetInfo     `json:"presets"`
}

type chatRequest struct {
	Text string `json:"text"`
}

type simulateRequest struct {
	Preset Preset `json:"preset"`
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
	mux.HandleFunc("POST /api/routines/{id}/run", app.handleRoutine)
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
	}, Workers: WorkersView{Capacity: app.Router.Capacity()}}
	var err error
	steps := []func() error{
		func() error { view.Channel, err = app.Store.Channel(ctx, 200); return err },
		func() error { view.Proposals, err = app.Store.Proposals(ctx, 50); return err },
		func() error { view.Tasks, err = app.Store.Tasks(ctx, 40); return err },
		func() error { view.Workers.Running, err = app.Store.CountTriggers(ctx, TriggerRunning); return err },
		func() error { view.Ledger, err = app.Store.Ledger(ctx, 150); return err },
		func() error { view.Triggers, err = app.Store.Triggers(ctx, 50); return err },
		func() error { view.Routines, err = app.Store.Routines(ctx); return err },
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

// handleTask lets the user check a task now or cancel it. Workers complete
// tasks; only the user cancels them.
func (app *App) handleTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	action := r.PathValue("action")
	if err != nil || (action != "wake" && action != "cancel") {
		writeJSON(w, http.StatusNotFound, errorResponse{"unknown task action"})
		return
	}
	if _, err := decode[struct{}](r); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	now := app.now()
	var task Task
	if action == "wake" {
		task, err = app.Store.WakeTask(r.Context(), id, "用户点了立即检查", "me（信道）", now)
	} else {
		task, err = app.Store.CloseTask(r.Context(), id, TaskCancelled, "用户取消", "me（信道）", now)
	}
	switch {
	case errors.Is(err, ErrTaskClosed):
		writeJSON(w, http.StatusConflict, errorResponse{err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	if action == "wake" {
		app.Scheduler.Wake()
	} else {
		_, _ = app.Store.Post(r.Context(), ChannelMessage{Role: RoleSystem, Text: fmt.Sprintf("已取消任务 #%d「%s」，不再自动检查。", task.ID, task.Title), TriggerID: task.LastTrigger}, now)
	}
	writeJSON(w, http.StatusOK, task)
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
// executes it through MCP and reports the outcome on the channel.
func (app *App) handleDecision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	decision := r.PathValue("decision")
	if err != nil || (decision != "approve" && decision != "deny") {
		writeJSON(w, http.StatusNotFound, errorResponse{"unknown proposal decision"})
		return
	}
	approve := decision == "approve"
	now := app.now()
	proposal, err := app.Store.DecideProposal(r.Context(), id, approve, "me（信道）", now)
	switch {
	case errors.Is(err, ErrProposalDecided):
		writeJSON(w, http.StatusConflict, errorResponse{err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
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
