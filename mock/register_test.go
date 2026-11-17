package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterMakesCodeValid(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeNormal, 0)
	if _, body := post(t, ts.URL+"/cz/verify", `{"code":"`+unknownCode+`"}`); body != `{"is_valid":false,"reason":"UNKNOWN_CODE"}` {
		t.Fatalf("before register: %s", body)
	}
	status, body := post(t, ts.URL+"/cz/register", `{"codes":["`+unknownCode+`","`+knownCode+`"]}`)
	if status != http.StatusOK || body != `{"registered":1,"known":1,"registry_size":2}` {
		t.Fatalf("register: %d %s", status, body)
	}
	if _, body := post(t, ts.URL+"/cz/verify", `{"code":"`+unknownCode+`","gtin":"4601234567893"}`); body != `{"is_valid":true}` {
		t.Fatalf("after register: %s", body)
	}
	// Registering again is idempotent.
	if _, body := post(t, ts.URL+"/cz/register", `{"codes":["`+unknownCode+`"]}`); body != `{"registered":0,"known":1,"registry_size":2}` {
		t.Fatalf("repeat: %s", body)
	}
}

func TestRegisterRejectsBadInput(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeNormal, 0)
	for _, body := range []string{`{}`, `{"codes":[]}`, `nope`, `{"codes":["(01)04601234567893(21)AbC123"]}`} {
		status, resp := post(t, ts.URL+"/cz/register", body)
		if status != http.StatusBadRequest {
			t.Errorf("%s: got %d %s, want 400", body, status, resp)
		}
	}
	// A rejected batch registers nothing.
	post(t, ts.URL+"/cz/register", `{"codes":["`+unknownCode+`","bad"]}`)
	if _, body := post(t, ts.URL+"/cz/verify", `{"code":"`+unknownCode+`"}`); body != `{"is_valid":false,"reason":"UNKNOWN_CODE"}` {
		t.Errorf("partial register: %s", body)
	}
}

func TestRegisterFollowsMode(t *testing.T) {
	_, ts, _ := newTestServer(t, ModeError, 0)
	if status, _ := post(t, ts.URL+"/cz/register", `{"codes":["`+unknownCode+`"]}`); status != http.StatusInternalServerError {
		t.Errorf("error mode: got %d, want 500", status)
	}
}

func TestRegisteredCodesSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registered.csv")
	start := func() *httptest.Server {
		reg, err := ParseRegistry(strings.NewReader("code\n" + knownCode + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		srv, err := NewServer(reg, ModeNormal, 0, slog.New(slog.NewJSONHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := srv.PersistTo(path); err != nil {
			t.Fatal(err)
		}
		ts := httptest.NewServer(srv.Handler())
		t.Cleanup(ts.Close)
		return ts
	}
	first := start()
	post(t, first.URL+"/cz/register", `{"codes":["`+unknownCode+`"]}`)
	first.Close()

	second := start()
	if _, body := post(t, second.URL+"/cz/verify", `{"code":"`+unknownCode+`"}`); body != `{"is_valid":true}` {
		t.Fatalf("after restart: %s", body)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "code\n"+unknownCode+"\n" {
		t.Errorf("file = %q", data)
	}
	resp, err := http.Get(second.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var h struct {
		RegistrySize int `json:"registry_size"`
	}
	json.NewDecoder(resp.Body).Decode(&h)
	if h.RegistrySize != 2 {
		t.Errorf("registry_size = %d, want 2", h.RegistrySize)
	}
}
