package traceview

import (
	"embed"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
)

// Assets preserve the existing Python ledger's typography and interaction.
//
//go:embed style.css filter.js
var assets embed.FS
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&#x27;")

func escape(text string) string { return htmlEscaper.Replace(text) }
func Comma(value int64) string {
	text := fmt.Sprint(value)
	start := 0
	if value < 0 {
		start = 1
	}
	for i := len(text) - 3; i > start; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}
func FormatDuration(value *float64) string {
	if value == nil {
		return "in flight"
	}
	ms := *value
	if ms >= 60000 {
		return fmt.Sprintf("%.1f min", ms/60000)
	}
	if ms >= 1000 {
		return fmt.Sprintf("%.2f s", ms/1000)
	}
	return fmt.Sprintf("%.0f ms", ms)
}
func offset(ts, base *float64) string {
	if ts == nil || base == nil {
		return ""
	}
	return fmt.Sprintf("+%.1fs", math.Max(0, *ts-*base))
}
func capped(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + fmt.Sprintf("\n[... %s more characters -- export the trajectory for the full record]", Comma(int64(len(runes)-limit)))
}
func preview(text string) string {
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f }), " ")
	runes := []rune(text)
	if len(runes) > PreviewChars {
		return string(runes[:PreviewChars]) + "..."
	}
	return text
}
func overview(out *strings.Builder, ledger Ledger) {
	if ledger.StartedAt == nil || ledger.EndedAt == nil {
		return
	}
	total := math.Max(*ledger.EndedAt-*ledger.StartedAt, 1e-9)
	out.WriteString(`<div class="overview">`)
	for _, row := range ledger.Rows {
		if (row.Kind != Model && row.Kind != Tool) || row.Timestamp == nil {
			continue
		}
		left := math.Max(0, (*row.Timestamp-*ledger.StartedAt)/total*100)
		classes := "sp " + string(row.Kind)
		if row.Depth != 0 {
			classes += " nested"
		}
		title := escape(row.Label + " · " + FormatDuration(row.DurationMS))
		if row.DurationMS == nil {
			fmt.Fprintf(out, `<div class="%s open" style="left:%.2f%%" title="%s"></div>`, escape(classes), left, title)
			continue
		}
		if row.Error {
			classes += " err"
		}
		width := math.Max(*row.DurationMS/1000/total*100, .25)
		fmt.Fprintf(out, `<div class="%s" style="left:%.2f%%;width:%.2f%%" title="%s"></div>`, escape(classes), left, math.Min(width, 100-left), title)
	}
	out.WriteString(`</div>`)
}
func renderRow(out *strings.Builder, row Row, base *float64) {
	if row.Kind == Step {
		out.WriteString(`<hr class="step-rule"><div class="step-label">` + escape(row.Label) + `</div>`)
		return
	}
	classes := "row kind-" + string(row.Kind)
	if row.Error {
		classes += " err"
	}
	indent := ""
	if row.Depth != 0 {
		indent = fmt.Sprintf(` style="padding-left:%.1frem"`, float64(row.Depth)*1.4)
	}
	fmt.Fprintf(out, `<details class="%s"%s><summary>`, escape(classes), indent)
	seq := ""
	if row.Sequence != nil {
		seq = fmt.Sprint(*row.Sequence)
	}
	out.WriteString(`<span class="idx">` + escape(seq) + `</span><span class="off">` + escape(offset(row.Timestamp, base)) + `</span><span class="badge">` + escape(row.Label) + `</span>`)
	if row.Depth != 0 && row.Agent != nil && *row.Agent != "" {
		out.WriteString(`<span class="agent-chip">` + escape(*row.Agent) + `</span>`)
	}
	out.WriteString(`<span class="prev">` + escape(preview(row.Content)) + `</span>`)
	dur := row.Status
	if row.HasDuration {
		dur = FormatDuration(row.DurationMS)
	}
	if dur != "" {
		out.WriteString(`<span class="dur">` + escape(dur) + `</span>`)
	}
	out.WriteString(`</summary><div class="inspector">`)
	if row.Depth != 0 {
		agent := "None"
		if row.Agent != nil {
			agent = *row.Agent
		}
		out.WriteString(fmt.Sprintf(`<h4>Agent</h4><pre>%s (delegation depth %d)</pre>`, escape(agent), row.Depth))
	}
	for _, field := range row.Detail {
		out.WriteString(`<h4>` + escape(field.Name) + `</h4><pre>` + escape(capped(field.Text, InspectorChars)) + `</pre>`)
	}
	out.WriteString(`</div></details>`)
}

// Render is self-contained; all supplied text crosses the same HTML escaping boundary.
func Render(ledgers []Ledger, title string, generatedAt time.Time) string {
	var out strings.Builder
	css, _ := assets.ReadFile("style.css")
	js, _ := assets.ReadFile("filter.js")
	out.WriteString("<!doctype html><html><head><meta charset='utf-8'><title>" + escape(title) + "</title><meta name='viewport' content='width=device-width,initial-scale=1'><style>" + string(css) + "</style></head><body><h1>" + escape(title) + "</h1>")
	var totals Metrics
	session := ""
	for _, ledger := range ledgers {
		totals.ModelCalls += ledger.Metrics.ModelCalls
		totals.ToolCalls += ledger.Metrics.ToolCalls
		totals.ToolErrors += ledger.Metrics.ToolErrors
		totals.Errors += ledger.Metrics.Errors
		totals.InputTokens += ledger.Metrics.InputTokens
		totals.OutputTokens += ledger.Metrics.OutputTokens
		if session == "" {
			session = string(ledger.Session)
		}
	}
	out.WriteString(fmt.Sprintf(`<div class="meta">session %s · %d turn(s) · generated %s</div>`, escape(session), len(ledgers), generatedAt.Format("2006-01-02 15:04:05")))
	out.WriteString(`<div class="totals">`)
	for i, value := range []struct {
		name  string
		count int64
	}{{"model calls", int64(totals.ModelCalls)}, {"tool calls", int64(totals.ToolCalls)}, {"tool errors", int64(totals.ToolErrors)}, {"errors", int64(totals.Errors)}, {"input tokens", totals.InputTokens}, {"output tokens", totals.OutputTokens}} {
		if i > 0 {
			out.WriteString(" · ")
		}
		fmt.Fprintf(&out, `%s <b>%s</b>`, value.name, Comma(value.count))
	}
	out.WriteString(`</div><input id="q" type="search" placeholder="filter records">`)
	for i, ledger := range ledgers {
		status := string(ledger.Status)
		classes := "status"
		if status != "completed" && status != "running" {
			classes += " err"
		}
		dur := status
		if ledger.DurationMS != nil {
			dur = FormatDuration(ledger.DurationMS)
		}
		partial := ""
		if ledger.Partial {
			partial = " · partial"
		}
		fmt.Fprintf(&out, `<section class="turn"><div class="turn-head"><b>turn %d</b><span>%s</span><span class="%s">%s%s</span><span class="status">%s</span></div>`, i+1, escape(string(ledger.TrajectoryID)), classes, escape(status), partial, escape(dur))
		overview(&out, ledger)
		if ledger.Omitted != 0 {
			out.WriteString(fmt.Sprintf(`<div class="omitted">%s earlier records omitted from this page -- export the trajectory as JSONL for the full log</div>`, Comma(int64(ledger.Omitted))))
		}
		for _, row := range ledger.Rows {
			renderRow(&out, row, ledger.StartedAt)
		}
		out.WriteString(`</section>`)
	}
	out.WriteString("<script>" + string(js) + "</script></body></html>")
	return out.String()
}
