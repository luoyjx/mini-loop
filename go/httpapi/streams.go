package httpapi

import (
	"context"
	"fmt"
	"github.com/luoyjx/mini-loop/go/agent"
	"net/http"
	"strconv"
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
	cursor, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	subscription := session.Subscribe(true)
	defer subscription.Close()
	if err := startSSE(w); err != nil {
		return
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
			if int64(record.Sequence) <= cursor {
				continue
			}
			if writeEvent(s, w, record, envelope) != nil {
				return
			}
			cursor = int64(record.Sequence)
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
