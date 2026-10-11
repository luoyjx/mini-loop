package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/protocol"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestCapturedProxyTLSWireMatchesSDK(t *testing.T) {
	type wireRequest struct {
		Method, Target, Version, Host string
		APIKey                        *string `json:"api_key"`
	}
	var fixture struct {
		Message json.RawMessage
		SSE     string
		Cases   []struct {
			Route, Name, Mode, Text string
			BaseURL                 string `json:"base_url"`
			Identity                Description
			Requests, Connects      []wireRequest
			SNIs                    []string `json:"snis"`
			Dials                   []struct {
				Host string
				Port json.RawMessage
			}
		}
	}
	data, err := os.ReadFile("../testdata/python-provider-transport-wire.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 24 {
		t.Fatal("transport inventory changed")
	}
	pair, err := tls.LoadX509KeyPair("../testdata/provider-wire-test-cert.pem", "../testdata/provider-wire-test-key.pem")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := os.ReadFile("../testdata/provider-wire-test-cert.pem")
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		t.Fatal("invalid test root")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Route+"/"+row.Name+"/"+row.Mode, func(t *testing.T) {
			var mu sync.Mutex
			requests, connects := []wireRequest{}, []wireRequest{}
			snis, dials := []string{}, []string{}
			capture := func(r *http.Request) wireRequest {
				var key *string
				if value := r.Header.Get("x-api-key"); value != "" {
					key = &value
				}
				return wireRequest{r.Method, r.RequestURI, r.Proto, r.Host, key}
			}
			reply := func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, capture(r))
				mu.Unlock()
				w.Header().Set("Connection", "close")
				if row.Mode == "stream" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, fixture.SSE)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(fixture.Message)
				}
			}
			origin := httptest.NewUnstartedServer(http.HandlerFunc(reply))
			origin.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, NextProtos: []string{"http/1.1"}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				mu.Lock()
				snis = append(snis, hello.ServerName)
				mu.Unlock()
				return nil, nil
			}}
			origin.StartTLS()
			defer origin.Close()
			var tunnels sync.WaitGroup
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "CONNECT" {
					reply(w, r)
					return
				}
				mu.Lock()
				connects = append(connects, capture(r))
				mu.Unlock()
				upstream, err := net.DialTimeout("tcp", origin.Listener.Addr().String(), 10*time.Second)
				if err != nil {
					t.Errorf("tunnel dial: %v", err)
					http.Error(w, "dial", 502)
					return
				}
				client, buffer, err := w.(http.Hijacker).Hijack()
				if err != nil {
					upstream.Close()
					t.Errorf("hijack: %v", err)
					return
				}
				tunnels.Add(1)
				defer tunnels.Done()
				defer client.Close()
				defer upstream.Close()
				_, err = buffer.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
				if err == nil {
					err = buffer.Flush()
				}
				if err != nil {
					t.Errorf("CONNECT response: %v", err)
					return
				}
				joined := make(chan struct{})
				go func() {
					defer close(joined)
					_, _ = io.Copy(upstream, buffer)
					if tcp, ok := upstream.(*net.TCPConn); ok {
						_ = tcp.CloseWrite()
					}
				}()
				_, _ = io.Copy(client, upstream)
				_ = client.Close()
				_ = upstream.Close()
				<-joined
			}))
			defer proxy.Close()
			proxyURL, err := url.Parse(proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				mu.Lock()
				dials = append(dials, address)
				mu.Unlock()
				if row.Route == "https-direct" {
					if address != "fixture.invalid:443" {
						return nil, fmt.Errorf("unexpected origin authority %s", address)
					}
					address = origin.Listener.Addr().String()
				} else if address != proxyURL.Host {
					return nil, fmt.Errorf("unexpected proxy authority %s", address)
				}
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}}
			if row.Route != "https-direct" {
				transport.Proxy = http.ProxyURL(proxyURL)
			}
			defer transport.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cfg := Config{BaseURL: row.BaseURL, APIKey: "fixture-key", HTTPClient: &http.Client{Transport: transport}}
			request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}}
			var response protocol.ModelReply
			var identity Description
			if row.Mode == "stream" {
				c, e := NewStreaming(cfg)
				err = e
				if err == nil {
					identity = c.Describe()
					response, err = c.CompleteStream(ctx, request, nil)
				}
			} else {
				c, e := New(cfg)
				err = e
				if err == nil {
					identity = c.Describe()
					response, err = c.Complete(ctx, request)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			transport.CloseIdleConnections()
			tunnels.Wait()
			if identity != row.Identity {
				t.Error("configured identity differs")
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(requests, row.Requests) {
				got, _ := json.Marshal(requests)
				want, _ := json.Marshal(row.Requests)
				t.Errorf("application wire differs: actual=%s Source=%s", got, want)
			}
			if len(connects) != len(row.Connects) {
				t.Error("CONNECT count differs")
			} else if len(connects) > 0 && !reflect.DeepEqual(connects, row.Connects) {
				t.Error("CONNECT wire differs", connects, row.Connects)
			}
			if len(snis) != len(row.SNIs) {
				t.Error("SNI count differs", snis, row.SNIs)
			} else if len(snis) > 0 && !reflect.DeepEqual(snis, row.SNIs) {
				t.Error("SNI differs", snis, row.SNIs)
			}
			if len(dials) != 1 || len(row.Dials) != 1 {
				t.Error("dial count differs", dials)
			}
			if len(dials) == 1 && len(row.Dials) == 1 {
				if row.Route == "https-direct" {
					var port int
					if err := json.Unmarshal(row.Dials[0].Port, &port); err != nil {
						t.Fatal(err)
					}
					want := net.JoinHostPort(row.Dials[0].Host, fmt.Sprint(port))
					if dials[0] != want {
						t.Error("origin dial differs", dials[0], want)
					}
				} else {
					var role string
					if err := json.Unmarshal(row.Dials[0].Port, &role); err != nil {
						t.Fatal(err)
					}
					if role != "owned-proxy" || row.Dials[0].Host != proxyURL.Hostname() || dials[0] != proxyURL.Host {
						t.Error("proxy dial differs", dials, row.Dials)
					}
				}
			}
			if len(response.Content) != 1 {
				t.Fatal("reply count differs")
			}
			text, ok := response.Content[0].Text()
			if !ok || text.Text != row.Text {
				t.Error("reply text differs")
			}
		})
	}
}
