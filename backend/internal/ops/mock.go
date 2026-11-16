package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MockClient calls the "ЦРПТ" mock: POST {base}/cz/verify.
type MockClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewMockClient returns a client with the SPEC timeout of 2 s.
func NewMockClient(baseURL string) *MockClient {
	return &MockClient{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 2 * time.Second}}
}

// MockResult is the mock answer. Raw is the JSON body (or a synthesized one on failure).
type MockResult struct {
	Available bool
	IsValid   bool
	Reason    string
	Raw       json.RawMessage
}

// Verify asks the mock about a code. Network errors, timeouts, non-200 and
// bodies without boolean is_valid give Available=false.
func (m *MockClient) Verify(ctx context.Context, code, gtin string) MockResult {
	req := map[string]string{"code": code}
	if gtin != "" {
		req["gtin"] = gtin
	}
	body, _ := json.Marshal(req)
	unavailable := func(detail string) MockResult {
		raw, _ := json.Marshal(map[string]any{"is_valid": false, "reason": "MOCK_UNAVAILABLE", "detail": detail})
		return MockResult{Available: false, Reason: "MOCK_UNAVAILABLE", Raw: raw}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+"/cz/verify", bytes.NewReader(body))
	if err != nil {
		return unavailable(err.Error())
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := m.HTTP.Do(hreq)
	if err != nil {
		return unavailable(err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return unavailable(err.Error())
	}
	if resp.StatusCode != http.StatusOK {
		return unavailable(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	var parsed struct {
		IsValid *bool  `json:"is_valid"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.IsValid == nil {
		return unavailable("нет поля is_valid")
	}
	return MockResult{Available: true, IsValid: *parsed.IsValid, Reason: parsed.Reason, Raw: json.RawMessage(data)}
}
