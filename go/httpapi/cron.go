package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/cron"
	"net/http"
	"strings"
	"unicode/utf8"
)

type CronListResponse struct {
	Session agent.SessionID     `json:"session"`
	Jobs    []agent.CronJobView `json:"jobs"`
}
type CronResultResponse struct {
	Session agent.SessionID `json:"session"`
	Result  string          `json:"result"`
}
type CronArmResponse struct {
	Session agent.SessionID `json:"session"`
	Job     cron.ID         `json:"job"`
	Armed   bool            `json:"armed"`
}

// Raw field bytes exist only in this JSON boundary. Live state stays typed.
type CronScheduleRequest struct {
	Cron, Prompt       string
	Recurring, Durable *bool
}

func (v *CronScheduleRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Cron      *string         `json:"cron"`
		Prompt    *string         `json:"prompt"`
		Recurring json.RawMessage `json:"recurring"`
		Durable   json.RawMessage `json:"durable"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Cron == nil || wire.Prompt == nil || utf8.RuneCountInString(*wire.Cron) < 1 || utf8.RuneCountInString(*wire.Cron) > 100 || len(*wire.Prompt) == 0 {
		return errors.New("invalid cron request")
	}
	r, err := cronHTTPBool(wire.Recurring)
	if err != nil {
		return err
	}
	d, err := cronHTTPBool(wire.Durable)
	if err != nil {
		return err
	}
	*v = CronScheduleRequest{*wire.Cron, *wire.Prompt, r, d}
	return nil
}
func cronHTTPBool(data []byte) (*bool, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if string(data) == "null" {
		return nil, errors.New("cron boolean cannot be null")
	}
	var value bool
	if json.Unmarshal(data, &value) == nil {
		return &value, nil
	}
	var text string
	if json.Unmarshal(data, &text) == nil {
		switch strings.ToLower(text) {
		case "0", "off", "f", "false", "n", "no":
			value = false
			return &value, nil
		case "1", "on", "t", "true", "y", "yes":
			value = true
			return &value, nil
		}
	}
	var number float64
	if json.Unmarshal(data, &number) == nil && (number == 0 || number == 1) {
		value = number == 1
		return &value, nil
	}
	return nil, errors.New("invalid cron boolean")
}

func (s *Server) cronFailure(w http.ResponseWriter, id agent.SessionID, err error) {
	switch {
	case errors.Is(err, agent.ErrSessionNotFound):
		writeJSON(s, w, 404, ErrorResponse{"No session '" + string(id) + "'"})
	case errors.Is(err, agent.ErrManagerStopped):
		writeJSON(s, w, 503, ErrorResponse{"session is closed"})
	default:
		writeJSON(s, w, 500, ErrorResponse{"cron operation failed"})
	}
}
func (s *Server) listCron(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	jobs, err := s.manager.CronJobs(principal(r.Context()).ID, session.ID())
	if err != nil {
		s.cronFailure(w, session.ID(), err)
		return
	}
	writeJSON(s, w, 200, CronListResponse{session.ID(), jobs})
}
func (s *Server) scheduleCron(w http.ResponseWriter, r *http.Request) {
	v, ok := decodeBody[CronScheduleRequest](s, w, r)
	if !ok {
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	job, err := s.manager.ScheduleCron(principal(r.Context()).ID, session.ID(), agent.ScheduleCronRequest{Cron: v.Cron, Prompt: v.Prompt, Recurring: v.Recurring, Durable: v.Durable})
	if err != nil {
		if strings.HasPrefix(err.Error(), "Error:") {
			writeJSON(s, w, 400, ErrorResponse{err.Error()})
		} else {
			s.cronFailure(w, session.ID(), err)
		}
		return
	}
	writeJSON(s, w, 200, CronResultResponse{session.ID(), job.Render()})
}
func (s *Server) cancelCron(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	result, err := s.manager.CancelCron(principal(r.Context()).ID, session.ID(), cron.ID(r.PathValue("job_id")))
	if err != nil {
		s.cronFailure(w, session.ID(), err)
		return
	}
	if strings.HasPrefix(result, "No cron") {
		writeJSON(s, w, 404, ErrorResponse{result})
		return
	}
	writeJSON(s, w, 200, CronResultResponse{session.ID(), result})
}
func (s *Server) armCron(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	job := cron.ID(r.PathValue("job_id"))
	result, err := s.manager.ArmCron(principal(r.Context()).ID, session.ID(), job)
	if err != nil {
		s.cronFailure(w, session.ID(), err)
		return
	}
	if strings.HasPrefix(result, "Error") {
		writeJSON(s, w, 404, ErrorResponse{result})
		return
	}
	writeJSON(s, w, 200, CronArmResponse{session.ID(), job, true})
}
