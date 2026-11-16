package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// GTIN-13 4601234567893 (valid, docs/13) -> GTIN-14 form 04601234567893.
	knownCode   = "010460123456789321AbC123"
	unknownCode = "010460123456789321ZZZ999"
)

// syncBuffer is a goroutine-safe log sink (handlers log after the response is sent).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls the log until cond holds or a second passes.
func (b *syncBuffer) waitFor(t *testing.T, cond func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		s := b.String()
		if cond(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("log condition not met; logs:\n%s", s)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newTestServer(t *testing.T, mode string, slow time.Duration) (*Server, *httptest.Server, *syncBuffer) {
	t.Helper()
	reg, err := ParseRegistry(strings.NewReader("code\n" + knownCode + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	srv, err := NewServer(reg, mode, slow, slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts, logs
}

func post(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func TestVerifyScenarios(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeNormal, 0)
	cases := []struct {
		name, body, want string
	}{
		{"valid without gtin", `{"code":"` + knownCode + `"}`, `{"is_valid":true}`},
		{"valid with gtin-13", `{"code":"` + knownCode + `","gtin":"4601234567893"}`, `{"is_valid":true}`},
		{"valid with gtin-14", `{"code":"` + knownCode + `","gtin":"04601234567893"}`, `{"is_valid":true}`},
		{"empty gtin means not given", `{"code":"` + knownCode + `","gtin":""}`, `{"is_valid":true}`},
		{"null gtin means not given", `{"code":"` + knownCode + `","gtin":null}`, `{"is_valid":true}`},
		{"gtin mismatch", `{"code":"` + knownCode + `","gtin":"04600000000015"}`, `{"is_valid":false,"reason":"GTIN_MISMATCH"}`},
		{"gtin 14 with other indicator", `{"code":"` + knownCode + `","gtin":"14601234567890"}`, `{"is_valid":false,"reason":"GTIN_MISMATCH"}`},
		{"unknown code", `{"code":"` + unknownCode + `"}`, `{"is_valid":false,"reason":"UNKNOWN_CODE"}`},
		{"unknown code checked before gtin", `{"code":"` + unknownCode + `","gtin":"1"}`, `{"is_valid":false,"reason":"UNKNOWN_CODE"}`},
		{"short serial", `{"code":"010460123456789321AbC12"}`, `{"is_valid":false,"reason":"INVALID_FORMAT"}`},
		{"gs1 brackets", `{"code":"(01)04601234567893(21)AbC123"}`, `{"is_valid":false,"reason":"INVALID_FORMAT"}`},
		{"cyrillic serial", `{"code":"010460123456789321АbC123"}`, `{"is_valid":false,"reason":"INVALID_FORMAT"}`},
		{"no trimming", `{"code":" ` + knownCode + `"}`, `{"is_valid":false,"reason":"INVALID_FORMAT"}`},
		{"missing code", `{}`, `{"is_valid":false,"reason":"INVALID_FORMAT"}`},
		{"format checked before registry and gtin", `{"code":"bad","gtin":"x"}`, `{"is_valid":false,"reason":"INVALID_FORMAT"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, body := post(t, ts.URL+"/cz/verify", c.body)
			if status != http.StatusOK || body != c.want {
				t.Errorf("got %d %s, want 200 %s", status, body, c.want)
			}
		})
	}
}

func TestVerifyBadJSON(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeNormal, 0)
	status, body := post(t, ts.URL+"/cz/verify", `not json`)
	if status != http.StatusBadRequest || !strings.Contains(body, `"BAD_REQUEST"`) {
		t.Errorf("got %d %s", status, body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeNormal, 0)
	resp, err := http.Get(ts.URL + "/cz/verify")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /cz/verify = %d, want 405", resp.StatusCode)
	}
}

func TestModeError(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeError, 0)
	status, body := post(t, ts.URL+"/cz/verify", `{"code":"`+knownCode+`"}`)
	if status != http.StatusInternalServerError || !strings.Contains(body, "MOCK_ERROR") {
		t.Errorf("got %d %s, want 500", status, body)
	}
}

func TestModeGarbage(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeGarbage, 0)
	status, body := post(t, ts.URL+"/cz/verify", `{"code":"`+knownCode+`"}`)
	if status != http.StatusOK || body != `{}` {
		t.Errorf("got %d %s, want 200 {}", status, body)
	}
}

func TestModeSlowDelaysThenAnswers(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeSlow, 150*time.Millisecond)
	start := time.Now()
	status, body := post(t, ts.URL+"/cz/verify", `{"code":"`+knownCode+`"}`)
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Errorf("answered after %v, want >= 150ms", d)
	}
	if status != http.StatusOK || body != `{"is_valid":true}` {
		t.Errorf("got %d %s", status, body)
	}
}

func TestModeSlowClientTimeout(t *testing.T) {
	_, ts, logs := newTestServer(t, ModeSlow, 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/cz/verify", strings.NewReader(`{"code":"`+knownCode+`"}`))
	start := time.Now()
	_, err := http.DefaultClient.Do(req)
	if err == nil {
		t.Fatal("expected client timeout")
	}
	// The handler must notice the cancelled request instead of sleeping the full delay.
	logs.waitFor(t, func(s string) bool { return strings.Contains(s, `"client_gone":true`) })
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("handler held for %v", d)
	}
}

func TestAdminMode(t *testing.T) {
	srv, ts, _ := newTestServer(t, ModeNormal, 0)

	resp, err := http.Get(ts.URL + "/admin/mode")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(b)) != `{"mode":"normal"}` {
		t.Errorf("GET /admin/mode = %s", b)
	}

	for _, m := range []string{ModeError, ModeGarbage, ModeSlow, ModeNormal} {
		status, body := post(t, ts.URL+"/admin/mode", `{"mode":"`+m+`"}`)
		if status != http.StatusOK || body != `{"mode":"`+m+`"}` || srv.Mode() != m {
			t.Errorf("set %s: got %d %s, mode %s", m, status, body, srv.Mode())
		}
	}

	// Runtime switch affects /cz/verify.
	post(t, ts.URL+"/admin/mode", `{"mode":"error"}`)
	if status, _ := post(t, ts.URL+"/cz/verify", `{"code":"`+knownCode+`"}`); status != http.StatusInternalServerError {
		t.Errorf("verify after switch to error = %d", status)
	}

	for _, bad := range []string{`{"mode":"broken"}`, `{}`, `nope`} {
		status, body := post(t, ts.URL+"/admin/mode", bad)
		if status != http.StatusBadRequest || !strings.Contains(body, "BAD_MODE") {
			t.Errorf("%s: got %d %s", bad, status, body)
		}
	}
	if srv.Mode() != ModeError {
		t.Errorf("invalid request changed mode to %s", srv.Mode())
	}
}

func TestHealthz(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeError, 0)
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// healthz stays 200 in every mode: modes only affect /cz/verify.
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" || body["registry_size"] != float64(1) {
		t.Errorf("healthz = %d %v", resp.StatusCode, body)
	}
}

func TestOneStructuredLogLinePerRequest(t *testing.T) {
	_, ts, logs := newTestServer(t, ModeNormal, 0)
	post(t, ts.URL+"/cz/verify", `{"code":"`+unknownCode+`","gtin":"4601234567893"}`)
	if resp, err := http.Get(ts.URL + "/healthz"); err == nil {
		resp.Body.Close()
	}

	out := logs.waitFor(t, func(s string) bool { return strings.Count(s, "\n") >= 2 })
	time.Sleep(20 * time.Millisecond) // make sure no extra line follows
	if out = logs.String(); strings.Count(out, "\n") != 2 {
		t.Fatalf("want exactly 2 log lines:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var entry map[string]any
	verifyLine := lines[0]
	if !strings.Contains(verifyLine, "/cz/verify") {
		verifyLine = lines[1]
	}
	if err := json.Unmarshal([]byte(verifyLine), &entry); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	want := map[string]any{
		"method": "POST", "path": "/cz/verify", "status": float64(200), "mode": "normal",
		"code": unknownCode, "is_valid": false, "reason": "UNKNOWN_CODE",
	}
	for k, v := range want {
		if entry[k] != v {
			t.Errorf("log %s = %v, want %v", k, entry[k], v)
		}
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Error("log line has no duration_ms")
	}
}

func TestParseRegistry(t *testing.T) {
	reg, err := ParseRegistry(strings.NewReader(string(rune(0xFEFF)) + "code\n" + knownCode + "\n\n" + unknownCode + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg) != 2 {
		t.Errorf("registry size = %d, want 2", len(reg))
	}
	if _, err := ParseRegistry(strings.NewReader("gtin\n123\n")); err == nil {
		t.Error("expected error for wrong header")
	}
	if _, err := ParseRegistry(strings.NewReader("")); err == nil {
		t.Error("expected error for empty file")
	}
}

func TestLoadRegistryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reg.csv")
	if err := os.WriteFile(path, []byte("code\n"+knownCode+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(path)
	if err != nil || len(reg) != 1 {
		t.Fatalf("LoadRegistry = %v, %v", reg, err)
	}
	if _, err := LoadRegistry(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Error("expected error for missing file")
	}
}

// The real generated registry (if present) must load and accept its demo codes.
func TestGeneratedRegistry(t *testing.T) {
	reg, err := LoadRegistry("../data/C16/mock_registry.csv")
	if err != nil {
		t.Skip("generated registry not present:", err)
	}
	if len(reg) < 500 {
		t.Errorf("registry size = %d", len(reg))
	}
	demo, err := os.ReadFile("../data/C16/demo_codes.csv")
	if err != nil {
		t.Skip(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(demo)), "\n")[1:] {
		code := strings.Split(line, ",")[0]
		if res := reg.Verify(code, ""); !res.IsValid {
			t.Errorf("demo code %s: %+v", code, res)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	cfg, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.RegistryFile != "/data/mock_registry.csv" || cfg.Mode != ModeNormal || cfg.Slow != 5*time.Second {
		t.Errorf("defaults = %+v", cfg)
	}

	cfg, err = LoadConfig(env(map[string]string{"MOCK_MODE": "slow", "MOCK_SLOW_MS": "250", "REGISTRY_FILE": "/x.csv", "ADDR": ":9000"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeSlow || cfg.Slow != 250*time.Millisecond || cfg.RegistryFile != "/x.csv" || cfg.Addr != ":9000" {
		t.Errorf("cfg = %+v", cfg)
	}

	if _, err := LoadConfig(env(map[string]string{"MOCK_MODE": "turbo"})); err == nil {
		t.Error("expected error for bad mode")
	}
	if _, err := LoadConfig(env(map[string]string{"MOCK_SLOW_MS": "-1"})); err == nil {
		t.Error("expected error for bad slow ms")
	}
}
