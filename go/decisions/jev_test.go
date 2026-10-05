package decisions

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type decisionRoundTripper func(*http.Request) (*http.Response, error)

func (f decisionRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type decisionTimeout struct{}

func (decisionTimeout) Error() string { return "private-response-secret" }
func (decisionTimeout) Timeout() bool { return true }
func decisionResultBody(t *testing.T, f decisionFixture) []byte {
	t.Helper()
	v, e := DecodeValue(f.Results[0].Input)
	if e != nil {
		t.Fatal(e)
	}
	delete(v.object, "provider")
	delete(v.object, "probability_source")
	b, e := v.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestJevHTTPMatchesActualPythonWireRetriesAndSanitizedErrors(t *testing.T) {
	f := loadDecisions(t)
	request := fixtureRequest(t, f)
	for _, row := range f.HTTP {
		t.Run(row.Name, func(t *testing.T) {
			var calls []decisionHTTPCall
			transport := decisionRoundTripper(func(r *http.Request) (*http.Response, error) {
				b, e := io.ReadAll(r.Body)
				if e != nil {
					t.Fatal(e)
				}
				v, e := DecodeValue(b)
				if e != nil {
					t.Fatal(e)
				}
				calls = append(calls, decisionHTTPCall{r.Method, r.URL.String(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), v})
				if row.Exception != nil {
					if *row.Exception == "ReadTimeout" {
						return nil, decisionTimeout{}
					}
					return nil, errors.New("private-response-secret")
				}
				body := decisionResultBody(t, f)
				if row.Body != nil {
					body = []byte(*row.Body)
				}
				status := row.Statuses[min(len(calls)-1, len(row.Statuses)-1)]
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{row.Header}}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
			})
			borrowed := &http.Client{Transport: transport}
			cfg := DefaultJevConfig("test-private-key")
			cfg.Client = borrowed
			p, e := NewJev(cfg)
			if e != nil {
				t.Fatal(e)
			}
			result, e := p.Evaluate(context.Background(), request)
			if row.Error != "" {
				if e == nil || e.Error() != row.Error || strings.Contains(e.Error(), "private-") {
					t.Fatal(e, row.Error)
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				b, e := result.MarshalJSON()
				if e != nil {
					t.Fatal(e)
				}
				sameJSON(t, b, row.Result)
			}
			if len(calls) != len(row.Calls) || !row.BorrowedOpen || borrowed.CheckRedirect != nil {
				t.Fatal(len(calls), len(row.Calls), row.BorrowedOpen)
			}
			for i, v := range calls {
				want := row.Calls[i]
				a, _ := v.Body.MarshalJSON()
				b, _ := want.Body.MarshalJSON()
				sameJSON(t, a, b)
				v.Body = Value{}
				want.Body = Value{}
				if !reflect.DeepEqual(v, want) {
					t.Fatal(v, want)
				}
			}
			for i, delay := range row.Delays {
				if got := retryDelay(row.Header, i).Seconds(); got != delay {
					t.Fatal(got, delay)
				}
			}
		})
	}
}
func TestJevTotalDeadlineCancellationResponseLimitAndBodyClose(t *testing.T) {
	f := loadDecisions(t)
	request := fixtureRequest(t, f)
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancel"}[cancelled], func(t *testing.T) {
			entered := make(chan struct{})
			client := &http.Client{Transport: decisionRoundTripper(func(r *http.Request) (*http.Response, error) {
				close(entered)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			cfg := DefaultJevConfig("key")
			cfg.Client = client
			cfg.Timeout = 20 * time.Millisecond
			p, _ := NewJev(cfg)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, e := p.Evaluate(ctx, request); done <- e }()
			<-entered
			if cancelled {
				cancel()
			}
			select {
			case e := <-done:
				if cancelled {
					if !errors.Is(e, context.Canceled) {
						t.Fatal(e)
					}
				} else if e == nil || e.Error() != "TypeSafe decision request timed out." {
					t.Fatal(e)
				}
			case <-time.After(time.Second):
				t.Fatal("did not cancel")
			}
		})
	}
	reader := &decisionCountingBody{remaining: MaxResponseBytes * 2}
	client := &http.Client{Transport: decisionRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader, Request: r}, nil
	})}
	cfg := DefaultJevConfig("key")
	cfg.Client = client
	p, _ := NewJev(cfg)
	_, e := p.Evaluate(context.Background(), request)
	if e == nil || e.Error() != "TypeSafe decision response exceeds the byte limit." || reader.read != MaxResponseBytes+1 || !reader.closed {
		t.Fatal(e, reader)
	}
	closed := false
	client.Transport = decisionRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"999"}}, Body: decisionCloseBody{Reader: strings.NewReader("private-response-secret"), closed: &closed}, Request: r}, nil
	})
	cfg.Timeout = 20 * time.Millisecond
	p, _ = NewJev(cfg)
	started := time.Now()
	_, e = p.Evaluate(context.Background(), request)
	if e == nil || e.Error() != "TypeSafe decision request timed out." || !closed || time.Since(started) > time.Second {
		t.Fatal(e, closed)
	}
}

type decisionCountingBody struct {
	remaining, read int
	closed          bool
}

func (b *decisionCountingBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.remaining)
	for i := 0; i < n; i++ {
		p[i] = ' '
	}
	b.remaining -= n
	b.read += n
	return n, nil
}
func (b *decisionCountingBody) Close() error { b.closed = true; return nil }

type decisionCloseBody struct {
	io.Reader
	closed *bool
}

func (b decisionCloseBody) Close() error { *b.closed = true; return nil }
func TestJevFixedEndpointNoRedirectAndConcurrentBorrowedTransport(t *testing.T) {
	f := loadDecisions(t)
	request := fixtureRequest(t, f)
	body := decisionResultBody(t, f)
	var mu sync.Mutex
	calls := 0
	local := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		if r.Method != "POST" || r.Host != "api.typesafe.ai" || r.URL.Path != "/v1/systemone" {
			t.Error(r.Method, r.Host, r.URL)
		}
		w.Header().Set("Location", "https://other.invalid/private")
		w.WriteHeader(http.StatusFound)
	}))
	defer local.Close()
	transport := local.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig.ServerName = local.Certificate().DNSNames[0]
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", local.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	redirectCalled := false
	borrowed := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { redirectCalled = true; return nil }}
	cfg := DefaultJevConfig("key")
	cfg.Client = borrowed
	p, _ := NewJev(cfg)
	_, e := p.Evaluate(context.Background(), request)
	if e == nil || e.Error() != "TypeSafe decision request failed (HTTP 302)." || redirectCalled || calls != 1 {
		t.Fatal(e, redirectCalled, calls)
	}
	borrowed.Transport = decisionRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
	p, _ = NewJev(cfg)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := p.Evaluate(context.Background(), request)
			if e != nil || r.Model() != "jev-1.13.0" {
				t.Error(r, e)
			}
		}()
	}
	wg.Wait()
}
func TestJevConstructionIsOfflineAndBounded(t *testing.T) {
	cfg := DefaultJevConfig("key")
	p, e := NewJev(cfg)
	if e != nil || p.Model() != "jev-latest" {
		t.Fatal(p, e)
	}
	for _, mutate := range []func(*JevConfig){func(c *JevConfig) { c.APIKey = " " }, func(c *JevConfig) { c.APIKey = "key\nprivate" }, func(c *JevConfig) { c.Model = "" }, func(c *JevConfig) { c.Model = strings.Repeat("界", 129) }, func(c *JevConfig) { c.Timeout = 0 }, func(c *JevConfig) { c.Timeout = 121 * time.Second }, func(c *JevConfig) { c.MaxRetries = -1 }, func(c *JevConfig) { c.MaxRetries = 4 }} {
		v := cfg
		mutate(&v)
		if _, e := NewJev(v); e == nil || !IsValidation(e) || strings.Contains(e.Error(), "private") {
			t.Fatal(e)
		}
	}
	if _, e = p.Evaluate(context.Background(), Request{}); e == nil || !IsValidation(e) {
		t.Fatal(e)
	}
}
