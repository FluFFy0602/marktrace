package reports

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"marktrace/backend/internal/ops"
	"marktrace/backend/internal/rules"
)

// Service serves read models.
type Service struct {
	Pool      *pgxpool.Pool
	GoldenDir string

	mu     sync.Mutex
	lastMs *float64
}

// CodeFilter are the GET /codes filters.
type CodeFilter struct {
	Status, ItemCode, Actor, From, To, Q string
	Limit, Offset                        int
}

// CodesResult is the GET /codes response.
type CodesResult struct {
	Rows      []ops.CodeRow  `json:"rows"`
	Total     int            `json:"total"`
	Counters  map[string]int `json:"counters"`
	ElapsedMs float64        `json:"elapsed_ms"`
}

func (f CodeFilter) where(includeStatus bool) (*where, error) {
	w := &where{}
	if includeStatus && f.Status != "" {
		w.add("status = ?", f.Status)
	}
	if f.ItemCode != "" {
		w.add("item_code = ?", f.ItemCode)
	}
	if f.Actor != "" {
		w.add("last_actor = ?", f.Actor)
	}
	if f.Q != "" {
		w.add("strpos(code, ?) > 0", f.Q)
	}
	if err := addPeriod(w, "ts", f.From, f.To); err != nil {
		return nil, err
	}
	return w, nil
}

// Codes runs the status report; its duration is K4.
func (s *Service) Codes(ctx context.Context, f CodeFilter) (*CodesResult, error) {
	start := time.Now()
	w, err := f.where(true)
	if err != nil {
		return nil, err
	}
	// Counters respect every filter except status, so the status tiles keep the full breakdown
	// while one of them is selected.
	cw, _ := f.where(false)
	res := &CodesResult{Rows: []ops.CodeRow{}, Counters: map[string]int{}}
	for _, st := range rules.Statuses {
		res.Counters[st] = 0
	}
	rows, err := s.Pool.Query(ctx, `SELECT status, count(*) FROM codes`+cw.sql()+` GROUP BY status`, cw.args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return nil, err
		}
		res.Counters[st] = n
		if f.Status == "" || f.Status == st {
			res.Total += n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	q := w.clone()
	sql := `SELECT code, item_code, status, ts, last_actor FROM codes` + q.sql() +
		` ORDER BY code COLLATE "C" LIMIT ` + q.next(f.Limit) + ` OFFSET ` + q.next(f.Offset)
	res.Rows, err = s.queryCodeRows(ctx, sql, q.args...)
	if err != nil {
		return nil, err
	}
	ms := math.Round(float64(time.Since(start).Microseconds())/10) / 100
	res.ElapsedMs = ms
	s.mu.Lock()
	s.lastMs = &ms
	s.mu.Unlock()
	return res, nil
}

// LastReportMs returns the duration of the last status report (K4).
func (s *Service) LastReportMs() *float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastMs
}

func (s *Service) queryCodeRows(ctx context.Context, sql string, args ...any) ([]ops.CodeRow, error) {
	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ops.CodeRow{}
	for rows.Next() {
		var r ops.CodeRow
		var ts time.Time
		if err := rows.Scan(&r.Code, &r.ItemCode, &r.Status, &ts, &r.LastActor); err != nil {
			return nil, err
		}
		r.LastTs = rules.FormatTime(ts)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CodesCSV writes the filtered status export, sorted by code.
func (s *Service) CodesCSV(ctx context.Context, f CodeFilter, out io.Writer) error {
	w, err := f.where(true)
	if err != nil {
		return err
	}
	list, err := s.queryCodeRows(ctx, `SELECT code, item_code, status, ts, last_actor FROM codes`+w.sql()+
		` ORDER BY code COLLATE "C"`, w.args...)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(out)
	cw.Write([]string{"code", "item_code", "status", "last_ts"})
	for _, r := range list {
		cw.Write([]string{r.Code, r.ItemCode, r.Status, r.LastTs})
	}
	cw.Flush()
	return cw.Error()
}

// StatusSummary returns [status, count] rows for all statuses in lifecycle order.
func (s *Service) StatusSummary(ctx context.Context) ([][]string, error) {
	counts := map[string]int{}
	rows, err := s.Pool.Query(ctx, `SELECT status, count(*) FROM codes GROUP BY status`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return nil, err
		}
		counts[st] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([][]string, 0, len(rules.Statuses))
	for _, st := range rules.Statuses {
		out = append(out, []string{st, fmt.Sprint(counts[st])})
	}
	return out, nil
}

// WriteCSV writes a header and rows.
func WriteCSV(out io.Writer, header []string, rows [][]string) error {
	cw := csv.NewWriter(out)
	cw.Write(header)
	for _, r := range rows {
		cw.Write(r)
	}
	cw.Flush()
	return cw.Error()
}

// Item is a catalogue entry.
type Item struct {
	ItemCode string `json:"item_code"`
	Name     string `json:"name"`
	GTIN     string `json:"gtin"`
}

// Items lists the catalogue.
func (s *Service) Items(ctx context.Context) ([]Item, error) {
	rows, err := s.Pool.Query(ctx, `SELECT item_code, name, gtin FROM items ORDER BY item_code COLLATE "C"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ItemCode, &it.Name, &it.GTIN); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Registry is the binding of a code.
type Registry struct {
	Code           string  `json:"code"`
	ItemCode       string  `json:"item_code"`
	ExpectedStatus *string `json:"expected_status"`
	ExpectedTs     *string `json:"expected_ts"`
}

// Operation is an accepted operation.
type Operation struct {
	OpID      string  `json:"op_id"`
	Code      string  `json:"code"`
	OpType    string  `json:"op_type"`
	Ts        string  `json:"ts"`
	Actor     string  `json:"actor"`
	AttemptID *string `json:"attempt_id"`
	CreatedAt string  `json:"created_at"`
}

// Attempt is an operation attempt.
type Attempt struct {
	AttemptID    string  `json:"attempt_id"`
	Source       string  `json:"source"`
	ImportRowID  *string `json:"import_row_id"`
	OpID         string  `json:"op_id"`
	Code         string  `json:"code"`
	OpType       string  `json:"op_type"`
	TsRaw        string  `json:"ts_raw"`
	Actor        string  `json:"actor"`
	Initiator    string  `json:"initiator"`
	Role         string  `json:"role"`
	Decision     string  `json:"decision"`
	Reason       *string `json:"reason"`
	Message      *string `json:"message"`
	MockResult   any     `json:"mock_result"`
	RulesVersion string  `json:"rules_version"`
	CreatedAt    string  `json:"created_at"`
}

// CodeDetail is the GET /codes/{code} response.
type CodeDetail struct {
	Code       string       `json:"code"`
	Current    *ops.CodeRow `json:"current"`
	Registry   *Registry    `json:"registry"`
	Item       *Item        `json:"item"`
	Operations []Operation  `json:"operations"`
	Attempts   []Attempt    `json:"attempts"`
	Violations []Violation  `json:"violations"`
}

// ErrNotFound is returned when nothing is known about the requested entity.
var ErrNotFound = errors.New("not found")

// CodeDetail returns the history of a code.
func (s *Service) CodeDetail(ctx context.Context, code string) (*CodeDetail, error) {
	d := &CodeDetail{Code: code, Operations: []Operation{}, Attempts: []Attempt{}, Violations: []Violation{}}
	cur, err := ops.LoadCodeRow(ctx, s.Pool, code)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	d.Current = cur

	var reg Registry
	var expTs *time.Time
	err = s.Pool.QueryRow(ctx, `SELECT code, item_code, expected_status, expected_ts FROM code_registry WHERE code=$1`, code).
		Scan(&reg.Code, &reg.ItemCode, &reg.ExpectedStatus, &expTs)
	if err == nil {
		if expTs != nil {
			v := rules.FormatTime(*expTs)
			reg.ExpectedTs = &v
		}
		d.Registry = &reg
		var it Item
		if err := s.Pool.QueryRow(ctx, `SELECT item_code, name, gtin FROM items WHERE item_code=$1`, reg.ItemCode).
			Scan(&it.ItemCode, &it.Name, &it.GTIN); err == nil {
			d.Item = &it
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	rows, err := s.Pool.Query(ctx, `SELECT op_id, code, op_type, ts, actor, attempt_id::text, created_at
		FROM operations WHERE code=$1 ORDER BY ts`, code)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var o Operation
		var ts, created time.Time
		if err := rows.Scan(&o.OpID, &o.Code, &o.OpType, &ts, &o.Actor, &o.AttemptID, &created); err != nil {
			rows.Close()
			return nil, err
		}
		o.Ts, o.CreatedAt = rules.FormatTime(ts), rules.FormatTime(created)
		d.Operations = append(d.Operations, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	d.Attempts, err = s.attempts(ctx, `a.code = $1`, code)
	if err != nil {
		return nil, err
	}
	vr, err := s.Violations(ctx, ViolationFilter{Code: code, Limit: 1000})
	if err != nil {
		return nil, err
	}
	d.Violations = vr.Rows
	if d.Current == nil && d.Registry == nil && len(d.Attempts) == 0 && len(d.Violations) == 0 {
		return nil, ErrNotFound
	}
	return d, nil
}

func (s *Service) attempts(ctx context.Context, cond string, args ...any) ([]Attempt, error) {
	rows, err := s.Pool.Query(ctx, `SELECT a.attempt_id::text, a.source, a.import_row_id::text, a.op_id, a.code, a.op_type,
		a.ts_raw, a.actor, a.initiator, a.role, a.decision, a.reason,
		(SELECT v.message FROM violations v WHERE v.attempt_id = a.attempt_id LIMIT 1),
		a.mock_result, a.rules_version, a.created_at
		FROM operation_attempts a WHERE `+cond+` ORDER BY a.created_at, a.attempt_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attempt{}
	for rows.Next() {
		var a Attempt
		var created time.Time
		if err := rows.Scan(&a.AttemptID, &a.Source, &a.ImportRowID, &a.OpID, &a.Code, &a.OpType, &a.TsRaw,
			&a.Actor, &a.Initiator, &a.Role, &a.Decision, &a.Reason, &a.Message, &a.MockResult,
			&a.RulesVersion, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = rules.FormatTime(created)
		out = append(out, a)
	}
	return out, rows.Err()
}
