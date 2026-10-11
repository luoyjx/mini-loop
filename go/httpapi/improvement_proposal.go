package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/improvement"
	"github.com/luoyjx/mini-loop/go/selfimprove"
)

type ImprovementRequest struct {
	Objective         string
	AcceptanceCommand string
	MaxRounds         int64
	ParentID          *improvement.ProposalID
}

var improvementIntegerText = regexp.MustCompile(`^[+-]?[0-9]+(?:_[0-9]+)*(?:\.0+)?$`)

func improvementInteger(v ValidationInput) (*big.Int, RequestValidationCode, string) {
	invalid := func(code RequestValidationCode, message string) (*big.Int, RequestValidationCode, string) {
		return nil, code, message
	}
	switch v.kind {
	case validationBool:
		if v.boolean {
			return big.NewInt(1), "", ""
		}
		return big.NewInt(0), "", ""
	case validationNumber:
		raw := string(v.number)
		if !strings.ContainsAny(raw, ".eE") {
			n, ok := new(big.Int).SetString(raw, 10)
			if ok {
				return n, "", ""
			}
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) && math.Trunc(f) == f {
			n, _ := new(big.Float).SetFloat64(f).Int(nil)
			return n, "", ""
		}
		return invalid("int_from_float", "Input should be a valid integer, got a number with a fractional part")
	case validationText:
		raw := strings.TrimSpace(v.text)
		if len(raw) > 4300 {
			return invalid("int_parsing_size", "Unable to parse input string as an integer, exceeded maximum size")
		}
		if improvementIntegerText.MatchString(raw) {
			raw = strings.SplitN(raw, ".", 2)[0]
			raw = strings.ReplaceAll(raw, "_", "")
			if n, ok := new(big.Int).SetString(raw, 10); ok {
				return n, "", ""
			}
		}
		return invalid("int_parsing", "Input should be a valid integer, unable to parse string as an integer")
	case validationNonfinite:
		return invalid("finite_number", "Input should be a finite number")
	default:
		return invalid("int_type", "Input should be a valid integer")
	}
}

func improvementValidation(input ValidationInput) (ImprovementRequest, []RequestValidationDetail) {
	result := ImprovementRequest{MaxRounds: 3}
	issue := func(code RequestValidationCode, field, message string, value ValidationInput, ctx *RequestValidationContext) RequestValidationDetail {
		loc := []ValidationLocation{{field: "body"}}
		if field != "" {
			loc = append(loc, ValidationLocation{field: field})
		}
		return RequestValidationDetail{code, loc, message, value, ctx}
	}
	if input.kind == validationNull {
		return result, []RequestValidationDetail{issue(validationMissing, "", "Field required", input, nil)}
	}
	if input.kind != validationObject {
		return result, []RequestValidationDetail{issue(validationModel, "", "Input should be a valid dictionary or object to extract fields from", input, nil)}
	}
	details := []RequestValidationDetail{}
	for _, field := range []string{"objective", "acceptance_command", "max_rounds", "parent_id"} {
		value := ValidationInput{}
		present := false
		for _, m := range input.members {
			if m.key == field {
				value = m.value
				present = true
				break
			}
		}
		if !present {
			if field == "objective" || field == "acceptance_command" {
				details = append(details, issue(validationMissing, field, "Field required", input, nil))
			}
			continue
		}
		if field == "max_rounds" {
			n, code, message := improvementInteger(value)
			if n == nil {
				details = append(details, issue(code, field, message, value, nil))
				continue
			}
			if n.Cmp(big.NewInt(1)) < 0 {
				min := 1
				details = append(details, issue("greater_than_equal", field, "Input should be greater than or equal to 1", value, &RequestValidationContext{GE: &min}))
			} else if n.Cmp(big.NewInt(10)) > 0 {
				max := 10
				details = append(details, issue("less_than_equal", field, "Input should be less than or equal to 10", value, &RequestValidationContext{LE: &max}))
			} else {
				result.MaxRounds = n.Int64()
			}
			continue
		}
		if field == "parent_id" && value.kind == validationNull {
			continue
		}
		if value.kind != validationText {
			details = append(details, issue(validationString, field, "Input should be a valid string", value, nil))
			continue
		}
		if field == "parent_id" {
			v := improvement.ProposalID(value.text)
			result.ParentID = &v
			continue
		}
		limit := 4000
		if field == "acceptance_command" {
			limit = 1000
		}
		length := requestRuneCount([]byte(value.text))
		if length < 1 {
			min := 1
			details = append(details, issue(validationShort, field, "String should have at least 1 character", value, &RequestValidationContext{MinLength: &min}))
		} else if length > limit {
			details = append(details, issue(validationLong, field, fmt.Sprintf("String should have at most %d characters", limit), value, &RequestValidationContext{MaxLength: &limit}))
		} else if field == "objective" {
			result.Objective = value.text
		} else {
			result.AcceptanceCommand = value.text
		}
	}
	return result, details
}

func (s *Server) proposeImprovement(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeRequestBody(s, w, r)
	if !ok {
		return
	}
	req, details := improvementValidation(input)
	if len(details) > 0 {
		writeRequestValidation(s, w, RequestValidationResponse{Detail: details})
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	run, err := agent.AuthenticatedHTTPRunContext(agent.ActorID(principal(r.Context()).ID))
	if err != nil {
		writeSelfAuditFailure(w)
		return
	}
	proposal, err := s.manager.ProposeImprovement(r.Context(), principal(r.Context()).ID, session.ID(), req.Objective, req.AcceptanceCommand, req.MaxRounds, req.ParentID, run)
	if err != nil {
		var admission *selfimprove.AdmissionError
		switch {
		case errors.Is(err, agent.ErrSessionNotFound):
			writeJSON(s, w, 404, ErrorResponse{fmt.Sprintf("No session '%s'", session.ID())})
		case errors.Is(err, agent.ErrSessionBusy):
			writeJSON(s, w, 409, ErrorResponse{fmt.Sprintf("session %s is running a turn", session.ID())})
		case errors.As(err, &admission):
			writeJSON(s, w, 400, ErrorResponse{admission.Error()})
		default:
			writeSelfAuditFailure(w)
		}
		return
	}
	// Source returns this proposal without a second registered-secret mask.
	data, err := json.Marshal(proposal)
	if err != nil {
		writeSelfAuditFailure(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
