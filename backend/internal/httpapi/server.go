// Package httpapi exposes the REST API (SPEC §6).
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"marktrace/backend/internal/auth"
	"marktrace/backend/internal/importer"
	"marktrace/backend/internal/ops"
	"marktrace/backend/internal/reports"
	"marktrace/backend/internal/rules"
)

// MaxUpload is the multipart upload limit.
const MaxUpload = 20 << 20

// Server wires services to HTTP.
type Server struct {
	Auth     *auth.Service
	Ops      *ops.Service
	Importer *importer.Importer
	Reports  *reports.Service
	Mock     *ops.MockClient
}

// New builds a server from a pool and configuration.
func New(pool *pgxpool.Pool, mockURL, goldenDir string) *Server {
	mock := ops.NewMockClient(mockURL)
	return &Server{
		Auth:     &auth.Service{Pool: pool},
		Ops:      &ops.Service{Pool: pool, Mock: mock},
		Importer: &importer.Importer{Pool: pool},
		Reports:  &reports.Service{Pool: pool, GoldenDir: goldenDir},
		Mock:     mock,
	}
}

// Role sets.
var (
	allRoles     = []string{"A1", "A2", "A3", "A4", "admin"}
	opRoles      = []string{"A1", "A2", "admin"}
	importRoles  = []string{"A1", "A3", "admin"}
	resolveRoles = []string{"A3", "admin"}
)

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	health := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /api/healthz", health)

	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.Handle("GET /api/auth/me", s.with(allRoles, s.me))

	mux.Handle("GET /api/rules", s.with(allRoles, s.rules))
	mux.Handle("GET /api/items", s.with(allRoles, s.items))
	mux.Handle("POST /api/operations", s.with(opRoles, s.operation))
	mux.Handle("GET /api/codes", s.with(allRoles, s.codes))
	mux.Handle("GET /api/codes/export.csv", s.with(allRoles, s.codesCSV))
	mux.Handle("GET /api/codes/{code}", s.with(allRoles, s.codeDetail))
	mux.Handle("GET /api/status-summary.csv", s.with(allRoles, s.statusSummary))
	mux.Handle("GET /api/violations", s.with(allRoles, s.violations))
	mux.Handle("GET /api/violations/export.csv", s.with(allRoles, s.violationsCSV))
	mux.Handle("POST /api/violations/{id}/resolutions", s.with(resolveRoles, s.resolve))
	mux.Handle("GET /api/kpi", s.with(allRoles, s.kpi))
	mux.Handle("POST /api/imports", s.with(importRoles, s.importFiles))
	mux.Handle("GET /api/imports", s.with(allRoles, s.imports))
	mux.Handle("GET /api/imports/{id}", s.with(allRoles, s.importDetail))
	mux.Handle("GET /api/golden/compare", s.with(allRoles, s.golden))
	mux.Handle("POST /api/verify", s.with(allRoles, s.verify))
	// Catch-all keeps unknown API paths (and wrong methods) in JSON.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "404", "Ресурс не найден")
	})
	return recoverer(logger(mux))
}

// with enforces login and role membership.
func (s *Server) with(roles []string, h func(http.ResponseWriter, *http.Request, *auth.User)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		if c, err := r.Cookie(auth.CookieName); err == nil {
			token = c.Value
		}
		u, err := s.Auth.Lookup(r.Context(), token)
		if err != nil {
			internalError(w, err)
			return
		}
		if u == nil {
			writeError(w, http.StatusUnauthorized, "401", "Требуется вход в систему")
			return
		}
		if !hasRole(u.Role, roles) {
			writeError(w, http.StatusForbidden, rules.AuthDenied, rules.Message(rules.AuthDenied, ""))
			return
		}
		h(w, r.WithContext(auth.WithUser(r.Context(), u)), u)
	})
}

func hasRole(role string, roles []string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	e := apiError{Error: code, Message: message}
	if code == rules.AuthDenied {
		e.Reason = code
	}
	writeJSON(w, status, e)
}

func internalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "500", "Внутренняя ошибка сервера")
}

func decodeJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return errors.New("empty body")
	}
	return json.Unmarshal(body, v)
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v\n%s", rec, debug.Stack())
				writeError(w, http.StatusInternalServerError, "500", "Внутренняя ошибка сервера")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path != "/healthz" && r.URL.Path != "/api/healthz" {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
		}
	})
}
