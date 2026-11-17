package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"marktrace/backend/internal/auth"
	"marktrace/backend/internal/importer"
	"marktrace/backend/internal/ops"
	"marktrace/backend/internal/reports"
	"marktrace/backend/internal/rules"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "400", "Некорректный JSON в запросе")
		return
	}
	u, token, err := s.Auth.Login(r.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "401", "Неверный логин или пароль")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	auth.SetCookie(w, token)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		if err := s.Auth.Logout(r.Context(), c.Value); err != nil {
			internalError(w, err)
			return
		}
	}
	auth.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, u *auth.User) {
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request, u *auth.User) {
	writeJSON(w, http.StatusOK, map[string]any{
		"rules_version": rules.Version,
		"code_mask":     rules.CodeMask,
		"transitions":   rules.Transitions,
		"statuses":      rules.Statuses,
		"op_types":      rules.OpTypes,
		"reasons":       rules.Reasons,
	})
}

func (s *Server) items(w http.ResponseWriter, r *http.Request, u *auth.User) {
	list, err := s.Reports.Items(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) operation(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var req ops.FormRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "400", "Некорректный JSON в запросе")
		return
	}
	resp, err := s.Ops.Submit(r.Context(), u.Username, u.Role, req)
	if errors.Is(err, ops.ErrAuthDenied) {
		writeError(w, http.StatusForbidden, rules.AuthDenied,
			rules.Message(rules.AuthDenied, fmt.Sprintf("роль %s не может выполнять операцию %s", u.Role, req.OpType)))
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) issueBatch(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var req ops.BatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "400", "Некорректный JSON в запросе")
		return
	}
	resp, err := s.Ops.IssueBatch(r.Context(), u.Username, u.Role, req)
	var be *ops.BatchError
	switch {
	case errors.Is(err, ops.ErrAuthDenied):
		writeError(w, http.StatusForbidden, rules.AuthDenied,
			rules.Message(rules.AuthDenied, fmt.Sprintf("роль %s не может выпускать коды", u.Role)))
	case errors.As(err, &be):
		writeError(w, be.Status, be.Reason, be.Message)
	case err != nil:
		internalError(w, err)
	case resp.Replayed:
		writeJSON(w, http.StatusOK, resp)
	default:
		writeJSON(w, http.StatusCreated, resp)
	}
}

// badFilter answers 400 for filter parse errors; returns true if handled.
func badFilter(w http.ResponseWriter, err error) bool {
	if errors.Is(err, reports.ErrBadFilter) {
		writeError(w, http.StatusBadRequest, rules.ValueFormat, rules.Message(rules.ValueFormat, err.Error()))
		return true
	}
	return false
}

func codeFilter(r *http.Request, defLimit, maxLimit int) (reports.CodeFilter, error) {
	q := r.URL.Query()
	limit, offset, err := reports.Paging(q.Get("limit"), q.Get("offset"), defLimit, maxLimit)
	if err != nil {
		return reports.CodeFilter{}, err
	}
	return reports.CodeFilter{
		Status: q.Get("status"), ItemCode: q.Get("item_code"), Actor: q.Get("actor"),
		From: q.Get("from"), To: q.Get("to"), Q: q.Get("q"), Limit: limit, Offset: offset,
	}, nil
}

func (s *Server) codes(w http.ResponseWriter, r *http.Request, u *auth.User) {
	f, err := codeFilter(r, 100, 5000)
	if err == nil {
		var res *reports.CodesResult
		res, err = s.Reports.Codes(r.Context(), f)
		if err == nil {
			writeJSON(w, http.StatusOK, res)
			return
		}
	}
	if !badFilter(w, err) {
		internalError(w, err)
	}
}

func csvHeaders(w http.ResponseWriter, filename string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
}

func (s *Server) codesCSV(w http.ResponseWriter, r *http.Request, u *auth.User) {
	f, err := codeFilter(r, 0, 0)
	if err != nil {
		badFilter(w, err)
		return
	}
	var buf strings.Builder
	if err := s.Reports.CodesCSV(r.Context(), f, &buf); err != nil {
		if !badFilter(w, err) {
			internalError(w, err)
		}
		return
	}
	csvHeaders(w, "codes.csv")
	io.WriteString(w, buf.String())
}

func (s *Server) codeDetail(w http.ResponseWriter, r *http.Request, u *auth.User) {
	code := r.PathValue("code")
	d, err := s.Reports.CodeDetail(r.Context(), code)
	if errors.Is(err, reports.ErrNotFound) {
		writeError(w, http.StatusNotFound, rules.CodeNotFound, rules.Message(rules.CodeNotFound, code))
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) statusSummary(w http.ResponseWriter, r *http.Request, u *auth.User) {
	rows, err := s.Reports.StatusSummary(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	csvHeaders(w, "status_summary.csv")
	reports.WriteCSV(w, []string{"status", "count"}, rows)
}

func violationFilter(r *http.Request) (reports.ViolationFilter, error) {
	q := r.URL.Query()
	limit, offset, err := reports.Paging(q.Get("limit"), q.Get("offset"), 100, 5000)
	if err != nil {
		return reports.ViolationFilter{}, err
	}
	return reports.ViolationFilter{
		Reason: q.Get("reason"), Source: q.Get("source"), State: q.Get("state"), ItemCode: q.Get("item_code"),
		Actor: q.Get("actor"), From: q.Get("from"), To: q.Get("to"), Q: q.Get("q"), Limit: limit, Offset: offset,
	}, nil
}

func (s *Server) violations(w http.ResponseWriter, r *http.Request, u *auth.User) {
	f, err := violationFilter(r)
	if err == nil {
		var res *reports.ViolationsResult
		res, err = s.Reports.Violations(r.Context(), f)
		if err == nil {
			writeJSON(w, http.StatusOK, res)
			return
		}
	}
	if !badFilter(w, err) {
		internalError(w, err)
	}
}

func (s *Server) violationsCSV(w http.ResponseWriter, r *http.Request, u *auth.User) {
	f, err := violationFilter(r)
	var rows [][]string
	if err == nil {
		rows, err = s.Reports.ViolationExportRows(r.Context(), f)
	}
	if err != nil {
		if !badFilter(w, err) {
			internalError(w, err)
		}
		return
	}
	csvHeaders(w, "violations.csv")
	reports.WriteCSV(w, []string{"code", "reason", "source", "ref"}, rows)
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var req struct {
		Action  string `json:"action"`
		Comment string `json:"comment"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "400", "Некорректный JSON в запросе")
		return
	}
	res, err := s.Reports.Resolve(r.Context(), r.PathValue("id"), req.Action, req.Comment, u.Username)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, res)
	case errors.Is(err, reports.ErrBadAction):
		writeError(w, http.StatusBadRequest, rules.ValueFormat, rules.Message(rules.ValueFormat, "действие должно быть comment или close"))
	case errors.Is(err, reports.ErrEmptyComment):
		writeError(w, http.StatusBadRequest, rules.RequiredField, rules.Message(rules.RequiredField, "comment"))
	case errors.Is(err, reports.ErrNotFound):
		writeError(w, http.StatusNotFound, "404", "Расхождение не найдено")
	case errors.Is(err, reports.ErrAlreadyClosed):
		writeError(w, http.StatusConflict, "409", "Расхождение уже закрыто")
	default:
		internalError(w, err)
	}
}

func (s *Server) kpi(w http.ResponseWriter, r *http.Request, u *auth.User) {
	k, err := s.Reports.KPI(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) importFiles(w http.ResponseWriter, r *http.Request, u *auth.User) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUpload)
	if err := r.ParseMultipartForm(MaxUpload); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "413", "Размер загрузки превышает 20 МБ")
			return
		}
		writeError(w, http.StatusBadRequest, "400", "Ожидается multipart/form-data с полями items, codes, operations")
		return
	}
	defer r.MultipartForm.RemoveAll()
	var files []importer.File
	for _, ent := range importer.Entities {
		fhs := r.MultipartForm.File[ent]
		if len(fhs) == 0 {
			continue
		}
		f, err := fhs[0].Open()
		if err != nil {
			internalError(w, err)
			return
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			internalError(w, err)
			return
		}
		files = append(files, importer.File{Entity: ent, Filename: fhs[0].Filename, Data: data})
	}
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, rules.RequiredField,
			rules.Message(rules.RequiredField, "нужен хотя бы один файл: items, codes или operations"))
		return
	}
	// A client disconnect must not abort a half-processed session.
	res, err := s.Importer.Run(context.WithoutCancel(r.Context()), u.Username, u.Role, files)
	if err != nil && res == nil {
		internalError(w, err)
		return
	}
	sess, err := s.Reports.Import(r.Context(), res.SessionID)
	if err != nil {
		internalError(w, err)
		return
	}
	snapshot := res.Snapshot
	sess.SnapshotViolations = &snapshot
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) imports(w http.ResponseWriter, r *http.Request, u *auth.User) {
	list, err := s.Reports.Imports(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) importDetail(w http.ResponseWriter, r *http.Request, u *auth.User) {
	sess, err := s.Reports.Import(r.Context(), r.PathValue("id"))
	if errors.Is(err, reports.ErrNotFound) {
		writeError(w, http.StatusNotFound, "404", "Сессия импорта не найдена")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) golden(w http.ResponseWriter, r *http.Request, u *auth.User) {
	g, err := s.Reports.GoldenCompare(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var req struct {
		Code string `json:"code"`
		GTIN string `json:"gtin"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "400", "Некорректный JSON в запросе")
		return
	}
	res := s.Mock.Verify(r.Context(), req.Code, req.GTIN)
	if !res.Available {
		writeJSON(w, http.StatusOK, map[string]any{"is_valid": false, "reason": rules.MockUnavailable,
			"message": rules.Message(rules.MockUnavailable, "")})
		return
	}
	var body map[string]any
	if err := json.Unmarshal(res.Raw, &body); err != nil {
		body = map[string]any{"is_valid": res.IsValid, "reason": res.Reason}
	}
	writeJSON(w, http.StatusOK, body)
}
