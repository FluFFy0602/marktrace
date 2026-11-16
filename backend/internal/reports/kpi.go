package reports

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"marktrace/backend/internal/rules"
)

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return round2(float64(part) * 100 / float64(total))
}

// KPI is the GET /kpi response.
type KPI struct {
	K1 struct {
		Accepted int     `json:"accepted"`
		Rejected int     `json:"rejected"`
		Percent  float64 `json:"percent"`
		Target   int     `json:"target"`
	} `json:"k1"`
	K2 struct {
		Consistent int     `json:"consistent"`
		Total      int     `json:"total"`
		Percent    float64 `json:"percent"`
	} `json:"k2"`
	K3 struct {
		LastSessionID *string `json:"last_session_id"`
		RowsCreated   *int    `json:"rows_created"`
		Reimport      bool    `json:"reimport"`
	} `json:"k3"`
	K4 struct {
		LastMs *float64 `json:"last_ms"`
	} `json:"k4"`
	K5 struct {
		Percent *float64 `json:"percent"`
	} `json:"k5"`
}

// KPI computes K1–K5 (SPEC §5).
func (s *Service) KPI(ctx context.Context) (*KPI, error) {
	k := &KPI{}
	k.K1.Target = 99
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE decision='accepted'), count(*) FILTER (WHERE decision='rejected')
		FROM operation_attempts`).Scan(&k.K1.Accepted, &k.K1.Rejected); err != nil {
		return nil, err
	}
	k.K1.Percent = percent(k.K1.Accepted, k.K1.Accepted+k.K1.Rejected)

	// K2: replay = status produced by the latest accepted operation by ts.
	if err := s.Pool.QueryRow(ctx, `WITH last AS (
			SELECT DISTINCT ON (code) code, op_type FROM operations ORDER BY code, ts DESC)
		SELECT count(*) FILTER (WHERE c.status = CASE l.op_type
				WHEN 'issue' THEN 'issued' WHEN 'introduce' THEN 'introduced' WHEN 'sell' THEN 'sold'
				WHEN 'return' THEN 'returned' WHEN 'dispose' THEN 'disposed' END),
			count(*)
		FROM codes c LEFT JOIN last l ON l.code = c.code`).Scan(&k.K2.Consistent, &k.K2.Total); err != nil {
		return nil, err
	}
	k.K2.Percent = percent(k.K2.Consistent, k.K2.Total)
	if k.K2.Total == 0 {
		k.K2.Percent = 100
	}

	// K3: the latest completed session with all three files whose content matches an
	// earlier completed full session (a real re-import of the same files); falls back to
	// the latest completed session. Content is compared by per-file sha256 (filename and
	// size for sessions recorded without a hash).
	var sid string
	var created *int
	err := s.Pool.QueryRow(ctx, `WITH full_sessions AS (
			SELECT session_id, rows_created, started_at,
				(SELECT string_agg((f->>'entity') || ':' ||
						coalesce(f->>'sha256', (f->>'filename') || '/' || (f->>'size')), ',' ORDER BY f->>'entity')
					FROM jsonb_array_elements(files) f) AS sig
			FROM import_sessions
			WHERE status = 'completed' AND jsonb_array_length(files) = 3)
		SELECT a.session_id::text, a.rows_created FROM full_sessions a
		WHERE EXISTS (SELECT 1 FROM full_sessions b WHERE b.sig = a.sig AND b.started_at < a.started_at)
		ORDER BY a.started_at DESC LIMIT 1`).
		Scan(&sid, &created)
	if err == nil {
		k.K3.LastSessionID, k.K3.RowsCreated, k.K3.Reimport = &sid, created, true
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = s.Pool.QueryRow(ctx, `SELECT session_id::text, rows_created FROM import_sessions
			WHERE status='completed' ORDER BY started_at DESC LIMIT 1`).Scan(&sid, &created)
		if err == nil {
			k.K3.LastSessionID, k.K3.RowsCreated = &sid, created
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	} else {
		return nil, err
	}

	k.K4.LastMs = s.LastReportMs()
	if g, err := s.GoldenCompare(ctx); err == nil && g.Error == "" {
		p := g.Percent
		k.K5.Percent = &p
	}
	return k, nil
}

// CompareSection is the diff of one golden file.
type CompareSection struct {
	Match        bool     `json:"match"`
	ExpectedRows int      `json:"expected_rows"`
	ActualRows   int      `json:"actual_rows"`
	Missing      []string `json:"missing"`
	Extra        []string `json:"extra"`
	matched      int
}

// GoldenResult is the GET /golden/compare response.
type GoldenResult struct {
	StatusSummary CompareSection `json:"status_summary"`
	Violations    CompareSection `json:"violations"`
	Percent       float64        `json:"percent"`
	Error         string         `json:"error,omitempty"`
}

// GoldenCompare compares current exports with GOLDEN_DIR/*.csv as multisets of rows.
func (s *Service) GoldenCompare(ctx context.Context) (*GoldenResult, error) {
	summary, err := s.StatusSummary(ctx)
	if err != nil {
		return nil, err
	}
	viol, err := s.ViolationExportRows(ctx, ViolationFilter{})
	if err != nil {
		return nil, err
	}
	res := &GoldenResult{}
	var errs []string
	expSummary, err := readGolden(filepath.Join(s.GoldenDir, "status_summary.csv"))
	if err != nil {
		errs = append(errs, err.Error())
	}
	expViol, err := readGolden(filepath.Join(s.GoldenDir, "violations.csv"))
	if err != nil {
		errs = append(errs, err.Error())
	}
	res.StatusSummary = compare(expSummary, summary)
	res.Violations = compare(expViol, viol)
	matched := res.StatusSummary.matched + res.Violations.matched
	denom := res.StatusSummary.ExpectedRows + res.Violations.ExpectedRows + len(res.StatusSummary.Extra) + len(res.Violations.Extra)
	res.Percent = 100
	if denom > 0 {
		res.Percent = percent(matched, denom)
	}
	if len(errs) > 0 {
		res.Error = "Эталонные файлы недоступны: " + strings.Join(errs, "; ")
		res.Percent = 0
		res.StatusSummary.Match, res.Violations.Match = false, false
	}
	return res, nil
}

func readGolden(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	if len(recs) > 0 {
		recs = recs[1:] // header
	}
	return recs, nil
}

func csvLine(rec []string) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Write(rec)
	w.Flush()
	return strings.TrimRight(b.String(), "\r\n")
}

func compare(expected, actual [][]string) CompareSection {
	c := CompareSection{ExpectedRows: len(expected), ActualRows: len(actual), Missing: []string{}, Extra: []string{}}
	pool := map[string]int{}
	for _, r := range actual {
		pool[csvLine(r)]++
	}
	for _, r := range expected {
		l := csvLine(r)
		if pool[l] > 0 {
			pool[l]--
			c.matched++
		} else {
			c.Missing = append(c.Missing, l)
		}
	}
	for _, r := range actual {
		l := csvLine(r)
		if pool[l] > 0 {
			pool[l]--
			c.Extra = append(c.Extra, l)
		}
	}
	c.Match = len(c.Missing) == 0 && len(c.Extra) == 0
	return c
}

// ImportSession is an import session with counters.
type ImportSession struct {
	SessionID     string          `json:"session_id"`
	StartedBy     string          `json:"started_by"`
	Role          string          `json:"role"`
	StartedAt     string          `json:"started_at"`
	FinishedAt    *string         `json:"finished_at"`
	Status        string          `json:"status"`
	FailureReason *string         `json:"failure_reason"`
	Files         json.RawMessage `json:"files"`
	Counters      json.RawMessage `json:"counters"`
	RowsCreated   *int            `json:"rows_created"`
	RejectedRows  []ImportRow     `json:"rejected_rows"`
	// SnapshotViolations is only set in the POST /api/imports response.
	SnapshotViolations *int `json:"snapshot_violations,omitempty"`
}

// ImportRow is a processed import row.
type ImportRow struct {
	RowID            string          `json:"row_id"`
	Entity           string          `json:"entity"`
	LineNo           int             `json:"line_no"`
	Key              string          `json:"key"`
	Raw              json.RawMessage `json:"raw"`
	ProcessingStatus string          `json:"processing_status"`
	Reason           *string         `json:"reason"`
	Message          *string         `json:"message"`
}

const sessionCols = `session_id::text, started_by, role, started_at, finished_at, status, failure_reason,
	files::text, coalesce(counters, '{}'::jsonb)::text, rows_created`

func scanSession(row pgx.Row) (*ImportSession, error) {
	var s ImportSession
	var started time.Time
	var finished *time.Time
	var files, counters string
	if err := row.Scan(&s.SessionID, &s.StartedBy, &s.Role, &started, &finished, &s.Status, &s.FailureReason,
		&files, &counters, &s.RowsCreated); err != nil {
		return nil, err
	}
	s.StartedAt = rules.FormatTime(started)
	if finished != nil {
		f := rules.FormatTime(*finished)
		s.FinishedAt = &f
	}
	s.Files, s.Counters = json.RawMessage(files), json.RawMessage(counters)
	s.RejectedRows = []ImportRow{}
	return &s, nil
}

// Imports lists recent sessions, newest first.
func (s *Service) Imports(ctx context.Context) ([]*ImportSession, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+sessionCols+` FROM import_sessions ORDER BY started_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ImportSession{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// Import returns one session with its rejected rows.
func (s *Service) Import(ctx context.Context, id string) (*ImportSession, error) {
	sess, err := scanSession(s.Pool.QueryRow(ctx, `SELECT `+sessionCols+` FROM import_sessions WHERE session_id::text=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT r.row_id::text, r.entity, r.line_no, r.key, r.raw::text, r.processing_status, r.reason,
			(SELECT v.message FROM violations v WHERE v.import_row_id = r.row_id LIMIT 1)
		FROM import_rows r WHERE r.session_id::text=$1 AND r.processing_status='rejected'
		ORDER BY CASE r.entity WHEN 'items' THEN 0 WHEN 'codes' THEN 1 ELSE 2 END, r.line_no`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r ImportRow
		var raw string
		if err := rows.Scan(&r.RowID, &r.Entity, &r.LineNo, &r.Key, &raw, &r.ProcessingStatus, &r.Reason, &r.Message); err != nil {
			return nil, err
		}
		r.Raw = json.RawMessage(raw)
		sess.RejectedRows = append(sess.RejectedRows, r)
	}
	return sess, rows.Err()
}
