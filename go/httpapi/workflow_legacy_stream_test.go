package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestWorkflowLegacyArchiveStreamsOverOwnedTCPCatchup(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-legacy-archive.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name     string `json:"name"`
			Recorded string `json:"recorded_wire"`
			Frame    string `json:"frame_wire"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 5 {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			s, parent, store := catchupHTTP(t)
			rebound := strings.ReplaceAll(row.Recorded, `"s"`, `"`+string(parent.ID())+`"`)
			event, err := agent.DecodeStoredEvent([]byte(rebound))
			if err != nil {
				t.Fatal(err)
			}
			store.mu.Lock()
			prefix := event
			prefix.Sequence = 1
			store.events[parent.ID()] = []agent.SessionEventRecord{prefix, event}
			store.mu.Unlock()
			server := httptest.NewServer(s)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/sessions/"+string(parent.ID())+"/events?envelope=true", nil)
			req.Header.Set("Authorization", "Bearer token-a")
			req.Header.Set("Last-Event-ID", "1")
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal(response.StatusCode)
			}
			scanner := bufio.NewScanner(response.Body)
			var frame strings.Builder
			var payload string
			for scanner.Scan() {
				line := scanner.Text()
				frame.WriteString(line + "\n")
				if strings.HasPrefix(line, "data: ") {
					payload = strings.TrimPrefix(line, "data: ")
				}
				if line == "" && payload != "" {
					break
				}
			}
			if payload == "" || !strings.Contains(frame.String(), "event: agent_event") || !strings.Contains(frame.String(), "id: 2") {
				t.Fatal(frame.String(), scanner.Err())
			}
			got, err := jsonvalue.Decode(payload)
			if err != nil {
				t.Fatal(err)
			}
			want, err := jsonvalue.Decode(strings.ReplaceAll(row.Frame, `"s"`, `"`+string(parent.ID())+`"`))
			if err != nil {
				t.Fatal(err)
			}
			a, _ := jsonvalue.AppendLegacyDefault(got.Sorted())
			b, _ := jsonvalue.AppendLegacyDefault(want.Sorted())
			if string(a) != string(b) {
				t.Fatalf("Source SSE mismatch: %s; %s", a, b)
			}
			cancel()
			response.Body.Close()
			wait(t, func() bool { return parent.Info().Subscribers == 0 })
			if store.loads.Load() == 0 {
				t.Fatal("missed configured-store path")
			}
			if foreign := request(s, "GET", "/sessions/"+string(parent.ID())+"/events", "", "token-b"); foreign.Code != 404 {
				t.Fatal(foreign.Code)
			}
		})
	}
}
