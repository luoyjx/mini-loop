package httpapi

import (
	"context"
	"fmt"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func startSSE(w http.ResponseWriter) error {
	if _, ok := w.(http.Flusher); !ok {
		return fmt.Errorf("streaming unsupported")
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	return http.NewResponseController(w).Flush()
}
func writeEvent(s *Server, w http.ResponseWriter, record agent.SessionEventRecord, envelope bool) error {
	data, err := recordingJSON(s, record)
	if err != nil {
		return err
	}
	name := string(record.Event.Kind())
	if envelope {
		name = "agent_event"
	}
	if _, err = fmt.Fprintf(w, "id: %d\r\nevent: %s\r\ndata: %s\r\n\r\n", record.Sequence, name, data); err != nil {
		return err
	}
	return http.NewResponseController(w).Flush()
}
func ping(w http.ResponseWriter) error {
	if _, err := fmt.Fprint(w, ": ping\r\n\r\n"); err != nil {
		return err
	}
	return http.NewResponseController(w).Flush()
}

// eventCursor follows Python int's whitespace/sign/decimal/underscore syntax.
// Negative/invalid IDs select the fresh backlog. Huge positive IDs saturate,
// preserving the source behavior of suppressing all representable sequences.
func eventCursor(value string) agent.EventSequence {
	value = strings.TrimFunc(value, func(r rune) bool { return pytext.Space(r) && (r < 0x1c || r > 0x1f) })
	if strings.HasPrefix(value, "+") {
		value = value[1:]
	} else if strings.HasPrefix(value, "-") {
		return 0
	}
	if value == "" {
		return 0
	}
	// Python 3.11 default integer conversion bound, pinned by the source probe.
	const maxCursorDigits = 4300
	digits := 0
	var result uint64
	digitBefore := false
	for _, r := range value {
		if r == '_' {
			if !digitBefore {
				return 0
			}
			digitBefore = false
			continue
		}
		digit, ok := pytext.DecimalDigit(r)
		if !ok {
			return 0
		}
		digits++
		if digits > maxCursorDigits {
			return 0
		}
		digitBefore = true
		n := uint64(digit - '0')
		if result > (math.MaxUint64-n)/10 {
			result = math.MaxUint64
		} else {
			result = result*10 + n
		}
	}
	if !digitBefore {
		return 0
	}
	return agent.EventSequence(result)
}

func (s *Server) observe(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	envelope := false
	if value := r.URL.Query().Get("envelope"); value != "" {
		var err error
		envelope, err = strconv.ParseBool(value)
		if err != nil {
			writeJSON(s, w, 422, ErrorResponse{"invalid envelope"})
			return
		}
	}
	cursor := eventCursor(r.Header.Get("Last-Event-ID"))
	subscription := session.Subscribe(true)
	defer subscription.Close()
	caught, err := session.CatchUpEvents(r.Context(), cursor)
	if err != nil {
		if r.Context().Err() == nil {
			writeJSON(s, w, http.StatusServiceUnavailable, ErrorResponse{"event catch-up failed"})
		}
		return
	}
	if err := startSSE(w); err != nil {
		return
	}
	for _, record := range caught {
		if r.Context().Err() != nil || writeEvent(s, w, record, envelope) != nil {
			return
		}
		cursor = record.Sequence
	}
	ticker := time.NewTicker(s.ping)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if ping(w) != nil {
				return
			}
		case record, open := <-subscription.Events():
			if !open {
				return
			}
			if record.Sequence <= cursor {
				continue
			}
			if writeEvent(s, w, record, envelope) != nil {
				return
			}
			cursor = record.Sequence
		}
	}
}
func (s *Server) messageStream(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBody[MessageRequest](s, w, r)
	if !ok {
		return
	}
	if req.Message == nil {
		writeJSON(s, w, 422, ErrorResponse{"message is required"})
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	caller := principal(r.Context())
	if !s.rate(w, caller) {
		return
	}
	run, err := agent.AuthenticatedHTTPRunContext(agent.ActorID(caller.ID))
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"message identity failed"})
		return
	}
	subscription := session.Subscribe(false)
	defer subscription.Close()
	if err = startSSE(w); err != nil {
		return
	}
	done := make(chan struct{})
	// A live source probe confirms disconnect cancels this submitted turn,
	// including a queued caller, rather than an unrelated admission holder.
	turnContext, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() { defer close(done); session.RunWithContext(turnContext, *req.Message, run) }()
	ticker := time.NewTicker(s.ping)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if ping(w) != nil {
				return
			}
		case record, open := <-subscription.Events():
			if !open || writeEvent(s, w, record, false) != nil {
				return
			}
		case <-done:
			for {
				select {
				case record, open := <-subscription.Events():
					if !open || writeEvent(s, w, record, false) != nil {
						return
					}
				default:
					return
				}
			}
		}
	}
}
