package decisions

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const (
	MaxQuestions     = 32
	MaxRequestBytes  = 128 * 1024
	MaxResponseBytes = 512 * 1024
	tolerance        = 1e-5
)

type ErrorKind string

const (
	ValidationError ErrorKind = "DecisionValidationError"
	ProviderError   ErrorKind = "DecisionError"
)

// Error is safe feedback: transport details, credentials and response bodies
// are never included. Validation messages describe contracts, not input data.
type Error struct {
	kind    ErrorKind
	message string
}

func (e *Error) Error() string   { return e.message }
func (e *Error) Kind() ErrorKind { return e.kind }
func invalid(s string) error     { return &Error{ValidationError, s} }
func failure(s string) error     { return &Error{ProviderError, s} }
func IsValidation(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.kind == ValidationError
}
func nonempty(s string) bool { return strings.TrimFunc(s, pytext.Space) != "" }

type Kind string

const (
	Choice Kind = "choice"
	Score  Kind = "score"
	Noul   Kind = "noul"
)

type ProbabilitySource string

const (
	Jev         ProbabilitySource = "jev"
	LLMEstimate ProbabilitySource = "llm_estimate"
)

// Question has one supported criterion shape; accessors return detached copies.
type Question struct {
	kind                          Kind
	instructions                  Value
	choices                       map[string]Value
	levels                        []Value
	booleanCriteria               map[string]Value
	criteriaPresent, criteriaNull bool
}

func NewChoice(instructions Value, criteria map[string]Value) (Question, error) {
	return questionFromValue(ObjectValue(map[string]Value{"type": StringValue(string(Choice)), "instructions": instructions, "criteria": ObjectValue(criteria)}))
}
func NewScore(instructions Value, criteria []Value) (Question, error) {
	return questionFromValue(ObjectValue(map[string]Value{"type": StringValue(string(Score)), "instructions": instructions, "criteria": ArrayValue(criteria)}))
}
func NewNoul(instructions Value) (Question, error) {
	return questionFromValue(ObjectValue(map[string]Value{"type": StringValue(string(Noul)), "instructions": instructions}))
}
func NewNoulWithCriteria(instructions Value, criteria map[string]Value) (Question, error) {
	return questionFromValue(ObjectValue(map[string]Value{"type": StringValue(string(Noul)), "instructions": instructions, "criteria": ObjectValue(criteria)}))
}
func (q Question) Kind() Kind          { return q.kind }
func (q Question) Instructions() Value { return q.instructions.Clone() }
func (q Question) ChoiceCriteria() (map[string]Value, bool) {
	return cloneObject(q.choices), q.kind == Choice
}
func (q Question) ScoreCriteria() ([]Value, bool) { return cloneValues(q.levels), q.kind == Score }
func (q Question) NoulCriteria() (map[string]Value, bool) {
	return cloneObject(q.booleanCriteria), q.kind == Noul
}
func (q Question) Clone() Question {
	q.instructions = q.instructions.Clone()
	q.choices = cloneObject(q.choices)
	q.levels = cloneValues(q.levels)
	q.booleanCriteria = cloneObject(q.booleanCriteria)
	return q
}
func content(v Value, required bool) bool {
	switch v.kind {
	case String:
		return !required || nonempty(v.text)
	case Array:
		return !required || len(v.array) > 0
	case Object:
		return !required || len(v.object) > 0
	}
	return false
}
func questionFromValue(v Value) (Question, error) {
	if v.kind != Object {
		return Question{}, invalid("Decision questions require nonempty string IDs and objects.")
	}
	for k := range v.object {
		if k != "type" && k != "instructions" && k != "criteria" {
			return Question{}, invalid("Decision question contains unsupported fields.")
		}
	}
	instructions := v.object["instructions"]
	if !content(instructions, true) {
		return Question{}, invalid("Decision questions require nonempty instructions.")
	}
	typ, _ := v.object["type"].Text()
	c, present := v.object["criteria"]
	q := Question{kind: Kind(typ), instructions: instructions.Clone(), criteriaPresent: present, criteriaNull: c.kind == Null}
	switch q.kind {
	case Choice:
		if c.kind != Object || len(c.object) < 1 || len(c.object) > 255 {
			return Question{}, invalid("Choice requires 1 to 255 named criteria.")
		}
		for k, x := range c.object {
			if !nonempty(k) || (x.kind != Null && !content(x, false)) {
				return Question{}, invalid("Choice criteria require names and JSON descriptions.")
			}
		}
		q.choices = cloneObject(c.object)
	case Score:
		if c.kind != Array || len(c.array) < 2 || len(c.array) > 10 {
			return Question{}, invalid("Score requires 2 to 10 ordered JSON descriptions.")
		}
		for _, x := range c.array {
			if !content(x, false) {
				return Question{}, invalid("Score requires 2 to 10 ordered JSON descriptions.")
			}
		}
		q.levels = cloneValues(c.array)
	case Noul:
		if c.kind != Null {
			if c.kind != Object {
				return Question{}, invalid("Noul criteria may describe true and false only.")
			}
			for k, x := range c.object {
				if (k != "true" && k != "false") || (x.kind != Null && !content(x, false)) {
					return Question{}, invalid("Noul criteria may describe true and false only.")
				}
			}
			q.booleanCriteria = cloneObject(c.object)
		}
	default:
		return Question{}, invalid("Decision type must be choice, score, or noul.")
	}
	return q, nil
}
func (q Question) value() Value {
	obj := map[string]Value{"type": StringValue(string(q.kind)), "instructions": q.instructions}
	if q.criteriaPresent {
		switch q.kind {
		case Choice:
			obj["criteria"] = ObjectValue(q.choices)
		case Score:
			obj["criteria"] = ArrayValue(q.levels)
		case Noul:
			if q.criteriaNull {
				obj["criteria"] = NullValue()
			} else {
				obj["criteria"] = ObjectValue(q.booleanCriteria)
			}
		}
	}
	return ObjectValue(obj)
}
func (q Question) MarshalJSON() ([]byte, error) { return q.value().MarshalJSON() }

type Request struct {
	state     Value
	questions map[string]Question
}

func NewRequest(state Value, questions map[string]Question) (Request, error) {
	if !content(state, false) {
		return Request{}, invalid("Decision state must be a string, object, or array.")
	}
	if len(questions) < 1 || len(questions) > MaxQuestions {
		return Request{}, invalid("Decision requests require 1 to 32 questions.")
	}
	r := Request{state: state.Clone(), questions: make(map[string]Question, len(questions))}
	for k, q := range questions {
		if !nonempty(k) {
			return Request{}, invalid("Decision questions require nonempty string IDs and objects.")
		}
		v, e := questionFromValue(q.value())
		if e != nil {
			return Request{}, e
		}
		r.questions[k] = v
	}
	if _, e := encodeValue(r.value(), MaxRequestBytes); e != nil {
		return Request{}, e
	}
	return r, nil
}
func DecodeRequest(b []byte) (Request, error) {
	// Raw wire whitespace is allowed within the response boundary; semantic
	// compact UTF-8 request size is checked separately, matching source budgets.
	v, e := DecodeValue(b)
	if e != nil {
		return Request{}, e
	}
	if v.kind != Object || len(v.object) != 2 {
		return Request{}, invalid("Decision request requires only state and questions.")
	}
	state, ok := v.object["state"]
	if !ok {
		return Request{}, invalid("Decision request requires only state and questions.")
	}
	questions, ok := v.object["questions"]
	if !ok || questions.kind != Object || len(questions.object) < 1 || len(questions.object) > MaxQuestions {
		return Request{}, invalid("Decision requests require 1 to 32 questions.")
	}
	qs := map[string]Question{}
	for k, x := range questions.object {
		if !nonempty(k) || x.kind != Object {
			return Request{}, invalid("Decision questions require nonempty string IDs and objects.")
		}
		q, e := questionFromValue(x)
		if e != nil {
			return Request{}, e
		}
		qs[k] = q
	}
	return NewRequest(state, qs)
}
func (r Request) State() Value { return r.state.Clone() }
func (r Request) Questions() map[string]Question {
	qs := make(map[string]Question, len(r.questions))
	for k, q := range r.questions {
		qs[k] = q.Clone()
	}
	return qs
}
func (r Request) Clone() Request { return Request{r.State(), r.Questions()} }

// Value returns the detached complete request structure for pre-encoding masks.
func (r Request) Value() Value { return r.value() }
func (r Request) value() Value {
	q := map[string]Value{}
	for k, v := range r.questions {
		q[k] = v.value()
	}
	return ObjectValue(map[string]Value{"state": r.state, "questions": ObjectValue(q)})
}
func (r Request) MarshalJSON() ([]byte, error) { return encodeValue(r.value(), MaxRequestBytes) }
func (r *Request) UnmarshalJSON(b []byte) error {
	v, e := DecodeRequest(b)
	if e == nil {
		*r = v
	}
	return e
}

type ChoiceAnswer struct {
	Choice        string
	Confidence    float64
	Probabilities map[string]float64
}
type ScoreAnswer struct {
	Score, Confidence float64
	Probabilities     map[string]float64
	Legend            map[string]Value
}
type NoulAnswer struct{ Probability float64 }

// answerNumberShape preserves integer-vs-float JSON spelling at the typed wire
// boundary. It contains only numeric representation flags, never raw payloads.
type answerNumberShape struct {
	confidenceInteger, scoreInteger, noulInteger bool
	probabilityIntegers                          map[string]bool
	probabilityOrder                             []string
}

func (s answerNumberShape) clone() answerNumberShape {
	s.probabilityOrder = append([]string(nil), s.probabilityOrder...)
	if s.probabilityIntegers != nil {
		m := make(map[string]bool, len(s.probabilityIntegers))
		for k, v := range s.probabilityIntegers {
			m[k] = v
		}
		s.probabilityIntegers = m
	}
	return s
}

type Answer struct {
	kind   Kind
	choice *ChoiceAnswer
	score  *ScoreAnswer
	noul   *NoulAnswer
	shape  answerNumberShape
}

func ChoiceResult(v ChoiceAnswer) Answer {
	v.Probabilities = cloneProbabilities(v.Probabilities)
	return Answer{kind: Choice, choice: &v}
}
func ScoreResult(v ScoreAnswer) Answer {
	v.Probabilities = cloneProbabilities(v.Probabilities)
	v.Legend = cloneObject(v.Legend)
	return Answer{kind: Score, score: &v}
}
func NoulResult(v NoulAnswer) Answer { return Answer{kind: Noul, noul: &v} }
func (a Answer) Kind() Kind          { return a.kind }
func (a Answer) Choice() (ChoiceAnswer, bool) {
	if a.choice == nil {
		return ChoiceAnswer{}, false
	}
	v := *a.choice
	v.Probabilities = cloneProbabilities(v.Probabilities)
	return v, true
}
func (a Answer) Score() (ScoreAnswer, bool) {
	if a.score == nil {
		return ScoreAnswer{}, false
	}
	v := *a.score
	v.Probabilities = cloneProbabilities(v.Probabilities)
	v.Legend = cloneObject(v.Legend)
	return v, true
}
func (a Answer) Noul() (NoulAnswer, bool) {
	if a.noul == nil {
		return NoulAnswer{}, false
	}
	return *a.noul, true
}
func (a Answer) Clone() Answer {
	var out Answer
	switch a.kind {
	case Choice:
		if v, ok := a.Choice(); ok {
			out = ChoiceResult(v)
		}
	case Score:
		if v, ok := a.Score(); ok {
			out = ScoreResult(v)
		}
	case Noul:
		if v, ok := a.Noul(); ok {
			out = NoulResult(v)
		}
	}
	out.shape = a.shape.clone()
	return out
}
func cloneProbabilities(v map[string]float64) map[string]float64 {
	if v == nil {
		return nil
	}
	out := make(map[string]float64, len(v))
	for k, p := range v {
		out[k] = p
	}
	return out
}
func probability(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }
func floatValue(f float64) (Value, error) {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return NumberValue(s)
}
func integerSpelling(v Value) bool { return v.kind == Number && !strings.ContainsAny(v.text, ".eE") }
func projectedNumber(f float64, integer bool) (Value, error) {
	if integer && math.Trunc(f) == f {
		return NumberValue(strconv.FormatFloat(f, 'f', 0, 64))
	}
	return floatValue(f)
}
func (a Answer) value() (Value, error) {
	obj := map[string]Value{"type": StringValue(string(a.kind))}
	var probs map[string]float64
	var confidence float64
	switch a.kind {
	case Noul:
		if a.noul == nil {
			return Value{}, invalid("Decision answer type must match its question.")
		}
		v, e := projectedNumber(a.noul.Probability, a.shape.noulInteger)
		if e != nil {
			return Value{}, e
		}
		obj["noul"] = v
		return ObjectValue(obj), nil
	case Choice:
		if a.choice == nil {
			return Value{}, invalid("Decision answer type must match its question.")
		}
		obj["choice"] = StringValue(a.choice.Choice)
		probs = a.choice.Probabilities
		confidence = a.choice.Confidence
	case Score:
		if a.score == nil {
			return Value{}, invalid("Decision answer type must match its question.")
		}
		v, e := projectedNumber(a.score.Score, a.shape.scoreInteger)
		if e != nil {
			return Value{}, e
		}
		obj["score"] = v
		obj["legend"] = ObjectValue(a.score.Legend)
		probs = a.score.Probabilities
		confidence = a.score.Confidence
	default:
		return Value{}, invalid("Decision answer type must match its question.")
	}
	v, e := projectedNumber(confidence, a.shape.confidenceInteger)
	if e != nil {
		return Value{}, e
	}
	obj["confidence"] = v
	p := map[string]Value{}
	for k, f := range probs {
		v, e := projectedNumber(f, a.shape.probabilityIntegers[k])
		if e != nil {
			return Value{}, e
		}
		p[k] = v
	}
	obj["probabilities"] = ObjectValue(p)
	return ObjectValue(obj), nil
}
func (a Answer) MarshalJSON() ([]byte, error) {
	v, e := a.value()
	if e != nil {
		return nil, e
	}
	return v.MarshalJSON()
}

// TokenUsage is optional as a whole: {} means statistics were unavailable.
// Partial usage is never synthesized. Counts are bounded signed Go integers.
type TokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
type Result struct {
	provider, model string
	source          ProbabilitySource
	answers         map[string]Answer
	usage           *TokenUsage
}

func NewResult(request Request, provider, model string, source ProbabilitySource, answers map[string]Answer, usage *TokenUsage) (Result, error) {
	r := Result{provider: provider, model: model, source: source, answers: make(map[string]Answer, len(answers))}
	for k, v := range answers {
		r.answers[k] = v.Clone()
	}
	if usage != nil {
		v := *usage
		r.usage = &v
	}
	if e := ValidateResult(request, r); e != nil {
		return Result{}, e
	}
	return r, nil
}
func (r Result) Provider() string                     { return r.provider }
func (r Result) Model() string                        { return r.model }
func (r Result) ProbabilitySource() ProbabilitySource { return r.source }
func (r Result) Answers() map[string]Answer {
	out := make(map[string]Answer, len(r.answers))
	for k, a := range r.answers {
		out[k] = a.Clone()
	}
	return out
}
func (r Result) Usage() *TokenUsage {
	if r.usage == nil {
		return nil
	}
	v := *r.usage
	return &v
}
func (r Result) value() (Value, error) {
	a := map[string]Value{}
	for k, v := range r.answers {
		x, e := v.value()
		if e != nil {
			return Value{}, e
		}
		a[k] = x
	}
	u := map[string]Value{}
	if r.usage != nil {
		x, _ := NumberValue(strconv.FormatInt(r.usage.InputTokens, 10))
		y, _ := NumberValue(strconv.FormatInt(r.usage.OutputTokens, 10))
		u["input_tokens"] = x
		u["output_tokens"] = y
	}
	return ObjectValue(map[string]Value{"provider": StringValue(r.provider), "model": StringValue(r.model), "probability_source": StringValue(string(r.source)), "answers": ObjectValue(a), "usage": ObjectValue(u)}), nil
}
func (r Result) MarshalJSON() ([]byte, error) {
	v, e := r.value()
	if e != nil {
		return nil, e
	}
	return encodeValue(v, MaxResponseBytes)
}
func sameDescription(a, b Value) bool {
	if a.kind != b.kind {
		if (a.kind == Number || a.kind == Boolean) && (b.kind == Number || b.kind == Boolean) {
			x, ok := exactNumeric(a)
			y, yes := exactNumeric(b)
			return ok && yes && x.Cmp(y) == 0
		}
		return false
	}
	switch a.kind {
	case Null:
		return true
	case String:
		return a.text == b.text
	case Number:
		x, ok := exactNumeric(a)
		y, yes := exactNumeric(b)
		return ok && yes && x.Cmp(y) == 0
	case Boolean:
		return a.boolean == b.boolean
	case Array:
		if len(a.array) != len(b.array) {
			return false
		}
		for i, v := range a.array {
			if !sameDescription(v, b.array[i]) {
				return false
			}
		}
		return true
	case Object:
		if len(a.object) != len(b.object) {
			return false
		}
		for k, v := range a.object {
			x, ok := b.object[k]
			if !ok || !sameDescription(v, x) {
				return false
			}
		}
		return true
	}
	return false
}
func exactNumeric(v Value) (*big.Rat, bool) {
	if v.kind == Boolean {
		if v.boolean {
			return big.NewRat(1, 1), true
		}
		return big.NewRat(0, 1), true
	}
	if v.kind != Number {
		return nil, false
	}
	if !strings.ContainsAny(v.text, ".eE") {
		x, ok := new(big.Int).SetString(v.text, 10)
		if !ok {
			return nil, false
		}
		return new(big.Rat).SetInt(x), true
	}
	x, e := strconv.ParseFloat(v.text, 64)
	if e != nil || math.IsInf(x, 0) || math.IsNaN(x) {
		return nil, false
	}
	return new(big.Rat).SetFloat64(x), true
}
func ValidateResult(request Request, r Result) error {
	if _, e := NewRequest(request.state, request.questions); e != nil {
		return e
	}
	if !nonempty(r.provider) || !nonempty(r.model) || (r.source != Jev && r.source != LLMEstimate) {
		return invalid("Decision result requires provider and model provenance.")
	}
	if r.usage != nil && (r.usage.InputTokens < 0 || r.usage.OutputTokens < 0) {
		return invalid("Decision usage requires nonnegative integer token counts.")
	}
	if len(r.answers) != len(request.questions) {
		return invalid("Decision answer IDs must exactly match the questions.")
	}
	for k := range request.questions {
		if _, ok := r.answers[k]; !ok {
			return invalid("Decision answer IDs must exactly match the questions.")
		}
	}
	for k, q := range request.questions {
		a := r.answers[k]
		if a.kind != q.kind {
			return invalid("Decision answer type must match its question.")
		}
		if q.kind == Noul {
			if a.noul == nil || !probability(a.noul.Probability) {
				return invalid("Noul answers require only a finite yes probability.")
			}
			continue
		}
		var probs map[string]float64
		var conf float64
		if q.kind == Choice {
			if a.choice == nil {
				return invalid("Decision answer type must match its question.")
			}
			probs = a.choice.Probabilities
			conf = a.choice.Confidence
		} else {
			if a.score == nil {
				return invalid("Decision answer type must match its question.")
			}
			probs = a.score.Probabilities
			conf = a.score.Confidence
		}
		if !probability(conf) {
			return invalid("Decision answer fields or confidence are invalid.")
		}
		expected := map[string]bool{}
		if q.kind == Choice {
			for k := range q.choices {
				expected[k] = true
			}
		} else {
			for i := range q.levels {
				expected[strconv.Itoa(i)] = true
			}
		}
		sum, maxP := 0.0, -1.0
		valid := len(probs) == len(expected)
		for _, name := range a.probabilityKeys(probs) {
			p := probs[name]
			valid = valid && expected[name] && probability(p)
			sum += p
			maxP = math.Max(maxP, p)
		}
		if !valid || math.Abs(sum-1) > tolerance {
			return invalid("Decision probabilities must cover the criteria and sum to one.")
		}
		if q.kind == Choice {
			if !expected[a.choice.Choice] || probs[a.choice.Choice] != maxP {
				return invalid("Choice must name a highest-probability criterion.")
			}
		} else {
			wanted := 0.0
			valid := len(a.score.Legend) == len(q.levels)
			for i, v := range q.levels {
				key := strconv.Itoa(i)

				x, ok := a.score.Legend[key]
				valid = valid && ok && sameDescription(x, v)
			}
			for _, key := range a.probabilityKeys(probs) {
				i, _ := strconv.Atoi(key)
				wanted += float64(i) * probs[key]
			}
			if !valid || math.IsNaN(a.score.Score) || math.IsInf(a.score.Score, 0) || a.score.Score < 0 || a.score.Score > float64(len(q.levels)-1) || math.Abs(a.score.Score-wanted) > tolerance {
				return invalid("Score and legend must match the requested rubric and probabilities.")
			}
		}
	}
	_, e := r.MarshalJSON()
	return e
}

type Provider interface {
	Evaluate(context.Context, Request) (Result, error)
}

func number(v Value) (float64, bool) {
	if v.kind != Number {
		return 0, false
	}
	x, e := strconv.ParseFloat(v.text, 64)
	return x, e == nil && !math.IsInf(x, 0) && !math.IsNaN(x)
}
func probabilityField(v Value) (float64, bool) { x, ok := number(v); return x, ok && probability(x) }
func answerFromValue(v Value) (Answer, error) {
	if v.kind != Object {
		return Answer{}, invalid("Decision answer type must match its question.")
	}
	typ, _ := v.object["type"].Text()
	if Kind(typ) == Noul {
		p, ok := probabilityField(v.object["noul"])
		if !ok || len(v.object) != 2 {
			return Answer{}, invalid("Noul answers require only a finite yes probability.")
		}
		out := NoulResult(NoulAnswer{p})
		out.shape.noulInteger = integerSpelling(v.object["noul"])
		return out, nil
	}
	required := map[string]bool{"type": true, "probabilities": true, "confidence": true}
	switch Kind(typ) {
	case Choice:
		required["choice"] = true
	case Score:
		required["score"] = true
		required["legend"] = true
	default:
		return Answer{}, invalid("Decision answer type must match its question.")
	}
	conf, ok := probabilityField(v.object["confidence"])
	valid := ok && len(v.object) == len(required)
	for k := range required {
		_, exists := v.object[k]
		valid = valid && exists
	}
	if !valid {
		return Answer{}, invalid("Decision answer fields or confidence are invalid.")
	}
	p := v.object["probabilities"]
	if p.kind != Object {
		return Answer{}, invalid("Decision probabilities must cover the criteria and sum to one.")
	}
	probs := map[string]float64{}
	for k, v := range p.object {
		x, ok := probabilityField(v)
		if !ok {
			return Answer{}, invalid("Decision probabilities must cover the criteria and sum to one.")
		}
		probs[k] = x
	}
	if Kind(typ) == Choice {
		choice, ok := v.object["choice"].Text()
		if !ok {
			return Answer{}, invalid("Choice must name a highest-probability criterion.")
		}
		out := ChoiceResult(ChoiceAnswer{choice, conf, probs})
		out.shape.confidenceInteger = integerSpelling(v.object["confidence"])
		out.shape.probabilityOrder = p.Keys()
		out.shape.probabilityIntegers = make(map[string]bool, len(p.object))
		for k, x := range p.object {
			out.shape.probabilityIntegers[k] = integerSpelling(x)
		}
		return out, nil
	}
	score, ok := number(v.object["score"])
	legend := v.object["legend"]
	if !ok || legend.kind != Object {
		return Answer{}, invalid("Score and legend must match the requested rubric and probabilities.")
	}
	out := ScoreResult(ScoreAnswer{score, conf, probs, cloneObject(legend.object)})
	out.shape.confidenceInteger = integerSpelling(v.object["confidence"])
	out.shape.scoreInteger = integerSpelling(v.object["score"])
	out.shape.probabilityOrder = p.Keys()
	out.shape.probabilityIntegers = make(map[string]bool, len(p.object))
	for k, x := range p.object {
		out.shape.probabilityIntegers[k] = integerSpelling(x)
	}
	return out, nil
}
func usageFromValue(v Value) (*TokenUsage, error) {
	if v.kind != Object || (len(v.object) != 0 && len(v.object) != 2) {
		return nil, invalid("Decision usage requires nonnegative integer token counts.")
	}
	if len(v.object) == 0 {
		return nil, nil
	}
	n := TokenUsage{}
	for k, v := range v.object {
		if (k != "input_tokens" && k != "output_tokens") || v.kind != Number || strings.ContainsAny(v.text, ".eE") {
			return nil, invalid("Decision usage requires nonnegative integer token counts.")
		}
		x, e := strconv.ParseInt(v.text, 10, 64)
		if e != nil || x < 0 {
			return nil, invalid("Decision usage requires nonnegative integer token counts.")
		}
		if k == "input_tokens" {
			n.InputTokens = x
		} else {
			n.OutputTokens = x
		}
	}
	return &n, nil
}
func resultFromValue(request Request, v Value, provider string, source ProbabilitySource) (Result, error) {
	if v.kind != Object {
		return Result{}, invalid("TypeSafe returned an invalid decision response.")
	}
	model, _ := v.object["model"].Text()
	if !nonempty(provider) || !nonempty(model) || (source != Jev && source != LLMEstimate) {
		return Result{}, invalid("Decision result requires provider and model provenance.")
	}
	u, e := usageFromValue(v.object["usage"])
	if e != nil {
		return Result{}, e
	}
	a := v.object["answers"]
	if a.kind != Object || len(a.object) != len(request.questions) {
		return Result{}, invalid("Decision answer IDs must exactly match the questions.")
	}
	for k := range request.questions {
		if _, ok := a.object[k]; !ok {
			return Result{}, invalid("Decision answer IDs must exactly match the questions.")
		}
	}
	answers := map[string]Answer{}
	for k, v := range a.object {
		typ, _ := v.object["type"].Text()
		if Kind(typ) != request.questions[k].kind {
			return Result{}, invalid("Decision answer type must match its question.")
		}
		x, e := answerFromValue(v)
		if e != nil {
			return Result{}, e
		}
		answers[k] = x
	}
	return NewResult(request, provider, model, source, answers, u)
}

// DecodeResult validates one complete custom-provider result against its request.
func DecodeResult(request Request, b []byte) (Result, error) {
	v, e := DecodeValue(b)
	if e != nil {
		return Result{}, e
	}
	if v.kind != Object {
		return Result{}, invalid("Decision result requires provider and model provenance.")
	}
	provider, _ := v.object["provider"].Text()
	source, _ := v.object["probability_source"].Text()
	return resultFromValue(request, v, provider, ProbabilitySource(source))
}

// Compile-time checks preserve explicit JSON boundaries and provider signatures.
var _ json.Marshaler = Request{}
