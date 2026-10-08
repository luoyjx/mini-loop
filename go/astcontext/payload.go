package astcontext

import (
	"fmt"
	"github.com/luoyjx/mini-loop/go/internal/jsonsyntax"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"math/big"
	"strings"
)

// PayloadIterationError preserves ordinary Source TypeError escapes. It is not
// synthesized into an empty collection or an applied semantic result.
type PayloadIterationError struct{ kind jsonvalue.Kind }

func (e PayloadIterationError) Error() string {
	name := "NoneType"
	switch e.kind {
	case jsonvalue.Integer:
		name = "int"
	case jsonvalue.Float:
		name = "float"
	case jsonvalue.Boolean:
		name = "bool"
	}
	return fmt.Sprintf("'%s' object is not iterable", name)
}
func payloadItems(v jsonvalue.Value) ([]jsonvalue.Value, error) {
	switch v.Kind() {
	case jsonvalue.Array:
		items, _ := v.Array()
		return items, nil
	case jsonvalue.Object:
		items := make([]jsonvalue.Value, 0, len(v.Keys()))
		for _, k := range v.Keys() {
			items = append(items, jsonvalue.TextValue(k))
		}
		return items, nil
	case jsonvalue.Text:
		text, _ := v.Text()
		items := make([]jsonvalue.Value, 0, jsonvalue.RuneCount(text))
		for text != "" {
			head := jsonvalue.TextPrefix(text, 1)
			items = append(items, jsonvalue.TextValue(head))
			text = text[len(head):]
		}
		return items, nil
	default:
		return nil, PayloadIterationError{v.Kind()}
	}
}
func payloadMember(v jsonvalue.Value, key string, fallback jsonvalue.Value) jsonvalue.Value {
	if child, ok := v.Lookup(key); ok {
		return child
	}
	return fallback
}
func rewritePayload(v jsonvalue.Value, rewrites []snapshotRewrite) jsonvalue.Value {
	switch v.Kind() {
	case jsonvalue.Text:
		text, _ := v.Text()
		for _, r := range rewrites {
			text = strings.ReplaceAll(text, r.snapshot, r.workspace)
		}
		return jsonvalue.TextValue(text)
	case jsonvalue.Array:
		items, _ := v.Array()
		for i, item := range items {
			items[i] = rewritePayload(item, rewrites)
		}
		return jsonvalue.ArrayValue(items)
	case jsonvalue.Object:
		fields := make([]jsonvalue.Field, 0, len(v.Keys()))
		for _, k := range v.Keys() {
			child, _ := v.Lookup(k)
			fields = append(fields, jsonvalue.Field{Name: k, Value: rewritePayload(child, rewrites)})
		}
		return jsonvalue.ObjectValue(fields)
	default:
		return v
	}
}
func removeImports(v jsonvalue.Value) (jsonvalue.Value, error) {
	files := payloadMember(v, "files", jsonvalue.ArrayValue(nil))
	items, err := payloadItems(files)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if files.Kind() != jsonvalue.Array {
		return v, nil
	}
	for i, item := range items {
		if item.Kind() != jsonvalue.Object {
			continue
		}
		fields := []jsonvalue.Field{}
		for _, k := range item.Keys() {
			if k == "imports" || k == "conditional_imports_count" || k == "import_regions" {
				continue
			}
			child, _ := item.Lookup(k)
			fields = append(fields, jsonvalue.Field{Name: k, Value: child})
		}
		items[i] = jsonvalue.ObjectValue(fields)
	}
	fields := make([]jsonvalue.Field, 0, len(v.Keys()))
	for _, k := range v.Keys() {
		child, _ := v.Lookup(k)
		if k == "files" {
			child = jsonvalue.ArrayValue(items)
		}
		fields = append(fields, jsonvalue.Field{Name: k, Value: child})
	}
	return jsonvalue.ObjectValue(fields), nil
}
func metricTotal(v jsonvalue.Value, keys ...string) *big.Int {
	total := new(big.Int)
	if v.Kind() == jsonvalue.Array {
		items, _ := v.Array()
		for _, child := range items {
			total.Add(total, metricTotal(child, keys...))
		}
		return total
	}
	if v.Kind() != jsonvalue.Object {
		return total
	}
	for _, key := range v.Keys() {
		child, _ := v.Lookup(key)
		matched := false
		for _, wanted := range keys {
			if key == wanted {
				matched = true
				break
			}
		}
		if !matched {
			total.Add(total, metricTotal(child, keys...))
			continue
		}
		switch child.Kind() {
		case jsonvalue.Boolean:
			b, _ := child.Bool()
			if b {
				total.Add(total, big.NewInt(1))
			}
		case jsonvalue.Integer:
			literal, _ := child.Integer()
			n, ok := new(big.Int).SetString(literal, 10)
			if ok && n.Sign() > 0 {
				total.Add(total, n)
			}
		case jsonvalue.Array:
			items, _ := child.Array()
			total.Add(total, big.NewInt(int64(len(items))))
		case jsonvalue.Object:
			total.Add(total, big.NewInt(int64(len(child.Keys()))))
		}
	}
	return total
}
func payloadAmbiguous(op Operation, v jsonvalue.Value) (bool, error) {
	if op != ShowSymbol {
		return false, nil
	}
	items, err := payloadItems(payloadMember(v, "results", jsonvalue.ArrayValue(nil)))
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.Kind() != jsonvalue.Object {
			continue
		}
		ambiguous, _ := item.Lookup("ambiguous")
		b, ok := ambiguous.Bool()
		matches := payloadMember(item, "matches", jsonvalue.ArrayValue(nil))
		list, isList := matches.Array()
		if (ok && b) || (isList && len(list) > 1) {
			return true, nil
		}
	}
	return false, nil
}
func payloadNoMatch(op Operation, v jsonvalue.Value) (bool, error) {
	if op == SymbolReferences {
		summary := payloadMember(v, "summary", jsonvalue.ObjectValue(nil))
		count, _ := summary.Lookup("total_matches")
		if summary.Kind() == jsonvalue.Object && count.PythonEqual(jsonvalue.IntegerValue(0)) {
			return true, nil
		}
		files := payloadMember(v, "files", jsonvalue.ArrayValue(nil))
		if !files.Truth() {
			return false, nil
		}
		items, err := payloadItems(files)
		if err != nil {
			return false, err
		}
		for _, item := range items {
			if item.Kind() == jsonvalue.Object {
				matches, _ := item.Lookup("matches")
				if matches.Truth() {
					return false, nil
				}
			}
		}
		return true, nil
	}
	if op == ShowSymbol {
		results := payloadMember(v, "results", jsonvalue.ArrayValue(nil))
		if !results.Truth() {
			return true, nil
		}
		items, err := payloadItems(results)
		if err != nil {
			return false, err
		}
		for _, item := range items {
			matches, _ := item.Lookup("matches")
			if item.Kind() != jsonvalue.Object || matches.Truth() {
				return false, nil
			}
		}
		return true, nil
	}
	files := payloadMember(v, "files", jsonvalue.ArrayValue(nil))
	items, isList := files.Array()
	return isList && len(items) == 0, nil
}

// classifyPayload interprets captured output only. Executable admission, actual
// subprocess lifetime and frozen source ownership stay in the Adapter caller.
func classifyPayload(op Operation, probe Probe, process processResult, includeImports *bool, rewrites []snapshotRewrite, schema int) (Result, error) {
	p := ResultParameters{Status: probe.status, Operation: op, Probe: &probe, Message: probe.message}
	if probe.status != Applied {
		return NewResult(p)
	}
	p.Status = process.status
	p.Message = ""
	p.OutputBytes = len(process.stdout)
	rewriteText := func(text string) string {
		v := rewritePayload(jsonvalue.TextValue(text), rewrites)
		out, _ := v.Text()
		return out
	}
	if process.status != Applied {
		p.Message = rewriteText(process.message)
		return NewResult(p)
	}
	if process.exitCode != 0 {
		detail := pytext.Strip(process.stderr)
		if detail == "" {
			detail = pytext.Strip(process.stdout)
		}
		p.Status = Error
		p.Message = fmt.Sprintf("ast-outline exited %d: %s", process.exitCode, rewriteText(detail))
		return NewResult(p)
	}
	if failure := jsonsyntax.Scan([]byte(process.stdout), jsonsyntax.Limits{Containers: 1000, IntegerDigits: 4300}); failure != nil {
		if failure.Code() == jsonsyntax.IntegerLimit {
			prefix := jsonvalue.TextPrefix(process.stdout, failure.Position())
			raw := process.stdout[len(prefix):]
			length := jsonsyntax.NumberLength([]byte(raw))
			digits := len(strings.TrimPrefix(raw[:length], "-"))
			return Result{}, PayloadIntegerError{digits: digits}
		}
		if failure.Code() == jsonsyntax.NestingLimit {
			return Result{}, jsonvalue.ErrDepth
		}
		prefix := jsonvalue.TextPrefix(process.stdout, failure.Position())
		line := strings.Count(prefix, "\n") + 1
		column := jsonvalue.RuneCount(prefix[strings.LastIndex(prefix, "\n")+1:]) + 1
		p.Status = Error
		p.Message = fmt.Sprintf("ast-outline returned invalid JSON: %s: line %d column %d (char %d)", failure.Message(), line, column, failure.Position())
		return NewResult(p)
	}
	v, err := jsonvalue.Decode(process.stdout)
	if err != nil {
		return Result{}, err
	}
	if v.Kind() != jsonvalue.Object {
		p.Status = Error
		p.Message = "ast-outline JSON must be an object"
		p.OutputBytes = 0
		return NewResult(p)
	}
	v = rewritePayload(v, rewrites)
	tool, _ := v.Lookup("tool")
	version, _ := v.Lookup("schema_version")
	command, _ := v.Lookup("command")
	if !tool.PythonEqual(jsonvalue.TextValue("ast-outline")) || !version.PythonEqual(jsonvalue.IntegerValue(int64(schema))) || !command.PythonEqual(jsonvalue.TextValue(op.command())) {
		p.Status = Incompatible
		p.Message = fmt.Sprintf("unexpected ast-outline JSON envelope: expected tool=ast-outline, schema_version=%d, command=%s", schema, op.command())
		return NewResult(p)
	}
	if reported, ok := v.Lookup("error"); ok {
		notes := payloadMember(reported, "notes", jsonvalue.ArrayValue(nil))
		hint := payloadMember(reported, "hint", jsonvalue.TextValue(""))
		items, err := payloadItems(notes)
		if err != nil {
			return Result{}, err
		}
		parts := make([]string, len(items))
		for i, item := range items {
			parts[i] = item.PythonString()
		}
		p.Message = strings.Join(parts, "; ")
		if hint.Truth() {
			if p.Message != "" {
				p.Message += " (hint: " + hint.PythonString() + ")"
			} else {
				p.Message = hint.PythonString()
			}
		}
		if p.Message == "" {
			p.Message = "ast-outline reported an error"
		}
		p.Status = Error
		payload, _ := NewPayload(v)
		p.Data = &payload
		return NewResult(p)
	}
	if includeImports != nil && !*includeImports {
		v, err = removeImports(v)
		if err != nil {
			return Result{}, err
		}
	}
	notes, err := payloadItems(payloadMember(v, "notes", jsonvalue.ArrayValue(nil)))
	if err != nil {
		return Result{}, err
	}
	for _, note := range notes {
		if text, ok := note.Text(); ok {
			p.Warnings = append(p.Warnings, text)
		}
	}
	parseErrors := metricTotal(v, "error_count", "parse_errors")
	truncated := metricTotal(v, "truncated_count")
	if parseErrors.Sign() != 0 {
		text, err := metricText(parseErrors)
		if err != nil {
			return Result{}, err
		}
		p.Warnings = append(p.Warnings, "ast_parse_errors:"+text)
	}
	if truncated.Sign() != 0 {
		text, err := metricText(truncated)
		if err != nil {
			return Result{}, err
		}
		p.Warnings = append(p.Warnings, "ast_matches_truncated:"+text)
	}
	p.Status = Applied
	if parseErrors.Sign() != 0 || truncated.Sign() != 0 {
		p.Status = Partial
	} else {
		ambiguous, err := payloadAmbiguous(op, v)
		if err != nil {
			return Result{}, err
		}
		if ambiguous {
			p.Status = Ambiguous
		} else {
			empty, err := payloadNoMatch(op, v)
			if err != nil {
				return Result{}, err
			}
			if empty {
				p.Status = NoMatch
			}
		}
	}
	payload, _ := NewPayload(v)
	p.Data = &payload
	return NewResult(p)
}
