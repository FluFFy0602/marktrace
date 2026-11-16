package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Modes of the mock (SPEC §8).
const (
	ModeNormal  = "normal"
	ModeSlow    = "slow"
	ModeError   = "error"
	ModeGarbage = "garbage"
)

var validModes = map[string]bool{ModeNormal: true, ModeSlow: true, ModeError: true, ModeGarbage: true}

var codeMask = regexp.MustCompile(`^01(\d{14})21([A-Za-z0-9]{6})$`)

// Registry is the set of codes known to the mock "ЦРПТ".
type Registry map[string]struct{}

// LoadRegistry reads a CSV file whose first column header is "code".
func LoadRegistry(path string) (Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseRegistry(f)
}

// ParseRegistry reads registry CSV from r.
func ParseRegistry(r io.Reader) (Registry, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("registry header: %w", err)
	}
	if len(header) == 0 || strings.TrimPrefix(header[0], string(rune(0xFEFF))) != "code" {
		return nil, fmt.Errorf("registry header must start with column \"code\", got %q", header)
	}
	reg := Registry{}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) > 0 && rec[0] != "" {
			reg[rec[0]] = struct{}{}
		}
	}
	return reg, nil
}

// VerifyResponse is the /cz/verify answer.
type VerifyResponse struct {
	IsValid bool   `json:"is_valid"`
	Reason  string `json:"reason,omitempty"`
}

// normGTIN converts GTIN-13 into its GTIN-14 comparison form.
func normGTIN(g string) string {
	if len(g) == 13 {
		return "0" + g
	}
	return g
}

// Verify applies the SPEC §8 checks in order; gtin == "" means "not given".
func (reg Registry) Verify(code, gtin string) VerifyResponse {
	m := codeMask.FindStringSubmatch(code)
	if m == nil {
		return VerifyResponse{IsValid: false, Reason: "INVALID_FORMAT"}
	}
	if _, ok := reg[code]; !ok {
		return VerifyResponse{IsValid: false, Reason: "UNKNOWN_CODE"}
	}
	if gtin != "" && normGTIN(gtin) != m[1] {
		return VerifyResponse{IsValid: false, Reason: "GTIN_MISMATCH"}
	}
	return VerifyResponse{IsValid: true}
}

// Server is the HTTP mock. The mode is the only mutable state.
type Server struct {
	reg   Registry
	slow  time.Duration
	log   *slog.Logger
	mu    sync.RWMutex
	mode  string
	clock func() time.Time
}

// NewServer creates a server; mode must be valid.
func NewServer(reg Registry, mode string, slow time.Duration, log *slog.Logger) (*Server, error) {
	if !validModes[mode] {
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
	return &Server{reg: reg, slow: slow, log: log, mode: mode, clock: time.Now}, nil
}

// Mode returns the current mode.
func (s *Server) Mode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

func (s *Server) setMode(m string) {
	s.mu.Lock()
	s.mode = m
	s.mu.Unlock()
}

// Handler returns the routed handler wrapped with request logging.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cz/verify", s.handleVerify)
	mux.HandleFunc("GET /admin/mode", s.handleGetMode)
	mux.HandleFunc("POST /admin/mode", s.handleSetMode)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return s.logRequests(mux)
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	// Read the whole body first: net/http only notices a client that gave up
	// (e.g. backend timeout in slow mode) once the request body is consumed.
	var req struct {
		Code string  `json:"code"`
		GTIN *string `json:"gtin"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err == nil {
		err = json.Unmarshal(raw, &req)
	}

	mode := s.Mode()
	addLogAttrs(w, slog.String("mode", mode))
	switch mode {
	case ModeError:
		writeJSON(w, http.StatusInternalServerError, errorBody{"MOCK_ERROR", "Имитация внутренней ошибки сервиса проверки"})
		return
	case ModeGarbage:
		writeJSON(w, http.StatusOK, struct{}{})
		return
	case ModeSlow:
		t := time.NewTimer(s.slow)
		select {
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			addLogAttrs(w, slog.Bool("client_gone", true))
			return
		}
	}

	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{"BAD_REQUEST", "Тело запроса должно быть JSON вида {\"code\": \"...\", \"gtin\": \"...\"}"})
		return
	}
	gtin := ""
	if req.GTIN != nil {
		gtin = *req.GTIN
	}
	res := s.reg.Verify(req.Code, gtin)
	addLogAttrs(w, slog.String("code", req.Code), slog.String("gtin", gtin),
		slog.Bool("is_valid", res.IsValid), slog.String("reason", res.Reason))
	writeJSON(w, http.StatusOK, res)
}

type modeBody struct {
	Mode string `json:"mode"`
}

func (s *Server) handleGetMode(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, modeBody{s.Mode()})
}

func (s *Server) handleSetMode(w http.ResponseWriter, r *http.Request) {
	var req modeBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || !validModes[req.Mode] {
		writeJSON(w, http.StatusBadRequest, errorBody{"BAD_MODE", "Режим должен быть одним из: normal, slow, error, garbage"})
		return
	}
	s.setMode(req.Mode)
	addLogAttrs(w, slog.String("new_mode", req.Mode))
	writeJSON(w, http.StatusOK, modeBody{req.Mode})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "mode": s.Mode(), "registry_size": len(s.reg)})
}

// logWriter captures the status code and extra attributes set by handlers.
type logWriter struct {
	http.ResponseWriter
	status int
	attrs  []any
}

func (lw *logWriter) WriteHeader(code int) {
	if lw.status == 0 {
		lw.status = code
	}
	lw.ResponseWriter.WriteHeader(code)
}

func (lw *logWriter) Write(b []byte) (int, error) {
	if lw.status == 0 {
		lw.status = http.StatusOK
	}
	return lw.ResponseWriter.Write(b)
}

func addLogAttrs(w http.ResponseWriter, attrs ...any) {
	if lw, ok := w.(*logWriter); ok {
		lw.attrs = append(lw.attrs, attrs...)
	}
}

// logRequests emits exactly one structured log line per request.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.clock()
		lw := &logWriter{ResponseWriter: w}
		next.ServeHTTP(lw, r)
		if lw.status == 0 {
			lw.status = http.StatusOK
			if r.Context().Err() != nil {
				lw.status = 499 // client closed the request before we answered
			}
		}
		attrs := append([]any{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", lw.status),
			slog.Int64("duration_ms", s.clock().Sub(start).Milliseconds()),
			slog.String("remote", r.RemoteAddr),
		}, lw.attrs...)
		s.log.Info("request", attrs...)
	})
}
