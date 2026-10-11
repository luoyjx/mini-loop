package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestTargetTransportOwnsWireCloneAndProxySelection(t *testing.T) {
	for _, forwardProxy := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "forward-proxy"}[forwardProxy], func(t *testing.T) {
			var dials, policies atomic.Int32
			observed := make(chan string, 3)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed <- r.RequestURI
				_, _ = io.WriteString(w, "ok")
			}))
			defer server.Close()
			proxy, _ := url.Parse(server.URL)
			const endpoint = "http://fixture.invalid/a|b"
			const path = "/a|b/v1/messages"
			const absolute = endpoint + "/v1/messages"
			caller := &http.Transport{Proxy: func(request *http.Request) (*url.URL, error) {
				policies.Add(1)
				if request.URL.String() != absolute || request.URL.RawPath != path {
					t.Error("proxy policy did not receive Source metadata", request.URL)
				}
				if forwardProxy {
					return proxy, nil
				}
				return nil, nil
			}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}, MaxIdleConns: 7}
			defer caller.CloseIdleConnections()
			client, err := New(Config{BaseURL: endpoint, HTTPClient: &http.Client{Transport: caller}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			owned, ok := client.http.Transport.(*targetTransport)
			if !ok || owned.base == caller || owned.base.MaxIdleConns != 7 || caller.Proxy == nil {
				t.Fatal("caller transport ownership or configuration changed")
			}
			for i := 0; i < 3; i++ {
				if i == 2 {
					client.CloseIdleConnections()
				}
				request, err := client.target.request(context.Background(), []byte("{}"))
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.http.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				_, readErr := io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if readErr != nil || response.Request != request || request.URL.String() != absolute || request.URL.RawPath != path {
					t.Fatal("response or caller request metadata changed", readErr)
				}
				want := path
				if forwardProxy {
					want = absolute
				}
				if actual := <-observed; actual != want {
					t.Fatalf("wire target %q; want %q", actual, want)
				}
			}
			if policies.Load() != 3 || dials.Load() != 2 {
				t.Fatal("proxy called more than once, or idle pool closure/reuse failed", policies.Load(), dials.Load())
			}
		})
	}
}

func TestTargetTransportProxyFailurePreventsDial(t *testing.T) {
	want := errors.New("proxy policy refused")
	var calls, dials int
	caller := &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
		calls++
		return nil, want
	}, DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("unexpected dial")
	}}
	client, err := New(Config{BaseURL: "http://fixture.invalid/a^b", HTTPClient: &http.Client{Transport: caller}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	request, err := client.target.request(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	before := request.URL.String()
	_, err = client.http.Do(request)
	if !errors.Is(err, want) || calls != 1 || dials != 0 || request.URL.String() != before {
		t.Fatal("proxy refusal boundary changed", err, calls, dials)
	}
}
