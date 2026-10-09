package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxReportBytes bounds the untrusted health document before JSON decoding.
const MaxReportBytes = 1 << 20

// Fetch reads public /healthz with Source's ten-second deadline. An optional
// client is borrowed without mutation or credential injection. Redirect and
// proxy behavior follow that client's transport policy.
func Fetch(ctx context.Context, base string, client *http.Client) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/healthz", nil)
	if err != nil {
		return Report{}, fmt.Errorf("health request: %w", err)
	}
	if client == nil {
		client = &http.Client{}
	}
	response, err := client.Do(request)
	if err != nil {
		return Report{}, fmt.Errorf("health request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Report{}, fmt.Errorf("health request: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxReportBytes+1))
	if err != nil {
		return Report{}, fmt.Errorf("health response: %w", err)
	}
	if len(body) > MaxReportBytes {
		return Report{}, fmt.Errorf("health response exceeds %d bytes", MaxReportBytes)
	}
	var report Report
	if err := json.Unmarshal(body, &report); err != nil {
		return Report{}, fmt.Errorf("health report: %w", err)
	}
	return report, nil
}
