package decisions

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

type decisionContractCase struct {
	Name            string
	Input, Accepted json.RawMessage
	Error           string
}
type decisionBoundCase struct {
	Char         string
	Count, Bytes int
	Error        string
}
type decisionHTTPCall struct {
	Method, URL, Authorization string
	ContentType                string `json:"content_type"`
	Body                       Value
}
type decisionHTTPCase struct {
	Name            string
	Statuses        []int
	Header          string
	Body, Exception *string
	Result          json.RawMessage
	Error           string
	Calls           []decisionHTTPCall
	Delays          []float64
	BorrowedOpen    bool `json:"borrowed_open"`
}
type decisionFixture struct {
	Requests, Results []decisionContractCase
	Bounds            []decisionBoundCase
	HTTP              []decisionHTTPCase
}

func loadDecisions(t *testing.T) decisionFixture {
	t.Helper()
	b, e := os.ReadFile("../testdata/python-decisions.json")
	if e != nil {
		t.Fatal(e)
	}
	var f decisionFixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func sameJSON(t *testing.T, got []byte, want []byte) {
	t.Helper()
	a, e := DecodeValue(got)
	if e != nil {
		t.Fatal(e)
	}
	b, e := DecodeValue(want)
	if e != nil {
		t.Fatal(e)
	}
	if !sameDescription(a, b) {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	// Object order is canonical, but source integer/float spelling must survive
	// decoding and emission so that compact UTF-8 byte budgets remain truthful.
	x, err := a.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	y, err := b.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(x, y) {
		t.Fatalf("numeric wire shape: got %s\nwant %s", x, y)
	}
}
func fixtureRequest(t *testing.T, f decisionFixture) Request {
	t.Helper()
	r, e := DecodeRequest(f.Requests[0].Input)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestDecisionRequestsMatchActualPythonContracts(t *testing.T) {
	f := loadDecisions(t)
	for _, row := range f.Requests {
		t.Run(row.Name, func(t *testing.T) {
			r, e := DecodeRequest(row.Input)
			if row.Error != "" {
				if e == nil || e.Error() != row.Error {
					t.Fatal(e, row.Error)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			b, e := r.MarshalJSON()
			if e != nil {
				t.Fatal(e)
			}
			sameJSON(t, b, row.Accepted)
		})
	}
	for _, row := range f.Bounds {
		t.Run("UTF8-"+row.Char, func(t *testing.T) {
			q, e := NewNoul(StringValue("True?"))
			if e != nil {
				t.Fatal(e)
			}
			r, e := NewRequest(StringValue(strings.Repeat(row.Char, row.Count)), map[string]Question{"q": q})
			if row.Error != "" {
				if e == nil || e.Error() != row.Error {
					t.Fatal(e, row.Error)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			b, e := r.MarshalJSON()
			if e != nil || len(b) != row.Bytes {
				t.Fatal(len(b), row.Bytes, e)
			}
		})
	}
}
func TestDecisionResultsMatchActualPythonContracts(t *testing.T) {
	f := loadDecisions(t)
	request := fixtureRequest(t, f)
	for _, row := range f.Results {
		t.Run(row.Name, func(t *testing.T) {
			r, e := DecodeResult(request, row.Input)
			if row.Error != "" {
				if e == nil || e.Error() != row.Error {
					t.Fatal(e, row.Error)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			b, e := r.MarshalJSON()
			if e != nil {
				t.Fatal(e)
			}
			sameJSON(t, b, row.Accepted)
		})
	}
}
func TestDecisionSnapshotsDetachAllMutableSurfaces(t *testing.T) {
	f := loadDecisions(t)
	request := fixtureRequest(t, f)
	original, _ := request.MarshalJSON()
	state, _ := request.State().Object()
	state["ticket"] = StringValue("changed")
	questions := request.Questions()
	delete(questions, "department")
	q := request.Questions()["urgency"]
	levels, _ := q.ScoreCriteria()
	levels[0] = StringValue("changed")
	obj, _ := q.Instructions().Object()
	obj["question"] = StringValue("changed")
	choices, _ := request.Questions()["department"].ChoiceCriteria()
	choices["billing"] = StringValue("changed")
	after, _ := request.MarshalJSON()
	if !bytes.Equal(original, after) {
		t.Fatal(string(after))
	}
	r, e := DecodeResult(request, f.Results[0].Input)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := r.MarshalJSON()
	answers := r.Answers()
	a, _ := answers["urgency"].Score()
	a.Probabilities["1"] = 0
	a.Legend["1"] = StringValue("changed")
	c, _ := answers["department"].Choice()
	c.Probabilities["billing"] = 0
	delete(answers, "refund")
	r.Usage().InputTokens = 0
	after, _ = r.MarshalJSON()
	if !bytes.Equal(before, after) {
		t.Fatal(string(after))
	}
	// Constructor inputs also become independent immutable snapshots.
	criteria := map[string]Value{"a": StringValue("A"), "b": NullValue()}
	cq, e := NewChoice(StringValue("Which?"), criteria)
	if e != nil {
		t.Fatal(e)
	}
	criteria["a"] = BoolValue(true)
	rs := map[string]Question{"q": cq}
	rq, e := NewRequest(StringValue(""), rs)
	if e != nil {
		t.Fatal(e)
	}
	delete(rs, "q")
	cp := map[string]float64{"a": 1, "b": 0}
	ar := ChoiceResult(ChoiceAnswer{Choice: "a", Confidence: 1, Probabilities: cp})
	cp["a"] = 0
	usage := &TokenUsage{1, 2}
	result, e := NewResult(rq, "custom", "m", LLMEstimate, map[string]Answer{"q": ar}, usage)
	if e != nil {
		t.Fatal(e)
	}
	usage.InputTokens = -1
	if e = ValidateResult(rq, result); e != nil {
		t.Fatal(e)
	}
}
func TestDecisionBoundaryRejectsDuplicateInvalidUnicodeAndNonfiniteData(t *testing.T) {
	for _, input := range []string{`{"x":1,"x":2}`, `{"x":{"q":0,"q":1}}`, `"\ud800"`, `"\udfff"`, `"\ud800\u0041"`, `{"x":1e999}`, `NaN`, `[] []`} {
		if _, e := DecodeValue([]byte(input)); e == nil {
			t.Fatal(input)
		}
	}
	for _, input := range []string{`"\ud83d\ude00"`, `"\\ud800"`, `{"\"":null}`} {
		if _, e := DecodeValue([]byte(input)); e != nil {
			t.Fatal(input, e)
		}
	}
	if _, e := DecodeValue([]byte{'"', 0xff, '"'}); e == nil {
		t.Fatal("invalid UTF8 accepted")
	}
	for _, text := range []string{"nan", "+1", "01", "-Inf", "1e999"} {
		if _, e := NumberValue(text); e == nil {
			t.Fatal(text)
		}
	}
	v := StringValue(string([]byte{0xff}))
	q, _ := NewNoul(StringValue("Yes?"))
	if _, e := NewRequest(v, map[string]Question{"q": q}); e == nil {
		t.Fatal("invalid string accepted")
	}
	req, _ := NewRequest(StringValue(""), map[string]Question{"q": q})
	for _, x := range []float64{math.NaN(), math.Inf(1), -1, 1.1} {
		if _, e := NewResult(req, "custom", "m", LLMEstimate, map[string]Answer{"q": NoulResult(NoulAnswer{x})}, nil); e == nil {
			t.Fatal(x)
		}
	}
	if _, e := NewResult(Request{}, "custom", "m", Jev, nil, nil); e == nil {
		t.Fatal("zero request accepted")
	}
}
func TestDecisionNumbersPreserveSourceFloatEncodingAndLegendEquality(t *testing.T) {
	for _, row := range []struct{ in, want string }{{"1.0", "1.0"}, {"1e-7", "1e-07"}, {"-0.0", "-0.0"}, {"1e15", "1000000000000000.0"}, {"1e16", "1e+16"}, {"-0", "0"}} {
		v, e := NumberValue(row.in)
		if e != nil {
			t.Fatal(e)
		}
		b, e := v.MarshalJSON()
		if e != nil || string(b) != row.want {
			t.Fatal(row, string(b), e)
		}
	}
	a, _ := NumberValue("9007199254740992")
	b, _ := NumberValue("9007199254740993")
	if sameDescription(a, b) {
		t.Fatal("large integer legend equality rounded")
	}
	b, _ = NumberValue("9007199254740992.0")
	if !sameDescription(a, b) {
		t.Fatal("source int/float equality lost")
	}
	a, _ = NumberValue("1")
	if !sameDescription(a, BoolValue(true)) {
		t.Fatal("source boolean equality lost")
	}
}
func TestDecisionTypedConstructorsAndResponseBudget(t *testing.T) {
	q, e := NewNoulWithCriteria(StringValue("Yes?"), map[string]Value{"true": NullValue(), "false": ArrayValue(nil)})
	if e != nil {
		t.Fatal(e)
	}
	m, ok := q.NoulCriteria()
	if !ok || len(m) != 2 {
		t.Fatal(m, ok)
	}
	m["other"] = NullValue()
	if len(q.booleanCriteria) != 2 {
		t.Fatal("noul criteria alias")
	}
	s, e := NewScore(ObjectValue(map[string]Value{"question": StringValue("How?")}), []Value{StringValue("low"), StringValue("high")})
	if e != nil {
		t.Fatal(e)
	}
	req, e := NewRequest(ArrayValue([]Value{BoolValue(true)}), map[string]Question{"q": s})
	if e != nil {
		t.Fatal(e)
	}
	a := ScoreResult(ScoreAnswer{Score: .7, Confidence: .6, Probabilities: map[string]float64{"0": .3, "1": .7}, Legend: map[string]Value{"0": StringValue("low"), "1": StringValue("high")}})
	r, e := NewResult(req, "custom", "m", LLMEstimate, map[string]Answer{"q": a}, nil)
	if e != nil || r.Provider() != "custom" || r.Model() != "m" || r.ProbabilitySource() != LLMEstimate || r.Usage() != nil {
		t.Fatal(r, e)
	}
	if _, e := NewResult(req, "custom", strings.Repeat("x", MaxResponseBytes), Jev, map[string]Answer{"q": a}, nil); e == nil || !strings.Contains(e.Error(), "byte limit") {
		t.Fatal(e)
	}
	nested := StringValue("value")
	for i := 0; i < 40; i++ {
		nested = ArrayValue([]Value{nested})
	}
	if _, e = NewRequest(nested, map[string]Question{"q": q}); e == nil || !strings.Contains(e.Error(), "nesting") {
		t.Fatal(e)
	}
}
