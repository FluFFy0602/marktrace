package reports

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"marktrace/backend/internal/rules"
)

// Resolution is a comment or close on a violation.
type Resolution struct {
	ID          string `json:"id"`
	ViolationID string `json:"violation_id"`
	Action      string `json:"action"`
	Comment     string `json:"comment"`
	Author      string `json:"author"`
	CreatedAt   string `json:"created_at"`
}

// Violation is one recorded violation.
type Violation struct {
	ViolationID  string       `json:"violation_id"`
	Source       string       `json:"source"`
	Ref          string       `json:"ref"`
	Code         *string      `json:"code"`
	ItemCode     *string      `json:"item_code"`
	Reason       string       `json:"reason"`
	Message      string       `json:"message"`
	AttemptID    *string      `json:"attempt_id"`
	ImportRowID  *string      `json:"import_row_id"`
	RulesVersion string       `json:"rules_version"`
	CreatedAt    string       `json:"created_at"`
	OpTs         *string      `json:"op_ts"`
	Actor        *string      `json:"actor"`
	State        string       `json:"state"`
	Resolutions  []Resolution `json:"resolutions"`
}

// ViolationFilter are the GET /violations filters.
type ViolationFilter struct {
	Reason, Source, State, ItemCode, Actor, From, To, Q string
	Code                                                string // exact code (code history)
	Limit, Offset                                       int
}

// ViolationsResult is the GET /violations response.
type ViolationsResult struct {
	Rows   []Violation    `json:"rows"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

const closedExpr = `EXISTS (SELECT 1 FROM violation_resolutions r WHERE r.violation_id = v.violation_id AND r.action = 'close')`

func (f ViolationFilter) where(includeReason bool) (*where, error) {
	w := &where{}
	if includeReason && f.Reason != "" {
		w.add("v.reason = ?", f.Reason)
	}
	if f.Source != "" {
		w.add("v.source = ?", f.Source)
	}
	switch f.State {
	case "":
	case "open":
		w.addRaw("NOT " + closedExpr)
	case "closed":
		w.addRaw(closedExpr)
	default:
		return nil, errBad("state")
	}
	if f.ItemCode != "" {
		w.add("v.item_code = ?", f.ItemCode)
	}
	if f.Actor != "" {
		w.add("v.actor = ?", f.Actor)
	}
	if f.Code != "" {
		w.add("v.code = ?", f.Code)
	}
	if f.Q != "" {
		// All placeholders refer to the same argument.
		w.add("(strpos(coalesce(v.code,''), ?) > 0 OR strpos(v.ref, ?) > 0 OR strpos(v.message, ?) > 0)", f.Q)
	}
	if err := addPeriod(w, "v.created_at", f.From, f.To); err != nil {
		return nil, err
	}
	return w, nil
}

type badFilter string

func (b badFilter) Error() string { return "bad filter: " + string(b) }
func (b badFilter) Unwrap() error { return ErrBadFilter }
func errBad(name string) error    { return badFilter(name) }

const violationCols = `v.violation_id::text, v.source, v.ref, v.code, v.item_code, v.reason, v.message,
	v.attempt_id::text, v.import_row_id::text, v.rules_version, v.created_at, v.op_ts, v.actor, ` + closedExpr

func scanViolation(rows pgx.Rows) (Violation, error) {
	var v Violation
	var created time.Time
	var opTs *time.Time
	var closed bool
	if err := rows.Scan(&v.ViolationID, &v.Source, &v.Ref, &v.Code, &v.ItemCode, &v.Reason, &v.Message,
		&v.AttemptID, &v.ImportRowID, &v.RulesVersion, &created, &opTs, &v.Actor, &closed); err != nil {
		return v, err
	}
	v.CreatedAt = rules.FormatTime(created)
	if opTs != nil {
		s := rules.FormatTime(*opTs)
		v.OpTs = &s
	}
	v.State = "open"
	if closed {
		v.State = "closed"
	}
	v.Resolutions = []Resolution{}
	return v, nil
}

// Violations lists violations with resolutions and counts by reason.
// Counts apply every filter except reason, so the UI can show the breakdown.
func (s *Service) Violations(ctx context.Context, f ViolationFilter) (*ViolationsResult, error) {
	w, err := f.where(true)
	if err != nil {
		return nil, err
	}
	res := &ViolationsResult{Rows: []Violation{}, Counts: map[string]int{}}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM violations v`+w.sql(), w.args...).Scan(&res.Total); err != nil {
		return nil, err
	}
	cw, _ := f.where(false)
	crows, err := s.Pool.Query(ctx, `SELECT v.reason, count(*) FROM violations v`+cw.sql()+` GROUP BY v.reason`, cw.args...)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var r string
		var n int
		if err := crows.Scan(&r, &n); err != nil {
			crows.Close()
			return nil, err
		}
		res.Counts[r] = n
	}
	crows.Close()

	q := w.clone()
	rows, err := s.Pool.Query(ctx, `SELECT `+violationCols+` FROM violations v`+q.sql()+
		` ORDER BY v.created_at DESC, v.violation_id LIMIT `+q.next(f.Limit)+` OFFSET `+q.next(f.Offset), q.args...)
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	ids := []string{}
	for rows.Next() {
		v, err := scanViolation(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		index[v.ViolationID] = len(res.Rows)
		ids = append(ids, v.ViolationID)
		res.Rows = append(res.Rows, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		rrows, err := s.Pool.Query(ctx, `SELECT id::text, violation_id::text, action, comment, author, created_at
			FROM violation_resolutions WHERE violation_id = ANY($1::uuid[]) ORDER BY created_at, id`, ids)
		if err != nil {
			return nil, err
		}
		for rrows.Next() {
			var r Resolution
			var created time.Time
			if err := rrows.Scan(&r.ID, &r.ViolationID, &r.Action, &r.Comment, &r.Author, &created); err != nil {
				rrows.Close()
				return nil, err
			}
			r.CreatedAt = rules.FormatTime(created)
			i := index[r.ViolationID]
			res.Rows[i].Resolutions = append(res.Rows[i].Resolutions, r)
		}
		rrows.Close()
		if err := rrows.Err(); err != nil {
			return nil, err
		}
	}
	return res, nil
}

var sourceOrder = map[string]int{"items": 0, "codes": 1, "operations": 2}

// ViolationExportRows returns rows `code,reason,source,ref` sorted per SPEC §7.
func (s *Service) ViolationExportRows(ctx context.Context, f ViolationFilter) ([][]string, error) {
	w, err := f.where(true)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT coalesce(v.code,''), v.reason, v.source, v.ref FROM violations v`+w.sql(), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]string{}
	for rows.Next() {
		var c, r, src, ref string
		if err := rows.Scan(&c, &r, &src, &ref); err != nil {
			return nil, err
		}
		out = append(out, []string{c, r, src, ref})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	SortViolationRows(out)
	return out, nil
}

// SortViolationRows sorts [code, reason, source, ref] rows: source order, ref, reason (bytes), then code.
func SortViolationRows(rows [][]string) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if sa, sb := sourceOrder[a[2]], sourceOrder[b[2]]; sa != sb {
			return sa < sb
		}
		if a[3] != b[3] {
			return a[3] < b[3]
		}
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		return a[0] < b[0]
	})
}

// Resolution errors.
var (
	ErrAlreadyClosed = errors.New("already closed")
	ErrBadAction     = errors.New("bad action")
	ErrEmptyComment  = errors.New("empty comment")
)

// Resolve adds a comment or closes a violation.
func (s *Service) Resolve(ctx context.Context, violationID, action, comment, author string) (*Resolution, error) {
	if action != "comment" && action != "close" {
		return nil, ErrBadAction
	}
	if comment == "" {
		return nil, ErrEmptyComment
	}
	if _, err := uuid.Parse(violationID); err != nil {
		return nil, ErrNotFound
	}
	var r Resolution
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM violations WHERE violation_id=$1::uuid)`, violationID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if action == "close" {
			var closed bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM violation_resolutions WHERE violation_id=$1::uuid AND action='close')`, violationID).Scan(&closed); err != nil {
				return err
			}
			if closed {
				return ErrAlreadyClosed
			}
		}
		var created time.Time
		r = Resolution{ID: uuid.NewString(), ViolationID: violationID, Action: action, Comment: comment, Author: author}
		if err := tx.QueryRow(ctx, `INSERT INTO violation_resolutions(id, violation_id, action, comment, author)
			VALUES ($1::uuid,$2::uuid,$3,$4,$5) RETURNING created_at`, r.ID, violationID, action, comment, author).Scan(&created); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrAlreadyClosed
			}
			return err
		}
		r.CreatedAt = rules.FormatTime(created)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &r, nil
}
