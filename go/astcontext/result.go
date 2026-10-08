package astcontext

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Operation identifies one semantic read; CLI command spelling is kept separate.
type Operation uint8

const (
	InvalidOperation Operation = iota
	RepoMap
	FileOutline
	ShowSymbol
	SymbolReferences
)

func (o Operation) String() string {
	switch o {
	case RepoMap:
		return "repo_map"
	case FileOutline:
		return "file_outline"
	case ShowSymbol:
		return "show_symbol"
	case SymbolReferences:
		return "symbol_references"
	}
	return ""
}
func (o Operation) command() string {
	switch o {
	case RepoMap:
		return "digest"
	case FileOutline:
		return "outline"
	case ShowSymbol:
		return "show"
	case SymbolReferences:
		return "grep"
	}
	return ""
}
func ParseOperation(value string) (Operation, error) {
	for _, o := range []Operation{RepoMap, FileOutline, ShowSymbol, SymbolReferences} {
		if o.String() == value {
			return o, nil
		}
	}
	return InvalidOperation, errors.New("unknown AST operation")
}

// PayloadValue is inert immutable JSON with closed null/text/number/bool/container
// variants. Unknown CLI members are preserved as data and confer no authority.
type PayloadValue = jsonvalue.Value
type Payload struct{ body PayloadValue }

func NewPayload(body PayloadValue) (Payload, error) {
	if body.Kind() != jsonvalue.Object {
		return Payload{}, errors.New("AST payload must be an object")
	}
	return Payload{body}, nil
}
func (p Payload) Value() PayloadValue { return p.body }

type ResultParameters struct {
	Status      Status
	Operation   Operation
	Data        *Payload
	Probe       *Probe
	Warnings    []string
	Message     string
	OutputBytes int
}

// Result owns detached optional records and warnings. Zero results cannot publish.
type Result struct {
	parameters ResultParameters
	valid      bool
}

func NewResult(p ResultParameters) (Result, error) {
	if p.Status < Applied || p.Status > Incompatible || p.Operation.command() == "" || p.OutputBytes < 0 {
		return Result{}, errors.New("invalid AST result")
	}
	if p.Data != nil && p.Data.body.Kind() != jsonvalue.Object {
		return Result{}, errors.New("invalid AST payload")
	}
	if p.Probe != nil && (p.Probe.status < Applied || p.Probe.status > Incompatible) {
		return Result{}, errors.New("invalid AST probe")
	}
	return Result{parameters: detachResult(p), valid: true}, nil
}
func detachResult(p ResultParameters) ResultParameters {
	p.Warnings = append([]string(nil), p.Warnings...)
	if p.Data != nil {
		copy := *p.Data
		p.Data = &copy
	}
	if p.Probe != nil {
		copy := *p.Probe
		p.Probe = &copy
	}
	return p
}
func (r Result) Parameters() ResultParameters { return detachResult(r.parameters) }
func (r Result) Value() (PayloadValue, error) {
	if !r.valid {
		return PayloadValue{}, errors.New("uninitialized AST result")
	}
	p := r.parameters
	warnings := make([]jsonvalue.Value, len(p.Warnings))
	for i, w := range p.Warnings {
		warnings[i] = jsonvalue.TextValue(w)
	}
	fields := []jsonvalue.Field{{Name: "status", Value: jsonvalue.TextValue(p.Status.String())}, {Name: "operation", Value: jsonvalue.TextValue(p.Operation.String())}, {Name: "warnings", Value: jsonvalue.ArrayValue(warnings)}}
	if p.Probe != nil {
		probe := p.Probe.Snapshot()
		pf := []jsonvalue.Field{{Name: "status", Value: jsonvalue.TextValue(probe.Status.String())}, {Name: "version", Value: jsonvalue.TextValue(probe.Version)}, {Name: "homepage", Value: jsonvalue.TextValue(probe.Homepage)}, {Name: "schema_version", Value: jsonvalue.IntegerValue(int64(probe.SchemaVersion))}}
		if probe.Message != "" {
			pf = append(pf, jsonvalue.Field{Name: "message", Value: jsonvalue.TextValue(probe.Message)})
		}
		fields = append(fields, jsonvalue.Field{Name: "probe", Value: jsonvalue.ObjectValue(pf)})
	}
	if p.Message != "" {
		fields = append(fields, jsonvalue.Field{Name: "message", Value: jsonvalue.TextValue(p.Message)})
	}
	if p.Data != nil {
		fields = append(fields, jsonvalue.Field{Name: "data", Value: p.Data.body})
	}
	if p.OutputBytes != 0 {
		fields = append(fields, jsonvalue.Field{Name: "output_bytes", Value: jsonvalue.IntegerValue(int64(p.OutputBytes))})
	}
	return jsonvalue.ObjectValue(fields), nil
}
func (r Result) MarshalJSON() ([]byte, error) {
	v, err := r.Value()
	if err != nil {
		return nil, err
	}
	return v.MarshalJSON()
}

// ToJSON retains Source's historical nonfinite values at the final UTF-8 boundary.
func (r Result) ToJSON() (string, error) {
	v, err := r.Value()
	if err != nil {
		return "", err
	}
	raw, err := v.MarshalLegacyUTF8()
	return string(raw), err
}
