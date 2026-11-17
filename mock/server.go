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

// MaxRegister caps the number of codes in one POST /cz/register.
const MaxRegister = 10000

// Server is the HTTP mock. Mutable state: the mode and codes registered at runtime.
type Server struct {
	slow  time.Duration
	log   *slog.Logger
	mu    sync.RWMutex
	mode  string
	clock func() time.Time

	regMu sync.RWMutex
	reg   Registry
	// registeredFile keeps codes added via /cz/register across restarts ("" = memory only).
	registeredFile string
}

// NewServer creates a server; mode must be valid.
func NewServer(reg Registry, mode string, slow time.Duration, log *slog.Logger) (*Server, error) {
	if !validModes[mode] {
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
	return &Server{reg: reg, slow: slow, log: log, mode: mode, clock: time.Now}, nil
}

// PersistTo makes /cz/register append new codes to path (CSV with header "code").
// Codes already stored there are merged into the registry; a missing file is fine.
func (s *Server) PersistTo(path string) (int, error) {
	stored, err := LoadRegistry(path)
	if errors.Is(err, os.ErrNotExist) {
		stored = Registry{}
	} else if err != nil {
		return 0, err
	}
	s.regMu.Lock()
	defer s.regMu.Unlock()
	for c := range stored {
		s.reg[c] = struct{}{}
	}
	s.registeredFile = path
	return len(stored), nil
}

func (s *Server) verify(code, gtin string) VerifyResponse {
	s.regMu.RLock()
	defer s.regMu.RUnlock()
	return s.reg.Verify(code, gtin)
}

func (s *Server) registrySize() int {
	s.regMu.RLock()
	defer s.regMu.RUnlock()
	return len(s.reg)
}

// register adds codes to the registry (and the persistence file) and returns how many were new.
func (s *Server) register(codes []string) (int, error) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	var fresh []string
	seen := map[string]bool{}
	for _, c := range codes {
		if _, ok := s.reg[c]; !ok && !seen[c] {
			seen[c] = true
			fresh = append(fresh, c)
		}
	}
	if len(fresh) == 0 {
		return 0, nil
	}
	if s.registeredFile != "" {
		if err := appendCodes(s.registeredFile, fresh); err != nil {
			return 0, err
		}
	}
	for _, c := range fresh {
		s.reg[c] = struct{}{}
	}
	return len(fresh), nil
}

func appendCodes(path string, codes []string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w := csv.NewWriter(f)
	if st.Size() == 0 {
		_ = w.Write([]string{"code"})
	}
	for _, c := range codes {
		_ = w.Write([]string{c})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
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
	mux.HandleFunc("POST /cz/register", s.handleRegister)
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
	if s.applyMode(w, r) {
		return
	}

	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{"BAD_REQUEST", "Тело запроса должно быть JSON вида {\"code\": \"...\", \"gtin\": \"...\"}"})
		return
	}
	gtin := ""
	if req.GTIN != nil {
		gtin = *req.GTIN
	}
	res := s.verify(req.Code, gtin)
	addLogAttrs(w, slog.String("code", req.Code), slog.String("gtin", gtin),
		slog.Bool("is_valid", res.IsValid), slog.String("reason", res.Reason))
	writeJSON(w, http.StatusOK, res)
}

// applyMode plays the failure modes (SPEC §8); true means the answer is already written.
// The request body must be consumed before the call (see handleVerify).
func (s *Server) applyMode(w http.ResponseWriter, r *http.Request) bool {
	mode := s.Mode()
	addLogAttrs(w, slog.String("mode", mode))
	switch mode {
	case ModeError:
		writeJSON(w, http.StatusInternalServerError, errorBody{"MOCK_ERROR", "Имитация внутренней ошибки сервиса проверки"})
		return true
	case ModeGarbage:
		writeJSON(w, http.StatusOK, struct{}{})
		return true
	case ModeSlow:
		t := time.NewTimer(s.slow)
		select {
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			addLogAttrs(w, slog.Bool("client_gone", true))
			return true
		}
	}
	return false
}

// RegisterResponse is the /cz/register answer.
type RegisterResponse struct {
	Registered   int `json:"registered"`
	Known        int `json:"known"`
	RegistrySize int `json:"registry_size"`
}

// handleRegister adds codes issued by MarkTrace so that /cz/verify accepts them:
// POST /cz/register {"codes": ["01…21…", …]}. Re-registering a known code is not an error.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Codes []string `json:"codes"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err == nil {
		err = json.Unmarshal(raw, &req)
	}
	if s.applyMode(w, r) {
		return
	}
	if err != nil || len(req.Codes) == 0 || len(req.Codes) > MaxRegister {
		writeJSON(w, http.StatusBadRequest, errorBody{"BAD_REQUEST",
			fmt.Sprintf("Тело запроса должно быть JSON вида {\"codes\": [...]} с 1–%d кодами", MaxRegister)})
		return
	}
	for _, c := range req.Codes {
		if !codeMask.MatchString(c) {
			writeJSON(w, http.StatusBadRequest, errorBody{"INVALID_FORMAT",
				fmt.Sprintf("Код %q не соответствует маске 01 + GTIN-14 + 21 + 6 символов", c)})
			return
		}
	}
	added, err := s.register(req.Codes)
	if err != nil {
		s.log.Error("register: persist", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, errorBody{"STORE_ERROR", "Не удалось сохранить коды"})
		return
	}
	size := s.registrySize()
	addLogAttrs(w, slog.Int("codes", len(req.Codes)), slog.Int("registered", added))
	writeJSON(w, http.StatusOK, RegisterResponse{Registered: added, Known: len(req.Codes) - added, RegistrySize: size})
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
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "mode": s.Mode(), "registry_size": s.registrySize()})
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
