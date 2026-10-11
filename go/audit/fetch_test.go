package audit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchDeadlineCancellationAndBorrowedClientPolicy(t *testing.T) {
	client := &http.Client{Timeout: time.Minute}
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > 10*time.Second || remaining < 9*time.Second {
			t.Fatal("Source ten-second request budget missing", remaining)
		}
		return nil, context.DeadlineExceeded
	})
	if _, err := Fetch(context.Background(), "http://fixture.invalid", client); !errors.Is(err, context.DeadlineExceeded) || client.Timeout != time.Minute {
		t.Fatal("deadline/client policy", err, client.Timeout)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	result := make(chan error, 1)
	go func() { _, err := Fetch(ctx, server.URL, nil); result <- err }()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request ignored caller cancellation")
	}
}

func TestFetchRejectsFailedOrMalformedOrOversizedHealth(t *testing.T) {
	for _, row := range []struct {
		name, body string
		status     int
	}{
		{"status", "private-error-body", 503},
		{"syntax", "{", 200},
		{"trailing", "{} {}", 200},
		{"type", `{"build":4}`, 200},
		{"pid-type", `{"pid":"17"}`, 200},
		{"pid-overflow", `{"pid":99999999999999999999999}`, 200},
		{"oversized", strings.Repeat(" ", MaxReportBytes+1), 200},
	} {
		t.Run(row.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(row.status)
				io.WriteString(w, row.body)
			}))
			defer server.Close()
			if _, err := Fetch(context.Background(), server.URL, nil); err == nil || strings.Contains(err.Error(), "private-error-body") {
				t.Fatal("accepted invalid health or leaked error body", err)
			}
		})
	}
}
