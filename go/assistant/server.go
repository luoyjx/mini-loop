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

// StateView is everything the simulated channel page renders.
type StateView struct {
	Model     string           `json:"model"`
	Now       time.Time        `json:"now"`
	Channel   []ChannelMessage `json:"channel"`
	Proposals []Proposal       `json:"proposals"`
	Inbox     []Email          `json:"inbox"`
	Sent      []Email          `json:"sent"`
	Calendar  []CalendarEvent  `json:"calendar"`
	Ledger    []LedgerEntry    `json:"ledger"`
	Triggers  []Trigger        `json:"triggers"`
	Routines  []Routine        `json:"routines"`
	Presets   []PresetInfo     `json:"presets"`
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
	mux.HandleFunc("POST /api/proposals/{id}/{decision}", app.handleDecision)
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
	view := StateView{Model: app.ModelLabel, Now: now, Presets: Presets}
	var err error
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	steps := []func() error{
		func() error { view.Channel, err = app.Store.Channel(ctx, 200); return err },
		func() error { view.Proposals, err = app.Store.Proposals(ctx, 50); return err },
		func() error { view.Inbox, err = app.Store.ListMail(ctx, FolderInbox, 50); return err },
		func() error { view.Sent, err = app.Store.ListMail(ctx, FolderSent, 50); return err },
		func() error { view.Calendar, err = app.Store.Events(ctx, day, day.AddDate(0, 0, 7)); return err },
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
	id, err := app.Store.Deliver(r.Context(), request.Preset, app.now())
	app.accepted(w, id, err)
}

func (app *App) handleCustom(w http.ResponseWriter, r *http.Request) {
	request, err := decode[CustomMail](r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	id, err := app.Store.DeliverCustom(r.Context(), request, app.now())
	app.accepted(w, id, err)
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
// move a proposal out of pending, and approval applies the effect atomically.
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
	text := fmt.Sprintf("已取消提案 #%d，什么都没有发出。", proposal.ID)
	if approve {
		text = fmt.Sprintf("已按你的批准发出提案 #%d：%s → %s", proposal.ID, proposal.Subject, proposal.To)
	}
	_, _ = app.Store.Post(r.Context(), ChannelMessage{Role: RoleSystem, Text: text, TriggerID: proposal.TriggerID}, now)
	writeJSON(w, http.StatusOK, proposal)
}
