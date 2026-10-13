package httpapi

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

type queryBenchmarkObservation struct {
	Caller   Principal
	Token    scalarQueryValue
	Envelope scalarQueryValue
}

var queryBenchmarkResult queryBenchmarkObservation

// Compare the prior two URL.Query parses and principal context with the owned
// decode-once admission path, using well-formed queries with equal observations.
// Store, HTTP I/O, response encoding and model execution are outside this measure.
func BenchmarkHTTPQueryAdmission(b *testing.B) {
	queries := []struct{ Name, Raw string }{
		{"no-query", ""},
		{"short", "access_token=token-a&envelope=true"},
		{"repeated", strings.Repeat("access_token=ignored&envelope=false&", 32) + "access_token=token-a&envelope=true"},
		{"unrelated", strings.Repeat("unused="+strings.Repeat("x", 512)+"&", 64) + "access_token=token-a&envelope=true"},
	}
	caller := Principal{ID: "alice"}
	ctx := context.Background()
	for _, query := range queries {
		b.Run(query.Name+"/previous", func(b *testing.B) {
			requestURL := &url.URL{RawQuery: query.Raw}
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				admission := context.WithValue(ctx, principalKey{}, caller)
				var token, envelope scalarQueryValue
				if query.Raw != "" {
					tokens := requestURL.Query()["access_token"]
					envelopes := requestURL.Query()["envelope"]
					token = scalarQueryValue{Text: tokens[len(tokens)-1], Present: true}
					envelope = scalarQueryValue{Text: envelopes[len(envelopes)-1], Present: true}
				}
				queryBenchmarkResult = queryBenchmarkObservation{
					Caller:   admission.Value(principalKey{}).(Principal),
					Token:    token,
					Envelope: envelope,
				}
			}
			if queryBenchmarkResult.Caller != caller || query.Raw != "" && (queryBenchmarkResult.Token.Text != "token-a" || queryBenchmarkResult.Envelope.Text != "true") || query.Raw == "" && (queryBenchmarkResult.Token.Present || queryBenchmarkResult.Envelope.Present) {
				b.Fatal(queryBenchmarkResult)
			}
		})
		b.Run(query.Name+"/owned", func(b *testing.B) {
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				values := parseScalarQuery(query.Raw)
				admission := withAdmission(ctx, caller, values)
				queryBenchmarkResult = queryBenchmarkObservation{
					Caller: principal(admission), Token: values.AccessToken, Envelope: admittedQueries(admission).Envelope,
				}
			}
			if queryBenchmarkResult.Caller != caller || query.Raw != "" && (queryBenchmarkResult.Token.Text != "token-a" || queryBenchmarkResult.Envelope.Text != "true") || query.Raw == "" && (queryBenchmarkResult.Token.Present || queryBenchmarkResult.Envelope.Present) {
				b.Fatal(queryBenchmarkResult)
			}
		})
	}
}
